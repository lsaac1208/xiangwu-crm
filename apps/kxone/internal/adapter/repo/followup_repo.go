package repo

import (
	"context"
	"sort"

	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

// GormFollowupRepository GORM 实现的跟进记录仓储
type GormFollowupRepository struct {
	db *gorm.DB
}

func NewGormFollowupRepository(db *gorm.DB) *GormFollowupRepository {
	return &GormFollowupRepository{db: db}
}

func (r *GormFollowupRepository) Create(ctx context.Context, followup *types.LeadFollowup) error {
	model := &LeadFollowupModel{}
	model.FromEntity(followup)

	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return err
	}

	followup.ID = model.ID
	return nil
}

func (r *GormFollowupRepository) ListByLeadID(ctx context.Context, leadID int64) ([]*types.LeadFollowup, error) {
	var models []LeadFollowupModel

	err := r.db.WithContext(ctx).
		Where("lead_id = ?", leadID).
		Order(absoluteTimeSort(r.db, "created_at", "DESC", "id")).
		Find(&models).Error

	if err != nil {
		return nil, err
	}

	followups := make([]*types.LeadFollowup, len(models))
	for i, model := range models {
		followups[i] = model.ToEntity()
	}

	return followups, nil
}

func (r *GormFollowupRepository) ListAll(ctx context.Context, offset, limit int) ([]*types.LeadFollowup, int, error) {
	query := r.db.WithContext(ctx).Model(&LeadFollowupModel{})

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var models []LeadFollowupModel
	if err := query.
		Order(absoluteTimeSort(r.db, "created_at", "DESC", "id")).
		Offset(offset).
		Limit(limit).
		Find(&models).Error; err != nil {
		return nil, 0, err
	}

	followups := make([]*types.LeadFollowup, len(models))
	for i, model := range models {
		followups[i] = model.ToEntity()
	}

	return followups, int(total), nil
}

func (r *GormFollowupRepository) ListByLeadIDs(ctx context.Context, leadIDs []int64) ([]*types.LeadFollowup, error) {
	if len(leadIDs) == 0 {
		return []*types.LeadFollowup{}, nil
	}

	// Stay below SQLite's variable limit and keep the same path portable to
	// MySQL. Ordering is restored after all chunks have been loaded.
	const chunkSize = 500
	models := make([]LeadFollowupModel, 0)
	for start := 0; start < len(leadIDs); start += chunkSize {
		end := start + chunkSize
		if end > len(leadIDs) {
			end = len(leadIDs)
		}
		var chunk []LeadFollowupModel
		if err := r.db.WithContext(ctx).Where("lead_id IN ?", leadIDs[start:end]).Find(&chunk).Error; err != nil {
			return nil, err
		}
		models = append(models, chunk...)
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].CreatedAt.Equal(models[j].CreatedAt) {
			return models[i].ID > models[j].ID
		}
		return models[i].CreatedAt.After(models[j].CreatedAt)
	})
	followups := make([]*types.LeadFollowup, len(models))
	for i := range models {
		followups[i] = models[i].ToEntity()
	}
	return followups, nil
}
