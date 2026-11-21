package usecase

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestValidateAssignableInviterAcceptsInviterAndLegacySalesOnly(t *testing.T) {
	db, err := repo.NewDB(&config.DatabaseConfig{
		Driver: "sqlite", Database: filepath.Join(t.TempDir(), "inviter-role.db"), MaxConns: 1,
	})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := db.AutoMigrate(&repo.WeComUserBindingModel{}); err != nil {
		t.Fatalf("migrate bindings: %v", err)
	}
	repository := repo.NewGormUserBindingRepository(db)
	now := time.Now()
	bindings := []*types.WeComUserBinding{
		{CRMUserID: 1, WeComUserID: "inviter-1", Role: types.UserRoleInviter, Status: types.UserStatusActive, CreatedAt: now, UpdatedAt: now},
		{CRMUserID: 2, WeComUserID: "sales-1", Role: types.UserRoleSales, Status: types.UserStatusActive, CreatedAt: now, UpdatedAt: now},
		{CRMUserID: 3, WeComUserID: "closer-1", Role: types.UserRoleCloser, Status: types.UserStatusActive, CreatedAt: now, UpdatedAt: now},
		{CRMUserID: 4, WeComUserID: "inviter-off", Role: types.UserRoleInviter, Status: types.UserStatusDisabled, CreatedAt: now, UpdatedAt: now},
	}
	for _, binding := range bindings {
		if err := repository.Create(context.Background(), binding); err != nil {
			t.Fatalf("create binding %s: %v", binding.WeComUserID, err)
		}
	}

	for _, userID := range []string{"inviter-1", "sales-1"} {
		if err := validateAssignableInviter(context.Background(), repository, userID); err != nil {
			t.Fatalf("%s should be assignable: %v", userID, err)
		}
	}
	for _, userID := range []string{"closer-1", "inviter-off", "missing"} {
		if err := validateAssignableInviter(context.Background(), repository, userID); err != types.ErrInvalidInviter {
			t.Fatalf("%s error = %v, want ErrInvalidInviter", userID, err)
		}
	}
}
