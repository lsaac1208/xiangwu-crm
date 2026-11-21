package agent

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/security"
	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The interrupted state is seeded, not a real provider or process-crash test.
// Closing/reopening the file proves recovery uses durable data rather than the
// previous Service's in-memory maps. Real child-process acceptance is separate.
func TestRestartReopensPersistedRunsWithoutRepeatingCallsOrReleasingUnknownBudget(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "business.sqlite")
	open := func() *gorm.DB {
		t.Helper()
		db, err := gorm.Open(sqlite.Open(databasePath+"?_journal_mode=WAL&_busy_timeout=5000"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	closeDB := func(db *gorm.DB) {
		t.Helper()
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		if err := pool.Close(); err != nil {
			t.Fatal(err)
		}
	}
	db := open()
	if err := MigrateForTest(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	c := Conversation{ID: "recovery-conversation", AccountID: 1, Title: "持久化恢复", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"running", "queued", "completed"} {
		r := Run{ID: "run-" + status, ConversationID: c.ID, AccountID: 1, AuthVersion: 1, RequestID: "request-" + status, Status: status, CreatedAt: now}
		if status == "completed" {
			r.FinishedAt = &now
		}
		if err := db.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
		for _, role := range []string{"user", "assistant"} {
			m := Message{ID: r.ID + "-" + role, ConversationID: c.ID, RunID: r.ID, Role: role, Status: status, BlocksJSON: "[]", CreatedAt: now}
			if role == "user" {
				m.Status, m.Content = "completed", "核对"+status
			} else if status == "completed" {
				m.Content = "已保存的历史回答"
			}
			if err := db.Create(&m).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	exploration := Result{ID: "saved-exploration", Columns: []string{"n"}, Rows: [][]any{{7}}, SnapshotAt: now, TotalRows: 1, ReturnedRows: 1}
	raw, _ := json.Marshal(exploration)
	stored := ResultRecord{ID: exploration.ID, RunID: "run-running", Data: string(raw), CreatedAt: now}
	if err := db.Create(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if err := appendRunEvent(db, "run-completed", "done", map[string]any{"status": "completed"}); err != nil {
		t.Fatal(err)
	}
	budget := NewBudget(db)
	unknown, err := budget.Reserve(t.Context(), "run-running", 1, 450000, 300000000, 3000000, "test-price", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.Settle(t.Context(), unknown, 0, Usage{}); !errors.Is(err, ErrBudgetUsageUnknown) {
		t.Fatalf("unknown usage must retain reservation: %v", err)
	}
	known, err := budget.Reserve(t.Context(), "run-completed", 1, 120000, 300000000, 3000000, "test-price", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := budget.Settle(t.Context(), known, 50000, Usage{InputTokens: 10, OutputTokens: 10}); err != nil {
		t.Fatal(err)
	}
	var before []BudgetReservation
	if err := db.Order("id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	closeDB(db)
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "recovery must never call provider", http.StatusServiceUnavailable)
	}))
	defer provider.Close()
	protector, err := security.NewAESGCMFieldProtector(strings.Repeat("R", 32))
	if err != nil {
		t.Fatal(err)
	}
	accounts := &agentSecurityAccounts{users: map[int64]types.WeComUserBinding{1: {ID: 1, AuthVersion: 1, Role: types.UserRoleAdmin, Status: types.UserStatusActive}}}
	cfg := DefaultConfig()
	cfg.Enabled, cfg.APIKey, cfg.BaseURL = true, "TEST_ONLY_RECOVERY_KEY", provider.URL+"/v1/chat/completions"
	cfg.Directory = filepath.Join(directory, "runtime")
	if err := os.MkdirAll(cfg.Directory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"snapshot-abandoned.sqlite", "snapshot-abandoned.sqlite-wal", "snapshot-abandoned.sqlite-shm", "keep-report.html"} {
		if err := os.WriteFile(filepath.Join(cfg.Directory, name), []byte("synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for restart := 1; restart <= 2; restart++ {
		db = open()
		s, err := NewService(db, cfg, accounts, protector, true)
		if err != nil {
			t.Fatal(err)
		}
		for _, previousStatus := range []string{"running", "queued", "completed"} {
			id := "run-" + previousStatus
			r, err := s.ownedRun(t.Context(), id, securityAdmin)
			if err != nil {
				t.Fatal(err)
			}
			expected := "interrupted"
			if previousStatus == "completed" {
				expected = "completed"
			}
			if r.Status != expected || r.FinishedAt == nil {
				t.Fatalf("restart %d changed recovery state: %+v", restart, r)
			}
			replayed, err := s.Start(t.Context(), securityAdmin, c.ID, "核对"+previousStatus, "request-"+previousStatus)
			if err != nil || replayed.ID != id || replayed.Status != expected {
				t.Fatalf("same request restarted analysis: %+v %v", replayed, err)
			}
			var m Message
			if err := db.First(&m, "id = ?", id+"-assistant").Error; err != nil || m.Status != expected {
				t.Fatalf("assistant recovery status: %+v %v", m, err)
			}
			if expected == "interrupted" {
				if m.Content != "" || m.BlocksJSON != "[]" || !strings.Contains(r.Error, "中断") {
					t.Fatalf("unfinished draft became an answer: %+v", m)
				}
				selection, selected, err := ResolveRunAnswerResults(r.Status, nil, []Result{exploration})
				if err != nil || selection.Status != "unfinished" || len(selected) != 0 {
					t.Fatalf("exploration promoted after restart: %+v %v", selection, err)
				}
			} else if m.Content != "已保存的历史回答" {
				t.Fatal("completed history was rewritten")
			}
			var events []Event
			if err := db.Where("run_id = ? AND type = 'done'", id).Find(&events).Error; err != nil || len(events) != 1 || !strings.Contains(events[0].Data, expected) {
				t.Fatalf("terminal event absent/duplicated: %+v %v", events, err)
			}
		}
		var after []BudgetReservation
		if err := db.Order("id").Find(&after).Error; err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("restart changed paid/unknown ledger: %+v %v", after, err)
		}
		summary, err := s.budget.Summary(t.Context(), now)
		if err != nil || summary.ReservedMicro != 450000 || summary.SettledMicro != 50000 || summary.PendingCalls != 1 || summary.SettledCalls != 1 {
			t.Fatalf("usage no longer reconciles: %+v %v", summary, err)
		}
		var result ResultRecord
		if err := db.First(&result, "id = ?", stored.ID).Error; err != nil || result.Data != stored.Data {
			t.Fatal("saved evidence lost or mutated")
		}
		entries, err := os.ReadDir(cfg.Directory)
		if err != nil || len(entries) != 1 || entries[0].Name() != "keep-report.html" {
			t.Fatalf("snapshot cleanup removed too much or too little: %v %v", entries, err)
		}
		if calls.Load() != 0 || len(s.running) != 0 || len(s.activeAccounts) != 0 {
			t.Fatal("startup or idempotent replay silently restarted model work")
		}
		s.Close()
		closeDB(db)
	}
}
