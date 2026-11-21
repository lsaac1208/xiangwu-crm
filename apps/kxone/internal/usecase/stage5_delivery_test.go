package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

type stage5Fixture struct {
	appointments *AppointmentUseCase
	negotiation  *NegotiationUseCase
	leads        repo.LeadRepository
	appointment  repo.AppointmentRepository
	activities   repo.LeadActivityRepository
	followups    repo.FollowupRepository
	pool         repo.LeadPoolRepository
	reminders    repo.LeadReminderRepository
	tx           repo.TransactionManager
	settings     stage5SettingsStub
}

type stage5SettingsStub struct{ stage4SettingsStub }

func (stage5SettingsStub) FirstFollowupDue(context.Context) time.Duration { return 2 * time.Hour }
func (stage5SettingsStub) FollowupDueByStar(_ context.Context, star int) time.Duration {
	return time.Duration(6-star) * 12 * time.Hour
}
func (stage5SettingsStub) FollowupReminderOffsets(context.Context) []time.Duration {
	return []time.Duration{0, time.Hour, 4 * time.Hour}
}
func (stage5SettingsStub) PoolMaxActiveLeads(context.Context) int             { return 100 }
func (stage5SettingsStub) PoolCooldown(context.Context) time.Duration         { return 24 * time.Hour }
func (stage5SettingsStub) PoolUnclaimedTimeout(context.Context) time.Duration { return 24 * time.Hour }
func (stage5SettingsStub) PoolFollowupGrace(context.Context) time.Duration    { return 4 * time.Hour }

func TestStage5ConfirmArrivalAtomicallyDeliversToCloser(t *testing.T) {
	fx := newStage5Fixture(t)
	lead, appointment := fx.scheduledAppointment(t, 501, tomorrowAt(10, 0), "closer-1")

	err := fx.appointments.ConfirmArrival(context.Background(), &ConfirmArrivalRequest{AppointmentID: appointment.ID, ActorUserID: "inviter-1", ActorRole: "inviter", ExpectedVersion: 1, Confirmation: "客户已到校，完成当面交接"})
	if err != nil {
		t.Fatal(err)
	}

	storedAppointment, _ := fx.appointment.GetByID(context.Background(), appointment.ID)
	if storedAppointment.Status != types.AppointmentArrivedDelivered || storedAppointment.ArrivedAt == nil || storedAppointment.Version != 2 {
		t.Fatalf("appointment = %+v", storedAppointment)
	}
	storedLead, _ := fx.leads.GetByID(context.Background(), lead.ID)
	if storedLead.BusinessStage != types.BusinessStageNegotiation || storedLead.CurrentOwnerRole != types.OwnerRoleCloser || storedLead.CurrentOwnerUserID != "closer-1" || storedLead.OwnerUserID != "closer-1" {
		t.Fatalf("delivered lead = %+v", storedLead)
	}
	if storedLead.InviterUserID != "inviter-1" || storedLead.AssignedInviterUserID != "inviter-1" || storedLead.ArrivedAt == nil || storedLead.DeliveredAt == nil || storedLead.NextFollowupDueAt == nil {
		t.Fatalf("delivery attribution = %+v", storedLead)
	}
	activities, _ := fx.activities.ListByLead(context.Background(), lead.ID)
	if len(activities) != 1 || activities[0].ActivityType != types.LeadActivityArrivedDelivered || activities[0].ActorRole != "inviter" {
		t.Fatalf("activities = %+v", activities)
	}
	if err := fx.appointments.ConfirmArrival(context.Background(), &ConfirmArrivalRequest{AppointmentID: appointment.ID, ActorUserID: "inviter-1", ActorRole: "inviter", ExpectedVersion: 2}); !errors.Is(err, types.ErrDeliveryAlreadyCompleted) {
		t.Fatalf("duplicate delivery error = %v", err)
	}
	if _, err := enterLeadIntoPublicPool(storedLead, types.PoolReasonManual, time.Now()); !errors.Is(err, types.ErrPublicPoolForbidden) {
		t.Fatalf("negotiation pool error = %v", err)
	}
}

func TestStage5NoShowAndUnauthorizedActorsCannotDeliver(t *testing.T) {
	fx := newStage5Fixture(t)
	_, appointment := fx.scheduledAppointment(t, 502, tomorrowAt(11, 0), "closer-1")
	if err := fx.appointments.ConfirmArrival(context.Background(), &ConfirmArrivalRequest{AppointmentID: appointment.ID, ActorUserID: "closer-1", ActorRole: "closer", ExpectedVersion: 1}); !errors.Is(err, types.ErrUnauthorized) {
		t.Fatalf("closer confirmation error = %v", err)
	}
	appointment.StartAt = time.Now().Add(-2 * time.Hour)
	appointment.EndAt = time.Now().Add(-time.Hour)
	updated, err := fx.appointment.UpdateCAS(context.Background(), appointment, 1)
	if err != nil || !updated {
		t.Fatalf("move appointment to past: updated=%v err=%v", updated, err)
	}
	if err := fx.appointments.NoShow(context.Background(), &AppointmentActionRequest{AppointmentID: appointment.ID, ActorUserID: "closer-1", ActorRole: "closer", ExpectedVersion: 2, Reason: "客户未到"}); err != nil {
		t.Fatal(err)
	}
	if err := fx.appointments.ConfirmArrival(context.Background(), &ConfirmArrivalRequest{AppointmentID: appointment.ID, ActorUserID: "inviter-1", ActorRole: "inviter", ExpectedVersion: 3}); !errors.Is(err, types.ErrDeliveryForbidden) {
		t.Fatalf("no-show delivery error = %v", err)
	}
}

func TestStage5CloserOwnsFollowupAndCompletionAfterDelivery(t *testing.T) {
	fx := newStage5Fixture(t)
	lead, appointment := fx.scheduledAppointment(t, 503, tomorrowAt(14, 0), "closer-1")
	if err := fx.appointments.ConfirmArrival(context.Background(), &ConfirmArrivalRequest{AppointmentID: appointment.ID, ActorUserID: "inviter-1", ActorRole: "inviter", ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	followupUC := NewCreateFollowupUseCase(fx.leads, fx.followups).
		WithTransactionManager(fx.tx).
		WithStage3Workflow(fx.settings, fx.settings, fx.pool, fx.reminders)
	followupReq := &CreateFollowupRequest{LeadID: lead.ID, SalesUserID: "inviter-1", FollowupResult: "connected", Remark: "邀约师不应能编辑", IsValid: true, StarRating: 4}
	if err := followupUC.Execute(context.Background(), followupReq); !errors.Is(err, types.ErrUnauthorized) {
		t.Fatalf("inviter followup error = %v", err)
	}
	followupReq.SalesUserID = "closer-1"
	followupReq.Remark = "谈单后持续跟进"
	if err := followupUC.Execute(context.Background(), followupReq); err != nil {
		t.Fatal(err)
	}

	completeUC := NewCompleteLeadUseCase(fx.leads).WithWorkflow(fx.settings)
	if _, err := completeUC.Execute(context.Background(), &CompleteLeadRequest{LeadID: lead.ID, ActorUserID: "inviter-1", ActorRole: "inviter", Action: "convert", DealAmount: 19800}); !errors.Is(err, types.ErrUnauthorized) {
		t.Fatalf("inviter completion error = %v", err)
	}
	completed, err := completeUC.Execute(context.Background(), &CompleteLeadRequest{LeadID: lead.ID, ActorUserID: "closer-1", ActorRole: "closer", Action: "convert", DealAmount: 19800})
	if err != nil {
		t.Fatal(err)
	}
	if completed.BusinessStage != types.BusinessStageFinished || completed.ResultStatus != types.LeadResultConverted || completed.DealAmount != 19800 {
		t.Fatalf("completed lead = %+v", completed)
	}
}

func TestStage5ManagerCanReassignNegotiationCloser(t *testing.T) {
	fx := newStage5Fixture(t)
	lead, appointment := fx.scheduledAppointment(t, 504, tomorrowAt(16, 0), "closer-1")
	if err := fx.appointments.ConfirmArrival(context.Background(), &ConfirmArrivalRequest{AppointmentID: appointment.ID, ActorUserID: "inviter-1", ActorRole: "inviter", ExpectedVersion: 1}); err != nil {
		t.Fatal(err)
	}
	stored, _ := fx.leads.GetByID(context.Background(), lead.ID)
	if err := fx.negotiation.ReassignCloser(context.Background(), &ReassignNegotiationCloserRequest{LeadID: lead.ID, ActorUserID: "manager-1", ActorRole: "manager", CloserUserID: "closer-2", ExpectedVersion: stored.Version, Reason: "主管调整谈单安排"}); err != nil {
		t.Fatal(err)
	}
	updated, _ := fx.leads.GetByID(context.Background(), lead.ID)
	if updated.CloserUserID != "closer-2" || updated.CurrentOwnerUserID != "closer-2" || updated.InviterUserID != "inviter-1" {
		t.Fatalf("reassigned lead = %+v", updated)
	}
	activities, _ := fx.activities.ListByLead(context.Background(), lead.ID)
	if len(activities) != 2 || activities[1].ActivityType != types.LeadActivityCloserReassigned {
		t.Fatalf("activities = %+v", activities)
	}
}

func newStage5Fixture(t *testing.T) *stage5Fixture {
	t.Helper()
	db := newStage3TestDB(t)
	leads := repo.NewGormLeadRepository(db)
	appointments := repo.NewGormAppointmentRepository(db)
	availability := repo.NewGormCloserAvailabilityRepository(db)
	bindings := repo.NewGormUserBindingRepository(db)
	activities := repo.NewGormLeadActivityRepository(db)
	followups := repo.NewGormFollowupRepository(db)
	pool := repo.NewGormLeadPoolRepository(db)
	reminders := repo.NewGormLeadReminderRepository(db)
	tx := repo.NewGormTransactionManager(db)
	now := time.Now()
	for i, item := range []*types.WeComUserBinding{{WeComUserID: "inviter-1", Role: types.UserRoleInviter, Status: types.UserStatusActive}, {WeComUserID: "closer-1", Role: types.UserRoleCloser, Status: types.UserStatusActive}, {WeComUserID: "closer-2", Role: types.UserRoleCloser, Status: types.UserStatusActive}} {
		item.CRMUserID = int64(i + 1)
		item.CreatedAt, item.UpdatedAt = now, now
		if err := bindings.Create(context.Background(), item); err != nil {
			t.Fatal(err)
		}
	}
	settings := stage5SettingsStub{}
	appointmentUC := NewAppointmentUseCase(appointments, availability, leads, bindings, tx, settings, settings).WithStage5Delivery(settings, activities, reminders)
	return &stage5Fixture{appointments: appointmentUC, negotiation: NewNegotiationUseCase(leads, bindings, activities, tx), leads: leads, appointment: appointments, activities: activities, followups: followups, pool: pool, reminders: reminders, tx: tx, settings: settings}
}

func (fx *stage5Fixture) scheduledAppointment(t *testing.T, id int64, start time.Time, closer string) (*types.Lead, *types.LeadAppointment) {
	t.Helper()
	lead := stage4Lead(id, "inviter-1")
	lead.DispatchManagerUserID = "manager-1"
	mustCreateStage4Lead(t, fx.leads, lead)
	appointment, err := fx.appointments.Create(context.Background(), stage4CreateRequest(lead.ID, start))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fx.appointments.AssignCloser(context.Background(), &AssignCloserRequest{AppointmentID: appointment.ID, ActorUserID: "manager-1", ActorRole: "manager", CloserUserID: closer, ExpectedVersion: 0}); err != nil {
		t.Fatal(err)
	}
	appointment, err = fx.appointment.GetByID(context.Background(), appointment.ID)
	if err != nil {
		t.Fatal(err)
	}
	return lead, appointment
}
