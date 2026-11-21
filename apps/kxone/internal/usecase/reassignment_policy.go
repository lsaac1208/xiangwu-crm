package usecase

import "github.com/lsaac128/kxone/pkg/types"

// Single and batch transfers share this policy; managers jointly manage leads.
func validateReassignmentState(lead *types.Lead, target, role string, stage types.BusinessStage) error {
	if role != string(types.UserRoleAdmin) && role != string(types.UserRoleManager) && role != "" {
		return types.ErrUnauthorized
	}
	types.ProjectLegacyWorkflow(lead)
	if lead.BusinessStage != stage || lead.ResultStatus != types.LeadResultActive || types.IsTerminalLeadStatus(lead.Status) {
		return types.ErrLeadStatusInvalid
	}
	if target == "" || target == lead.CurrentOwnerUserID || target == lead.OwnerUserID || target == lead.AssignedSalesUserID || (stage == types.BusinessStageInvitation && target == lead.AssignedInviterUserID) {
		return types.ErrUnauthorized
	}
	if stage == types.BusinessStageInvitation && lead.Status != types.LeadStatusAssignedPendingClaim && lead.Status != types.LeadStatusClaimed && lead.Status != types.LeadStatusFollowing {
		return types.ErrLeadStatusInvalid
	}
	if stage == types.BusinessStageNegotiation && lead.CurrentOwnerRole != types.OwnerRoleCloser {
		return types.ErrLeadStatusInvalid
	}
	return nil
}
