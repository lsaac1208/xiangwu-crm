package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise the same service boundary as a real failed selection. Safe metadata
// must survive a completed parser answer; raw provider bodies must not.
func TestMetricFailureRecordsSafeDiagnostics(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"error":"PRIVATE_UPSTREAM_DETAIL"}`))
	})
	f.service.cfg.FixedMetrics = true
	answer, events := askScopeQuestion(t, f, "各员工跟进了多少条")
	found := false
	for _, event := range events {
		if strings.Contains(event.Data, "PRIVATE_UPSTREAM_DETAIL") || strings.Contains(event.Data, f.service.cfg.APIKey) {
			t.Fatal("provider detail escaped into an event")
		}
		if event.Type != "model_usage" {
			continue
		}
		found = true
		var data map[string]any
		if err := json.Unmarshal([]byte(event.Data), &data); err != nil {
			t.Fatal(err)
		}
		if data["failure_kind"] != "http" || data["provider_http_status"] != float64(503) || data["stage"] != "select" || data["duration_ms"] == nil || data["settle_result"] != "unknown" {
			t.Fatalf("missing safe failure diagnostics: %s", event.Data)
		}
	}
	if !found || strings.Contains(answer.Content, "PRIVATE_UPSTREAM_DETAIL") {
		t.Fatalf("missing failure record or unsafe answer: %s", answer.Content)
	}
}

func TestMetricStalledPrimaryLeavesTimeForConfiguredFallback(t *testing.T) {
	var primary, backupCalls atomic.Int32
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		primary.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	f.service.cfg.FixedMetrics = true
	f.service.cfg.Timeout = 1500 * time.Millisecond
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backupCalls.Add(1)
		finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []MetricSelection{{MetricID: "followup_by_actor"}}}))
	}))
	t.Cleanup(backup.Close)
	configureTestBackup(t, f, backup.URL)
	_, events := askScopeQuestion(t, f, "各员工实际记录了多少条跟进？按操作人统计。")
	if primary.Load() != 1 || backupCalls.Load() != 1 {
		t.Fatalf("stalled primary consumed fallback time: primary=%d backup=%d", primary.Load(), backupCalls.Load())
	}
	found := false
	for _, event := range events {
		found = found || event.Type == "provider_switch"
	}
	if !found {
		t.Fatal("fallback was not recorded")
	}
	summary, err := f.service.budget.Summary(t.Context(), time.Now())
	if err != nil || summary.PendingCalls != 1 || summary.ReservedMicro <= 0 {
		t.Fatalf("unknown primary usage was discarded: %+v %v", summary, err)
	}
}

func TestOptionalInterpretationLeavesTimeToPublishComputedNumbers(t *testing.T) {
	var primary atomic.Int32
	stall := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if primary.Add(1) == 1 {
			time.Sleep(450 * time.Millisecond)
			finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []MetricSelection{{MetricID: "source_performance"}}}))
			return
		}
		stall(w, r)
	})
	f.service.cfg.FixedMetrics = true
	f.service.cfg.Timeout = 1800 * time.Millisecond
	seedOwnerInventory(t, f)
	backup := httptest.NewServer(http.HandlerFunc(stall))
	t.Cleanup(backup.Close)
	configureTestBackup(t, f, backup.URL)
	started := time.Now()
	answer, events := askScopeQuestion(t, f, "比较各来源表现")
	if elapsed := time.Since(started); elapsed > 1450*time.Millisecond {
		t.Fatalf("optional prose consumed publication time: %v", elapsed)
	}
	if !strings.Contains(answer.Content, "条") || primary.Load() != 2 {
		t.Fatal("computed result was lost or interpretation never ran:", answer.Content)
	}
	for _, event := range events {
		if event.Type == "interpretation_unavailable" {
			return
		}
	}
	t.Fatal("failed optional interpretation was not recorded")
}

func TestMetricTruncatedStreamRecordsSafeFailurePhase(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"))
	})
	f.service.cfg.FixedMetrics = true
	_, events := askScopeQuestion(t, f, "各员工跟进了多少条")
	for _, event := range events {
		if event.Type != "model_usage" {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(event.Data), &data); err != nil {
			t.Fatal(err)
		}
		if data["failure_kind"] != "transport" || data["failure_phase"] != "response_body" || data["failure_reason"] != "truncated" || data["provider_http_status"] != float64(200) {
			t.Fatalf("lost stream failure phase: %s", event.Data)
		}
		return
	}
	t.Fatal("missing model failure event")
}

func TestMetricHTTP200FailuresKeepKnownStatusAndSafeCategory(t *testing.T) {
	for _, sample := range []struct{ name, body, kind string }{
		{"upstream", "data: {\"error\":{\"message\":\"PRIVATE_ERROR\"}}\n\n", "upstream"},
		{"malformed", "data: malformed PRIVATE_ERROR\n\n", "protocol"},
	} {
		t.Run(sample.name, func(t *testing.T) {
			f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(sample.body))
			})
			f.service.cfg.FixedMetrics = true
			_, events := askScopeQuestion(t, f, "各员工跟进了多少条")
			for _, event := range events {
				if event.Type != "model_usage" {
					continue
				}
				var data map[string]any
				_ = json.Unmarshal([]byte(event.Data), &data)
				if data["provider_http_status"] != float64(200) || data["failure_kind"] != sample.kind || data["failure_phase"] != "response_body" || strings.Contains(event.Data, "PRIVATE_ERROR") {
					t.Fatalf("known response failure lost safe diagnostics: %s", event.Data)
				}
				return
			}
			t.Fatal("missing failure event")
		})
	}
}

func TestSimpleOwnerInventoryDoesNotDependOnProviderAvailability(t *testing.T) {
	var calls atomic.Int32
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(503)
	})
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	for _, question := range []string{"占伟手上有多少线索 ？", "请问目前占伟名下有多少线索？", "占伟负责多少线索？"} {
		answer, events := askScopeQuestion(t, f, question)
		if !strings.Contains(answer.Content, "4 条") || !strings.Contains(answer.Content, "1 条") {
			t.Fatal(answer.Content)
		}
		found := false
		for _, e := range events {
			found = found || (e.Type == "model_skipped" && strings.Contains(e.Data, `"reason":"not_requested"`))
		}
		if !found {
			t.Fatal("no explicit evidence that this completed run made no model request")
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("simple, fully resolved inventory still waits for provider: %d calls", calls.Load())
	}
}

func TestOwnerInventoryExtraClauseStillValidatesModelSelection(t *testing.T) {
	var calls atomic.Int32
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// The supplier drops the named owner, as the old fallback did online.
		finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []MetricSelection{{MetricID: "lead_count"}}}))
	})
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	answer, events := askScopeQuestion(t, f, "占伟手上有多少线索？请按状态拆分。")
	if calls.Load() != 1 || !strings.Contains(answer.Content, "4 条") {
		t.Fatalf("extra clause bypassed model or lost owner: calls=%d answer=%s", calls.Load(), answer.Content)
	}
	for _, event := range events {
		if event.Type == "model_skipped" && strings.Contains(event.Data, `"reason":"not_requested"`) {
			t.Fatal("a paid request was marked free")
		}
		if event.Type == "metric_selected" && (!strings.Contains(event.Data, `"owner_id":"staff-zhan"`) || !strings.Contains(event.Data, `"owner_inventory"`)) {
			t.Fatal("model selection escaped scope guard:", event.Data)
		}
	}
}
