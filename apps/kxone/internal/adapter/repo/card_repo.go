package repo

import (
	"context"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

// GormCardTaskRepository GORM 实现的卡片任务仓储
type GormCardTaskRepository struct {
	db *gorm.DB
}

func NewGormCardTaskRepository(db *gorm.DB) *GormCardTaskRepository {
	return &GormCardTaskRepository{db: db}
}

func (r *GormCardTaskRepository) Create(ctx context.Context, task *types.WeComCardTask) error {
	model := &WeComCardTaskModel{}
	model.FromEntity(task)

	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return err
	}

	task.ID = model.ID
	return nil
}

func (r *GormCardTaskRepository) GetByID(ctx context.Context, id int64) (*types.WeComCardTask, error) {
	var model WeComCardTaskModel

	err := r.db.WithContext(ctx).First(&model, id).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return model.ToEntity(), nil
}

func (r *GormCardTaskRepository) GetByTaskID(ctx context.Context, taskID string) (*types.WeComCardTask, error) {
	var model WeComCardTaskModel
	if err := r.db.WithContext(ctx).Where("task_id = ?", taskID).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return model.ToEntity(), nil
}

func (r *GormCardTaskRepository) GetLatestByScene(ctx context.Context, leadID int64, scene types.CardScene) (*types.WeComCardTask, error) {
	var model WeComCardTaskModel

	err := r.db.WithContext(ctx).
		Where("lead_id = ? AND card_scene = ?", leadID, string(scene)).
		Order("sent_at DESC").
		First(&model).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return model.ToEntity(), nil
}

func (r *GormCardTaskRepository) ListLatestBySceneRecipients(ctx context.Context, leadID int64, scene types.CardScene) ([]*types.WeComCardTask, error) {
	var models []WeComCardTaskModel
	if err := r.db.WithContext(ctx).
		Where("lead_id = ? AND card_scene = ?", leadID, string(scene)).
		Order("sent_at DESC, id DESC").
		Find(&models).Error; err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(models))
	tasks := make([]*types.WeComCardTask, 0, len(models))
	for i := range models {
		receiver := models[i].ReceiverUserID
		if _, exists := seen[receiver]; exists {
			continue
		}
		seen[receiver] = struct{}{}
		tasks = append(tasks, models[i].ToEntity())
	}
	return tasks, nil
}

func (r *GormCardTaskRepository) Update(ctx context.Context, task *types.WeComCardTask) error {
	model := &WeComCardTaskModel{}
	model.FromEntity(task)

	return r.db.WithContext(ctx).Save(model).Error
}

func (r *GormCardTaskRepository) TryStartRetry(ctx context.Context, taskID int64, expectedRetryCount int) (bool, error) {
	now := time.Now()
	result := r.db.WithContext(ctx).
		Model(&WeComCardTaskModel{}).
		Where("id = ? AND card_status = ? AND retry_count = ?", taskID, string(types.CardStatusUpdateFailed), expectedRetryCount).
		Updates(map[string]any{
			"card_status":   string(types.CardStatusUpdating),
			"next_retry_at": nil,
			"updated_at":    &now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (r *GormCardTaskRepository) TryStartUpdate(ctx context.Context, taskID int64, expectedStatus types.CardStatus) (bool, error) {
	if expectedStatus != types.CardStatusSent && expectedStatus != types.CardStatusUpdated {
		return false, nil
	}
	now := time.Now()
	result := r.db.WithContext(ctx).
		Model(&WeComCardTaskModel{}).
		Where("id = ? AND card_status = ?", taskID, string(expectedStatus)).
		Updates(map[string]any{
			"card_status": string(types.CardStatusUpdating),
			"updated_at":  &now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func (r *GormCardTaskRepository) CountStaleUpdating(ctx context.Context, before time.Time) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&WeComCardTaskModel{}).
		Where("card_status = ?", string(types.CardStatusUpdating)).
		Where("COALESCE(updated_at, sent_at) < ?", before).
		Count(&count).Error
	return count, err
}

func (r *GormCardTaskRepository) ListFailedTasks(ctx context.Context, limit int) ([]*types.WeComCardTask, error) {
	var models []WeComCardTaskModel

	err := r.db.WithContext(ctx).
		Where("card_status = ? AND retry_count < ?",
			string(types.CardStatusUpdateFailed), types.MaxCardRetryCount).
		Where("next_retry_at IS NULL OR next_retry_at <= ?", time.Now()).
		Order("next_retry_at ASC").
		Limit(limit).
		Find(&models).Error

	if err != nil {
		return nil, err
	}

	tasks := make([]*types.WeComCardTask, len(models))
	for i, model := range models {
		tasks[i] = model.ToEntity()
	}

	return tasks, nil
}
