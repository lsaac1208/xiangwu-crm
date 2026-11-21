package worker

import (
	"context"
	"time"

	"github.com/hibiken/asynq"
	"github.com/lsaac128/kxone/internal/adapter/repo"
)

const TypeSecurityCleanup = "security:metadata-cleanup"

func (s *Server) WithSecurityCleanup(repository *repo.GormAccessAuditRepository) *Server {
	s.securityCleanup = repository != nil
	if repository != nil {
		s.mux.HandleFunc(TypeSecurityCleanup, func(ctx context.Context, _ *asynq.Task) error { return repository.Cleanup(ctx, time.Now().UTC()) })
	}
	return s
}
