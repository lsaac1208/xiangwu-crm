package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFinalCorrectionIncludesRepeatedTokensAndUnsupportedScopeTogether(t *testing.T) {
	r := &Result{ID: "r", Columns: []string{"employee_name", "followup_count"}, Rows: [][]any{{"员工乙", 6}, {"小宇", 1}}}
	req := FinalizeRequest{Markdown: "已按实际操作人统计。\n\n{{a}}\n\n{{b}}\n\n本批数据中无未匹配记录，全部关联成功。", References: []AnswerReference{
		{Token: "a", ResultID: r.ID, Row: intPointer(0), Column: "followup_count", Level: "cell"},
		{Token: "a", ResultID: r.ID, Row: intPointer(0), Column: "employee_name", Level: "cell"},
		{Token: "b", ResultID: r.ID, Row: intPointer(1), Column: "followup_count", Level: "cell"},
		{Token: "b", ResultID: r.ID, Row: intPointer(1), Column: "employee_name", Level: "cell"},
	}}
	_, _, err := ValidateAnswer(req, []string{r.ID}, map[string]*Result{r.ID: r})
	if err == nil {
		t.Fatal("ambiguous tokens accepted")
	}
	for _, want := range []string{"重复", `"a"`, `"b"`, "无未匹配"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("single correction omitted %q: %v", want, err)
		}
	}
	// A single correction can address every reported defect. The employee
	// labels already come from the referenced rows; no separate name binding.
	req.Markdown = "已按实际操作人统计。"
	req.References = []AnswerReference{req.References[0], req.References[2]}
	for i := range req.References {
		req.References[i].Token = ""
	}
	if _, _, err := ValidateAnswer(req, []string{r.ID}, map[string]*Result{r.ID: r}); err != nil {
		t.Fatalf("one correction should suffice: %v", err)
	}
}

func TestReferencePreflightCollectsStructureNarrativeAndPlacement(t *testing.T) {
	r := &Result{ID: "r", Columns: []string{"count"}, Rows: [][]any{{3}}}
	req := FinalizeRequest{Markdown: "客户#1有 {{valid}} 条历史计划。\n\n{{missing}}\n\n{{not valid}}\n\n全部关联成功，有四条记录。", References: []AnswerReference{
		{Token: "valid", ResultID: r.ID, Row: intPointer(0), Column: "count", Level: "cell"},
		{Token: "not valid", FactID: "f", ResultID: r.ID, Level: "cell"},
		{Token: "valid", ResultID: r.ID, Level: "global"},
	}}
	_, _, err := ValidateAnswer(req, []string{r.ID}, map[string]*Result{r.ID: r})
	if err == nil {
		t.Fatal("invalid contract accepted")
	}
	for _, want := range []string{"token格式", "重复", "混用", "非负row", "事实引用level", "单元格引用level", "missing", "令牌格式", "四条", "独立成段"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("single feedback omitted %q: %v", want, err)
		}
	}
}

func TestAutomaticReferenceTokensCannotCollideOrRepairExplicitDuplicates(t *testing.T) {
	r := &Result{ID: "r", Columns: []string{"count"}, Rows: [][]any{{3}, {4}}}
	cell := func(token string, row int) AnswerReference {
		return AnswerReference{Token: token, ResultID: r.ID, Row: intPointer(row), Column: "count", Level: "cell"}
	}
	req := FinalizeRequest{Markdown: "已核对。", References: []AnswerReference{cell("", 0), cell("reference_1", 1)}}
	out, verification, err := ValidateAnswer(req, []string{r.ID}, map[string]*Result{r.ID: r})
	if err != nil {
		t.Fatal(err)
	}
	if verification.References[0].Token != "reference_2" || verification.References[1].Token != "reference_1" || !strings.Contains(out, "count：3") || !strings.Contains(out, "count：4") {
		t.Fatalf("presentation identity collision: %s %+v", out, verification)
	}
	req.References = []AnswerReference{cell("same", 0), cell("same", 1)}
	if _, _, err := ValidateAnswer(req, []string{r.ID}, map[string]*Result{r.ID: r}); err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("explicit duplicate silently repaired: %v", err)
	}
	req.References = []AnswerReference{cell("", 0)}
	req.References[0].ResultID = "invented"
	if _, _, err := ValidateAnswer(req, []string{r.ID}, map[string]*Result{r.ID: r}); err == nil {
		t.Fatal("automatic token bypassed evidence provenance")
	}
}

func TestFinalizeSchemaMakesExplicitTokensUniqueAndOptional(t *testing.T) {
	raw, err := json.Marshal(agentTools())
	if err != nil {
		t.Fatal(err)
	}
	var tools []struct {
		Function struct {
			Name       string `json:"name"`
			Parameters struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"parameters"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &tools); err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools {
		if tool.Function.Name != "finalize_answer" {
			continue
		}
		var refs struct {
			Items struct {
				Required   []string `json:"required"`
				Properties map[string]struct {
					Pattern     string `json:"pattern"`
					Description string `json:"description"`
				} `json:"properties"`
			} `json:"items"`
		}
		if err := json.Unmarshal(tool.Function.Parameters.Properties["references"], &refs); err != nil {
			t.Fatal(err)
		}
		if refs.Items.Required == nil || len(refs.Items.Required) != 0 || refs.Items.Properties["token"].Pattern != validReferenceToken.String() || !strings.Contains(refs.Items.Properties["token"].Description, "唯一") {
			t.Fatalf("schema contradicts optional unique token contract: %+v", refs)
		}
		return
	}
	t.Fatal("finalize tool missing")
}

func TestReferencesWithoutPlaceholdersCanOmitTokensWithoutGuessingBindings(t *testing.T) {
	r := &Result{ID: "r", Columns: []string{"employee_name", "followup_count"}, Rows: [][]any{{"员工乙", 6}, {"小宇", 1}}}
	req := FinalizeRequest{Markdown: "已按实际操作人统计。", References: []AnswerReference{
		{ResultID: r.ID, Row: intPointer(0), Column: "followup_count", Level: "cell"},
		{ResultID: r.ID, Row: intPointer(1), Column: "followup_count", Level: "cell"},
	}}
	out, verification, err := ValidateAnswer(req, []string{r.ID}, map[string]*Result{r.ID: r})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "followup_count：6") || !strings.Contains(out, "employee_name=员工乙") || !strings.Contains(out, "followup_count：1") || !strings.Contains(out, "employee_name=小宇") || verification.References[0].Token == verification.References[1].Token || verification.References[0].Token == "" {
		t.Fatalf("implicit display lost value/identity: %s %+v", out, verification)
	}
	if req.References[0].Token != "" {
		t.Fatal("input reference slice was mutated")
	}
	req.Markdown = "{{unknown}}"
	if _, _, err := ValidateAnswer(req, []string{r.ID}, map[string]*Result{r.ID: r}); err == nil {
		t.Fatal("missing token guessed an explicit placeholder binding")
	}
}
