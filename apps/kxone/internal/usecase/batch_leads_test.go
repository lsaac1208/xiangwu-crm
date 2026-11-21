package usecase

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestBatchAssignRollsBackEntireBatchOnVersionConflict(t *testing.T) {
	db, err := repo.NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "batch.db"), MaxConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&repo.LeadModel{}, &repo.LeadDispatchRecordModel{}, &repo.WeComUserBindingModel{}, &repo.GroupRobotNotificationModel{}); err != nil {
		t.Fatal(err)
	}
	bindings := repo.NewGormUserBindingRepository(db)
	now := time.Now()
	if err := bindings.Create(context.Background(), &types.WeComUserBinding{CRMUserID: 1, WeComUserID: "inviter-1", Username: "inviter1", EmployeeName: "邀约一", Role: types.UserRoleInviter, Status: types.UserStatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	leads := repo.NewGormLeadRepository(db)
	first := makePendingBatchLead("BATCH-1", now)
	second := makePendingBatchLead("BATCH-2", now)
	if err := leads.Create(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := leads.Create(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	uc := NewBatchLeadsUseCase(repo.NewGormTransactionManager(db))
	_, err = uc.Assign(context.Background(), &BatchLeadRequest{ActorUserID: "dispatcher-1", ActorRole: string(types.UserRoleDispatcher), TargetUserID: "inviter-1", Items: []BatchLeadItem{{LeadID: first.ID, Version: first.Version, ProfileVersion: first.ProfileVersion}, {LeadID: second.ID, Version: second.Version + 1, ProfileVersion: second.ProfileVersion}}})
	var conflict *BatchLeadConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("err=%v", err)
	}
	for _, id := range []int64{first.ID, second.ID} {
		current, _ := leads.GetByID(context.Background(), id)
		if current.Status != types.LeadStatusPendingDispatch {
			t.Fatalf("lead %d changed despite rollback: %s", id, current.Status)
		}
	}
	var count int64
	_ = db.Model(&repo.LeadDispatchRecordModel{}).Count(&count).Error
	if count != 0 {
		t.Fatalf("dispatch count=%d", count)
	}
}

func TestBatchAssignCreatesIndependentHistoryAndNotifications(t *testing.T) {
	db, _ := repo.NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "batch-success.db"), MaxConns: 1})
	_ = db.AutoMigrate(&repo.LeadModel{}, &repo.LeadDispatchRecordModel{}, &repo.WeComUserBindingModel{}, &repo.GroupRobotNotificationModel{})
	now := time.Now()
	bindings := repo.NewGormUserBindingRepository(db)
	_ = bindings.Create(context.Background(), &types.WeComUserBinding{CRMUserID: 1, WeComUserID: "manager-1", Username: "manager1", EmployeeName: "主管", Role: types.UserRoleManager, Status: types.UserStatusActive, CreatedAt: now, UpdatedAt: now})
	leads := repo.NewGormLeadRepository(db)
	a, b := makePendingBatchLead("OK-1", now), makePendingBatchLead("OK-2", now)
	_ = leads.Create(context.Background(), a)
	_ = leads.Create(context.Background(), b)
	result, err := NewBatchLeadsUseCase(repo.NewGormTransactionManager(db)).Assign(context.Background(), &BatchLeadRequest{ActorUserID: "admin-1", ActorRole: string(types.UserRoleAdmin), TargetUserID: "manager-1", Items: []BatchLeadItem{{a.ID, a.Version, a.ProfileVersion}, {b.ID, b.Version, b.ProfileVersion}}})
	if err != nil || result.Updated != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	var dispatches, notifications int64
	_ = db.Model(&repo.LeadDispatchRecordModel{}).Count(&dispatches).Error
	_ = db.Model(&repo.GroupRobotNotificationModel{}).Count(&notifications).Error
	if dispatches != 2 || notifications != 2 {
		t.Fatalf("dispatches=%d notifications=%d", dispatches, notifications)
	}
}

func makePendingBatchLead(no string, now time.Time) *types.Lead {
	return &types.Lead{LeadNo: no, CustomerName: no, Status: types.LeadStatusPendingDispatch, BusinessStage: types.BusinessStageInvitation, OwnershipState: types.OwnershipPendingAssignment, ResultStatus: types.LeadResultActive, Version: 1, ProfileVersion: 1, CreatedAt: now, UpdatedAt: now}
}
