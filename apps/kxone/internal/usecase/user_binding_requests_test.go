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

func TestReviewBindingRequestRollsBackBindingWhenRequestUpdateFails(t *testing.T) {
	db, err := repo.NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "review.db"), MaxConns: 4})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := repo.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	requestRepo := repo.NewGormUserBindingRequestRepository(db)
	bindingRepo := repo.NewGormUserBindingRepository(db)
	now := time.Now()
	request := &types.WeComUserBindingRequest{
		WeComUserID: "rollback-user", EmployeeName: "Rollback User",
		Status: types.BindingRequestStatusPending, CreatedAt: now, UpdatedAt: now,
	}
	if err := requestRepo.Create(context.Background(), request); err != nil {
		t.Fatalf("create request: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_binding_request_update
		BEFORE UPDATE ON crm_wecom_user_binding_requests
		BEGIN SELECT RAISE(ABORT, 'forced request update failure'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	uc := NewUserBindingRequestsUseCase(requestRepo, bindingRepo).
		WithTransactionManager(repo.NewGormTransactionManager(db))
	_, err = uc.Review(context.Background(), &ReviewBindingRequest{
		ID: request.ID, Action: "approve", Role: types.UserRoleSales, ReviewedBy: "admin1",
	})
	if err == nil {
		t.Fatal("review succeeded, want forced update failure")
	}
	binding, getErr := bindingRepo.GetByWeComUserID(context.Background(), request.WeComUserID)
	if getErr != nil {
		t.Fatalf("get binding: %v", getErr)
	}
	if binding != nil {
		t.Fatalf("binding was not rolled back: %+v", binding)
	}
	stored, getErr := requestRepo.GetByID(context.Background(), request.ID)
	if getErr != nil {
		t.Fatalf("get request: %v", getErr)
	}
	if stored == nil || stored.Status != types.BindingRequestStatusPending {
		t.Fatalf("request status after rollback = %+v", stored)
	}
}
