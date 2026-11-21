package usecase

import (
	"context"
	"sort"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

type WorkbenchSummary struct {
	PendingDispatch              int `json:"pending_dispatch"`
	PendingClaim                 int `json:"pending_claim"`
	OverdueFollowup              int `json:"overdue_followup"`
	AppointmentPendingAssignment int `json:"appointment_pending_assignment"`
	AppointmentUnconfirmed       int `json:"appointment_unconfirmed"`
	TodayAppointments            int `json:"today_appointments"`
	NegotiationActive            int `json:"negotiation_active"`
	HandledToday                 int `json:"handled_today"`
}

type WorkbenchTask struct {
	Type          string     `json:"type"`
	Severity      string     `json:"severity"`
	LeadID        int64      `json:"lead_id"`
	LeadNo        string     `json:"lead_no,omitempty"`
	CustomerName  string     `json:"customer_name,omitempty"`
	AppointmentID int64      `json:"appointment_id,omitempty"`
	Title         string     `json:"title"`
	DueAt         *time.Time `json:"due_at,omitempty"`
}

type WorkbenchResponse struct {
	Role    string           `json:"role"`
	Summary WorkbenchSummary `json:"summary"`
	Tasks   []WorkbenchTask  `json:"tasks"`
}

type GetWorkbenchUseCase struct {
	leads        repo.LeadRepository
	appointments repo.AppointmentRepository
	now          func() time.Time
}

func NewGetWorkbenchUseCase(leads repo.LeadRepository, appointments repo.AppointmentRepository) *GetWorkbenchUseCase {
	return &GetWorkbenchUseCase{leads: leads, appointments: appointments, now: time.Now}
}

func (uc *GetWorkbenchUseCase) Execute(ctx context.Context, userID, roleName string) (*WorkbenchResponse, error) {
	role := types.UserRole(roleName)
	now := uc.now()
	var allLeads []*types.Lead
	var databaseSummary *repo.WorkbenchLeadSnapshot
	var err error
	if databaseRepo, ok := uc.leads.(repo.WorkbenchLeadRepository); ok {
		databaseSummary, err = databaseRepo.BuildWorkbenchLeadSnapshot(ctx, userID, role, now, 200)
		if err != nil {
			return nil, err
		}
		allLeads = databaseSummary.Leads
	} else {
		allLeads, _, err = uc.leads.ListAll(ctx, "", 0, 1000)
		if err != nil {
			return nil, err
		}
	}
	leads := make([]*types.Lead, 0, len(allLeads))
	leadIDs := make(map[int64]struct{}, len(allLeads))
	leadByID := make(map[int64]*types.Lead, len(allLeads))
	for _, lead := range allLeads {
		if lead == nil || !workbenchCanViewLead(lead, userID, role) {
			continue
		}
		types.ProjectLegacyWorkflow(lead)
		leads = append(leads, lead)
		leadIDs[lead.ID] = struct{}{}
		leadByID[lead.ID] = lead
	}

	var appointments []*types.LeadAppointment
	var appointmentSummary *repo.WorkbenchAppointmentSnapshot
	if databaseRepo, ok := uc.appointments.(repo.WorkbenchAppointmentRepository); ok {
		appointmentSummary, err = databaseRepo.BuildWorkbenchAppointmentSnapshot(ctx, userID, role, now, 50)
		if err == nil {
			appointments = appointmentSummary.Appointments
		}
	} else {
		appointments, err = uc.appointments.List(ctx, repo.AppointmentListFilter{})
	}
	if err != nil {
		return nil, err
	}
	resp := &WorkbenchResponse{Role: roleName, Tasks: make([]WorkbenchTask, 0)}
	if appointmentSummary != nil {
		resp.Summary.AppointmentPendingAssignment = appointmentSummary.PendingAssignment
		resp.Summary.AppointmentUnconfirmed = appointmentSummary.Unconfirmed
		resp.Summary.TodayAppointments = appointmentSummary.Today
	}
	if databaseSummary != nil {
		resp.Summary.PendingDispatch = databaseSummary.PendingDispatch
		resp.Summary.PendingClaim = databaseSummary.PendingClaim
		resp.Summary.OverdueFollowup = databaseSummary.OverdueFollowup
		resp.Summary.NegotiationActive = databaseSummary.NegotiationActive
		resp.Summary.HandledToday = databaseSummary.HandledToday
	}
	for _, lead := range leads {
		uc.addLeadTasksForUser(resp, lead, userID, role, now, databaseSummary == nil)
	}
	for _, appointment := range appointments {
		if appointment == nil || (appointmentSummary == nil && !workbenchCanViewAppointment(appointment, leadIDs, userID, role)) {
			continue
		}
		uc.addAppointmentTasksWithSummary(resp, appointment, role, now, appointmentSummary == nil)
	}
	for i := range resp.Tasks {
		if lead := leadByID[resp.Tasks[i].LeadID]; lead != nil {
			resp.Tasks[i].LeadNo, resp.Tasks[i].CustomerName = lead.LeadNo, lead.CustomerName
		}
	}
	sort.SliceStable(resp.Tasks, func(i, j int) bool {
		if severityRank(resp.Tasks[i].Severity) != severityRank(resp.Tasks[j].Severity) {
			return severityRank(resp.Tasks[i].Severity) > severityRank(resp.Tasks[j].Severity)
		}
		if resp.Tasks[i].DueAt == nil {
			return false
		}
		if resp.Tasks[j].DueAt == nil {
			return true
		}
		return resp.Tasks[i].DueAt.Before(*resp.Tasks[j].DueAt)
	})
	if len(resp.Tasks) > 50 {
		resp.Tasks = resp.Tasks[:50]
	}
	return resp, nil
}

func workbenchCanViewLead(lead *types.Lead, userID string, role types.UserRole) bool {
	if role == types.UserRoleAdmin || role == types.UserRoleManager {
		return true
	}
	if types.IsInvitationRole(role) {
		return lead.AssignedInviterUserID == userID || lead.InviterUserID == userID ||
			(lead.BusinessStage != types.BusinessStageNegotiation && (lead.AssignedSalesUserID == userID || lead.OwnerUserID == userID))
	}
	if role == types.UserRoleCloser {
		return lead.CloserUserID == userID || (lead.CurrentOwnerRole == types.OwnerRoleCloser && lead.CurrentOwnerUserID == userID)
	}
	if role == types.UserRoleDispatcher {
		return lead.Status == types.LeadStatusPendingDispatch || lead.CreatedByUserID == userID
	}
	return false
}

func workbenchCanViewAppointment(item *types.LeadAppointment, leadIDs map[int64]struct{}, userID string, role types.UserRole) bool {
	if role == types.UserRoleAdmin || role == types.UserRoleManager {
		_, ok := leadIDs[item.LeadID]
		return ok
	}
	if types.IsInvitationRole(role) {
		return item.InviterUserID == userID
	}
	return role == types.UserRoleCloser && item.CloserUserID == userID
}

func (uc *GetWorkbenchUseCase) addLeadTasks(resp *WorkbenchResponse, lead *types.Lead, role types.UserRole, now time.Time) {
	uc.addLeadTasksForUser(resp, lead, "", role, now, true)
}

func (uc *GetWorkbenchUseCase) addLeadTasksForUser(resp *WorkbenchResponse, lead *types.Lead, userID string, role types.UserRole, now time.Time, countSummary bool) {
	manager := role == types.UserRoleManager || role == types.UserRoleAdmin
	if (manager || role == types.UserRoleDispatcher) && lead.BusinessStage == types.BusinessStageInvitation && lead.OwnershipState == types.OwnershipPendingAssignment {
		if countSummary {
			resp.Summary.PendingDispatch++
		}
		resp.Tasks = append(resp.Tasks, WorkbenchTask{Type: "pending_dispatch", Severity: "high", LeadID: lead.ID, Title: "新线索待分配邀约师"})
	}
	if role == types.UserRoleDispatcher && lead.CreatedByUserID == userID {
		dayStart := types.BusinessDayStart(now)
		if !lead.UpdatedAt.Before(dayStart) {
			if countSummary {
				resp.Summary.HandledToday++
			}
		}
	}
	assignedToActor := lead.AssignedInviterUserID == userID || (userID == "" && lead.AssignedInviterUserID != "")
	closerCreatedPending := role == types.UserRoleCloser && lead.CreatedByUserID == userID
	if (((types.IsInvitationRole(role) || role == types.UserRoleManager) && assignedToActor) || closerCreatedPending) && lead.OwnershipState == types.OwnershipAssignedPendingClaim {
		if countSummary {
			resp.Summary.PendingClaim++
		}
		title := "线索待领取"
		if closerCreatedPending {
			title = "您创建的线索待邀约师领取"
		}
		resp.Tasks = append(resp.Tasks, WorkbenchTask{Type: "pending_claim", Severity: "high", LeadID: lead.ID, Title: title})
	}
	if types.IsCurrentFollowupEligible(lead) && (manager || userID == "" || lead.CurrentOwnerUserID == userID) && lead.NextFollowupDueAt.Before(now) {
		if countSummary {
			resp.Summary.OverdueFollowup++
		}
		resp.Tasks = append(resp.Tasks, WorkbenchTask{Type: "overdue_followup", Severity: "high", LeadID: lead.ID, Title: "客户回访已逾期", DueAt: lead.NextFollowupDueAt})
	}
	if lead.BusinessStage == types.BusinessStageNegotiation && lead.ResultStatus == types.LeadResultActive {
		if manager || role == types.UserRoleCloser {
			if countSummary {
				resp.Summary.NegotiationActive++
			}
		}
		if role == types.UserRoleCloser && lead.DeliveredAt != nil && (lead.LastFollowupAt == nil || !lead.LastFollowupAt.After(*lead.DeliveredAt)) {
			resp.Tasks = append(resp.Tasks, WorkbenchTask{Type: "negotiation_first_followup", Severity: "high", LeadID: lead.ID, Title: "已交付，待首次谈单跟进", DueAt: lead.NextFollowupDueAt})
		}
	}
}

func (uc *GetWorkbenchUseCase) addAppointmentTasks(resp *WorkbenchResponse, item *types.LeadAppointment, role types.UserRole, now time.Time) {
	uc.addAppointmentTasksWithSummary(resp, item, role, now, true)
}

func (uc *GetWorkbenchUseCase) addAppointmentTasksWithSummary(resp *WorkbenchResponse, item *types.LeadAppointment, role types.UserRole, now time.Time, countSummary bool) {
	manager := role == types.UserRoleManager || role == types.UserRoleAdmin
	if manager && item.Status == types.AppointmentPendingAssignment {
		if countSummary {
			resp.Summary.AppointmentPendingAssignment++
		}
		resp.Tasks = append(resp.Tasks, WorkbenchTask{Type: "appointment_pending_assignment", Severity: "high", LeadID: item.LeadID, AppointmentID: item.ID, Title: "预约待分配谈单师", DueAt: &item.StartAt})
	}
	if item.Status != types.AppointmentScheduled {
		return
	}
	if !item.StartAt.After(now) {
		if countSummary {
			resp.Summary.AppointmentUnconfirmed++
		}
		resp.Tasks = append(resp.Tasks, WorkbenchTask{Type: "appointment_unconfirmed", Severity: "high", LeadID: item.LeadID, AppointmentID: item.ID, Title: "预约时间已到，尚未确认到访", DueAt: &item.StartAt})
	}
	dayStart := types.BusinessDayStart(now)
	if !item.StartAt.Before(dayStart) && item.StartAt.Before(dayStart.Add(24*time.Hour)) {
		if countSummary {
			resp.Summary.TodayAppointments++
		}
		if role == types.UserRoleCloser && item.StartAt.After(now) {
			resp.Tasks = append(resp.Tasks, WorkbenchTask{Type: "appointment_today", Severity: "normal", LeadID: item.LeadID, AppointmentID: item.ID, Title: "今日待接待预约", DueAt: &item.StartAt})
		}
	}
}

func severityRank(severity string) int {
	if severity == "high" {
		return 2
	}
	return 1
}
