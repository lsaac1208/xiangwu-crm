package usecase

import (
	"strings"

	"github.com/lsaac128/kxone/pkg/types"
)

func applyLeadMobileVisibility(lead *types.Lead, currentUserID, role string) {
	if lead != nil && types.UserRole(strings.TrimSpace(role)) == types.UserRoleDispatcher {
		// Recompute from decrypted originals before clearing them. This repairs
		// legacy rows whose persisted masked columns contain caller-controlled
		// or otherwise dirty values.
		lead.CustomerMobileMasked = maskMobile(firstNonEmpty(lead.CustomerMobile, lead.CustomerMobileMasked))
		lead.CustomerWechatMasked = maskWechat(firstNonEmpty(lead.CustomerWechat, lead.CustomerWechatMasked))
		lead.CustomerMobile = ""
		lead.CustomerWechat = ""
		return
	}
	if lead == nil || canViewFullLeadMobile(lead, currentUserID, role) {
		return
	}
	lead.CustomerMobileMasked = maskMobile(firstNonEmpty(lead.CustomerMobile, lead.CustomerMobileMasked))
	lead.CustomerMobile = ""
}

func applyLeadsMobileVisibility(leads []*types.Lead, currentUserID, role string) {
	for _, lead := range leads {
		applyLeadMobileVisibility(lead, currentUserID, role)
	}
}

func canViewFullLeadMobile(lead *types.Lead, currentUserID, role string) bool {
	if lead == nil {
		return false
	}
	currentUserID = strings.TrimSpace(currentUserID)
	switch types.UserRole(strings.TrimSpace(role)) {
	case types.UserRoleAdmin, types.UserRoleManager:
		return true
	case types.UserRoleSales, types.UserRoleInviter:
		return currentUserID != "" && (lead.AssignedSalesUserID == currentUserID || lead.OwnerUserID == currentUserID || lead.InviterUserID == currentUserID || lead.AssignedInviterUserID == currentUserID)
	case types.UserRoleCloser:
		return currentUserID != "" && (lead.CreatedByUserID == currentUserID ||
			((lead.BusinessStage == types.BusinessStageNegotiation || ((lead.Status == types.LeadStatusConverted || lead.Status == types.LeadStatusInvalid) && correctionStage(lead) == types.BusinessStageNegotiation)) &&
				(lead.CloserUserID == currentUserID || lead.CurrentOwnerUserID == currentUserID)))
	default:
		return false
	}
}
