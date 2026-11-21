package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func finalResultFixtureReply(w http.ResponseWriter, content string, tools ...ToolCall) {
	w.Header().Set("Content-Type", "text/event-stream")
	delta := map[string]any{"content": content}
	if len(tools) > 0 {
		calls := []any{}
		for i, call := range tools {
			calls = append(calls, map[string]any{"index": i, "id": call.ID, "type": "function", "function": call.Function})
		}
		delta["tool_calls"] = calls
	}
	raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
	fmt.Fprintf(w, "data: %s\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":10}}\n\ndata: [DONE]\n\n", raw)
}

func finalResultTool(name string, args any) ToolCall {
	raw, _ := json.Marshal(args)
	var call ToolCall
	call.ID, call.Type, call.Function.Name, call.Function.Arguments = uuid.NewString(), "function", name, string(raw)
	return call
}

func TestAgentDoesNotCompleteDataAnswerWithoutFinalSelection(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var req ModelRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		queried := false
		for _, message := range req.Messages {
			if message.Role == "tool" {
				var result Result
				_ = json.Unmarshal([]byte(message.Content), &result)
				queried = queried || result.ID != ""
			}
		}
		if !queried {
			finalResultFixtureReply(w, "", finalResultTool("query_data", map[string]any{"sql": "SELECT COUNT(*) AS n FROM leads"}))
			return
		}
		finalResultFixtureReply(w, "已查询，但模型故意未声明最终采用的结果。")
	})
	c := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "请分析", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	if finished.Status == "completed" {
		t.Fatal("data answer completed without selecting final evidence; exploratory results would be ambiguous")
	}
	if !strings.Contains(finished.Error, "最终") {
		t.Fatalf("missing actionable selection error: %+v", finished)
	}
}

func TestAgentFinalSelectionDropsAbandonedPresentationsAndSources(t *testing.T) {
	var oldID, newID string
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var req ModelRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		selected := false
		for _, m := range req.Messages {
			for _, call := range m.ToolCalls {
				selected = selected || call.Function.Name == "select_final_results"
			}
			if m.Role == "tool" {
				var result Result
				if json.Unmarshal([]byte(m.Content), &result) == nil && result.ID != "" {
					if len(result.Rows) > 0 && integerValue(result.Rows[0][0]) == 49 {
						oldID = result.ID
					} else {
						newID = result.ID
					}
				}
			}
		}
		if oldID == "" {
			finalResultFixtureReply(w, "", finalResultTool("query_data", map[string]any{"sql": "SELECT 49 AS n"}), finalResultTool("query_data", map[string]any{"sql": "SELECT 7 AS n"}))
			return
		}
		if !selected {
			finalResultFixtureReply(w, "", finalResultTool("present_result", map[string]any{"type": "table", "result_id": oldID, "title": "弃用的探索"}), finalResultTool("present_result", map[string]any{"type": "table", "result_id": newID, "title": "最终结果"}), finalResultTool("select_final_results", map[string]any{"result_ids": []string{newID}}))
			return
		}
		finalResultFixtureReply(w, "", finalResultTool("finalize_answer", map[string]any{"markdown": "核对后的结果。\n\n{{value}}。\n\n早先探索查询未采用。", "references": []AnswerReference{{Token: "value", ResultID: newID, Row: intPointer(0), Column: "n", Level: "cell"}}}))
	})
	c := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "分析并复核", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	if finished.Status != "completed" {
		t.Fatalf("selection did not complete: %+v", finished)
	}
	var message Message
	if err := f.db.Where("run_id=? AND role='assistant'", run.ID).First(&message).Error; err != nil {
		t.Fatal(err)
	}
	var blocks []Block
	_ = json.Unmarshal([]byte(message.BlocksJSON), &blocks)
	selected := false
	for _, b := range blocks {
		if b.ResultID == oldID {
			t.Fatal("abandoned result remains in final blocks")
		}
		if b.Type == "sources" {
			selected = len(b.ResultIDs) == 1 && b.ResultIDs[0] == newID
		}
	}
	if !selected {
		t.Fatalf("missing explicit final manifest: %s", message.BlocksJSON)
	}
}
