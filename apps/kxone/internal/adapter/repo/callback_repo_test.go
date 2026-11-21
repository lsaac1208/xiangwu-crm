package repo

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestGormCallbackLogRepositoryDeduplicatesTaskUserEvent(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{
		Driver:   "sqlite",
		Database: filepath.Join(t.TempDir(), "callbacks.db"),
		MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewGormCallbackLogRepository(db)
	ctx := context.Background()
	callback := &types.WeComCallbackLog{
		TaskID:        "l42_spc",
		FromUserID:    "sales1",
		EventKey:      "CLAIM_LEAD",
		RawPayload:    "<xml/>",
		ProcessStatus: "processing",
	}
	if err := repo.Create(ctx, callback); err != nil {
		t.Fatalf("create callback: %v", err)
	}

	exists, err := repo.ExistsByTaskAndUser(ctx, "l42_spc", "sales1", "CLAIM_LEAD")
	if err != nil {
		t.Fatalf("exists callback: %v", err)
	}
	if !exists {
		t.Fatalf("callback dedupe key should exist")
	}

	duplicate := &types.WeComCallbackLog{
		TaskID:        "l42_spc",
		FromUserID:    "sales1",
		EventKey:      "CLAIM_LEAD",
		RawPayload:    "<xml/>",
		ProcessStatus: "processing",
	}
	if err := repo.Create(ctx, duplicate); !errors.Is(err, types.ErrDuplicateCallback) {
		t.Fatalf("duplicate create err = %v, want %v", err, types.ErrDuplicateCallback)
	}
}
