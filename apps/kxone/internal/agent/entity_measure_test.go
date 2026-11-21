package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
)

func intPointer(n int) *int { return &n }

func measuredFixture(t *testing.T) *Snapshot {
	t.Helper()
	db := snapshotFixtureDB(t)
	for id := int64(1); id <= 3; id++ {
		createSnapshotLead(t, db, id)
	}
	for i, lead := range []int64{1, 1, 2, 2, 3, 3, 1} {
		actor := "sales-a"
		if i == 6 {
			actor = "sales-b"
		}
		if err := db.Create(&repo.LeadFollowupModel{ID: int64(i + 1), LeadID: lead, SalesUserID: actor, CreatedAt: time.Now().UTC()}).Error; err != nil {
			t.Fatal(err)
		}
	}
	s, err := BuildSnapshot(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMeasuredGlobalIsUnionNotSumOfGroupedDistinct(t *testing.T) {
	s := measuredFixture(t)
	r, err := s.Measure(t.Context(), MeasureRequest{Source: "lead_followups", GroupBy: "actor_user_id"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]int64{"global:": {7, 3}, "group:sales-a": {6, 3}, "group:sales-b": {1, 1}}
	for _, row := range r.Rows {
		key := fmt.Sprint(row[0]) + ":" + fmt.Sprint(row[1])
		v := want[key]
		if len(v) == 0 || integerValue(row[2]) != v[0] || integerValue(row[3]) != v[1] {
			t.Fatalf("unexpected independently expected row %v", row)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("missing rows %v", want)
	}
	for _, f := range r.Facts {
		if f.Metric == "distinct_leads" && f.Additive {
			t.Fatal("distinct lead count declared additive")
		}
	}
	if r.MeasureScope.Kind != "whole_source" {
		t.Fatal("server-owned unsliced source not identified")
	}
	if err := ValidateChart(Block{Type: "chart", ChartType: "pie", XColumn: "group_value", YColumns: []string{"distinct_leads"}}, *r); err == nil {
		t.Fatal("mixed overall/group counts accepted as a pie dataset")
	}
}

func TestMeasureDeduplicatesProjectionsAndDoesNotTrustAliases(t *testing.T) {
	s := measuredFixture(t)
	r, err := s.Measure(t.Context(), MeasureRequest{SQL: `SELECT 'lead_followups' record_type,id record_id,999 lead_id FROM lead_followups UNION ALL SELECT record_type,record_id,999 FROM analysis_records WHERE record_type='lead_followups'`})
	if err != nil || integerValue(r.Rows[0][2]) != 7 || integerValue(r.Rows[0][3]) != 3 {
		t.Fatalf("duplicated projection/forged alias changed counts: %v %v", r, err)
	}
	if r.MeasureScope.Kind != "query_set" {
		t.Fatal("arbitrary SQL promoted to whole source")
	}
	for _, query := range []string{`SELECT 'lead_followups' record_type,99999 record_id`, `SELECT 'leads' record_type,'1' record_id`, `SELECT 'sqlite_master' record_type,1 record_id`, `SELECT name record_type,1 record_id FROM sqlite_master`, `DELETE FROM leads RETURNING 'leads',id`} {
		if _, err := s.Measure(t.Context(), MeasureRequest{SQL: query}); err == nil {
			t.Fatalf("invalid identity/query accepted: %s", query)
		}
	}
}

func TestMeasureContainsNoTextRowsAndRespectsDeletionVisibility(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	createSnapshotLead(t, db, 2)
	deleted := time.Now().UTC()
	if err := db.Model(&repo.LeadModel{}).Where("id=2").Update("deleted_at", deleted).Error; err != nil {
		t.Fatal(err)
	}
	for i, lead := range []int64{1, 2} {
		if err := db.Create(&repo.LeadFollowupModel{ID: int64(i + 1), LeadID: lead}).Error; err != nil {
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
		if err != nil || integerValue(r.Rows[0][2]) != want {
			t.Fatalf("no-text/deleted rows: %s %+v %v", visibility, r, err)
		}
	}
	if _, err := s.Measure(t.Context(), MeasureRequest{SQL: `SELECT 'lead_followups' record_type,2 record_id`}); err == nil {
		t.Fatal("deleted identity crossed current visibility")
	}
}

func TestEntityIndexUnknownTimeRemainsNull(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	if err := db.Create(&repo.WeComCardTaskModel{ID: 1, LeadID: 1, CardScene: "assign", ReceiverUserID: "sales-a", CardStatus: "sent"}).Error; err != nil {
		t.Fatal(err)
	}
	s, err := BuildSnapshot(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.Query(t.Context(), `SELECT COUNT(*) FROM entity_index WHERE record_type='wecom_card_tasks' AND occurred_date_shanghai IS NULL`, nil, 1)
	if err != nil || integerValue(r.Rows[0][0]) != 1 {
		t.Fatalf("untrusted time became blank/zero calendar value: %+v %v", r, err)
	}
}

func TestValidatedReferencesRejectRawGlobalAndWrongLayer(t *testing.T) {
	s := measuredFixture(t)
	r, err := s.Measure(context.Background(), MeasureRequest{Source: "lead_followups", GroupBy: "actor_user_id"})
	if err != nil {
		t.Fatal(err)
	}
	results := map[string]*Result{r.ID: r}
	var global, group MeasuredFact
	for _, f := range r.Facts {
		if f.Metric == "distinct_leads" {
			if f.Level == "global" {
				global = f
			} else {
				group = f
			}
		}
	}
	req := FinalizeRequest{Markdown: "{{coverage}}。\n\n不能据此证明历史流失原因。", References: []AnswerReference{{Token: "coverage", FactID: global.ID, Level: "global"}}}
	out, verification, err := ValidateAnswer(req, []string{r.ID}, results)
	if err != nil || !strings.Contains(out, "去重线索数：3") || verification.Version != 6 {
		t.Fatalf("valid evidence failed: %s %+v %v", out, verification, err)
	}
	inline := req
	inline.ResultIDs = []string{r.ID}
	if _, _, err := ValidateAnswer(inline, nil, results); err != nil {
		t.Fatalf("inline evidence selection did not share the same checks: %v", err)
	}
	inline.ResultIDs = []string{"external-result"}
	if _, _, err := ValidateAnswer(inline, nil, results); err == nil {
		t.Fatal("inline selection accepted another run result")
	}
	for _, raw := range []string{"覆盖4条线索", "覆盖四条线索", "两组互不重叠", "全库共有七条", "合计{{coverage}}", "|员工|数量|\n|员工乙|6|"} {
		bad := req
		bad.Markdown = raw
		if _, _, err := ValidateAnswer(bad, []string{r.ID}, results); err == nil {
			t.Fatalf("unbound claim published: %s", raw)
		}
	}
	bad := req
	bad.References = []AnswerReference{{Token: "coverage", FactID: group.ID, Level: "global"}}
	if _, _, err := ValidateAnswer(bad, []string{r.ID}, results); err == nil {
		t.Fatal("group fact declared global")
	}
	if _, _, err := ValidateAnswer(req, []string{}, results); err == nil {
		t.Fatal("unselected fact accepted")
	}
	global.SnapshotAt = global.SnapshotAt.Add(time.Second)
	for i := range r.Facts {
		if r.Facts[i].ID == global.ID {
			r.Facts[i] = global
		}
	}
	if _, _, err := ValidateAnswer(req, []string{r.ID}, results); err == nil {
		t.Fatal("cross-snapshot fact accepted")
	}
	encoded, _ := json.Marshal(r)
	if len(encoded) == 0 {
		t.Fatal("facts not serializable")
	}
}
