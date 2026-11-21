package types

import (
	"fmt"
	"strings"
	"time"
)

// NewFollowupReminders describes the current owner/version's reminder schedule.
// Callers persist the records in the same transaction as the lead change.
func NewFollowupReminders(lead *Lead, offsets []time.Duration, now time.Time) []*LeadReminderRecord {
	if !IsCurrentFollowupEligible(lead) {
		return nil
	}
	result := make([]*LeadReminderRecord, 0, len(offsets))
	for index, offset := range offsets {
		receivers := lead.CurrentOwnerUserID
		if index > 0 && lead.DispatchManagerUserID != "" && lead.DispatchManagerUserID != receivers {
			receivers += "|" + lead.DispatchManagerUserID
		}
		result = append(result, &LeadReminderRecord{
			LeadID: lead.ID, ReminderType: ReminderTypeFollowupDue, BusinessVersion: lead.Version,
			DueAt: lead.NextFollowupDueAt.Add(offset), ReceiverUserID: receivers, Status: ReminderStatusPending,
			IdempotencyKey: fmt.Sprintf("followup:%d:v%d:r%d:%s", lead.ID, lead.Version, index, strings.ReplaceAll(receivers, "|", "-")),
			CreatedAt:      now, UpdatedAt: now,
		})
	}
	return result
}
