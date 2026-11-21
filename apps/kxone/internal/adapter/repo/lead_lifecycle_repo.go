package repo

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

var (
	ErrLeadLifecycleNotFound = errors.New("lead not found")
	ErrLeadLifecycleConflict = errors.New("lead changed")
)

type LeadDeletionEventModel struct {
	ID          int64     `gorm:"primaryKey;autoIncrement"`
	LeadID      int64     `gorm:"index;not null"`
	LeadNo      string    `gorm:"type:varchar(64);not null"`
	Action      string    `gorm:"type:varchar(16);not null"`
	ActorUserID string    `gorm:"column:actor_userid;type:varchar(128);not null"`
	Reason      string    `gorm:"type:varchar(200);not null"`
	OccurredAt  time.Time `gorm:"not null"`
}

func (LeadDeletionEventModel) TableName() string { return "lead_deletion_events" }

type DeletedLead struct {
	ID            int64     `json:"id"`
	LeadNo        string    `json:"lead_no"`
	CustomerName  string    `json:"customer_name"`
	SourceChannel string    `json:"source_channel"`
	Status        string    `json:"status"`
	DeletedAt     time.Time `json:"deleted_at"`
	DeletedBy     string    `json:"deleted_by"`
	Reason        string    `json:"reason"`
}

type FollowupReminderPolicy interface {
	FollowupReminderOffsets(context.Context) []time.Duration
}

type GormLeadLifecycleRepository struct {
	db             *gorm.DB
	reminderPolicy FollowupReminderPolicy
}

func NewGormLeadLifecycleRepository(db *gorm.DB, policy FollowupReminderPolicy) *GormLeadLifecycleRepository {
	return &GormLeadLifecycleRepository{db: db, reminderPolicy: policy}
}

func (r *GormLeadLifecycleRepository) Delete(ctx context.Context, id int64, leadNo string, version, profileVersion int, actor, reason string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lead LeadModel
		if err := tx.First(&lead, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrLeadLifecycleNotFound
			}
			return err
		}
		if lead.LeadNo != leadNo || lead.Version != version || lead.ProfileVersion != profileVersion {
			return ErrLeadLifecycleConflict
		}
		now := time.Now().UTC()
		result := tx.Model(&LeadModel{}).
			Where("id = ? AND version = ? AND profile_version = ?", id, version, profileVersion).
			Updates(map[string]any{
				"deleted_at": now, "updated_at": now,
				"version": gorm.Expr("version + 1"), "profile_version": gorm.Expr("profile_version + 1"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrLeadLifecycleConflict
		}
		if err := tx.Model(&LeadReminderRecordModel{}).
			Where("lead_id = ? AND status IN ?", id, []string{types.ReminderStatusPending, types.ReminderStatusSending}).
			Updates(map[string]any{"status": types.ReminderStatusCancelled, "error_message": "lead_deleted", "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&GroupRobotNotificationModel{}).
			Where("lead_id = ? AND status IN ?", id, []string{types.GroupRobotNotificationPending, types.GroupRobotNotificationFailed, types.GroupRobotNotificationSending}).
			Updates(map[string]any{"status": types.GroupRobotNotificationCancelled, "last_error": "lead_deleted", "next_retry_at": nil, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Create(&LeadDeletionEventModel{
			LeadID: id, LeadNo: lead.LeadNo, Action: "delete", ActorUserID: actor,
			Reason: reason, OccurredAt: now,
		}).Error
	})
}

func (r *GormLeadLifecycleRepository) Restore(ctx context.Context, id int64, actor, reason string) error {
	// Runtime settings can use this database too; resolve them before acquiring
	// the transaction (including SQLite's single-connection configuration).
	var offsets []time.Duration
	if r.reminderPolicy != nil {
		offsets = r.reminderPolicy.FollowupReminderOffsets(ctx)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lead LeadModel
		if err := tx.Unscoped().First(&lead, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrLeadLifecycleNotFound
			}
			return err
		}
		if !lead.DeletedAt.Valid {
			return ErrLeadLifecycleConflict
		}
		now := time.Now().UTC()
		result := tx.Unscoped().Model(&LeadModel{}).
			Where("id = ? AND deleted_at IS NOT NULL AND version = ? AND profile_version = ?", id, lead.Version, lead.ProfileVersion).
			Updates(map[string]any{
				"deleted_at": nil, "updated_at": now,
				"version": gorm.Expr("version + 1"), "profile_version": gorm.Expr("profile_version + 1"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrLeadLifecycleConflict
		}
		lead.Version++
		restored := lead.ToEntity()
		if types.IsCurrentFollowupEligible(restored) && r.reminderPolicy == nil {
			return errors.New("follow-up reminder policy is required to restore an owned lead")
		}
		reminders := NewGormLeadReminderRepository(tx)
		for _, reminder := range types.NewFollowupReminders(restored, offsets, now) {
			if _, err := reminders.Create(ctx, reminder); err != nil {
				return err
			}
		}
		return tx.Create(&LeadDeletionEventModel{
			LeadID: id, LeadNo: lead.LeadNo, Action: "restore", ActorUserID: actor,
			Reason: reason, OccurredAt: now,
		}).Error
	})
}

func (r *GormLeadLifecycleRepository) ListDeleted(ctx context.Context, offset, limit int) ([]DeletedLead, int64, error) {
	query := r.db.WithContext(ctx).Unscoped().Model(&LeadModel{}).Where("deleted_at IS NOT NULL")
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var leads []LeadModel
	if err := query.Order(absoluteTimeSort(r.db, "deleted_at", "DESC", "id")).Offset(offset).Limit(limit).Find(&leads).Error; err != nil {
		return nil, 0, err
	}
	result := make([]DeletedLead, 0, len(leads))
	ids := make([]int64, 0, len(leads))
	for _, lead := range leads {
		result = append(result, DeletedLead{
			ID: lead.ID, LeadNo: lead.LeadNo, CustomerName: lead.CustomerName,
			SourceChannel: lead.SourceChannel, Status: lead.Status, DeletedAt: lead.DeletedAt.Time,
		})
		ids = append(ids, lead.ID)
	}
	if len(ids) == 0 {
		return result, total, nil
	}
	var events []LeadDeletionEventModel
	if err := r.db.WithContext(ctx).Where("lead_id IN ? AND action = ?", ids, "delete").
		Order(absoluteTimeSort(r.db, "occurred_at", "DESC", "id")).Find(&events).Error; err != nil {
		return nil, 0, err
	}
	latest := make(map[int64]LeadDeletionEventModel, len(ids))
	for _, event := range events {
		if _, present := latest[event.LeadID]; !present {
			latest[event.LeadID] = event
		}
	}
	for index := range result {
		if event, present := latest[result[index].ID]; present {
			result[index].DeletedBy = event.ActorUserID
			result[index].Reason = event.Reason
		}
	}
	return result, total, nil
}

func ValidLeadLifecycleReason(reason string) bool {
	reason = strings.TrimSpace(reason)
	return reason != "" && len([]rune(reason)) <= 200
}
