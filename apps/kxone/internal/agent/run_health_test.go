package agent

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRunHealthShanghaiCreatedMonthAndDurationCoverage(t *testing.T) {
	f := newAgentSecurityFixture(t, nil)
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	start := time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 31, 16, 0, 0, 0, time.UTC)
	add := func(id, status string, created time.Time, duration *time.Duration) {
		t.Helper()
		var finished *time.Time
		if duration != nil {
			value := created.Add(*duration)
			finished = &value
		}
		run := Run{ID: id, RequestID: id, ConversationID: "private-history", AccountID: 1, Status: status, CreatedAt: created, FinishedAt: finished}
		if err := f.db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
	}
	one, three, negative := time.Second, 3*time.Second, -time.Second
	add("before-shanghai-month", "failed", start.Add(-time.Second), &three)
	add("at-shanghai-month", "completed", start, &one)
	add("second-success", "completed", now, &three)
	add("unknown-finish", "completed", now, nil)
	add("negative-duration", "completed", now, &negative)
	for _, status := range []string{"failed", "interrupted", "cancelled", "budget_exceeded", "queued", "running"} {
		add(status, status, now, nil)
	}
	add("next-shanghai-month", "failed", end, &one)
	health, err := runHealthSummary(context.Background(), f.db, now)
	if err != nil {
		t.Fatal(err)
	}
	if health.Month != "2026-10" || health.Total != 10 || health.Completed != 4 || health.Failed != 1 || health.Interrupted != 1 || health.Cancelled != 1 || health.BudgetExceeded != 1 || health.Queued != 1 || health.Running != 1 {
		t.Fatalf("unexpected month/status coverage: %+v", health)
	}
	if health.FailureSampleCount != 6 || health.FailureRate == nil || math.Abs(*health.FailureRate-1.0/3) > 1e-9 {
		t.Fatalf("failure denominator includes cancellations/pending: %+v", health)
	}
	if health.DurationSampleCount != 2 || health.UnknownDurationCount != 2 || health.AverageDurationMS == nil || math.Abs(*health.AverageDurationMS-2000) > 1 {
		t.Fatalf("invalid timestamps counted as zero/negative duration: %+v", health)
	}
}

func TestRunHealthEmptySamplesStayUnknown(t *testing.T) {
	f := newAgentSecurityFixture(t, nil)
	for _, status := range []string{"", "cancelled"} {
		if status != "" {
			if err := f.db.Create(&Run{ID: "cancelled-only", RequestID: "cancelled-only", Status: status, CreatedAt: time.Now().UTC()}).Error; err != nil {
				t.Fatal(err)
			}
		}
		health, err := runHealthSummary(context.Background(), f.db, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if health.FailureRate != nil || health.AverageDurationMS != nil || health.FailureSampleCount != 0 || health.DurationSampleCount != 0 {
			t.Fatalf("no valid sample must be null: %+v", health)
		}
	}
}

func TestSettingsRunHealthAdminOnlyAndDeletedConversationScope(t *testing.T) {
	f := newAgentSecurityFixture(t, nil)
	now := time.Now().UTC()
	conversation := Conversation{ID: "private-conversation-sentinel", AccountID: 1, Title: "private-title-sentinel", CreatedAt: now, UpdatedAt: now}
	if err := f.db.Create(&conversation).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&Run{ID: "private-run-sentinel", RequestID: "run-health-test", ConversationID: conversation.ID, AccountID: 1, Status: "failed", Error: "private-error-sentinel", CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	for _, actor := range []Actor{securityAdmin, securityManager} {
		response := securityRequest(t, f, http.MethodGet, "/api/agent/settings", nil, actor)
		if response.Code != 200 {
			t.Fatalf("settings HTTP %d", response.Code)
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if actor.Role != "admin" {
			if _, present := body["run_health"]; present {
				t.Fatal("non-admin received aggregate operation health")
			}
			continue
		}
		var health RunHealthSummary
		if err := json.Unmarshal(body["run_health"], &health); err != nil || health.Total != 1 || health.Failed != 1 {
			t.Fatalf("admin health missing: %s (%v)", body["run_health"], err)
		}
		if !strings.Contains(health.Scope, "删除会话后不再计入") {
			t.Fatal("retained-conversation scope must be explicit")
		}
		for _, forbidden := range []string{"private-", "account_id", "conversation_id", "error"} {
			if strings.Contains(string(body["run_health"]), forbidden) {
				t.Fatalf("run health exposes private detail: %s", forbidden)
			}
		}
	}
	deleted := securityRequest(t, f, http.MethodDelete, "/api/agent/conversations/"+conversation.ID, nil, securityAdmin)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete conversation HTTP %d", deleted.Code)
	}
	health, err := runHealthSummary(context.Background(), f.db, now)
	if err != nil || health.Total != 0 || health.FailureRate != nil {
		t.Fatalf("deleted samples still included: %+v (%v)", health, err)
	}
}
