package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
)

func TestSnapshotReminderReceiverSetPreservesEmployeeMappings(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	now := time.Date(2026, 10, 8, 4, 0, 0, 0, time.UTC)
	for _, employee := range []repo.WeComUserBindingModel{
		{ID: 11, CRMUserID: 101, Username: "receiver-a", WeComUserID: "inviter-a", EmployeeName: "员工甲", Role: "inviter", Status: "disabled", CreatedAt: now, UpdatedAt: now},
		{ID: 12, CRMUserID: 102, Username: "receiver-b", WeComUserID: "13900001111", EmployeeName: "员工乙", Role: "manager", Status: "active", CreatedAt: now, UpdatedAt: now},
	} {
		if err := db.Create(&employee).Error; err != nil {
			t.Fatal(err)
		}
	}
	recipients := []string{"inviter-a|13900001111", "13900001111", " inviter-a | missing-history | "}
	for index, recipient := range recipients {
		row := repo.LeadReminderRecordModel{ID: int64(index + 1), LeadID: 1, ReminderType: "followup_escalation", BusinessVersion: 1, ReceiverUserID: recipient, Status: "pending", IdempotencyKey: fmt.Sprintf("receiver-contract-%d", index), DueAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
		if index == 1 {
			row.ReminderType = "appointment_upcoming"
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := BuildSnapshot(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	dictionary := snapshot.Describe().(SnapshotDescription)
	var relations []SnapshotRelationship
	for _, relation := range dictionary.Relationships {
		if relation.FromTable == "lead_reminder_records" && relation.ToTable == "crm_wecom_user_bindings" {
			relations = append(relations, relation)
		}
	}
	if len(relations) != 1 || relations[0].Separator != "|" || relations[0].Cardinality != "one_record_to_zero_or_many_employees" || relations[0].ToColumn != "wecom_userid" || fmt.Sprint(relations[0].FromColumns) != "[receiver_user_id]" {
		t.Fatalf("recipient sets must not be advertised as an atomic employee foreign key: %+v", relations)
	}
	if relations[0].Meaning == "" || dictionary.FieldMeanings["lead_reminder_records.receiver_user_id"] == "" {
		t.Fatal("recipient relation omitted its aggregation and actor semantics")
	}

	// The same sensitive business identifier must still join both inside a
	// multi-recipient escalation and in an appointment's single-recipient row.
	result, err := snapshot.Query(context.Background(), `WITH RECURSIVE recipients(reminder_id, recipient_id, rest) AS (
 SELECT id, '', receiver_user_id || '|' FROM lead_reminder_records
 UNION ALL
 SELECT reminder_id, trim(substr(rest, 1, instr(rest, '|') - 1)), substr(rest, instr(rest, '|') + 1)
 FROM recipients WHERE rest <> ''
)
SELECT r.reminder_id, r.recipient_id, u.employee_name FROM recipients r
LEFT JOIN crm_wecom_user_bindings u ON r.recipient_id = u.wecom_userid
WHERE r.recipient_id <> '' ORDER BY r.reminder_id, r.recipient_id`, nil, 100)
	if err != nil || fmt.Sprint(result.Rows) != "[[1 inviter-a 员工甲] [1 员工#12 员工乙] [2 员工#12 员工乙] [3 inviter-a 员工甲] [3 missing-history <nil>]]" {
		t.Fatalf("recipient splitting lost a mapping, inactive employee, or unmatched history: %+v err=%v", result, err)
	}
	if strings.Contains(fmt.Sprint(result), "13900001111") {
		t.Fatal("sensitive employee identifier escaped snapshot redaction")
	}
	count, err := snapshot.Query(context.Background(), "SELECT COUNT(*) FROM lead_reminder_records", nil, 100)
	if err != nil || fmt.Sprint(count.Rows) != "[[3]]" || result.TotalRows != 5 {
		t.Fatalf("recipient association count was confused with reminder count: reminders=%+v associations=%+v err=%v", count, result, err)
	}
	var original []repo.LeadReminderRecordModel
	if err := db.Order("id").Find(&original).Error; err != nil {
		t.Fatal(err)
	}
	for index, row := range original {
		if row.ReceiverUserID != recipients[index] {
			t.Fatalf("building the analysis snapshot rewrote source receiver %d", row.ID)
		}
	}
}

func TestSnapshotRecordIndexPreservesStateTimeAndStageProvenance(t *testing.T) {
	db := snapshotFixtureDB(t)
	created := time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC)
	followed := created.Add(24 * time.Hour)
	changed := created.Add(40 * 24 * time.Hour)
	for _, row := range []any{
		&repo.LeadModel{ID: 1, LeadNo: "INDEX-STATE", CustomerName: "隔离客户", Status: "converted", BusinessStage: "finished", ResultStatus: "converted", CreatedAt: created, UpdatedAt: changed},
		&repo.LeadFollowupModel{ID: 1, LeadID: 1, SalesUserID: "inviter-a", BusinessStage: "invitation", Remark: "首次邀约", IsValid: true, CreatedAt: followed},
		&repo.LeadActivityRecordModel{ID: 1, LeadID: 1, ActivityType: "customer_profile_updated", ActorUserID: "inviter-a", CreatedAt: changed},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := BuildSnapshot(context.Background(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	result, err := snapshot.Query(context.Background(), "SELECT id, record_id, lead_id, business_stage, occurred_at FROM analysis_records ORDER BY id", nil, 100)
	expected := fmt.Sprintf("[[lead_activity_records:1 1 1 <nil> %s] [lead_followups:1 1 1 invitation %s] [leads:1 1 1 finished %s]]", changed.Format("2006-01-02T15:04:05.000000000Z"), followed.Format("2006-01-02T15:04:05.000000000Z"), created.Format("2006-01-02T15:04:05.000000000Z"))
	if err != nil || fmt.Sprint(result.Rows) != expected {
		t.Fatalf("index conflated current state with historical stage/time or cross-table ID: %+v err=%v", result, err)
	}
	// A stage filter intentionally cannot cover an event without a stored stage.
	filtered, err := snapshot.Query(context.Background(), "SELECT COUNT(*) FROM analysis_records WHERE business_stage='invitation'", nil, 100)
	if err != nil || fmt.Sprint(filtered.Rows) != "[[1]]" {
		t.Fatalf("historical stage was inferred from a different record: %+v err=%v", filtered, err)
	}
	dictionary := snapshot.Describe().(SnapshotDescription)
	for _, field := range []string{"analysis_records.id / record_type / record_id", "analysis_records.occurred_at", "analysis_records.business_stage", "analysis_records.event_type", "lead_appointments.created_at / start_at / arrived_at", "lead_reminder_records.created_at / due_at / sent_at", "group_robot_notifications.created_at / due_at / sent_at", "wecom_card_tasks.sent_at"} {
		if dictionary.FieldMeanings[field] == "" {
			t.Errorf("index provenance is not explained to model: %s", field)
		}
	}
}

func TestSnapshotCallbackEventIndexPreservesFailedOutcome(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	now := time.Date(2026, 10, 8, 4, 0, 0, 0, time.UTC)
	leadID := int64(1)
	for _, row := range []any{
		&repo.WeComCallbackLogModel{ID: 1, LeadID: &leadID, FromUserID: "inviter-a", EventKey: "claim", TaskID: "failed-claim", ProcessStatus: "failed", ErrorMessage: "业务版本冲突", CreatedAt: now},
		&repo.WeComCallbackLogModel{ID: 2, LeadID: &leadID, FromUserID: "inviter-a", EventKey: "", TaskID: "unknown-event", ProcessStatus: "failed", CreatedAt: now},
		&repo.LeadDispatchRecordModel{ID: 1, LeadID: 1, ManagerUserID: "manager-a", SalesUserID: "inviter-a", ActionType: "assign", CreatedAt: now},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := BuildSnapshot(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	result, err := snapshot.Query(t.Context(), `SELECT r.id, r.event_type, c.process_status
 FROM analysis_records r JOIN wecom_callback_logs c ON r.record_id=c.id
 WHERE r.record_type='wecom_callback_logs' ORDER BY r.id`, nil, 100)
	if err != nil || fmt.Sprint(result.Rows) != "[[wecom_callback_logs:1 claim failed] [wecom_callback_logs:2  failed]]" {
		t.Fatalf("callback event metadata omitted event_key or fabricated a successful outcome: %+v err=%v", result, err)
	}
	dispatch, err := snapshot.Query(t.Context(), "SELECT event_type FROM analysis_records WHERE record_type='lead_dispatch_records'", nil, 100)
	if err != nil || fmt.Sprint(dispatch.Rows) != "[[assign]]" {
		t.Fatalf("callback projection changed another record type: %+v err=%v", dispatch, err)
	}
	// Snapshot indexing records the failed attempt, without mutating the CRM
	// state to imply the business operation succeeded.
	var lead repo.LeadModel
	if err := db.First(&lead, 1).Error; err != nil || lead.Status != "following" {
		t.Fatalf("callback indexing changed business state: %+v err=%v", lead, err)
	}
}
