package types

import "time"

type BusinessStage string

const (
	BusinessStageInvitation  BusinessStage = "invitation"
	BusinessStageNegotiation BusinessStage = "negotiation"
	BusinessStageFinished    BusinessStage = "finished"
)

type OwnershipState string

const (
	OwnershipPendingAssignment    OwnershipState = "pending_assignment"
	OwnershipAssignedPendingClaim OwnershipState = "assigned_pending_claim"
	OwnershipPrivate              OwnershipState = "private"
	OwnershipPublicPool           OwnershipState = "public_pool"
	OwnershipAppointmentPending   OwnershipState = "appointment_pending"
	OwnershipAppointmentScheduled OwnershipState = "appointment_scheduled"
)

type LeadResultStatus string

const (
	LeadResultActive    LeadResultStatus = "active"
	LeadResultConverted LeadResultStatus = "converted"
	LeadResultInvalid   LeadResultStatus = "invalid"
	LeadResultClosed    LeadResultStatus = "closed"
)

type OwnerRole string

const (
	OwnerRoleInviter OwnerRole = "inviter"
	OwnerRoleCloser  OwnerRole = "closer"
)

type AppointmentStatus string

const (
	AppointmentPendingAssignment AppointmentStatus = "pending_assignment"
	AppointmentScheduled         AppointmentStatus = "scheduled"
	AppointmentArrivedDelivered  AppointmentStatus = "arrived_delivered"
	AppointmentNoShow            AppointmentStatus = "no_show"
	AppointmentCancelled         AppointmentStatus = "cancelled"
	AppointmentRescheduled       AppointmentStatus = "rescheduled"
)

type LeadAppointment struct {
	ID                int64             `json:"id"`
	AppointmentNo     string            `json:"appointment_no"`
	LeadID            int64             `json:"lead_id"`
	InviterUserID     string            `json:"inviter_userid"`
	CloserUserID      string            `json:"closer_userid,omitempty"`
	AssignedByUserID  string            `json:"assigned_by_userid,omitempty"`
	StartAt           time.Time         `json:"start_at"`
	EndAt             time.Time         `json:"end_at"`
	BusinessTimezone  string            `json:"business_timezone"`
	Campus            string            `json:"campus"`
	VisitorCount      int               `json:"visitor_count"`
	CustomerConcerns  string            `json:"customer_concerns,omitempty"`
	HandoffNotes      string            `json:"handoff_notes,omitempty"`
	SpecialNotes      string            `json:"special_notes,omitempty"`
	Status            AppointmentStatus `json:"status"`
	ArrivedAt         *time.Time        `json:"arrived_at,omitempty"`
	CancelledReason   string            `json:"cancelled_reason,omitempty"`
	NoShowReason      string            `json:"no_show_reason,omitempty"`
	RescheduledReason string            `json:"rescheduled_reason,omitempty"`
	Version           int               `json:"version"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type LeadAppointmentEvent struct {
	ID            int64     `json:"id"`
	AppointmentID int64     `json:"appointment_id"`
	LeadID        int64     `json:"lead_id"`
	EventType     string    `json:"event_type"`
	ActorUserID   string    `json:"actor_userid"`
	ActorRole     string    `json:"actor_role"`
	FromStatus    string    `json:"from_status,omitempty"`
	ToStatus      string    `json:"to_status,omitempty"`
	SnapshotJSON  string    `json:"snapshot_json"`
	Reason        string    `json:"reason,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type CloserAvailability struct {
	ID           int64     `json:"id"`
	CloserUserID string    `json:"closer_userid"`
	StartAt      time.Time `json:"start_at"`
	EndAt        time.Time `json:"end_at"`
	Campus       string    `json:"campus,omitempty"`
	Capacity     int       `json:"capacity"`
	Kind         string    `json:"kind"`
	Reason       string    `json:"reason,omitempty"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

const (
	AvailabilityKindWorking     = "working"
	AvailabilityKindUnavailable = "unavailable"
)

type AppointmentConflict struct {
	Level         string `json:"level"`
	Code          string `json:"code"`
	Message       string `json:"message"`
	AppointmentID int64  `json:"appointment_id,omitempty"`
}

const (
	ConflictLevelHard = "hard"
	ConflictLevelSoft = "soft"

	ConflictCodeOverlap         = "overlap"
	ConflictCodeUnavailable     = "unavailable"
	ConflictCodeCapacity        = "capacity_exceeded"
	ConflictCodeBuffer          = "insufficient_buffer"
	ConflictCodeCrossCampus     = "cross_campus"
	ConflictCodeOutsideWorktime = "outside_working_hours"
	ConflictCodeDailyOverload   = "daily_overload"
)

const (
	AppointmentEventCreated          = "created"
	AppointmentEventCloserAssigned   = "closer_assigned"
	AppointmentEventCloserChanged    = "closer_changed"
	AppointmentEventRescheduled      = "rescheduled"
	AppointmentEventCancelled        = "cancelled"
	AppointmentEventNoShow           = "no_show"
	AppointmentEventArrivedDelivered = "arrived_delivered"
)

const (
	LeadActivityArrivedDelivered          = "arrived_delivered"
	LeadActivityCloserReassigned          = "closer_reassigned"
	LeadActivityNegotiationClosed         = "negotiation_closed"
	LeadActivityProfileUpdated            = "customer_profile_updated"
	LeadActivityInvitationDirectConverted = "invitation_direct_convert"
	LeadActivityNegotiationConverted      = "negotiation_convert"
)

type LeadPoolRecord struct {
	ID                  int64     `json:"id"`
	LeadID              int64     `json:"lead_id"`
	Action              string    `json:"action"`
	Reason              string    `json:"reason,omitempty"`
	PreviousOwnerUserID string    `json:"previous_owner_userid,omitempty"`
	ActorUserID         string    `json:"actor_userid,omitempty"`
	PoolVersion         int       `json:"pool_version"`
	CreatedAt           time.Time `json:"created_at"`
}

const (
	PoolActionEnter = "enter"
	PoolActionClaim = "claim"
)

const (
	PoolReasonUnclaimedTimeout   = "unclaimed_timeout"
	PoolReasonFollowupOverdue    = "followup_overdue"
	PoolReasonRecoverableInvalid = "recoverable_invalid"
	PoolReasonManual             = "manual"
)

type LeadActivityRecord struct {
	ID            int64     `json:"id"`
	LeadID        int64     `json:"lead_id"`
	ActivityType  string    `json:"activity_type"`
	ActorUserID   string    `json:"actor_userid,omitempty"`
	ActorName     string    `json:"actor_name,omitempty"`
	ActorRole     string    `json:"actor_role,omitempty"`
	FromValueJSON string    `json:"from_value_json,omitempty"`
	ToValueJSON   string    `json:"to_value_json,omitempty"`
	MetadataJSON  string    `json:"metadata_json,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type LeadReminderRecord struct {
	ID              int64      `json:"id"`
	LeadID          int64      `json:"lead_id"`
	AppointmentID   *int64     `json:"appointment_id,omitempty"`
	ReminderType    string     `json:"reminder_type"`
	BusinessVersion int        `json:"business_version"`
	DueAt           time.Time  `json:"due_at"`
	SentAt          *time.Time `json:"sent_at,omitempty"`
	ReceiverUserID  string     `json:"receiver_userid"`
	Status          string     `json:"status"`
	ErrorMessage    string     `json:"error_message,omitempty"`
	IdempotencyKey  string     `json:"idempotency_key"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

const (
	ReminderTypeFollowupDue            = "followup_due"
	ReminderTypeAppointmentUpcoming    = "appointment_upcoming"
	ReminderTypeAppointmentUnconfirmed = "appointment_unconfirmed"
	ReminderStatusPending              = "pending"
	ReminderStatusSending              = "sending"
	ReminderStatusSent                 = "sent"
	ReminderStatusSuppressed           = "suppressed"
	ReminderStatusCancelled            = "cancelled"
	ReminderStatusFailed               = "failed"
)

// ProjectLegacyWorkflow supplies V2 read compatibility while the legacy
// columns remain the source of truth during rollout.
func ProjectLegacyWorkflow(lead *Lead) {
	if lead == nil {
		return
	}
	if lead.BusinessStage == "" {
		switch lead.Status {
		case LeadStatusConverted, LeadStatusInvalid, LeadStatusClosed:
			lead.BusinessStage = BusinessStageFinished
		default:
			lead.BusinessStage = BusinessStageInvitation
		}
	}
	if lead.OwnershipState == "" {
		switch lead.Status {
		case LeadStatusPendingDispatch:
			lead.OwnershipState = OwnershipPendingAssignment
		case LeadStatusAssignedPendingClaim:
			lead.OwnershipState = OwnershipAssignedPendingClaim
		case LeadStatusPublicPool:
			lead.OwnershipState = OwnershipPublicPool
		default:
			lead.OwnershipState = OwnershipPrivate
		}
	}
	if lead.ResultStatus == "" {
		switch lead.Status {
		case LeadStatusConverted:
			lead.ResultStatus = LeadResultConverted
		case LeadStatusInvalid:
			lead.ResultStatus = LeadResultInvalid
		case LeadStatusClosed:
			lead.ResultStatus = LeadResultClosed
		default:
			lead.ResultStatus = LeadResultActive
		}
	}
	if lead.AssignedInviterUserID == "" {
		lead.AssignedInviterUserID = lead.AssignedSalesUserID
	}
	if lead.InviterUserID == "" {
		lead.InviterUserID = lead.OwnerUserID
	}
	if lead.CurrentOwnerUserID == "" {
		lead.CurrentOwnerUserID = lead.OwnerUserID
	}
	if lead.CurrentOwnerRole == "" && lead.CurrentOwnerUserID != "" {
		lead.CurrentOwnerRole = OwnerRoleInviter
	}
	if lead.InviterAssignedAt == nil {
		lead.InviterAssignedAt = lead.AssignedAt
	}
	if lead.InviterClaimedAt == nil {
		lead.InviterClaimedAt = lead.ClaimedAt
	}
}
