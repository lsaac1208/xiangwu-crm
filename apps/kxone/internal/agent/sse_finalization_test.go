package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func seedFinalizationRun(t *testing.T, f *agentSecurityFixture) Run {
	t.Helper()
	conversation := securityCreateConversation(t, f, securityAdmin)
	run := Run{ID: uuid.NewString(), ConversationID: conversation.ID, AccountID: securityAdmin.AccountID, AuthVersion: securityAdmin.AuthVersion, RequestID: uuid.NewString(), Status: "running", Model: "fixture", CreatedAt: time.Now().UTC()}
	if err := f.db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&Message{ID: uuid.NewString(), ConversationID: run.ConversationID, RunID: run.ID, Role: "assistant", Content: "original", Status: "running", CreatedAt: run.CreatedAt}).Error; err != nil {
		t.Fatal(err)
	}
	return run
}

func TestSSEDrainsFinalEventsCommittedAfterItsOldEventBatch(t *testing.T) {
	var paidCalls atomic.Int64
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		paidCalls.Add(1)
		fixtureModelHandler(w, r)
	})
	run := seedFinalizationRun(t, f)
	if err := f.service.event(run.ID, "status", map[string]any{"status": "running"}); err != nil {
		t.Fatal(err)
	}
	// The exact race: the old event batch has already been scanned, then the
	// writer commits terminal state before the reader can decide to stop.
	var injected atomic.Bool
	callback := "test:finish_after_old_event_batch"
	if err := f.db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "ai_events" || !injected.CompareAndSwap(false, true) {
			return
		}
		if err := f.service.finishRun(context.Background(), run.ID, "completed", "", "finished", []Block{}); err != nil {
			t.Error(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.db.Callback().Query().Remove(callback) })
	response := securityRequest(t, f, http.MethodGet, "/api/agent/runs/"+run.ID+"/events", nil, securityAdmin)
	if !injected.Load() || response.Code != 200 || strings.Count(response.Body.String(), `"type":"message"`) != 1 || strings.Count(response.Body.String(), `"type":"done"`) != 1 {
		t.Fatalf("SSE missed the final committed batch: %d %s", response.Code, response.Body)
	}
	var last Event
	if err := f.db.Where("run_id=?", run.ID).Order("seq DESC").First(&last).Error; err != nil || last.Type != "done" {
		t.Fatalf("terminal cursor missing: %+v err=%v", last, err)
	}
	reconnect := securityRequest(t, f, http.MethodGet, fmt.Sprintf("/api/agent/runs/%s/events?after=%d", run.ID, last.Seq), nil, securityAdmin)
	if reconnect.Code != 200 || strings.Contains(reconnect.Body.String(), "event: update") || paidCalls.Load() != 0 {
		t.Fatalf("cursor replay duplicated events or paid calls: %d %s calls=%d", reconnect.Code, reconnect.Body, paidCalls.Load())
	}
}

func TestTerminalRunMessageAndEventsCommitAtomically(t *testing.T) {
	for _, status := range []string{"completed", "failed", "cancelled", "budget_exceeded"} {
		t.Run(status, func(t *testing.T) {
			f := newAgentSecurityFixture(t, nil)
			run := seedFinalizationRun(t, f)
			var observed atomic.Bool
			callback := "test:observe_uncommitted_terminal_run"
			if err := f.db.Callback().Update().After("gorm:commit_or_rollback_transaction").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table != "ai_runs" || !observed.CompareAndSwap(false, true) {
					return
				}
				// This second connection must still see the whole old state.
				var stored Run
				var message Message
				var events int64
				if err := f.db.First(&stored, "id=?", run.ID).Error; err != nil {
					t.Error(err)
				}
				_ = f.db.Where("run_id=? AND role='assistant'", run.ID).First(&message).Error
				_ = f.db.Model(&Event{}).Where("run_id=?", run.ID).Count(&events).Error
				if stored.Status != "running" || message.Status != "running" || message.Content != "original" || events != 0 {
					t.Errorf("partial terminal state visible before events commit: run=%s message=%+v events=%d", stored.Status, message, events)
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = f.db.Callback().Update().Remove(callback) })
			if err := f.service.finishRun(context.Background(), run.ID, status, "terminal-reason", "final-text", []Block{}); err != nil {
				t.Fatal(err)
			}
			var events []Event
			if err := f.db.Where("run_id=?", run.ID).Order("seq").Find(&events).Error; err != nil || !observed.Load() || len(events) != 2 || events[0].Type != "message" || events[1].Type != "done" {
				t.Fatalf("terminal events missing: %+v observed=%v err=%v", events, observed.Load(), err)
			}
			var done map[string]any
			_ = json.Unmarshal([]byte(events[1].Data), &done)
			if done["status"] != status || done["error"] != "terminal-reason" || !strings.Contains(events[0].Data, `"content":"final-text"`) {
				t.Fatalf("terminal outcome was lost: %+v", events)
			}
		})
	}
}

func TestFailedFinalEventRollsBackAndRetryDoesNotDuplicate(t *testing.T) {
	f := newAgentSecurityFixture(t, nil)
	run := seedFinalizationRun(t, f)
	var rejected atomic.Bool
	callback := "test:reject_first_done_insert"
	if err := f.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*Event); ok && event.Type == "done" && rejected.CompareAndSwap(false, true) {
			tx.AddError(errors.New("injected final event insert failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.db.Callback().Create().Remove(callback) })
	if err := f.service.finishRun(context.Background(), run.ID, "cancelled", "stopped", "final", []Block{}); err == nil {
		t.Fatal("event insertion failure was discarded")
	}
	var stored Run
	var message Message
	var count int64
	_ = f.db.First(&stored, "id=?", run.ID).Error
	_ = f.db.Where("run_id=?", run.ID).First(&message).Error
	_ = f.db.Model(&Event{}).Where("run_id=?", run.ID).Count(&count).Error
	if stored.Status != "running" || message.Content != "original" || message.Status != "running" || count != 0 {
		t.Fatalf("failed final transaction partially committed: %+v %+v events=%d", stored, message, count)
	}
	for i := 0; i < 2; i++ {
		if err := f.service.finishRun(context.Background(), run.ID, "cancelled", "stopped", "final", []Block{}); err != nil {
			t.Fatal(err)
		}
	}
	_ = f.db.Model(&Event{}).Where("run_id=?", run.ID).Count(&count).Error
	if count != 2 {
		t.Fatalf("retry duplicated final events: %d", count)
	}
}

func TestExecuteRetriesOnlyTerminalWriteWithoutRepeatingModel(t *testing.T) {
	var calls, rejected atomic.Int64
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		finalResultFixtureReply(w, "无需查询即可说明只读能力。")
	})
	callback := "test:transient_done_insert_failure"
	if err := f.db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*Event); ok && event.Type == "done" && rejected.Add(1) == 1 {
			tx.AddError(errors.New("injected transient final event write failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.db.Callback().Create().Remove(callback) })
	conversation := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, conversation.ID, "说明查询能力", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	var done, messages, charges int64
	_ = f.db.Model(&Event{}).Where("run_id=? AND type='done'", run.ID).Count(&done).Error
	_ = f.db.Model(&Event{}).Where("run_id=? AND type='message'", run.ID).Count(&messages).Error
	_ = f.db.Model(&BudgetReservation{}).Where("run_id=?", run.ID).Count(&charges).Error
	if finished.Status != "completed" || calls.Load() != 1 || rejected.Load() != 2 || done != 1 || messages != 1 || charges != 1 {
		t.Fatalf("terminal retry repeated provider work or lost final state: %+v calls=%d writes=%d done=%d messages=%d charges=%d", finished, calls.Load(), rejected.Load(), done, messages, charges)
	}
}
