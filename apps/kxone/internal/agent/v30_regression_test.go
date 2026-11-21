package agent

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

// v30 A21-2: “客户#1（两条跟进…）和客户#2（一条跟进…）” over three referenced
// follow-up rows was rejected for “一条”. Per-lead referenced row counts bind.
func TestV30PerLeadReferencedRowCountsAreBound(t *testing.T) {
	notes := &Result{ID: "notes", Columns: []string{"lead_id", "text"}, Rows: [][]any{{int64(1), "费用需商量"}, {int64(1), "费用已说明"}, {int64(2), "担心费用"}}, TotalRows: 3}
	results := map[string]*Result{notes.ID: notes}
	reads := newEvidenceReadState()
	page, _ := modelResultPage(notes, 0, 20)
	reads.observe(page)
	refs := []AnswerReference{}
	for i := 0; i < 3; i++ {
		refs = append(refs, AnswerReference{ResultID: notes.ID, Row: intPointer(i), Column: "text", Level: "cell"})
	}
	req := FinalizeRequest{ResultIDs: []string{notes.ID}, Markdown: "提到费用的线索涉及客户#1（两条跟进）和客户#2（一条跟进），现有材料无法证明费用造成流失。", References: refs}
	if _, _, err := ValidateAnswer(req, req.ResultIDs, results, reads); err != nil {
		t.Fatalf("per-lead counts rejected: %v", err)
	}
	req.Markdown = "客户#2（四条跟进）。"
	if _, _, err := ValidateAnswer(req, req.ResultIDs, results, reads); err == nil {
		t.Fatal("unprovable count accepted")
	}
	// Negative causal assertions remain rejected (policy unchanged).
	req.Markdown = "客户#1已成交，说明费用并未导致其流失。"
	if _, _, err := ValidateAnswer(req, req.ResultIDs, results, reads); err == nil {
		t.Fatal("causal assertion accepted")
	}
}

// v30 A21-2: finalize_answer without result_ids was rejected procedurally and
// consumed the single correction. With no earlier selection, the server now
// adopts exactly the referenced real results and completes in one call.
func TestV30FinalizeWithoutResultIDsAdoptsReferencedResults(t *testing.T) {
	var calls atomic.Int32
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var req ModelRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch calls.Add(1) {
		case 1:
			finalResultFixtureReply(w, "", finalResultTool("query_data", map[string]any{"sql": "SELECT 0 AS current_followup_eligible"}))
		case 2:
			var id string
			for _, m := range req.Messages {
				var result Result
				if m.Role == "tool" && json.Unmarshal([]byte(m.Content), &result) == nil && result.ID != "" {
					id = result.ID
				}
			}
			finalResultFixtureReply(w, "", finalResultTool("finalize_answer", map[string]any{"markdown": "当前普通待办资格为 0。", "references": []AnswerReference{{ResultID: id, Row: intPointer(0), Column: "current_followup_eligible", Level: "cell"}}}))
		default:
			t.Error("extra model call")
			w.WriteHeader(500)
		}
	})
	c := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "核对当前回访资格", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	done := securityWaitRun(t, f, run.ID)
	if done.Status != "completed" || calls.Load() != 2 {
		t.Fatalf("not completed in one finalize: %+v calls=%d", done, calls.Load())
	}
	var events []Event
	f.db.Where("run_id=? AND type=?", run.ID, "final_results_inferred").Find(&events)
	if len(events) != 1 {
		t.Fatal("inference not recorded")
	}
	// Forged/unknown result IDs are never adopted.
	if ids := referencedResultIDs([]AnswerReference{{ResultID: "forged", Row: intPointer(0), Column: "x"}}, map[string]*Result{}); len(ids) != 0 || strings.Join(ids, "") != "" {
		t.Fatal("forged id adopted")
	}
}
