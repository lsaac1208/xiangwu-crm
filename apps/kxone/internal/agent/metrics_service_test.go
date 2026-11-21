package agent

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func TestSanitizeInterpretationDropsNumbersAndCausalClaims(t *testing.T) {
	text, dropped := sanitizeInterpretation(nil, "好学校来源的成交表现较好。共有两条线索。成交率为50%。费用导致客户流失。现有记录不足以证明费用造成流失。客户#2目前有有效预约。")
	if dropped != 3 {
		t.Fatalf("dropped=%d text=%s", dropped, text)
	}
	for _, kept := range []string{"好学校来源的成交表现较好。", "现有记录不足以证明费用造成流失。", "客户#2目前有有效预约。"} {
		if !strings.Contains(text, kept) {
			t.Fatalf("missing %q in %s", kept, text)
		}
	}
	for _, removed := range []string{"两条", "50", "费用导致"} {
		if strings.Contains(text, removed) {
			t.Fatalf("kept %q in %s", removed, text)
		}
	}
	if all, n := sanitizeInterpretation(nil, "共7条。"); n != 1 || all != "" {
		t.Fatalf("all-dropped fallback %q", all)
	}
}

func metricToolCall(name string, args any) ToolCall {
	return finalResultTool(name, args)
}

func TestFixedMetricRunUsesTwoCallsAndDowngradesInterpretation(t *testing.T) {
	var calls atomic.Int32
	var second ModelRequest
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var req ModelRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch calls.Add(1) {
		case 1:
			if len(req.Tools) != 2 {
				t.Errorf("select stage tools=%d", len(req.Tools))
			}
			finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []any{map[string]any{"metric_id": "source_performance", "params": map[string]any{"exclude_sources": []string{"转介绍"}}}}}))
		case 2:
			second = req
			finalResultFixtureReply(w, "", metricToolCall("write_interpretation", map[string]any{"text": "各来源表现接近。共有三条线索成交。投放不足导致转化偏低。现有数据无法证明长期效果。"}))
		default:
			t.Error("extra model call")
			w.WriteHeader(500)
		}
	})
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	c := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "比较各来源成交表现，排除转介绍", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	done := securityWaitRun(t, f, run.ID)
	if done.Status != "completed" || calls.Load() != 2 {
		t.Fatalf("run %+v calls=%d", done, calls.Load())
	}
	if len(second.Tools) != 1 || !strings.Contains(second.Messages[len(second.Messages)-1].Content, "write_interpretation") {
		t.Fatal("interpretation stage did not receive server results and only the interpretation tool")
	}
	var answer Message
	f.db.Where("run_id=? AND role=?", run.ID, "assistant").First(&answer)
	if strings.Contains(answer.Content, "三条") || strings.Contains(answer.Content, "导致") || !strings.Contains(answer.Content, "无法证明长期效果") || !strings.Contains(answer.Content, "排除「转介绍」") {
		t.Fatalf("interpretation not downgraded: %s", answer.Content)
	}
	var blocks []Block
	_ = json.Unmarshal([]byte(answer.BlocksJSON), &blocks)
	tables := 0
	for _, b := range blocks {
		if b.Type == "table" {
			tables++
		}
		if b.Type == "sources" && len(b.ResultIDs) != 1 {
			t.Fatalf("sources manifest %v", b.ResultIDs)
		}
	}
	if tables != 1 {
		t.Fatalf("tables=%d", tables)
	}
	var events []Event
	f.db.Where("run_id=? AND type IN ?", run.ID, []string{"interpretation_downgraded", "metric_selected"}).Find(&events)
	if len(events) != 2 {
		t.Fatalf("events %+v", events)
	}
}

func TestFixedMetricModelUnavailableAndManualFallback(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	f.service.cfg.FixedMetrics = true
	c := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "各员工跟进了多少条", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	done := securityWaitRun(t, f, run.ID)
	// The model is down: the server parser still answers the common question.
	var fallbackAnswer Message
	f.db.Where("run_id=? AND role=?", run.ID, "assistant").First(&fallbackAnswer)
	var fallbackEvents []Event
	f.db.Where("run_id=? AND type IN ?", run.ID, []string{"metric_fallback", "metric_selected"}).Find(&fallbackEvents)
	if done.Status != "completed" || len(fallbackEvents) != 2 || !strings.Contains(fallbackEvents[1].Data, "followup_by_actor") || strings.TrimSpace(fallbackAnswer.Content) == "" {
		t.Fatalf("unavailable run %+v events=%+v answer=%s", done, fallbackEvents, fallbackAnswer.Content)
	}
	// Switch AI off entirely: manual metrics still work without any model.
	f.db.Model(&Settings{}).Where("id=1").Update("enabled", false)
	if _, err := f.service.Start(t.Context(), securityAdmin, c.ID, "再问一次", uuid.NewString()); err == nil {
		t.Fatal("model run allowed while disabled")
	}
	manual, err := f.service.StartMetric(t.Context(), securityAdmin, c.ID, MetricSelection{MetricID: "followup_by_actor"}, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if done := securityWaitRun(t, f, manual.ID); done.Status != "completed" || done.Model != metricModelName {
		t.Fatalf("manual %+v", done)
	}
	if _, err := f.service.StartMetric(t.Context(), securityAdmin, c.ID, MetricSelection{MetricID: "overview", Params: MetricParams{DateFrom: "10月1日"}}, uuid.NewString()); err == nil {
		t.Fatal("invalid date accepted")
	}
}

func TestFixedMetricCannotAnswerCompletesWithoutNumbers(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		finalResultFixtureReply(w, "", metricToolCall("cannot_answer", map[string]any{"reason": "系统没有广告成本，无法计算ROI。可参考来源表现中的成交数和净成交额，约有5个来源。"}))
	})
	f.service.cfg.FixedMetrics = true
	c := securityCreateConversation(t, f, securityAdmin)
	run, _ := f.service.Start(t.Context(), securityAdmin, c.ID, "计算各来源ROI", uuid.NewString())
	done := securityWaitRun(t, f, run.ID)
	var answer Message
	f.db.Where("run_id=? AND role=?", run.ID, "assistant").First(&answer)
	if done.Status != "completed" || strings.Contains(answer.Content, "5个") || !strings.Contains(answer.Content, "无法计算ROI") {
		t.Fatalf("%+v %s", done, answer.Content)
	}
}
