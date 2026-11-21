package repo

import (
	"context"
	"errors"
	"time"

	"github.com/lsaac128/kxone/internal/security"
	"gorm.io/gorm"
)

func (r *GormUserBindingRepository) CreateSession(ctx context.Context, s *security.Session) error {
	return r.db.WithContext(ctx).Create(s).Error
}
func (r *GormUserBindingRepository) GetSession(ctx context.Context, sid string) (*security.Session, error) {
	var s security.Session
	err := r.db.WithContext(ctx).Where("sid = ?", sid).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &s, err
}
func (r *GormUserBindingRepository) RevokeSession(ctx context.Context, sid string, account int64, now time.Time) error {
	return r.db.WithContext(ctx).Model(&security.Session{}).Where("sid = ? AND account_id = ? AND revoked_at IS NULL", sid, account).Update("revoked_at", now.UTC()).Error
}
