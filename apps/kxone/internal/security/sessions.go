package security

import (
	"context"
	"time"
)

// Session metadata never contains either credential. Expiry is absolute.
type Session struct {
	ID          string    `gorm:"column:sid;primaryKey;type:varchar(36)"`
	AccountID   int64     `gorm:"not null;index"`
	AuthVersion int64     `gorm:"not null"`
	CreatedAt   time.Time `gorm:"not null"`
	ExpiresAt   time.Time `gorm:"not null;index"`
	RevokedAt   *time.Time
}

func (Session) TableName() string { return "crm_account_sessions" }

type SessionStore interface {
	CreateSession(context.Context, *Session) error
	GetSession(context.Context, string) (*Session, error)
	RevokeSession(context.Context, string, int64, time.Time) error
}
