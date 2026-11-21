package usecase

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestClaimLeadWithDatabaseSettingsUsesSingleConnection(t *testing.T) {
	db, err := repo.NewDB(&config.DatabaseConfig{
		Driver: "sqlite", Database: filepath.Join(t.TempDir(), "claim-transaction.db"), MaxConns: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&repo.LeadModel{}, &repo.LeadDispatchRecordModel{}, &repo.LeadReminderRecordModel{}, &repo.SystemSettingModel{},
	); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if got := sqlDB.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("max open connections = %d, want 1", got)
	}

	leads := repo.NewGormLeadRepository(db)
	dispatches := repo.NewGormDispatchRecordRepository(db)
	reminders := repo.NewGormLeadReminderRepository(db)
	settingsRepo := repo.NewGormSystemSettingRepository(db)
	now := time.Now()
	if err := settingsRepo.Upsert(context.Background(), &types.SystemSetting{
		Key: runtime.SettingWorkflowV2Enabled, Value: "true", ValueType: "boolean", Group: "workflow", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	settings := runtime.NewDatabaseRuntimeSettings(settingsRepo, 0)
	lead := &types.Lead{
		LeadNo: "CLAIM-SINGLE-CONNECTION", CustomerName: "单连接领取测试",
		Status: types.LeadStatusAssignedPendingClaim, Version: 2,
		DispatchManagerUserID: "manager-1", AssignedSalesUserID: "inviter-1",
		AssignedInviterUserID: "inviter-1", OwnershipState: types.OwnershipAssignedPendingClaim,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}

	useCase := NewClaimLeadUseCase(leads, nil, nil, nil, nil).
		WithDispatchHistory(dispatches, repo.NewGormTransactionManager(db)).
		WithStage3Workflow(settings, settings, reminders)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := useCase.Execute(ctx, &ClaimLeadRequest{
		LeadID: lead.ID, SalesUserID: "inviter-1", Source: "h5",
	}); err != nil {
		t.Fatalf("claim with one DB connection: %v", err)
	}

	updated, err := leads.GetByID(context.Background(), lead.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != types.LeadStatusClaimed || updated.OwnerUserID != "inviter-1" || updated.NextFollowupDueAt == nil {
		t.Fatalf("updated lead = %+v", updated)
	}
	records, err := dispatches.ListByLeadID(context.Background(), lead.ID)
	if err != nil || len(records) != 1 || records[0].ActionType != "claim" {
		t.Fatalf("dispatch records = %+v, err=%v", records, err)
	}
	rows, err := reminders.ListDue(context.Background(), now.Add(7*24*time.Hour), 10)
	if err != nil || len(rows) != 3 {
		t.Fatalf("followup reminders = %d, err=%v", len(rows), err)
	}
}
