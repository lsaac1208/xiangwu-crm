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

func TestGetSLAStatsManagerCannotQueryAnotherManager(t *testing.T) {
	db, err := repo.NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "sla.db"), MaxConns: 4})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := repo.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	leadRepo := repo.NewGormLeadRepository(db)
	now := time.Now()
	for i, managerID := range []string{"manager1", "manager2"} {
		status := types.LeadStatusPendingDispatch
		if managerID == "manager2" {
			status = types.LeadStatusFollowing
		}
		lead := &types.Lead{
			LeadNo: "SLA-" + managerID, CustomerName: managerID,
			Status: status, DispatchManagerUserID: managerID,
			CreatedAt: now.Add(time.Duration(i) * time.Second), UpdatedAt: now,
		}
		if err := leadRepo.Create(context.Background(), lead); err != nil {
			t.Fatalf("create lead: %v", err)
		}
	}

	result, err := NewGetSLAStatsUseCase(leadRepo).Execute(context.Background(), &GetSLAStatsRequest{
		CurrentUserID: "manager1", Role: string(types.UserRoleManager),
		Limit: 100,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.TotalCount != 2 || len(result.LeadSLAs) != 2 {
		t.Fatalf("manager result = %+v, want all leads", result)
	}
}
