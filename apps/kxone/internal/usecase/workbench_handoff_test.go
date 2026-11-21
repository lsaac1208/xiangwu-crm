package usecase

import (
	"context"
	"testing"
	"time"
)

func TestWorkbenchAppointmentVisibilityFollowsUnclaimedHandoff(t *testing.T) {
	ctx := context.Background()
	fx := newStage5Fixture(t)
	lead, appointment := fx.scheduledAppointment(t, 8610, tomorrowAt(10, 0), "closer-1")
	uc := NewGetWorkbenchUseCase(fx.leads, fx.appointment)
	uc.now = func() time.Time { return appointment.StartAt.Add(time.Minute) }
	check := func(actor, role string, want int) {
		t.Helper()
		result, err := uc.Execute(ctx, actor, role)
		if err != nil || result.Summary.AppointmentUnconfirmed != want {
			t.Fatalf("%s workbench appointment count: %+v, err=%v, want=%d", actor, result, err, want)
		}
		for _, task := range result.Tasks {
			if task.AppointmentID == appointment.ID && want == 0 {
				t.Fatalf("%s still sees transferred appointment", actor)
			}
		}
	}
	check("inviter-1", "inviter", 1)
	check("inviter-2", "inviter", 0)
	transfer := NewReassignLeadUseCase(fx.leads, nil, nil, nil).WithTransactionManager(fx.tx)
	if err := transfer.Execute(ctx, &ReassignLeadRequest{LeadID: lead.ID, ManagerUserID: "manager-2", ActorRole: "manager", NewSalesUserID: "inviter-2"}); err != nil {
		t.Fatal(err)
	}
	check("inviter-1", "inviter", 0)
	check("inviter-2", "inviter", 1)
	check("closer-1", "closer", 1)
	check("manager-2", "manager", 1)
	claim := NewClaimLeadUseCase(fx.leads, nil, nil, nil, nil).WithDispatchHistory(nil, fx.tx).WithStage3Workflow(fx.settings, fx.settings, fx.reminders)
	if err := claim.Execute(ctx, &ClaimLeadRequest{LeadID: lead.ID, SalesUserID: "inviter-2", Source: "test"}); err != nil {
		t.Fatal(err)
	}
	check("inviter-1", "inviter", 0)
	check("inviter-2", "inviter", 1)
}
