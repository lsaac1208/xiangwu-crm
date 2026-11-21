package repo

import (
	"context"

	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

// GormDispatchRecordRepository GORM 实现的分配记录仓储
type GormDispatchRecordRepository struct {
	db *gorm.DB
}

func NewGormDispatchRecordRepository(db *gorm.DB) *GormDispatchRecordRepository {
	return &GormDispatchRecordRepository{db: db}
}

func (r *GormDispatchRecordRepository) Create(ctx context.Context, record *types.LeadDispatchRecord) error {
	model := &LeadDispatchRecordModel{}
	model.FromEntity(record)

	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return err
	}

	record.ID = model.ID
	return nil
}

func (r *GormDispatchRecordRepository) ListByLeadID(ctx context.Context, leadID int64) ([]*types.LeadDispatchRecord, error) {
	var models []LeadDispatchRecordModel

	err := r.db.WithContext(ctx).
		Where("lead_id = ?", leadID).
		Order(absoluteTimeSort(r.db, "created_at", "DESC", "id")).
		Find(&models).Error

	if err != nil {
		return nil, err
	}

	records := make([]*types.LeadDispatchRecord, len(models))
	for i, model := range models {
		records[i] = model.ToEntity()
	}

	return records, nil
}

func (r *GormDispatchRecordRepository) ListLatestByLeadIDs(ctx context.Context, leadIDs []int64) (map[int64]*types.LeadDispatchRecord, error) {
	result := make(map[int64]*types.LeadDispatchRecord, len(leadIDs))
	if len(leadIDs) == 0 {
		return result, nil
	}

	var models []LeadDispatchRecordModel
	err := r.db.WithContext(ctx).
		Where("lead_id IN ?", leadIDs).
		Order("lead_id ASC, " + absoluteTimeSort(r.db, "created_at", "DESC", "id")).
		Find(&models).Error
	if err != nil {
		return nil, err
	}

	for i := range models {
		model := models[i]
		if _, exists := result[model.LeadID]; exists {
			continue
		}
		result[model.LeadID] = model.ToEntity()
	}

	return result, nil
}
