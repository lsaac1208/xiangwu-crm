package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// A local event write can cross the primary window after Reserve succeeds.
// No provider received that request, so it is not an unknown paid invocation.
func TestPrimaryWindowExpiryAfterReservationDoesNotInventUsage(t *testing.T) {
	var primaryCalls, backupCalls atomic.Int64
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backupCalls.Add(1)
		fixtureModelHandler(w, r)
	}))
	defer backup.Close()
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		primaryCalls.Add(1)
		fixtureModelHandler(w, r)
	})
	f.service.cfg.Timeout = 3 * time.Second
	configureTestBackup(t, f, backup.URL)
	var delayed atomic.Bool
	if err := f.db.Callback().Create().Before("gorm:create").Register("test:delay_pre_dispatch_status", func(tx *gorm.DB) {
		event, ok := tx.Statement.Dest.(*Event)
		if !ok || event.Type != "status" {
			return
		}
		var data map[string]any
		if json.Unmarshal([]byte(event.Data), &data) == nil && data["label"] == "整理回答中" && delayed.CompareAndSwap(false, true) {
			time.Sleep(1700 * time.Millisecond)
		}
	}); err != nil {
		t.Fatal(err)
	}
	conversation := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, conversation.ID, "统计来源数量", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	if finished.Status != "completed" || primaryCalls.Load() != 0 || backupCalls.Load() != 4 {
		t.Fatalf("dispatch boundary: status=%s primary=%d backup=%d error=%s", finished.Status, primaryCalls.Load(), backupCalls.Load(), finished.Error)
	}
	summary, err := f.service.budget.Summary(t.Context(), finished.CreatedAt)
	if err != nil || summary.PendingCalls != 0 || summary.ReservedMicro != 0 || summary.SettledCalls != 4 {
		t.Fatalf("unsent request must not become unknown usage: %+v %v", summary, err)
	}
	var reservations []BudgetReservation
	if err := f.db.Where("run_id=?", run.ID).Find(&reservations).Error; err != nil {
		t.Fatal(err)
	}
	notSent := 0
	for _, reservation := range reservations {
		if reservation.Status == "not_sent" {
			notSent++
			if reservation.AmountMicro <= 0 || reservation.ActualMicro != 0 || reservation.InputTokens != 0 || reservation.OutputTokens != 0 {
				t.Fatal("undispatched reservation lost original ceiling or invented usage")
			}
		}
	}
	if notSent != 1 || len(reservations) != 5 {
		t.Fatalf("unspent reservation audit missing: not_sent=%d total=%d", notSent, len(reservations))
	}
	var usageEvents []Event
	if err := f.db.Where("run_id=? AND type='model_usage'", run.ID).Find(&usageEvents).Error; err != nil || len(usageEvents) != 4 {
		t.Fatalf("unattempted call entered model usage: events=%d err=%v", len(usageEvents), err)
	}
	var switches []Event
	if err := f.db.Where("run_id=? AND type='provider_switch'", run.ID).Find(&switches).Error; err != nil || len(switches) != 1 {
		t.Fatalf("expected one scheduling switch: count=%d err=%v", len(switches), err)
	}
	var data map[string]any
	if json.Unmarshal([]byte(switches[0].Data), &data) != nil || data["reason"] != "primary_time_window" {
		t.Fatalf("unsent request falsely became a provider failure: %v", data)
	}
}
