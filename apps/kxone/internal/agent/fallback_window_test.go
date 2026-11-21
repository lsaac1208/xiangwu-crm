package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/lsaac128/kxone/pkg/types"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPrimaryCumulativeWindowLeavesTimeForBackup(t *testing.T) {
	var primary, backupCalls atomic.Int64
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backupCalls.Add(1)
		fixtureModelHandler(w, r)
	}))
	defer backup.Close()
	release := make(chan struct{})
	defer close(release)
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if primary.Add(1) <= 2 {
			time.Sleep(350 * time.Millisecond)
			fixtureModelHandler(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, ": heartbeat\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	f.service.cfg.Timeout = 2 * time.Second
	configureTestBackup(t, f, backup.URL)
	conversation := securityCreateConversation(t, f, securityAdmin)
	started := time.Now()
	run, err := f.service.Start(t.Context(), securityAdmin, conversation.ID, "统计来源数量", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	if finished.Status != "completed" || finished.Model != "deepseek-flash" {
		t.Fatalf("slow multi-call primary used backup window: status=%s model=%s error=%s", finished.Status, finished.Model, finished.Error)
	}
	if primary.Load() != 3 || backupCalls.Load() != 4 || time.Since(started) >= 2*time.Second {
		t.Fatalf("calls/time changed original run budget: primary=%d backup=%d elapsed=%s", primary.Load(), backupCalls.Load(), time.Since(started))
	}
	summary, err := f.service.budget.Summary(t.Context(), finished.CreatedAt)
	if err != nil || summary.PendingCalls != 1 || summary.ReservedMicro <= 0 || summary.SettledCalls != 6 {
		t.Fatalf("unknown timed-out primary usage must remain reserved: %+v %v", summary, err)
	}
	var events []Event
	if err := f.db.Where("run_id=? AND type='provider_switch'", run.ID).Find(&events).Error; err != nil || len(events) != 1 {
		t.Fatalf("one fallback transition expected: %v count=%d", err, len(events))
	}
	var detail map[string]any
	if json.Unmarshal([]byte(events[0].Data), &detail) != nil || detail["reason"] != "primary_unavailable" {
		t.Fatalf("timeout must take the existing failed-provider transition: %v", detail)
	}
}

func TestPrimaryWindowDeadlinesAndToolsDoNotRenew(t *testing.T) {
	now := time.Now()
	for _, test := range []struct{ timeout, elapsed, want time.Duration }{
		{180 * time.Second, 0, 90 * time.Second},
		{180 * time.Second, 100 * time.Second, 20 * time.Second},
		{8 * time.Second, 0, 4 * time.Second},
	} {
		ctx, cancel := context.WithDeadline(t.Context(), now.Add(test.timeout))
		anchor := now.Add(test.elapsed)
		window := newPrimaryTimeWindow(ctx, anchor, test.timeout)
		if got := window.deadline.Sub(anchor); got != test.want {
			t.Fatalf("window=%s want=%s", got, test.want)
		}
		// Advancing through a completed query consumes the same anchored allowance.
		afterTool := window.deadline.Add(-time.Millisecond)
		child, release := window.callContext(ctx, afterTool)
		deadline, _ := child.Deadline()
		if !deadline.Equal(window.deadline) || window.expired(afterTool) || !window.expired(window.deadline) {
			t.Fatal("successful calls/tool time must not renew primary allowance")
		}
		release()
		cancel()
	}
}

type windowDelayAccounts struct {
	base    AccountReader
	primary *atomic.Int64
	once    sync.Once
}

func (a *windowDelayAccounts) GetByID(ctx context.Context, id int64) (*types.WeComUserBinding, error) {
	if a.primary.Load() > 0 {
		a.once.Do(func() {
			select {
			case <-time.After(1200 * time.Millisecond):
			case <-ctx.Done():
			}
		})
	}
	return a.base.GetByID(ctx, id)
}

func TestElapsedPrimaryWindowDispatchesUnspentSlotWithoutFakeUsage(t *testing.T) {
	for _, last := range []bool{false, true} {
		name := "backup_finishes"
		if last {
			name = "last_unused_call"
		}
		t.Run(name, func(t *testing.T) {
			var primary, backupCalls atomic.Int64
			backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				backupCalls.Add(1)
				if last {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				fixtureModelHandler(w, r)
			}))
			defer backup.Close()
			f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) { primary.Add(1); fixtureModelHandler(w, r) })
			f.service.cfg.Timeout = 2 * time.Second
			f.service.cfg.MaxCalls = 5
			if last {
				f.service.cfg.MaxCalls = 2
			}
			f.service.accounts = &windowDelayAccounts{base: f.accounts, primary: &primary}
			configureTestBackup(t, f, backup.URL)
			c := securityCreateConversation(t, f, securityAdmin)
			run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "统计来源数量", uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			done := securityWaitRun(t, f, run.ID)
			wantStatus := "completed"
			wantBackup := int64(4)
			wantPending := int64(0)
			wantSettled := int64(5)
			if last {
				wantStatus = "failed"
				wantBackup = 1
				wantPending = 1
				wantSettled = 1
			}
			if done.Status != wantStatus || primary.Load() != 1 || backupCalls.Load() != wantBackup {
				t.Fatalf("unused slot lost: status=%s primary=%d backup=%d error=%s", done.Status, primary.Load(), backupCalls.Load(), done.Error)
			}
			summary, err := f.service.budget.Summary(t.Context(), done.CreatedAt)
			if err != nil || int64(summary.PendingCalls) != wantPending || int64(summary.SettledCalls) != wantSettled {
				t.Fatalf("fake call/fee created: %+v %v", summary, err)
			}
			var events []Event
			f.db.Where("run_id=? AND type='provider_switch'", run.ID).Find(&events)
			if len(events) != 1 {
				t.Fatalf("switch count=%d", len(events))
			}
			var detail map[string]any
			if json.Unmarshal([]byte(events[0].Data), &detail) != nil || detail["reason"] != "primary_time_window" {
				t.Fatalf("scheduling falsely labelled provider fault: %v", detail)
			}
		})
	}
}
