package agent

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
)

func TestSnapshotRecordActorsDistinguishOperatorsFromParticipants(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	createSnapshotLead(t, db, 2)
	if err := db.Model(&repo.LeadModel{}).Where("id=1").Updates(map[string]any{"created_by_user_id": "creator-a", "inviter_user_id": "sales-a", "current_owner_user_id": "sales-b"}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 8, 2, 0, 0, 0, time.UTC)
	later := now.Add(time.Hour)
	leadID := int64(1)
	rows := []struct {
		key   string
		model any
		actor string
	}{
		{"lead_dispatch_records:1", &repo.LeadDispatchRecordModel{ID: 1, LeadID: 1, ManagerUserID: "manager-a", SalesUserID: "sales-a", ActionType: "assign", CreatedAt: now}, "manager-a"},
		{"lead_dispatch_records:2", &repo.LeadDispatchRecordModel{ID: 2, LeadID: 1, ManagerUserID: "manager-a", SalesUserID: "sales-b", ActionType: "claim", CreatedAt: now}, "sales-b"},
		{"lead_dispatch_records:3", &repo.LeadDispatchRecordModel{ID: 3, LeadID: 1, ManagerUserID: "", SalesUserID: "sales-a", ActionType: "assign", CreatedAt: now}, ""},
		{"lead_dispatch_records:4", &repo.LeadDispatchRecordModel{ID: 4, LeadID: 1, ManagerUserID: "manager-a", SalesUserID: "sales-a", ActionType: "unrecognized_legacy_action", CreatedAt: now}, ""},
		{"lead_dispatch_records:5", &repo.LeadDispatchRecordModel{ID: 5, LeadID: 1, ManagerUserID: "manager-b", SalesUserID: "sales-a", ActionType: "reassign", CreatedAt: now}, "manager-b"},
		{"lead_dispatch_records:6", &repo.LeadDispatchRecordModel{ID: 6, LeadID: 1, ManagerUserID: "manager-b", SalesUserID: "sales-a", ActionType: "transfer", CreatedAt: now}, "manager-b"},
		{"lead_dispatch_records:7", &repo.LeadDispatchRecordModel{ID: 7, LeadID: 1, ManagerUserID: "manager-a", SalesUserID: "manager-a", ActionType: "self_assign", CreatedAt: now}, "manager-a"},
		{"lead_dispatch_records:8", &repo.LeadDispatchRecordModel{ID: 8, LeadID: 1, ManagerUserID: "sales-a", SalesUserID: "sales-a", ActionType: "self_create", CreatedAt: now}, "sales-a"},
		{"lead_followups:1", &repo.LeadFollowupModel{ID: 1, LeadID: 1, SalesUserID: "sales-b", Remark: "实际跟进", IsValid: true, CreatedAt: now}, "sales-b"},
		{"lead_appointments:1", &repo.LeadAppointmentModel{ID: 1, LeadID: 1, AppointmentNo: "ACTOR-1", InviterUserID: "sales-a", CloserUserID: "closer-a", AssignedByUserID: "manager-b", StartAt: later, EndAt: later.Add(time.Hour), Status: "scheduled", CreatedAt: now, UpdatedAt: later}, ""},
		{"lead_appointment_events:1", &repo.LeadAppointmentEventModel{ID: 1, LeadID: 1, AppointmentID: 1, EventType: "created", ActorUserID: "manager-a", SnapshotJSON: "{}", CreatedAt: now}, "manager-a"},
		{"closer_availability:1", &repo.CloserAvailabilityModel{ID: 1, CloserUserID: "closer-a", CreatedBy: "manager-a", Kind: "available", StartAt: now, EndAt: later, CreatedAt: now, UpdatedAt: later}, "manager-a"},
		{"lead_pool_records:1", &repo.LeadPoolRecordModel{ID: 1, LeadID: 1, Action: "reclaim", PreviousOwnerUserID: "sales-b", ActorUserID: "system", CreatedAt: now}, "system"},
		{"lead_pool_records:2", &repo.LeadPoolRecordModel{ID: 2, LeadID: 1, Action: "reclaim", PreviousOwnerUserID: "sales-b", CreatedAt: now}, ""},
		{"lead_activity_records:1", &repo.LeadActivityRecordModel{ID: 1, LeadID: 1, ActivityType: "customer_profile_updated", ActorUserID: "manager-b", CreatedAt: now}, "manager-b"},
		{"lead_reminder_records:1", &repo.LeadReminderRecordModel{ID: 1, LeadID: 1, ReminderType: "followup_due", ReceiverUserID: "sales-b|manager-a", Status: "sent", IdempotencyKey: "actor-reminder", DueAt: now, CreatedAt: now, UpdatedAt: later}, ""},
		{"channel_integrations:1", &repo.ChannelIntegrationModel{ID: 1, Platform: "douyin", AccountName: "official", CreatedBy: "creator-a", UpdatedBy: "manager-b", CreatedAt: now, UpdatedAt: later}, "creator-a"},
		{"channel_integration_audits:1", &repo.ChannelIntegrationAuditModel{ID: 1, IntegrationID: 1, Action: "update", ActorUserID: "manager-b", SnapshotJSON: "{}", CreatedAt: later}, "manager-b"},
		{"group_robot_notifications:1", &repo.GroupRobotNotificationModel{ID: 1, LeadID: 1, EventKey: "actor-notify", EventType: "followup_due", Category: "reminder", Status: "sent", DueAt: now, CreatedAt: now, UpdatedAt: later}, ""},
		{"wecom_card_tasks:1", &repo.WeComCardTaskModel{ID: 1, LeadID: 1, ReceiverUserID: "sales-b", CardScene: "claim", CardStatus: "sent", TaskID: "actor-card", SentAt: now, UpdatedAt: &later}, ""},
		{"wecom_callback_logs:1", &repo.WeComCallbackLogModel{ID: 1, LeadID: &leadID, FromUserID: "sales-b", EventKey: "claim", TaskID: "actor-callback", ProcessStatus: "success", CreatedAt: now}, "sales-b"},
		{"lead_deletion_events:1", &repo.LeadDeletionEventModel{ID: 1, LeadID: 1, LeadNo: "CRM-1", Action: "restore", ActorUserID: "manager-a", Reason: "恢复", OccurredAt: now}, "manager-a"},
		{"xiangwu_form_submissions:1", &repo.XiangwuFormSubmission{ID: 1, LeadID: 1, Source: "website", ExternalLeadID: "actor-form", Details: "咨询", CreatedAt: now}, ""},
	}
	want := map[string]string{"leads:1": "creator-a", "leads:2": ""}
	for _, row := range rows {
		if err := db.Create(row.model).Error; err != nil {
			t.Fatalf("insert %s: %v", row.key, err)
		}
		want[row.key] = row.actor
	}
	snapshot, err := BuildSnapshot(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	result, err := snapshot.Query(t.Context(), "SELECT id,actor_user_id FROM analysis_records", nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, row := range result.Rows {
		got[fmt.Sprint(row[0])] = fmt.Sprint(row[1])
	}
	if !reflect.DeepEqual(got, want) {
		for key, actor := range want {
			if got[key] != actor {
				t.Errorf("%s actor=%q; want %q", key, got[key], actor)
			}
		}
		t.Fatalf("index row count=%d want=%d", len(got), len(want))
	}
	// Participant IDs remain available in their original roles, not as operators.
	participants, err := snapshot.Query(t.Context(), "SELECT inviter_user_id,closer_user_id,assigned_by_user_id FROM lead_appointments WHERE id=1", nil, 1)
	if err != nil || !reflect.DeepEqual(participants.Rows, [][]any{{"sales-a", "closer-a", "manager-b"}}) {
		t.Fatalf("appointment participants lost: %+v err=%v", participants, err)
	}
	search, err := snapshot.Search(t.Context(), SearchRequest{ActorID: "sales-b", LeadID: 1, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if search.TotalRows != 3 {
		t.Fatalf("operator search must include followup, own claim and callback only; got %d rows: %#v", search.TotalRows, search.Rows)
	}
}
