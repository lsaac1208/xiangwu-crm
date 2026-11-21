package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestFillMissingLeadSourcePersistsAuditsAndChecksVersion(t *testing.T) {
	db := newRoleAwareCreationTestDB(t)
	ctx := context.Background()
	leads := repo.NewGormLeadRepository(db)
	lead := makePendingBatchLead("SOURCE-MISSING", time.Now())
	lead.CustomerMobile = "13912345678"
	if err := leads.Create(ctx, lead); err != nil {
		t.Fatal(err)
	}
	uc := NewUpdateLeadProfileUseCase(leads, repo.NewGormLeadActivityRepository(db)).WithTransactionManager(repo.NewGormTransactionManager(db))
	source := "襄武小红书（员工乙/员工丙）"
	req := &UpdateLeadProfileRequest{LeadID: lead.ID, ActorUserID: "manager-a", ActorRole: "manager", ExpectedBusinessVersion: lead.Version, ExpectedProfileVersion: lead.ProfileVersion, SourceChannel: &source}
	result, err := uc.Execute(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := leads.GetByID(ctx, lead.ID)
	if err != nil || stored.SourceChannel != source || stored.Version != lead.Version || stored.Status != lead.Status || stored.ProfileVersion != lead.ProfileVersion+1 {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	var records []repo.LeadActivityRecordModel
	if err := db.Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ToValueJSON != `{"source_channel":"襄武小红书（员工乙/员工丙）"}` {
		t.Fatalf("audit=%+v", records)
	}
	if _, err := uc.Execute(ctx, req); !errors.Is(err, types.ErrLeadVersionConflict) {
		t.Fatalf("stale edit=%v", err)
	}
	req.ExpectedProfileVersion = result.Lead.ProfileVersion
	for _, invalid := range []string{"", "不在选项中的来源"} {
		req.SourceChannel = &invalid
		if _, err := uc.Execute(ctx, req); !errors.Is(err, types.ErrInvalidLeadProfile) {
			t.Fatalf("source=%q error=%v", invalid, err)
		}
	}
	req.SourceChannel = &source
	req.ActorRole = "dispatcher"
	if _, err := uc.Execute(ctx, req); !errors.Is(err, types.ErrUnauthorized) {
		t.Fatalf("permission=%v", err)
	}
}
