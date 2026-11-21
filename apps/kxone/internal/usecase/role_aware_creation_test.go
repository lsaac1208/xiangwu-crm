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
	"gorm.io/gorm"
)

func newRoleAwareCreationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repo.NewDB(&config.DatabaseConfig{
		Driver: "sqlite", Database: filepath.Join(t.TempDir(), "role-aware-create.db"), MaxConns: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func createActiveBinding(t *testing.T, repository repo.UserBindingRepository, userID string, role types.UserRole) {
	t.Helper()
	now := time.Now()
	if err := repository.Create(context.Background(), &types.WeComUserBinding{
		WeComUserID: userID, EmployeeName: userID + "-真实姓名", Role: role,
		Status: types.UserStatusActive, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
}

func newRoleAwareCreateUseCase(db *gorm.DB) *CreateLeadUseCase {
	leads := repo.NewGormLeadRepository(db)
	return NewCreateLeadUseCase(leads, nil).WithRoleAwareCreation(
		repo.NewGormUserBindingRepository(db),
		repo.NewGormDispatchRecordRepository(db),
		repo.NewGormLeadReminderRepository(db),
		repo.NewGormTransactionManager(db),
	)
}

func TestRoleAwareCreateInviterAndLegacySalesStartFollowing(t *testing.T) {
	for _, role := range []types.UserRole{types.UserRoleInviter, types.UserRoleSales} {
		t.Run(string(role), func(t *testing.T) {
			db := newRoleAwareCreationTestDB(t)
			userID := string(role) + "-1"
			response, err := newRoleAwareCreateUseCase(db).Execute(context.Background(), &CreateLeadRequest{
				CustomerName: "自建客户", CustomerMobile: "13912345678", ActorUserID: userID, ActorRole: string(role),
			})
			if err != nil {
				t.Fatal(err)
			}
			lead := response.Lead
			if response.Outcome != "self_following" || lead.Status != types.LeadStatusFollowing {
				t.Fatalf("outcome=%s status=%s", response.Outcome, lead.Status)
			}
			if lead.CreatedByUserID != userID || lead.CreatedByRole != role || lead.CurrentOwnerUserID != userID || lead.AssignedInviterUserID != userID {
				t.Fatalf("unexpected ownership: %+v", lead)
			}
			if lead.NextFollowupDueAt == nil || lead.ClaimedAt == nil || lead.AssignedAt == nil {
				t.Fatalf("following timestamps/due missing: %+v", lead)
			}

			var dispatches []repo.LeadDispatchRecordModel
			if err := db.Where("lead_id = ?", lead.ID).Find(&dispatches).Error; err != nil {
				t.Fatal(err)
			}
			if len(dispatches) != 1 || dispatches[0].ActionType != "self_create" {
				t.Fatalf("dispatches=%+v", dispatches)
			}
			var reminderCount int64
			if err := db.Model(&repo.LeadReminderRecordModel{}).Where("lead_id = ?", lead.ID).Count(&reminderCount).Error; err != nil {
				t.Fatal(err)
			}
			if reminderCount != 3 {
				t.Fatalf("reminder count=%d, want 3", reminderCount)
			}
		})
	}
}

func TestRoleAwareCreateManagerAndAdminRemainPendingDispatch(t *testing.T) {
	for _, role := range []types.UserRole{types.UserRoleManager, types.UserRoleAdmin} {
		t.Run(string(role), func(t *testing.T) {
			db := newRoleAwareCreationTestDB(t)
			response, err := newRoleAwareCreateUseCase(db).Execute(context.Background(), &CreateLeadRequest{
				CustomerName: "待派发客户", CustomerMobile: "13912345678", ActorUserID: string(role) + "-1", ActorRole: string(role),
			})
			if err != nil {
				t.Fatal(err)
			}
			if response.Outcome != "pending_dispatch" || response.Lead.Status != types.LeadStatusPendingDispatch || response.Lead.CurrentOwnerUserID != "" {
				t.Fatalf("response=%+v", response)
			}
		})
	}
}

func TestRoleAwareCreatePersistsContactAndRating(t *testing.T) {
	db := newRoleAwareCreationTestDB(t)
	response, err := newRoleAwareCreateUseCase(db).Execute(context.Background(), &CreateLeadRequest{
		CustomerName: "资料完整客户", CustomerMobile: "13800138000", CustomerWechat: "wx-customer",
		City: "武汉市", ProductName: "咨询学费", SourceChannel: "线下到访", SourceCampaign: "开放日",
		LeadLevel: "high", StarRating: 5, ActorUserID: "manager-1", ActorRole: string(types.UserRoleManager),
	})
	if err != nil {
		t.Fatal(err)
	}
	lead := response.Lead
	if lead.CustomerWechat != "wx-customer" || lead.CustomerWechatMasked == "" || lead.StarRating != 5 {
		t.Fatalf("contact/rating not persisted: %+v", lead)
	}
	if lead.City != "武汉市" || lead.ProductName != "咨询学费" || lead.SourceCampaign != "开放日" {
		t.Fatalf("lead profile not persisted: %+v", lead)
	}

	_, err = newRoleAwareCreateUseCase(newRoleAwareCreationTestDB(t)).Execute(context.Background(), &CreateLeadRequest{
		CustomerName: "非法星级", StarRating: 6, ActorUserID: "manager-1", ActorRole: string(types.UserRoleManager),
	})
	if !errors.Is(err, types.ErrInvalidStarRating) {
		t.Fatalf("error=%v, want ErrInvalidStarRating", err)
	}
}

func TestRoleAwareCreateDerivesContactMasksServerSide(t *testing.T) {
	db := newRoleAwareCreationTestDB(t)
	response, err := newRoleAwareCreateUseCase(db).Execute(context.Background(), &CreateLeadRequest{
		CustomerName: "脱敏不可绕过", CustomerMobile: "13800138000", CustomerMobileMasked: "13800138000",
		CustomerWechat: "wx-customer", CustomerWechatMasked: "wx-customer",
		ActorUserID: "dispatcher-1", ActorRole: string(types.UserRoleDispatcher),
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Lead.CustomerMobile != "" || response.Lead.CustomerWechat != "" {
		t.Fatalf("dispatcher response leaked raw contacts: %+v", response.Lead)
	}
	if response.Lead.DispatchManagerUserID != "" {
		t.Fatalf("dispatcher became business supervisor: %+v", response.Lead)
	}
	if response.Lead.CustomerMobileMasked != "138****8000" || response.Lead.CustomerWechatMasked != "wx****er" {
		t.Fatalf("server masks = %q/%q", response.Lead.CustomerMobileMasked, response.Lead.CustomerWechatMasked)
	}

	_, err = newRoleAwareCreateUseCase(db).Execute(context.Background(), &CreateLeadRequest{
		CustomerName: "短联系方式", CustomerMobile: "12345", CustomerWechat: "wx", ActorUserID: "dispatcher-1", ActorRole: string(types.UserRoleDispatcher),
	})
	if err != types.ErrInvalidLeadProfile {
		t.Fatalf("invalid phone accepted: %v", err)
	}

}

func TestRoleAwareCreateCloserRequiresActiveInviter(t *testing.T) {
	db := newRoleAwareCreationTestDB(t)
	bindings := repo.NewGormUserBindingRepository(db)
	createActiveBinding(t, bindings, "inviter-1", types.UserRoleInviter)
	createActiveBinding(t, bindings, "closer-2", types.UserRoleCloser)
	uc := newRoleAwareCreateUseCase(db)

	for name, target := range map[string]string{"missing": "", "unknown": "missing-user", "closer": "closer-2"} {
		t.Run(name, func(t *testing.T) {
			_, err := uc.Execute(context.Background(), &CreateLeadRequest{
				CustomerName: "非法指定", ActorUserID: "closer-1", ActorRole: string(types.UserRoleCloser), AssignedInviterUserID: target,
			})
			if !errors.Is(err, types.ErrInvalidInviter) {
				t.Fatalf("error=%v, want ErrInvalidInviter", err)
			}
		})
	}

	response, err := uc.Execute(context.Background(), &CreateLeadRequest{
		CustomerName: "谈单师创建", CustomerWechat: "wechat-only", ActorUserID: "closer-1", ActorRole: string(types.UserRoleCloser), AssignedInviterUserID: "inviter-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	lead := response.Lead
	if response.Outcome != "assigned_pending_claim" || lead.Status != types.LeadStatusAssignedPendingClaim || lead.AssignedInviterUserID != "inviter-1" {
		t.Fatalf("response=%+v", response)
	}
	if lead.CreatedByUserID != "closer-1" || lead.CreatedByRole != types.UserRoleCloser || lead.CurrentOwnerUserID != "" {
		t.Fatalf("creator/owner mismatch: %+v", lead)
	}
}

func TestManagerCanAssignSelfAndAnotherManager(t *testing.T) {
	db := newRoleAwareCreationTestDB(t)
	bindings := repo.NewGormUserBindingRepository(db)
	createActiveBinding(t, bindings, "manager-1", types.UserRoleManager)
	createActiveBinding(t, bindings, "manager-2", types.UserRoleManager)
	leads := repo.NewGormLeadRepository(db)
	dispatches := repo.NewGormDispatchRecordRepository(db)
	uc := NewAssignLeadUseCase(leads, dispatches, nil, nil, nil, nil).WithUserBindingRepository(bindings)

	now := time.Now()
	selfLead := &types.Lead{LeadNo: "MANAGER-SELF", CustomerName: "主管自己跟进", Status: types.LeadStatusPendingDispatch, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := leads.Create(context.Background(), selfLead); err != nil {
		t.Fatal(err)
	}
	if err := uc.Execute(context.Background(), &AssignLeadRequest{
		LeadID: selfLead.ID, ManagerUserID: "manager-1", ActorRole: string(types.UserRoleManager), SalesUserID: "manager-1",
	}); err != nil {
		t.Fatal(err)
	}
	assigned, _ := leads.GetByID(context.Background(), selfLead.ID)
	if assigned.Status != types.LeadStatusAssignedPendingClaim || assigned.AssignedInviterUserID != "manager-1" {
		t.Fatalf("assigned=%+v", assigned)
	}

	otherLead := &types.Lead{LeadNo: "OTHER-MANAGER", CustomerName: "分给其他主管", Status: types.LeadStatusPendingDispatch, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := leads.Create(context.Background(), otherLead); err != nil {
		t.Fatal(err)
	}
	err := uc.Execute(context.Background(), &AssignLeadRequest{
		LeadID: otherLead.ID, ManagerUserID: "manager-1", ActorRole: string(types.UserRoleManager), SalesUserID: "manager-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	assigned, err = leads.GetByID(context.Background(), otherLead.ID)
	if err != nil || assigned.DispatchManagerUserID != "manager-2" || assigned.AssignedInviterUserID != "manager-2" || assigned.Status != types.LeadStatusAssignedPendingClaim {
		t.Fatalf("assigned=%+v err=%v", assigned, err)
	}
}

func TestAdminAndDispatcherCanAssignLeadToActiveManager(t *testing.T) {
	for _, actorRole := range []types.UserRole{types.UserRoleAdmin, types.UserRoleDispatcher} {
		t.Run(string(actorRole), func(t *testing.T) {
			db := newRoleAwareCreationTestDB(t)
			bindings := repo.NewGormUserBindingRepository(db)
			createActiveBinding(t, bindings, "manager-target", types.UserRoleManager)
			leads := repo.NewGormLeadRepository(db)
			dispatches := repo.NewGormDispatchRecordRepository(db)
			uc := NewAssignLeadUseCase(leads, dispatches, nil, nil, nil, nil).WithUserBindingRepository(bindings)

			now := time.Now()
			lead := &types.Lead{LeadNo: "ASSIGN-MANAGER-" + string(actorRole), CustomerName: "分给主管", Status: types.LeadStatusPendingDispatch, Version: 1, ProfileVersion: 1, CreatedAt: now, UpdatedAt: now}
			if err := leads.Create(context.Background(), lead); err != nil {
				t.Fatal(err)
			}
			if err := uc.Execute(context.Background(), &AssignLeadRequest{
				LeadID: lead.ID, ManagerUserID: string(actorRole) + "-1", ActorRole: string(actorRole), SalesUserID: "manager-target",
			}); err != nil {
				t.Fatal(err)
			}
			assigned, err := leads.GetByID(context.Background(), lead.ID)
			if err != nil {
				t.Fatal(err)
			}
			if assigned.Status != types.LeadStatusAssignedPendingClaim || assigned.AssignedInviterUserID != "manager-target" || assigned.DispatchManagerUserID != "manager-target" {
				t.Fatalf("assigned = %+v", assigned)
			}
		})
	}
}

func TestAssignmentCandidatesIncludeActiveManager(t *testing.T) {
	db := newRoleAwareCreationTestDB(t)
	bindings := repo.NewGormUserBindingRepository(db)
	createActiveBinding(t, bindings, "inviter-1", types.UserRoleInviter)
	createActiveBinding(t, bindings, "manager-1", types.UserRoleManager)
	now := time.Now()
	if err := bindings.Create(context.Background(), &types.WeComUserBinding{WeComUserID: "manager-off", EmployeeName: "停用主管", Role: types.UserRoleManager, Status: types.UserStatusDisabled, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	result, err := NewAdminUserBindingsUseCase(bindings).ListAssignableInviters(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bindings) != 2 || result.Bindings[0].WeComUserID != "manager-1" || result.Bindings[1].WeComUserID != "inviter-1" {
		t.Fatalf("bindings = %+v", result.Bindings)
	}
}

func TestManagerSelfClaimStartsFollowingAndSchedulesReminders(t *testing.T) {
	db := newRoleAwareCreationTestDB(t)
	leads := repo.NewGormLeadRepository(db)
	now := time.Now()
	lead := &types.Lead{LeadNo: "SELF-CLAIM", CustomerName: "主管亲自跟进", Status: types.LeadStatusPendingDispatch, BusinessStage: types.BusinessStageInvitation, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	uc := NewAssignLeadUseCase(leads, repo.NewGormDispatchRecordRepository(db), nil, nil, nil, nil).
		WithTransactionManager(repo.NewGormTransactionManager(db))
	if err := uc.SelfClaim(context.Background(), lead.ID, "manager-1", string(types.UserRoleManager)); err != nil {
		t.Fatal(err)
	}
	updated, _ := leads.GetByID(context.Background(), lead.ID)
	if updated.Status != types.LeadStatusFollowing || updated.CurrentOwnerUserID != "manager-1" || updated.CurrentOwnerRole != types.OwnerRoleInviter || updated.NextFollowupDueAt == nil {
		t.Fatalf("updated=%+v", updated)
	}
	var records []repo.LeadDispatchRecordModel
	if err := db.Where("lead_id = ?", lead.ID).Order("id ASC").Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].ActionType != "self_assign" || records[1].ActionType != "claim" {
		t.Fatalf("records=%+v", records)
	}
	var reminderCount int64
	if err := db.Model(&repo.LeadReminderRecordModel{}).Where("lead_id = ?", lead.ID).Count(&reminderCount).Error; err != nil {
		t.Fatal(err)
	}
	if reminderCount != 3 {
		t.Fatalf("reminder count=%d, want 3", reminderCount)
	}

	if err := uc.SelfClaim(context.Background(), lead.ID, "admin-1", string(types.UserRoleAdmin)); !errors.Is(err, types.ErrUnauthorized) {
		t.Fatalf("admin self claim error=%v, want ErrUnauthorized", err)
	}
}

func TestManagerCanTakeOverAssignedLeadAndPreservesFollowupDue(t *testing.T) {
	db := newRoleAwareCreationTestDB(t)
	leads := repo.NewGormLeadRepository(db)
	now := time.Now()
	dueAt := now.Add(45 * time.Minute)
	lead := &types.Lead{
		LeadNo: "MANAGER-TAKEOVER", CustomerName: "主管接管", Status: types.LeadStatusFollowing,
		BusinessStage: types.BusinessStageInvitation, ResultStatus: types.LeadResultActive,
		OwnershipState: types.OwnershipPrivate, AssignedInviterUserID: "inviter-1",
		CurrentOwnerUserID: "inviter-1", CurrentOwnerRole: types.OwnerRoleInviter,
		NextFollowupDueAt: &dueAt, Version: 3, ProfileVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	uc := NewAssignLeadUseCase(leads, repo.NewGormDispatchRecordRepository(db), nil, nil, nil, nil).
		WithTransactionManager(repo.NewGormTransactionManager(db))
	if err := uc.SelfClaim(context.Background(), lead.ID, "manager-1", string(types.UserRoleManager)); err != nil {
		t.Fatal(err)
	}
	updated, _ := leads.GetByID(context.Background(), lead.ID)
	if updated.Status != types.LeadStatusFollowing || updated.CurrentOwnerUserID != "manager-1" || updated.NextFollowupDueAt == nil || !updated.NextFollowupDueAt.Equal(dueAt) {
		t.Fatalf("updated=%+v", updated)
	}
	var records []repo.LeadDispatchRecordModel
	if err := db.Where("lead_id = ?", lead.ID).Order("id ASC").Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].ActionType != "reassign" || records[1].ActionType != "claim" {
		t.Fatalf("records=%+v", records)
	}
}
