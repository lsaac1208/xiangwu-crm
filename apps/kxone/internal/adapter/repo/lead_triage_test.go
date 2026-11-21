package repo

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestNeedsActionListKeepsFiltersPaginationAndSoftDeletion(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "triage.db"), LogLevel: "silent"})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := db.AutoMigrate(&LeadModel{}); err != nil {
		t.Fatal(err)
	}
	leads := NewGormLeadRepository(db)
	now := time.Now().UTC()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	fixtures := []*types.Lead{
		{LeadNo: "triage-pending", CustomerName: "match pending", Status: types.LeadStatusPendingDispatch},
		{LeadNo: "triage-claim", CustomerName: "match claim", Status: types.LeadStatusAssignedPendingClaim, AssignedSalesUserID: "owner-a", AssignedInviterUserID: "owner-a"},
		{LeadNo: "triage-overdue", CustomerName: "match overdue", Status: types.LeadStatusFollowing, OwnerUserID: "owner-a", InviterUserID: "owner-a", CurrentOwnerUserID: "owner-a", NextFollowupDueAt: &past},
		{LeadNo: "triage-notdue", CustomerName: "match future", Status: types.LeadStatusFollowing, OwnerUserID: "owner-a", CurrentOwnerUserID: "owner-a", NextFollowupDueAt: &future},
		{LeadNo: "triage-other", CustomerName: "other overdue", Status: types.LeadStatusFollowing, OwnerUserID: "owner-b", CurrentOwnerUserID: "owner-b", NextFollowupDueAt: &past},
		{LeadNo: "triage-invalid", CustomerName: "match invalid", Status: types.LeadStatusInvalid, NextFollowupDueAt: &past},
		{LeadNo: "triage-pool", CustomerName: "match pool", Status: types.LeadStatusPublicPool, NextFollowupDueAt: &past},
		{LeadNo: "triage-deleted", CustomerName: "match deleted", Status: types.LeadStatusPendingDispatch},
	}
	for _, lead := range fixtures {
		lead.CreatedAt, lead.UpdatedAt = now, now
		types.ProjectLegacyWorkflow(lead)
		if err := leads.Create(context.Background(), lead); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Delete(&LeadModel{}, fixtures[7].ID).Error; err != nil {
		t.Fatal(err)
	}
	filter := LeadListFilter{Status: "needs_action", Keyword: "match", SortBy: "created_at", SortOrder: "asc"}
	page, total, err := leads.ListAllFiltered(context.Background(), filter, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(page) != 2 || page[0].LeadNo != "triage-pending" || page[1].LeadNo != "triage-claim" {
		t.Fatalf("first page total=%d leads=%v", total, triageNumbers(page))
	}
	page, total, err = leads.ListAllFiltered(context.Background(), filter, 2, 2)
	if err != nil || total != 3 || len(page) != 1 || page[0].LeadNo != "triage-overdue" {
		t.Fatalf("second page total=%d leads=%v err=%v", total, triageNumbers(page), err)
	}
	filter.SalesUserID = "owner-a"
	page, total, err = leads.ListAllFiltered(context.Background(), filter, 0, 20)
	if err != nil || total != 2 {
		t.Fatalf("owner filter total=%d leads=%v err=%v", total, triageNumbers(page), err)
	}
	filter.Keyword = "not-a-customer"
	page, total, err = leads.ListAllFiltered(context.Background(), filter, 0, 20)
	if err != nil || total != 0 || len(page) != 0 {
		t.Fatalf("keyword bypassed total=%d err=%v", total, err)
	}
	filter.Keyword, filter.SalesUserID = "", ""
	start := now.Add(time.Hour)
	filter.StartTime = &start
	page, total, err = leads.ListAllFiltered(context.Background(), filter, 0, 20)
	if err != nil || total != 0 || len(page) != 0 {
		t.Fatalf("date bypassed total=%d err=%v", total, err)
	}
}

func triageNumbers(leads []*types.Lead) []string {
	numbers := make([]string, len(leads))
	for i, lead := range leads {
		numbers[i] = lead.LeadNo
	}
	return numbers
}

func TestPersonalNeedsActionExcludesHistoricalOwnership(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "personal.db"), LogLevel: "silent"})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := db.AutoMigrate(&LeadModel{}); err != nil {
		t.Fatal(err)
	}
	r := NewGormLeadRepository(db)
	past := time.Now().Add(-time.Hour)
	fixtures := []*types.Lead{
		{LeadNo: "old-owner-due", Status: types.LeadStatusFollowing, InviterUserID: "a", AssignedSalesUserID: "a", OwnerUserID: "b", CurrentOwnerUserID: "b", NextFollowupDueAt: &past},
		{LeadNo: "old-inviter-claim", Status: types.LeadStatusAssignedPendingClaim, InviterUserID: "a", AssignedSalesUserID: "b", AssignedInviterUserID: "b"},
		{LeadNo: "my-due", Status: types.LeadStatusFollowing, InviterUserID: "a", OwnerUserID: "a", CurrentOwnerUserID: "a", NextFollowupDueAt: &past},
		{LeadNo: "my-claim", Status: types.LeadStatusAssignedPendingClaim, AssignedSalesUserID: "a", AssignedInviterUserID: "a"},
	}
	for _, lead := range fixtures {
		lead.CreatedAt, lead.UpdatedAt = time.Now(), time.Now()
		types.ProjectLegacyWorkflow(lead)
		if err := r.Create(context.Background(), lead); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := r.ListBySalesFiltered(context.Background(), "a", LeadListFilter{Status: "needs_action", NeedsActionUserID: "a", SortBy: "created_at", SortOrder: "asc"}, 0, 20)
	if err != nil || total != 2 || len(items) != 2 || items[0].LeadNo != "my-due" || items[1].LeadNo != "my-claim" {
		t.Fatalf("personal queue total=%d leads=%v err=%v", total, triageNumbers(items), err)
	}
}
