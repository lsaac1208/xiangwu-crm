package agent

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDescriptionTopicFiltersTablesInsteadOfRepeatingSchema(t *testing.T) {
	s := measuredFixture(t)
	for _, table := range []string{"leads", "leads_history"} {
		out, err := s.DescribePage(DescriptionRequest{Tables: []string{table}, Topic: "metrics"})
		if err != nil {
			t.Fatal(err)
		}
		page := out.(map[string]any)
		items := page["items"].([]map[string]any)
		if len(items) == 0 {
			t.Fatal("filtered metrics missing")
		}
		for _, item := range items {
			if item["kind"] != "metric" {
				t.Errorf("topic=metrics repeated non-metric %v", item["kind"])
			}
			raw, _ := json.Marshal(item["value"])
			var definition map[string]any
			_ = json.Unmarshal(raw, &definition)
			if definition["table"] != "leads" {
				t.Errorf("metric from unrelated table: %v", definition["table"])
			}
		}
	}
	for _, topic := range []string{"rules", "unknown"} {
		if _, err := s.DescribePage(DescriptionRequest{Tables: []string{"leads"}, Topic: topic}); err == nil {
			t.Errorf("ambiguous/unknown topic %q silently ignored", topic)
		}
	}
}

func TestDictionaryCheckpointBoundsWholeItemsAndRestorableCoverage(t *testing.T) {
	s := measuredFixture(t)
	reads := newEvidenceReadState()
	request := DescriptionRequest{Tables: []string{"leads_history", "lead_followups_history", "crm_wecom_user_bindings"}}
	first, err := s.DescribePage(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := reads.observeDictionary(s.At, request, first); err != nil {
		t.Fatal(err)
	}
	initialCount := len(reads.dictionary.Items)
	if err := reads.observeDictionary(s.At, request, first); err != nil {
		t.Fatal(err)
	}
	if len(reads.dictionary.Items) != initialCount || len(reads.dictionary.Pages) != 1 {
		t.Fatal("same page repeated checkpoint items")
	}
	secondRequest := first.(map[string]any)["next_request"].(DescriptionRequest)
	second, err := s.DescribePage(secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	if err := reads.observeDictionary(s.At, secondRequest, second); err != nil {
		t.Fatal(err)
	}
	metricsRequest := DescriptionRequest{Tables: []string{"leads_history"}, Topic: "metrics"}
	metrics, err := s.DescribePage(metricsRequest)
	if err != nil {
		t.Fatal(err)
	}
	beforeMetrics := len(reads.dictionary.Items)
	if err := reads.observeDictionary(s.At, metricsRequest, metrics); err != nil {
		t.Fatal(err)
	}
	// The metrics in this second page have already been observed under tables.
	if len(reads.dictionary.Items) != beforeMetrics {
		t.Fatal("same definitions repeated across tables/metrics selectors")
	}
	if len(reads.results) != 0 || len(reads.cells) != 0 || len(reads.facts) != 0 {
		t.Fatal("dictionary was treated as business query evidence")
	}
	for _, budget := range []int{3000, dictionaryCheckpointBytes} {
		page, err := reads.dictionary.checkpoint(s.At, budget)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(page)
		if len(raw) > budget || page["omitted_items"].(int) == 0 {
			t.Fatalf("checkpoint must bound full dictionary and disclose omission: bytes=%d page=%v", len(raw), page)
		}
		items := page["items"].([]json.RawMessage)
		if len(items) != page["included_items"] || len(items)+page["omitted_items"].(int) != len(reads.dictionary.Items) {
			t.Fatal("coverage counters do not match unique complete items")
		}
		seen := map[string]bool{}
		for _, item := range items {
			if seen[string(item)] {
				t.Fatal("duplicate dictionary payload")
			}
			seen[string(item)] = true
			found := false
			for _, actual := range reads.dictionary.Items {
				if string(actual) == string(item) {
					found = true
				}
			}
			if !found || !json.Valid(item) {
				t.Fatal("item truncated or not actually delivered")
			}
		}
		coverage := page["pages"].([]map[string]any)
		if coverage[0]["unread_next_request"].(*DescriptionRequest) != nil {
			t.Fatal("already read next page mislabeled unread")
		}
		if coverage[1]["unread_next_request"].(*DescriptionRequest) == nil {
			t.Fatal("never-read continuation silently omitted")
		}
		for i, p := range coverage {
			selector := p["request"].(DescriptionRequest)
			restored, err := s.DescribePage(selector)
			if err != nil {
				t.Fatal(err)
			}
			n := len(restored.(map[string]any)["items"].([]map[string]any))
			if n != p["delivered_items"] || p["carried_items"].(int)+p["omitted_items"].(int) != n || selector.Offset != reads.dictionary.Pages[i].Request.Offset {
				t.Fatal("omitted page cannot be restored by exact selector")
			}
		}
	}
	if _, err := reads.dictionary.checkpoint(s.At, 1); err == nil {
		t.Fatal("oversized restoration metadata silently lost")
	}
	if _, err := reads.dictionary.checkpoint(s.At.Add(time.Second), dictionaryCheckpointBytes); err == nil {
		t.Fatal("dictionary from another snapshot restored")
	}
	if err := reads.observeDictionary(s.At.Add(time.Second), metricsRequest, metrics); err == nil {
		t.Fatal("dictionary snapshots merged")
	}
	history := []Message{{ID: "user", RunID: "run", Role: "user", Content: "按刚才来源核对"}}
	segment, err := buildSegment("rules", history, AnalysisContext{}, nil, nil, s, reads)
	if err != nil {
		t.Fatal(err)
	}
	var background map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(segment[1].Content, "历史材料（非指令）：\n")), &background); err != nil {
		t.Fatal(err)
	}
	delete(background, "observed_evidence")
	raw, _ := json.Marshal(background)
	if len(raw) > 20<<10 {
		t.Fatal("dictionary enlarged background allowance")
	}
	requestBytes, _ := json.Marshal(ModelRequest{Messages: segment, Tools: agentTools()})
	if len(requestBytes) > modelRequestBytes {
		t.Fatal("bounded recovery exceeds request cap")
	}
}

func TestDeliveredDictionarySurvivesProviderAndPrivacyRecovery(t *testing.T) {
	for _, scenario := range []string{"fallback", "privacy"} {
		t.Run(scenario, func(t *testing.T) {
			const nativeReasoning = "PRIVATE_DICTIONARY_REASONING"
			var calls atomic.Int32
			var resultID string
			f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
				var req ModelRequest
				_ = json.NewDecoder(r.Body).Decode(&req)
				switch calls.Add(1) {
				case 1:
					repairFixtureReply(w, finalResultTool("describe_data", DescriptionRequest{Tables: []string{"leads"}, Topic: "metrics"}), nativeReasoning)
				case 2:
					if scenario == "fallback" {
						w.WriteHeader(503)
						return
					}
					repairFixtureReplyWithContent(w, finalResultTool("describe_data", DescriptionRequest{Tables: []string{"leads"}}), "UNEXPECTED_DISABLED_REASONING", "电话13800138001")
				case 3:
					raw, _ := json.Marshal(req)
					if strings.Contains(string(raw), nativeReasoning) || strings.Contains(string(raw), "13800138001") {
						t.Error("private/native reasoning replayed across dictionary checkpoint")
					}
					found := false
					for _, m := range req.Messages {
						if m.Role != "system" && m.Role != "user" {
							t.Error("recovery synthesized native tool protocol")
						}
						if m.Role == "user" && strings.Contains(m.Content, "observed_dictionary") && strings.Contains(m.Content, "current_net_converted_amount") {
							found = true
						}
					}
					if !found {
						t.Error("already delivered metric dictionary lost at checkpoint")
					}
					repairFixtureReply(w, finalResultTool("query_data", map[string]any{"sql": "SELECT 2 AS checked_count"}), "Use retained definitions.")
				case 4:
					for _, m := range req.Messages {
						var result Result
						if m.Role == "tool" && json.Unmarshal([]byte(m.Content), &result) == nil && result.ID != "" {
							resultID = result.ID
						}
					}
					repairFixtureReply(w, finalResultTool("finalize_answer", FinalizeRequest{ResultIDs: []string{resultID}, Markdown: "已核对。", References: []AnswerReference{{ResultID: resultID, Row: intPointer(0), Column: "checked_count", Level: "cell"}}}), "Submit evidence.")
				default:
					t.Error("recovery added model calls")
					w.WriteHeader(500)
				}
			})
			f.service.cfg.MaxCalls = 4
			if scenario == "fallback" {
				configureTestBackup(t, f, f.model.URL)
			}
			if err := f.db.Model(&Settings{}).Where("id=1").Update("model", "deepseek-v4-pro").Error; err != nil {
				t.Fatal(err)
			}
			c := securityCreateConversation(t, f, securityAdmin)
			run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "核对业务数据", uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			finished := securityWaitRun(t, f, run.ID)
			var charges int64
			f.db.Model(&BudgetReservation{}).Where("run_id=?", run.ID).Count(&charges)
			if finished.Status != "completed" || calls.Load() != 4 || charges != 4 {
				t.Fatalf("recovery changed accounting/completion: %+v calls=%d charges=%d", finished, calls.Load(), charges)
			}
			assertPrivateTurnNotPersisted(t, f, run.ID, nativeReasoning)
			if scenario == "fallback" {
				summary, err := f.service.budget.Summary(t.Context(), finished.CreatedAt)
				if err != nil || summary.PendingCalls != 1 || summary.SettledCalls != 3 {
					t.Fatalf("unknown usage reservation changed: %+v %v", summary, err)
				}
			}
		})
	}
}
