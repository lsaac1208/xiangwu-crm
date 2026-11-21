package repo

import (
	"context"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

type LeadListPreferenceRecord struct {
	UserID        string    `gorm:"column:user_id;primaryKey"`
	ColumnsJSON   string    `gorm:"column:columns_json"`
	Density       string    `gorm:"column:density"`
	DefaultViewID *int64    `gorm:"column:default_view_id"`
	Version       int       `gorm:"column:version"`
	CreatedAt     time.Time `gorm:"column:created_at"`
	UpdatedAt     time.Time `gorm:"column:updated_at"`
}

func (LeadListPreferenceRecord) TableName() string { return "crm_user_ui_preferences" }

type LeadSavedViewRecord struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement"`
	UserID      string    `gorm:"column:user_id"`
	Name        string    `gorm:"column:name"`
	FiltersJSON string    `gorm:"column:filters_json"`
	SortBy      string    `gorm:"column:sort_by"`
	SortOrder   string    `gorm:"column:sort_order"`
	Version     int       `gorm:"column:version"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

func (LeadSavedViewRecord) TableName() string { return "crm_lead_saved_views" }

type LeadUIPreferenceRepository interface {
	GetPreference(context.Context, string) (*LeadListPreferenceRecord, error)
	UpsertPreference(context.Context, *LeadListPreferenceRecord, int) (bool, error)
	ListViews(context.Context, string) ([]*LeadSavedViewRecord, error)
	GetView(context.Context, int64, string) (*LeadSavedViewRecord, error)
	CreateView(context.Context, *LeadSavedViewRecord) error
	UpdateView(context.Context, *LeadSavedViewRecord, int) (bool, error)
	DeleteView(context.Context, int64, string) (bool, error)
	CountViews(context.Context, string) (int, error)
}

type GormLeadUIPreferenceRepository struct{ db *gorm.DB }

func NewGormLeadUIPreferenceRepository(db *gorm.DB) *GormLeadUIPreferenceRepository {
	return &GormLeadUIPreferenceRepository{db: db}
}

func (r *GormLeadUIPreferenceRepository) GetPreference(ctx context.Context, userID string) (*LeadListPreferenceRecord, error) {
	var record LeadListPreferenceRecord
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func (r *GormLeadUIPreferenceRepository) UpsertPreference(ctx context.Context, record *LeadListPreferenceRecord, expectedVersion int) (bool, error) {
	now := time.Now()
	if expectedVersion == 0 {
		var count int64
		if err := r.db.WithContext(ctx).Model(&LeadListPreferenceRecord{}).Where("user_id = ?", record.UserID).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return false, nil
		}
		record.Version, record.CreatedAt, record.UpdatedAt = 1, now, now
		result := r.db.WithContext(ctx).Create(record)
		return result.RowsAffected == 1, result.Error
	}
	result := r.db.WithContext(ctx).Model(&LeadListPreferenceRecord{}).
		Where("user_id = ? AND version = ?", record.UserID, expectedVersion).
		Updates(map[string]any{"columns_json": record.ColumnsJSON, "density": record.Density, "default_view_id": record.DefaultViewID, "version": gorm.Expr("version + 1"), "updated_at": now})
	if result.RowsAffected == 1 {
		record.Version, record.UpdatedAt = expectedVersion+1, now
	}
	return result.RowsAffected == 1, result.Error
}

func (r *GormLeadUIPreferenceRepository) ListViews(ctx context.Context, userID string) ([]*LeadSavedViewRecord, error) {
	var records []*LeadSavedViewRecord
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("updated_at DESC, id DESC").Find(&records).Error
	return records, err
}

func (r *GormLeadUIPreferenceRepository) GetView(ctx context.Context, id int64, userID string) (*LeadSavedViewRecord, error) {
	var record LeadSavedViewRecord
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

func (r *GormLeadUIPreferenceRepository) CreateView(ctx context.Context, record *LeadSavedViewRecord) error {
	now := time.Now()
	record.Version, record.CreatedAt, record.UpdatedAt = 1, now, now
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&LeadSavedViewRecord{}).Where("user_id = ?", record.UserID).Count(&count).Error; err != nil {
			return err
		}
		if count >= 20 {
			return types.ErrLeadViewLimit
		}
		return tx.Create(record).Error
	})
}

func (r *GormLeadUIPreferenceRepository) UpdateView(ctx context.Context, record *LeadSavedViewRecord, expectedVersion int) (bool, error) {
	now := time.Now()
	result := r.db.WithContext(ctx).Model(&LeadSavedViewRecord{}).
		Where("id = ? AND user_id = ? AND version = ?", record.ID, record.UserID, expectedVersion).
		Updates(map[string]any{"name": record.Name, "filters_json": record.FiltersJSON, "sort_by": record.SortBy, "sort_order": record.SortOrder, "version": gorm.Expr("version + 1"), "updated_at": now})
	if result.RowsAffected == 1 {
		record.Version, record.UpdatedAt = expectedVersion+1, now
	}
	return result.RowsAffected == 1, result.Error
}

func (r *GormLeadUIPreferenceRepository) DeleteView(ctx context.Context, id int64, userID string) (bool, error) {
	deleted := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where("id = ? AND user_id = ?", id, userID).Delete(&LeadSavedViewRecord{})
		if result.Error != nil {
			return result.Error
		}
		deleted = result.RowsAffected == 1
		if !deleted {
			return nil
		}
		return tx.Model(&LeadListPreferenceRecord{}).Where("user_id = ? AND default_view_id = ?", userID, id).Updates(map[string]any{"default_view_id": nil, "version": gorm.Expr("version + 1"), "updated_at": time.Now()}).Error
	})
	return deleted, err
}

func (r *GormLeadUIPreferenceRepository) CountViews(ctx context.Context, userID string) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&LeadSavedViewRecord{}).Where("user_id = ?", userID).Count(&count).Error
	return int(count), err
}
