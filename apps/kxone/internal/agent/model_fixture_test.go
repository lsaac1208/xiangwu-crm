package agent

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"net/http"
	"strings"
	"time"
)

// FixtureModelHandler is only compiled into tests. It exercises the real tool
// loop and reads actual query result IDs, rather than supplying report numbers.
func fixtureModelHandler(w http.ResponseWriter, r *http.Request) {
	var req ModelRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		w.WriteHeader(400)
		return
	}
	if !req.Stream {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"OK"}}],"usage":{"prompt_tokens":30,"completion_tokens":2}}`)
		return
	}
	question := ""
	lastTool := ""
	lastResult := ""
	columns := []string{}
	for _, m := range req.Messages {
		if m.Role == "user" {
			question = m.Content
		}
		if len(m.ToolCalls) > 0 {
			lastTool = m.ToolCalls[len(m.ToolCalls)-1].Function.Name
		}
		if m.Role == "tool" {
			var result Result
			if json.Unmarshal([]byte(m.Content), &result) == nil && result.ID != "" {
				lastResult = result.ID
				columns = result.Columns
			}
		}
	}
	if strings.Contains(question, "模拟失败") {
		w.WriteHeader(429)
		return
	}
	if strings.Contains(question, "慢速") {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	send := func(value any) {
		raw, _ := json.Marshal(value)
		fmt.Fprintf(w, "data: %s\n\n", raw)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	name := ""
	args := ""
	switch lastTool {
	case "":
		name = "describe_data"
		args = "{}"
	case "describe_data":
		name = "query_data"
		args = `{"sql":"SELECT source_channel, COUNT(*) AS lead_count FROM leads GROUP BY source_channel ORDER BY source_channel","limit":100}`
	case "query_data":
		name = "present_result"
		p := map[string]any{"type": "table", "result_id": lastResult, "title": "来源线索数量"}
		if strings.Contains(question, "图表") && len(columns) > 1 {
			p["type"] = "chart"
			p["chart_type"] = "bar"
			p["x_column"] = columns[0]
			p["y_columns"] = []string{columns[1]}
		}
		raw, _ := json.Marshal(p)
		args = string(raw)
	}
	if name != "" {
		calls := []any{map[string]any{"index": 0, "id": uuid.NewString(), "type": "function", "function": map[string]any{"name": name, "arguments": args}}}
		if name == "present_result" {
			raw, _ := json.Marshal(map[string]any{"result_ids": []string{lastResult}})
			calls = append(calls, map[string]any{"index": 1, "id": uuid.NewString(), "type": "function", "function": map[string]any{"name": "select_final_results", "arguments": string(raw)}})
		}
		send(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": calls}}}})
	} else {
		for _, text := range []string{"已核对本轮数据。", "请展开结果查看来源数量。"} {
			send(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": text}}}})
		}
	}
	send(map[string]any{"choices": []any{}, "usage": map[string]any{"prompt_tokens": 500, "completion_tokens": 80}})
	fmt.Fprint(w, "data: [DONE]\n\n")
}
