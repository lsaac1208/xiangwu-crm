package agent

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
)

func TestSourcePerformanceWithoutPeriodKeepsTheLeadCohort(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []MetricSelection{{MetricID: "source_deals"}}}))
	})
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	_, events := askScopeQuestion(t, f, "先比较所有来源成交表现。")
	found := false
	for _, event := range events {
		if event.Type != "metric_selected" {
			continue
		}
		var selection MetricSelection
		if err := json.Unmarshal([]byte(event.Data), &selection); err != nil {
			t.Fatal(err)
		}
		if selection.MetricID != "source_performance" {
			t.Fatal("all-time conversion performance lost the full lead denominator:", event.Data)
		}
		found = true
	}
	if !found {
		t.Fatal("no cohort result")
	}
}

func TestDealOccurrenceFollowUpCannotBecomeOwnerCreationCohort(t *testing.T) {
	for _, question := range []string{"按当前负责人拆开。", "按人拆开。"} {
		t.Run(question, func(t *testing.T) {
			f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
				finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []MetricSelection{{MetricID: "owner_current"}}}))
			})
			f.service.cfg.FixedMetrics = true
			seedOwnerInventory(t, f)
			c := securityCreateConversation(t, f, securityAdmin)
			askScopeInConversation(t, f, c.ID, "本月各来源成交了几单？")
			_, events := askScopeInConversation(t, f, c.ID, question)
			declined := false
			for _, event := range events {
				if event.Type == "metric_selected" {
					t.Fatal("deal-date subset was replaced with created-date leads:", event.Data)
				}
				if event.Type == "metric_declined" {
					var answer map[string]any
					if err := json.Unmarshal([]byte(event.Data), &answer); err != nil {
						t.Fatal(err)
					}
					declined = answer["answer_status"] == "unsupported"
				}
			}
			if !declined {
				t.Fatal("unsupported occurrence breakdown was not identified")
			}
		})
	}
}

func TestDealOccurrencePersonFollowUpKeepsItsDateBasis(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []MetricSelection{{MetricID: "owner_current"}}}))
	})
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	c := securityCreateConversation(t, f, securityAdmin)
	askScopeInConversation(t, f, c.ID, "本月各来源成交了几单？")
	_, events := askScopeInConversation(t, f, c.ID, "换成占伟呢？")
	found := false
	for _, event := range events {
		if event.Type != "metric_selected" {
			continue
		}
		var selection MetricSelection
		if err := json.Unmarshal([]byte(event.Data), &selection); err != nil {
			t.Fatal(err)
		}
		if selection.MetricID != "source_deals" || selection.Params.Owner != "占伟" || selection.Params.DateFrom == "" {
			t.Fatal("person follow-up lost deal-date scope:", event.Data)
		}
		found = true
	}
	if !found {
		t.Fatal("supported person follow-up was not answered")
	}
}

func TestDealCountSourceFollowUpUsesTheOccurrenceBreakdown(t *testing.T) {
	var calls atomic.Int32
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		metric := "source_deals"
		if calls.Add(1) == 1 {
			metric = "deal_count"
		}
		finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []MetricSelection{{MetricID: metric}}}))
	})
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	c := securityCreateConversation(t, f, securityAdmin)
	askScopeInConversation(t, f, c.ID, "本月成交了几单？")
	_, events := askScopeInConversation(t, f, c.ID, "按来源拆开。")
	found := false
	for _, event := range events {
		if event.Type != "metric_selected" {
			continue
		}
		var selection MetricSelection
		if err := json.Unmarshal([]byte(event.Data), &selection); err != nil {
			t.Fatal(err)
		}
		if selection.MetricID != "source_deals" || selection.Params.DateFrom == "" {
			t.Fatal("source follow-up lost the requested breakdown:", event.Data)
		}
		found = true
	}
	if !found {
		t.Fatal("supported source follow-up was not answered")
	}
}
