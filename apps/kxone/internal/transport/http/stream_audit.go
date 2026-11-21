package http

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/security"
)

// StreamAccessAudit runs after resource authorization and before any SSE bytes.
func StreamAccessAudit(store accessAuditWriter) func(*gin.Context, string, string) error {
	return func(c *gin.Context, id, outcome string) error {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return err
		}
		requestID := c.GetString("request_id")
		if requestID == "" {
			requestID = uuid.NewString()
		}
		if store == nil {
			return errors.New("access audit unavailable")
		}
		return store.Create(c.Request.Context(), &repo.AccessAudit{RequestID: requestID, AccountID: c.GetInt64("account_id"), Role: c.GetString("role"), Action: "GET /api/agent/runs/:id/events", Outcome: outcome, ObjectIDs: `["resource:` + parsed.String() + `"]`, ObjectCount: 1, CreatedAt: time.Now().UTC()})
	}
}

// StreamSessionValid never writes JSON into an already-started event stream.
func StreamSessionValid(store security.SessionStore) func(*gin.Context) bool {
	return func(c *gin.Context) bool {
		return liveStreamSession(c.Request.Context(), store, c.GetString("session_id"), c.GetInt64("account_id"), c.GetInt64("auth_version"))
	}
}
func liveStreamSession(ctx context.Context, store security.SessionStore, sid string, account, version int64) bool {
	if store == nil || sid == "" {
		return false
	}
	s, err := store.GetSession(ctx, sid)
	return err == nil && s != nil && s.AccountID == account && s.AuthVersion == version && s.RevokedAt == nil && s.ExpiresAt.After(time.Now())
}
