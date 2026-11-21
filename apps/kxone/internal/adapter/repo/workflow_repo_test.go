package repo

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

func TestAppointmentRepositoryOverlapAndOptimisticUpdate(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := db.AutoMigrate(&LeadAppointmentModel{}, &LeadAppointmentEventModel{}); err != nil {
		t.Fatalf("migrate appointment tables: %v", err)
	}
	repository := NewGormAppointmentRepository(db)
	ctx := context.Background()
	start := time.Date(2026, 7, 23, 2, 0, 0, 0, time.UTC)
	appointment := &types.LeadAppointment{
		AppointmentNo:    "AP-1",
		LeadID:           10,
		InviterUserID:    "inviter-1",
		CloserUserID:     "closer-1",
		StartAt:          start,
		EndAt:            start.Add(90 * time.Minute),
		BusinessTimezone: "Asia/Shanghai",
		Campus:           "main",
		VisitorCount:     1,
		Status:           types.AppointmentScheduled,
		CreatedAt:        start.Add(-time.Hour),
		UpdatedAt:        start.Add(-time.Hour),
	}
	if err := repository.Create(ctx, appointment); err != nil {
		t.Fatalf("create appointment: %v", err)
	}

	overlaps, err := repository.ListCloserOverlaps(ctx, "closer-1", start.Add(time.Hour), start.Add(2*time.Hour), 0)
	if err != nil || len(overlaps) != 1 {
		t.Fatalf("overlaps = %d, err = %v; want 1", len(overlaps), err)
	}
	adjacent, err := repository.ListCloserOverlaps(ctx, "closer-1", appointment.EndAt, appointment.EndAt.Add(time.Hour), 0)
	if err != nil || len(adjacent) != 0 {
		t.Fatalf("adjacent overlaps = %d, err = %v; want 0", len(adjacent), err)
	}

	appointment.Campus = "west"
	appointment.UpdatedAt = start
	updated, err := repository.UpdateCAS(ctx, appointment, 0)
	if err != nil || !updated || appointment.Version != 1 {
		t.Fatalf("first CAS updated=%v version=%d err=%v", updated, appointment.Version, err)
	}
	updated, err = repository.UpdateCAS(ctx, appointment, 0)
	if err != nil || updated {
		t.Fatalf("stale CAS updated=%v err=%v", updated, err)
	}
}

func TestReminderRepositoryCreateIsIdempotent(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := db.AutoMigrate(&LeadReminderRecordModel{}); err != nil {
		t.Fatalf("migrate reminder table: %v", err)
	}
	repository := NewGormLeadReminderRepository(db)
	now := time.Now().UTC().Truncate(time.Second)
	record := &types.LeadReminderRecord{
		LeadID:          20,
		ReminderType:    "followup_due",
		BusinessVersion: 3,
		DueAt:           now,
		ReceiverUserID:  "inviter-1",
		Status:          "pending",
		IdempotencyKey:  "lead:20:followup_due:3:inviter-1",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	created, err := repository.Create(context.Background(), record)
	if err != nil || !created {
		t.Fatalf("first create created=%v err=%v", created, err)
	}
	duplicate := *record
	duplicate.ID = 0
	created, err = repository.Create(context.Background(), &duplicate)
	if err != nil || created {
		t.Fatalf("duplicate create created=%v err=%v", created, err)
	}
	var count int64
	if err := db.Model(&LeadReminderRecordModel{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("reminder count=%d err=%v", count, err)
	}
}

func TestReminderRepositoryFiltersFollowupsBeforeLimitAndSuppressesDue(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := db.AutoMigrate(&LeadReminderRecordModel{}); err != nil {
		t.Fatal(err)
	}
	repository := NewGormLeadReminderRepository(db)
	now := time.Now().UTC().Truncate(time.Second)
	appointments := make([]LeadReminderRecordModel, 205)
	for index := range appointments {
		appointments[index] = LeadReminderRecordModel{
			LeadID: int64(index + 1), ReminderType: types.ReminderTypeAppointmentUpcoming,
			DueAt: now.Add(-time.Hour), ReceiverUserID: "closer-1", Status: types.ReminderStatusPending,
			IdempotencyKey: fmt.Sprintf("appointment-%d", index), CreatedAt: now, UpdatedAt: now,
		}
	}
	if err := db.Create(&appointments).Error; err != nil {
		t.Fatal(err)
	}
	followup := &types.LeadReminderRecord{
		LeadID: 999, ReminderType: types.ReminderTypeFollowupDue, DueAt: now.Add(-time.Minute),
		ReceiverUserID: "inviter-1", Status: types.ReminderStatusPending,
		IdempotencyKey: "followup-after-appointments", CreatedAt: now, UpdatedAt: now,
	}
	if _, err := repository.Create(context.Background(), followup); err != nil {
		t.Fatal(err)
	}
	due, err := repository.ListDueByType(context.Background(), types.ReminderTypeFollowupDue, now, 200)
	if err != nil || len(due) != 1 || due[0].ID != followup.ID {
		t.Fatalf("followup due=%+v err=%v", due, err)
	}
	count, err := repository.SuppressDue(context.Background(), types.ReminderTypeFollowupDue, now, "disabled")
	if err != nil || count != 1 {
		t.Fatalf("suppressed=%d err=%v", count, err)
	}
	var storedFollowup LeadReminderRecordModel
	if err := db.First(&storedFollowup, followup.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedFollowup.Status != types.ReminderStatusSuppressed {
		t.Fatalf("followup status=%s", storedFollowup.Status)
	}
	var pendingAppointments int64
	if err := db.Model(&LeadReminderRecordModel{}).
		Where("reminder_type = ? AND status = ?", types.ReminderTypeAppointmentUpcoming, types.ReminderStatusPending).
		Count(&pendingAppointments).Error; err != nil || pendingAppointments != 205 {
		t.Fatalf("pending appointments=%d err=%v", pendingAppointments, err)
	}
}

func TestLeadModelProjectsLegacyWorkflowFields(t *testing.T) {
	now := time.Now()
	model := &LeadModel{
		Status:              string(types.LeadStatusClaimed),
		AssignedSalesUserID: "sales-1",
		OwnerUserID:         "sales-1",
		AssignedAt:          &now,
		ClaimedAt:           &now,
	}
	lead := model.ToEntity()
	if lead.BusinessStage != types.BusinessStageInvitation || lead.OwnershipState != types.OwnershipPrivate || lead.ResultStatus != types.LeadResultActive {
		t.Fatalf("unexpected workflow projection: %+v", lead)
	}
	if lead.AssignedInviterUserID != "sales-1" || lead.InviterUserID != "sales-1" || lead.CurrentOwnerRole != types.OwnerRoleInviter {
		t.Fatalf("unexpected owner projection: %+v", lead)
	}
}

func TestAppointmentAndReminderQueriesCompareAbsoluteInstants(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := db.AutoMigrate(&LeadAppointmentModel{}, &CloserAvailabilityModel{}, &LeadReminderRecordModel{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	start := time.Date(2026, 10, 2, 10, 0, 0, 0, types.BusinessLocation)
	for i, at := range []time.Time{start, start.UTC()} {
		if err := db.Create(&LeadAppointmentModel{ID: int64(i + 1), AppointmentNo: fmt.Sprint(i), LeadID: int64(i + 1), CloserUserID: "closer", Status: string(types.AppointmentScheduled), StartAt: at, EndAt: at.Add(time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&CloserAvailabilityModel{ID: int64(i + 1), CloserUserID: "closer", StartAt: at, EndAt: at.Add(time.Hour), Kind: types.AvailabilityKindWorking, Capacity: 1}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&LeadReminderRecordModel{ID: int64(i + 1), LeadID: int64(i + 1), DueAt: at, Status: types.ReminderStatusPending, IdempotencyKey: fmt.Sprint(i)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	appointments := NewGormAppointmentRepository(db)
	overlaps, err := appointments.ListCloserOverlaps(ctx, "closer", start.UTC(), start.UTC().Add(time.Hour), 0)
	if err != nil || len(overlaps) != 2 {
		t.Fatalf("absolute overlaps %d %v", len(overlaps), err)
	}
	dayStart := types.BusinessDayStart(start)
	dayEnd := dayStart.AddDate(0, 0, 1)
	items, err := appointments.List(ctx, AppointmentListFilter{EndAfter: &dayStart, StartBefore: &dayEnd})
	if err != nil || len(items) != 2 {
		t.Fatalf("day range %d %v", len(items), err)
	}
	availability, err := NewGormCloserAvailabilityRepository(db).ListOverlaps(ctx, "closer", start.UTC(), start.UTC().Add(time.Hour))
	if err != nil || len(availability) != 2 {
		t.Fatalf("availability %d %v", len(availability), err)
	}
	reminders := NewGormLeadReminderRepository(db)
	early, err := reminders.ListDue(ctx, start.Add(-time.Minute), 10)
	if err != nil || len(early) != 0 {
		t.Fatalf("early reminder %d %v", len(early), err)
	}
	due, err := reminders.ListDue(ctx, start.UTC().Add(time.Second), 10)
	if err != nil || len(due) != 2 {
		t.Fatalf("due reminders %d %v", len(due), err)
	}
}
