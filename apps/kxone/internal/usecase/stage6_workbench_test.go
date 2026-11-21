package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

type stage6ReminderRepo struct{ records []*types.LeadReminderRecord }

func (r *stage6ReminderRepo) Create(_ context.Context, record *types.LeadReminderRecord) (bool, error) {
	for _, existing := range r.records {
		if existing.IdempotencyKey == record.IdempotencyKey {
			return false, nil
		}
	}
	r.records = append(r.records, record)
	return true, nil
}
func (r *stage6ReminderRepo) ListDue(context.Context, time.Time, int) ([]*types.LeadReminderRecord, error) {
	return nil, nil
}
func (r *stage6ReminderRepo) ListDueByType(context.Context, string, time.Time, int) ([]*types.LeadReminderRecord, error) {
	return nil, nil
}
func (r *stage6ReminderRepo) TryMarkSending(context.Context, int64) (bool, error) { return true, nil }
func (r *stage6ReminderRepo) MarkResult(context.Context, int64, string, string, *time.Time) error {
	return nil
}
func (r *stage6ReminderRepo) SuppressDue(context.Context, string, time.Time, string) (int64, error) {
	return 0, nil
}

func TestAppointmentNotificationDeduplicatesReceiverPerEvent(t *testing.T) {
	queue := &MockTaskQueue{}
	uc := &AppointmentUseCase{notifications: queue}
	uc.enqueueAppointmentCards(context.Background(), 42, 7, 3, types.CardSceneAppointmentAssigned, "inviter-1", "closer-1", "inviter-1", "")

	if queue.sendCardCalls != 2 {
		t.Fatalf("send card calls = %d, want 2", queue.sendCardCalls)
	}
	if queue.sendCardReceivers[0] != "inviter-1" || queue.sendCardReceivers[1] != "closer-1" {
		t.Fatalf("receivers = %#v", queue.sendCardReceivers)
	}
}

func TestAppointmentReminderLedgerIsReceiverAndVersionIdempotent(t *testing.T) {
	leads := NewMockLeadRepository()
	leads.leads[42] = &types.Lead{ID: 42, DispatchManagerUserID: "manager-1"}
	reminders := &stage6ReminderRepo{}
	uc := &AppointmentUseCase{leads: leads, reminders: reminders}
	appointment := &types.LeadAppointment{ID: 7, LeadID: 42, Version: 3, Status: types.AppointmentScheduled, InviterUserID: "inviter-1", CloserUserID: "closer-1", StartAt: time.Now().Add(time.Hour)}

	uc.scheduleAppointmentReminders(context.Background(), appointment)
	uc.scheduleAppointmentReminders(context.Background(), appointment)
	if len(reminders.records) != 5 {
		t.Fatalf("reminder records = %d, want 5", len(reminders.records))
	}

	appointment.Version = 4
	uc.scheduleAppointmentReminders(context.Background(), appointment)
	if len(reminders.records) != 10 {
		t.Fatalf("rescheduled reminder records = %d, want 10", len(reminders.records))
	}
}

func TestWorkbenchRoleVisibilityAndTasks(t *testing.T) {
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.Local)
	overdue := now.Add(-time.Hour)
	managerLead := &types.Lead{ID: 1, DispatchManagerUserID: "manager-1", BusinessStage: types.BusinessStageInvitation, OwnershipState: types.OwnershipPendingAssignment, ResultStatus: types.LeadResultActive}
	otherManagerLead := &types.Lead{ID: 2, DispatchManagerUserID: "manager-2", BusinessStage: types.BusinessStageInvitation, OwnershipState: types.OwnershipPendingAssignment, ResultStatus: types.LeadResultActive}
	inviterLead := &types.Lead{ID: 3, CurrentOwnerUserID: "inviter-1", DispatchManagerUserID: "manager-1", InviterUserID: "inviter-1", BusinessStage: types.BusinessStageInvitation, OwnershipState: types.OwnershipPrivate, ResultStatus: types.LeadResultActive, NextFollowupDueAt: &overdue}
	closerLead := &types.Lead{ID: 4, DispatchManagerUserID: "manager-1", CloserUserID: "closer-1", CurrentOwnerUserID: "closer-1", CurrentOwnerRole: types.OwnerRoleCloser, BusinessStage: types.BusinessStageNegotiation, OwnershipState: types.OwnershipPrivate, ResultStatus: types.LeadResultActive, DeliveredAt: &overdue}

	if !workbenchCanViewLead(managerLead, "manager-1", types.UserRoleManager) || !workbenchCanViewLead(otherManagerLead, "manager-1", types.UserRoleManager) {
		t.Fatal("manager should have global workbench visibility")
	}
	if !workbenchCanViewLead(inviterLead, "inviter-1", types.UserRoleInviter) || workbenchCanViewLead(closerLead, "inviter-1", types.UserRoleInviter) {
		t.Fatal("inviter scope is incorrect")
	}
	if !workbenchCanViewLead(closerLead, "closer-1", types.UserRoleCloser) || workbenchCanViewLead(inviterLead, "closer-1", types.UserRoleCloser) {
		t.Fatal("closer scope is incorrect")
	}

	resp := &WorkbenchResponse{Tasks: []WorkbenchTask{}}
	uc := &GetWorkbenchUseCase{}
	uc.addLeadTasks(resp, inviterLead, types.UserRoleInviter, now)
	if resp.Summary.OverdueFollowup != 1 || len(resp.Tasks) != 1 || resp.Tasks[0].Type != "overdue_followup" {
		t.Fatalf("inviter workbench = %#v", resp)
	}

	closerResp := &WorkbenchResponse{Tasks: []WorkbenchTask{}}
	uc.addLeadTasks(closerResp, closerLead, types.UserRoleCloser, now)
	if closerResp.Summary.NegotiationActive != 1 || len(closerResp.Tasks) != 1 || closerResp.Tasks[0].Type != "negotiation_first_followup" {
		t.Fatalf("closer workbench = %#v", closerResp)
	}

	inviterAfterDelivery := &WorkbenchResponse{Tasks: []WorkbenchTask{}}
	uc.addLeadTasks(inviterAfterDelivery, closerLead, types.UserRoleInviter, now)
	if inviterAfterDelivery.Summary.NegotiationActive != 0 || len(inviterAfterDelivery.Tasks) != 0 {
		t.Fatalf("inviter retained negotiation workload: %#v", inviterAfterDelivery)
	}
}

func TestDashboardSalesFilterKeepsInviterAndCloserAttribution(t *testing.T) {
	leads := []*types.Lead{
		{ID: 1, InviterUserID: "inviter-1", CurrentOwnerUserID: "closer-1", CloserUserID: "closer-1"},
		{ID: 2, AssignedInviterUserID: "inviter-2"},
	}
	if got := filterLeadsBySales(leads, "inviter-1"); len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("inviter attribution = %#v", got)
	}
	if got := filterLeadsBySales(leads, "closer-1"); len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("closer attribution = %#v", got)
	}
}
