package repo

import (
	"errors"
	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestOwnerCycleRepairEvidenceVersionAndIdempotency(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-time.Hour)
	first := start.Add(time.Minute)
	lead := &types.Lead{LeadNo: "repair-proof", CustomerName: "unchanged", CustomerMobile: "legacy-invalid", SourceChannel: "legacy", StarRating: 5, Status: types.LeadStatusClaimed, BusinessStage: types.BusinessStageInvitation, OwnershipState: types.OwnershipPrivate, ResultStatus: types.LeadResultActive, OwnerUserID: "a", CurrentOwnerUserID: "a", CreatedAt: start.Add(-time.Hour), UpdatedAt: now, Version: 3, ProfileVersion: 1}
	if err := NewGormLeadRepository(db).Create(t.Context(), lead); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&LeadDispatchRecordModel{LeadID: lead.ID, SalesUserID: "a", ActionType: "claim", CreatedAt: start}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&LeadFollowupModel{LeadID: lead.ID, SalesUserID: "a", IsValid: true, CreatedAt: first}).Error; err != nil {
		t.Fatal(err)
	}
	plan, err := PlanOwnerCycleRepairs(t.Context(), db)
	if err != nil || len(plan.Items) != 1 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	item := plan.Items[0]
	if item.After.Status != "following" || item.After.StartedAt == nil || !item.After.StartedAt.Equal(start) {
		t.Fatalf("missing proof-backed fields: %+v", item)
	}
	tampered := item
	wrong := now
	tampered.After.StartedAt = &wrong
	if _, err := ApplyOwnerCycleRepair(t.Context(), db, tampered, nil); err == nil {
		t.Fatal("tampered evidence accepted")
	}
	fail := func(*gorm.DB, *types.Lead) error { return errors.New("reminder write failed") }
	if _, err := ApplyOwnerCycleRepair(t.Context(), db, item, fail); err == nil {
		t.Fatal("transaction failure accepted")
	}
	changed, err := ApplyOwnerCycleRepair(t.Context(), db, item, nil)
	if err != nil || !changed {
		t.Fatalf("apply=%v err=%v", changed, err)
	}
	changed, err = ApplyOwnerCycleRepair(t.Context(), db, item, nil)
	if err != nil || changed {
		t.Fatalf("idempotency=%v err=%v", changed, err)
	}
	loaded, err := NewGormLeadRepository(db).GetByID(t.Context(), lead.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.StarRating != 5 || loaded.CustomerMobile != "legacy-invalid" || loaded.SourceChannel != "legacy" || loaded.NextFollowupDueAt != nil || loaded.Version != 4 {
		t.Fatalf("unapproved fields rewritten: %+v", loaded)
	}
	var audits int64
	db.Model(&LeadActivityRecordModel{}).Where("lead_id = ?", lead.ID).Count(&audits)
	if audits != 1 {
		t.Fatalf("audit count=%d", audits)
	}
}
func TestOwnerCycleRepairWithholdsConcurrentChange(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	lead := LeadModel{LeadNo: "repair-conflict", Status: "claimed", Version: 1, ProfileVersion: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now}
	db.Create(&lead)
	db.Create(&LeadFollowupModel{LeadID: lead.ID, SalesUserID: "a", IsValid: true, CreatedAt: now})
	plan, err := PlanOwnerCycleRepairs(t.Context(), db)
	if err != nil || len(plan.Items) != 1 {
		t.Fatalf("plan=%+v error=%v", plan, err)
	}
	db.Model(&LeadModel{}).Where("id = ?", lead.ID).Update("version", 2)
	if _, err := ApplyOwnerCycleRepair(t.Context(), db, plan.Items[0], nil); !errors.Is(err, types.ErrLeadVersionConflict) {
		t.Fatalf("concurrent change not protected: %v", err)
	}
}

func TestOwnerCycleRepairPreservesExistingTimestampRepresentation(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-time.Hour)
	lead := LeadModel{LeadNo: "repair-preserve", Status: "following", CurrentOwnerUserID: "a", OwnershipState: "private", Version: 1, ProfileVersion: 1, CreatedAt: start.Add(-time.Hour), UpdatedAt: start}
	if err := db.Create(&lead).Error; err != nil {
		t.Fatal(err)
	}
	original := start.Add(time.Minute).In(time.FixedZone("Shanghai", 8*3600)).Format("2006-01-02 15:04:05-07:00")
	if err := db.Exec("UPDATE leads SET first_followup_at = ? WHERE id = ?", original, lead.ID).Error; err != nil {
		t.Fatal(err)
	}
	db.Create(&LeadDispatchRecordModel{LeadID: lead.ID, SalesUserID: "a", ActionType: "claim", CreatedAt: start})
	plan, err := PlanOwnerCycleRepairs(t.Context(), db)
	if err != nil || len(plan.Items) != 1 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	if _, err := ApplyOwnerCycleRepair(t.Context(), db, plan.Items[0], nil); err != nil {
		t.Fatal(err)
	}
	var stored string
	db.Raw("SELECT CAST(first_followup_at AS TEXT) FROM leads WHERE id = ?", lead.ID).Scan(&stored)
	if stored != original {
		t.Fatalf("unchanged lifecycle timestamp rewritten: %q -> %q", original, stored)
	}
}
