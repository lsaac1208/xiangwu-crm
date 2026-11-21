package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

type unavailableQueueNotifier struct{ attempts int }

func (n *unavailableQueueNotifier) NotifyLead(context.Context, wecom.GroupRobotLead) error {
	n.attempts++
	return errors.New("queue unavailable")
}

func TestCreationNotificationWarningDoesNotRepeatBusinessOrNotificationOnRetry(t *testing.T) {
	notifier := &unavailableQueueNotifier{}
	router := newTestRouterWithOptions(t, testRouterOptions{groupRobotNotifier: notifier})
	admin := issueDevToken(t, router, "queue-warning-admin", "admin")
	payload := []byte(`{"customer_name":"queue warning fixture","customer_wechat":"queue-warning","source_channel":"转介绍"}`)
	key := "f31faf3a-25b2-4fa3-8b67-cb81d68f7f43"
	headers := map[string]string{"Content-Type": "application/json", "Idempotency-Key": key}
	first := doRaw(t, router, http.MethodPost, "/api/leads", admin, payload, headers)
	if first.Code != 201 || !strings.Contains(first.Body.String(), "notification_failed") {
		t.Fatalf("creation=%d %s", first.Code, first.Body.String())
	}
	var initial struct {
		Lead types.Lead `json:"lead"`
	}
	decodeResponse(t, first, &initial)
	second := doRaw(t, router, http.MethodPost, "/api/leads", admin, payload, headers)
	var recovered struct {
		Lead     types.Lead `json:"lead"`
		Replayed bool       `json:"replayed"`
	}
	decodeResponse(t, second, &recovered)
	if second.Code != 201 || !recovered.Replayed || recovered.Lead.ID != initial.Lead.ID || notifier.attempts != 1 {
		t.Fatalf("retry=%d replay=%v sameLead=%v external attempts=%d", second.Code, recovered.Replayed, recovered.Lead.ID == initial.Lead.ID, notifier.attempts)
	}
}

func TestNotificationDiagnosticsIsReadOnlyPrivateAndTimeBounded(t *testing.T) {
	var db *gorm.DB
	router := newTestRouterWithOptions(t, testRouterOptions{agentFixtureDB: &db})
	admin := issueDevToken(t, router, "diagnostic-admin", "admin")
	created := doJSON(t, router, http.MethodPost, "/api/leads", admin, map[string]any{"customer_name": "diagnostic fixture", "customer_wechat": "diagnostic-fixture", "source_channel": "转介绍"})
	if created.Code != 201 {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var body struct {
		Lead types.Lead `json:"lead"`
	}
	decodeResponse(t, created, &body)
	now := time.Now().UTC()
	old, future, overdue, sent := now.Add(-48*time.Hour), now.Add(time.Hour), now.Add(-30*time.Minute), now.Add(-10*time.Minute)
	rows := []repo.GroupRobotNotificationModel{
		{EventKey: "diagnostic-retry", Category: "new_lead", EventType: "new_lead", LeadID: body.Lead.ID, DueAt: overdue, Status: "failed", Attempts: 1, NextRetryAt: &future, LastError: "Post https://example.invalid/?key=probe-secret: context deadline exceeded", CreatedAt: old, UpdatedAt: now},
		{EventKey: "diagnostic-stopped", Category: "new_lead", EventType: "dispatch_assigned", LeadID: body.Lead.ID, DueAt: overdue, Status: "failed", Attempts: 4, LastError: "vendor response includes private-contact", CreatedAt: old, UpdatedAt: now},
		{EventKey: "diagnostic-old", Category: "new_lead", EventType: "new_lead", LeadID: body.Lead.ID, DueAt: old, Status: "failed", Attempts: 4, CreatedAt: old, UpdatedAt: old},
		{EventKey: "diagnostic-backlog", Category: "new_lead", EventType: "new_lead", LeadID: body.Lead.ID, DueAt: overdue, Status: "pending", CreatedAt: old, UpdatedAt: old},
		{EventKey: "diagnostic-future", Category: "new_lead", EventType: "new_lead", LeadID: body.Lead.ID, DueAt: future, Status: "pending", CreatedAt: now, UpdatedAt: now},
		{EventKey: "diagnostic-sent", Category: "new_lead", EventType: "new_lead", LeadID: body.Lead.ID, DueAt: sent, Status: "sent", SentAt: &sent, CreatedAt: old, UpdatedAt: sent},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	response := doJSON(t, router, http.MethodGet, "/api/admin/group-robot/health", admin, nil)
	if response.Code != 200 {
		t.Fatalf("health=%d %s", response.Code, response.Body.String())
	}
	var health struct {
		Failed      int        `json:"failed_count"`
		Retrying    int        `json:"retrying_count"`
		Stopped     int        `json:"exhausted_count"`
		Backlog     int        `json:"pending_overdue_count"`
		Healthy     bool       `json:"healthy"`
		LastSuccess *time.Time `json:"last_success_at"`
		Failures    []struct {
			Reason string `json:"reason"`
		} `json:"recent_failures"`
	}
	decodeResponse(t, response, &health)
	if health.Failed != 2 || health.Retrying != 1 || health.Stopped != 1 || health.Backlog != 1 || health.Healthy || health.LastSuccess == nil || len(health.Failures) != 2 {
		t.Fatalf("health=%+v", health)
	}
	for _, secret := range []string{"probe-secret", "private-contact", "https://"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatal("diagnostics disclosed original transport content")
		}
	}
	manager := issueDevToken(t, router, "diagnostic-manager", "manager")
	if r := doJSON(t, router, http.MethodGet, "/api/admin/group-robot/health", manager, nil); r.Code != 403 {
		t.Fatalf("manager=%d", r.Code)
	}
	if r := doJSON(t, router, http.MethodGet, "/api/admin/group-robot/health", "", nil); r.Code != 401 {
		t.Fatalf("anonymous=%d", r.Code)
	}
	detail := doJSON(t, router, http.MethodGet, fmt.Sprintf("/api/leads/%d", body.Lead.ID), admin, nil)
	var info struct {
		Failures []struct {
			Event string `json:"event_type"`
			Retry bool   `json:"will_retry"`
		} `json:"notification_failures"`
	}
	decodeResponse(t, detail, &info)
	if len(info.Failures) != 2 {
		t.Fatalf("lead diagnostics=%s", detail.Body.String())
	}
}
