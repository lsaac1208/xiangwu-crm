package repo

import "time"

type LeadAppointmentModel struct {
	ID                int64     `gorm:"primaryKey;autoIncrement"`
	AppointmentNo     string    `gorm:"type:varchar(64);uniqueIndex;not null"`
	LeadID            int64     `gorm:"index;index:idx_appointments_lead_status,priority:1;not null"`
	InviterUserID     string    `gorm:"type:varchar(128);index;not null"`
	CloserUserID      string    `gorm:"type:varchar(128);index:idx_appointments_closer_status_time,priority:1"`
	AssignedByUserID  string    `gorm:"type:varchar(128)"`
	StartAt           time.Time `gorm:"index:idx_appointments_closer_status_time,priority:3;not null"`
	EndAt             time.Time `gorm:"index:idx_appointments_closer_status_time,priority:4;not null"`
	BusinessTimezone  string    `gorm:"type:varchar(64);not null"`
	Campus            string    `gorm:"type:varchar(128);index"`
	VisitorCount      int       `gorm:"not null;default:1"`
	CustomerConcerns  string    `gorm:"type:text"`
	HandoffNotes      string    `gorm:"type:text"`
	SpecialNotes      string    `gorm:"type:text"`
	Status            string    `gorm:"type:varchar(32);index:idx_appointments_closer_status_time,priority:2;index:idx_appointments_lead_status,priority:2;not null"`
	ArrivedAt         *time.Time
	CancelledReason   string    `gorm:"type:varchar(255)"`
	NoShowReason      string    `gorm:"type:varchar(255)"`
	RescheduledReason string    `gorm:"type:varchar(255)"`
	Version           int       `gorm:"not null;default:0"`
	CreatedAt         time.Time `gorm:"not null"`
	UpdatedAt         time.Time `gorm:"not null"`
}

func (LeadAppointmentModel) TableName() string { return "lead_appointments" }

type LeadAppointmentEventModel struct {
	ID            int64     `gorm:"primaryKey;autoIncrement"`
	AppointmentID int64     `gorm:"index;not null"`
	LeadID        int64     `gorm:"index;not null"`
	EventType     string    `gorm:"type:varchar(64);index;not null"`
	ActorUserID   string    `gorm:"type:varchar(128);not null"`
	ActorRole     string    `gorm:"type:varchar(32);not null"`
	FromStatus    string    `gorm:"type:varchar(32)"`
	ToStatus      string    `gorm:"type:varchar(32)"`
	SnapshotJSON  string    `gorm:"type:text;not null"`
	Reason        string    `gorm:"type:varchar(255)"`
	CreatedAt     time.Time `gorm:"index;not null"`
}

func (LeadAppointmentEventModel) TableName() string { return "lead_appointment_events" }

type CloserAvailabilityModel struct {
	ID           int64     `gorm:"primaryKey;autoIncrement"`
	CloserUserID string    `gorm:"type:varchar(128);index:idx_closer_availability_time,priority:1;not null"`
	StartAt      time.Time `gorm:"index:idx_closer_availability_time,priority:2;not null"`
	EndAt        time.Time `gorm:"index:idx_closer_availability_time,priority:3;not null"`
	Campus       string    `gorm:"type:varchar(128);index"`
	Capacity     int       `gorm:"not null;default:1"`
	Kind         string    `gorm:"type:varchar(32);not null"`
	Reason       string    `gorm:"type:varchar(255)"`
	CreatedBy    string    `gorm:"type:varchar(128);not null"`
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
}

func (CloserAvailabilityModel) TableName() string { return "closer_availability" }

type LeadPoolRecordModel struct {
	ID                  int64     `gorm:"primaryKey;autoIncrement"`
	LeadID              int64     `gorm:"index:idx_pool_records_lead_version,priority:1;not null"`
	Action              string    `gorm:"type:varchar(32);index;not null"`
	Reason              string    `gorm:"type:varchar(255)"`
	PreviousOwnerUserID string    `gorm:"type:varchar(128)"`
	ActorUserID         string    `gorm:"type:varchar(128)"`
	PoolVersion         int       `gorm:"index:idx_pool_records_lead_version,priority:2;not null"`
	CreatedAt           time.Time `gorm:"index;not null"`
}

func (LeadPoolRecordModel) TableName() string { return "lead_pool_records" }

type LeadActivityRecordModel struct {
	ID            int64     `gorm:"primaryKey;autoIncrement"`
	LeadID        int64     `gorm:"index:idx_activity_lead_created,priority:1;not null"`
	ActivityType  string    `gorm:"type:varchar(64);index;not null"`
	ActorUserID   string    `gorm:"type:varchar(128)"`
	ActorRole     string    `gorm:"type:varchar(32)"`
	FromValueJSON string    `gorm:"type:text"`
	ToValueJSON   string    `gorm:"type:text"`
	MetadataJSON  string    `gorm:"type:text"`
	CreatedAt     time.Time `gorm:"index:idx_activity_lead_created,priority:2;not null"`
}

func (LeadActivityRecordModel) TableName() string { return "lead_activity_records" }

type LeadReminderRecordModel struct {
	ID              int64     `gorm:"primaryKey;autoIncrement"`
	LeadID          int64     `gorm:"index:idx_reminders_status_due,priority:3;not null"`
	AppointmentID   *int64    `gorm:"index"`
	ReminderType    string    `gorm:"type:varchar(64);not null"`
	BusinessVersion int       `gorm:"not null"`
	DueAt           time.Time `gorm:"index:idx_reminders_status_due,priority:2;not null"`
	SentAt          *time.Time
	ReceiverUserID  string    `gorm:"type:varchar(128);not null"`
	Status          string    `gorm:"type:varchar(32);index:idx_reminders_status_due,priority:1;not null"`
	ErrorMessage    string    `gorm:"type:text"`
	IdempotencyKey  string    `gorm:"type:varchar(191);uniqueIndex;not null"`
	CreatedAt       time.Time `gorm:"not null"`
	UpdatedAt       time.Time `gorm:"not null"`
}

func (LeadReminderRecordModel) TableName() string { return "lead_reminder_records" }
