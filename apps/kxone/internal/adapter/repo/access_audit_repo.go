package repo

import (
	"context"
	"strconv"
	"time"

	"github.com/lsaac128/kxone/internal/security"
	"gorm.io/gorm"
)

type AccessAudit struct {
	ID          int64     `json:"id" gorm:"primaryKey"`
	RequestID   string    `json:"request_id" gorm:"type:varchar(36);not null;index"`
	AccountID   int64     `json:"account_id" gorm:"not null;index"`
	Role        string    `json:"role" gorm:"type:varchar(32);not null"`
	Action      string    `json:"action" gorm:"type:varchar(128);not null"`
	Outcome     string    `json:"outcome" gorm:"type:varchar(16);not null"`
	ObjectIDs   string    `json:"object_ids" gorm:"type:text;not null"`
	ObjectCount int       `json:"object_count" gorm:"not null"`
	Aggregate   bool      `json:"aggregate" gorm:"not null"`
	CreatedAt   time.Time `json:"created_at" gorm:"not null;index"`
}

func (AccessAudit) TableName() string { return "crm_access_audits" }

type AccessAuditFilter struct {
	AccountID, LeadID int64
	Start, End        *time.Time
	Offset, Limit     int
}
type GormAccessAuditRepository struct{ db *gorm.DB }

func NewGormAccessAuditRepository(db *gorm.DB) *GormAccessAuditRepository {
	return &GormAccessAuditRepository{db}
}
func (r *GormAccessAuditRepository) Create(ctx context.Context, a *AccessAudit) error {
	return r.db.WithContext(ctx).Create(a).Error
}
func (r *GormAccessAuditRepository) List(ctx context.Context, f AccessAuditFilter) ([]AccessAudit, int64, error) {
	q := r.db.WithContext(ctx).Model(&AccessAudit{})
	if f.AccountID > 0 {
		q = q.Where("account_id = ?", f.AccountID)
	}
	if f.LeadID > 0 {
		q = q.Where("object_ids LIKE ?", "%\"lead:"+strconv.FormatInt(f.LeadID, 10)+"\"%")
	}
	if f.Start != nil {
		q = q.Where(absoluteTimeComparison(r.db, "created_at", ">="), databaseTimeBoundary(r.db, *f.Start))
	}
	if f.End != nil {
		q = q.Where(absoluteTimeComparison(r.db, "created_at", "<"), databaseTimeBoundary(r.db, *f.End))
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return nil, 0, err
	}
	rows := []AccessAudit{}
	err := q.Order("id DESC").Offset(f.Offset).Limit(f.Limit).Find(&rows).Error
	return rows, count, err
}
func (r *GormAccessAuditRepository) Cleanup(ctx context.Context, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(absoluteTimeComparison(tx, "created_at", "<"), databaseTimeBoundary(tx, now.AddDate(0, 0, -90))).Delete(&AccessAudit{}).Error; err != nil {
			return err
		}
		return tx.Where(absoluteTimeComparison(tx, "expires_at", "<"), databaseTimeBoundary(tx, now)).Delete(&security.Session{}).Error
	})
}
