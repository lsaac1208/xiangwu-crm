package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

type failingGroupRobotNotificationRepository struct {
	repo.GroupRobotNotificationRepository
	notification *types.GroupRobotNotification
}

func (r *failingGroupRobotNotificationRepository) Create(_ context.Context, notification *types.GroupRobotNotification) (bool, error) {
	copyNotification := *notification
	r.notification = &copyNotification
	return false, errors.New("notification ledger unavailable")
}

func TestAutoReturnDoesNotRollbackWhenNotificationRecordingFails(t *testing.T) {
	db := newStage3TestDB(t)
	leads := repo.NewGormLeadRepository(db)
	pools := repo.NewGormLeadPoolRepository(db)
	settings := stage3SettingsStub{workflowEnabled: true, poolEnabled: true, autoPoolEnabled: true}
	recorder := &failingGroupRobotNotificationRepository{}
	useCase := NewLeadPoolUseCase(leads, pools, settings, settings).
		WithDependencies(nil, repo.NewGormTransactionManager(db)).
		WithGroupRobotNotifications(recorder)

	now := time.Now()
	assignedAt := now.Add(-48 * time.Hour)
	lead := stage3OwnedLead(600, "inviter-fallback")
	lead.Status = types.LeadStatusAssignedPendingClaim
	lead.OwnershipState = types.OwnershipAssignedPendingClaim
	lead.CurrentOwnerUserID = ""
	lead.OwnerUserID = ""
	lead.InviterUserID = ""
	lead.InviterAssignedAt = &assignedAt
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}

	if err := useCase.AutoReturnOverdue(context.Background(), now, 20); err != nil {
		t.Fatalf("auto return must not fail with notification recording: %v", err)
	}
	updated, err := leads.GetByID(context.Background(), lead.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.OwnershipState != types.OwnershipPublicPool || updated.Status != types.LeadStatusPublicPool {
		t.Fatalf("business transition was rolled back: %+v", updated)
	}
	if recorder.notification == nil || recorder.notification.EventType != types.GroupRobotEventPoolAutoReturn {
		t.Fatalf("pool notification was not attempted: %+v", recorder.notification)
	}
	if recorder.notification.BusinessVersion != updated.Version ||
		recorder.notification.EventKey != fmt.Sprintf("pool-auto-return:%d:v%d", lead.ID, updated.Version) {
		t.Fatalf("notification used stale pool version: notification=%+v lead_version=%d", recorder.notification, updated.Version)
	}
	var payload struct {
		PreviousOwnerID string `json:"previous_owner_user_id"`
	}
	if err := json.Unmarshal([]byte(recorder.notification.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.PreviousOwnerID != "inviter-fallback" {
		t.Fatalf("previous owner fallback = %q", payload.PreviousOwnerID)
	}
}
