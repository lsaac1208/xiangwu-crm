package main

// These two test-only fixtures distinguish current business state from dates.
// They never mutate gold, start a provider request, or use production data.
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/lsaac128/kxone/internal/agent"
	"gorm.io/gorm"
)

const liveSemanticGoldSHA256 = "bc3c9cc69c0bda41f2e5589fcc37611f1ffe996d1ef47ddabc309fdc82936c92"

func liveSemanticFilename(dataset string) string {
	switch dataset {
	case "semantic-active-timestamp-v1":
		return "../../docs/Agent-Semantic-Active-Timestamp-Fixture.json"
	case "semantic-missing-timestamp-v1":
		return "../../docs/Agent-Semantic-Missing-Timestamp-Fixture.json"
	default:
		return ""
	}
}

func loadLiveSemanticFixture(t *testing.T, dataset string) (liveFixture, string) {
	t.Helper()
	base, err := os.ReadFile("../../docs/Agent-Evaluation-Fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	variant, err := os.ReadFile(liveSemanticFilename(dataset))
	if err != nil {
		t.Fatal(err)
	}
	fixture, hash, err := validateLiveSemanticFixture(base, variant, dataset)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, hash
}

func validateLiveSemanticFixture(baseRaw, variantRaw []byte, dataset string) (liveFixture, string, error) {
	reject := func() (liveFixture, string, error) {
		return liveFixture{}, "", errors.New("semantic fixture differs from its pinned single-field variant; no cloud call allowed")
	}
	baseHash := sha256.Sum256(baseRaw)
	if liveSemanticFilename(dataset) == "" || hex.EncodeToString(baseHash[:]) != liveSemanticGoldSHA256 {
		return reject()
	}
	var base, variant map[string]any
	var fixture liveFixture
	if json.Unmarshal(baseRaw, &base) != nil || json.Unmarshal(variantRaw, &variant) != nil || json.Unmarshal(variantRaw, &fixture) != nil {
		return reject()
	}
	if fixture.Version != "agent-"+dataset || fixture.Dataset != dataset || fixture.BaseFixtureHash != liveSemanticGoldSHA256 {
		return reject()
	}
	targetID, timestampCount, orCount := float64(2), float64(3), float64(3)
	var before any
	var after any = "2026-09-02T12:00:00+08:00"
	if dataset == "semantic-missing-timestamp-v1" {
		targetID, timestampCount, orCount = 3, 1, 2
		before = "2026-09-06T16:00:00+08:00"
		after = nil
	}
	semantic, ok := variant["semantic_variant"].(map[string]any)
	if !ok {
		return reject()
	}
	change, ok := semantic["only_business_change"].(map[string]any)
	if !ok || !reflect.DeepEqual(change, map[string]any{"lead_id": targetID, "field": "converted_at", "before": before, "after": after}) {
		return reject()
	}
	expected, ok := semantic["expected"].(map[string]any)
	if !ok {
		return reject()
	}
	if !reflect.DeepEqual(expected, map[string]any{
		"current_converted_by_status": float64(2), "current_converted_by_result_status": float64(2),
		"total_leads": float64(7), "nonempty_converted_at": timestampCount, "status_or_nonempty_converted_at": orCount,
		"current_conversion_rate_percent": float64(200.0 / 7),
		"source_current_converted":        map[string]any{"good_school": float64(1), "staff_d_douyin": float64(1)},
	}) {
		return reject()
	}
	fixture.SemanticExpected = expected
	tables, ok := variant["tables"].(map[string]any)
	if !ok {
		return reject()
	}
	leads, ok := tables["leads"].([]any)
	if !ok {
		return reject()
	}
	changes := 0
	for _, item := range leads {
		row, ok := item.(map[string]any)
		if !ok {
			return reject()
		}
		if row["id"] == targetID {
			value, present := row["converted_at"]
			if !present || value != after {
				return reject()
			}
			changes++
			if before == nil {
				delete(row, "converted_at")
			} else {
				row["converted_at"] = before
			}
		}
	}
	if changes != 1 {
		return reject()
	}
	variant["version"] = base["version"]
	for _, key := range []string{"dataset", "base_fixture_sha256", "semantic_variant"} {
		delete(variant, key)
	}
	// Check the complete original document, including fields not consumed by
	// liveFixture, so adding/removing unrelated business facts cannot go unnoticed.
	if !reflect.DeepEqual(base, variant) {
		return reject()
	}
	hash := sha256.Sum256(variantRaw)
	return fixture, hex.EncodeToString(hash[:]), nil
}

func TestAgentLiveSemanticFixturesIndependentExpectations(t *testing.T) {
	for _, dataset := range []string{"semantic-active-timestamp-v1", "semantic-missing-timestamp-v1"} {
		t.Run(dataset, func(t *testing.T) {
			fixture, _ := loadLiveSemanticFixture(t, dataset)
			var db *gorm.DB
			newTestRouterWithOptions(t, testRouterOptions{agentFixtureDB: &db, agentPrepareFixture: func(t *testing.T, db *gorm.DB) { importLiveFixture(t, db, fixture, 1) }})
			var got struct{ Total, ByStatus, ByResultStatus, ByTimestamp, ByEither, StateConflict int64 }
			query := `SELECT count(*) AS total,sum(status='converted') AS by_status,sum(result_status='converted') AS by_result_status,sum(converted_at IS NOT NULL) AS by_timestamp,sum(status='converted' OR converted_at IS NOT NULL) AS by_either,sum(result_status<>'' AND ((status='converted')<>(result_status='converted'))) AS state_conflict FROM leads`
			if err := db.Raw(query).Scan(&got).Error; err != nil {
				t.Fatal(err)
			}
			if got.Total != 7 || got.ByStatus != 2 || got.ByResultStatus != 2 || got.StateConflict != 0 || got.ByTimestamp != int64(fixture.SemanticExpected["nonempty_converted_at"].(float64)) || got.ByEither != int64(fixture.SemanticExpected["status_or_nonempty_converted_at"].(float64)) {
				t.Fatalf("independent semantic oracle mismatch: %+v", got)
			}
			snapshot, err := agent.BuildSnapshot(t.Context(), db, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer snapshot.Close()
			result, err := snapshot.Query(t.Context(), query, nil, 20)
			if err != nil {
				t.Fatal(err)
			}
			want, _ := json.Marshal([][]any{{got.Total, got.ByStatus, got.ByResultStatus, got.ByTimestamp, got.ByEither, got.StateConflict}})
			actual, _ := json.Marshal(result.Rows)
			if string(actual) != string(want) {
				t.Fatalf("snapshot changed date/state distinction: %s != %s", actual, want)
			}
		})
	}
}

func TestAgentLiveSemanticFixtureRejectsUnrelatedChanges(t *testing.T) {
	base, err := os.ReadFile("../../docs/Agent-Evaluation-Fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(liveSemanticFilename("semantic-active-timestamp-v1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(map[string]any){
		func(v map[string]any) { v["base_fixture_sha256"] = "changed" },
		func(v map[string]any) {
			v["tables"].(map[string]any)["leads"].([]any)[0].(map[string]any)["deal_amount"] = 10001
		},
		func(v map[string]any) {
			v["tables"].(map[string]any)["leads"].([]any)[1].(map[string]any)["converted_at"] = nil
		},
		func(v map[string]any) {
			v["semantic_variant"].(map[string]any)["expected"].(map[string]any)["nonempty_converted_at"] = 2
		},
	} {
		var v map[string]any
		_ = json.Unmarshal(raw, &v)
		mutate(v)
		changed, _ := json.Marshal(v)
		if _, _, err := validateLiveSemanticFixture(base, changed, "semantic-active-timestamp-v1"); err == nil {
			t.Fatal("unrelated or misdeclared variant accepted")
		}
	}
	if _, _, err := validateLiveSemanticFixture(append(base, ' '), raw, "semantic-active-timestamp-v1"); err == nil {
		t.Fatal("changed gold content hash accepted")
	}
}
