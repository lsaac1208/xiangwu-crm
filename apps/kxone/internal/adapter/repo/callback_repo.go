package repo

import (
	"context"

	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormCallbackLogRepository GORM 实现的回调日志仓储
type GormCallbackLogRepository struct {
	db *gorm.DB
}

func NewGormCallbackLogRepository(db *gorm.DB) *GormCallbackLogRepository {
	return &GormCallbackLogRepository{db: db}
}

func (r *GormCallbackLogRepository) Create(ctx context.Context, log *types.WeComCallbackLog) error {
	model := &WeComCallbackLogModel{}
	model.FromEntity(log)

	result := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "task_id"},
				{Name: "from_user_id"},
				{Name: "event_key"},
			},
			DoNothing: true,
		}).
		Create(model)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return types.ErrDuplicateCallback
	}
	if model.ID == 0 {
		return types.ErrDuplicateCallback
	}

	log.ID = model.ID
	return nil
}

func (r *GormCallbackLogRepository) UpdateProcessResult(ctx context.Context, id int64, status, errorMessage string) error {
	return r.db.WithContext(ctx).
		Model(&WeComCallbackLogModel{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"process_status": status,
			"error_message":  errorMessage,
		}).Error
}

func (r *GormCallbackLogRepository) ExistsByTaskAndUser(ctx context.Context, taskID, fromUserID, eventKey string) (bool, error) {
	var count int64

	err := r.db.WithContext(ctx).
		Model(&WeComCallbackLogModel{}).
		Where("task_id = ? AND from_user_id = ? AND event_key = ?", taskID, fromUserID, eventKey).
		Count(&count).Error

	if err != nil {
		return false, err
	}

	return count > 0, nil
}
