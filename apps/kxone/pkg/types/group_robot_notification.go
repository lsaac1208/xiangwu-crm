package types

import "time"

const (
	GroupRobotCategoryNewLead       = "new_lead"
	GroupRobotCategoryDispatchClaim = "dispatch_claim"
	GroupRobotCategoryFollowup      = "followup"
	GroupRobotCategoryPool          = "pool"
)

const (
	GroupRobotEventNewLead          = "new_lead"
	GroupRobotEventDispatchOverdue  = "dispatch_overdue"
	GroupRobotEventClaimOverdue     = "claim_overdue"
	GroupRobotEventDispatchAssigned = "dispatch_assigned"
	GroupRobotEventFollowupDue      = "followup_due"
	GroupRobotEventPoolAutoReturn   = "pool_auto_return"
)

const (
	GroupRobotNotificationPending    = "pending"
	GroupRobotNotificationSending    = "sending"
	GroupRobotNotificationSent       = "sent"
	GroupRobotNotificationSuppressed = "suppressed"
	GroupRobotNotificationCancelled  = "cancelled"
	GroupRobotNotificationFailed     = "failed"
)

// GroupRobotNotification is the durable delivery ledger for the shared
// enterprise-WeChat group robot. EventKey is immutable and unique, so worker
// restarts and repeated rule scans cannot produce duplicate deliveries.
type GroupRobotNotification struct {
	ID              int64      `json:"id"`
	EventKey        string     `json:"event_key"`
	Category        string     `json:"category"`
	EventType       string     `json:"event_type"`
	LeadID          int64      `json:"lead_id"`
	ReminderID      *int64     `json:"reminder_id,omitempty"`
	BusinessVersion int        `json:"business_version"`
	DueAt           time.Time  `json:"due_at"`
	Status          string     `json:"status"`
	Attempts        int        `json:"attempts"`
	NextRetryAt     *time.Time `json:"next_retry_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	SentAt          *time.Time `json:"sent_at,omitempty"`
	PayloadJSON     string     `json:"payload_json,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}
