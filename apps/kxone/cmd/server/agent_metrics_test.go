package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type metricTable struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

func (m metricTable) by(key string) map[string]map[string]any {
	idx := -1
	for i, c := range m.Columns {
		if c == key {
			idx = i
		}
	}
	out := map[string]map[string]any{}
	for _, row := range m.Rows {
		r := map[string]any{}
		for i, c := range m.Columns {
			r[c] = row[i]
		}
		if idx >= 0 {
			out[fmt.Sprint(row[idx])] = r
		}
	}
	return out
}

func runManualMetric(t *testing.T, router http.Handler, token, conversation, metric string, params map[string]any) []metricTable {
	t.Helper()
	response := doJSON(t, router, http.MethodPost, "/api/agent/conversations/"+conversation+"/metric-runs", token, map[string]any{"metric_id": metric, "params": params, "request_id": "metric-" + uuid.NewString()})
	if response.Code != http.StatusAccepted {
		t.Fatalf("%s start failed: %d %s", metric, response.Code, response.Body.String())
	}
	var run struct{ ID string }
	_ = json.Unmarshal(response.Body.Bytes(), &run)
	deadline := time.Now().Add(20 * time.Second)
	for {
		detail := doJSON(t, router, http.MethodGet, "/api/agent/runs/"+run.ID, token, nil)
		var body struct {
			Run     struct{ Status, Error, Model string }
			Results []metricTable
			Select  struct {
				ResultIDs []string `json:"result_ids"`
			} `json:"result_selection"`
		}
		_ = json.Unmarshal(detail.Body.Bytes(), &body)
		if body.Run.Status != "running" && body.Run.Status != "" {
			if body.Run.Status != "completed" || body.Run.Model != "fixed-metrics" || len(body.Select.ResultIDs) != len(body.Results) {
				t.Fatalf("%s run not completed as fixed metric: %s", metric, detail.Body.String())
			}
			return body.Results
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not finish", metric)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

// The fixed metrics reproduce the independent expectations of the evaluation
// fixture without any model, and work while AI analysis is switched off.
func TestAgentFixedMetricsMatchIndependentFixture(t *testing.T) {
	fixture, _ := loadLiveFixture(t)
	var db *gorm.DB
	router := newTestRouterWithOptions(t, testRouterOptions{agentFixtureDB: &db, agentPrepareFixture: func(t *testing.T, db *gorm.DB) { importLiveFixture(t, db, fixture, 1) }})
	admin := issueDevToken(t, router, "metrics-admin", "admin")
	created := doJSON(t, router, http.MethodPost, "/api/agent/conversations", admin, map[string]any{})
	var conversation struct{ ID string }
	_ = json.Unmarshal(created.Body.Bytes(), &conversation)
	if conversation.ID == "" {
		t.Fatalf("conversation: %s", created.Body.String())
	}
	catalog := doJSON(t, router, http.MethodGet, "/api/agent/metrics", admin, nil)
	if catalog.Code != 200 || !strings.Contains(catalog.Body.String(), "followup_by_actor") {
		t.Fatalf("catalog: %d %s", catalog.Code, catalog.Body.String())
	}
	c := conversation.ID

	overview := runManualMetric(t, router, admin, c, "overview", nil)[0].Rows[0]
	if num(overview[0]) != 7 || num(overview[1]) != 2 || num(overview[2]) != 28.57 || num(overview[3]) != 15000 || num(overview[4]) != 2000 || num(overview[5]) != 13000 {
		t.Fatalf("overview %v", overview)
	}
	sources := runManualMetric(t, router, admin, c, "source_performance", nil)[0].by("来源")
	for name, want := range map[string]float64{"好学校": 2, "员工丁抖音（员工乙）": 2, "转介绍": 1, "自己上门的": 1, "旧广告外部来源": 1} {
		if num(sources[name]["线索数"]) != want {
			t.Fatalf("source %s %v", name, sources)
		}
	}
	excluded := runManualMetric(t, router, admin, c, "source_performance", map[string]any{"exclude_sources": []string{"转介绍"}})[0].by("来源")
	if _, ok := excluded["转介绍"]; ok || len(excluded) != 4 {
		t.Fatalf("exclusion %v", excluded)
	}
	owners := runManualMetric(t, router, admin, c, "owner_current", nil)[0].by("当前归属")
	for name, want := range map[string]float64{"员工乙": 3, "小宇": 2, "已分配待领取": 1, "公海": 1} {
		if num(owners[name]["线索数"]) != want {
			t.Fatalf("owner %s %v", name, owners)
		}
	}
	school := runManualMetric(t, router, admin, c, "owner_current", map[string]any{"sources": []string{"好学校"}})[0].by("当前归属")
	if len(school) != 2 || num(school["小宇"]["线索数"]) != 1 || num(school["员工乙"]["线索数"]) != 1 || num(school["小宇"]["净成交额(元)"]) != 8000 {
		t.Fatalf("好学校 by owner %v", school)
	}
	actors := runManualMetric(t, router, admin, c, "followup_by_actor", nil)[0].by("操作人")
	if num(actors["员工乙"]["跟进条数"]) != 6 || num(actors["员工乙"]["涉及线索数(去重)"]) != 3 || num(actors["小宇"]["跟进条数"]) != 1 || num(actors["全部操作人（整体去重）"]["涉及线索数(去重)"]) != 3 {
		t.Fatalf("actors %v", actors)
	}
	keyword := runManualMetric(t, router, admin, c, "followup_keyword", map[string]any{"keyword": "费用"})
	if len(keyword) != 2 || num(keyword[0].Rows[0][0]) != 3 || num(keyword[0].Rows[0][1]) != 2 || num(keyword[0].Rows[0][2]) != 1 || len(keyword[1].Rows) != 3 {
		t.Fatalf("keyword %v", keyword)
	}
	raw, _ := json.Marshal(keyword)
	for _, fragment := range fixture.IndependentExpected["forbidden_provider_payload_fragments"].([]any) {
		if strings.Contains(string(raw), fragment.(string)) {
			t.Fatal("keyword metric exposes a sensitive fixture fragment")
		}
	}
	status := runManualMetric(t, router, admin, c, "lead_followup_status", map[string]any{"customer": "客户#2"})[0].by("客户")["客户#2"]
	if status["当前普通回访资格"] != "否" || num(status["当前普通待办数"]) != 0 || num(status["历史回访计划数"]) < 2 || status["有效预约"] != "有" {
		t.Fatalf("客户#2 status %v", status)
	}
	taskResults := runManualMetric(t, router, admin, c, "followup_tasks", nil)
	if num(taskResults[0].Rows[0][0]) != 1 || num(taskResults[0].Rows[0][1]) != 1 {
		t.Fatalf("task aggregate %v", taskResults[0])
	}
	tasks := taskResults[1]
	if len(tasks.Rows) != 1 || tasks.Rows[0][0] != "客户#7" {
		t.Fatalf("tasks %v", tasks)
	}
	ratings := runManualMetric(t, router, admin, c, "rating_distribution", nil)[0].by("评级")
	if num(ratings["无有效评级"]["线索数"]) != 2 {
		t.Fatalf("ratings %v", ratings)
	}
	daily := runManualMetric(t, router, admin, c, "daily_new_leads", map[string]any{"date_from": "2026-10-08", "date_to": "2026-10-08"})[0]
	if len(daily.Rows) != 1 || num(daily.Rows[0][1]) != 1 {
		t.Fatalf("daily %v", daily)
	}
	records := runManualMetric(t, router, admin, c, "followup_records", map[string]any{"sources": []string{"好学校"}})[0]
	if len(records.Rows) != 6 {
		t.Fatalf("好学校 records %d", len(records.Rows))
	}
	for _, metric := range []string{"lead_status", "followup_sla", "appointment_status"} {
		if results := runManualMetric(t, router, admin, c, metric, nil); len(results) == 0 || len(results[0].Rows) == 0 {
			t.Fatalf("%s empty", metric)
		}
	}
	bad := doJSON(t, router, http.MethodPost, "/api/agent/conversations/"+c+"/metric-runs", admin, map[string]any{"metric_id": "followup_keyword", "params": map[string]any{}, "request_id": "metric-" + uuid.NewString()})
	if bad.Code == http.StatusAccepted {
		t.Fatal("keyword metric without keyword accepted")
	}
	sales := issueDevToken(t, router, "sales-a", "sales")
	if denied := doJSON(t, router, http.MethodPost, "/api/agent/conversations/"+c+"/metric-runs", sales, map[string]any{"metric_id": "overview", "request_id": "metric-" + uuid.NewString()}); denied.Code == http.StatusAccepted {
		t.Fatal("sales account ran a metric")
	}
}
