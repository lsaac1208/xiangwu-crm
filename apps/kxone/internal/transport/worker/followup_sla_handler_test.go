package worker

import (
	"context"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/config"
	"path/filepath"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/lsaac128/kxone/pkg/types"
)

type fakeReminderRepo struct {
	records map[int64]*types.LeadReminderRecord
}

func (r *fakeReminderRepo) Create(_ context.Context, record *types.LeadReminderRecord) (bool, error) {
	if r.records == nil {
		r.records = map[int64]*types.LeadReminderRecord{}
	}
	if record.ID == 0 {
		record.ID = int64(len(r.records) + 1)
	}
	if _, exists := r.records[record.ID]; exists {
		return false, nil
	}
	copy := *record
	r.records[record.ID] = &copy
	return true, nil
}
func (r *fakeReminderRepo) ListDue(_ context.Context, now time.Time, limit int) ([]*types.LeadReminderRecord, error) {
	result := []*types.LeadReminderRecord{}
	for _, record := range r.records {
		if record.Status == types.ReminderStatusPending && !record.DueAt.After(now) {
			copy := *record
			result = append(result, &copy)
		}
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}
func (r *fakeReminderRepo) ListDueByType(ctx context.Context, reminderType string, now time.Time, limit int) ([]*types.LeadReminderRecord, error) {
	records, err := r.ListDue(ctx, now, 0)
	if err != nil {
		return nil, err
	}
	result := make([]*types.LeadReminderRecord, 0, len(records))
	for _, record := range records {
		if record.ReminderType == reminderType {
			result = append(result, record)
		}
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}
func (r *fakeReminderRepo) TryMarkSending(_ context.Context, id int64) (bool, error) {
	record := r.records[id]
	if record == nil || record.Status != types.ReminderStatusPending {
		return false, nil
	}
	record.Status = types.ReminderStatusSending
	return true, nil
}
func (r *fakeReminderRepo) MarkResult(_ context.Context, id int64, status, message string, sentAt *time.Time) error {
	record := r.records[id]
	record.Status = status
	record.ErrorMessage = message
	record.SentAt = sentAt
	return nil
}
func (r *fakeReminderRepo) SuppressDue(_ context.Context, reminderType string, dueBefore time.Time, reason string) (int64, error) {
	var count int64
	for _, record := range r.records {
		if record.ReminderType == reminderType && record.Status == types.ReminderStatusPending && !record.DueAt.After(dueBefore) {
			record.Status = types.ReminderStatusSuppressed
			record.ErrorMessage = reason
			count++
		}
	}
	return count, nil
}

func TestFollowupSLAReminderSendsOnceAndPersistsResult(t *testing.T) {
	now := time.Now()
	leads := newFakeWorkerLeadRepo()
	leads.leads[1] = &types.Lead{ID: 1, LeadNo: "L-1", CustomerName: "客户", Version: 3, StarRating: 5, BusinessStage: types.BusinessStageInvitation, OwnershipState: types.OwnershipPrivate, ResultStatus: types.LeadResultActive, CurrentOwnerUserID: "inviter-1", NextFollowupDueAt: &now}
	reminders := &fakeReminderRepo{records: map[int64]*types.LeadReminderRecord{1: {ID: 1, LeadID: 1, ReminderType: types.ReminderTypeFollowupDue, BusinessVersion: 3, DueAt: now.Add(-time.Minute), ReceiverUserID: "inviter-1|manager-1", Status: types.ReminderStatusPending}}}
	client := &fakeWeComClient{}
	handler := NewTaskHandler(leads, newFakeCardTaskRepo(), client, nil).WithStage3Workflow(nil, reminders, nil)
	task := asynq.NewTask(TypeScanFollowupSLA, nil)
	if err := handler.HandleScanFollowupSLA(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if client.textCalls != 1 || reminders.records[1].Status != types.ReminderStatusSent || reminders.records[1].SentAt == nil {
		t.Fatalf("calls=%d reminder=%+v", client.textCalls, reminders.records[1])
	}
	if err := handler.HandleScanFollowupSLA(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if client.textCalls != 1 {
		t.Fatalf("duplicate reminder sent, calls=%d", client.textCalls)
	}
}

func TestFollowupSLAReminderSendsForNegotiationStage(t *testing.T) {
	now := time.Now()
	leads := newFakeWorkerLeadRepo()
	leads.leads[3] = &types.Lead{ID: 3, LeadNo: "L-3", CustomerName: "客户", Version: 4, StarRating: 4, BusinessStage: types.BusinessStageNegotiation, OwnershipState: types.OwnershipPrivate, ResultStatus: types.LeadResultActive, CurrentOwnerUserID: "closer-1", NextFollowupDueAt: &now}
	reminders := &fakeReminderRepo{records: map[int64]*types.LeadReminderRecord{3: {ID: 3, LeadID: 3, ReminderType: types.ReminderTypeFollowupDue, BusinessVersion: 4, DueAt: now.Add(-time.Minute), ReceiverUserID: "closer-1|manager-1", Status: types.ReminderStatusPending}}}
	client := &fakeWeComClient{}
	handler := NewTaskHandler(leads, newFakeCardTaskRepo(), client, nil).WithStage3Workflow(nil, reminders, nil)

	if err := handler.HandleScanFollowupSLA(context.Background(), asynq.NewTask(TypeScanFollowupSLA, nil)); err != nil {
		t.Fatal(err)
	}
	if client.textCalls != 1 || reminders.records[3].Status != types.ReminderStatusSent {
		t.Fatalf("calls=%d reminder=%+v", client.textCalls, reminders.records[3])
	}
}

func TestFollowupSLAReminderCancelsStaleVersion(t *testing.T) {
	now := time.Now()
	leads := newFakeWorkerLeadRepo()
	leads.leads[2] = &types.Lead{ID: 2, Version: 8, BusinessStage: types.BusinessStageInvitation, OwnershipState: types.OwnershipPrivate, ResultStatus: types.LeadResultActive, NextFollowupDueAt: &now}
	reminders := &fakeReminderRepo{records: map[int64]*types.LeadReminderRecord{2: {ID: 2, LeadID: 2, ReminderType: types.ReminderTypeFollowupDue, BusinessVersion: 7, DueAt: now.Add(-time.Minute), ReceiverUserID: "inviter-1", Status: types.ReminderStatusPending}}}
	client := &fakeWeComClient{}
	handler := NewTaskHandler(leads, newFakeCardTaskRepo(), client, nil).WithStage3Workflow(nil, reminders, nil)
	if err := handler.HandleScanFollowupSLA(context.Background(), asynq.NewTask(TypeScanFollowupSLA, nil)); err != nil {
		t.Fatal(err)
	}
	if client.textCalls != 0 || reminders.records[2].Status != types.ReminderStatusCancelled {
		t.Fatalf("calls=%d reminder=%+v", client.textCalls, reminders.records[2])
	}
}

func TestAppointmentReminderRejectsOldVersionOwnerAndPendingHandoff(t *testing.T) {
	ctx := context.Background()
	db, err := repo.NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "reminder.db"), MaxConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&repo.LeadAppointmentModel{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	appointments := repo.NewGormAppointmentRepository(db)
	appointment := &types.LeadAppointment{LeadID: 71, InviterUserID: "new-owner", CloserUserID: "closer", Status: types.AppointmentScheduled, Version: 8, StartAt: time.Now().Add(time.Hour), EndAt: time.Now().Add(2 * time.Hour)}
	if err := appointments.Create(ctx, appointment); err != nil {
		t.Fatal(err)
	}
	lead := &types.Lead{ID: 71, ResultStatus: types.LeadResultActive, BusinessStage: types.BusinessStageInvitation, CurrentAppointmentID: &appointment.ID, CurrentOwnerUserID: "new-owner", OwnershipState: types.OwnershipAppointmentScheduled}
	record := &types.LeadReminderRecord{LeadID: 71, AppointmentID: &appointment.ID, BusinessVersion: 8, ReceiverUserID: "new-owner", ReminderType: types.ReminderTypeAppointmentUpcoming}
	handler := NewTaskHandler(nil, nil, nil, nil).WithStage6Appointments(appointments)
	if !handler.isCurrentReminder(ctx, record, lead) {
		t.Fatal("current reminder rejected")
	}
	record.BusinessVersion = 7
	if handler.isCurrentReminder(ctx, record, lead) {
		t.Fatal("old version accepted")
	}
	record.BusinessVersion = 8
	record.ReceiverUserID = "old-owner"
	if handler.isCurrentReminder(ctx, record, lead) {
		t.Fatal("old owner accepted")
	}
	record.ReceiverUserID = "new-owner"
	lead.OwnershipState = types.OwnershipAssignedPendingClaim
	if handler.isCurrentReminder(ctx, record, lead) {
		t.Fatal("handoff reminder sent before claim")
	}
}
