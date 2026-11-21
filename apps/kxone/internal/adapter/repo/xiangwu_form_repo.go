package repo

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// XiangwuFormSubmission is the durable payload fingerprint and full form detail
// for one website submission. It is inserted in the same transaction as the lead.
type XiangwuFormSubmission struct {
	ID                   int64     `gorm:"primaryKey;autoIncrement"`
	LeadID               int64     `gorm:"uniqueIndex;not null"`
	Source               string    `gorm:"type:varchar(32);uniqueIndex:idx_xiangwu_form_source_external;not null"`
	ExternalLeadID       string    `gorm:"type:varchar(36);uniqueIndex:idx_xiangwu_form_source_external;not null"`
	PayloadHash          string    `gorm:"type:varchar(64);not null"`
	Details              string    `gorm:"type:text;not null"`
	SourcePath           string    `gorm:"type:varchar(512);not null"`
	CompatibilityRequest bool      `gorm:"not null;default:false"`
	CreatedAt            time.Time `gorm:"not null"`
}

func (XiangwuFormSubmission) TableName() string { return "xiangwu_form_submissions" }

type XiangwuFormSubmissionRepository interface {
	Create(ctx context.Context, item *XiangwuFormSubmission) error
	Get(ctx context.Context, source, externalID string) (*XiangwuFormSubmission, error)
}

type GormXiangwuFormSubmissionRepository struct{ db *gorm.DB }

func NewGormXiangwuFormSubmissionRepository(db *gorm.DB) *GormXiangwuFormSubmissionRepository {
	return &GormXiangwuFormSubmissionRepository{db: db}
}

func (r *GormXiangwuFormSubmissionRepository) Create(ctx context.Context, item *XiangwuFormSubmission) error {
	return r.db.WithContext(ctx).Create(item).Error
}

func (r *GormXiangwuFormSubmissionRepository) Get(ctx context.Context, source, externalID string) (*XiangwuFormSubmission, error) {
	var item XiangwuFormSubmission
	err := r.db.WithContext(ctx).Where("source = ? AND external_lead_id = ?", source, externalID).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}
