package repo

import (
	"context"

	"github.com/lsaac128/kxone/internal/security"
	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SystemSettingRepository 系统配置仓储接口。
type SystemSettingRepository interface {
	List(ctx context.Context, group string) ([]*types.SystemSetting, error)
	GetByKey(ctx context.Context, key string) (*types.SystemSetting, error)
	Upsert(ctx context.Context, setting *types.SystemSetting) error
}

// GormSystemSettingRepository GORM 实现的系统配置仓储。
type GormSystemSettingRepository struct {
	db        *gorm.DB
	protector security.FieldProtector
}

func (r *GormSystemSettingRepository) WithFieldProtector(protector security.FieldProtector) *GormSystemSettingRepository {
	r.protector = protector
	return r
}

func NewGormSystemSettingRepository(db *gorm.DB) *GormSystemSettingRepository {
	return &GormSystemSettingRepository{db: db}
}

func (r *GormSystemSettingRepository) List(ctx context.Context, group string) ([]*types.SystemSetting, error) {
	var models []SystemSettingModel
	query := r.db.WithContext(ctx).Order("setting_group ASC, setting_key ASC")
	if group != "" {
		query = query.Where("setting_group = ?", group)
	}
	if err := query.Find(&models).Error; err != nil {
		return nil, err
	}

	settings := make([]*types.SystemSetting, len(models))
	for i := range models {
		setting, err := r.settingFromModel(&models[i])
		if err != nil {
			return nil, err
		}
		settings[i] = setting
	}
	return settings, nil
}

func (r *GormSystemSettingRepository) GetByKey(ctx context.Context, key string) (*types.SystemSetting, error) {
	var model SystemSettingModel
	if err := r.db.WithContext(ctx).Where("setting_key = ?", key).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return r.settingFromModel(&model)
}

func (r *GormSystemSettingRepository) Upsert(ctx context.Context, setting *types.SystemSetting) error {
	model, err := r.modelFromSetting(setting)
	if err != nil {
		return err
	}

	err = r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "setting_key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"setting_value",
			"value_type",
			"setting_group",
			"description",
			"is_sensitive",
			"updated_by",
			"updated_at",
		}),
	}).Create(model).Error
	if err != nil {
		return err
	}

	setting.ID = model.ID
	return nil
}

// CreateIfAbsent persists an immutable runtime marker without replacing an
// existing value. The unique setting_key constraint makes initialization safe
// when cron and an immediate worker wake-up race on the first rollout scan.
func (r *GormSystemSettingRepository) CreateIfAbsent(ctx context.Context, setting *types.SystemSetting) (bool, error) {
	model, err := r.modelFromSetting(setting)
	if err != nil {
		return false, err
	}
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "setting_key"}},
		DoNothing: true,
	}).Create(model)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected > 0 {
		setting.ID = model.ID
	}
	return result.RowsAffected > 0, nil
}

func (r *GormSystemSettingRepository) modelFromSetting(setting *types.SystemSetting) (*SystemSettingModel, error) {
	model := &SystemSettingModel{}
	model.FromEntity(setting)
	if model.IsSensitive && r.protector != nil {
		protected, err := r.protector.Protect(model.Value)
		if err != nil {
			return nil, err
		}
		model.Value = protected
	}
	return model, nil
}

func (r *GormSystemSettingRepository) settingFromModel(model *SystemSettingModel) (*types.SystemSetting, error) {
	setting := model.ToEntity()
	if setting.IsSensitive && r.protector != nil {
		value, err := r.protector.Reveal(setting.Value)
		if err != nil {
			return nil, err
		}
		setting.Value = value
	}
	return setting, nil
}
