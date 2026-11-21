package agent

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAgentFallbackSharesLimitsAndKeepsUnknownPrimaryReservation(t *testing.T) {
	var primaryCalls, backupCalls atomic.Int64
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backupCalls.Add(1)
		var raw map[string]any
		_ = json.NewDecoder(r.Body).Decode(&raw)
		thinking, _ := raw["thinking"].(map[string]any)
		if thinking["type"] != "disabled" {
			t.Error("DeepSeek tool calls require explicit non-thinking mode")
		}
		encoded, _ := json.Marshal(raw)
		var request ModelRequest
		_ = json.Unmarshal(encoded, &request)
		fixtureModelHandler(w, rWithoutBody(request, r))
	}))
	defer backup.Close()
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) { primaryCalls.Add(1); w.WriteHeader(503) })
	const backupKey = "SYNTHETIC_BACKUP_CREDENTIAL_ONLY"
	response := securityRequest(t, f, http.MethodPut, "/api/agent/settings", map[string]any{"base_url": f.service.cfg.BaseURL, "model": f.service.cfg.Model, "enabled": true, "budget_micro": 300000000, "expected_version": 1, "fallback": map[string]any{"enabled": true, "base_url": backup.URL, "model": "deepseek-flash", "api_key": backupKey, "input_price_cny": 2.0, "output_price_cny": 8.0}}, securityAdmin)
	if response.Code != 200 {
		t.Fatalf("fallback settings rejected: %d", response.Code)
	}
	var settings map[string]any
	_ = json.Unmarshal(response.Body.Bytes(), &settings)
	if settings["fallback"] == nil {
		t.Fatal("configured backup is not returned in masked settings")
	}
	if strings.Contains(response.Body.String(), backupKey) {
		t.Fatal("settings exposed backup credential")
	}
	c := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "统计来源数量", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	if finished.Status != "completed" || finished.Model != "deepseek-flash" {
		t.Fatalf("primary failure was not recovered: status=%s model=%s error=%s", finished.Status, finished.Model, finished.Error)
	}
	if primaryCalls.Load() != 1 || backupCalls.Load() != 4 {
		t.Fatalf("unexpected provider calls: primary=%d backup=%d", primaryCalls.Load(), backupCalls.Load())
	}
	summary, err := f.service.budget.Summary(t.Context(), finished.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if summary.PendingCalls != 1 || summary.ReservedMicro <= 0 || summary.SettledCalls != 4 || summary.SettledMicro != 6560 {
		t.Fatalf("fallback changed budget rules or used wrong prices: %+v", summary)
	}
	var events []Event
	_ = f.db.Where("run_id=? AND type='provider_switch'", run.ID).Find(&events).Error
	if len(events) != 1 {
		t.Fatal("provider switch was not recorded exactly once")
	}
}

func configureTestBackup(t *testing.T, f *agentSecurityFixture, url string) {
	t.Helper()
	r := securityRequest(t, f, http.MethodPut, "/api/agent/settings", map[string]any{"base_url": f.service.cfg.BaseURL, "model": f.service.cfg.Model, "enabled": true, "budget_micro": 300000000, "expected_version": 1, "fallback": map[string]any{"enabled": true, "base_url": url, "model": "deepseek-flash", "api_key": "SYNTHETIC_SECOND_PROVIDER_CREDENTIAL", "input_price_cny": 2.0, "output_price_cny": 8.0}}, securityAdmin)
	if r.Code != 200 {
		t.Fatalf("backup setup: %d", r.Code)
	}
}

func TestFallbackDoesNotHideInvalidCredentialsOrExceedCallLimit(t *testing.T) {
	for _, test := range []struct {
		name          string
		status, limit int
		wantBackup    int64
	}{
		{"invalid_request", 400, 8, 0}, {"invalid_key", 401, 8, 0}, {"forbidden", 403, 8, 0},
		{"rate_limit", 429, 8, 1}, {"server_error", 500, 8, 1}, {"last_call", 503, 1, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			var backupCalls atomic.Int64
			backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { backupCalls.Add(1); w.WriteHeader(503) }))
			defer backup.Close()
			f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status) })
			f.service.cfg.MaxCalls = test.limit
			configureTestBackup(t, f, backup.URL)
			c := securityCreateConversation(t, f, securityAdmin)
			run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "分析测试", uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			if finished := securityWaitRun(t, f, run.ID); finished.Status != "failed" {
				t.Fatalf("unexpected status %s", finished.Status)
			}
			if backupCalls.Load() != test.wantBackup {
				t.Fatalf("fallback calls=%d expected=%d", backupCalls.Load(), test.wantBackup)
			}
		})
	}
}

func TestFallbackSettingsAreEncryptedVersionedAndPrivate(t *testing.T) {
	f := newAgentSecurityFixture(t, nil)
	configureTestBackup(t, f, f.service.cfg.BaseURL)
	var current Settings
	if err := f.db.First(&current, 1).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(current.FallbackJSON, "SYNTHETIC_SECOND_PROVIDER_CREDENTIAL") || !strings.Contains(current.FallbackJSON, "enc:v1:") {
		t.Fatal("backup credential was not stored encrypted")
	}
	update := map[string]any{"base_url": current.BaseURL, "model": current.Model, "enabled": true, "budget_micro": 300000000, "expected_version": 1, "fallback": map[string]any{"enabled": false, "base_url": current.BaseURL, "model": "deepseek-flash", "api_key": "SYNTHETIC_STALE_CREDENTIAL", "input_price_cny": 2.0, "output_price_cny": 8.0}}
	if r := securityRequest(t, f, http.MethodPut, "/api/agent/settings", update, securityAdmin); r.Code != 409 {
		t.Fatalf("stale config did not conflict: %d", r.Code)
	}
	var after Settings
	f.db.First(&after, 1)
	if after.FallbackJSON != current.FallbackJSON || after.KeyCiphertext != current.KeyCiphertext {
		t.Fatal("stale update changed credentials")
	}
	if r := securityRequest(t, f, http.MethodPut, "/api/agent/settings", update, securityManager); r.Code != 403 {
		t.Fatal("manager edited shared provider credentials")
	}
	r := securityRequest(t, f, http.MethodGet, "/api/agent/settings", nil, securityManager)
	if r.Code != 200 || strings.Contains(r.Body.String(), "key_ciphertext") || strings.Contains(r.Body.String(), "fallback_json") || strings.Contains(r.Body.String(), current.BaseURL) {
		t.Fatal("private configuration entered manager response")
	}
	r = securityRequest(t, f, http.MethodPost, "/api/agent/settings/test", map[string]any{"target": "fallback"}, securityAdmin)
	if r.Code != 200 {
		t.Fatalf("stored backup connection test failed: %d", r.Code)
	}
}

func TestCancellationDoesNotStartBackup(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int64
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(503) }))
	defer backup.Close()
	release := make(chan struct{})
	defer close(release)
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, ": heartbeat\n\n")
		w.(http.Flusher).Flush()
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	configureTestBackup(t, f, backup.URL)
	c := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "可停止的分析", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("model call did not start")
	}
	if err := f.service.Cancel(t.Context(), securityAdmin, run.ID); err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	if finished.Status != "cancelled" || calls.Load() != 0 {
		t.Fatal("cancelled request incorrectly switched provider")
	}
}
