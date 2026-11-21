package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"gorm.io/gorm"
)

type performanceDistribution struct {
	Count int     `json:"count"`
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
	P99MS float64 `json:"p99_ms"`
	MaxMS float64 `json:"max_ms"`
}

type performanceGroup struct {
	Name                      string                  `json:"name"`
	StartedAt                 time.Time               `json:"started_at"`
	FinishedAt                time.Time               `json:"finished_at"`
	Operation                 performanceDistribution `json:"transaction"`
	ScheduleLag               performanceDistribution `json:"scheduled_arrival_lag"`
	Errors                    int                     `json:"errors"`
	LockErrors                int                     `json:"lock_errors"`
	BothAnalysesActiveSamples int                     `json:"both_analyses_active_samples,omitempty"`
	BothAnalysesActive        performanceDistribution `json:"both_analyses_active_transactions,omitempty"`
	ScheduledSamples          int                     `json:"scheduled_samples"`
	IdleTailSamples           int                     `json:"idle_tail_samples,omitempty"`
	latencies                 []time.Duration
	bothLatencies             []time.Duration
	lag                       []time.Duration
}

type performanceAnalysis struct {
	Worker           int                     `json:"worker"`
	SnapshotMS       float64                 `json:"snapshot_ms"`
	SourceReadMS     float64                 `json:"source_read_ms"`
	SnapshotAt       time.Time               `json:"snapshot_at"`
	Query            performanceDistribution `json:"queries"`
	CompletedQueries int                     `json:"completed_queries"`
	Error            string                  `json:"error,omitempty"`
}

type performanceRound struct {
	Round              int                   `json:"round"`
	BaselineP95MS      float64               `json:"baseline_p95_ms"`
	ActiveP95MS        float64               `json:"active_p95_ms"`
	Ratio              float64               `json:"active_to_baseline_p95_ratio"`
	DegradationPercent float64               `json:"degradation_percent"`
	Workers            []performanceAnalysis `json:"analyses"`
}

// TestAgentBusinessLatencyWithTwoAnalyses is deliberately opt-in. Running a
// latency ratio alongside a full test suite, a frontend build, or another
// process benchmark does not establish the feature's performance impact.
//
// Run in an otherwise idle environment:
// CRM_AGENT_PERF=1 go test ./internal/agent -run '^TestAgentBusinessLatencyWithTwoAnalyses$' -count=1 -v
func TestAgentBusinessLatencyWithTwoAnalyses(t *testing.T) {
	if os.Getenv("CRM_AGENT_PERF") != "1" {
		t.Skip("explicit performance acceptance: set CRM_AGENT_PERF=1 and run this test alone")
	}
	db := snapshotFixtureDB(t)
	if err := db.Exec(`WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v+1 FROM n WHERE v<10001)
INSERT INTO leads(id,lead_no,customer_name,source_channel,status,business_stage,ownership_state,result_status,current_owner_user_id,current_owner_role,deal_amount,refund_amount,payment_status,created_at,updated_at,current_owner_started_at,next_followup_due_at)
SELECT v,'perf-lead-'||v,'测试客户'||v,'source-'||(v%17),'following','invitation','private','active','employee-'||(v%4),'inviter',10.01,0.01,'unpaid','2026-10-08 00:00:00+00:00','2026-10-08 00:00:00+00:00','2026-10-08 00:00:00+00:00','2026-10-09 00:00:00+00:00' FROM n`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v+1 FROM n WHERE v<50001)
INSERT INTO lead_followups(id,lead_id,sales_user_id,followup_result,remark,is_valid,created_at)
SELECT v,((v-1)%10001)+1,'employee-'||(v%4),'connected','希望参加校园参观',1,'2026-10-08 00:30:00+00:00' FROM n`).Error; err != nil {
		t.Fatal(err)
	}
	var sqliteVersion string
	if err := db.Raw("SELECT sqlite_version()").Scan(&sqliteVersion).Error; err != nil {
		t.Fatal(err)
	}
	sequence := 0
	write := func() error {
		sequence++
		now := time.Now().UTC()
		leadID := int64((sequence-1)%10001 + 1)
		return db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("UPDATE leads SET last_followup_at=?, next_followup_due_at=?, current_owner_first_followup_at=COALESCE(current_owner_first_followup_at,?), version=version+1 WHERE id=?", now, now.Add(24*time.Hour), now, leadID).Error; err != nil {
				return err
			}
			return tx.Create(&repo.LeadFollowupModel{LeadID: leadID, SalesUserID: "employee-1", FollowupResult: "connected", Remark: "业务请求新增跟进记录", IsValid: true, CreatedAt: now}).Error
		})
	}
	// Warm up the source connection, write path, SQLite statement cache, and WAL.
	for i := 0; i < 30; i++ {
		if err := write(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	const interval = 20 * time.Millisecond
	const baselineSamples = 150
	measure := func(name string, count int, active *atomic.Int32, deadline time.Time) (performanceGroup, error) {
		group := performanceGroup{Name: name, StartedAt: time.Now().UTC()}
		next := time.Now()
		for i := 0; i < count; i++ {
			if !deadline.IsZero() && time.Now().After(deadline) {
				return group, fmt.Errorf("performance sampling exceeded deadline")
			}
			if delay := time.Until(next); delay > 0 {
				timer := time.NewTimer(delay)
				<-timer.C
			}
			before := int32(0)
			if active != nil {
				before = active.Load()
			}
			started := time.Now()
			lag := started.Sub(next)
			if lag < 0 {
				lag = 0
			}
			err := write()
			elapsed := time.Since(started)
			after := int32(0)
			if active != nil {
				after = active.Load()
			}
			if err != nil {
				group.Errors++
				if strings.Contains(strings.ToLower(err.Error()), "locked") || strings.Contains(strings.ToLower(err.Error()), "busy") {
					group.LockErrors++
				}
			}
			group.ScheduledSamples++
			if active == nil || before > 0 || after > 0 {
				group.latencies = append(group.latencies, elapsed)
				group.lag = append(group.lag, lag)
			} else {
				group.IdleTailSamples++
			}
			if before == 2 && after == 2 {
				group.BothAnalysesActiveSamples++
				group.bothLatencies = append(group.bothLatencies, elapsed)
			}
			// No accumulating synthetic queue: each arrival follows the fixed
			// cadence unless the preceding real operation took longer than it.
			next = next.Add(interval)
			if next.Before(time.Now()) {
				next = time.Now().Add(interval)
			}
		}
		group.FinishedAt = time.Now().UTC()
		group.Operation = latencyDistribution(group.latencies)
		group.ScheduleLag = latencyDistribution(group.lag)
		group.BothAnalysesActive = latencyDistribution(group.bothLatencies)
		return group, nil
	}
	preparer := &Service{db: db, snapshotSlot: make(chan struct{}, 1)}
	var groups []performanceGroup
	var rounds []performanceRound
	baseline, err := measure("baseline_before_round_1", baselineSamples, nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	groups = append(groups, baseline)
	for round := 1; round <= 2; round++ {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		var active atomic.Int32
		active.Store(2)
		var wg sync.WaitGroup
		reports := make(chan performanceAnalysis, 2)
		gate := make(chan struct{})
		dir := t.TempDir()
		for worker := 0; worker < 2; worker++ {
			wg.Add(1)
			go func(worker int) {
				defer wg.Done()
				defer active.Add(-1)
				report := performanceAnalysis{Worker: worker}
				defer func() { reports <- report }()
				<-gate
				started := time.Now()
				snapshot, err := preparer.buildSnapshot(ctx, dir)
				report.SnapshotMS = float64(time.Since(started)) / float64(time.Millisecond)
				if err != nil {
					report.Error = err.Error()
					return
				}
				defer snapshot.Close()
				report.SourceReadMS = float64(snapshot.sourceReadDuration) / float64(time.Millisecond)
				report.SnapshotAt = snapshot.At
				queries := []string{
					"SELECT source_channel,count(*) AS leads,sum(net_deal_amount_cents) AS net_cents FROM leads GROUP BY source_channel",
					"SELECT sales_user_id,count(*) AS followups FROM lead_followups GROUP BY sales_user_id",
					"SELECT l.current_owner_user_id,count(*) AS leads FROM leads l WHERE EXISTS (SELECT 1 FROM lead_followups f WHERE f.lead_id=l.id AND f.is_valid=1) GROUP BY l.current_owner_user_id",
				}
				var elapsed []time.Duration
				for i := 0; i < 12; i++ {
					// Bound tools like a real run, and provide a non-zero pause
					// between calls rather than hammering the DB in a tight loop.
					if i > 0 {
						timer := time.NewTimer(350 * time.Millisecond)
						select {
						case <-ctx.Done():
							timer.Stop()
							report.Error = ctx.Err().Error()
							return
						case <-timer.C:
						}
					}
					started = time.Now()
					if _, err = snapshot.Query(ctx, queries[i%len(queries)], nil, 200); err != nil {
						report.Error = err.Error()
						return
					}
					elapsed = append(elapsed, time.Since(started))
					report.CompletedQueries++
				}
				report.Query = latencyDistribution(elapsed)
			}(worker)
		}
		close(gate)
		// 150 samples cover at least three seconds of scheduled business
		// arrivals; another group continues while both full analyses finish.
		activeGroup, err := measure(fmt.Sprintf("active_round_%d_first", round), 150, &active, time.Now().Add(40*time.Second))
		if err != nil {
			cancel()
			wg.Wait()
			t.Fatal(err)
		}
		groups = append(groups, activeGroup)
		activeLatencies := append([]time.Duration{}, activeGroup.bothLatencies...)
		for active.Load() > 0 {
			continuation, measureErr := measure(fmt.Sprintf("active_round_%d_continued_%d", round, len(groups)), 100, &active, time.Now().Add(40*time.Second))
			groups = append(groups, continuation)
			activeLatencies = append(activeLatencies, continuation.bothLatencies...)
			if measureErr != nil {
				cancel()
				wg.Wait()
				t.Fatal(measureErr)
			}
		}
		wg.Wait()
		cancel()
		close(reports)
		var workerReports []performanceAnalysis
		for report := range reports {
			workerReports = append(workerReports, report)
		}
		sort.Slice(workerReports, func(i, j int) bool { return workerReports[i].Worker < workerReports[j].Worker })
		post, err := measure(fmt.Sprintf("baseline_after_round_%d", round), baselineSamples, nil, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		groups = append(groups, post)
		baseSamples := append(append([]time.Duration{}, baseline.latencies...), post.latencies...)
		baseP95 := latencyDistribution(baseSamples).P95MS
		activeP95 := latencyDistribution(activeLatencies).P95MS
		ratio := activeP95 / baseP95
		rounds = append(rounds, performanceRound{Round: round, BaselineP95MS: baseP95, ActiveP95MS: activeP95, Ratio: ratio, DegradationPercent: (ratio - 1) * 100, Workers: workerReports})
		baseline = post
	}
	allBaseline := []time.Duration{}
	allActive := []time.Duration{}
	errors, locks, bothSamples := 0, 0, 0
	for _, group := range groups {
		errors += group.Errors
		locks += group.LockErrors
		bothSamples += group.BothAnalysesActiveSamples
		if strings.HasPrefix(group.Name, "baseline_") {
			allBaseline = append(allBaseline, group.latencies...)
		} else {
			allActive = append(allActive, group.bothLatencies...)
		}
	}
	baseDist, activeDist := latencyDistribution(allBaseline), latencyDistribution(allActive)
	ratio := activeDist.P95MS / baseDist.P95MS
	evidence := struct {
		Type                      string                  `json:"type"`
		Timestamp                 time.Time               `json:"timestamp"`
		GOOS                      string                  `json:"goos"`
		GOARCH                    string                  `json:"goarch"`
		CPUs                      int                     `json:"cpus"`
		GOMAXPROCS                int                     `json:"gomaxprocs"`
		GoVersion                 string                  `json:"go_version"`
		SQLiteVersion             string                  `json:"sqlite_version"`
		InitialLeads              int                     `json:"initial_leads"`
		InitialFollowups          int                     `json:"initial_followups"`
		BusinessArrivalIntervalMS int                     `json:"business_arrival_interval_ms"`
		ConcurrentAnalyses        int                     `json:"concurrent_analyses"`
		Groups                    []performanceGroup      `json:"groups"`
		Rounds                    []performanceRound      `json:"rounds"`
		Baseline                  performanceDistribution `json:"baseline"`
		Active                    performanceDistribution `json:"active"`
		P95Ratio                  float64                 `json:"active_to_baseline_p95_ratio"`
		P95DegradationPercent     float64                 `json:"p95_degradation_percent"`
		AllowedRatio              float64                 `json:"allowed_ratio"`
		Errors                    int                     `json:"business_errors"`
		LockErrors                int                     `json:"business_lock_errors"`
		BothAnalysesActiveSamples int                     `json:"both_analyses_active_samples"`
		Conditions                string                  `json:"conditions"`
	}{Type: "crm_agent_sqlite_business_latency", Timestamp: time.Now().UTC(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, CPUs: runtime.NumCPU(), GOMAXPROCS: runtime.GOMAXPROCS(0), GoVersion: runtime.Version(), SQLiteVersion: sqliteVersion, InitialLeads: 10001, InitialFollowups: 50001, BusinessArrivalIntervalMS: 20, ConcurrentAnalyses: 2, Groups: groups, Rounds: rounds, Baseline: baseDist, Active: activeDist, P95Ratio: ratio, P95DegradationPercent: (ratio - 1) * 100, AllowedRatio: 1.20, Errors: errors, LockErrors: locks, BothAnalysesActiveSamples: bothSamples, Conditions: "Opt-in isolated real SQLite WAL fixture; short update+followup insert transactions; no network or production writes; each analysis uses the shared production preparation gate, builds a full physical redacted snapshot and runs 12 bounded tools, spaced 350ms apart; source transaction released before query phase. External machine load must be controlled by the caller."}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("PERFORMANCE_EVIDENCE %s", encoded)
	if errors != 0 || locks != 0 {
		t.Errorf("business writes encountered %d errors (%d lock/busy errors)", errors, locks)
	}
	if len(allBaseline) < 100 || len(allActive) < 100 || bothSamples < 100 {
		t.Errorf("insufficient actual overlap: baseline=%d active=%d both=%d", len(allBaseline), len(allActive), bothSamples)
	}
	for _, round := range rounds {
		if round.Ratio > 1.20 {
			t.Errorf("round %d business transaction P95 degraded %.2f%% (baseline %.3fms, two active analyses %.3fms), exceeds 20%% target", round.Round, round.DegradationPercent, round.BaselineP95MS, round.ActiveP95MS)
		}
		for _, worker := range round.Workers {
			if worker.Error != "" || worker.CompletedQueries != 12 {
				t.Errorf("analysis round %d worker %d incomplete: %s queries=%d", round.Round, worker.Worker, worker.Error, worker.CompletedQueries)
			}
		}
	}
	// The acceptance threshold is intentionally not relaxed for a faster or
	// slower machine, outliers, or a failing implementation.
	if ratio > 1.20 {
		t.Errorf("business transaction P95 degraded %.2f%% (baseline %.3fms, active %.3fms), exceeds 20%% target", (ratio-1)*100, baseDist.P95MS, activeDist.P95MS)
	}
}

func latencyDistribution(values []time.Duration) performanceDistribution {
	if len(values) == 0 {
		return performanceDistribution{}
	}
	ordered := append([]time.Duration{}, values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	percentile := func(p float64) float64 {
		index := int(math.Ceil(float64(len(ordered))*p)) - 1
		if index < 0 {
			index = 0
		}
		return float64(ordered[index]) / float64(time.Millisecond)
	}
	return performanceDistribution{Count: len(values), P50MS: percentile(.50), P95MS: percentile(.95), P99MS: percentile(.99), MaxMS: float64(ordered[len(ordered)-1]) / float64(time.Millisecond)}
}
