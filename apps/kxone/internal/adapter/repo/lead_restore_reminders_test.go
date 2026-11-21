package repo

import (
	"context"
	"testing"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

type lifecycleReminderPolicy struct{}

func (lifecycleReminderPolicy) FollowupReminderOffsets(context.Context) []time.Duration {
	return []time.Duration{0, 30 * time.Minute}
}

func TestRestoringOwnedLeadReschedulesCurrentVersionAndRollsBackOnFailure(t *testing.T) {
	for _, failInsert := range []bool{false, true} {
		t.Run(map[bool]string{false: "reminders restored", true: "reminder failure rolls back restore"}[failInsert], func(t *testing.T) {
			db := newMigrationTestDB(t)
			if err := RunMigrations(db, "../../../migrations"); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			due := time.Now().Add(time.Hour).UTC()
			lead := &LeadModel{LeadNo: "restore-followup", Status: string(types.LeadStatusFollowing), BusinessStage: string(types.BusinessStageInvitation), OwnershipState: string(types.OwnershipPrivate), ResultStatus: string(types.LeadResultActive), CurrentOwnerUserID: "sales-1", CurrentOwnerRole: string(types.OwnerRoleInviter), DispatchManagerUserID: "manager-1", NextFollowupDueAt: &due, Version: 2, ProfileVersion: 1}
			if err := db.Create(lead).Error; err != nil {
				t.Fatal(err)
			}
			old := &LeadReminderRecordModel{LeadID: lead.ID, ReminderType: types.ReminderTypeFollowupDue, BusinessVersion: 2, DueAt: due, ReceiverUserID: "sales-1", Status: types.ReminderStatusPending, IdempotencyKey: "before-deletion"}
			if err := db.Create(old).Error; err != nil {
				t.Fatal(err)
			}
			repository := NewGormLeadLifecycleRepository(db, lifecycleReminderPolicy{})
			if err := repository.Delete(ctx, lead.ID, lead.LeadNo, 2, 1, "admin", "cleanup"); err != nil {
				t.Fatal(err)
			}
			if failInsert {
				if err := db.Exec("CREATE TRIGGER reject_restored_reminder BEFORE INSERT ON lead_reminder_records BEGIN SELECT RAISE(ABORT, 'fixture reminder failure'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			err := repository.Restore(ctx, lead.ID, "admin", "restore")
			if failInsert {
				if err == nil {
					t.Fatal("restore succeeded despite reminder failure")
				}
				var stored LeadModel
				if err := db.Unscoped().First(&stored, lead.ID).Error; err != nil {
					t.Fatal(err)
				}
				if !stored.DeletedAt.Valid || stored.Version != 3 {
					t.Fatalf("partial restore: %+v", stored)
				}
				var events int64
				db.Model(&LeadDeletionEventModel{}).Where("lead_id = ? AND action = 'restore'", lead.ID).Count(&events)
				if events != 0 {
					t.Fatal("failed restore committed its audit event")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var records []LeadReminderRecordModel
			if err := db.Where("lead_id = ?", lead.ID).Order("id").Find(&records).Error; err != nil {
				t.Fatal(err)
			}
			if len(records) != 3 || records[0].Status != types.ReminderStatusCancelled {
				t.Fatalf("reminder history: %+v", records)
			}
			for i, reminder := range records[1:] {
				if reminder.Status != types.ReminderStatusPending || reminder.BusinessVersion != 4 || !reminder.DueAt.Equal(due.Add(time.Duration(i)*30*time.Minute)) {
					t.Fatalf("restored reminder: %+v", reminder)
				}
			}
			if records[1].ReceiverUserID != "sales-1" || records[2].ReceiverUserID != "sales-1|manager-1" {
				t.Fatalf("wrong reminder receivers: %+v", records)
			}
		})
	}
}
