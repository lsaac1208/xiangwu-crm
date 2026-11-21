package usecase

import (
	"context"
	"sort"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

type SupervisionSummary struct {
	PendingDispatch      int `json:"pending_dispatch"`
	PendingClaim         int `json:"pending_claim"`
	OverdueFollowup      int `json:"overdue_followup"`
	OverdueFirstFollowup int `json:"overdue_first_followup"`
	UnknownFirstFollowup int `json:"unknown_first_followup"`
	MissingDeadline      int `json:"missing_deadline"`
}

type SupervisionOwner struct {
	UserID               string `json:"userid"`
	Name                 string `json:"name"`
	OverdueFollowup      int    `json:"overdue_followup"`
	OverdueFirstFollowup int    `json:"overdue_first_followup"`
	PendingClaim         int    `json:"pending_claim"`
	UnknownFirstFollowup int    `json:"unknown_first_followup"`
	MissingDeadline      int    `json:"missing_deadline"`
}

type SupervisionTask struct {
	Type      string     `json:"type"`
	LeadID    int64      `json:"lead_id"`
	Title     string     `json:"title"`
	OwnerID   string     `json:"owner_userid"`
	OwnerName string     `json:"owner_name"`
	DueAt     *time.Time `json:"due_at"`
}

type FollowupSupervisionResponse struct {
	GeneratedAt time.Time          `json:"generated_at"`
	Summary     SupervisionSummary `json:"summary"`
	Owners      []SupervisionOwner `json:"owners"`
	Tasks       []SupervisionTask  `json:"tasks"`
}

func (uc *ManagementStatsUseCase) Supervision(ctx context.Context) (*FollowupSupervisionResponse, error) {
	now := uc.now()
	resp := &FollowupSupervisionResponse{GeneratedAt: now, Owners: []SupervisionOwner{}, Tasks: []SupervisionTask{}}
	owners := map[string]*SupervisionOwner{}
	ownerRow := func(userid string) *SupervisionOwner {
		if owners[userid] == nil {
			owners[userid] = &SupervisionOwner{UserID: userid}
		}
		return owners[userid]
	}
	err := uc.leads.VisitManagementLeads(ctx, repo.LeadStatsFilter{}, func(lead *types.Lead) {
		types.ProjectLegacyWorkflow(lead)
		if lead.ResultStatus != types.LeadResultActive || types.IsTerminalLeadStatus(lead.Status) {
			return
		}
		title := firstNonEmpty(lead.CustomerName, lead.LeadNo)
		if lead.BusinessStage == types.BusinessStageInvitation && lead.OwnershipState == types.OwnershipPendingAssignment {
			resp.Summary.PendingDispatch++
			addSupervisionTask(resp, SupervisionTask{Type: "pending_dispatch", LeadID: lead.ID, Title: title + " · 待分配"})
		}
		if lead.OwnershipState == types.OwnershipAssignedPendingClaim {
			userid := firstNonEmpty(lead.AssignedInviterUserID, lead.InviterUserID)
			resp.Summary.PendingClaim++
			ownerRow(userid).PendingClaim++
			addSupervisionTask(resp, SupervisionTask{Type: "pending_claim", LeadID: lead.ID, OwnerID: userid, Title: title + " · 待领取"})
			return
		}
		// Reuse the workflow's eligibility rule. A missing deadline is recorded
		// separately instead of being silently presented as "zero overdue".
		candidate := *lead
		if candidate.NextFollowupDueAt == nil {
			candidate.NextFollowupDueAt = &now
		}
		if !types.IsCurrentFollowupEligible(&candidate) {
			return
		}
		owner := ownerRow(lead.CurrentOwnerUserID)
		cycleKnown := lead.CurrentOwnerStartedAt != nil && !lead.CurrentOwnerStartedAt.After(now)
		if lead.CurrentOwnerFirstFollowupAt != nil {
			_, cycleKnown = types.OwnerResponseDuration(lead)
			cycleKnown = cycleKnown && !lead.CurrentOwnerFirstFollowupAt.After(now)
		}
		if !cycleKnown {
			resp.Summary.UnknownFirstFollowup++
			owner.UnknownFirstFollowup++
		}
		if lead.NextFollowupDueAt == nil {
			resp.Summary.MissingDeadline++
			owner.MissingDeadline++
			addSupervisionTask(resp, SupervisionTask{Type: "missing_deadline", LeadID: lead.ID, OwnerID: lead.CurrentOwnerUserID, Title: title + " · 未设置跟进截止"})
			return
		}
		if !lead.NextFollowupDueAt.Before(now) {
			return
		}
		resp.Summary.OverdueFollowup++
		owner.OverdueFollowup++
		taskType, label := "overdue_followup", "跟进逾期"
		if cycleKnown && lead.CurrentOwnerFirstFollowupAt == nil {
			resp.Summary.OverdueFirstFollowup++
			owner.OverdueFirstFollowup++
			taskType, label = "overdue_first_followup", "首跟超时"
		}
		addSupervisionTask(resp, SupervisionTask{Type: taskType, LeadID: lead.ID, OwnerID: lead.CurrentOwnerUserID, Title: title + " · " + label, DueAt: lead.NextFollowupDueAt})
	})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(owners))
	for userid := range owners {
		ids = append(ids, userid)
	}
	names, err := loadUserDisplayNames(ctx, uc.users, ids)
	if err != nil {
		return nil, err
	}
	for _, owner := range owners {
		owner.Name = firstNonEmpty(names[owner.UserID], owner.UserID, "尚未指定")
		resp.Owners = append(resp.Owners, *owner)
	}
	for i := range resp.Tasks {
		resp.Tasks[i].OwnerName = firstNonEmpty(names[resp.Tasks[i].OwnerID], resp.Tasks[i].OwnerID, "尚未指定")
	}
	sort.Slice(resp.Owners, func(i, j int) bool {
		a, b := resp.Owners[i], resp.Owners[j]
		if a.OverdueFollowup != b.OverdueFollowup {
			return a.OverdueFollowup > b.OverdueFollowup
		}
		if a.PendingClaim != b.PendingClaim {
			return a.PendingClaim > b.PendingClaim
		}
		return a.UserID < b.UserID
	})
	return resp, nil
}

// Keep only the twenty highest-priority tasks while visiting the full cohort.
func addSupervisionTask(resp *FollowupSupervisionResponse, task SupervisionTask) {
	index := sort.Search(len(resp.Tasks), func(i int) bool { return supervisionTaskLess(task, resp.Tasks[i]) })
	if index >= 20 {
		return
	}
	resp.Tasks = append(resp.Tasks, task)
	copy(resp.Tasks[index+1:], resp.Tasks[index:len(resp.Tasks)-1])
	resp.Tasks[index] = task
	if len(resp.Tasks) > 20 {
		resp.Tasks = resp.Tasks[:20]
	}
}
func supervisionTaskLess(a, b SupervisionTask) bool {
	if (a.DueAt != nil) != (b.DueAt != nil) {
		return a.DueAt != nil
	}
	if a.DueAt != nil && !a.DueAt.Equal(*b.DueAt) {
		return a.DueAt.Before(*b.DueAt)
	}
	return a.LeadID < b.LeadID
}
