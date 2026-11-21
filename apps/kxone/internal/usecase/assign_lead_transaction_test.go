package usecase

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

func TestAssignLeadUsesTransactionUserBindingRepositoryWithSingleConnection(t *testing.T) {
	db, leads, dispatches, bindings := newAssignLeadTransactionFixture(t, 1, "inviter-1")
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if got := sqlDB.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("max open connections = %d, want 1", got)
	}
	lead := createPendingAssignmentLead(t, leads, "manager-1", "SINGLE-CONNECTION")
	useCase := NewAssignLeadUseCase(leads, dispatches, nil, nil, nil, nil).
		WithUserBindingRepository(bindings).
		WithTransactionManager(repo.NewGormTransactionManager(db))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := useCase.Execute(ctx, &AssignLeadRequest{
		LeadID: lead.ID, ManagerUserID: "manager-1", ActorRole: string(types.UserRoleManager),
		SalesUserID: "inviter-1", Remark: "single connection assignment",
	}); err != nil {
		t.Fatalf("assign with one DB connection: %v", err)
	}
	updated, err := leads.GetByID(context.Background(), lead.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != types.LeadStatusAssignedPendingClaim || updated.AssignedInviterUserID != "inviter-1" {
		t.Fatalf("updated lead = %+v", updated)
	}
	records, err := dispatches.ListByLeadID(context.Background(), lead.ID)
	if err != nil || len(records) != 1 {
		t.Fatalf("dispatch records = %d, err=%v", len(records), err)
	}
}

func TestAssignLeadConcurrentFirstAssignmentCreatesOneWinner(t *testing.T) {
	db, leads, dispatches, bindings := newAssignLeadTransactionFixture(t, 4, "inviter-a", "inviter-b")
	lead := createPendingAssignmentLead(t, leads, "manager-1", "CONCURRENT-ASSIGNMENT")
	useCase := NewAssignLeadUseCase(leads, dispatches, nil, nil, nil, nil).
		WithUserBindingRepository(bindings).
		WithTransactionManager(repo.NewGormTransactionManager(db))

	start := make(chan struct{})
	errorsByTarget := make(map[string]error, 2)
	var mutex sync.Mutex
	var wait sync.WaitGroup
	for _, target := range []string{"inviter-a", "inviter-b"} {
		wait.Add(1)
		go func(target string) {
			defer wait.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := useCase.Execute(ctx, &AssignLeadRequest{
				LeadID: lead.ID, ManagerUserID: "manager-1", ActorRole: string(types.UserRoleManager),
				SalesUserID: target, Remark: "concurrent first assignment",
			})
			mutex.Lock()
			errorsByTarget[target] = err
			mutex.Unlock()
		}(target)
	}
	close(start)
	wait.Wait()

	successes := 0
	conflicts := 0
	for target, err := range errorsByTarget {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, types.ErrLeadVersionConflict), errors.Is(err, types.ErrLeadStatusInvalid):
			conflicts++
		default:
			t.Fatalf("%s returned unexpected error: %v", target, err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d errors=%+v", successes, conflicts, errorsByTarget)
	}
	records, err := dispatches.ListByLeadID(context.Background(), lead.ID)
	if err != nil || len(records) != 1 {
		t.Fatalf("dispatch records = %d, err=%v", len(records), err)
	}
	updated, err := leads.GetByID(context.Background(), lead.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != lead.Version+1 || updated.Status != types.LeadStatusAssignedPendingClaim {
		t.Fatalf("updated lead = %+v", updated)
	}
}

func newAssignLeadTransactionFixture(t *testing.T, maxConnections int, targetUserIDs ...string) (*gorm.DB, *repo.GormLeadRepository, *repo.GormDispatchRecordRepository, *repo.GormUserBindingRepository) {
	t.Helper()
	db, err := repo.NewDB(&config.DatabaseConfig{
		Driver: "sqlite", Database: filepath.Join(t.TempDir(), "assign-transaction.db"), MaxConns: maxConnections,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&repo.LeadModel{}, &repo.LeadDispatchRecordModel{}, &repo.WeComUserBindingModel{}); err != nil {
		t.Fatal(err)
	}
	leads := repo.NewGormLeadRepository(db)
	dispatches := repo.NewGormDispatchRecordRepository(db)
	bindings := repo.NewGormUserBindingRepository(db)
	now := time.Now()
	for index, userID := range targetUserIDs {
		if err := bindings.Create(context.Background(), &types.WeComUserBinding{
			CRMUserID: int64(index + 1), Username: userID, WeComUserID: userID,
			EmployeeName: userID, Role: types.UserRoleInviter, Status: types.UserStatusActive,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return db, leads, dispatches, bindings
}

func createPendingAssignmentLead(t *testing.T, leads repo.LeadRepository, managerUserID, leadNo string) *types.Lead {
	t.Helper()
	now := time.Now()
	lead := &types.Lead{
		LeadNo: leadNo, CustomerName: "事务分配测试", Status: types.LeadStatusPendingDispatch,
		Version: 1, DispatchManagerUserID: managerUserID, CreatedAt: now, UpdatedAt: now,
	}
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	return lead
}
