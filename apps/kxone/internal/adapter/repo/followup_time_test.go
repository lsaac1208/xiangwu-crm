package repo

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

func TestFollowupQueriesCompareInstantsAcrossStoredTimezones(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repository := NewGormLeadRepository(db)
	now := time.Now().In(types.BusinessLocation).Truncate(time.Second)
	deadlines := []time.Time{now.Add(-2 * time.Hour).UTC(), now.Add(2 * time.Hour).UTC(), now.Add(-2 * time.Hour), now.Add(2 * time.Hour)}
	for i, deadline := range deadlines {
		lead := &types.Lead{
			LeadNo: fmt.Sprintf("timezone-%d", i), Status: types.LeadStatusFollowing,
			BusinessStage: types.BusinessStageInvitation, OwnershipState: types.OwnershipPrivate,
			ResultStatus: types.LeadResultActive, CurrentOwnerUserID: "inviter-1",
			CurrentOwnerRole: types.OwnerRoleInviter, NextFollowupDueAt: &deadline,
			CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now,
		}
		if err := repository.Create(ctx, lead); err != nil {
			t.Fatal(err)
		}
	}

	items, count, err := repository.ListAllFiltered(ctx, LeadListFilter{Status: "followup_overdue"}, 0, 20)
	if err != nil || count != 2 || len(items) != 2 {
		t.Fatalf("overdue list = %d/%d, err=%v; only the two past instants are overdue", len(items), count, err)
	}
	for _, item := range items {
		if item.LeadNo != "timezone-0" && item.LeadNo != "timezone-2" {
			t.Fatalf("future follow-up was included: %s", item.LeadNo)
		}
	}
	snapshot, err := repository.BuildWorkbenchLeadSnapshot(ctx, "admin", types.UserRoleAdmin, now, 200)
	if err != nil || snapshot.OverdueFollowup != 2 {
		t.Fatalf("workbench snapshot = %+v, err=%v; want two overdue tasks", snapshot, err)
	}
	candidates, err := repository.ListAutoPoolCandidates(ctx, now.Add(-24*time.Hour), now.Add(-time.Hour), 20)
	if err != nil || len(candidates) != 2 {
		t.Fatalf("auto-pool candidates = %d, err=%v; future instants cannot be reclaimed", len(candidates), err)
	}
}

func TestLeadCreatedAtRangesCompareInstantsAcrossStoredTimezones(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	repository := NewGormLeadRepository(db)
	start := time.Date(2026, 10, 21, 9, 0, 0, 0, types.BusinessLocation)
	end := start.Add(time.Hour)
	created := []time.Time{start.Add(30 * time.Minute), start.Add(30 * time.Minute).UTC(), start.Add(-time.Minute).UTC(), end.UTC()}
	for i, stamp := range created {
		if err := repository.Create(ctx, &types.Lead{LeadNo: fmt.Sprintf("created-timezone-%d", i), Status: types.LeadStatusPendingDispatch, CreatedAt: stamp, UpdatedAt: stamp}); err != nil {
			t.Fatal(err)
		}
	}
	items, count, err := repository.ListAllFiltered(ctx, LeadListFilter{StartTime: &start, EndTime: &end}, 0, 20)
	if err != nil || count != 2 || len(items) != 2 {
		t.Fatalf("created range = %d/%d, err=%v", len(items), count, err)
	}
	stats, err := repository.ListForStats(ctx, LeadStatsFilter{StartTime: &start, EndTime: &end}, 20)
	if err != nil || len(stats) != 2 {
		t.Fatalf("stats range = %d, err=%v", len(stats), err)
	}
}

func TestFollowupSortUsesInstantsAndKeepsMissingDeadlinesLast(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	r := NewGormLeadRepository(db)
	early, _ := time.Parse(time.RFC3339, "2026-10-23T10:00:00+08:00")
	late, _ := time.Parse(time.RFC3339, "2026-10-23T03:00:00Z")
	for i, due := range []*time.Time{&early, &late, nil} {
		if err := r.Create(ctx, &types.Lead{LeadNo: fmt.Sprintf("sort-%d", i), NextFollowupDueAt: due, CreatedAt: early, UpdatedAt: early}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ order, first string }{{"asc", "sort-0"}, {"desc", "sort-1"}} {
		items, _, err := r.ListAllFiltered(ctx, LeadListFilter{SortBy: "next_followup_due_at", SortOrder: tc.order}, 0, 20)
		if err != nil || len(items) != 3 {
			t.Fatalf("items=%d err=%v", len(items), err)
		}
		if items[0].LeadNo != tc.first || items[2].LeadNo != "sort-2" {
			t.Fatalf("%s first=%s last=%s", tc.order, items[0].LeadNo, items[2].LeadNo)
		}
	}
}
