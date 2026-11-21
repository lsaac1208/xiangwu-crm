package usecase

import (
	"context"
	"sort"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

type ManagementLeadReader interface {
	VisitManagementLeads(context.Context, repo.LeadStatsFilter, func(*types.Lead)) error
}

type ManagementTeamStats struct {
	SalesUserID          string   `json:"sales_userid"`
	SalesName            string   `json:"sales_name"`
	Role                 string   `json:"role"`
	Assigned             int      `json:"assigned"`
	Converted            int      `json:"converted"`
	PendingClaim         int      `json:"pending_claim"`
	FirstFollowups       int      `json:"first_followups"`
	UnknownFirstFollowup int      `json:"unknown_first_followup"`
	FirstFollowupRate    *float64 `json:"first_followup_rate"`
	ConversionRate       float64  `json:"conversion_rate"`
	OverdueFollowupCount int      `json:"overdue_followup_count"`
}

type ManagementCohort struct {
	Total           int `json:"total"`
	Assigned        int `json:"assigned"`
	PendingDispatch int `json:"pending_dispatch"`
	PendingClaim    int `json:"pending_claim"`
	Converted       int `json:"converted"`
	Invalid         int `json:"invalid"`
}

type ManagementStatsResponse struct {
	DateBasis   string                `json:"date_basis"`
	GeneratedAt time.Time             `json:"generated_at"`
	Assignment  ManagementCohort      `json:"assignment"`
	Sources     []SourceStats         `json:"sources"`
	Teams       []ManagementTeamStats `json:"teams"`
}

type ManagementStatsUseCase struct {
	leads ManagementLeadReader
	users repo.UserBindingRepository
	now   func() time.Time
}

func NewManagementStatsUseCase(leads ManagementLeadReader, users repo.UserBindingRepository) *ManagementStatsUseCase {
	return &ManagementStatsUseCase{leads: leads, users: users, now: time.Now}
}

func (uc *ManagementStatsUseCase) Report(ctx context.Context, filter repo.LeadStatsFilter) (*ManagementStatsResponse, error) {
	now := uc.now()
	result := &ManagementStatsResponse{DateBasis: "lead_created_at", GeneratedAt: now, Sources: []SourceStats{}, Teams: []ManagementTeamStats{}}
	sources := map[[2]string]*SourceStats{}
	teams := map[[2]string]*ManagementTeamStats{}
	err := uc.leads.VisitManagementLeads(ctx, filter, func(lead *types.Lead) {
		types.ProjectLegacyWorkflow(lead)
		a := &result.Assignment
		a.Total++
		if lead.OwnershipState == types.OwnershipPendingAssignment {
			a.PendingDispatch++
		}
		if lead.OwnershipState == types.OwnershipAssignedPendingClaim {
			a.PendingClaim++
		}
		inviter := firstNonEmpty(lead.AssignedInviterUserID, lead.InviterUserID)
		closer := lead.CloserUserID
		if closer == "" && lead.CurrentOwnerRole == types.OwnerRoleCloser {
			closer = lead.CurrentOwnerUserID
		}
		if inviter != "" || closer != "" {
			a.Assigned++
		}
		if lead.Status == types.LeadStatusConverted {
			a.Converted++
		}
		if lead.Status == types.LeadStatusInvalid {
			a.Invalid++
		}
		source, account := splitSource(lead.SourceChannel)
		source = firstNonEmpty(source, "未知来源")
		key := [2]string{source, account}
		if sources[key] == nil {
			sources[key] = &SourceStats{Source: source, Account: account}
		}
		s := sources[key]
		s.Total++
		if lead.FirstFollowupAt != nil && !lead.FirstFollowupAt.After(now) {
			s.FirstFollowups++
		}
		if lead.Status == types.LeadStatusConverted {
			s.Converted++
		}
		if lead.Status == types.LeadStatusInvalid {
			s.Invalid++
		}
		for _, owner := range [][2]string{{"inviter", inviter}, {"closer", closer}} {
			if owner[1] == "" {
				continue
			}
			if teams[owner] == nil {
				teams[owner] = &ManagementTeamStats{Role: owner[0], SalesUserID: owner[1]}
			}
			t := teams[owner]
			t.Assigned++
			if lead.Status == types.LeadStatusConverted {
				t.Converted++
			}
			if owner[0] == "inviter" && lead.OwnershipState == types.OwnershipAssignedPendingClaim {
				t.PendingClaim++
			}
			followed, known := managementFirstResponse(lead, owner[1], owner[0], now)
			if followed {
				t.FirstFollowups++
			}
			if !known {
				t.UnknownFirstFollowup++
			}
			if lead.CurrentOwnerUserID == owner[1] && string(lead.CurrentOwnerRole) == owner[0] && types.IsCurrentFollowupEligible(lead) && lead.NextFollowupDueAt.Before(now) {
				t.OverdueFollowupCount++
			}
		}
	})
	if err != nil {
		return nil, err
	}
	for _, s := range sources {
		s.ConversionRate = ratio(s.Converted, s.Total)
		s.InvalidRate = ratio(s.Invalid, s.Total)
		result.Sources = append(result.Sources, *s)
	}
	ids := make([]string, 0, len(teams))
	for _, t := range teams {
		ids = append(ids, t.SalesUserID)
	}
	names, err := loadUserDisplayNames(ctx, uc.users, ids)
	if err != nil {
		return nil, err
	}
	for _, t := range teams {
		t.SalesName = firstNonEmpty(names[t.SalesUserID], t.SalesUserID)
		t.ConversionRate = ratio(t.Converted, t.Assigned)
		if known := t.Assigned - t.UnknownFirstFollowup; known > 0 {
			rate := ratio(t.FirstFollowups, known)
			t.FirstFollowupRate = &rate
		}
		result.Teams = append(result.Teams, *t)
	}
	sort.Slice(result.Teams, func(i, j int) bool {
		a, b := result.Teams[i], result.Teams[j]
		if a.Role != b.Role {
			return a.Role < b.Role
		}
		if a.Converted != b.Converted {
			return a.Converted > b.Converted
		}
		return a.SalesUserID < b.SalesUserID
	})
	sort.Slice(result.Sources, func(i, j int) bool {
		a, b := result.Sources[i], result.Sources[j]
		if a.Total != b.Total {
			return a.Total > b.Total
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Account < b.Account
	})
	return result, nil
}

// Do not attribute another owner's first follow-up to a new responsibility
// cycle, or treat an unrecorded historical cycle as an immediate response.
func managementFirstResponse(lead *types.Lead, userid, role string, now time.Time) (bool, bool) {
	if lead.CurrentOwnerStartedAt != nil && lead.CurrentOwnerStartedAt.After(now) {
		return false, false
	}
	if lead.CurrentOwnerFirstFollowupAt != nil && lead.CurrentOwnerFirstFollowupAt.After(now) {
		return false, false
	}
	if role == "inviter" && lead.OwnershipState == types.OwnershipAssignedPendingClaim {
		return false, true
	}
	if lead.CurrentOwnerUserID == userid && string(lead.CurrentOwnerRole) == role && lead.CurrentOwnerStartedAt != nil {
		if lead.CurrentOwnerFirstFollowupAt == nil {
			return false, true
		}
		_, known := types.OwnerResponseDuration(lead)
		return known, known
	}
	if role == "inviter" && lead.InviterClaimedAt != nil && !lead.InviterClaimedAt.After(now) && lead.FirstFollowupAt != nil && !lead.FirstFollowupAt.After(now) && !lead.FirstFollowupAt.Before(*lead.InviterClaimedAt) && (lead.DeliveredAt == nil || !lead.FirstFollowupAt.After(*lead.DeliveredAt)) {
		return true, true
	}
	return false, false
}
