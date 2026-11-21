package main

import (
	"encoding/json"
	"fmt"
	"github.com/lsaac128/kxone/pkg/types"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAgentRealAPIConversationPrivacyToolsAndExports(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(browserAgentModelHandler))
	defer model.Close()
	router := newTestRouterWithOptions(t, testRouterOptions{agentModelURL: model.URL, leadFixtures: []*types.Lead{{LeadNo: "AI-1", CustomerName: "张客户", CustomerMobile: "13812345678", SourceChannel: "好学校", Status: types.LeadStatusPendingDispatch, CreatedAt: time.Now(), UpdatedAt: time.Now()}, {LeadNo: "AI-2", CustomerName: "李客户", SourceChannel: "转介绍", Status: types.LeadStatusPendingDispatch, CreatedAt: time.Now(), UpdatedAt: time.Now()}}})
	admin := issueDevToken(t, router, "admin_001", "admin")
	manager := issueDevToken(t, router, "manager_001", "manager")
	sales := issueDevToken(t, router, "sales_001", "sales")
	forbidden := doJSON(t, router, http.MethodGet, "/api/agent/conversations", sales, nil)
	if forbidden.Code != 403 {
		t.Fatalf("sales Agent access %d", forbidden.Code)
	}
	created := doJSON(t, router, http.MethodPost, "/api/agent/conversations", admin, map[string]any{})
	if created.Code != 201 {
		t.Fatal(created.Body.String())
	}
	var conv struct{ ID string }
	decodeResponse(t, created, &conv)
	for _, path := range []string{"/api/agent/conversations/" + conv.ID} {
		denied := doJSON(t, router, http.MethodGet, path, manager, nil)
		if denied.Code != 403 {
			t.Fatalf("another manager sees private conversation: %d", denied.Code)
		}
	}
	body := map[string]any{"message": "按来源统计全部线索并展示图表", "request_id": "agent-real-api-request-1"}
	started := doJSON(t, router, http.MethodPost, "/api/agent/conversations/"+conv.ID+"/runs", admin, body)
	if started.Code != 202 {
		t.Fatal(started.Body.String())
	}
	var run struct{ ID string }
	decodeResponse(t, started, &run)
	duplicate := doJSON(t, router, http.MethodPost, "/api/agent/conversations/"+conv.ID+"/runs", admin, body)
	if duplicate.Code != 202 || !strings.Contains(duplicate.Body.String(), run.ID) {
		t.Fatalf("idempotency: %s", duplicate.Body.String())
	}
	var output struct {
		Run struct {
			Status string
			Error  string
		}
		Results []struct {
			ID      string
			Columns []string
			Rows    [][]any
		}
		Messages []struct {
			Role    string
			Content string
			Blocks  []struct {
				Type     string
				ResultID string `json:"result_id"`
			}
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp := doJSON(t, router, http.MethodGet, "/api/agent/runs/"+run.ID, admin, nil)
		decodeResponse(t, resp, &output)
		if output.Run.Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if output.Run.Status != "completed" {
		t.Fatalf("run failed %+v", output.Run)
	}
	if len(output.Results) != 1 || len(output.Results[0].Rows) != 2 {
		t.Fatalf("actual source query result %+v", output.Results)
	}
	for _, row := range output.Results[0].Rows {
		if row[1] != float64(1) {
			t.Fatalf("count must come from SQLite, got %+v", row)
		}
	}
	for _, msg := range output.Messages {
		if msg.Role == "assistant" {
			if len(msg.Blocks) < 2 || msg.Blocks[0].Type != "chart" || msg.Blocks[0].ResultID != output.Results[0].ID {
				t.Fatalf("chart must bind actual result: %+v", msg)
			}
		}
	}
	eventReplay := doJSON(t, router, http.MethodGet, "/api/agent/runs/"+run.ID+"/events", admin, nil)
	if eventReplay.Code != 200 || !strings.Contains(eventReplay.Body.String(), "event: update") || !strings.Contains(eventReplay.Body.String(), `"type":"done"`) {
		t.Fatalf("SSE replay: %s", eventReplay.Body.String())
	}
	if doJSON(t, router, http.MethodGet, "/api/agent/results/"+output.Results[0].ID, manager, nil).Code != 403 {
		t.Fatal("result privacy failed")
	}
	for _, format := range []string{"xlsx", "html"} {
		exported := doJSON(t, router, http.MethodPost, "/api/agent/runs/"+run.ID+"/exports", admin, map[string]any{"format": format})
		if exported.Code != 201 {
			t.Fatal(exported.Body.String())
		}
		var file struct {
			ID  string
			URL string
		}
		decodeResponse(t, exported, &file)
		if doJSON(t, router, http.MethodGet, file.URL, manager, nil).Code != 403 {
			t.Fatal("export privacy failed")
		}
		download := doJSON(t, router, http.MethodGet, file.URL, admin, nil)
		if download.Code != 200 || download.Body.Len() < 100 {
			t.Fatalf("download %s failed", format)
		}
	}
	if resp := doJSON(t, router, http.MethodDelete, "/api/agent/conversations/"+conv.ID, admin, nil); resp.Code != 204 {
		t.Fatal(resp.Body.String())
	}
	if doJSON(t, router, http.MethodGet, "/api/agent/results/"+output.Results[0].ID, admin, nil).Code != 403 {
		t.Fatal("deleted evidence survives")
	}
}

func TestAgentWebSettingsEncryptedKeyConflictConnectionAndRules(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(browserAgentModelHandler))
	defer model.Close()
	router := newTestRouter(t)
	admin := issueDevToken(t, router, "admin_001", "admin")
	manager := issueDevToken(t, router, "manager_001", "manager")
	response := doJSON(t, router, http.MethodGet, "/api/agent/settings", admin, nil)
	var setting map[string]any
	decodeResponse(t, response, &setting)
	values := map[string]any{"base_url": model.URL, "api_key": "test-secret-never-returned", "model": "qwen3.8-max", "enabled": true, "budget_micro": 300_000_000, "expected_version": setting["version"], "input_price_cny": 12, "output_price_cny": 36}
	save := doJSON(t, router, http.MethodPut, "/api/agent/settings", admin, values)
	if save.Code != 200 || strings.Contains(save.Body.String(), "test-secret-never-returned") || strings.Contains(save.Body.String(), "enc:v1:") {
		t.Fatalf("save/key privacy %s", save.Body.String())
	}
	if resp := doJSON(t, router, http.MethodPut, "/api/agent/settings", admin, values); resp.Code != 409 {
		t.Fatalf("settings conflict %d", resp.Code)
	}
	if resp := doJSON(t, router, http.MethodPut, "/api/agent/settings", manager, values); resp.Code != 403 {
		t.Fatalf("manager config access %d", resp.Code)
	}
	test := doJSON(t, router, http.MethodPost, "/api/agent/settings/test", admin, map[string]any{"base_url": model.URL, "model": "qwen3.8-max"})
	if test.Code != 200 || !strings.Contains(test.Body.String(), `"ok":true`) {
		t.Fatalf("connection test %s", test.Body.String())
	}
	rule := doJSON(t, router, http.MethodPost, "/api/agent/rules", manager, map[string]any{"content": "没有广告成本时说明不能计算 ROI"})
	var proposed struct{ ID int64 }
	decodeResponse(t, rule, &proposed)
	path := fmt.Sprintf("/api/agent/rules/%d/publish", proposed.ID)
	if doJSON(t, router, http.MethodPost, path, manager, map[string]any{}).Code != 403 {
		t.Fatal("manager can publish public rules")
	}
	if response := doJSON(t, router, http.MethodPost, path, admin, map[string]any{}); response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	prefs := doJSON(t, router, http.MethodPut, "/api/agent/preferences", manager, map[string]any{"content": "请先给结论再给依据"})
	if prefs.Code != 200 {
		t.Fatal(prefs.Body.String())
	}
	other := doJSON(t, router, http.MethodGet, "/api/agent/preferences", admin, nil)
	if strings.Contains(other.Body.String(), "请先给结论") {
		t.Fatal("personal preferences leaked")
	}
	var result map[string]any
	if json.Unmarshal(test.Body.Bytes(), &result) != nil {
		t.Fatal("invalid connectivity response")
	}
}
