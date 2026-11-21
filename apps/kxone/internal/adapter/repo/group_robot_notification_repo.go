package repo

import (
	"context"
	"strings"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	maxGroupRobotDeliveryAttempts = 4
	maxGroupRobotReconcileBatch   = 200
)

type GroupRobotNotificationRepository interface {
	Create(ctx context.Context, notification *types.GroupRobotNotification) (bool, error)
	ListReady(ctx context.Context, now time.Time, limit int) ([]*types.GroupRobotNotification, error)
	TryMarkSending(ctx context.Context, id int64) (bool, error)
	MarkSent(ctx context.Context, id int64, sentAt time.Time) error
	MarkSuppressed(ctx context.Context, id int64, reason string) error
	MarkCancelled(ctx context.Context, id int64, reason string) error
	MarkFailed(ctx context.Context, id int64, lastError string, nextRetryAt *time.Time) error
	RecoverStaleSending(ctx context.Context, staleBefore, now time.Time) (int64, error)
	SuppressDueCategory(ctx context.Context, category string, dueBefore time.Time, reason string) (int64, error)
	SuppressCreatedBeforeCategories(ctx context.Context, categories []string, createdBefore time.Time, reason string) (int64, error)
	GetByEventKey(ctx context.Context, eventKey string) (*types.GroupRobotNotification, error)
}

// GroupRobotSourceEvent is the minimal immutable source data required to
// rebuild an outbox row when the best-effort write immediately after a
// business transaction failed.
type GroupRobotSourceEvent struct {
	LeadID              int64
	BusinessVersion     int
	Reason              string
	PreviousOwnerUserID string
	ActorUserID         string
	CreatedAt           time.Time
}

// GroupRobotOutboxReconciler finds source events that do not yet have a
// durable notification ledger entry. Implementations must do the anti-join in
// the database and honor limit so the minute worker scan stays bounded.
type GroupRobotOutboxReconciler interface {
	ListMissingNewLeadEvents(ctx context.Context, cutoff time.Time, limit int) ([]*GroupRobotSourceEvent, error)
	ListMissingAutoPoolEvents(ctx context.Context, cutoff time.Time, limit int) ([]*GroupRobotSourceEvent, error)
}

type GroupRobotNotificationModel struct {
	ID              int64      `gorm:"primaryKey;autoIncrement"`
	EventKey        string     `gorm:"type:varchar(191);uniqueIndex;not null"`
	Category        string     `gorm:"type:varchar(32);index:idx_group_robot_category;not null"`
	EventType       string     `gorm:"type:varchar(64);index;index:idx_group_robot_source_event,priority:1;not null"`
	LeadID          int64      `gorm:"index;index:idx_group_robot_source_event,priority:2;not null"`
	ReminderID      *int64     `gorm:"index"`
	BusinessVersion int        `gorm:"index:idx_group_robot_source_event,priority:3;not null;default:0"`
	DueAt           time.Time  `gorm:"index:idx_group_robot_ready,priority:2;not null"`
	Status          string     `gorm:"type:varchar(32);index:idx_group_robot_ready,priority:1;not null"`
	Attempts        int        `gorm:"not null;default:0"`
	NextRetryAt     *time.Time `gorm:"index"`
	LastError       string     `gorm:"type:text"`
	SentAt          *time.Time
	PayloadJSON     string    `gorm:"type:text"`
	CreatedAt       time.Time `gorm:"not null"`
	UpdatedAt       time.Time `gorm:"not null"`
}

func (GroupRobotNotificationModel) TableName() string { return "group_robot_notifications" }

type GormGroupRobotNotificationRepository struct{ db *gorm.DB }

func NewGormGroupRobotNotificationRepository(db *gorm.DB) *GormGroupRobotNotificationRepository {
	return &GormGroupRobotNotificationRepository{db: db}
}

func (r *GormGroupRobotNotificationRepository) ListMissingNewLeadEvents(ctx context.Context, cutoff time.Time, limit int) ([]*GroupRobotSourceEvent, error) {
	limit = boundedGroupRobotReconcileLimit(limit)
	covered := r.db.Table("group_robot_notifications AS covered").
		Select("1").
		Where("covered.event_type = ?", types.GroupRobotEventNewLead).
		Where("covered.lead_id = source_leads.id")
	query := r.db.WithContext(ctx).Table("leads AS source_leads").
		Select("source_leads.id AS lead_id, source_leads.version AS business_version, source_leads.created_at AS created_at").
		Where("source_leads.deleted_at IS NULL").
		Where("source_leads.created_at >= ?", databaseTimeBoundary(r.db, cutoff)).
		Where("NOT EXISTS (?)", covered).
		Order(absoluteTimeSort(r.db, "source_leads.created_at", "ASC", "source_leads.id"))
	query = query.Limit(limit)
	var events []*GroupRobotSourceEvent
	if err := query.Scan(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

func (r *GormGroupRobotNotificationRepository) ListMissingAutoPoolEvents(ctx context.Context, cutoff time.Time, limit int) ([]*GroupRobotSourceEvent, error) {
	limit = boundedGroupRobotReconcileLimit(limit)
	covered := r.db.Table("group_robot_notifications AS covered").
		Select("1").
		Where("covered.event_type = ?", types.GroupRobotEventPoolAutoReturn).
		Where("covered.lead_id = source_pool.lead_id").
		Where("covered.business_version = source_pool.pool_version")
	query := r.db.WithContext(ctx).Table("lead_pool_records AS source_pool").
		Joins("JOIN leads AS source_leads ON source_leads.id = source_pool.lead_id AND source_leads.deleted_at IS NULL").
		Select(strings.Join([]string{
			"source_pool.lead_id AS lead_id",
			"source_pool.pool_version AS business_version",
			"source_pool.reason AS reason",
			"source_pool.previous_owner_user_id AS previous_owner_user_id",
			"source_pool.actor_user_id AS actor_user_id",
			"source_pool.created_at AS created_at",
		}, ", ")).
		Where("source_pool.created_at >= ?", databaseTimeBoundary(r.db, cutoff)).
		Where("source_pool.action = ?", types.PoolActionEnter).
		Where("source_pool.reason IN ?", []string{types.PoolReasonUnclaimedTimeout, types.PoolReasonFollowupOverdue}).
		Where("source_pool.actor_user_id = ?", "system").
		Where("NOT EXISTS (?)", covered).
		Order(absoluteTimeSort(r.db, "source_pool.created_at", "ASC", "source_pool.id"))
	query = query.Limit(limit)
	var events []*GroupRobotSourceEvent
	if err := query.Scan(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

func boundedGroupRobotReconcileLimit(limit int) int {
	if limit <= 0 || limit > maxGroupRobotReconcileBatch {
		return maxGroupRobotReconcileBatch
	}
	return limit
}

func (r *GormGroupRobotNotificationRepository) Create(ctx context.Context, notification *types.GroupRobotNotification) (bool, error) {
	if notification.Status == "" {
		notification.Status = types.GroupRobotNotificationPending
	}
	now := time.Now().UTC()
	if notification.CreatedAt.IsZero() {
		notification.CreatedAt = now
	}
	if notification.UpdatedAt.IsZero() {
		notification.UpdatedAt = now
	}
	model := groupRobotNotificationModelFromEntity(notification)
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "event_key"}},
		DoNothing: true,
	}).Create(model)
	if result.Error != nil {
		return false, result.Error
	}
	created := result.RowsAffected > 0
	if !created {
		var existing GroupRobotNotificationModel
		if err := r.db.WithContext(ctx).Select("id").Where("event_key = ?", notification.EventKey).First(&existing).Error; err != nil {
			return false, err
		}
		model.ID = existing.ID
	}
	notification.ID = model.ID
	return created, nil
}

func (r *GormGroupRobotNotificationRepository) ListReady(ctx context.Context, now time.Time, limit int) ([]*types.GroupRobotNotification, error) {
	query := r.db.WithContext(ctx).
		Where(absoluteTimeComparison(r.db, "due_at", "<="), databaseTimeBoundary(r.db, now)).
		Where("status = ? OR (status = ? AND next_retry_at IS NOT NULL AND "+absoluteTimeComparison(r.db, "next_retry_at", "<=")+")",
			types.GroupRobotNotificationPending, types.GroupRobotNotificationFailed, databaseTimeBoundary(r.db, now)).
		Order(absoluteTimeSort(r.db, "due_at", "ASC", "id"))
	if limit > 0 {
		query = query.Limit(limit)
	}
	var models []GroupRobotNotificationModel
	if err := query.Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*types.GroupRobotNotification, len(models))
	for index := range models {
		result[index] = groupRobotNotificationEntityFromModel(&models[index])
	}
	return result, nil
}

func (r *GormGroupRobotNotificationRepository) TryMarkSending(ctx context.Context, id int64) (bool, error) {
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&GroupRobotNotificationModel{}).
		Where("id = ?", id).
		Where("status = ? OR (status = ? AND next_retry_at IS NOT NULL AND "+absoluteTimeComparison(r.db, "next_retry_at", "<=")+")",
			types.GroupRobotNotificationPending, types.GroupRobotNotificationFailed, databaseTimeBoundary(r.db, now)).
		Updates(map[string]any{
			"status":        types.GroupRobotNotificationSending,
			"attempts":      gorm.Expr("attempts + 1"),
			"next_retry_at": nil,
			"last_error":    "",
			"updated_at":    now.UTC(),
		})
	return result.RowsAffected > 0, result.Error
}

func (r *GormGroupRobotNotificationRepository) MarkSent(ctx context.Context, id int64, sentAt time.Time) error {
	return r.markTerminal(ctx, id, types.GroupRobotNotificationSent, "", &sentAt)
}

func (r *GormGroupRobotNotificationRepository) MarkSuppressed(ctx context.Context, id int64, reason string) error {
	return r.markTerminal(ctx, id, types.GroupRobotNotificationSuppressed, reason, nil)
}

func (r *GormGroupRobotNotificationRepository) MarkCancelled(ctx context.Context, id int64, reason string) error {
	return r.markTerminal(ctx, id, types.GroupRobotNotificationCancelled, reason, nil)
}

func (r *GormGroupRobotNotificationRepository) MarkFailed(ctx context.Context, id int64, lastError string, nextRetryAt *time.Time) error {
	return r.db.WithContext(ctx).Model(&GroupRobotNotificationModel{}).Where("id = ?", id).Updates(map[string]any{
		"status":        types.GroupRobotNotificationFailed,
		"last_error":    lastError,
		"next_retry_at": utcPointer(nextRetryAt),
		"updated_at":    time.Now().UTC(),
	}).Error
}

func (r *GormGroupRobotNotificationRepository) RecoverStaleSending(ctx context.Context, staleBefore, now time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Model(&GroupRobotNotificationModel{}).
		Where("status = ? AND "+absoluteTimeComparison(r.db, "updated_at", "<=")+" AND attempts < ?", types.GroupRobotNotificationSending, databaseTimeBoundary(r.db, staleBefore), maxGroupRobotDeliveryAttempts).
		Updates(map[string]any{
			"status":        types.GroupRobotNotificationFailed,
			"last_error":    "stale sending delivery recovered",
			"next_retry_at": now.UTC(),
			"updated_at":    now.UTC(),
		})
	if result.Error != nil {
		return 0, result.Error
	}
	terminal := r.db.WithContext(ctx).Model(&GroupRobotNotificationModel{}).
		Where("status = ? AND "+absoluteTimeComparison(r.db, "updated_at", "<=")+" AND attempts >= ?", types.GroupRobotNotificationSending, databaseTimeBoundary(r.db, staleBefore), maxGroupRobotDeliveryAttempts).
		Updates(map[string]any{
			"status":        types.GroupRobotNotificationFailed,
			"last_error":    "stale sending delivery exhausted retries",
			"next_retry_at": nil,
			"updated_at":    now.UTC(),
		})
	return result.RowsAffected + terminal.RowsAffected, terminal.Error
}

func (r *GormGroupRobotNotificationRepository) SuppressDueCategory(ctx context.Context, category string, dueBefore time.Time, reason string) (int64, error) {
	result := r.db.WithContext(ctx).Model(&GroupRobotNotificationModel{}).
		Where("category = ? AND "+absoluteTimeComparison(r.db, "due_at", "<=")+" AND status IN ?", category, databaseTimeBoundary(r.db, dueBefore), []string{
			types.GroupRobotNotificationPending, types.GroupRobotNotificationFailed,
		}).
		Updates(map[string]any{
			"status":        types.GroupRobotNotificationSuppressed,
			"last_error":    reason,
			"next_retry_at": nil,
			"updated_at":    time.Now().UTC(),
		})
	return result.RowsAffected, result.Error
}

// SuppressCreatedBeforeCategories applies the immutable first-rollout boundary
// to business events, independently of when their reminders become due.
func (r *GormGroupRobotNotificationRepository) SuppressCreatedBeforeCategories(ctx context.Context, categories []string, createdBefore time.Time, reason string) (int64, error) {
	if len(categories) == 0 || createdBefore.IsZero() {
		return 0, nil
	}
	result := r.db.WithContext(ctx).Model(&GroupRobotNotificationModel{}).
		Where("category IN ? AND "+absoluteTimeComparison(r.db, "created_at", "<")+" AND status IN ?", categories, databaseTimeBoundary(r.db, createdBefore), []string{
			types.GroupRobotNotificationPending, types.GroupRobotNotificationFailed,
		}).
		Updates(map[string]any{
			"status":        types.GroupRobotNotificationSuppressed,
			"last_error":    reason,
			"next_retry_at": nil,
			"updated_at":    time.Now().UTC(),
		})
	return result.RowsAffected, result.Error
}

func (r *GormGroupRobotNotificationRepository) GetByEventKey(ctx context.Context, eventKey string) (*types.GroupRobotNotification, error) {
	var model GroupRobotNotificationModel
	if err := r.db.WithContext(ctx).Where("event_key = ?", eventKey).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return groupRobotNotificationEntityFromModel(&model), nil
}

func (r *GormGroupRobotNotificationRepository) markTerminal(ctx context.Context, id int64, status, message string, sentAt *time.Time) error {
	return r.db.WithContext(ctx).Model(&GroupRobotNotificationModel{}).Where("id = ?", id).Updates(map[string]any{
		"status":        status,
		"last_error":    message,
		"next_retry_at": nil,
		"sent_at":       utcPointer(sentAt),
		"updated_at":    time.Now().UTC(),
	}).Error
}

func groupRobotNotificationModelFromEntity(entity *types.GroupRobotNotification) *GroupRobotNotificationModel {
	return &GroupRobotNotificationModel{
		ID: entity.ID, EventKey: entity.EventKey, Category: entity.Category, EventType: entity.EventType,
		LeadID: entity.LeadID, ReminderID: entity.ReminderID, BusinessVersion: entity.BusinessVersion,
		DueAt: entity.DueAt.UTC(), Status: entity.Status, Attempts: entity.Attempts, NextRetryAt: utcPointer(entity.NextRetryAt),
		LastError: entity.LastError, SentAt: utcPointer(entity.SentAt), PayloadJSON: entity.PayloadJSON,
		CreatedAt: entity.CreatedAt.UTC(), UpdatedAt: entity.UpdatedAt.UTC(),
	}
}

func groupRobotNotificationEntityFromModel(model *GroupRobotNotificationModel) *types.GroupRobotNotification {
	return &types.GroupRobotNotification{
		ID: model.ID, EventKey: model.EventKey, Category: model.Category, EventType: model.EventType,
		LeadID: model.LeadID, ReminderID: model.ReminderID, BusinessVersion: model.BusinessVersion,
		DueAt: model.DueAt, Status: model.Status, Attempts: model.Attempts, NextRetryAt: model.NextRetryAt,
		LastError: model.LastError, SentAt: model.SentAt, PayloadJSON: model.PayloadJSON,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
}
