package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
)

func TestMeasuredFactCannotBeRenamedBySurroundingProse(t *testing.T) {
	s := measuredFixture(t)
	r, err := s.Measure(t.Context(), MeasureRequest{Source: "lead_followups"})
	if err != nil {
		t.Fatal(err)
	}
	ref := AnswerReference{Token: "n", FactID: r.Facts[0].ID, Level: "global"}
	for _, text := range []string{
		"客户#2名下确有 {{n}} 条历史跟进计划记录。",
		"历史回访计划：{{n}}。",
		"{{n}} 个预约。",
		"**历史计划**\n{{n}}",
	} {
		if _, _, err := ValidateAnswer(FinalizeRequest{Markdown: text, References: []AnswerReference{ref}}, []string{r.ID}, map[string]*Result{r.ID: r}); err == nil {
			t.Errorf("record fact was renamed: %s", text)
		}
	}
}

func TestChineseQuantitiesIncludeTasksAndOtherEntityUnits(t *testing.T) {
	for _, text := range []string{"普通待办为零项。", "有两笔退款。", "共三份记录。", "涉及四户家庭。", "共一组名单。"} {
		if err := validateNarrative(text); err == nil {
			t.Errorf("unbound quantity accepted: %s", text)
		}
	}
}

func TestSQLCellCannotRenameRecordCountAsHistoricalPlans(t *testing.T) {
	s := measuredFixture(t)
	for _, sql := range []string{
		`SELECT COUNT(*) AS record_count FROM lead_followups WHERE lead_id=1`,
		`SELECT CAST(COUNT(*) AS TEXT) AS record_count FROM lead_followups WHERE lead_id=1`,
	} {
		r, err := s.Query(t.Context(), sql, nil, 1)
		if err != nil {
			t.Fatal(err)
		}
		ref := AnswerReference{Token: "n", ResultID: r.ID, Row: intPointer(0), Column: "record_count", Level: "cell"}
		for _, text := range []string{"客户#1有 {{n}} 条历史跟进计划。", "历史计划：{{n}}。", "{{n}} 个预约。"} {
			if _, _, err := ValidateAnswer(FinalizeRequest{Markdown: text, References: []AnswerReference{ref}}, []string{r.ID}, map[string]*Result{r.ID: r}); err == nil {
				t.Errorf("SQL cell was renamed: %s", text)
			}
		}
		for _, markdown := range []string{"已核对查询结果。", "{{n}}。\n\n已核对查询结果。"} {
			out, verification, err := ValidateAnswer(FinalizeRequest{Markdown: markdown, References: []AnswerReference{ref}}, []string{r.ID}, map[string]*Result{r.ID: r})
			if err != nil || !strings.Contains(out, "查询指标 record_count：3") || verification.Version != 6 {
				t.Fatalf("safe complete cell statement rejected: %s %v", out, err)
			}
		}
	}
}

func TestFollowupPlansAreVerifiedSeparatelyFromFollowupRecords(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	date := time.Date(2026, 9, 3, 1, 0, 0, 0, time.UTC)
	for i := int64(1); i <= 3; i++ {
		row := repo.LeadFollowupModel{ID: i, LeadID: 1, SalesUserID: "sales-a", CreatedAt: date}
		if i <= 2 {
			row.NextFollowupAt = &date
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	s, err := BuildSnapshot(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.Measure(t.Context(), MeasureRequest{SQL: `SELECT 'lead_followups' record_type,id record_id,'historical_plan' AS record_label FROM lead_followups WHERE lead_id=1`})
	if err != nil {
		t.Fatal(err)
	}
	metrics := map[string]int64{}
	for _, f := range r.Facts {
		metrics[f.Metric] = f.Value
	}
	if metrics["distinct_records"] != 3 || metrics["historical_followup_plan_records"] != 2 {
		t.Fatalf("records incorrectly became plans: %+v", metrics)
	}
	refs := []AnswerReference{}
	for i, f := range r.Facts {
		if f.Metric == "distinct_records" || f.Metric == "historical_followup_plan_records" {
			refs = append(refs, AnswerReference{Token: string(rune('a' + i)), FactID: f.ID, Level: "global"})
		}
	}
	out, _, err := ValidateAnswer(FinalizeRequest{Markdown: "历史计划和当前待办采用不同口径。", References: refs}, []string{r.ID}, map[string]*Result{r.ID: r})
	if err != nil || !strings.Contains(out, "跟进记录数：3") || !strings.Contains(out, "有历史回访计划的跟进记录数：2") {
		t.Fatalf("typed facts not rendered independently: %s %v", out, err)
	}
}

func TestTypedMetricsRespectMixedSetsAndDeletedVisibility(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	createSnapshotLead(t, db, 2)
	date := time.Now().UTC()
	if err := db.Model(&repo.LeadModel{}).Where("id=2").Update("deleted_at", date).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []repo.LeadFollowupModel{
		{ID: 1, LeadID: 1, NextFollowupAt: &date},
		{ID: 2, LeadID: 1},
		{ID: 3, LeadID: 2, NextFollowupAt: &date},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	s, err := BuildSnapshot(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for visibility, want := range map[string]int64{"current": 1, "history": 2} {
		r, err := s.Measure(t.Context(), MeasureRequest{Source: "lead_followups", Visibility: visibility})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, f := range r.Facts {
			if f.Metric == "historical_followup_plan_records" {
				found = true
				if f.Value != want {
					t.Fatalf("plan visibility=%s got=%d want=%d", visibility, f.Value, want)
				}
			}
		}
		if !found {
			t.Fatalf("typed plan metric missing for %s", visibility)
		}
	}
	r, err := s.Measure(t.Context(), MeasureRequest{SQL: `SELECT 'leads' record_type,id record_id FROM leads WHERE id=1 UNION ALL SELECT 'lead_followups',id FROM lead_followups WHERE id=1`})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Facts[0].RecordTypes) != 2 || measuredMetricLabel(r.Facts[0]) != "去重业务记录数" || r.Facts[0].Value != 2 {
		t.Fatalf("mixed sources adopted a single entity name: %+v", r.Facts[0])
	}
	for _, f := range r.Facts {
		if f.Metric == "historical_followup_plan_records" && f.Value != 1 {
			t.Fatalf("mixed source changed plan meaning: %+v", f)
		}
	}
	r, err = s.Measure(t.Context(), MeasureRequest{SQL: `SELECT 'lead_followups' record_type,id record_id FROM lead_followups WHERE id=2`})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.Facts {
		if f.Metric == "historical_followup_plan_records" && f.Value != 0 {
			t.Fatalf("missing plan became a plan: %+v", f)
		}
	}
}

func TestQuantityAndEntityErrorsShareTheSingleCorrection(t *testing.T) {
	s := measuredFixture(t)
	r, err := s.Measure(t.Context(), MeasureRequest{Source: "lead_followups"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = ValidateAnswer(FinalizeRequest{Markdown: "客户#2有 {{n}} 条计划，普通待办零项。", References: []AnswerReference{{Token: "n", FactID: r.Facts[0].ID, Level: "global"}}}, []string{r.ID}, map[string]*Result{r.ID: r})
	if err == nil || !strings.Contains(err.Error(), "零项") || !strings.Contains(err.Error(), "独立成段") {
		t.Fatalf("single correction lost one defect: %v", err)
	}
}
