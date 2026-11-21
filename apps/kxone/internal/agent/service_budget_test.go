package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func sharedBudgetFixture(t *testing.T, ledger *Budget, handler http.HandlerFunc) *agentSecurityFixture {
	t.Helper()
	f := newAgentSecurityFixture(t, handler)
	original := f.service
	original.Close()
	s, err := NewService(f.db, original.cfg, f.accounts, original.protector, true, WithBudget(ledger))
	if err != nil {
		t.Fatal(err)
	}
	f.service = s
	t.Cleanup(func() {
		s.Close()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			s.mu.Lock()
			active := len(s.running)
			s.mu.Unlock()
			if active == 0 {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Error("shared budget fixture did not stop")
	})
	f.router = gin.New()
	f.router.Use(func(c *gin.Context) {
		id, _ := strconv.ParseInt(c.GetHeader("X-Test-Account"), 10, 64)
		version, _ := strconv.ParseInt(c.GetHeader("X-Test-Version"), 10, 64)
		c.Set("account_id", id)
		c.Set("auth_version", version)
		c.Set("role", c.GetHeader("X-Test-Role"))
	})
	NewHandler(s).WithHistoryAccess(func(*gin.Context, string, string) error { return nil }, func(*gin.Context) bool { return true }).Register(f.router.Group("/api/agent"))
	return f
}

func TestServiceBudgetRejectsInvalidDependencies(t *testing.T) {
	f := newAgentSecurityFixture(t, nil)
	closed := testBudget(t)
	pool, _ := closed.db.DB()
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	missingTable := testBudget(t)
	if err := missingTable.db.Migrator().DropTable(&BudgetReservation{}); err != nil {
		t.Fatal(err)
	}
	for name, option := range map[string]ServiceOption{
		"nil option":           nil,
		"nil budget":           WithBudget(nil),
		"missing database":     WithBudget(&Budget{}),
		"invalid database":     WithBudget(&Budget{db: &gorm.DB{}}),
		"closed database":      WithBudget(closed),
		"missing ledger table": WithBudget(missingTable),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewService(f.db, f.service.cfg, f.accounts, f.service.protector, true, option); err == nil {
				t.Fatal("invalid budget dependency accepted")
			}
		})
	}
	if _, err := NewService(nil, f.service.cfg, f.accounts, f.service.protector, true); err == nil {
		t.Fatal("nil business database accepted")
	}
	if _, err := f.service.budget.Reserve(context.Background(), "default-service-ledger", 1, 10, 100, 100, "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := f.db.Model(&BudgetReservation{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("default service did not retain its local ledger: count=%d err=%v", count, err)
	}
}

func TestServiceSharedBudgetConcurrentConnectionTestsRetainUnknownUsage(t *testing.T) {
	ledger := testBudget(t)
	// Separate pools against the shared file ensure no service/process mutex can
	// accidentally satisfy the monthly-budget concurrency invariant.
	otherDB, err := gorm.Open(sqlite.Open(ledger.db.Dialector.(*sqlite.Dialector).DSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	otherPool, err := otherDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = otherPool.Close() })
	var calls atomic.Int64
	handler := func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`)
	}
	fixtures := []*agentSecurityFixture{sharedBudgetFixture(t, ledger, handler), sharedBudgetFixture(t, NewBudget(otherDB), handler)}
	cfg := fixtures[0].service.cfg
	no := false
	request := ModelRequest{Model: cfg.Model, Messages: []ModelMessage{{Role: "user", Content: "仅回答 OK，用于管理员主动发起的模型连通测试。"}}, Stream: false, MaxTokens: 32, Temperature: 0, EnableThinking: &no}
	raw, _ := json.Marshal(request)
	reserve := modelCost(int64(len(raw)+4096), 64, cfg)
	limit := reserve + reserve/2
	for _, f := range fixtures {
		if err := f.db.Model(&Settings{}).Where("id = 1").Update("budget_micro", limit).Error; err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for _, f := range fixtures {
		wg.Add(1)
		go func(f *agentSecurityFixture) {
			defer wg.Done()
			<-start
			response := securityRequest(t, f, http.MethodPost, "/api/agent/settings/test", map[string]any{}, securityAdmin)
			statuses <- response.Code
		}(f)
	}
	close(start)
	wg.Wait()
	close(statuses)
	successes, rejected := 0, 0
	for status := range statuses {
		switch status {
		case 200:
			successes++
		case 429:
			rejected++
		default:
			t.Fatalf("unexpected connection-test response %d", status)
		}
	}
	if successes != 1 || rejected != 1 || calls.Load() != 1 {
		t.Fatalf("separate services overspent shared budget: success=%d rejected=%d calls=%d", successes, rejected, calls.Load())
	}
	summary, err := ledger.Summary(context.Background(), time.Now())
	if err != nil || summary.PendingCalls != 1 || summary.ReservedMicro != reserve || summary.SettledCalls != 0 {
		t.Fatalf("unknown provider usage released shared reservation: %+v err=%v", summary, err)
	}
	for _, f := range fixtures {
		response := securityRequest(t, f, http.MethodGet, "/api/agent/settings", nil, securityAdmin)
		var settings map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &settings); err != nil || settings["reserved_micro"] != float64(reserve) {
			t.Fatalf("settings did not read shared usage: %s err=%v", response.Body.String(), err)
		}
		var count int64
		if err := f.db.Model(&BudgetReservation{}).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("connection test used isolated instead of shared budget: %d %v", count, err)
		}
	}
}

func TestServiceSharedBudgetConnectionTestSettlesReturnedUsage(t *testing.T) {
	ledger := testBudget(t)
	f := sharedBudgetFixture(t, ledger, nil)
	response := securityRequest(t, f, http.MethodPost, "/api/agent/settings/test", map[string]any{}, securityAdmin)
	if response.Code != http.StatusOK {
		t.Fatalf("connection test failed: %d %s", response.Code, response.Body.String())
	}
	summary, err := ledger.Summary(context.Background(), time.Now())
	if err != nil || summary.SettledCalls != 1 || summary.PendingCalls != 0 || summary.InputTokens != 30 || summary.OutputTokens != 2 {
		t.Fatalf("connection test lost shared settlement: %+v err=%v", summary, err)
	}
	var localCount int64
	if err := f.db.Model(&BudgetReservation{}).Count(&localCount).Error; err != nil || localCount != 0 {
		t.Fatalf("connection test wrote the local ledger: count=%d err=%v", localCount, err)
	}
}

func TestServiceSharedBudgetKeepsBusinessAndConversationsIsolated(t *testing.T) {
	ledger := testBudget(t)
	f := sharedBudgetFixture(t, ledger, nil)
	now := time.Now().UTC()
	lead := &types.Lead{ID: 901, LeadNo: "SHARED-BUDGET-901", CustomerName: "隔离客户", SourceChannel: "好学校", Status: types.LeadStatusPendingDispatch, CreatedAt: now, UpdatedAt: now}
	if err := repo.NewGormLeadRepository(f.db).Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	conversation := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(context.Background(), securityAdmin, conversation.ID, "按来源统计并展示表格", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	if finished.Status != "completed" {
		t.Fatalf("isolated analysis failed: %+v", finished)
	}
	summary, err := ledger.Summary(context.Background(), time.Now())
	if err != nil || summary.SettledCalls != 4 || summary.PendingCalls != 0 || summary.SettledMicro <= 0 {
		t.Fatalf("model loop did not settle shared ledger: %+v err=%v", summary, err)
	}
	for table, expected := range map[string]int64{"leads": 1, "ai_conversations": 1, "ai_runs": 1, "ai_messages": 2, "ai_results": 1, "ai_budget_reservations": 0} {
		var count int64
		if err := f.db.Table(table).Count(&count).Error; err != nil || count != expected {
			t.Fatalf("isolated %s rows=%d want=%d err=%v", table, count, expected, err)
		}
		if table != "ai_budget_reservations" && ledger.db.Migrator().HasTable(table) {
			t.Fatalf("shared ledger was used to store %s", table)
		}
	}
	var usageEvents []Event
	if err := f.db.Where("run_id = ? AND type = ?", run.ID, "model_usage").Find(&usageEvents).Error; err != nil || len(usageEvents) != 4 {
		t.Fatalf("missing call audit: count=%d err=%v", len(usageEvents), err)
	}
	for _, event := range usageEvents {
		var data map[string]any
		if err := json.Unmarshal([]byte(event.Data), &data); err != nil || data["settle_result"] != "settled" || data["has_usage"] != true || data["reservation_id"] == "" {
			t.Fatalf("invalid usage audit: %s err=%v", event.Data, err)
		}
		var charge BudgetReservation
		if err := ledger.db.First(&charge, "id = ?", data["reservation_id"]).Error; err != nil || charge.RunID != run.ID || charge.Status != "settled" {
			t.Fatalf("audit does not bind shared charge: %+v err=%v", charge, err)
		}
		if data["reservation_micro"] != float64(charge.AmountMicro) || charge.AmountMicro <= 0 {
			t.Fatalf("audit omitted original reservation ceiling: %s charge=%+v", event.Data, charge)
		}
	}
	// Full shared usage must stop a new run before writing local run/messages.
	if err := f.db.Model(&Settings{}).Where("id = 1").Update("budget_micro", summary.TotalMicro).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Start(context.Background(), securityAdmin, conversation.ID, "额度不足不能继续", uuid.NewString()); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("Start ignored shared total: %v", err)
	}
}

func TestServiceSharedBudgetUsageAuditSurvivesProviderFailure(t *testing.T) {
	for _, withUsage := range []bool{false, true} {
		t.Run(strconv.FormatBool(withUsage), func(t *testing.T) {
			ledger := testBudget(t)
			f := sharedBudgetFixture(t, ledger, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"id\":\"response-test\",\"model\":\"provider-model-snapshot\",\"choices\":[{\"delta\":{\"content\":\"部分回答\"}}]}\n\n")
				if withUsage {
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}],\"usage\":{\"prompt_tokens\":30,\"completion_tokens\":2}}\n\n")
				}
				fmt.Fprint(w, "data: [DONE]\n\n")
			})
			conversation := securityCreateConversation(t, f, securityAdmin)
			run, err := f.service.Start(context.Background(), securityAdmin, conversation.ID, "验证调用记录", uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			finished := securityWaitRun(t, f, run.ID)
			if withUsage && finished.Status != "failed" {
				t.Fatalf("truncated response must fail: %+v", finished)
			}
			var event Event
			if err := f.db.Where("run_id = ? AND type = ?", run.ID, "model_usage").First(&event).Error; err != nil {
				t.Fatal(err)
			}
			var data map[string]any
			if err := json.Unmarshal([]byte(event.Data), &data); err != nil {
				t.Fatal(err)
			}
			if data["reported_model"] != "provider-model-snapshot" || data["response_id"] != "response-test" || data["has_usage"] != withUsage {
				t.Fatalf("missing provider audit: %s", event.Data)
			}
			summary, err := ledger.Summary(context.Background(), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			var charge BudgetReservation
			if err := ledger.db.First(&charge, "id = ?", data["reservation_id"]).Error; err != nil || data["reservation_micro"] != float64(charge.AmountMicro) || charge.AmountMicro <= 0 {
				t.Fatalf("audit reservation missing for provider result: %s charge=%+v err=%v", event.Data, charge, err)
			}
			if withUsage {
				if summary.SettledCalls != 1 || data["settle_result"] != "settled" || data["estimated_actual_micro"] == nil {
					t.Fatalf("failure lost returned usage: %s %+v", event.Data, summary)
				}
			} else if summary.PendingCalls != 1 || summary.ReservedMicro <= 0 || data["settle_result"] != "unknown" || data["estimated_actual_micro"] != nil {
				t.Fatalf("unknown usage was treated as free: %s %+v", event.Data, summary)
			}
		})
	}
	if value := safeProviderMetadata("model-TEST_SECRET-end", "TEST_SECRET"); strings.Contains(value, "TEST_SECRET") || value != "model-[redacted]-end" {
		t.Fatalf("echoed credential in provider metadata: %q", value)
	}
}

func TestServiceBudgetRejectionsAndLedgerFailuresHaveDistinctTerminalStates(t *testing.T) {
	for _, mode := range []string{"monthly", "run", "database"} {
		t.Run(mode, func(t *testing.T) {
			ledger := testBudget(t)
			var providerCalls atomic.Int64
			f := sharedBudgetFixture(t, ledger, func(w http.ResponseWriter, r *http.Request) {
				providerCalls.Add(1)
				fixtureModelHandler(w, r)
			})
			conversation := securityCreateConversation(t, f, securityAdmin)
			now := time.Now().UTC()
			baseline := Run{ID: uuid.NewString(), ConversationID: conversation.ID, AccountID: securityAdmin.AccountID, AuthVersion: 1, RequestID: uuid.NewString(), Status: "completed", Model: "test", CreatedAt: now.Add(-time.Second), FinishedAt: &now}
			if err := f.db.Create(&baseline).Error; err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "monthly":
				if err := f.db.Model(&Settings{}).Where("id=1").Update("budget_micro", 1).Error; err != nil {
					t.Fatal(err)
				}
			case "run":
				f.service.cfg.RunBudgetMicro = 1
			case "database":
				// Start's summary succeeds, then its execution is held before
				// snapshot construction while the separate ledger goes down.
				f.service.snapshotSlot <- struct{}{}
			}
			run, err := f.service.Start(context.Background(), securityAdmin, conversation.ID, "核对全部来源", uuid.NewString())
			if mode == "database" {
				pool, _ := ledger.db.DB()
				_ = pool.Close()
				<-f.service.snapshotSlot
			}
			if err != nil {
				t.Fatal(err)
			}
			finished := securityWaitRun(t, f, run.ID)
			expectedStatus, expectedMessage := "budget_exceeded", "分析费用额度不足"
			if mode == "database" {
				expectedStatus, expectedMessage = "failed", "分析费用核验暂不可用"
			}
			if finished.Status != expectedStatus || !strings.Contains(finished.Error, expectedMessage) || providerCalls.Load() != 0 {
				t.Fatalf("invalid terminal status or unreserved provider call: %+v calls=%d", finished, providerCalls.Load())
			}
			var message Message
			if err := f.db.Where("run_id=? AND role=?", run.ID, "assistant").First(&message).Error; err != nil || message.Status != expectedStatus || !strings.Contains(message.Content, expectedMessage) {
				t.Fatalf("assistant state does not match run: %+v err=%v", message, err)
			}
			var done Event
			deadline := time.Now().Add(time.Second)
			for f.db.Where("run_id=? AND type=?", run.ID, "done").First(&done).Error != nil && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			var event map[string]any
			if err := json.Unmarshal([]byte(done.Data), &event); err != nil || event["status"] != expectedStatus {
				t.Fatalf("terminal SSE state differs: %s err=%v", done.Data, err)
			}
			health, err := runHealthSummary(context.Background(), f.db, time.Now())
			if err != nil || health.Total != 2 || health.Completed != 1 || health.FailureRate == nil {
				t.Fatalf("run health unavailable: %+v err=%v", health, err)
			}
			if mode == "database" {
				if health.BudgetExceeded != 0 || health.Failed != 1 || *health.FailureRate != 0.5 {
					t.Fatalf("ledger failure was hidden as budget exhaustion: %+v", health)
				}
			} else if health.BudgetExceeded != 1 || health.Failed != 0 || *health.FailureRate != 0 {
				t.Fatalf("budget exhaustion inflated failure rate: %+v", health)
			}
		})
	}
}
