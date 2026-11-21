package repo

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestManagementStatsVisitsCompleteCohortWithoutContacts(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "management.db"), MaxConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 10, 1, 16, 30, 0, 0, time.UTC)
	models := make([]LeadModel, 10020)
	for i := range models {
		models[i] = LeadModel{LeadNo: fmt.Sprintf("FULL-%d", i), Status: "following", OwnerUserID: "inviter-a", CustomerMobile: "private-contact-must-not-be-read", CreatedAt: created, UpdatedAt: created}
	}
	if err := db.Select("lead_no", "status", "owner_user_id", "customer_mobile", "created_at", "updated_at").CreateInBatches(models, 200).Error; err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 2, 0, 0, 0, 0, types.BusinessLocation)
	end := start.Add(24 * time.Hour)
	repository := NewGormLeadRepository(db)
	count := 0
	err = repository.VisitManagementLeads(context.Background(), LeadStatsFilter{SalesUserID: "inviter-a", StartTime: &start, EndTime: &end}, func(lead *types.Lead) {
		count++
		if lead.CustomerMobile != "" {
			t.Fatal("reporting unexpectedly loaded a private contact")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 10020 {
		t.Fatalf("report truncated at legacy cap: %d, want 10020", count)
	}
	outside := 0
	err = repository.VisitManagementLeads(context.Background(), LeadStatsFilter{SalesUserID: "other-person"}, func(*types.Lead) { outside++ })
	if err != nil {
		t.Fatal(err)
	}
	if outside != 0 {
		t.Fatalf("person filter leaked %d records", outside)
	}
}
