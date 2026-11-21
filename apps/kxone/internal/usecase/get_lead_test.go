package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestGetLead_ManagerCanViewSharedPendingDispatchDetail(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	now := time.Now()
	leadRepo.leads[1] = &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusPendingDispatch,
		Version:               1,
		DispatchManagerUserID: "manager1",
		CustomerMobile:        "13912345678",
		CustomerMobileMasked:  "139****5678",
		CreatedAt:             now,
		UpdatedAt:             now,
	}

	uc := NewGetLeadUseCase(leadRepo, &mockFollowupRepository{}, NewMockDispatchRecordRepository())
	result, err := uc.Execute(context.Background(), 1, "manager2", string(types.UserRoleManager))
	if err != nil {
		t.Fatalf("get shared pending dispatch lead: %v", err)
	}
	if result == nil || result.Lead == nil {
		t.Fatal("result lead is nil")
	}
	if result.Lead.CustomerMobile != "13912345678" {
		t.Fatalf("other manager full mobile = %q, want full mobile", result.Lead.CustomerMobile)
	}
	if result.Lead.CustomerMobileMasked != "139****5678" {
		t.Fatalf("masked mobile = %q", result.Lead.CustomerMobileMasked)
	}
}

func TestGetLead_ManagerCanViewOtherManagerAssignedLead(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	now := time.Now()
	leadRepo.leads[1] = &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusAssignedPendingClaim,
		Version:               1,
		DispatchManagerUserID: "manager1",
		AssignedSalesUserID:   "sales1",
		CreatedAt:             now,
		UpdatedAt:             now,
	}

	uc := NewGetLeadUseCase(leadRepo, &mockFollowupRepository{}, NewMockDispatchRecordRepository())
	result, err := uc.Execute(context.Background(), 1, "manager2", string(types.UserRoleManager))
	if err != nil {
		t.Fatalf("get other manager assigned lead: %v", err)
	}
	if result == nil || result.Lead == nil || result.Lead.ID != 1 {
		t.Fatalf("result = %+v, want lead 1", result)
	}
}

func TestGetLead_CloserCanOnlyViewDeliveredNegotiationLead(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	now := time.Now()
	leadRepo.leads[1] = &types.Lead{
		ID: 1, Status: types.LeadStatusFollowing, BusinessStage: types.BusinessStageInvitation,
		CloserUserID: "closer1", CreatedAt: now, UpdatedAt: now,
	}
	leadRepo.leads[2] = &types.Lead{
		ID: 2, Status: types.LeadStatusFollowing, BusinessStage: types.BusinessStageNegotiation,
		CloserUserID: "closer1", CurrentOwnerUserID: "closer1", CurrentOwnerRole: types.OwnerRoleCloser,
		CreatedAt: now, UpdatedAt: now,
	}
	uc := NewGetLeadUseCase(leadRepo, &mockFollowupRepository{}, NewMockDispatchRecordRepository())
	if _, err := uc.Execute(context.Background(), 1, "closer1", string(types.UserRoleCloser)); err != types.ErrUnauthorized {
		t.Fatalf("invitation lead err = %v, want unauthorized", err)
	}
	if _, err := uc.Execute(context.Background(), 2, "closer1", string(types.UserRoleCloser)); err != nil {
		t.Fatalf("delivered negotiation lead: %v", err)
	}
}

func TestGetLead_EnrichesActivityActorName(t *testing.T) {
	db := newStage3TestDB(t)
	leads := repo.NewGormLeadRepository(db)
	followups := repo.NewGormFollowupRepository(db)
	dispatches := repo.NewGormDispatchRecordRepository(db)
	activities := repo.NewGormLeadActivityRepository(db)
	bindings := repo.NewGormUserBindingRepository(db)
	now := time.Now()
	if err := bindings.Create(context.Background(), &types.WeComUserBinding{
		CRMUserID: 1, Username: "staff_f", WeComUserID: "staff_f",
		EmployeeName: "员工己", Role: types.UserRoleInviter, Status: types.UserStatusActive,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	lead := &types.Lead{
		LeadNo: "ACTOR-NAME-1", CustomerName: "客户", Status: types.LeadStatusConverted,
		BusinessStage: types.BusinessStageFinished, ResultStatus: types.LeadResultConverted,
		Version: 2, ProfileVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	if err := activities.Create(context.Background(), &types.LeadActivityRecord{
		LeadID: lead.ID, ActivityType: types.LeadActivityInvitationDirectConverted,
		ActorUserID: "staff_f", ActorRole: "inviter", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	uc := NewGetLeadUseCase(leads, followups, dispatches).
		WithUserBindingRepository(bindings).
		WithActivityRepository(activities)
	result, err := uc.Execute(context.Background(), lead.ID, "admin-1", string(types.UserRoleAdmin))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Activities) != 1 || result.Activities[0].ActorName != "员工己" {
		t.Fatalf("activities = %+v", result.Activities)
	}
}
