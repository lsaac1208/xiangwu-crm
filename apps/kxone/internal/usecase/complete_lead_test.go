package usecase

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestCompleteLeadUseCaseRejectsInvalidDealAmount(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	leadRepo.leads[1] = &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusFollowing,
		Version:               1,
		OwnerUserID:           "sales1",
		AssignedSalesUserID:   "sales1",
		DispatchManagerUserID: "manager1",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}

	uc := NewCompleteLeadUseCase(leadRepo)
	_, err := uc.Execute(context.Background(), &CompleteLeadRequest{
		LeadID:      1,
		ActorUserID: "sales1",
		ActorRole:   string(types.UserRoleSales),
		Action:      "convert",
		DealAmount:  0,
	})
	if err != types.ErrLeadStatusInvalid {
		t.Fatalf("err = %v, want %v", err, types.ErrLeadStatusInvalid)
	}
	if leadRepo.updateCalls != 0 {
		t.Fatalf("updateCalls = %d, want 0", leadRepo.updateCalls)
	}
}

func TestCompleteLeadUseCaseRejectsTerminalLead(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	leadRepo.leads[1] = &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusConverted,
		Version:               1,
		OwnerUserID:           "sales1",
		AssignedSalesUserID:   "sales1",
		DispatchManagerUserID: "manager1",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}

	uc := NewCompleteLeadUseCase(leadRepo)
	_, err := uc.Execute(context.Background(), &CompleteLeadRequest{
		LeadID:      1,
		ActorUserID: "manager1",
		ActorRole:   string(types.UserRoleManager),
		Action:      "close",
		CloseReason: "重复线索",
	})
	if err != types.ErrLeadStatusInvalid {
		t.Fatalf("err = %v, want %v", err, types.ErrLeadStatusInvalid)
	}
}

func TestCompleteLeadUseCaseRejectsOversizedCloseReason(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	leadRepo.leads[1] = &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusClaimed,
		Version:               1,
		OwnerUserID:           "sales1",
		AssignedSalesUserID:   "sales1",
		DispatchManagerUserID: "manager1",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}

	uc := NewCompleteLeadUseCase(leadRepo)
	_, err := uc.Execute(context.Background(), &CompleteLeadRequest{
		LeadID:      1,
		ActorUserID: "sales1",
		ActorRole:   string(types.UserRoleSales),
		Action:      "close",
		CloseReason: strings.Repeat("长", maxCloseReasonLength+1),
	})
	if err != types.ErrLeadStatusInvalid {
		t.Fatalf("err = %v, want %v", err, types.ErrLeadStatusInvalid)
	}
}

// TestCompleteLeadUseCase_ClaimedToConverted Claimed 状态可直接签单（新增状态机边）。
func TestCompleteLeadUseCase_ClaimedToConverted(t *testing.T) {
	nextFollowupAt := time.Now().Add(24 * time.Hour)
	leadRepo := NewMockLeadRepository()
	leadRepo.leads[1] = &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusClaimed,
		Version:               1,
		OwnerUserID:           "sales1",
		AssignedSalesUserID:   "sales1",
		DispatchManagerUserID: "manager1",
		NextFollowupDueAt:     &nextFollowupAt,
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}

	uc := NewCompleteLeadUseCase(leadRepo)
	result, err := uc.Execute(context.Background(), &CompleteLeadRequest{
		LeadID:      1,
		ActorUserID: "sales1",
		ActorRole:   string(types.UserRoleSales),
		Action:      "convert",
		DealAmount:  99000,
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if result.Status != types.LeadStatusConverted {
		t.Fatalf("status = %s, want %s", result.Status, types.LeadStatusConverted)
	}
	if result.ConvertedAt == nil {
		t.Fatal("ConvertedAt 不应为 nil")
	}
	if result.BusinessStage != types.BusinessStageFinished || result.ResultStatus != types.LeadResultConverted {
		t.Fatalf("workflow state = %s/%s, want finished/converted", result.BusinessStage, result.ResultStatus)
	}
	if result.NextFollowupDueAt != nil {
		t.Fatalf("NextFollowupDueAt = %v, want nil after conversion", result.NextFollowupDueAt)
	}
	if leadRepo.updateCalls != 1 {
		t.Fatalf("updateCalls = %d, want 1", leadRepo.updateCalls)
	}
}

// TestCompleteLeadUseCase_PendingDispatchClose PendingDispatch 状态可直接关闭，状态机保证合法性。
func TestCompleteLeadUseCase_PendingDispatchClose(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	leadRepo.leads[1] = &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusPendingDispatch,
		Version:               1,
		DispatchManagerUserID: "manager1",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}

	uc := NewCompleteLeadUseCase(leadRepo)
	result, err := uc.Execute(context.Background(), &CompleteLeadRequest{
		LeadID:      1,
		ActorUserID: "manager1",
		ActorRole:   string(types.UserRoleManager),
		Action:      "close",
		CloseReason: "重复线索",
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if result.Status != types.LeadStatusClosed {
		t.Fatalf("status = %s, want %s", result.Status, types.LeadStatusClosed)
	}
	if result.ClosedAt == nil {
		t.Fatal("ClosedAt 不应为 nil")
	}
}

func TestCompleteLeadUseCaseWorkflowV2InvitationDirectConversionPermissions(t *testing.T) {
	tests := []struct {
		name    string
		actorID string
		role    types.UserRole
		wantErr error
	}{
		{name: "current inviter", actorID: "inviter-1", role: types.UserRoleInviter},
		{name: "other inviter", actorID: "inviter-2", role: types.UserRoleInviter, wantErr: types.ErrUnauthorized},
		{name: "manager outside original scope", actorID: "manager-2", role: types.UserRoleManager},
		{name: "administrator", actorID: "admin-1", role: types.UserRoleAdmin},
		{name: "dispatcher", actorID: "dispatcher-1", role: types.UserRoleDispatcher, wantErr: types.ErrUnauthorized},
		{name: "closer", actorID: "closer-1", role: types.UserRoleCloser, wantErr: types.ErrUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			leadRepo := NewMockLeadRepository()
			leadRepo.leads[1] = &types.Lead{
				ID: 1, Status: types.LeadStatusFollowing, BusinessStage: types.BusinessStageInvitation,
				ResultStatus: types.LeadResultActive, Version: 3, ProfileVersion: 1,
				OwnerUserID: "inviter-1", InviterUserID: "inviter-1", CurrentOwnerUserID: "inviter-1", CurrentOwnerRole: types.OwnerRoleInviter,
				DispatchManagerUserID: "manager-1", CreatedAt: time.Now(), UpdatedAt: time.Now(),
			}
			uc := NewCompleteLeadUseCase(leadRepo).WithWorkflow(stage3SettingsStub{workflowEnabled: true})
			result, err := uc.Execute(context.Background(), &CompleteLeadRequest{LeadID: 1, ActorUserID: tt.actorID, ActorRole: string(tt.role), Action: "convert", DealAmount: 16800})
			if err != tt.wantErr {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && (result == nil || result.Status != types.LeadStatusConverted || result.Version != 4 || result.NextFollowupDueAt != nil) {
				t.Fatalf("converted lead = %+v", result)
			}
		})
	}
}

func TestCompleteLeadUseCaseSingleStageManagerCanConvertAnyActiveLead(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	leadRepo.leads[1] = &types.Lead{
		ID: 1, Status: types.LeadStatusFollowing, BusinessStage: types.BusinessStageInvitation,
		ResultStatus: types.LeadResultActive, Version: 1, ProfileVersion: 1,
		OwnerUserID: "inviter-1", AssignedSalesUserID: "inviter-1", DispatchManagerUserID: "manager-1",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	uc := NewCompleteLeadUseCase(leadRepo).WithWorkflow(stage3SettingsStub{workflowEnabled: false})
	result, err := uc.Execute(context.Background(), &CompleteLeadRequest{LeadID: 1, ActorUserID: "manager-2", ActorRole: "manager", Action: "convert", DealAmount: 8800})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != types.LeadStatusConverted || result.BusinessStage != types.BusinessStageFinished {
		t.Fatalf("result = %+v", result)
	}
}

func TestCompleteLeadUseCaseWritesInvitationConversionAudit(t *testing.T) {
	db := newStage3TestDB(t)
	leads := repo.NewGormLeadRepository(db)
	activities := repo.NewGormLeadActivityRepository(db)
	tx := repo.NewGormTransactionManager(db)
	nextFollowup := time.Now().Add(time.Hour)
	lead := &types.Lead{
		LeadNo: "AUDIT-CONVERT-1", CustomerName: "审计客户", ProductName: "咨询项目",
		Status: types.LeadStatusFollowing, BusinessStage: types.BusinessStageInvitation,
		OwnershipState: types.OwnershipPrivate, ResultStatus: types.LeadResultActive,
		Version: 2, ProfileVersion: 1, OwnerUserID: "inviter-1", InviterUserID: "inviter-1",
		CurrentOwnerUserID: "inviter-1", CurrentOwnerRole: types.OwnerRoleInviter,
		NextFollowupDueAt: &nextFollowup, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	uc := NewCompleteLeadUseCase(leads).
		WithWorkflow(stage3SettingsStub{workflowEnabled: true}).
		WithAudit(activities, tx)
	result, err := uc.Execute(context.Background(), &CompleteLeadRequest{LeadID: lead.ID, ActorUserID: "inviter-1", ActorRole: "inviter", Action: "convert", DealAmount: 28800})
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != 3 || result.NextFollowupDueAt != nil {
		t.Fatalf("result = %+v", result)
	}
	records, err := activities.ListByLead(context.Background(), lead.ID)
	if err != nil || len(records) != 1 {
		t.Fatalf("activities = %+v, err = %v", records, err)
	}
	if records[0].ActivityType != types.LeadActivityInvitationDirectConverted {
		t.Fatalf("activity type = %s", records[0].ActivityType)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(records[0].MetadataJSON), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["conversion_path"] != "invitation_direct" || metadata["deal_amount"] != float64(28800) {
		t.Fatalf("metadata = %+v", metadata)
	}
}
