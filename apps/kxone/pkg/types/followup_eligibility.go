package types

import (
	"time"
	_ "time/tzdata"
)

var BusinessLocation = func() *time.Location {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(err)
	}
	return location
}()

func BusinessDayStart(now time.Time) time.Time {
	now = now.In(BusinessLocation)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, BusinessLocation)
}

// Current follow-up tasks are owned, active, ordinary follow-ups, one per lead.
func IsCurrentFollowupEligible(lead *Lead) bool {
	return lead != nil && lead.ResultStatus == LeadResultActive && lead.OwnershipState == OwnershipPrivate && lead.CurrentOwnerUserID != "" && lead.NextFollowupDueAt != nil && lead.CurrentAppointmentID == nil && !IsTerminalLeadStatus(lead.Status) && lead.Status != LeadStatusAssignedPendingClaim && lead.Status != LeadStatusPublicPool && (lead.BusinessStage == BusinessStageInvitation || lead.BusinessStage == BusinessStageNegotiation)
}
