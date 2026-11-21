package agent

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
)

func readRecordCoverage(t *testing.T, result *Result) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(result)
	var output map[string]any
	if err := json.Unmarshal(raw, &output); err != nil {
		t.Fatal(err)
	}
	coverage, _ := output["record_coverage"].(map[string]any)
	return coverage
}

func TestRecordCoverageCountsRecordsSeparatelyFromLeadsAndPages(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	createSnapshotLead(t, db, 2)
	for i, leadID := range []int64{1, 1, 2} {
		row := repo.LeadFollowupModel{ID: int64(i + 1), LeadID: leadID, SalesUserID: "coverage-worker", BusinessStage: "invitation", ActorRole: "inviter", Remark: "费用需要确认", IsValid: true, CreatedAt: time.Date(2026, 10, 8, 2, i, 0, 0, time.UTC)}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := BuildSnapshot(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	page, err := snapshot.Search(t.Context(), SearchRequest{ActorID: "coverage-worker", Limit: 2, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	coverage := readRecordCoverage(t, page)
	if coverage == nil || coverage["distinct_records"] != float64(2) || coverage["distinct_leads"] != float64(1) || coverage["complete_query_output"] != false || coverage["scope"] != "returned_rows" {
		t.Fatalf("page record/lead coverage is missing or conflated: %#v", coverage)
	}
	full, err := snapshot.MaterializeResult(t.Context(), page, 100)
	if err != nil {
		t.Fatal(err)
	}
	coverage = readRecordCoverage(t, full)
	if coverage == nil || coverage["distinct_records"] != float64(3) || coverage["distinct_leads"] != float64(2) || coverage["complete_query_output"] != true {
		t.Fatalf("materialized coverage: %#v", coverage)
	}
	byType, _ := coverage["records_by_type"].(map[string]any)
	if byType["lead_followups"] != float64(3) {
		t.Fatalf("wrong type coverage: %#v", byType)
	}
	storedPage := resultPage(*full, 1, 2)
	coverage = readRecordCoverage(t, &storedPage)
	if coverage["distinct_records"] != float64(2) || coverage["distinct_leads"] != float64(1) || coverage["complete_query_output"] != false {
		t.Fatalf("saved page retained full coverage: %#v", coverage)
	}
	for _, sql := range []string{"SELECT COUNT(*) AS n FROM lead_followups", "SELECT 'lead_followups' AS record_type,id AS record_id,lead_id FROM lead_followups", "SELECT 0 AS lead_id"} {
		result, err := snapshot.Query(t.Context(), sql, nil, 100)
		if err != nil {
			t.Fatal(err)
		}
		if got := readRecordCoverage(t, result); got != nil {
			t.Fatalf("arbitrary SQL cannot authenticate record lineage: %s %#v", sql, got)
		}
	}
	empty, err := snapshot.Search(t.Context(), SearchRequest{ActorID: "not-found"})
	if err != nil {
		t.Fatal(err)
	}
	coverage = readRecordCoverage(t, empty)
	if coverage["distinct_records"] != float64(0) || coverage["complete_query_output"] != true {
		t.Fatalf("verified empty search must distinguish zero from unknown: %#v", coverage)
	}
}
