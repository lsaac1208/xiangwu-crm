package repo

import (
	"context"
	"strings"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

// Diagnostics expose delivery state, never vendor response bodies or webhook URLs.
type GroupRobotDeliveryFailure struct {
	ID        int64     `json:"id"`
	LeadID    int64     `json:"lead_id"`
	EventType string    `json:"event_type"`
	Reason    string    `json:"reason"`
	Attempts  int       `json:"attempts"`
	WillRetry bool      `json:"will_retry"`
	UpdatedAt time.Time `json:"updated_at"`
}

type GroupRobotDeliveryHealth struct {
	Available           bool                        `json:"available"`
	WindowHours         int                         `json:"window_hours"`
	FailedCount         int64                       `json:"failed_count"`
	RetryingCount       int64                       `json:"retrying_count"`
	ExhaustedCount      int64                       `json:"exhausted_count"`
	PendingOverdueCount int64                       `json:"pending_overdue_count"`
	Healthy             bool                        `json:"healthy"`
	LastSuccessAt       *time.Time                  `json:"last_success_at"`
	RecentFailures      []GroupRobotDeliveryFailure `json:"recent_failures"`
}

type GroupRobotDiagnosticsReader interface {
	DeliveryHealth(context.Context, time.Time) (*GroupRobotDeliveryHealth, error)
	LeadDeliveryFailures(context.Context, int64, time.Time) ([]GroupRobotDeliveryFailure, error)
}

func deliveryFailureReason(raw string) string {
	value := strings.ToLower(raw)
	switch {
	case strings.Contains(value, "45009"), strings.Contains(value, "rate limit"), strings.Contains(value, "429"):
		return "频率超限"
	case strings.Contains(value, "no such host"), strings.Contains(value, "connection refused"), strings.Contains(value, "invalid url"):
		return "地址或网络不可用"
	case strings.Contains(value, "timeout"), strings.Contains(value, "timed out"), strings.Contains(value, "deadline exceeded"):
		return "请求超时"
	case strings.TrimSpace(raw) == "":
		return "原因未记录"
	default:
		return "投递失败，请检查机器人配置"
	}
}

func (r *GormGroupRobotNotificationRepository) deliveryFailures(ctx context.Context, leadID int64, since time.Time, limit int) ([]GroupRobotDeliveryFailure, error) {
	query := r.db.WithContext(ctx).Where("status = ?", types.GroupRobotNotificationFailed).
		Where(absoluteTimeComparison(r.db, "updated_at", ">="), databaseTimeBoundary(r.db, since))
	if leadID > 0 {
		query = query.Where("lead_id = ?", leadID)
	}
	var rows []GroupRobotNotificationModel
	if err := query.Order(absoluteTimeSort(r.db, "updated_at", "DESC", "id")).Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	failures := make([]GroupRobotDeliveryFailure, 0, len(rows))
	for _, row := range rows {
		failures = append(failures, GroupRobotDeliveryFailure{ID: row.ID, LeadID: row.LeadID, EventType: row.EventType, Reason: deliveryFailureReason(row.LastError), Attempts: row.Attempts, WillRetry: row.NextRetryAt != nil && row.Attempts < maxGroupRobotDeliveryAttempts, UpdatedAt: row.UpdatedAt})
	}
	return failures, nil
}

func (r *GormGroupRobotNotificationRepository) LeadDeliveryFailures(ctx context.Context, leadID int64, since time.Time) ([]GroupRobotDeliveryFailure, error) {
	if leadID <= 0 {
		return []GroupRobotDeliveryFailure{}, nil
	}
	return r.deliveryFailures(ctx, leadID, since, 5)
}

func (r *GormGroupRobotNotificationRepository) DeliveryHealth(ctx context.Context, now time.Time) (*GroupRobotDeliveryHealth, error) {
	result := &GroupRobotDeliveryHealth{Available: true, WindowHours: 24}
	since := now.Add(-24 * time.Hour)
	failed := r.db.WithContext(ctx).Model(&GroupRobotNotificationModel{}).Where("status = ?", types.GroupRobotNotificationFailed).
		Where(absoluteTimeComparison(r.db, "updated_at", ">="), databaseTimeBoundary(r.db, since))
	if err := failed.Count(&result.FailedCount).Error; err != nil {
		return nil, err
	}
	retrying := r.db.WithContext(ctx).Model(&GroupRobotNotificationModel{}).Where("status = ? AND next_retry_at IS NOT NULL AND attempts < ?", types.GroupRobotNotificationFailed, maxGroupRobotDeliveryAttempts).
		Where(absoluteTimeComparison(r.db, "updated_at", ">="), databaseTimeBoundary(r.db, since))
	if err := retrying.Count(&result.RetryingCount).Error; err != nil {
		return nil, err
	}
	result.ExhaustedCount = result.FailedCount - result.RetryingCount
	if err := r.db.WithContext(ctx).Model(&GroupRobotNotificationModel{}).Where("status IN ?", []string{types.GroupRobotNotificationPending, types.GroupRobotNotificationSending}).
		Where(absoluteTimeComparison(r.db, "due_at", "<="), databaseTimeBoundary(r.db, now.Add(-10*time.Minute))).Count(&result.PendingOverdueCount).Error; err != nil {
		return nil, err
	}
	var last GroupRobotNotificationModel
	if err := r.db.WithContext(ctx).Where("status = ? AND sent_at IS NOT NULL", types.GroupRobotNotificationSent).
		Order(absoluteTimeSort(r.db, "sent_at", "DESC", "id")).Limit(1).Find(&last).Error; err != nil {
		return nil, err
	}
	result.LastSuccessAt = last.SentAt
	var err error
	result.RecentFailures, err = r.deliveryFailures(ctx, 0, since, 8)
	if err != nil {
		return nil, err
	}
	result.Healthy = result.FailedCount == 0 && result.PendingOverdueCount == 0
	return result, nil
}
