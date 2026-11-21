package agent

import (
	"strings"
	"testing"
)

// The pinned SQLite planner otherwise inlines the shared entity relation into
// both aggregation branches. Keep source scans bounded without timing-sensitive
// assertions, especially on a busy workstation during the large-data suite.
func TestGroupedMeasureDoesNotRepeatEntityOrPlanSourceScans(t *testing.T) {
	s := measuredFixture(t)
	for _, visibility := range []string{"current", "history"} {
		r, err := s.Measure(t.Context(), MeasureRequest{Source: "lead_followups", GroupBy: "actor_user_id", Visibility: visibility})
		if err != nil {
			t.Fatal(err)
		}
		rows, err := s.db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+r.Query)
		if err != nil {
			t.Fatal(err)
		}
		entities, followups := 0, 0
		var plan []string
		for rows.Next() {
			var id, parent, estimate int
			var detail string
			if err := rows.Scan(&id, &parent, &estimate, &detail); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			plan = append(plan, detail)
			parts := strings.Fields(detail)
			if len(parts) >= 2 && (parts[0] == "SCAN" || parts[0] == "SEARCH") && (parts[1] == "entity_index_history" || parts[1] == "e") {
				entities++
			}
			if strings.HasPrefix(detail, "SCAN lead_followups_history") || strings.HasPrefix(detail, "SEARCH lead_followups_history") {
				followups++
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rows.Close()
		// One source read establishes requested identities, one reads the
		// server-owned historical-plan field; grouping must not repeat either.
		if entities != 1 || followups != 2 {
			t.Fatalf("%s repeated source work: entity=%d followups=%d plan=%v", visibility, entities, followups, plan)
		}
	}
}

func TestMaterializedMeasureEmptySetRemainsZero(t *testing.T) {
	s := measuredFixture(t)
	r, err := s.Measure(t.Context(), MeasureRequest{SQL: "SELECT 'lead_followups' record_type,id record_id FROM lead_followups WHERE 0", GroupBy: "actor_user_id"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 1 {
		t.Fatalf("empty set created groups: %+v", r.Rows)
	}
	for _, value := range r.Rows[0][2:6] {
		if value == nil || integerValue(value) != 0 {
			t.Fatalf("empty metric became unknown or nonzero: %+v", r.Rows)
		}
	}
}
