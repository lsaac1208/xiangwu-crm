package usecase

import (
	"context"
	"fmt"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
	"time"
)

func appointmentHandoffPending(lead *types.Lead) bool {
	return lead != nil && lead.CurrentAppointmentID != nil && lead.OwnershipState == types.OwnershipAssignedPendingClaim
}

// The caller must use the same transaction as the lead update. Even a transfer
// that preserves the booking advances its version, invalidating stale actions.
func transferActiveAppointment(ctx context.Context, appointments repo.AppointmentRepository, lead *types.Lead, actor, role string, received bool) error {
	if lead.CurrentAppointmentID == nil {
		return nil
	}
	if appointments == nil {
		return fmt.Errorf("appointment handoff requires a transaction")
	}
	a, err := appointments.GetByID(ctx, *lead.CurrentAppointmentID)
	if err != nil {
		return err
	}
	if a == nil || a.LeadID != lead.ID || (a.Status != types.AppointmentScheduled && a.Status != types.AppointmentPendingAssignment) {
		return types.ErrAppointmentVersionConflict
	}
	event, reason := "lead_handoff_started", "待领取："+lead.AssignedInviterUserID
	if received {
		a.InviterUserID = actor
		lead.OwnershipState = types.OwnershipAppointmentPending
		if a.Status == types.AppointmentScheduled {
			lead.OwnershipState = types.OwnershipAppointmentScheduled
		}
		lead.NextFollowupDueAt = nil
		event, reason = "lead_handoff_received", "领取并接管预约："+actor
	}
	version := a.Version
	a.UpdatedAt = time.Now()
	updated, err := appointments.UpdateCAS(ctx, a, version)
	if err != nil {
		return err
	}
	if !updated {
		return types.ErrAppointmentVersionConflict
	}
	return createAppointmentEvent(ctx, appointments, a, actor, role, event, string(a.Status), string(a.Status), reason)
}

func canModifyLeadAppointment(a *types.LeadAppointment, lead *types.Lead, actor, role string) bool {
	if role == string(types.UserRoleManager) || role == string(types.UserRoleAdmin) {
		return true
	}
	return !appointmentHandoffPending(lead) && canModifyAppointment(a, actor, role)
}
func canViewLeadAppointment(a *types.LeadAppointment, lead *types.Lead, actor, role string) bool {
	if appointmentHandoffPending(lead) && lead.CurrentAppointmentID != nil && *lead.CurrentAppointmentID == a.ID {
		return role == string(types.UserRoleManager) || role == string(types.UserRoleAdmin) || (types.IsInvitationRole(types.UserRole(role)) && lead.AssignedInviterUserID == actor) || (role == string(types.UserRoleCloser) && a.CloserUserID == actor)
	}
	return canViewAppointment(a, actor, role)
}
