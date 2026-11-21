package usecase

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

type stage3SettingsStub struct {
	workflowEnabled bool
	poolEnabled     bool
	autoPoolEnabled bool
	maxActive       int
	cooldown        time.Duration
}

func (s stage3SettingsStub) WorkflowV2Enabled(context.Context) bool         { return s.workflowEnabled }
func (s stage3SettingsStub) AppointmentEnabled(context.Context) bool        { return false }
func (s stage3SettingsStub) PoolEnabled(context.Context) bool               { return s.poolEnabled }
func (s stage3SettingsStub) PoolAutoReturnEnabled(context.Context) bool     { return s.autoPoolEnabled }
func (s stage3SettingsStub) FirstFollowupDue(context.Context) time.Duration { return 2 * time.Hour }
func (s stage3SettingsStub) FollowupDueByStar(_ context.Context, star int) time.Duration {
	return time.Duration(6-star) * 12 * time.Hour
}
func (s stage3SettingsStub) FollowupReminderOffsets(context.Context) []time.Duration {
	return []time.Duration{0, time.Hour, 4 * time.Hour}
}
func (s stage3SettingsStub) PoolMaxActiveLeads(context.Context) int {
	if s.maxActive > 0 {
		return s.maxActive
	}
	return 100
}
func (s stage3SettingsStub) PoolCooldown(context.Context) time.Duration {
	if s.cooldown > 0 {
		return s.cooldown
	}
	return 24 * time.Hour
}
func (s stage3SettingsStub) PoolUnclaimedTimeout(context.Context) time.Duration {
	return 24 * time.Hour
}
func (s stage3SettingsStub) PoolFollowupGrace(context.Context) time.Duration { return 4 * time.Hour }

func TestStage3FollowupCreatesStarSnapshotDueAndReminders(t *testing.T) {
	db := newStage3TestDB(t)
	leads := repo.NewGormLeadRepository(db)
	followups := repo.NewGormFollowupRepository(db)
	pools := repo.NewGormLeadPoolRepository(db)
	reminders := repo.NewGormLeadReminderRepository(db)
	tx := repo.NewGormTransactionManager(db)
	settings := stage3SettingsStub{workflowEnabled: true, poolEnabled: true}

	lead := stage3OwnedLead(1, "inviter-1")
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	uc := NewCreateFollowupUseCase(leads, followups).WithTransactionManager(tx).WithStage3Workflow(settings, settings, pools, reminders)
	if err := uc.Execute(context.Background(), &CreateFollowupRequest{LeadID: lead.ID, SalesUserID: "inviter-1", FollowupResult: "connected", Remark: "首次有效回访", IsValid: true, StarRating: 5}); err != nil {
		t.Fatal(err)
	}

	updated, _ := leads.GetByID(context.Background(), lead.ID)
	if updated.StarRating != 5 || updated.Status != types.LeadStatusFollowing || updated.NextFollowupDueAt == nil {
		t.Fatalf("updated lead = %+v", updated)
	}
	history, _ := followups.ListByLeadID(context.Background(), lead.ID)
	if len(history) != 1 || history[0].StarRating != 5 || history[0].NextFollowupAt == nil || history[0].ActorRole != types.OwnerRoleInviter {
		t.Fatalf("follow-up snapshot = %+v", history)
	}
	var reminderCount int64
	if err := db.Model(&repo.LeadReminderRecordModel{}).Where("lead_id = ?", lead.ID).Count(&reminderCount).Error; err != nil || reminderCount != 3 {
		t.Fatalf("reminder count = %d, err=%v", reminderCount, err)
	}
}

func TestStage3FollowupDefaultsMissingStarAndCanReturnRecoverableInvalid(t *testing.T) {
	db := newStage3TestDB(t)
	leads := repo.NewGormLeadRepository(db)
	followups := repo.NewGormFollowupRepository(db)
	pools := repo.NewGormLeadPoolRepository(db)
	reminders := repo.NewGormLeadReminderRepository(db)
	settings := stage3SettingsStub{workflowEnabled: true, poolEnabled: true}
	uc := NewCreateFollowupUseCase(leads, followups).WithTransactionManager(repo.NewGormTransactionManager(db)).WithStage3Workflow(settings, settings, pools, reminders)

	lead := stage3OwnedLead(2, "inviter-1")
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	base := &CreateFollowupRequest{LeadID: lead.ID, SalesUserID: "inviter-1", FollowupResult: "connected", Remark: "test", IsValid: true}
	if err := uc.Execute(context.Background(), base); err != nil {
		t.Fatalf("missing star error = %v", err)
	}
	if err := uc.Execute(context.Background(), &CreateFollowupRequest{LeadID: lead.ID, SalesUserID: "inviter-1", FollowupResult: "not_interested", Remark: "当前不可联系", IsValid: false, InvalidReason: "暂时停机", ReturnToPool: true}); err != nil {
		t.Fatal(err)
	}
	updated, _ := leads.GetByID(context.Background(), lead.ID)
	if updated.OwnershipState != types.OwnershipPublicPool || updated.BusinessStage != types.BusinessStageInvitation || updated.ResultStatus != types.LeadResultActive || updated.CurrentOwnerUserID != "" {
		t.Fatalf("recoverable invalid lead = %+v", updated)
	}
	records, _ := pools.ListByLead(context.Background(), lead.ID)
	if len(records) != 1 || records[0].Reason != types.PoolReasonRecoverableInvalid || records[0].PreviousOwnerUserID != "inviter-1" {
		t.Fatalf("pool records = %+v", records)
	}
}

func TestStage3PublicPoolMasksPIIClaimsAndEnforcesCooldown(t *testing.T) {
	db := newStage3TestDB(t)
	leads := repo.NewGormLeadRepository(db)
	pools := repo.NewGormLeadPoolRepository(db)
	settings := stage3SettingsStub{workflowEnabled: true, poolEnabled: true, cooldown: time.Hour}
	uc := NewLeadPoolUseCase(leads, pools, settings, settings).WithDependencies(nil, repo.NewGormTransactionManager(db))

	lead := stage3PoolLead(3)
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	if err := pools.Create(context.Background(), &types.LeadPoolRecord{LeadID: lead.ID, Action: types.PoolActionEnter, PreviousOwnerUserID: "other", PoolVersion: lead.Version, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	listed, err := uc.List(context.Background(), &ListPublicPoolRequest{CurrentUser: "inviter-1", Role: "inviter", Page: 1, PageSize: 20})
	if err != nil || len(listed.Leads) != 1 || listed.Leads[0].CustomerMobile != "" || listed.Leads[0].CustomerMobileMasked == "" {
		t.Fatalf("pool list = %+v err=%v", listed, err)
	}
	if err := uc.Claim(context.Background(), lead.ID, "inviter-1", "inviter"); err != nil {
		t.Fatal(err)
	}
	claimed, _ := leads.GetByID(context.Background(), lead.ID)
	if claimed.OwnershipState != types.OwnershipPrivate || claimed.CurrentOwnerUserID != "inviter-1" || claimed.NextFollowupDueAt == nil {
		t.Fatalf("claimed lead = %+v", claimed)
	}

	cooldownLead := stage3PoolLead(4)
	if err := leads.Create(context.Background(), cooldownLead); err != nil {
		t.Fatal(err)
	}
	if err := pools.Create(context.Background(), &types.LeadPoolRecord{LeadID: cooldownLead.ID, Action: types.PoolActionEnter, PreviousOwnerUserID: "inviter-1", PoolVersion: cooldownLead.Version, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := uc.Claim(context.Background(), cooldownLead.ID, "inviter-1", "inviter"); err != types.ErrPublicPoolCooldown {
		t.Fatalf("cooldown error = %v", err)
	}
}

func TestStage3NegotiationLeadCannotEnterInvitationPool(t *testing.T) {
	db := newStage3TestDB(t)
	leads := repo.NewGormLeadRepository(db)
	pools := repo.NewGormLeadPoolRepository(db)
	settings := stage3SettingsStub{workflowEnabled: true, poolEnabled: true}
	uc := NewLeadPoolUseCase(leads, pools, settings, settings).WithDependencies(nil, repo.NewGormTransactionManager(db))
	lead := stage3OwnedLead(5, "closer-1")
	lead.BusinessStage = types.BusinessStageNegotiation
	lead.CurrentOwnerRole = types.OwnerRoleCloser
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	if err := uc.Enter(context.Background(), lead.ID, "manager-1", "manager", types.PoolReasonManual); err != types.ErrPublicPoolForbidden {
		t.Fatalf("negotiation enter error = %v", err)
	}

	now := time.Now()
	appointmentID := int64(88)
	protected := []*types.Lead{
		stage3OwnedLead(11, "inviter-1"),
		stage3OwnedLead(12, "inviter-1"),
		stage3OwnedLead(13, "inviter-1"),
	}
	protected[0].ArrivedAt = &now
	protected[1].DeliveredAt = &now
	protected[2].CurrentAppointmentID = &appointmentID
	for _, candidate := range protected {
		if err := leads.Create(context.Background(), candidate); err != nil {
			t.Fatal(err)
		}
		if err := uc.Enter(context.Background(), candidate.ID, "manager-1", "manager", types.PoolReasonManual); err != types.ErrPublicPoolForbidden {
			t.Fatalf("protected lead %d enter error = %v", candidate.ID, err)
		}
	}
}

func TestStage3DisabledPoolRejectsEntryButAllowsDrainingExistingLeads(t *testing.T) {
	db := newStage3TestDB(t)
	leads := repo.NewGormLeadRepository(db)
	pools := repo.NewGormLeadPoolRepository(db)
	settings := stage3SettingsStub{workflowEnabled: false}
	uc := NewLeadPoolUseCase(leads, pools, settings, settings).WithDependencies(nil, repo.NewGormTransactionManager(db))

	owned := stage3OwnedLead(8, "sales-1")
	if err := leads.Create(context.Background(), owned); err != nil {
		t.Fatal(err)
	}
	if err := uc.Enter(context.Background(), owned.ID, "manager-1", "manager", types.PoolReasonManual); err != types.ErrPublicPoolForbidden {
		t.Fatalf("disabled pool entry error = %v", err)
	}

	pooled := stage3PoolLead(9)
	if err := leads.Create(context.Background(), pooled); err != nil {
		t.Fatal(err)
	}
	listed, err := uc.List(context.Background(), &ListPublicPoolRequest{CurrentUser: "sales-1", Role: "sales", Page: 1, PageSize: 20})
	if err != nil || len(listed.Leads) != 1 {
		t.Fatalf("disabled pool drain list = %+v, err=%v", listed, err)
	}
	if err := uc.Claim(context.Background(), pooled.ID, "sales-1", "sales"); err != nil {
		t.Fatalf("disabled pool drain claim: %v", err)
	}
}

func TestStage3AutoReturnCoversUnclaimedAndFollowupOverdue(t *testing.T) {
	db := newStage3TestDB(t)
	leads := repo.NewGormLeadRepository(db)
	pools := repo.NewGormLeadPoolRepository(db)
	settings := stage3SettingsStub{workflowEnabled: true, poolEnabled: true, autoPoolEnabled: true}
	uc := NewLeadPoolUseCase(leads, pools, settings, settings).WithDependencies(nil, repo.NewGormTransactionManager(db))
	now := time.Now()
	oldAssigned := now.Add(-48 * time.Hour)
	unclaimed := stage3OwnedLead(6, "inviter-1")
	unclaimed.LeadNo = "STAGE3-AUTO-UNCLAIMED"
	unclaimed.Status = types.LeadStatusAssignedPendingClaim
	unclaimed.OwnershipState = types.OwnershipAssignedPendingClaim
	unclaimed.OwnerUserID, unclaimed.InviterUserID, unclaimed.CurrentOwnerUserID = "", "", ""
	unclaimed.CurrentOwnerRole = ""
	unclaimed.InviterAssignedAt = &oldAssigned
	if err := leads.Create(context.Background(), unclaimed); err != nil {
		t.Fatal(err)
	}

	overdue := stage3OwnedLead(7, "inviter-2")
	overdue.LeadNo = "STAGE3-AUTO-OVERDUE"
	overdue.Status = types.LeadStatusFollowing
	due := now.Add(-5 * time.Hour)
	overdue.NextFollowupDueAt = &due
	if err := leads.Create(context.Background(), overdue); err != nil {
		t.Fatal(err)
	}

	if err := uc.AutoReturnOverdue(context.Background(), now, 20); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{unclaimed.ID, overdue.ID} {
		updated, _ := leads.GetByID(context.Background(), id)
		if updated.OwnershipState != types.OwnershipPublicPool {
			t.Fatalf("lead %d not returned: %+v", id, updated)
		}
	}
}

func TestStage3AutoReturnRequiresPoolAndAutoReturnFlags(t *testing.T) {
	db := newStage3TestDB(t)
	leads := repo.NewGormLeadRepository(db)
	pools := repo.NewGormLeadPoolRepository(db)
	settings := stage3SettingsStub{workflowEnabled: false, poolEnabled: false, autoPoolEnabled: true}
	uc := NewLeadPoolUseCase(leads, pools, settings, settings).WithDependencies(nil, repo.NewGormTransactionManager(db))

	now := time.Now()
	lead := stage3OwnedLead(14, "sales-1")
	lead.Status = types.LeadStatusFollowing
	due := now.Add(-24 * time.Hour)
	lead.NextFollowupDueAt = &due
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	if err := uc.AutoReturnOverdue(context.Background(), now, 20); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := leads.GetByID(context.Background(), lead.ID)
	if unchanged.OwnershipState != types.OwnershipPrivate {
		t.Fatalf("disabled pool auto-returned lead = %+v", unchanged)
	}
}

func TestStage3SingleChainPoolClaimAndFollowupUseSharedPolicy(t *testing.T) {
	db := newStage3TestDB(t)
	leads := repo.NewGormLeadRepository(db)
	poolRepo := repo.NewGormLeadPoolRepository(db)
	followups := repo.NewGormFollowupRepository(db)
	reminders := repo.NewGormLeadReminderRepository(db)
	tx := repo.NewGormTransactionManager(db)
	settings := stage3SettingsStub{workflowEnabled: false, poolEnabled: true}

	lead := stage3PoolLead(10)
	if err := leads.Create(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	poolUC := NewLeadPoolUseCase(leads, poolRepo, settings, settings).WithDependencies(nil, tx)
	if err := poolUC.Claim(context.Background(), lead.ID, "sales-1", "sales"); err != nil {
		t.Fatal(err)
	}
	claimed, _ := leads.GetByID(context.Background(), lead.ID)
	if claimed.OwnerUserID != "sales-1" || claimed.InviterUserID != "sales-1" || claimed.NextFollowupDueAt == nil {
		t.Fatalf("single-chain claim = %+v", claimed)
	}

	followupUC := NewCreateFollowupUseCase(leads, followups).WithTransactionManager(tx).WithStage3Workflow(settings, settings, poolRepo, reminders)
	if err := followupUC.Execute(context.Background(), &CreateFollowupRequest{
		LeadID: lead.ID, SalesUserID: "sales-1", FollowupResult: "connected", Remark: "单链路回访",
		IsValid: true, StarRating: 4,
	}); err != nil {
		t.Fatal(err)
	}
	updated, _ := leads.GetByID(context.Background(), lead.ID)
	if updated.Status != types.LeadStatusFollowing || updated.StarRating != 4 || updated.NextFollowupDueAt == nil {
		t.Fatalf("single-chain followup = %+v", updated)
	}
	var reminderCount int64
	if err := db.Model(&repo.LeadReminderRecordModel{}).Where("lead_id = ?", lead.ID).Count(&reminderCount).Error; err != nil || reminderCount < 3 {
		t.Fatalf("single-chain reminders = %d, err=%v", reminderCount, err)
	}
}

func newStage3TestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repo.NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "stage3.db"), MaxConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func stage3OwnedLead(id int64, owner string) *types.Lead {
	now := time.Now()
	return &types.Lead{ID: id, LeadNo: fmt.Sprintf("STAGE3-OWNED-%d", id), CustomerName: "阶段3客户", CustomerMobile: "13800138000", CustomerMobileMasked: "138****8000", Status: types.LeadStatusClaimed, BusinessStage: types.BusinessStageInvitation, OwnershipState: types.OwnershipPrivate, ResultStatus: types.LeadResultActive, AssignedSalesUserID: owner, OwnerUserID: owner, AssignedInviterUserID: owner, InviterUserID: owner, CurrentOwnerUserID: owner, CurrentOwnerRole: types.OwnerRoleInviter, DispatchManagerUserID: "manager-1", Version: 1, CreatedAt: now, UpdatedAt: now}
}

func stage3PoolLead(id int64) *types.Lead {
	now := time.Now()
	return &types.Lead{ID: id, LeadNo: fmt.Sprintf("STAGE3-POOL-%d", id), CustomerName: "公海客户", CustomerMobile: "13900139000", CustomerMobileMasked: "139****9000", Status: types.LeadStatusPublicPool, BusinessStage: types.BusinessStageInvitation, OwnershipState: types.OwnershipPublicPool, ResultStatus: types.LeadResultActive, Version: 1, PoolEnteredAt: &now, PoolReason: types.PoolReasonManual, CreatedAt: now, UpdatedAt: now}
}
