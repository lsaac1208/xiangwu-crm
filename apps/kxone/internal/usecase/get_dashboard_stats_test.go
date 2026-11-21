package usecase

import (
	"testing"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

func TestDashboardOverdueUsesConfiguredReminderThresholds(t *testing.T) {
	now := time.Now()
	assignedAt := now.Add(-4 * time.Minute)
	claimThreshold := 3 * time.Minute
	dispatchThreshold := 2 * time.Minute
	leads := []*types.Lead{
		{
			Status:    types.LeadStatusPendingDispatch,
			CreatedAt: now.Add(-3 * time.Minute),
		},
		{
			Status:     types.LeadStatusAssignedPendingClaim,
			AssignedAt: &assignedAt,
			CreatedAt:  now.Add(-5 * time.Minute),
		},
	}

	overview := buildDashboardOverview(leads, overdueThresholds{
		Dispatch: &dispatchThreshold,
		Claim:    &claimThreshold,
		Followup: 10 * time.Minute,
	})

	if overview.OverdueDispatchCount != 1 {
		t.Fatalf("dispatch overdue = %d, want 1", overview.OverdueDispatchCount)
	}
	if overview.OverdueClaimCount != 1 {
		t.Fatalf("claim overdue = %d, want 1", overview.OverdueClaimCount)
	}
}

func TestDashboardOverdueDisabledWhenReminderRulesDisabled(t *testing.T) {
	now := time.Now()
	assignedAt := now.Add(-30 * time.Minute)
	leads := []*types.Lead{
		{
			Status:    types.LeadStatusPendingDispatch,
			CreatedAt: now.Add(-30 * time.Minute),
		},
		{
			Status:     types.LeadStatusAssignedPendingClaim,
			AssignedAt: &assignedAt,
			CreatedAt:  now.Add(-30 * time.Minute),
		},
	}

	overview := buildDashboardOverview(leads, overdueThresholds{Followup: 10 * time.Minute})
	assignment := buildAssignmentOverview(leads, overdueThresholds{Followup: 10 * time.Minute})

	if overview.OverdueDispatchCount != 0 || overview.OverdueClaimCount != 0 {
		t.Fatalf("overview overdue = dispatch:%d claim:%d, want 0/0", overview.OverdueDispatchCount, overview.OverdueClaimCount)
	}
	if assignment.OverdueClaimCount != 0 {
		t.Fatalf("assignment overdue claim = %d, want 0", assignment.OverdueClaimCount)
	}
}

func TestCurrentTasksIgnoreHistoricalPlansAndIneligibleLeads(t *testing.T) {
	now := time.Now()
	due := now.Add(-time.Minute)
	tomorrow := types.BusinessDayStart(now).AddDate(0, 0, 1).Add(time.Hour)
	makeLead := func(id int64) *types.Lead {
		return &types.Lead{ID: id, Status: types.LeadStatusFollowing, BusinessStage: types.BusinessStageInvitation, OwnershipState: types.OwnershipPrivate, ResultStatus: types.LeadResultActive, CurrentOwnerUserID: "new-owner", NextFollowupDueAt: &due}
	}
	current := makeLead(1)
	terminal := makeLead(2)
	terminal.Status = types.LeadStatusConverted
	pool := makeLead(3)
	pool.OwnershipState = types.OwnershipPublicPool
	pending := makeLead(4)
	pending.OwnershipState = types.OwnershipAssignedPendingClaim
	appointment := makeLead(5)
	appointmentID := int64(1)
	appointment.CurrentAppointmentID = &appointmentID
	future := makeLead(6)
	future.NextFollowupDueAt = &tomorrow
	leads := []*types.Lead{current, terminal, pool, pending, appointment, future}
	histories := []*types.LeadFollowup{{LeadID: 1, SalesUserID: "old-owner", NextFollowupAt: &due}, {LeadID: 1, SalesUserID: "old-owner", NextFollowupAt: &due}, {LeadID: 2, NextFollowupAt: &due}}
	tasks := buildPendingFollowupTasks(leads, histories)
	stats := buildFollowupStats(leads, histories)
	if len(tasks) != 1 || tasks[0].LeadID != 1 || tasks[0].SalesUserID != "new-owner" || !tasks[0].Overdue {
		t.Fatalf("current tasks=%+v", tasks)
	}
	if stats.OverdueCount != 1 || stats.TotalRecords != 3 {
		t.Fatalf("current/history counts=%+v", stats)
	}
}
