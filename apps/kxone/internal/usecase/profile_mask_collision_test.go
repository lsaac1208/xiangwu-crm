package usecase

import (
	"context"
	"testing"

	"github.com/lsaac128/kxone/internal/adapter/repo"
)

func TestAuditMaskedMobileChangeIsPersisted(t *testing.T) {
	ctx := context.Background()
	db := newStage3TestDB(t)
	leads := repo.NewGormLeadRepository(db)
	activities := repo.NewGormLeadActivityRepository(db)
	uc := NewUpdateLeadProfileUseCase(leads, activities).WithTransactionManager(repo.NewGormTransactionManager(db))
	lead := stage3OwnedLead(0, "inviter-1")
	lead.LeadNo = "AUDIT-MASK"
	lead.CustomerMobile = "13912345678"
	lead.CustomerMobileMasked = "139****5678"
	if err := leads.Create(ctx, lead); err != nil {
		t.Fatal(err)
	}
	replacement := "13999995678"
	result, err := uc.Execute(ctx, &UpdateLeadProfileRequest{LeadID: lead.ID, ActorUserID: "manager-1", ActorRole: "manager", ExpectedBusinessVersion: lead.Version, ExpectedProfileVersion: lead.ProfileVersion, CustomerMobile: &replacement})
	if err != nil {
		t.Fatal(err)
	}
	got, err := leads.GetByID(ctx, lead.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CustomerMobile != replacement {
		t.Fatalf("success response says synthetic phone=%s profile_version=%d; database retains=%s profile_version=%d", result.Lead.CustomerMobile, result.Lead.ProfileVersion, got.CustomerMobile, got.ProfileVersion)
	}
}
