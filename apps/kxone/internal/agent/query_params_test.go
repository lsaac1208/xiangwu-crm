package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
)

func savedParams(t *testing.T, result Result) any {
	t.Helper()
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err = json.Unmarshal(encoded, &data); err != nil {
		t.Fatal(err)
	}
	params, exists := data["query_params"]
	if !exists {
		t.Fatal("saved result omits executed query parameters")
	}
	return params
}

func TestSnapshotSavedQueryParametersSurviveMaterializationAndStorage(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	snapshot, err := BuildSnapshot(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	query := "SELECT id,customer_ref FROM leads WHERE id=? AND customer_ref=?"
	params := []any{int64(1), "张小明"}
	preview, err := snapshot.Query(context.Background(), query, params, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Rows) != 1 {
		t.Fatalf("sanitized name did not identify the expected lead: %+v", preview)
	}
	want := []any{float64(1), "客户#1"}
	if got := savedParams(t, *preview); !reflect.DeepEqual(got, want) {
		t.Fatalf("query params = %#v; want %#v", got, want)
	}
	params[0], params[1] = int64(999), "changed after query"
	full, err := snapshot.MaterializeResult(context.Background(), preview, 100000)
	if err != nil {
		t.Fatal(err)
	}
	if got := savedParams(t, *full); !reflect.DeepEqual(got, want) {
		t.Fatalf("materialization changed params: %#v", got)
	}
	if err = db.AutoMigrate(&ResultRecord{}); err != nil {
		t.Fatal(err)
	}
	service := &Service{db: db}
	if err = service.saveResult(context.Background(), "saved-run", full); err != nil {
		t.Fatal(err)
	}
	if err = snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	loaded, err := service.results(context.Background(), "saved-run")
	if err != nil || len(loaded) != 1 {
		t.Fatalf("load persisted result: %v %#v", err, loaded)
	}
	page := resultPage(loaded[0], 0, 20)
	if page.Query != query || !reflect.DeepEqual(savedParams(t, page), want) {
		t.Fatalf("saved query lost its binding: %+v", page)
	}
	encoded, _ := json.Marshal(page)
	if strings.Contains(string(encoded), "张小明") {
		t.Fatal("raw customer name persisted in query parameters")
	}
}

func TestSnapshotSearchPreservesKeywordPersonAndShanghaiDateBindings(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	when := time.Date(2026, 10, 8, 2, 0, 0, 0, time.UTC)
	if err := db.Create(&repo.LeadFollowupModel{ID: 1, LeadID: 1, SalesUserID: "inviter-a", BusinessStage: "invitation", Remark: "费用需要商量", IsValid: true, CreatedAt: when}).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err := BuildSnapshot(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	result, err := snapshot.Search(context.Background(), SearchRequest{Keyword: "费用", LeadID: 1, ActorID: "inviter-a", Stage: "invitation", From: "2026-10-08", To: "2026-10-08", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	want := []any{"%费用%", float64(1), "inviter-a", "invitation", "2026-10-07T16:00:00.000000000Z", "2026-10-08T15:59:59.999999999Z"}
	if got := savedParams(t, *result); !reflect.DeepEqual(got, want) {
		t.Fatalf("search bindings = %#v; want %#v", got, want)
	}
	if result.TotalRows != 1 || len(result.Sources) != 1 {
		t.Fatalf("search evidence unavailable: %+v", result)
	}
}

func TestSnapshotQueryParametersDistinguishNoBindingsFromLegacyUnknown(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	snapshot, err := BuildSnapshot(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	result, err := snapshot.Query(context.Background(), "SELECT COUNT(*) FROM leads", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if got := savedParams(t, *result); !reflect.DeepEqual(got, []any{}) {
		t.Fatalf("a known unparameterized query must save []: %#v", got)
	}
	var legacy Result
	if err = json.Unmarshal([]byte(`{"id":"old","query":"SELECT id FROM leads WHERE id=?"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if got := savedParams(t, legacy); got != nil {
		t.Fatalf("historical missing bindings must stay unknown: %#v", got)
	}
}

func TestQueryParameterBusinessVocabularyKeepsConfirmedCustomerNamesPrivate(t *testing.T) {
	r := newRedactor()
	r.prepare()
	for _, word := range []string{"费用", "收费", "学费"} {
		if clean, excluded := r.sanitize(word); clean != word || excluded != 0 {
			t.Fatalf("known business word was obscured: %q -> %q (%d)", word, clean, excluded)
		}
	}
	for _, name := range []string{"费小明", "费大勇", "陈大明"} {
		if clean, excluded := r.sanitize(name); strings.Contains(clean, name) || excluded == 0 {
			t.Fatalf("unknown possible name was exposed: %q -> %q", name, clean)
		}
	}
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	if err := db.Table("leads").Where("id=1").Update("customer_name", "费用").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&repo.LeadActivityRecordModel{ID: 1, LeadID: 1, ActivityType: "customer_profile_updated", FromValueJSON: `{"customer_name":"学费"}`, ToValueJSON: `{"customer_name":"费用"}`, CreatedAt: time.Date(2026, 10, 8, 1, 0, 0, 0, time.UTC)}).Error; err != nil {
		t.Fatal(err)
	}
	snapshot, err := BuildSnapshot(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	for _, name := range []string{"费用", "学费"} {
		result, err := snapshot.Query(context.Background(), "SELECT id,customer_ref FROM leads WHERE customer_ref=?", []any{name}, 20)
		if err != nil {
			t.Fatal(err)
		}
		if result.TotalRows != 1 || !reflect.DeepEqual(savedParams(t, *result), []any{"客户#1"}) {
			t.Fatalf("confirmed customer name/alias lost privacy precedence: %+v", result)
		}
	}
}
