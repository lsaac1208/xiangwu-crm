package sla

import (
	"testing"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

func TestCalculatorCalculateLeadSLA(t *testing.T) {
	calculator := NewCalculator()
	createdAt := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	assignedAt := createdAt.Add(2 * time.Minute)
	claimedAt := assignedAt.Add(3 * time.Minute)
	firstFollowupAt := claimedAt.Add(5 * time.Minute)

	result := calculator.CalculateLeadSLA(&types.Lead{
		ID:                    1,
		LeadNo:                "L1",
		Status:                types.LeadStatusFollowing,
		DispatchManagerUserID: "manager1",
		OwnerUserID:           "sales1",
		CreatedAt:             createdAt,
		AssignedAt:            &assignedAt,
		ClaimedAt:             &claimedAt,
		FirstFollowupAt:       &firstFollowupAt, CurrentOwnerStartedAt: &claimedAt, CurrentOwnerFirstFollowupAt: &firstFollowupAt,
	})

	if *result.DispatchResponseTime != 2*time.Minute {
		t.Fatalf("dispatch time = %v, want 2m", *result.DispatchResponseTime)
	}
	if *result.ClaimResponseTime != 3*time.Minute {
		t.Fatalf("claim time = %v, want 3m", *result.ClaimResponseTime)
	}
	if *result.FirstFollowupTime != 5*time.Minute {
		t.Fatalf("first followup time = %v, want 5m", *result.FirstFollowupTime)
	}
	if *result.TotalResponseTime != 10*time.Minute {
		t.Fatalf("total response time = %v, want 10m", *result.TotalResponseTime)
	}
}

func TestCalculatorAggregateSLAUsesNearestRankPercentiles(t *testing.T) {
	calculator := NewCalculator()
	base := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)

	leads := []*types.Lead{
		leadWithDispatch(base, 1*time.Minute),
		leadWithDispatch(base, 2*time.Minute),
		leadWithDispatch(base, 3*time.Minute),
		leadWithDispatch(base, 4*time.Minute),
	}

	result := calculator.AggregateSLA(leads)
	if result.TotalLeads != 4 {
		t.Fatalf("total leads = %d, want 4", result.TotalLeads)
	}
	if result.AvgDispatchTime == nil {
		t.Fatal("dispatch stats is nil")
	}
	if result.AvgDispatchTime.P50 != 2*time.Minute {
		t.Fatalf("p50 = %v, want 2m", result.AvgDispatchTime.P50)
	}
	if result.AvgDispatchTime.P90 != 4*time.Minute {
		t.Fatalf("p90 = %v, want 4m", result.AvgDispatchTime.P90)
	}
	if result.AvgDispatchTime.P95 != 4*time.Minute {
		t.Fatalf("p95 = %v, want 4m", result.AvgDispatchTime.P95)
	}
}

func leadWithDispatch(createdAt time.Time, duration time.Duration) *types.Lead {
	assignedAt := createdAt.Add(duration)
	return &types.Lead{
		Status:                types.LeadStatusAssignedPendingClaim,
		DispatchManagerUserID: "manager1",
		CreatedAt:             createdAt,
		AssignedAt:            &assignedAt,
	}
}

func TestOwnerCycleDoesNotReuseLifecycleTime(t *testing.T) {
	now := time.Now()
	old := now.Add(-time.Hour)
	started := now.Add(-time.Minute)
	first := now
	lead := &types.Lead{CreatedAt: old.Add(-time.Hour), FirstFollowupAt: &old, ClaimedAt: &started, CurrentOwnerStartedAt: &started, CurrentOwnerFirstFollowupAt: &first}
	calculator := NewCalculator()
	result := calculator.CalculateLeadSLA(lead)
	if result.FirstFollowupTime == nil || *result.FirstFollowupTime != time.Minute || *result.TotalResponseTime != time.Hour {
		t.Fatalf("wrong responsibility/lifecycle SLA: %+v", result)
	}
	lead.CurrentOwnerFirstFollowupAt = &old
	if result = calculator.CalculateLeadSLA(lead); result.FirstFollowupTime != nil {
		t.Fatal("negative time included")
	}
	if calculator.AggregateSLA([]*types.Lead{lead}).UnknownOwnerResponseCount != 1 {
		t.Fatal("unknown duration not reported")
	}
}
