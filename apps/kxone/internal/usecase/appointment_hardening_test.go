package usecase

import (
	"context"
	"errors"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
	"testing"
	"time"
)

func TestAppointmentShanghaiHoursAcceptUTCRepresentation(t *testing.T) {
	fx := newStage5Fixture(t)
	local := types.BusinessDayStart(time.Now()).AddDate(0, 0, 1).Add(10 * time.Hour)
	conflicts, err := fx.appointments.DetectConflicts(context.Background(), "closer-1", local.UTC(), local.Add(time.Hour).UTC(), "襄阳校区", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range conflicts {
		if c.Code == types.ConflictCodeOutsideWorktime {
			t.Fatalf("Shanghai 10:00 incorrectly outside work hours: %+v", conflicts)
		}
	}
}

func TestAppointmentTransfersWithLeadAndRequiresClaimBeforeArrival(t *testing.T) {
	ctx := context.Background()
	fx := newStage5Fixture(t)
	lead, appt := fx.scheduledAppointment(t, 8501, tomorrowAt(10, 0), "closer-1")
	transfer := NewReassignLeadUseCase(fx.leads, nil, nil, nil).WithTransactionManager(fx.tx)
	if err := transfer.Execute(ctx, &ReassignLeadRequest{LeadID: lead.ID, ManagerUserID: "manager-2", ActorRole: "manager", NewSalesUserID: "inviter-2"}); err != nil {
		t.Fatal(err)
	}
	pending, _ := fx.appointment.GetByID(ctx, appt.ID)
	if pending.ID != appt.ID || !pending.StartAt.Equal(appt.StartAt) || pending.CloserUserID != appt.CloserUserID || pending.InviterUserID != "inviter-1" || pending.Version <= appt.Version {
		t.Fatalf("handoff reservation %+v", pending)
	}
	if err := fx.appointments.ConfirmArrival(ctx, &ConfirmArrivalRequest{AppointmentID: appt.ID, ActorUserID: "manager-2", ActorRole: "manager", ExpectedVersion: pending.Version}); !errors.Is(err, types.ErrDeliveryForbidden) {
		t.Fatalf("arrival before claim: %v", err)
	}
	if err := fx.appointments.Cancel(ctx, &AppointmentActionRequest{AppointmentID: appt.ID, ActorUserID: "inviter-1", ActorRole: "inviter", ExpectedVersion: pending.Version, Reason: "旧邀约人"}); !errors.Is(err, types.ErrUnauthorized) {
		t.Fatalf("old inviter modification: %v", err)
	}
	visible, err := fx.appointments.List(ctx, &ListAppointmentsRequest{ActorUserID: "inviter-2", ActorRole: "inviter"})
	if err != nil || len(visible) != 1 {
		t.Fatalf("recipient view: %+v %v", visible, err)
	}
	old, err := fx.appointments.List(ctx, &ListAppointmentsRequest{ActorUserID: "inviter-1", ActorRole: "inviter"})
	if err != nil || len(old) != 0 {
		t.Fatalf("old inviter view %+v %v", old, err)
	}
	claim := NewClaimLeadUseCase(fx.leads, nil, nil, nil, nil).WithDispatchHistory(nil, fx.tx).WithStage3Workflow(fx.settings, fx.settings, fx.reminders)
	if err := claim.Execute(ctx, &ClaimLeadRequest{LeadID: lead.ID, SalesUserID: "inviter-2", Source: "test"}); err != nil {
		t.Fatal(err)
	}
	received, _ := fx.appointment.GetByID(ctx, appt.ID)
	current, _ := fx.leads.GetByID(ctx, lead.ID)
	if received.InviterUserID != "inviter-2" || current.OwnershipState != types.OwnershipAppointmentScheduled || current.CurrentOwnerUserID != "inviter-2" {
		t.Fatalf("partial claim: %+v %+v", received, current)
	}
	if err := fx.appointments.Cancel(ctx, &AppointmentActionRequest{AppointmentID: appt.ID, ActorUserID: "manager-2", ActorRole: "manager", ExpectedVersion: pending.Version, Reason: "过期编辑"}); !errors.Is(err, types.ErrAppointmentVersionConflict) {
		t.Fatalf("stale cancel: %v", err)
	}
	if err := fx.appointments.ConfirmArrival(ctx, &ConfirmArrivalRequest{AppointmentID: appt.ID, ActorUserID: "manager-2", ActorRole: "manager", ExpectedVersion: received.Version}); err != nil {
		t.Fatal(err)
	}
}

func TestAppointmentCancelPreservesPendingLeadTransfer(t *testing.T) {
	ctx := context.Background()
	fx := newStage5Fixture(t)
	lead, appt := fx.scheduledAppointment(t, 8502, tomorrowAt(11, 0), "closer-1")
	transfer := NewReassignLeadUseCase(fx.leads, nil, nil, nil).WithTransactionManager(fx.tx)
	for _, target := range []string{"inviter-2", "inviter-3"} {
		if err := transfer.Execute(ctx, &ReassignLeadRequest{LeadID: lead.ID, ManagerUserID: "manager-2", ActorRole: "manager", NewSalesUserID: target}); err != nil {
			t.Fatal(err)
		}
	}
	pending, _ := fx.appointment.GetByID(ctx, appt.ID)
	if err := fx.appointments.Cancel(ctx, &AppointmentActionRequest{AppointmentID: appt.ID, ActorUserID: "manager-2", ActorRole: "manager", ExpectedVersion: pending.Version, Reason: "客户改日"}); err != nil {
		t.Fatal(err)
	}
	current, _ := fx.leads.GetByID(ctx, lead.ID)
	if current.OwnershipState != types.OwnershipAssignedPendingClaim || current.AssignedInviterUserID != "inviter-3" || current.CurrentAppointmentID != nil {
		t.Fatalf("cancel destroyed transfer %+v", current)
	}
	claim := NewClaimLeadUseCase(fx.leads, nil, nil, nil, nil).WithDispatchHistory(nil, fx.tx)
	if err := claim.Execute(ctx, &ClaimLeadRequest{LeadID: lead.ID, SalesUserID: "inviter-3"}); err != nil {
		t.Fatal(err)
	}
	history, _ := fx.appointment.GetByID(ctx, appt.ID)
	if history.InviterUserID != "inviter-1" {
		t.Fatal("terminal history rewritten")
	}
	request := stage4CreateRequest(lead.ID, tomorrowAt(14, 0))
	request.ActorUserID = "inviter-3"
	if _, err := fx.appointments.Create(ctx, request); err != nil {
		t.Fatalf("rebooking: %v", err)
	}
}

func TestAppointmentClaimRollsBackIfReservationChanges(t *testing.T) {
	ctx := context.Background()
	fx := newStage5Fixture(t)
	lead, appt := fx.scheduledAppointment(t, 8503, tomorrowAt(15, 0), "closer-1")
	transfer := NewReassignLeadUseCase(fx.leads, nil, nil, nil).WithTransactionManager(fx.tx)
	if err := transfer.Execute(ctx, &ReassignLeadRequest{LeadID: lead.ID, ManagerUserID: "manager-2", ActorRole: "manager", NewSalesUserID: "inviter-2"}); err != nil {
		t.Fatal(err)
	}
	before, _ := fx.leads.GetByID(ctx, lead.ID)
	// Corrupt pointer in the isolated fixture to prove claim cannot commit half a handoff.
	if err := fx.tx.WithinTransaction(ctx, func(ctx context.Context, r repo.TransactionRepositories) error {
		a, _ := r.AppointmentRepository().GetByID(ctx, appt.ID)
		a.Status = types.AppointmentCancelled
		_, e := r.AppointmentRepository().UpdateCAS(ctx, a, a.Version)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	claim := NewClaimLeadUseCase(fx.leads, nil, nil, nil, nil).WithDispatchHistory(nil, fx.tx)
	if err := claim.Execute(ctx, &ClaimLeadRequest{LeadID: lead.ID, SalesUserID: "inviter-2"}); err == nil {
		t.Fatal("inconsistent reservation accepted")
	}
	after, _ := fx.leads.GetByID(ctx, lead.ID)
	if after.Version != before.Version || after.CurrentOwnerUserID != before.CurrentOwnerUserID {
		t.Fatal("partial claim committed")
	}
}

func TestAppointmentCancelAndClaimRaceHasNoPartialHandoff(t *testing.T) {
	ctx := context.Background()
	for attempt := 0; attempt < 5; attempt++ {
		fx := newStage5Fixture(t)
		lead, appt := fx.scheduledAppointment(t, int64(8600+attempt), tomorrowAt(10, 0), "closer-1")
		transfer := NewReassignLeadUseCase(fx.leads, nil, nil, nil).WithTransactionManager(fx.tx)
		if err := transfer.Execute(ctx, &ReassignLeadRequest{LeadID: lead.ID, ManagerUserID: "manager-2", ActorRole: "manager", NewSalesUserID: "inviter-2"}); err != nil {
			t.Fatal(err)
		}
		pending, _ := fx.appointment.GetByID(ctx, appt.ID)
		claim := NewClaimLeadUseCase(fx.leads, nil, nil, nil, nil).WithDispatchHistory(nil, fx.tx).WithStage3Workflow(fx.settings, fx.settings, fx.reminders)
		start := make(chan struct{})
		result := make(chan error, 2)
		go func() {
			<-start
			result <- claim.Execute(ctx, &ClaimLeadRequest{LeadID: lead.ID, SalesUserID: "inviter-2"})
		}()
		go func() {
			<-start
			result <- fx.appointments.Cancel(ctx, &AppointmentActionRequest{AppointmentID: appt.ID, ActorUserID: "manager-2", ActorRole: "manager", ExpectedVersion: pending.Version, Reason: "并发取消"})
		}()
		close(start)
		e1, e2 := <-result, <-result
		if e1 != nil && e2 != nil {
			t.Fatalf("both failed: %v %v", e1, e2)
		}
		a, _ := fx.appointment.GetByID(ctx, appt.ID)
		l, _ := fx.leads.GetByID(ctx, lead.ID)
		if a.Status == types.AppointmentScheduled {
			if l.CurrentOwnerUserID != "inviter-2" || a.InviterUserID != "inviter-2" || l.OwnershipState != types.OwnershipAppointmentScheduled {
				t.Fatalf("partial claimed reservation %+v %+v", l, a)
			}
		} else if a.Status == types.AppointmentCancelled {
			if l.CurrentAppointmentID != nil || a.InviterUserID != "inviter-1" {
				t.Fatalf("cancel changed history %+v %+v", l, a)
			}
		} else {
			t.Fatalf("unexpected appointment %+v", a)
		}
	}
}

func TestAppointmentCancelRebuildsCurrentOwnerReminders(t *testing.T) {
	ctx := context.Background()
	fx := newStage5Fixture(t)
	lead, appt := fx.scheduledAppointment(t, 8701, tomorrowAt(10, 0), "closer-1")
	transfer := NewReassignLeadUseCase(fx.leads, nil, nil, nil).WithTransactionManager(fx.tx)
	if err := transfer.Execute(ctx, &ReassignLeadRequest{LeadID: lead.ID, ManagerUserID: "manager-2", ActorRole: "manager", NewSalesUserID: "inviter-2"}); err != nil {
		t.Fatal(err)
	}
	claim := NewClaimLeadUseCase(fx.leads, nil, nil, nil, nil).WithDispatchHistory(nil, fx.tx).WithStage3Workflow(fx.settings, fx.settings, fx.reminders)
	if err := claim.Execute(ctx, &ClaimLeadRequest{LeadID: lead.ID, SalesUserID: "inviter-2"}); err != nil {
		t.Fatal(err)
	}
	received, _ := fx.appointment.GetByID(ctx, appt.ID)
	if err := fx.appointments.Cancel(ctx, &AppointmentActionRequest{AppointmentID: appt.ID, ActorUserID: "manager-2", ActorRole: "manager", ExpectedVersion: received.Version, Reason: "恢复普通跟进"}); err != nil {
		t.Fatal(err)
	}
	current, _ := fx.leads.GetByID(ctx, lead.ID)
	if !types.IsCurrentFollowupEligible(current) {
		t.Fatalf("missing current task %+v", current)
	}
	records, err := fx.reminders.ListDue(ctx, time.Now().AddDate(0, 0, 7), 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, record := range records {
		if record.ReminderType == types.ReminderTypeFollowupDue && record.BusinessVersion == current.Version && record.ReceiverUserID == "inviter-2" {
			found = true
		}
	}
	if !found {
		t.Fatal("current owner reminder not rebuilt")
	}
}
