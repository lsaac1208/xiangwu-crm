package repo

import (
	"context"

	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

// UserBindingRequestRepository 用户绑定申请仓储接口
type UserBindingRequestRepository interface {
	GetByID(ctx context.Context, id int64) (*types.WeComUserBindingRequest, error)
	GetLatestByWeComUserID(ctx context.Context, wecomUserID string) (*types.WeComUserBindingRequest, error)
	List(ctx context.Context, status string, offset, limit int) ([]*types.WeComUserBindingRequest, int, error)
	Create(ctx context.Context, request *types.WeComUserBindingRequest) error
	Update(ctx context.Context, request *types.WeComUserBindingRequest) error
}

// GormUserBindingRequestRepository GORM 实现的用户绑定申请仓储
type GormUserBindingRequestRepository struct {
	db *gorm.DB
}

func NewGormUserBindingRequestRepository(db *gorm.DB) *GormUserBindingRequestRepository {
	return &GormUserBindingRequestRepository{db: db}
}

func (r *GormUserBindingRequestRepository) GetByID(ctx context.Context, id int64) (*types.WeComUserBindingRequest, error) {
	var model WeComUserBindingRequestModel
	if err := r.db.WithContext(ctx).First(&model, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return model.ToEntity(), nil
}

func (r *GormUserBindingRequestRepository) GetLatestByWeComUserID(ctx context.Context, wecomUserID string) (*types.WeComUserBindingRequest, error) {
	var model WeComUserBindingRequestModel
	if err := r.db.WithContext(ctx).
		Where("wecom_userid = ?", wecomUserID).
		Order("created_at DESC").
		First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return model.ToEntity(), nil
}

func (r *GormUserBindingRequestRepository) List(ctx context.Context, status string, offset, limit int) ([]*types.WeComUserBindingRequest, int, error) {
	var total int64
	query := r.db.WithContext(ctx).Model(&WeComUserBindingRequestModel{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var models []WeComUserBindingRequestModel
	if err := query.Order("updated_at DESC").Offset(offset).Limit(limit).Find(&models).Error; err != nil {
		return nil, 0, err
	}

	requests := make([]*types.WeComUserBindingRequest, len(models))
	for i := range models {
		requests[i] = models[i].ToEntity()
	}
	return requests, int(total), nil
}

func (r *GormUserBindingRequestRepository) Create(ctx context.Context, request *types.WeComUserBindingRequest) error {
	model := &WeComUserBindingRequestModel{}
	model.FromEntity(request)
	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return err
	}
	request.ID = model.ID
	return nil
}

func (r *GormUserBindingRequestRepository) Update(ctx context.Context, request *types.WeComUserBindingRequest) error {
	model := &WeComUserBindingRequestModel{}
	model.FromEntity(request)
	return r.db.WithContext(ctx).
		Model(&WeComUserBindingRequestModel{}).
		Where("id = ?", request.ID).
		Updates(model).Error
}
