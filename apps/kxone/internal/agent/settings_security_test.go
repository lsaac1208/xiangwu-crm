package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/security"
	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This fixture AccountReader models real immutable IDs, status, roles and auth
// versions. It is test-only; production still uses the actual account repo.
type agentSecurityAccounts struct {
	mu    sync.RWMutex
	users map[int64]types.WeComUserBinding
}

func (a *agentSecurityAccounts) GetByID(ctx context.Context, id int64) (*types.WeComUserBinding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	u, ok := a.users[id]
	if !ok {
		return nil, nil
	}
	return &u, nil
}
func (a *agentSecurityAccounts) change(id int64, mutate func(*types.WeComUserBinding)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	u := a.users[id]
	mutate(&u)
	a.users[id] = u
}

type agentSecurityFixture struct {
	service  *Service
	db       *gorm.DB
	accounts *agentSecurityAccounts
	router   *gin.Engine
	model    *httptest.Server
}

func newAgentSecurityFixture(t *testing.T, modelHandler http.HandlerFunc) *agentSecurityFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "agent-security.sqlite")+"?_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = pool.Close() })
	if err := repo.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := MigrateForTest(db); err != nil {
		t.Fatal(err)
	}
	if modelHandler == nil {
		modelHandler = fixtureModelHandler
	}
	server := httptest.NewServer(modelHandler)
	t.Cleanup(server.Close)
	accounts := &agentSecurityAccounts{users: map[int64]types.WeComUserBinding{
		1: {ID: 1, AuthVersion: 1, AccountVersion: 1, Username: "admin-a", WeComUserID: "admin-a", Role: types.UserRoleAdmin, Status: types.UserStatusActive},
		2: {ID: 2, AuthVersion: 1, AccountVersion: 1, Username: "manager-b", WeComUserID: "manager-b", Role: types.UserRoleManager, Status: types.UserStatusActive},
		3: {ID: 3, AuthVersion: 1, AccountVersion: 1, Username: "admin-b", WeComUserID: "admin-b", Role: types.UserRoleAdmin, Status: types.UserStatusActive},
	}}
	protector, err := security.NewAESGCMFieldProtector(strings.Repeat("K", 32))
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.BaseURL = server.URL + "/v1/chat/completions"
	cfg.APIKey = "TEST_SECRET_INITIAL_MODEL_KEY"
	cfg.Directory = t.TempDir()
	cfg.Timeout = 8 * time.Second
	s, err := NewService(db, cfg, accounts, protector, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Close()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			s.mu.Lock()
			n := len(s.running)
			s.mu.Unlock()
			if n == 0 {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Error("fixture service failed to stop")
	})
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.GetHeader("X-Test-Account"), 10, 64)
		version, _ := strconv.ParseInt(c.GetHeader("X-Test-Version"), 10, 64)
		c.Set("account_id", id)
		c.Set("auth_version", version)
		c.Set("role", c.GetHeader("X-Test-Role"))
	})
	NewHandler(s).WithHistoryAccess(func(*gin.Context, string, string) error { return nil }, func(*gin.Context) bool { return true }).Register(router.Group("/api/agent"))
	return &agentSecurityFixture{service: s, db: db, accounts: accounts, router: router, model: server}
}

var securityAdmin = Actor{AccountID: 1, AuthVersion: 1, Role: "admin"}
var securityManager = Actor{AccountID: 2, AuthVersion: 1, Role: "manager"}

func securityRequest(t *testing.T, f *agentSecurityFixture, method, path string, body any, a Actor) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-Account", strconv.FormatInt(a.AccountID, 10))
	req.Header.Set("X-Test-Version", strconv.FormatInt(a.AuthVersion, 10))
	req.Header.Set("X-Test-Role", a.Role)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}
func securitySettings(t *testing.T, f *agentSecurityFixture) Settings {
	t.Helper()
	var s Settings
	if err := f.db.First(&s, 1).Error; err != nil {
		t.Fatal(err)
	}
	return s
}
func securitySettingsBody(s Settings) map[string]any {
	return map[string]any{"base_url": s.BaseURL, "model": s.Model, "enabled": s.Enabled, "budget_micro": s.BudgetMicro, "expected_version": s.Version}
}
func securityCreateConversation(t *testing.T, f *agentSecurityFixture, a Actor) Conversation {
	t.Helper()
	now := time.Now().UTC()
	c := Conversation{ID: uuid.NewString(), AccountID: a.AccountID, Title: "新对话", CreatedAt: now, UpdatedAt: now}
	if err := f.db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	return c
}
func securityWaitRun(t *testing.T, f *agentSecurityFixture, id string) Run {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		var r Run
		if err := f.db.First(&r, "id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		if r.Status != "running" && r.Status != "queued" {
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run did not terminate")
	return Run{}
}

func TestAgentWebSettingsEncryptNeverEchoAndRequireAdmin(t *testing.T) {
	f := newAgentSecurityFixture(t, nil)
	initial := securitySettings(t, f)
	if initial.KeyCiphertext == "TEST_SECRET_INITIAL_MODEL_KEY" || !security.IsEncryptedField(initial.KeyCiphertext) {
		t.Fatal("initial key stored in plaintext")
	}
	for _, a := range []Actor{securityAdmin, securityManager} {
		w := securityRequest(t, f, http.MethodGet, "/api/agent/settings", nil, a)
		if w.Code != 200 {
			t.Fatalf("settings GET=%d %s", w.Code, w.Body)
		}
		if strings.Contains(w.Body.String(), "TEST_SECRET_INITIAL_MODEL_KEY") || strings.Contains(w.Body.String(), initial.KeyCiphertext) {
			t.Fatal("settings read returned key/ciphertext")
		}
		if a.Role != "admin" && strings.Contains(w.Body.String(), initial.BaseURL) {
			t.Fatal("manager received private provider endpoint")
		}
	}
	body := securitySettingsBody(initial)
	body["api_key"] = "TEST_SECRET_REPLACED_KEY"
	w := securityRequest(t, f, http.MethodPut, "/api/agent/settings", body, securityManager)
	if w.Code != 403 {
		t.Fatalf("manager wrote settings: %d %s", w.Code, w.Body)
	}
	// The DB role, not an inconsistent caller role, must decide permissions.
	forged := securityManager
	forged.Role = "admin"
	w = securityRequest(t, f, http.MethodPut, "/api/agent/settings", body, forged)
	if w.Code != 403 {
		t.Fatalf("forged manager role wrote settings: %d %s", w.Code, w.Body)
	}
	w = securityRequest(t, f, http.MethodPut, "/api/agent/settings", body, securityAdmin)
	if w.Code != 200 {
		t.Fatalf("admin key update: %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "TEST_SECRET_REPLACED_KEY") {
		t.Fatal("key echoed from update")
	}
	stored := securitySettings(t, f)
	plain, err := f.service.protector.Reveal(stored.KeyCiphertext)
	if err != nil || plain != "TEST_SECRET_REPLACED_KEY" {
		t.Fatalf("key encryption roundtrip: %v", err)
	}
	if stored.KeyCiphertext == plain || !security.IsEncryptedField(stored.KeyCiphertext) {
		t.Fatal("updated key not encrypted")
	}
}

func TestAgentWebSettingsBlankKeyKeepsClearExplicitAndConflictDoesNotWrite(t *testing.T) {
	f := newAgentSecurityFixture(t, nil)
	current := securitySettings(t, f)
	for _, blank := range []string{"", " \t "} {
		body := securitySettingsBody(current)
		body["api_key"] = blank
		w := securityRequest(t, f, http.MethodPut, "/api/agent/settings", body, securityAdmin)
		if w.Code != 200 {
			t.Fatalf("blank update failed: %d %s", w.Code, w.Body)
		}
		next := securitySettings(t, f)
		if next.KeyCiphertext != current.KeyCiphertext {
			t.Fatal("blank/space key erased the configured key")
		}
		current = next
	}
	body := securitySettingsBody(current)
	body["expected_version"] = current.Version - 1
	body["api_key"] = "STALE_KEY_MUST_NOT_PERSIST"
	body["budget_micro"] = int64(100_000_000)
	w := securityRequest(t, f, http.MethodPut, "/api/agent/settings", body, securityAdmin)
	if w.Code != 409 {
		t.Fatalf("stale update=%d %s", w.Code, w.Body)
	}
	next := securitySettings(t, f)
	if next.Version != current.Version || next.KeyCiphertext != current.KeyCiphertext || next.BudgetMicro != current.BudgetMicro {
		t.Fatal("conflicting settings partially persisted")
	}
	body = securitySettingsBody(current)
	body["clear_api_key"] = true
	body["enabled"] = false
	w = securityRequest(t, f, http.MethodPut, "/api/agent/settings", body, securityAdmin)
	if w.Code != 200 {
		t.Fatalf("clear=%d %s", w.Code, w.Body)
	}
	next = securitySettings(t, f)
	if next.KeyCiphertext != "" {
		t.Fatal("explicit key clear failed")
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["key_configured"] != false || out["configured"] != false {
		t.Fatal("cleared key still shown as configured")
	}
}

func TestAgentChangedModelRequiresPriceAndSettingsRejectInvalidRates(t *testing.T) {
	f := newAgentSecurityFixture(t, nil)
	s := securitySettings(t, f)
	body := securitySettingsBody(s)
	body["model"] = "different-model"
	w := securityRequest(t, f, http.MethodPut, "/api/agent/settings", body, securityAdmin)
	if w.Code != 400 {
		t.Fatalf("changed model without rates=%d %s", w.Code, w.Body)
	}
	if securitySettings(t, f).Version != s.Version {
		t.Fatal("invalid model settings written")
	}
	body["input_price_cny"] = 12.0
	body["output_price_cny"] = 36.0
	w = securityRequest(t, f, http.MethodPut, "/api/agent/settings", body, securityAdmin)
	if w.Code != 200 {
		t.Fatalf("priced model update=%d %s", w.Code, w.Body)
	}
	next := securitySettings(t, f)
	if next.InputPriceMicro != 12_000_000 || next.OutputPriceMicro != 36_000_000 || next.PriceVersion == "" {
		t.Fatal("per-million rate/version not stored")
	}
	body = securitySettingsBody(next)
	body["input_price_cny"] = 0.0
	body["output_price_cny"] = 36.0
	w = securityRequest(t, f, http.MethodPut, "/api/agent/settings", body, securityAdmin)
	if w.Code != 400 {
		t.Fatalf("zero rate accepted: %d", w.Code)
	}
}

func TestAgentConnectionTestUsesNoBusinessDataAndPaysFromBudget(t *testing.T) {
	var calls atomic.Int64
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req ModelRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			t.Error("invalid model request")
		}
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "张小明") || strings.Contains(m.Content, "13912345678") || strings.Contains(m.Content, "leads") {
				t.Error("connection test sent business records")
			}
		}
		if req.EnableThinking == nil || *req.EnableThinking {
			t.Error("qwen connection test did not disable thinking")
		}
		fixtureModelHandler(w, rWithoutBody(req, r))
	})
	createSnapshotLead(t, f.db, 1)
	w := securityRequest(t, f, http.MethodPost, "/api/agent/settings/test", map[string]any{}, securityManager)
	if w.Code != 403 {
		t.Fatalf("manager test call=%d", w.Code)
	}
	w = securityRequest(t, f, http.MethodPost, "/api/agent/settings/test", map[string]any{}, securityAdmin)
	if w.Code != 200 {
		t.Fatalf("connection test=%d %s", w.Code, w.Body)
	}
	if calls.Load() != 1 {
		t.Fatalf("unexpected paid calls=%d", calls.Load())
	}
	summary, err := NewBudget(f.db).Summary(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if summary.SettledCalls != 1 || summary.SettledMicro <= 0 || summary.ReservedMicro != 0 {
		t.Fatalf("connection test not billed: %+v", summary)
	}
	if strings.Contains(w.Body.String(), "TEST_SECRET_INITIAL_MODEL_KEY") {
		t.Fatal("connection test leaked key")
	}
}

func TestAgentConnectionNewModelWithoutPricesMakesNoPaidRequest(t *testing.T) {
	var calls atomic.Int64
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fixtureModelHandler(w, r) })
	w := securityRequest(t, f, http.MethodPost, "/api/agent/settings/test", map[string]any{"model": "new-expensive-model"}, securityAdmin)
	if w.Code != 400 {
		t.Fatalf("unpriced changed test model=%d %s", w.Code, w.Body)
	}
	if calls.Load() != 0 {
		t.Fatal("unpriced model used old price for paid call")
	}
}

func rWithoutBody(req ModelRequest, r *http.Request) *http.Request {
	raw, _ := json.Marshal(req)
	clone := r.Clone(r.Context())
	clone.Body = http.NoBody
	if len(raw) > 0 {
		clone.Body = ioNopReadCloser{Reader: bytes.NewReader(raw)}
	}
	return clone
}

type ioNopReadCloser struct{ *bytes.Reader }

func (ioNopReadCloser) Close() error { return nil }

func TestAgentRuleRollbackCreatesNewVersionAndPreservesHistoricalRun(t *testing.T) {
	f := newAgentSecurityFixture(t, nil)
	post := func(text string) Rule {
		w := securityRequest(t, f, http.MethodPost, "/api/agent/rules", map[string]any{"content": text}, securityManager)
		if w.Code != 201 {
			t.Fatalf("propose=%d %s", w.Code, w.Body)
		}
		var rule Rule
		if json.Unmarshal(w.Body.Bytes(), &rule) != nil {
			t.Fatal("invalid rule response")
		}
		return rule
	}
	first := post("成交率默认说明分母。")
	w := securityRequest(t, f, http.MethodPost, fmt.Sprintf("/api/agent/rules/%d/publish", first.ID), nil, securityManager)
	if w.Code != 403 {
		t.Fatalf("manager published=%d", w.Code)
	}
	w = securityRequest(t, f, http.MethodPost, fmt.Sprintf("/api/agent/rules/%d/publish", first.ID), nil, securityAdmin)
	if w.Code != 200 {
		t.Fatalf("publish=%d %s", w.Code, w.Body)
	}
	var old Rule
	if err := f.db.Where("status = ?", "published").First(&old).Error; err != nil {
		t.Fatal(err)
	}
	c := securityCreateConversation(t, f, securityAdmin)
	historical := Run{ID: uuid.NewString(), ConversationID: c.ID, AccountID: 1, RequestID: uuid.NewString(), AuthVersion: 1, Status: "completed", Model: "fixture", RuleVersion: old.Version, Rules: old.Content, CreatedAt: time.Now()}
	if err := f.db.Create(&historical).Error; err != nil {
		t.Fatal(err)
	}
	second := post("所有日期用上海时区。")
	w = securityRequest(t, f, http.MethodPost, fmt.Sprintf("/api/agent/rules/%d/publish", second.ID), nil, securityAdmin)
	if w.Code != 200 {
		t.Fatalf("second publish=%d %s", w.Code, w.Body)
	}
	w = securityRequest(t, f, http.MethodPost, fmt.Sprintf("/api/agent/rules/%d/rollback", old.ID), nil, securityAdmin)
	if w.Code != 200 {
		t.Fatalf("rollback=%d %s", w.Code, w.Body)
	}
	var original, active Rule
	f.db.First(&original, old.ID)
	f.db.Where("status = ?", "published").First(&active)
	if original.Version != old.Version || original.Content != old.Content || active.ID == old.ID || active.Version <= old.Version || active.Content != old.Content {
		t.Fatalf("history overwritten: old=%+v original=%+v active=%+v", old, original, active)
	}
	var after Run
	if err := f.db.First(&after, "id = ?", historical.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.RuleVersion != historical.RuleVersion || after.Rules != historical.Rules {
		t.Fatal("old run altered by rule rollback")
	}
}

func TestAgentMissingModelUsageRemainsReserved(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"没有发生额外业务操作。\"}}]}\n\ndata: [DONE]\n\n")
	})
	c := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(context.Background(), securityAdmin, c.ID, "说明数据范围", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	if finished.Status != "completed" {
		t.Fatalf("model without usage status=%s error=%s", finished.Status, finished.Error)
	}
	summary, err := NewBudget(f.db).Summary(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if summary.ReservedMicro <= 0 || summary.SettledMicro != 0 || summary.PendingCalls != 1 {
		t.Fatalf("missing usage billed as free: %+v", summary)
	}
}

func TestAgentCancelAndRevokedAccountStopLongModelStream(t *testing.T) {
	for _, mode := range []string{"cancel", "disable", "demote"} {
		t.Run(mode, func(t *testing.T) {
			started := make(chan struct{})
			aborted := make(chan struct{})
			var startOnce, abortOnce sync.Once
			f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
				var req ModelRequest
				_ = json.NewDecoder(r.Body).Decode(&req)
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"正在处理中\"}}]}\n\n")
				w.(http.Flusher).Flush()
				startOnce.Do(func() { close(started) })
				<-r.Context().Done()
				abortOnce.Do(func() { close(aborted) })
			})
			c := securityCreateConversation(t, f, securityAdmin)
			run, err := f.service.Start(context.Background(), securityAdmin, c.ID, "需要长时间分析", uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("model stream did not start")
			}
			if mode == "cancel" {
				if err := f.service.Cancel(context.Background(), securityAdmin, run.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				f.accounts.change(1, func(u *types.WeComUserBinding) {
					u.AuthVersion++
					if mode == "disable" {
						u.Status = types.UserStatusDisabled
					} else {
						u.Role = types.UserRoleSales
					}
				})
			}
			select {
			case <-aborted:
			case <-time.After(3 * time.Second):
				t.Fatal("model HTTP request continued after cancel/revocation")
			}
			finished := securityWaitRun(t, f, run.ID)
			if finished.Status == "completed" || finished.Status == "running" {
				t.Fatalf("revoked/cancelled run status=%s", finished.Status)
			}
			summary, err := NewBudget(f.db).Summary(context.Background(), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if summary.PendingCalls != 1 || summary.ReservedMicro <= 0 {
				t.Fatalf("interrupted unknown usage released: %+v", summary)
			}
		})
	}
}

func TestAgentRequestReplayAndSSEReconnectNeverRepeatPaidCalls(t *testing.T) {
	var calls atomic.Int64
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fixtureModelHandler(w, r) })
	createSnapshotLead(t, f.db, 1)
	c := securityCreateConversation(t, f, securityAdmin)
	requestID := uuid.NewString()
	run, err := f.service.Start(context.Background(), securityAdmin, c.ID, "按来源查询", requestID)
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	if finished.Status != "completed" {
		t.Fatalf("run failed: %s", finished.Error)
	}
	before := calls.Load()
	if before != 4 {
		t.Fatalf("fixture tool loop should use 4 calls, got %d", before)
	}
	again, err := f.service.Start(context.Background(), securityAdmin, c.ID, "按来源查询", requestID)
	if err != nil || again.ID != run.ID {
		t.Fatalf("request replay=%+v %v", again, err)
	}
	if _, err := f.service.Start(context.Background(), securityAdmin, c.ID, "换一个问题", requestID); err != ErrConflict {
		t.Fatalf("idempotency payload conflict=%v", err)
	}
	first := securityRequest(t, f, http.MethodGet, "/api/agent/runs/"+run.ID+"/events", nil, securityAdmin)
	if first.Code != 200 || !strings.Contains(first.Body.String(), "event: update") {
		t.Fatalf("SSE replay failed: %d %s", first.Code, first.Body)
	}
	var last Event
	if err := f.db.Where("run_id = ?", run.ID).Order("seq DESC").First(&last).Error; err != nil {
		t.Fatal(err)
	}
	second := securityRequest(t, f, http.MethodGet, fmt.Sprintf("/api/agent/runs/%s/events?after=%d", run.ID, last.Seq), nil, securityAdmin)
	if second.Code != 200 {
		t.Fatalf("SSE reconnect=%d", second.Code)
	}
	if strings.Contains(second.Body.String(), "event: update") {
		t.Fatal("SSE cursor replayed delivered events")
	}
	if calls.Load() != before {
		t.Fatal("replay/reconnect repeated paid model request")
	}
	summary, err := NewBudget(f.db).Summary(context.Background(), time.Now())
	if err != nil || summary.SettledCalls != 4 {
		t.Fatalf("duplicate billed calls: %+v %v", summary, err)
	}
}

func TestAgentPrivateConversationResultsExportsAndStreamsRejectOtherAdmins(t *testing.T) {
	f := newAgentSecurityFixture(t, nil)
	createSnapshotLead(t, f.db, 1)
	c := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(context.Background(), securityAdmin, c.ID, "按来源查询", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if finished := securityWaitRun(t, f, run.ID); finished.Status != "completed" {
		t.Fatalf("fixture run failed: %s", finished.Error)
	}
	results, err := f.service.results(context.Background(), run.ID)
	if err != nil || len(results) == 0 {
		t.Fatalf("missing saved result %v", err)
	}
	exported := securityRequest(t, f, http.MethodPost, "/api/agent/runs/"+run.ID+"/exports", map[string]any{"format": "xlsx"}, securityAdmin)
	if exported.Code != 201 {
		t.Fatalf("export=%d %s", exported.Code, exported.Body)
	}
	var artifact struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(exported.Body.Bytes(), &artifact) != nil || artifact.ID == "" {
		t.Fatal("missing export id")
	}
	paths := []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/agent/conversations/" + c.ID, nil},
		{http.MethodPatch, "/api/agent/conversations/" + c.ID, map[string]any{"title": "不得改名"}},
		{http.MethodDelete, "/api/agent/conversations/" + c.ID, nil},
		{http.MethodPost, "/api/agent/conversations/" + c.ID + "/runs", map[string]any{"message": "越权提问", "request_id": uuid.NewString()}},
		{http.MethodGet, "/api/agent/runs/" + run.ID, nil},
		{http.MethodGet, "/api/agent/runs/" + run.ID + "/events", nil},
		{http.MethodPost, "/api/agent/runs/" + run.ID + "/cancel", nil},
		{http.MethodGet, "/api/agent/results/" + results[0].ID, nil},
		{http.MethodPost, "/api/agent/runs/" + run.ID + "/exports", map[string]any{"format": "html"}},
		{http.MethodGet, "/api/agent/exports/" + artifact.ID + "/download", nil},
	}
	for _, a := range []Actor{securityManager, {AccountID: 3, AuthVersion: 1, Role: "admin"}} {
		for _, p := range paths {
			w := securityRequest(t, f, p.method, p.path, p.body, a)
			if w.Code != 403 {
				t.Errorf("account %d accessed %s %s: %d %s", a.AccountID, p.method, p.path, w.Code, w.Body)
			}
		}
	}
	ownerDownload := securityRequest(t, f, http.MethodGet, "/api/agent/exports/"+artifact.ID+"/download", nil, securityAdmin)
	if ownerDownload.Code != 200 || !bytes.HasPrefix(ownerDownload.Body.Bytes(), []byte("PK")) {
		t.Fatal("owner cannot download actual XLSX")
	}
	f.service.accounts = nil
	denied := securityRequest(t, f, http.MethodGet, "/api/agent/conversations/"+c.ID, nil, securityAdmin)
	if denied.Code != 403 {
		t.Fatal("missing account validation dependency allowed read")
	}
}

func TestAgentReducingBudgetOrClearingKeyCancelsExistingPaidStream(t *testing.T) {
	for _, mode := range []string{"reduce-budget", "clear-key"} {
		t.Run(mode, func(t *testing.T) {
			started, aborted := make(chan struct{}), make(chan struct{})
			var startOnce, abortOnce sync.Once
			f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
				var req ModelRequest
				_ = json.NewDecoder(r.Body).Decode(&req)
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"处理中\"}}]}\n\n")
				w.(http.Flusher).Flush()
				startOnce.Do(func() { close(started) })
				<-r.Context().Done()
				abortOnce.Do(func() { close(aborted) })
			})
			c := securityCreateConversation(t, f, securityAdmin)
			run, err := f.service.Start(context.Background(), securityAdmin, c.ID, "长时间查询", uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("stream not started")
			}
			body := securitySettingsBody(securitySettings(t, f))
			if mode == "reduce-budget" {
				body["budget_micro"] = int64(100_000_000)
			} else {
				body["clear_api_key"] = true
			}
			w := securityRequest(t, f, http.MethodPut, "/api/agent/settings", body, securityAdmin)
			if w.Code != 200 {
				t.Fatalf("settings=%d %s", w.Code, w.Body)
			}
			select {
			case <-aborted:
			case <-time.After(3 * time.Second):
				t.Fatal("old paid stream continued after settings change")
			}
			finished := securityWaitRun(t, f, run.ID)
			if finished.Status == "completed" {
				t.Fatal("interrupted old-config call marked complete")
			}
		})
	}
}
