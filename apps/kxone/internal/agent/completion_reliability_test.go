package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAnswerCorrectionReportsEveryIndependentDefect(t *testing.T) {
	err := validateNarrative("两人合计 {{count}} 条；未匹配人员为 0 条，故所有记录均有姓名。")
	if err == nil {
		t.Fatal("unbound claims accepted")
	}
	// “合计/所有” are no longer narrative errors; ValidateAnswer checks them
	// against result completeness (TestV27OrdinaryTotalityWordAcceptedWithScopeNote).
	for _, want := range []string{"两人", "0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("single correction did not report %q: %v", want, err)
		}
	}
}

func TestCheckpointKeepsObservedEvidenceWithoutReadingHiddenCells(t *testing.T) {
	at := time.Now().UTC()
	r := &Result{ID: "result", Columns: []string{"n", "secret_unread"}, Rows: [][]any{{7, "unread value"}}, TotalRows: 1, SnapshotAt: at,
		Facts: []MeasuredFact{{ID: "known-fact", ResultID: "result", ScopeID: "scope", Level: "global", Metric: "distinct_records", Value: 7, SnapshotAt: at}, {ID: "unread-fact", ResultID: "result"}}}
	state := newEvidenceReadState()
	state.observe(map[string]any{"id": r.ID, "columns": []string{"n"}, "rows": [][]any{{7}}, "offset": 0, "facts": r.Facts[:1]})
	page := state.checkpoint(map[string]*Result{r.ID: r}, 4096)
	raw, _ := json.Marshal(page)
	if !strings.Contains(string(raw), "known-fact") || !strings.Contains(string(raw), `"value":7`) || strings.Contains(string(raw), "unread") {
		t.Fatalf("checkpoint did not preserve exactly observed evidence: %s", raw)
	}
	for i := 0; i < 100; i++ {
		state.observe(map[string]any{"id": r.ID, "columns": []string{"n"}, "rows": [][]any{{strings.Repeat("长", 150)}}, "offset": i})
	}
	page = state.checkpoint(map[string]*Result{r.ID: r}, 4096)
	raw, _ = json.Marshal(page)
	if len(raw) > 4096 || page["omitted_cells"].(int) == 0 {
		t.Fatalf("checkpoint must remain bounded and disclose omissions: %d %v", len(raw), page["omitted_cells"])
	}
}

func TestExecutionAllowanceDoesNotAccumulateAndReservesFinalTool(t *testing.T) {
	history := []ModelMessage{{Role: "system", Content: "rules"}, {Role: "user", Content: "question"}}
	first := withExecutionAllowance(history, 8, 12, false)
	last := withExecutionAllowance(history, 1, 1, true)
	if history[0].Content != "rules" || len(first) != len(history) || len(last) != len(history) || strings.Count(last[0].Content, "剩余模型调用") != 1 {
		t.Fatal("allowance mutated native history")
	}
	limited := toolsForAllowance(1)
	if len(limited) != 1 || limited[0].(map[string]any)["function"].(map[string]any)["name"] != "finalize_answer" {
		t.Fatal("last tool slot was not reserved for publication")
	}
}

func TestFinalTableCannotSmuggleUnselectedOrExternalResult(t *testing.T) {
	results := map[string]*Result{"chosen": {ID: "chosen"}, "abandoned": {ID: "abandoned"}}
	for _, id := range []string{"abandoned", "external"} {
		_, _, err := ValidateAnswer(FinalizeRequest{ResultIDs: []string{"chosen"}, TableResultIDs: []string{id}, Markdown: "已核对。"}, nil, results)
		if err == nil {
			t.Errorf("unselected table %q was accepted", id)
		}
	}
}

func TestExhaustedToolsNeverReserveOrCallAnotherModel(t *testing.T) {
	var calls atomic.Int64
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		finalResultFixtureReply(w, "", finalResultTool("query_data", map[string]any{"sql": "SELECT 1 AS value"}), finalResultTool("query_data", map[string]any{"sql": "SELECT 2 AS value"}))
	})
	f.service.cfg.MaxTools = 2
	c := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "核对数据", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	var reservations int64
	if err := f.db.Model(&BudgetReservation{}).Where("run_id=?", run.ID).Count(&reservations).Error; err != nil {
		t.Fatal(err)
	}
	if finished.Status == "completed" || calls.Load() != 1 || reservations != 1 || len(toolsForAllowance(0)) != 0 {
		t.Fatalf("exhaustion charged another model: run=%s calls=%d reservations=%d", finished.Status, calls.Load(), reservations)
	}
}

func TestCheckpointRetainsPendingFinalCorrectionWithoutExtraAllowance(t *testing.T) {
	const hidden = "CORRECTION_PRIVATE_REASONING_MARKER"
	var calls atomic.Int64
	var resultID string
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var request ModelRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		for _, m := range request.Messages {
			if m.Role == "tool" {
				var result Result
				_ = json.Unmarshal([]byte(m.Content), &result)
				if result.ID != "" {
					resultID = result.ID
				}
			}
		}
		switch calls.Add(1) {
		case 1:
			tool := finalResultTool("query_data", map[string]any{"sql": "SELECT 7 AS checked_value"})
			delta := map[string]any{"reasoning_content": "Check the requested fact.", "tool_calls": []any{map[string]any{"index": 0, "id": tool.ID, "type": "function", "function": tool.Function}}}
			raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
			fmt.Fprintf(w, "data: %s\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":30,\"completion_tokens\":50}}\n\ndata: [DONE]\n\n", raw)
		case 2:
			tool := finalResultTool("finalize_answer", FinalizeRequest{ResultIDs: []string{resultID}, Markdown: "缺失数量为0条。"})
			delta := map[string]any{"reasoning_content": hidden, "content": "UNVERIFIED_DRAFT_CAPACITY_" + strings.Repeat("x", 41000), "tool_calls": []any{map[string]any{"index": 0, "id": tool.ID, "type": "function", "function": tool.Function}}}
			raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
			fmt.Fprintf(w, "data: %s\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":30,\"completion_tokens\":50}}\n\ndata: [DONE]\n\n", raw)
		case 3:
			raw, _ := json.Marshal(request)
			if strings.Contains(string(raw), hidden) || !strings.Contains(request.Messages[0].Content, "纠正已使用=true") || !strings.Contains(request.Messages[0].Content, `未绑定数量 ["0"]`) {
				t.Errorf("checkpoint lost correction or retained hidden reasoning: %s", request.Messages[0].Content)
			}
			finalResultFixtureReply(w, "", finalResultTool("finalize_answer", FinalizeRequest{ResultIDs: []string{resultID}, Markdown: "已核对。\n\n{{n}}。", References: []AnswerReference{{Token: "n", ResultID: resultID, Row: intPointer(0), Column: "checked_value", Level: "cell"}}}))
		default:
			t.Error("correction allowance reset")
			w.WriteHeader(500)
		}
	})
	if err := f.db.Model(&Settings{}).Where("id=1").Update("model", "deepseek-v4-pro").Error; err != nil {
		t.Fatal(err)
	}
	c := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "核对数据", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	var checkpoints, charges int64
	_ = f.db.Model(&Event{}).Where("run_id=? AND type='context_checkpoint'", run.ID).Count(&checkpoints).Error
	_ = f.db.Model(&BudgetReservation{}).Where("run_id=?", run.ID).Count(&charges).Error
	if finished.Status != "completed" || calls.Load() != 3 || charges != 3 || checkpoints != 1 {
		t.Fatalf("correction checkpoint failed: run=%s calls=%d charges=%d checkpoints=%d", finished.Status, calls.Load(), charges, checkpoints)
	}
}

func TestFinalTableSubmissionUsesSameValidatedSelectionAndNoExtraCall(t *testing.T) {
	var calls atomic.Int64
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var request ModelRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		if calls.Add(1) == 1 {
			if !strings.Contains(request.Messages[0].Content, "简明数据目录") {
				t.Error("initial request requires an unnecessary catalog round trip")
			}
			finalResultFixtureReply(w, "", finalResultTool("query_data", map[string]any{"sql": "SELECT COUNT(*) AS checked_count FROM leads"}))
			return
		}
		var id string
		for _, m := range request.Messages {
			if m.Role == "tool" {
				var result Result
				_ = json.Unmarshal([]byte(m.Content), &result)
				if result.ID != "" {
					id = result.ID
				}
			}
		}
		finalResultFixtureReply(w, "", finalResultTool("finalize_answer", FinalizeRequest{ResultIDs: []string{id}, TableResultIDs: []string{id}, Markdown: "查询结果。\n\n{{n}}。", References: []AnswerReference{{Token: "n", ResultID: id, Row: intPointer(0), Column: "checked_count", Level: "cell"}}}))
	})
	conversation := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, conversation.ID, "查询线索并展示表格", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	if finished.Status != "completed" || calls.Load() != 2 {
		t.Fatalf("final table added calls or bypassed completion: %s calls=%d", finished.Status, calls.Load())
	}
	var answer Message
	if err = f.db.Where("run_id=? AND role='assistant'", run.ID).First(&answer).Error; err != nil {
		t.Fatal(err)
	}
	var blocks []Block
	if err = json.Unmarshal([]byte(answer.BlocksJSON), &blocks); err != nil {
		t.Fatal(err)
	}
	if len(blocks) < 2 || blocks[0].Type != "table" || blocks[0].Result == nil {
		t.Fatalf("validated table missing: %+v", blocks)
	}
}

func TestGroupTotalsCannotProveNoMissingEmployeeMappings(t *testing.T) {
	for _, text := range []string{
		"分组相加与全量一致，说明无遗漏或未匹配员工名的记录。",
		"本例中无未匹配记录。",
		"这些记录均能归到上述人员名下。",
	} {
		if err := validateNarrative(text); err == nil {
			t.Errorf("unproved negative accepted: %s", text)
		}
	}
	if err := validateNarrative("分组相加不代表无遗漏，也不能证明没有未匹配员工名的记录。"); err != nil {
		t.Fatal(err)
	}
}

func TestDictionaryPaginationCannotSilentlyReturnCatalog(t *testing.T) {
	db := snapshotFixtureDB(t)
	snapshot, err := BuildSnapshot(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	if _, err = snapshot.DescribePage(DescriptionRequest{Offset: 20}); err == nil {
		t.Fatal("orphan offset silently returned catalog")
	}
	page, err := snapshot.DescribePage(DescriptionRequest{Tables: []string{"leads", "lead_followups", "lead_activity_records"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(page)
	var decoded struct {
		HasMore     bool               `json:"has_more"`
		NextRequest DescriptionRequest `json:"next_request"`
	}
	_ = json.Unmarshal(raw, &decoded)
	if !decoded.HasMore || decoded.NextRequest.Offset == 0 || len(decoded.NextRequest.Tables) != 3 {
		t.Fatalf("pagination lost selector: %s", raw)
	}
}
