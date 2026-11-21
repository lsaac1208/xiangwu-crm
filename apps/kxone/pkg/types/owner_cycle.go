package types

import "time"

func BeginOwnerCycle(lead *Lead, now time.Time) {
	lead.CurrentOwnerStartedAt = &now
	lead.CurrentOwnerFirstFollowupAt = nil
}
func ClearOwnerCycle(lead *Lead) {
	lead.CurrentOwnerStartedAt = nil
	lead.CurrentOwnerFirstFollowupAt = nil
}
func OwnerResponseDuration(lead *Lead) (time.Duration, bool) {
	if lead.CurrentOwnerStartedAt == nil || lead.CurrentOwnerFirstFollowupAt == nil || lead.CurrentOwnerFirstFollowupAt.Before(*lead.CurrentOwnerStartedAt) {
		return 0, false
	}
	return lead.CurrentOwnerFirstFollowupAt.Sub(*lead.CurrentOwnerStartedAt), true
}
