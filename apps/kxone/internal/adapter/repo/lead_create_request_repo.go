package repo

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Durable deduplication stores identifiers and a keyed fingerprint, never contacts.
type LeadCreateRequest struct {
	ID          int64     `gorm:"primaryKey"`
	AccountID   int64     `gorm:"uniqueIndex:uq_crm_lead_create,priority:1;not null"`
	Endpoint    string    `gorm:"type:varchar(64);uniqueIndex:uq_crm_lead_create,priority:2;not null"`
	RequestKey  string    `gorm:"type:varchar(36);uniqueIndex:uq_crm_lead_create,priority:3;not null"`
	Fingerprint string    `gorm:"type:varchar(64);not null"`
	LeadID      int64     `gorm:"not null"`
	Outcome     string    `gorm:"type:varchar(32);not null"`
	CreatedAt   time.Time `gorm:"not null"`
}

func (LeadCreateRequest) TableName() string { return "crm_lead_create_requests" }

type LeadCreateRequestRepository interface {
	Reserve(context.Context, *LeadCreateRequest) (*LeadCreateRequest, bool, error)
	Complete(context.Context, int64, int64, string) error
}
type GormLeadCreateRequestRepository struct{ db *gorm.DB }

func (r *gormTransactionRepositories) LeadCreateRequests() LeadCreateRequestRepository {
	return &GormLeadCreateRequestRepository{db: r.db}
}
func (r *GormLeadCreateRequestRepository) Reserve(ctx context.Context, row *LeadCreateRequest) (*LeadCreateRequest, bool, error) {
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "account_id"}, {Name: "endpoint"}, {Name: "request_key"}}, DoNothing: true}).Create(row)
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 1 {
		return row, true, nil
	}
	var prior LeadCreateRequest
	err := r.db.WithContext(ctx).Where("account_id = ? AND endpoint = ? AND request_key = ?", row.AccountID, row.Endpoint, row.RequestKey).First(&prior).Error
	return &prior, false, err
}
func (r *GormLeadCreateRequestRepository) Complete(ctx context.Context, id, leadID int64, outcome string) error {
	return r.db.WithContext(ctx).Model(&LeadCreateRequest{}).Where("id = ?", id).Updates(map[string]any{"lead_id": leadID, "outcome": outcome}).Error
}
