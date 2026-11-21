package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lsaac128/kxone/internal/adapter/repo"
)

func TestSnapshotDictionaryCurrentOwnershipCategoriesAreExclusive(t *testing.T) {
	db := snapshotFixtureDB(t)
	rows := []struct{ status, ownership, owner, assigned string }{
		{"following", "private", "employee-a", ""},
		{"converted", "private", "employee-a", ""},
		{"invalid", "private", "employee-b", ""},
		{"assigned_pending_claim", "assigned_pending_claim", "", "employee-b"},
		{"public_pool", "public_pool", "", ""},
		{"pending_dispatch", "pending_assignment", "", ""},
		// A stale owner must not turn a not-yet-claimed assignment into ownership.
		{"assigned_pending_claim", "assigned_pending_claim", "stale-owner", "employee-b"},
	}
	for i, row := range rows {
		createSnapshotLead(t, db, int64(i+1))
		if err := db.Model(&repo.LeadModel{}).Where("id = ?", i+1).Updates(map[string]any{"status": row.status, "ownership_state": row.ownership, "current_owner_user_id": row.owner, "assigned_sales_user_id": row.assigned}).Error; err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := BuildSnapshot(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	definition, ok := snapshot.Describe().(SnapshotDescription).MetricDefinitions["current_ownership_category"]
	if !ok {
		t.Fatal("dictionary omits mutually exclusive ownership definition")
	}
	result, err := snapshot.Query(t.Context(), "SELECT "+definition.ValueSQL+" AS category, COUNT(*) FROM "+definition.Table+" WHERE "+definition.FilterSQL+" GROUP BY category ORDER BY category", nil, 100)
	if err != nil || fmt.Sprint(result.Rows) != "[[owner:employee-a 2] [owner:employee-b 1] [pending_claim 2] [public_pool 1] [unowned 1]]" {
		t.Fatalf("pending recipient, public pool, actual owner and missing owner were conflated: %+v err=%v", result, err)
	}
}

func TestSnapshotDictionaryCanonicalSQLTimeStrictBoundary(t *testing.T) {
	db := snapshotFixtureDB(t)
	boundary := time.Date(2026, 10, 8, 2, 0, 0, 0, time.UTC)
	for i, at := range []time.Time{boundary.Add(-time.Nanosecond), boundary, boundary.Add(time.Nanosecond)} {
		createSnapshotLead(t, db, int64(i+1))
		if err := db.Model(&repo.LeadModel{}).Where("id = ?", i+1).Update("next_followup_due_at", at).Error; err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := BuildSnapshot(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	description := snapshot.Describe().(SnapshotDescription)
	if !strings.Contains(description.FieldMeanings["UTC absolute time columns"], "固定9位") {
		t.Fatal("SQL timestamp comparison contract is missing")
	}
	query := "SELECT id FROM leads WHERE next_followup_due_at < '2026-10-08T02:00:00.000000000Z' ORDER BY id"
	result, err := snapshot.Query(t.Context(), query, nil, 100)
	if err != nil || fmt.Sprint(result.Rows) != "[[1]]" {
		t.Fatalf("strict canonical boundary: %+v err=%v", result, err)
	}
	// Preserve the actual rejected evaluation query as a counterexample. The
	// engine must not silently rewrite caller SQL and hide its semantic error.
	unpadded := "SELECT id FROM leads WHERE next_followup_due_at < '2026-10-08T02:00:00Z' ORDER BY id"
	wrong, err := snapshot.Query(t.Context(), unpadded, nil, 100)
	if err != nil || fmt.Sprint(wrong.Rows) != "[[1] [2] [3]]" || wrong.Query != unpadded {
		t.Fatalf("noncanonical counterexample was silently rewritten or no longer explains the failure: %+v err=%v", wrong, err)
	}
}

func TestSnapshotDictionaryFollowupReasonNeedsAppointmentEvidence(t *testing.T) {
	db := snapshotFixtureDB(t)
	for id := int64(1); id <= 3; id++ {
		createSnapshotLead(t, db, id)
	}
	now := time.Date(2026, 10, 8, 2, 0, 0, 0, time.UTC)
	for _, row := range []repo.LeadAppointmentModel{
		{ID: 12, LeadID: 2, AppointmentNo: "REASON-SCHEDULED", Status: "scheduled", StartAt: now, EndAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now},
		{ID: 13, LeadID: 3, AppointmentNo: "REASON-STALE-CANCELLED", Status: "cancelled", StartAt: now, EndAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&repo.LeadModel{}).Where("id = ?", row.LeadID).Update("current_appointment_id", row.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := BuildSnapshot(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	description := snapshot.Describe().(SnapshotDescription)
	meaning := description.FieldMeanings["leads.current_followup_eligible / current_followup_overdue"]
	if !strings.Contains(meaning, "current_appointment_id") || !strings.Contains(meaning, "提醒记录缺失") || !strings.Contains(meaning, "异常") {
		t.Fatal("dictionary does not distinguish eligibility from proof of its cause")
	}
	// All three have no reminder rows: one eligible, one valid appointment,
	// one stale pointer. Absence of reminders cannot distinguish these cases.
	result, err := snapshot.Query(t.Context(), "SELECT l.id,l.current_followup_eligible,l.current_appointment_id,a.status,(SELECT COUNT(*) FROM lead_reminder_records r WHERE r.lead_id=l.id) FROM leads l LEFT JOIN lead_appointments a ON a.id=l.current_appointment_id AND a.lead_id=l.id ORDER BY l.id", nil, 100)
	if err != nil || fmt.Sprint(result.Rows) != "[[1 1 <nil> <nil> 0] [2 0 12 scheduled 0] [3 0 13 cancelled 0]]" {
		t.Fatalf("qualification and source reasons disagree: %+v err=%v", result, err)
	}
}

func TestSnapshotDescriptionShanghaiTimeCrossesUTCDateWithoutChangingInstant(t *testing.T) {
	at := time.Date(2026, 10, 8, 16, 0, 0, 123456789, time.UTC)
	snapshot := &Snapshot{At: at}
	encoded, err := json.Marshal(snapshot.Describe())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	var utc, shanghai time.Time
	if err := json.Unmarshal(fields["snapshot_at"], &utc); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fields["snapshot_at_shanghai"], &shanghai); err != nil {
		t.Fatalf("missing Shanghai snapshot time: %v", err)
	}
	_, offset := shanghai.Zone()
	if !utc.Equal(shanghai) || !utc.Equal(at) || utc.Day() != 8 || shanghai.Day() != 9 || offset != 8*60*60 {
		t.Fatalf("snapshot time changed instant or failed Shanghai rollover: utc=%s shanghai=%s", utc, shanghai)
	}
}

func TestAgentSystemSnapshotTimeUsesShanghaiDictionaryInstant(t *testing.T) {
	var checked atomic.Bool
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var request ModelRequest
		if err := json.Unmarshal(body, &request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		var system string
		for _, message := range request.Messages {
			if message.Role == "system" {
				system = message.Content
			}
		}
		for _, message := range request.Messages {
			if message.Role != "tool" {
				continue
			}
			var dictionary struct {
				SnapshotAt         time.Time `json:"snapshot_at"`
				SnapshotAtShanghai time.Time `json:"snapshot_at_shanghai"`
				Timezone           string    `json:"timezone"`
			}
			if json.Unmarshal([]byte(message.Content), &dictionary) != nil || dictionary.SnapshotAt.IsZero() || dictionary.Timezone == "" {
				continue
			}
			_, offset := dictionary.SnapshotAtShanghai.Zone()
			want := dictionary.SnapshotAt.In(time.FixedZone("Shanghai", 8*60*60)).Format(time.RFC3339)
			if !dictionary.SnapshotAt.Equal(dictionary.SnapshotAtShanghai) || offset != 8*60*60 || !strings.Contains(system, "本轮数据快照（上海时间）"+want) {
				t.Errorf("system and dictionary snapshot time do not describe the same Shanghai instant")
			}
			checked.Store(true)
		}
		fixtureModelHandler(w, r)
	})
	conversation := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, conversation.ID, "查询来源线索数量", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if finished := securityWaitRun(t, f, run.ID); finished.Status != "completed" || !checked.Load() {
		t.Fatalf("snapshot time did not reach the controlled tool loop: %+v", finished)
	}
}
