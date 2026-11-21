package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/agent"
	"gorm.io/gorm"
)

type agentHTTPPerfTraceKey struct{}

type agentHTTPPerfTrace struct {
	mu              sync.Mutex
	transactionTime time.Duration
	transactions    int
	lockErrors      int
	interval        *agentHTTPPerfInterval
}

func (t *agentHTTPPerfTrace) record(duration time.Duration, err error) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.transactionTime += duration
	t.transactions++
	if t.interval != nil {
		t.interval.TransactionEnded = time.Now()
	}
	if err != nil && (strings.Contains(strings.ToLower(err.Error()), "locked") || strings.Contains(strings.ToLower(err.Error()), "busy")) {
		t.lockErrors++
	}
}

// This transparent fixture-only pool preserves the actual source *sql.DB,
// BEGIN/COMMIT behavior, and all application repositories. It adds no extra
// SQL and records only a business request's real transaction wall time.
type agentHTTPPerfPool struct{ *sql.DB }

func (p *agentHTTPPerfPool) GetDBConn() (*sql.DB, error) { return p.DB, nil }
func (p *agentHTTPPerfPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	started := time.Now()
	tx, err := p.DB.BeginTx(ctx, options)
	trace, _ := ctx.Value(agentHTTPPerfTraceKey{}).(*agentHTTPPerfTrace)
	if trace != nil && trace.interval != nil {
		trace.interval.TransactionStarted = started
		trace.recordDBPhase("begin", started, time.Now(), err != nil)
	}
	if err != nil {
		trace.record(time.Since(started), err)
		return nil, err
	}
	return &agentHTTPPerfTx{Tx: tx, db: p.DB, started: started, trace: trace}, nil
}

type agentHTTPPerfTx struct {
	*sql.Tx
	db      *sql.DB
	started time.Time
	trace   *agentHTTPPerfTrace
	once    sync.Once
}

func (t *agentHTTPPerfTx) GetDBConn() (*sql.DB, error) { return t.db, nil }
func (t *agentHTTPPerfTx) Commit() error {
	var started time.Time
	if t.trace != nil && t.trace.interval != nil {
		started = time.Now()
	}
	err := t.Tx.Commit()
	elapsed := time.Since(t.started)
	var ended time.Time
	if !started.IsZero() {
		ended = time.Now()
	}
	t.once.Do(func() { t.trace.record(elapsed, err) })
	if !started.IsZero() {
		t.trace.recordDBPhase("commit", started, ended, err != nil)
	}
	return err
}
func (t *agentHTTPPerfTx) Rollback() error {
	err := t.Tx.Rollback()
	t.once.Do(func() { t.trace.record(time.Since(t.started), err) })
	return err
}
func (t *agentHTTPPerfTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	var started time.Time
	if t.trace != nil && t.trace.interval != nil {
		started = time.Now()
	}
	result, err := t.Tx.ExecContext(ctx, query, args...)
	if !started.IsZero() {
		t.trace.recordDBPhase("exec", started, time.Now(), err != nil)
	}
	if err != nil && t.trace != nil && (strings.Contains(strings.ToLower(err.Error()), "locked") || strings.Contains(strings.ToLower(err.Error()), "busy")) {
		t.trace.mu.Lock()
		t.trace.lockErrors++
		t.trace.mu.Unlock()
	}
	return result, err
}

type agentHTTPPerfResponseWriter struct {
	http.ResponseWriter
	trace       *agentHTTPPerfTrace
	wroteHeader bool
}

func (w *agentHTTPPerfResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.trace.mu.Lock()
	w.Header().Set("X-CRM-Test-Transaction-Ns", strconv.FormatInt(int64(w.trace.transactionTime), 10))
	w.Header().Set("X-CRM-Test-Transactions", strconv.Itoa(w.trace.transactions))
	w.Header().Set("X-CRM-Test-Lock-Errors", strconv.Itoa(w.trace.lockErrors))
	w.trace.mu.Unlock()
	w.ResponseWriter.WriteHeader(code)
}
func (w *agentHTTPPerfResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

type agentHTTPPerfDistribution struct {
	Count int     `json:"count"`
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
	P99MS float64 `json:"p99_ms"`
	MaxMS float64 `json:"max_ms"`
}

type agentHTTPPerfGroup struct {
	Name                     string                    `json:"name"`
	StartedAt                time.Time                 `json:"started_at"`
	FinishedAt               time.Time                 `json:"finished_at"`
	HTTP                     agentHTTPPerfDistribution `json:"http"`
	Transaction              agentHTTPPerfDistribution `json:"transaction"`
	ScheduleLag              agentHTTPPerfDistribution `json:"scheduled_arrival_lag"`
	BothActiveHTTP           agentHTTPPerfDistribution `json:"both_active_http"`
	BothActiveTransaction    agentHTTPPerfDistribution `json:"both_active_transaction"`
	Samples                  int                       `json:"scheduled_samples"`
	IdleTailSamples          int                       `json:"idle_tail_samples"`
	HTTPErrors               int                       `json:"http_errors"`
	LockErrors               int                       `json:"lock_errors"`
	MissingTransactionTraces int                       `json:"missing_transaction_traces"`
	httpDurations            []time.Duration
	transactionDurations     []time.Duration
	bothHTTP                 []time.Duration
	bothTransactions         []time.Duration
	lag                      []time.Duration
}

type agentHTTPPerfRun struct {
	Actor         string  `json:"actor"`
	ID            string  `json:"run_id"`
	Status        string  `json:"status"`
	DurationMS    float64 `json:"duration_ms"`
	SSEBytes      int64   `json:"sse_bytes"`
	Results       int     `json:"results"`
	VerifiedLeads int64   `json:"verified_leads"`
	Error         string  `json:"error,omitempty"`
}

func agentHTTPPerfPercentiles(values []time.Duration) agentHTTPPerfDistribution {
	if len(values) == 0 {
		return agentHTTPPerfDistribution{}
	}
	ordered := append([]time.Duration{}, values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	get := func(p float64) float64 {
		index := int(math.Ceil(float64(len(ordered))*p)) - 1
		if index < 0 {
			index = 0
		}
		return float64(ordered[index]) / float64(time.Millisecond)
	}
	return agentHTTPPerfDistribution{Count: len(values), P50MS: get(.5), P95MS: get(.95), P99MS: get(.99), MaxMS: float64(ordered[len(ordered)-1]) / float64(time.Millisecond)}
}

func newAgentHTTPPerfProvider(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	calls := &atomic.Int64{}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// Four genuine stream requests execute describe/query/present/final.
		// Waiting simulates provider latency, rather than returning canned
		// Agent results or bypassing the application tool loop.
		timer := time.NewTimer(350 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
		}
		browserAgentModelHandler(w, r)
	}))
	t.Cleanup(model.Close)
	return model, calls
}

// TestAgentHTTPBusinessLatencyWithTwoRuns is an explicit, standalone acceptance
// test. A separate process may be compiling the suite, but no other performance
// test, build, or browser regression should run during its sampling period.
//
// CRM_AGENT_HTTP_PERF=1 GOTOOLCHAIN=go1.25.13 go test ./cmd/server -run '^TestAgentHTTPBusinessLatencyWithTwoRuns$' -count=1 -v
func TestAgentHTTPBusinessLatencyWithTwoRuns(t *testing.T) {
	if os.Getenv("CRM_AGENT_HTTP_PERF") != "1" {
		t.Skip("standalone HTTP performance acceptance: set CRM_AGENT_HTTP_PERF=1 and use GOTOOLCHAIN=go1.25.13")
	}
	diagnostics := newAgentHTTPPerfDiagnostics(os.Getenv("CRM_AGENT_HTTP_DIAGNOSTICS"))
	model, modelCalls := newAgentHTTPPerfProvider(t)
	var db *gorm.DB
	router := newTestRouterWithOptions(t, testRouterOptions{agentModelURL: model.URL, agentFixtureDB: &db})
	if db == nil {
		t.Fatal("fixture database was not captured")
	}
	admin := issueDevToken(t, router, "admin_001", "admin")
	manager := issueDevToken(t, router, "manager_001", "manager")
	sales := issueDevToken(t, router, "sales_001", "sales")
	if err := db.Exec(`WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v+1 FROM n WHERE v<10001)
INSERT INTO leads(id,lead_no,customer_name,source_channel,status,business_stage,ownership_state,result_status,star_rating,owner_user_id,assigned_sales_user_id,inviter_user_id,current_owner_user_id,current_owner_role,created_by_user_id,created_by_role,deal_amount,refund_amount,payment_status,created_at,updated_at,current_owner_started_at,next_followup_due_at)
SELECT v,'http-perf-'||v,'HTTP测试客户'||v,'source-'||(v%17),'following','invitation','private','active',5,'sales_001','sales_001','sales_001','sales_001','inviter','manager_001','manager',0,0,'unpaid','2026-10-08 00:00:00+00:00','2026-10-08 00:00:00+00:00','2026-10-08 00:00:00+00:00','2026-10-09 00:00:00+00:00' FROM n`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v+1 FROM n WHERE v<50001)
INSERT INTO lead_followups(id,lead_id,sales_user_id,followup_result,intention_level,star_rating,remark,is_valid,created_at)
SELECT v,((v-1)%10001)+1,'sales_001','connected','high',5,'历史跟进希望参加校园参观',1,'2026-10-08 00:30:00+00:00' FROM n`).Error; err != nil {
		t.Fatal(err)
	}
	var sqliteVersion string
	if err := db.Raw("SELECT sqlite_version()").Scan(&sqliteVersion).Error; err != nil {
		t.Fatal(err)
	}
	pooled, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	tracedPool := &agentHTTPPerfPool{DB: pooled}
	db.ConnPool = tracedPool
	db.Statement.ConnPool = tracedPool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-CRM-Test-Sample") != "" && strings.HasSuffix(r.URL.Path, "/followups") {
			trace := &agentHTTPPerfTrace{}
			if diagnostics != nil {
				sequence, _ := strconv.Atoi(r.Header.Get("X-CRM-Test-Sample"))
				trace.interval = &agentHTTPPerfInterval{Sequence: sequence, ServerStarted: time.Now()}
				defer func() {
					trace.interval.ServerEnded = time.Now()
					diagnostics.recordServer(*trace.interval)
				}()
			}
			r = r.WithContext(context.WithValue(r.Context(), agentHTTPPerfTraceKey{}, trace))
			router.ServeHTTP(&agentHTTPPerfResponseWriter{ResponseWriter: w, trace: trace}, r)
			return
		}
		router.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	transport := &http.Transport{Proxy: nil, MaxIdleConns: 10, MaxIdleConnsPerHost: 10, MaxConnsPerHost: 10, ResponseHeaderTimeout: 10 * time.Second}
	client := &http.Client{Transport: transport}
	t.Cleanup(transport.CloseIdleConnections)
	request := func(ctx context.Context, method, path, token string, body any) (int, []byte, http.Header, error) {
		var encoded []byte
		var err error
		if body != nil {
			encoded, err = json.Marshal(body)
			if err != nil {
				return 0, nil, nil, err
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(encoded))
		if err != nil {
			return 0, nil, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, nil, err
		}
		defer resp.Body.Close()
		payload, err := io.ReadAll(resp.Body)
		return resp.StatusCode, payload, resp.Header, err
	}
	// Create each private conversation using the real JWT-authenticated route;
	// neither a direct database Run insert nor Service.Start bypass is used.
	var conversations [2]string
	for i, token := range []string{manager, admin} {
		code, body, _, err := request(t.Context(), http.MethodPost, "/api/agent/conversations", token, map[string]any{"title": "HTTP性能验收"})
		if err != nil || code != 201 {
			t.Fatalf("create conversation: status=%d err=%v body=%s", code, err, body)
		}
		var conversation agent.Conversation
		if json.Unmarshal(body, &conversation) != nil || conversation.ID == "" {
			t.Fatal("invalid conversation response")
		}
		conversations[i] = conversation.ID
	}
	sequence := 0
	payload := []byte(`{"followup_result":"connected","intention_level":"high","remark":"HTTP性能回归"}`)
	sample := func() (time.Duration, time.Duration, int, int, error) {
		sequence++
		leadID := (sequence-1)%10001 + 1
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/api/leads/%d/followups", server.URL, leadID), bytes.NewReader(payload))
		if err != nil {
			return 0, 0, 0, 0, err
		}
		req.Header.Set("Authorization", "Bearer "+sales)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CRM-Test-Sample", strconv.Itoa(sequence))
		started := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			return time.Since(started), 0, 0, 0, err
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		elapsed := time.Since(started)
		if diagnostics != nil {
			diagnostics.recordClient(sequence, started, elapsed)
		}
		ns, parseErr := strconv.ParseInt(resp.Header.Get("X-CRM-Test-Transaction-Ns"), 10, 64)
		count, _ := strconv.Atoi(resp.Header.Get("X-CRM-Test-Transactions"))
		locks, _ := strconv.Atoi(resp.Header.Get("X-CRM-Test-Lock-Errors"))
		if readErr != nil {
			return elapsed, time.Duration(ns), count, locks, readErr
		}
		if resp.StatusCode != http.StatusOK {
			return elapsed, time.Duration(ns), count, locks, fmt.Errorf("followup status=%d body=%s", resp.StatusCode, body)
		}
		if parseErr != nil || count != 1 || ns <= 0 {
			return elapsed, time.Duration(ns), count, locks, fmt.Errorf("missing real transaction timing: count=%d ns=%d err=%v", count, ns, parseErr)
		}
		var result struct{ Success bool }
		if json.Unmarshal(body, &result) != nil || !result.Success {
			return elapsed, time.Duration(ns), count, locks, fmt.Errorf("followup response did not confirm success: %s", body)
		}
		return elapsed, time.Duration(ns), count, locks, nil
	}
	for i := 0; i < 20; i++ {
		if _, _, _, _, err := sample(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	measure := func(name string, count int, active *atomic.Int32) agentHTTPPerfGroup {
		group := agentHTTPPerfGroup{Name: name, StartedAt: time.Now().UTC()}
		next := time.Now()
		for i := 0; i < count; i++ {
			if wait := time.Until(next); wait > 0 {
				timer := time.NewTimer(wait)
				<-timer.C
			}
			before := int32(0)
			if active != nil {
				before = active.Load()
			}
			start := time.Now()
			lag := start.Sub(next)
			if lag < 0 {
				lag = 0
			}
			httpDuration, transactionDuration, transactions, locks, err := sample()
			after := int32(0)
			if active != nil {
				after = active.Load()
			}
			group.Samples++
			group.LockErrors += locks
			if err != nil {
				group.HTTPErrors++
				if group.HTTPErrors <= 3 {
					t.Logf("%s request failure: %v", name, err)
				}
			}
			if transactions != 1 {
				group.MissingTransactionTraces++
			}
			if active == nil || before > 0 || after > 0 {
				group.httpDurations = append(group.httpDurations, httpDuration)
				group.transactionDurations = append(group.transactionDurations, transactionDuration)
				group.lag = append(group.lag, lag)
			} else {
				group.IdleTailSamples++
			}
			if before == 2 && after == 2 {
				group.bothHTTP = append(group.bothHTTP, httpDuration)
				group.bothTransactions = append(group.bothTransactions, transactionDuration)
			}
			next = next.Add(20 * time.Millisecond)
			if next.Before(time.Now()) {
				next = time.Now().Add(20 * time.Millisecond)
			}
		}
		group.FinishedAt = time.Now().UTC()
		group.HTTP = agentHTTPPerfPercentiles(group.httpDurations)
		group.Transaction = agentHTTPPerfPercentiles(group.transactionDurations)
		group.ScheduleLag = agentHTTPPerfPercentiles(group.lag)
		group.BothActiveHTTP = agentHTTPPerfPercentiles(group.bothHTTP)
		group.BothActiveTransaction = agentHTTPPerfPercentiles(group.bothTransactions)
		return group
	}
	groups := []agentHTTPPerfGroup{measure("baseline_before", 150, nil)}
	var active atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	startedRuns := make(chan error, 2)
	runReports := make(chan agentHTTPPerfRun, 2)
	gate := make(chan struct{})
	for i, token := range []string{manager, admin} {
		wg.Add(1)
		go func(i int, token string) {
			defer wg.Done()
			report := agentHTTPPerfRun{Actor: []string{"manager", "admin"}[i]}
			defer func() { runReports <- report }()
			<-gate
			started := time.Now()
			code, body, _, err := request(ctx, http.MethodPost, "/api/agent/conversations/"+conversations[i]+"/runs", token, map[string]any{"message": "按来源统计全部线索并展示表格", "request_id": fmt.Sprintf("http-performance-run-%d-%d", time.Now().UnixNano(), i)})
			if err != nil || code != 202 {
				report.Error = fmt.Sprintf("start status=%d err=%v body=%s", code, err, body)
				startedRuns <- fmt.Errorf("%s", report.Error)
				return
			}
			var run agent.Run
			if json.Unmarshal(body, &run) != nil || run.ID == "" {
				report.Error = "invalid run response"
				startedRuns <- fmt.Errorf("%s", report.Error)
				return
			}
			report.ID = run.ID
			active.Add(1)
			defer active.Add(-1)
			startedRuns <- nil
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/agent/runs/"+run.ID+"/events", nil)
			if err != nil {
				report.Error = err.Error()
				return
			}
			req.Header.Set("Authorization", "Bearer "+token)
			stream, err := client.Do(req)
			if err != nil {
				report.Error = err.Error()
				return
			}
			if stream.StatusCode != 200 {
				stream.Body.Close()
				report.Error = fmt.Sprintf("SSE status=%d", stream.StatusCode)
				return
			}
			report.SSEBytes, err = io.Copy(io.Discard, stream.Body)
			stream.Body.Close()
			if err != nil {
				report.Error = err.Error()
				return
			}
			code, body, _, err = request(ctx, http.MethodGet, "/api/agent/runs/"+run.ID, token, nil)
			if err != nil || code != 200 {
				report.Error = fmt.Sprintf("completed run status=%d err=%v", code, err)
				return
			}
			var output struct {
				Run     agent.Run      `json:"run"`
				Results []agent.Result `json:"results"`
			}
			if json.Unmarshal(body, &output) != nil {
				report.Error = "invalid completed run response"
				return
			}
			report.Status = output.Run.Status
			report.Results = len(output.Results)
			report.DurationMS = float64(time.Since(started)) / float64(time.Millisecond)
			if output.Run.Status != "completed" {
				report.Error = "analysis did not complete: " + output.Run.Error
				return
			}
			if len(output.Results) == 0 {
				report.Error = "real query result missing"
				return
			}
			for _, row := range output.Results[0].Rows {
				if len(row) > 1 {
					if value, ok := row[1].(float64); ok {
						report.VerifiedLeads += int64(value)
					}
				}
			}
			if report.VerifiedLeads != 10001 {
				report.Error = fmt.Sprintf("query did not cover all 10001 leads: %d", report.VerifiedLeads)
			}
		}(i, token)
	}
	close(gate)
	for i := 0; i < 2; i++ {
		if err := <-startedRuns; err != nil {
			cancel()
			wg.Wait()
			t.Fatal(err)
		}
	}
	groups = append(groups, measure("two_agent_runs_active_first", 150, &active))
	for active.Load() > 0 {
		groups = append(groups, measure(fmt.Sprintf("two_agent_runs_active_continued_%d", len(groups)), 100, &active))
	}
	wg.Wait()
	close(runReports)
	var runs []agentHTTPPerfRun
	for report := range runReports {
		runs = append(runs, report)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].Actor < runs[j].Actor })
	groups = append(groups, measure("baseline_after", 150, nil))
	if diagnostics != nil {
		if err := diagnostics.save(db, runs); err != nil {
			t.Errorf("save optional diagnostic intervals: %v", err)
		}
	}
	var baselineHTTP, baselineTransactions, bothHTTP, bothTransactions []time.Duration
	errors, lockErrors, missingTraces := 0, 0, 0
	for _, group := range groups {
		errors += group.HTTPErrors
		lockErrors += group.LockErrors
		missingTraces += group.MissingTransactionTraces
		if strings.HasPrefix(group.Name, "baseline_") {
			baselineHTTP = append(baselineHTTP, group.httpDurations...)
			baselineTransactions = append(baselineTransactions, group.transactionDurations...)
		} else {
			bothHTTP = append(bothHTTP, group.bothHTTP...)
			bothTransactions = append(bothTransactions, group.bothTransactions...)
		}
	}
	baseHTTP, activeHTTP := agentHTTPPerfPercentiles(baselineHTTP), agentHTTPPerfPercentiles(bothHTTP)
	baseTxn, activeTxn := agentHTTPPerfPercentiles(baselineTransactions), agentHTTPPerfPercentiles(bothTransactions)
	httpRatio := activeHTTP.P95MS / baseHTTP.P95MS
	transactionRatio := activeTxn.P95MS / baseTxn.P95MS
	evidence := struct {
		Type                     string                    `json:"type"`
		Timestamp                time.Time                 `json:"timestamp"`
		GoVersion                string                    `json:"go_version"`
		GOOS                     string                    `json:"goos"`
		GOARCH                   string                    `json:"goarch"`
		CPUs                     int                       `json:"cpus"`
		GOMAXPROCS               int                       `json:"gomaxprocs"`
		SQLiteVersion            string                    `json:"sqlite_version"`
		InitialLeads             int                       `json:"initial_leads"`
		InitialFollowups         int                       `json:"initial_followups"`
		Groups                   []agentHTTPPerfGroup      `json:"groups"`
		Runs                     []agentHTTPPerfRun        `json:"runs"`
		ModelCalls               int64                     `json:"model_calls"`
		BaselineHTTP             agentHTTPPerfDistribution `json:"baseline_http"`
		TwoActiveHTTP            agentHTTPPerfDistribution `json:"two_active_http"`
		BaselineTransactions     agentHTTPPerfDistribution `json:"baseline_transactions"`
		TwoActiveTransactions    agentHTTPPerfDistribution `json:"two_active_transactions"`
		HTTPP95Ratio             float64                   `json:"http_p95_ratio"`
		TransactionP95Ratio      float64                   `json:"transaction_p95_ratio"`
		HTTPDegradationPercent   float64                   `json:"http_degradation_percent"`
		AllowedRatio             float64                   `json:"allowed_ratio"`
		HTTPErrors               int                       `json:"http_errors"`
		LockErrors               int                       `json:"lock_errors"`
		MissingTransactionTraces int                       `json:"missing_transaction_traces"`
		Conditions               string                    `json:"conditions"`
	}{Type: "crm_agent_real_http_followup_latency", Timestamp: time.Now().UTC(), GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, CPUs: runtime.NumCPU(), GOMAXPROCS: runtime.GOMAXPROCS(0), SQLiteVersion: sqliteVersion, InitialLeads: 10001, InitialFollowups: 50001, Groups: groups, Runs: runs, ModelCalls: modelCalls.Load(), BaselineHTTP: baseHTTP, TwoActiveHTTP: activeHTTP, BaselineTransactions: baseTxn, TwoActiveTransactions: activeTxn, HTTPP95Ratio: httpRatio, TransactionP95Ratio: transactionRatio, HTTPDegradationPercent: (httpRatio - 1) * 100, AllowedRatio: 1.20, HTTPErrors: errors, LockErrors: lockErrors, MissingTransactionTraces: missingTraces, Conditions: "Standalone isolated WAL SQLite; actual httptest TCP HTTP, JWT/database account authentication, permissions and followup usecase; two real private Agent runs, physical snapshots, SQL tools, persisted results and SSE subscriptions; local controlled provider has four stream tool rounds and 350ms per call; no cloud or production write. HTTP timing covers client request through complete response body; transaction timing is separate BEGIN-through-COMMIT. Only samples overlapping both accepted, unfinished analyses enter HTTP acceptance; idle tails excluded. Business arrivals spaced20ms. External machine load must be controlled by caller."}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("HTTP_PERFORMANCE_EVIDENCE %s", encoded)
	if errors != 0 || lockErrors != 0 || missingTraces != 0 {
		t.Errorf("HTTP business errors=%d locks=%d missing transaction traces=%d", errors, lockErrors, missingTraces)
	}
	if len(baselineHTTP) < 100 || len(bothHTTP) < 100 {
		t.Errorf("insufficient actual HTTP samples: baseline=%d both-active=%d", len(baselineHTTP), len(bothHTTP))
	}
	for _, run := range runs {
		if run.Error != "" || run.Status != "completed" || run.Results == 0 {
			t.Errorf("%s real Agent run failed: %+v", run.Actor, run)
		}
	}
	if modelCalls.Load() != 8 {
		t.Errorf("expected two real four-call model loops, got %d calls", modelCalls.Load())
	}
	if httpRatio > 1.20 {
		t.Errorf("full HTTP followup P95 degraded %.2f%% (baseline %.3fms, two active analyses %.3fms), exceeds20%% target", (httpRatio-1)*100, baseHTTP.P95MS, activeHTTP.P95MS)
	}
}
