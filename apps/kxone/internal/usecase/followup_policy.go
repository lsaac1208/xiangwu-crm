package usecase

import (
	"context"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/pkg/types"
)

func scheduleFollowupReminders(ctx context.Context, repository repo.LeadReminderRepository, settings runtime.FollowupPolicySettings, lead *types.Lead) error {
	if repository == nil || settings == nil || !types.IsCurrentFollowupEligible(lead) {
		return nil
	}
	for _, reminder := range types.NewFollowupReminders(lead, settings.FollowupReminderOffsets(ctx), time.Now()) {
		_, err := repository.Create(ctx, reminder)
		if err != nil {
			return err
		}
	}
	return nil
}

func enterLeadIntoPublicPool(lead *types.Lead, reason string, now time.Time) (string, error) {
	if lead == nil || lead.BusinessStage != types.BusinessStageInvitation || lead.ResultStatus != types.LeadResultActive ||
		lead.CurrentAppointmentID != nil || lead.ArrivedAt != nil || lead.DeliveredAt != nil {
		return "", types.ErrPublicPoolForbidden
	}
	if lead.OwnershipState == types.OwnershipPublicPool {
		return "", types.ErrLeadNotInPublicPool
	}
	previousOwner := lead.CurrentOwnerUserID
	if previousOwner == "" {
		previousOwner = lead.AssignedInviterUserID
	}
	lead.Status = types.LeadStatusPublicPool
	lead.OwnershipState = types.OwnershipPublicPool
	lead.AssignedSalesUserID = ""
	lead.OwnerUserID = ""
	lead.AssignedInviterUserID = ""
	lead.InviterUserID = ""
	lead.CurrentOwnerUserID = ""
	lead.CurrentOwnerRole = ""
	lead.AssignedAt = nil
	lead.ClaimedAt = nil
	types.ClearOwnerCycle(lead)
	lead.InviterAssignedAt = nil
	lead.InviterClaimedAt = nil
	lead.NextFollowupDueAt = nil
	lead.PoolEnteredAt = &now
	lead.PoolReason = reason
	lead.Version++
	lead.UpdatedAt = now
	return previousOwner, nil
}

func claimLeadFromPublicPool(lead *types.Lead, inviterUserID string, now time.Time) error {
	if lead == nil || lead.BusinessStage != types.BusinessStageInvitation || lead.ResultStatus != types.LeadResultActive || lead.OwnershipState != types.OwnershipPublicPool {
		return types.ErrLeadNotInPublicPool
	}
	lead.Status = types.LeadStatusClaimed
	lead.OwnershipState = types.OwnershipPrivate
	lead.AssignedSalesUserID = inviterUserID
	lead.OwnerUserID = inviterUserID
	lead.AssignedInviterUserID = inviterUserID
	lead.InviterUserID = inviterUserID
	lead.CurrentOwnerUserID = inviterUserID
	lead.CurrentOwnerRole = types.OwnerRoleInviter
	lead.AssignedAt = &now
	lead.ClaimedAt = &now
	types.BeginOwnerCycle(lead, now)
	lead.InviterAssignedAt = &now
	lead.InviterClaimedAt = &now
	lead.PoolEnteredAt = nil
	lead.PoolReason = ""
	lead.Version++
	lead.UpdatedAt = now
	return nil
}
