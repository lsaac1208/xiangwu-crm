package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestManagerTransferPreservesHistoryAndMovesResponsibility(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, status := range []types.LeadStatus{types.LeadStatusAssignedPendingClaim, types.LeadStatusClaimed, types.LeadStatusFollowing} {
			name := "single/" + string(status)
			if batch {
				name = "batch/" + string(status)
			}
			t.Run(name, func(t *testing.T) {
				db := newRoleAwareCreationTestDB(t)
				ctx := context.Background()
				bindings := repo.NewGormUserBindingRepository(db)
				createActiveBinding(t, bindings, "manager-a", types.UserRoleManager)
				createActiveBinding(t, bindings, "manager-b", types.UserRoleManager)
				createActiveBinding(t, bindings, "inviter-c", types.UserRoleInviter)
				leads := repo.NewGormLeadRepository(db)
				now := time.Now()
				lead := makePendingBatchLead("MANAGER-TRANSFER", now)
				lead.Status, lead.DispatchManagerUserID = status, "manager-a"
				lead.AssignedSalesUserID, lead.AssignedInviterUserID = "manager-a", "manager-a"
				lead.OwnerUserID, lead.InviterUserID, lead.CurrentOwnerUserID = "manager-a", "manager-a", "manager-a"
				lead.CurrentOwnerRole, lead.OwnershipState = types.OwnerRoleInviter, types.OwnershipPrivate
				lead.NextFollowupDueAt, lead.ClaimedAt, lead.InviterClaimedAt = &now, &now, &now
				lead.CreatedByUserID, lead.CreatedByRole = "manager-a", types.UserRoleManager
				if err := leads.Create(ctx, lead); err != nil {
					t.Fatal(err)
				}
				dispatches := repo.NewGormDispatchRecordRepository(db)
				tx := repo.NewGormTransactionManager(db)
				// Production has no legacy card repository. Transfers must still succeed.
				uc := NewReassignLeadUseCase(leads, dispatches, nil, nil).WithUserBindingRepository(bindings).WithTransactionManager(tx)
				if batch {
					_, err := NewBatchLeadsUseCase(tx).Reassign(ctx, &BatchLeadRequest{ActorUserID: "manager-a", ActorRole: "manager", TargetUserID: "manager-b", BusinessStage: "invitation", Items: []BatchLeadItem{{lead.ID, lead.Version, lead.ProfileVersion}}, Remark: "主管交接"})
					if err != nil {
						t.Fatal(err)
					}
				} else if err := uc.Execute(ctx, &ReassignLeadRequest{LeadID: lead.ID, ManagerUserID: "manager-a", ActorRole: "manager", NewSalesUserID: "manager-b", Remark: "主管交接"}); err != nil {
					t.Fatal(err)
				}
				updated, err := leads.GetByID(ctx, lead.ID)
				if err != nil {
					t.Fatal(err)
				}
				if updated.DispatchManagerUserID != "manager-b" || updated.AssignedInviterUserID != "manager-b" || updated.Status != types.LeadStatusAssignedPendingClaim || updated.CurrentOwnerUserID != "" || updated.InviterUserID != "" || updated.NextFollowupDueAt != nil || updated.InviterClaimedAt != nil || updated.Version != lead.Version+1 || updated.CreatedByUserID != "manager-a" {
					t.Fatalf("incorrect transfer: %+v", updated)
				}
				var records []repo.LeadDispatchRecordModel
				if err := db.Find(&records).Error; err != nil {
					t.Fatal(err)
				}
				if len(records) != 1 || records[0].ManagerUserID != "manager-a" || records[0].SalesUserID != "manager-b" || records[0].Remark != "主管交接" {
					t.Fatalf("history=%+v", records)
				}
				// The recipient can forward the lead again using its new manager scope.
				if status == types.LeadStatusFollowing {
					updated.Status = types.LeadStatusFollowing
					updated.CurrentOwnerUserID = "manager-b"
					updated.NextFollowupDueAt = &now
					updated.Version++
					if err := leads.Update(ctx, updated); err != nil {
						t.Fatal(err)
					}
				}
				if err := uc.Execute(ctx, &ReassignLeadRequest{LeadID: lead.ID, ManagerUserID: "manager-b", ActorRole: "manager", NewSalesUserID: "inviter-c"}); err != nil {
					t.Fatal(err)
				}
				updated, _ = leads.GetByID(ctx, lead.ID)
				if updated.DispatchManagerUserID != "manager-b" || updated.AssignedInviterUserID != "inviter-c" || updated.NextFollowupDueAt != nil {
					t.Fatalf("forward=%+v", updated)
				}
			})
		}
	}
}

func TestManagerInvitationTransferCannotChangeNegotiationOwner(t *testing.T) {
	uc := NewReassignLeadUseCase(nil, nil, nil, nil)
	for _, role := range []string{"manager", "admin"} {
		for _, status := range []types.LeadStatus{types.LeadStatusClaimed, types.LeadStatusFollowing} {
			lead := &types.Lead{BusinessStage: types.BusinessStageNegotiation, Status: status, DispatchManagerUserID: "manager-a", AssignedSalesUserID: "closer-a"}
			err := uc.validateReassignment(lead, &ReassignLeadRequest{ManagerUserID: "manager-a", ActorRole: role, NewSalesUserID: "manager-b"})
			if err != types.ErrLeadStatusInvalid {
				t.Fatalf("role=%s status=%s error=%v", role, status, err)
			}
		}
	}
}
