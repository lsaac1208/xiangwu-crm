package main

import (
	"reflect"
	"testing"

	"github.com/lsaac128/kxone/internal/agent"
	"gorm.io/gorm"
)

// Read the unchanged gold fixture through the same production schema and
// snapshot used by cloud acceptance; this test makes no provider requests.
func TestAgentLiveFixtureRecordActors(t *testing.T) {
	t.Setenv("KXONE_AGENT_LIVE_FIXTURE", "")
	fixture, _ := loadLiveFixture(t)
	var db *gorm.DB
	newTestRouterWithOptions(t, testRouterOptions{agentFixtureDB: &db, agentPrepareFixture: func(t *testing.T, db *gorm.DB) { importLiveFixture(t, db, fixture, 1) }})
	snapshot, err := agent.BuildSnapshot(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	checks := []struct {
		query string
		want  [][]any
	}{
		{"SELECT record_id,actor_user_id FROM analysis_records WHERE record_type='lead_dispatch_records' ORDER BY record_id", [][]any{{int64(1), "manager-a"}, {int64(2), "manager-b"}, {int64(3), "manager-a"}}},
		{"SELECT record_id,actor_user_id FROM analysis_records WHERE record_type='lead_followups' AND lead_id=1 ORDER BY record_id", [][]any{{int64(1), "sales-a"}, {int64(2), "sales-a"}, {int64(3), "sales-b"}}},
		{"SELECT record_id,actor_user_id FROM analysis_records WHERE record_type='lead_appointments' ORDER BY record_id", [][]any{{int64(11), ""}, {int64(12), ""}, {int64(13), ""}}},
		{"SELECT record_id,actor_user_id FROM analysis_records WHERE record_type='lead_reminder_records' ORDER BY record_id", [][]any{{int64(1), ""}, {int64(2), ""}}},
	}
	for _, check := range checks {
		result, err := snapshot.Query(t.Context(), check.query, nil, 100)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(result.Rows, check.want) {
			t.Errorf("query=%s\ngot=%#v\nwant=%#v", check.query, result.Rows, check.want)
		}
	}
	search, err := snapshot.Search(t.Context(), agent.SearchRequest{ActorID: "manager-a", LeadID: 1, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, source := range search.Sources {
		if source.Type == "lead_dispatch_records" && source.ID == "1" {
			found = true
		}
	}
	if !found {
		t.Fatal("actual manager's first dispatch is missing from operator-filtered evidence")
	}
}
