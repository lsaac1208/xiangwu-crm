package repo

import (
	"context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// FollowupRequest is a durable receipt. Neither remarks nor contact data are stored.
type FollowupRequest struct {
	ID          int64     `gorm:"primaryKey"`
	AccountID   int64     `gorm:"not null;uniqueIndex:uq_crm_followup_request,priority:1"`
	LeadID      int64     `gorm:"not null;uniqueIndex:uq_crm_followup_request,priority:2"`
	RequestKey  string    `gorm:"type:varchar(36);not null;uniqueIndex:uq_crm_followup_request,priority:3"`
	Fingerprint string    `gorm:"type:varchar(64);not null"`
	FollowupID  int64     `gorm:"not null"`
	CreatedAt   time.Time `gorm:"not null"`
}

func (FollowupRequest) TableName() string { return "crm_followup_requests" }

type FollowupRequestRepository interface {
	Reserve(context.Context, *FollowupRequest) (*FollowupRequest, bool, error)
	Complete(context.Context, int64, int64) error
}
type GormFollowupRequestRepository struct{ db *gorm.DB }

func (r *gormTransactionRepositories) FollowupRequests() FollowupRequestRepository {
	return &GormFollowupRequestRepository{db: r.db}
}
func (r *GormFollowupRequestRepository) Reserve(ctx context.Context, row *FollowupRequest) (*FollowupRequest, bool, error) {
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "account_id"}, {Name: "lead_id"}, {Name: "request_key"}}, DoNothing: true}).Create(row)
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 1 {
		return row, true, nil
	}
	var prior FollowupRequest
	err := r.db.WithContext(ctx).Where("account_id = ? AND lead_id = ? AND request_key = ?", row.AccountID, row.LeadID, row.RequestKey).First(&prior).Error
	return &prior, false, err
}
func (r *GormFollowupRequestRepository) Complete(ctx context.Context, id, followupID int64) error {
	return r.db.WithContext(ctx).Model(&FollowupRequest{}).Where("id = ?", id).Update("followup_id", followupID).Error
}
