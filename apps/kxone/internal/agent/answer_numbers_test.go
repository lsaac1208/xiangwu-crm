package agent

import (
	"strings"
	"testing"
)

// Real v28 A13-1 (run c7d3831a, model call 6) proposal, verbatim prose. It
// referenced current_followup_eligible=0 and two next_followup_at rows, yet was
// rejected for unbound "2" and "0". v27 A13-1 failed identically on "0".
func TestV28A13LiteralsEqualToReferencedCellsAreBound(t *testing.T) {
	lead := &Result{ID: "lead", Columns: []string{"current_followup_eligible"}, Rows: [][]any{{int64(0)}}, TotalRows: 1}
	plans := &Result{ID: "plans", Columns: []string{"next_followup_at_shanghai"}, Rows: [][]any{{"2026-10-09 10:00"}, {"2026-10-11 15:00"}}, TotalRows: 2}
	results := map[string]*Result{lead.ID: lead, plans.ID: plans}
	reads := newEvidenceReadState()
	for _, r := range []*Result{lead, plans} {
		page, _ := modelResultPage(r, 0, 20)
		reads.observe(page)
	}
	refs := []AnswerReference{
		{ResultID: lead.ID, Row: intPointer(0), Column: "current_followup_eligible", Level: "cell"},
		{ResultID: plans.ID, Row: intPointer(0), Column: "next_followup_at_shanghai", Level: "cell"},
		{ResultID: plans.ID, Row: intPointer(1), Column: "next_followup_at_shanghai", Level: "cell"},
	}
	req := FinalizeRequest{ResultIDs: []string{lead.ID, plans.ID}, Markdown: "客户#2 有 2 条历史回访计划（跟进记录中 next_followup_at 非空），但历史计划条数不等于当前普通待办数。当前普通待办以线索的当前资格与截止时间判断，客户#2 的 current_followup_eligible 为 0，即当前不算多项普通待办（当前无普通回访待办）。历史计划是历史记录，不构成新的独立待办。", References: refs}
	if _, _, err := ValidateAnswer(req, req.ResultIDs, results, reads); err != nil {
		t.Fatalf("v28 A13 answer still rejected: %v", err)
	}
	for _, bad := range []string{"客户#2 有 3 条历史回访计划。", "current_followup_eligible 为 5。", "共 2 元。", "占 2%。", "有两百条。"} {
		r := req
		r.Markdown = bad
		_, _, err := ValidateAnswer(r, r.ResultIDs, results, reads)
		if err == nil || !strings.Contains(err.Error(), "未绑定数量") {
			t.Errorf("mismatched literal accepted: %q (%v)", bad, err)
		}
	}
	r := req
	r.Markdown = "客户#2 有两条历史回访计划。"
	if _, _, err := ValidateAnswer(r, r.ResultIDs, results, reads); err != nil {
		t.Fatalf("两条 equal to referenced rows rejected: %v", err)
	}
	unread := newEvidenceReadState()
	if _, _, err := ValidateAnswer(r, r.ResultIDs, results, unread); err == nil {
		t.Fatal("binding from unread evidence accepted")
	}
}
