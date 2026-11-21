package usecase

import (
	"errors"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
	"testing"
)

func TestFollowupReceiptReplaysAfterPoolReturn(t *testing.T) {
	db := newStage3TestDB(t)
	if err := db.AutoMigrate(&repo.FollowupRequest{}); err != nil {
		t.Fatal(err)
	}
	leads, followups := repo.NewGormLeadRepository(db), repo.NewGormFollowupRepository(db)
	pools, reminders := repo.NewGormLeadPoolRepository(db), repo.NewGormLeadReminderRepository(db)
	settings := stage3SettingsStub{workflowEnabled: true, poolEnabled: true}
	uc := NewCreateFollowupUseCase(leads, followups).WithTransactionManager(repo.NewGormTransactionManager(db)).WithStage3Workflow(settings, settings, pools, reminders).WithIdempotencySecret("test-only-request-fingerprint-secret")
	lead := stage3OwnedLead(1, "inviter-1")
	if err := leads.Create(t.Context(), lead); err != nil {
		t.Fatal(err)
	}
	req := &CreateFollowupRequest{AccountID: 11, IdempotencyKey: "0404f98b-267d-48f4-a708-186a87fcd6dc", LeadID: lead.ID, SalesUserID: "inviter-1", FollowupResult: "not_interested", Remark: "可回收线索", IsValid: false, InvalidReason: "暂不考虑", ReturnToPool: true}
	if err := uc.Execute(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	first, _ := leads.GetByID(t.Context(), lead.ID)
	if first.OwnershipState != types.OwnershipPublicPool {
		t.Fatal("lead did not enter pool")
	}
	if err := uc.Execute(t.Context(), req); err != nil {
		t.Fatalf("acknowledging completed own request: %v", err)
	}
	history, err := followups.ListByLeadID(t.Context(), lead.ID)
	if err != nil || len(history) != 1 {
		t.Fatalf("history=%d err=%v", len(history), err)
	}
	after, _ := leads.GetByID(t.Context(), lead.ID)
	if after.Version != first.Version || !after.LastFollowupAt.Equal(*first.LastFollowupAt) {
		t.Fatal("replay advanced lead state")
	}
	other := *req
	other.AccountID = 12
	other.SalesUserID = "inviter-2"
	if err := uc.Execute(t.Context(), &other); !errors.Is(err, types.ErrUnauthorized) {
		t.Fatalf("another account reused receipt: %v", err)
	}
}

func TestFollowupReceiptFailureRollsBackAllEffectsAndAllowsRetry(t *testing.T) {
	db := newStage3TestDB(t)
	if err := db.AutoMigrate(&repo.FollowupRequest{}); err != nil {
		t.Fatal(err)
	}
	leads, followups := repo.NewGormLeadRepository(db), repo.NewGormFollowupRepository(db)
	settings := stage3SettingsStub{workflowEnabled: true, poolEnabled: true}
	reminders := repo.NewGormLeadReminderRepository(db)
	uc := NewCreateFollowupUseCase(leads, followups).WithTransactionManager(repo.NewGormTransactionManager(db)).WithStage3Workflow(settings, settings, repo.NewGormLeadPoolRepository(db), reminders).WithIdempotencySecret("test-only-request-fingerprint-secret")
	lead := stage3OwnedLead(1, "inviter-1")
	if err := leads.Create(t.Context(), lead); err != nil {
		t.Fatal(err)
	}
	before, _ := leads.GetByID(t.Context(), lead.ID)
	if err := db.Exec("CREATE TRIGGER receipt_storage_failure BEFORE UPDATE ON crm_followup_requests BEGIN SELECT RAISE(ABORT, 'synthetic storage failure'); END").Error; err != nil {
		t.Fatal(err)
	}
	req := &CreateFollowupRequest{AccountID: 11, IdempotencyKey: "3dd208d7-8231-4c44-aed5-a51e073b17e1", LeadID: lead.ID, SalesUserID: "inviter-1", FollowupResult: "connected", Remark: "事务故障重试", IsValid: true, StarRating: 4}
	if err := uc.Execute(t.Context(), req); err == nil {
		t.Fatal("expected injected storage failure")
	}
	history, err := followups.ListByLeadID(t.Context(), lead.ID)
	if err != nil || len(history) != 0 {
		t.Fatal("followup was committed after receipt failure")
	}
	after, _ := leads.GetByID(t.Context(), lead.ID)
	if after.Version != before.Version || after.LastFollowupAt != nil {
		t.Fatal("lead changed after rolled-back transaction")
	}
	var count int64
	if err := db.Model(&repo.LeadReminderRecordModel{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("reminders committed after rollback")
	}
	if err := db.Exec("DROP TRIGGER receipt_storage_failure").Error; err != nil {
		t.Fatal(err)
	}
	if err := uc.Execute(t.Context(), req); err != nil {
		t.Fatalf("same request cannot retry after rollback: %v", err)
	}
	if err := uc.Execute(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	history, _ = followups.ListByLeadID(t.Context(), lead.ID)
	if len(history) != 1 {
		t.Fatalf("retry wrote %d records", len(history))
	}
	if err := db.Model(&repo.LeadReminderRecordModel{}).Count(&count).Error; err != nil || count != 3 {
		t.Fatalf("reminder effects=%d err=%v", count, err)
	}
}
