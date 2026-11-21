package repo

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestGormUserBindingRepositoryGetByWeComUserID(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{
		Driver:   "sqlite",
		Database: filepath.Join(t.TempDir(), "bindings.db"),
		MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewGormUserBindingRepository(db)
	ctx := context.Background()
	now := time.Now()

	active := &types.WeComUserBinding{
		CRMUserID:    1,
		WeComUserID:  "active_user",
		EmployeeName: "Active",
		Role:         types.UserRoleSales,
		Status:       types.UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := repo.Create(ctx, active); err != nil {
		t.Fatalf("create active: %v", err)
	}

	disabled := &types.WeComUserBinding{
		CRMUserID:    2,
		WeComUserID:  "disabled_user",
		EmployeeName: "Disabled",
		Role:         types.UserRoleSales,
		Status:       types.UserStatusDisabled,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := repo.Create(ctx, disabled); err != nil {
		t.Fatalf("create disabled: %v", err)
	}

	gotActive, err := repo.GetByWeComUserID(ctx, "active_user")
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if gotActive == nil || gotActive.Status != types.UserStatusActive {
		t.Fatalf("active binding = %#v", gotActive)
	}

	gotDisabled, err := repo.GetByWeComUserID(ctx, "disabled_user")
	if err != nil {
		t.Fatalf("get disabled: %v", err)
	}
	if gotDisabled == nil || gotDisabled.Status != types.UserStatusDisabled {
		t.Fatalf("disabled binding = %#v", gotDisabled)
	}

	missing, err := repo.GetByWeComUserID(ctx, "missing_user")
	if err != nil {
		t.Fatalf("get missing: %v", err)
	}
	if missing != nil {
		t.Fatalf("missing binding = %#v", missing)
	}
}

func TestGormUserBindingRepositoryUpdatesOnlyActiveUsersPassword(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "password.db"), MaxConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repository := NewGormUserBindingRepository(db)
	now := time.Now()
	user := &types.WeComUserBinding{CRMUserID: 1, Username: "member", WeComUserID: "member", PasswordHash: "old-hash", Role: types.UserRoleSales, Status: types.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := repository.Create(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	if err := repository.ChangePassword(t.Context(), user.ID, user.AuthVersion, "old-hash", "new-hash"); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.GetByUsername(t.Context(), "member")
	if err != nil || loaded == nil || loaded.PasswordHash != "new-hash" {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
}

func TestGormUserBindingRepositoryConcurrentGeneratedCRMUserIDsAreUnique(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{
		Driver:   "sqlite",
		Database: filepath.Join(t.TempDir(), "concurrent-bindings.db"),
		MaxConns: 20,
	})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repository := NewGormUserBindingRepository(db)

	const count = 24
	bindings := make([]*types.WeComUserBinding, count)
	errs := make([]error, count)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			now := time.Now()
			binding := &types.WeComUserBinding{
				WeComUserID: fmt.Sprintf("concurrent-%02d", i),
				Role:        types.UserRoleSales, Status: types.UserStatusActive,
				CreatedAt: now, UpdatedAt: now,
			}
			errs[i] = repository.Create(context.Background(), binding)
			bindings[i] = binding
		}(i)
	}
	close(start)
	wg.Wait()

	seen := make(map[int64]struct{}, count)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("create binding %d: %v", i, err)
		}
		binding := bindings[i]
		if binding.ID <= 0 || binding.CRMUserID != binding.ID {
			t.Fatalf("binding %d has id=%d crm_user_id=%d", i, binding.ID, binding.CRMUserID)
		}
		if _, exists := seen[binding.CRMUserID]; exists {
			t.Fatalf("duplicate crm_user_id %d", binding.CRMUserID)
		}
		seen[binding.CRMUserID] = struct{}{}
	}
}
