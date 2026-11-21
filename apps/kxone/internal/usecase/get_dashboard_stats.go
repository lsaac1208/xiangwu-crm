package usecase

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/domain/sla"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/pkg/metrics"
	"github.com/lsaac128/kxone/pkg/types"
)

type GetDashboardStatsUseCase struct {
	leadRepo        repo.LeadRepository
	followupRepo    repo.FollowupRepository
	userBindingRepo repo.UserBindingRepository
	slaCalc         *sla.Calculator
	runtimeSettings runtime.RuntimeSettings
	appointmentRepo repo.AppointmentRepository
	poolRepo        repo.LeadPoolRepository
}

func (uc *GetDashboardStatsUseCase) WithDualStageRepositories(appointments repo.AppointmentRepository, pools repo.LeadPoolRepository) *GetDashboardStatsUseCase {
	uc.appointmentRepo = appointments
	uc.poolRepo = pools
	return uc
}

func NewGetDashboardStatsUseCase(leadRepo repo.LeadRepository, followupRepo repo.FollowupRepository) *GetDashboardStatsUseCase {
	return &GetDashboardStatsUseCase{
		leadRepo:     leadRepo,
		followupRepo: followupRepo,
		slaCalc:      sla.NewCalculator(),
	}
}

func (uc *GetDashboardStatsUseCase) WithUserBindingRepository(userBindingRepo repo.UserBindingRepository) *GetDashboardStatsUseCase {
	uc.userBindingRepo = userBindingRepo
	return uc
}

func (uc *GetDashboardStatsUseCase) WithRuntimeSettings(runtimeSettings runtime.RuntimeSettings) *GetDashboardStatsUseCase {
	uc.runtimeSettings = runtimeSettings
	return uc
}

type GetDashboardStatsRequest struct {
	CurrentUserID string
	Role          string
	SalesUserID   string
	StartTime     *time.Time
	EndTime       *time.Time
	Limit         int
}

type DashboardStatsResponse struct {
	DateBasis  string                  `json:"date_basis"`
	Overview   DashboardOverview       `json:"overview"`
	Assignment AssignmentOverview      `json:"assignment"`
	Funnel     []NameCount             `json:"funnel"`
	SLA        *sla.AggregatedSLA      `json:"sla"`
	Sources    []SourceStats           `json:"sources"`
	Sales      []SalesDashboardStats   `json:"sales"`
	Managers   []ManagerDashboardStats `json:"managers,omitempty"`
	Followup   FollowupQualityStats    `json:"followup"`
	NextTasks  []PendingFollowupLead   `json:"next_tasks"`
	Business   BusinessConversionStats `json:"business"`
	DualStage  DualStageMetrics        `json:"dual_stage"`
	Health     map[string]any          `json:"health"`
	TimeRange  TimeRange               `json:"time_range"`
}

type DualStageMetrics struct {
	InvitationClaimedCount        int     `json:"invitation_claimed_count"`
	AppointmentCount              int     `json:"appointment_count"`
	ArrivedCount                  int     `json:"arrived_count"`
	NegotiationConvertedCount     int     `json:"negotiation_converted_count"`
	PoolEnteredCount              int     `json:"pool_entered_count"`
	PoolClaimedCount              int     `json:"pool_claimed_count"`
	FollowupDueCount              int     `json:"followup_due_count"`
	FollowupOnTimeCount           int     `json:"followup_on_time_count"`
	InvitationRate                float64 `json:"invitation_rate"`
	ArrivalRate                   float64 `json:"arrival_rate"`
	NegotiationConversionRate     float64 `json:"negotiation_conversion_rate"`
	ScheduleUtilizationRate       float64 `json:"schedule_utilization_rate"`
	PoolTurnoverRate              float64 `json:"pool_turnover_rate"`
	CurrentFollowupTimelinessRate float64 `json:"current_followup_timeliness_rate"`
}

type AssignmentOverview struct {
	Total                int     `json:"total"`
	Assigned             int     `json:"assigned"`
	PendingDispatch      int     `json:"pending_dispatch"`
	PendingClaim         int     `json:"pending_claim"`
	Claimed              int     `json:"claimed"`
	ClaimedOrFollowing   int     `json:"claimed_or_following"`
	FirstFollowupCount   int     `json:"first_followup_count"`
	Converted            int     `json:"converted"`
	Invalid              int     `json:"invalid"`
	AssignRate           float64 `json:"assign_rate"`
	ClaimRate            float64 `json:"claim_rate"`
	FirstFollowupRate    float64 `json:"first_followup_rate"`
	ConversionRate       float64 `json:"conversion_rate"`
	OverdueClaimCount    int     `json:"overdue_claim_count"`
	OverdueFollowupCount int     `json:"overdue_followup_count"`
}

type DashboardOverview struct {
	TotalLeads           int     `json:"total_leads"`
	NewLeads             int     `json:"new_leads"`
	PendingDispatch      int     `json:"pending_dispatch"`
	PendingClaim         int     `json:"pending_claim"`
	Claimed              int     `json:"claimed"`
	Following            int     `json:"following"`
	Converted            int     `json:"converted"`
	Invalid              int     `json:"invalid"`
	Closed               int     `json:"closed"`
	FirstFollowupCount   int     `json:"first_followup_count"`
	FirstFollowupRate    float64 `json:"first_followup_rate"`
	ClaimRate            float64 `json:"claim_rate"`
	ConversionRate       float64 `json:"conversion_rate"`
	InvalidRate          float64 `json:"invalid_rate"`
	TotalDealAmount      float64 `json:"total_deal_amount"`
	AverageDealAmount    float64 `json:"average_deal_amount"`
	OverdueDispatchCount int     `json:"overdue_dispatch_count"`
	OverdueClaimCount    int     `json:"overdue_claim_count"`
	OverdueFollowupCount int     `json:"overdue_followup_count"`
}

type NameCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type SourceStats struct {
	Source         string  `json:"source"`
	Account        string  `json:"account,omitempty"`
	Total          int     `json:"total"`
	FirstFollowups int     `json:"first_followups"`
	Converted      int     `json:"converted"`
	Invalid        int     `json:"invalid"`
	ConversionRate float64 `json:"conversion_rate"`
	InvalidRate    float64 `json:"invalid_rate"`
}

type SalesDashboardStats struct {
	UnknownOwnerResponseCount int                `json:"unknown_owner_response_count"`
	SalesUserID               string             `json:"sales_userid"`
	SalesName                 string             `json:"sales_name,omitempty"`
	Assigned                  int                `json:"assigned"`
	PendingClaim              int                `json:"pending_claim"`
	Claimed                   int                `json:"claimed"`
	Following                 int                `json:"following"`
	FirstFollowups            int                `json:"first_followups"`
	Converted                 int                `json:"converted"`
	Invalid                   int                `json:"invalid"`
	FollowupRecords           int                `json:"followup_records"`
	OverdueClaimCount         int                `json:"overdue_claim_count"`
	OverdueFollowupCount      int                `json:"overdue_followup_count"`
	ClaimRate                 float64            `json:"claim_rate"`
	FirstFollowupRate         float64            `json:"first_followup_rate"`
	ConversionRate            float64            `json:"conversion_rate"`
	AvgClaimTime              *sla.DurationStats `json:"avg_claim_time,omitempty"`
	AvgFirstFollowupTime      *sla.DurationStats `json:"avg_first_followup_time,omitempty"`
}

type ManagerDashboardStats struct {
	ManagerUserID   string             `json:"manager_userid"`
	ManagerName     string             `json:"manager_name,omitempty"`
	Total           int                `json:"total"`
	Assigned        int                `json:"assigned"`
	PendingDispatch int                `json:"pending_dispatch"`
	Converted       int                `json:"converted"`
	ConversionRate  float64            `json:"conversion_rate"`
	AvgDispatchTime *sla.DurationStats `json:"avg_dispatch_time,omitempty"`
}

type FollowupQualityStats struct {
	TotalRecords      int         `json:"total_records"`
	AvgRecordsPerLead float64     `json:"avg_records_per_lead"`
	ByResult          []NameCount `json:"by_result"`
	ByIntention       []NameCount `json:"by_intention"`
	InvalidReasons    []NameCount `json:"invalid_reasons"`
	DueTodayCount     int         `json:"due_today_count"`
	OverdueCount      int         `json:"overdue_count"`
}

type PendingFollowupLead struct {
	LeadID         int64     `json:"lead_id"`
	LeadNo         string    `json:"lead_no"`
	CustomerName   string    `json:"customer_name"`
	ProductName    string    `json:"product_name"`
	SalesUserID    string    `json:"sales_userid"`
	SalesName      string    `json:"sales_name,omitempty"`
	NextFollowupAt time.Time `json:"next_followup_at"`
	Overdue        bool      `json:"overdue"`
}

type BusinessConversionStats struct {
	ConvertedCount    int     `json:"converted_count"`
	ClosedCount       int     `json:"closed_count"`
	TotalDealAmount   float64 `json:"total_deal_amount"`
	AverageDealAmount float64 `json:"average_deal_amount"`
}

func (uc *GetDashboardStatsUseCase) Execute(ctx context.Context, req *GetDashboardStatsRequest) (*DashboardStatsResponse, error) {
	// Reporting totals must cover the complete authorized cohort. Limit is kept
	// in the request for source compatibility, but never truncates aggregates.

	leads, _, err := uc.listScopedLeads(ctx, req)
	if err != nil {
		return nil, err
	}

	followups, err := uc.listScopedFollowups(ctx, leads)
	if err != nil {
		return nil, err
	}
	// Historical quality uses all scoped follow-ups; current tasks belong only to the current owner.
	taskLeads := leads
	if req.SalesUserID != "" {
		taskLeads = nil
		for _, lead := range leads {
			if lead.CurrentOwnerUserID == req.SalesUserID {
				taskLeads = append(taskLeads, lead)
			}
		}
	}
	thresholds := uc.overdueThresholds(ctx)

	resp := &DashboardStatsResponse{
		DateBasis:  "lead_created_at",
		Overview:   buildDashboardOverview(leads, thresholds),
		Assignment: buildAssignmentOverview(leads, thresholds),
		Funnel:     buildFunnel(leads, thresholds),
		SLA:        uc.slaCalc.AggregateSLA(leads),
		Sources:    buildSourceStats(leads),
		Sales:      buildSalesStats(leads, followups, uc.slaCalc, thresholds),
		Followup:   buildFollowupStats(leads, followups),
		NextTasks:  buildPendingFollowupTasks(taskLeads, followups),
		Business:   buildBusinessStats(leads),
		Health:     metrics.GetGlobalMetrics().GetStats(),
		TimeRange:  dashboardTimeRange(leads, req.StartTime, req.EndTime),
	}
	currentCounts := buildFollowupStats(taskLeads, nil)
	resp.Followup.DueTodayCount, resp.Followup.OverdueCount = currentCounts.DueTodayCount, currentCounts.OverdueCount
	resp.DualStage, err = uc.buildDualStageMetrics(ctx, leads, req.StartTime, req.EndTime)
	if err != nil {
		return nil, err
	}

	if req.Role == string(types.UserRoleAdmin) {
		resp.Managers = buildManagerStats(leads, uc.slaCalc)
	}

	if err := uc.enrichDisplayNames(ctx, resp); err != nil {
		return nil, err
	}

	return resp, nil
}

type overdueThresholds struct {
	Dispatch *time.Duration
	Claim    *time.Duration
	Followup time.Duration
}

func (uc *GetDashboardStatsUseCase) overdueThresholds(ctx context.Context) overdueThresholds {
	return overdueThresholds{
		Dispatch: firstPositiveDelay(uc.dispatchReminderDelays(ctx)),
		Claim:    firstPositiveDelay(uc.claimReminderDelays(ctx)),
		Followup: 10 * time.Minute,
	}
}

func (uc *GetDashboardStatsUseCase) dispatchReminderDelays(ctx context.Context) []time.Duration {
	if uc.runtimeSettings == nil {
		return []time.Duration{3 * time.Minute, 5 * time.Minute, 10 * time.Minute}
	}
	return uc.runtimeSettings.DispatchReminderDelays(ctx)
}

func (uc *GetDashboardStatsUseCase) claimReminderDelays(ctx context.Context) []time.Duration {
	if uc.runtimeSettings == nil {
		return []time.Duration{3 * time.Minute, 5 * time.Minute, 10 * time.Minute}
	}
	return uc.runtimeSettings.ClaimReminderDelays(ctx)
}

func firstPositiveDelay(delays []time.Duration) *time.Duration {
	for _, delay := range delays {
		if delay > 0 {
			value := delay
			return &value
		}
	}
	return nil
}

func (uc *GetDashboardStatsUseCase) listScopedLeads(ctx context.Context, req *GetDashboardStatsRequest) ([]*types.Lead, int, error) {
	if statsRepo, ok := uc.leadRepo.(repo.LeadStatsRepository); ok {
		filter := repo.LeadStatsFilter{
			SalesUserID: req.SalesUserID,
			StartTime:   req.StartTime,
			EndTime:     req.EndTime,
		}
		leads, err := statsRepo.ListForStats(ctx, filter, 0)
		return leads, len(leads), err
	}

	var (
		leads []*types.Lead
		total int
		err   error
	)
	for offset := 0; ; {
		var batch []*types.Lead
		batch, total, err = uc.leadRepo.ListAll(ctx, "", offset, 500)
		if err != nil {
			return nil, 0, err
		}
		leads = append(leads, batch...)
		offset += len(batch)
		if len(batch) == 0 || offset >= total {
			break
		}
	}
	leads = filterLeadsByCreatedAt(leads, req.StartTime, req.EndTime)
	leads = filterLeadsBySales(leads, req.SalesUserID)
	return leads, total, nil
}

func (uc *GetDashboardStatsUseCase) listScopedFollowups(ctx context.Context, leads []*types.Lead) ([]*types.LeadFollowup, error) {
	leadIDs := make([]int64, 0, len(leads))
	leadByID := make(map[int64]*types.Lead, len(leads))
	for _, lead := range leads {
		if lead == nil {
			continue
		}
		leadIDs = append(leadIDs, lead.ID)
		leadByID[lead.ID] = lead
	}
	if statsRepo, ok := uc.followupRepo.(repo.FollowupStatsRepository); ok {
		return statsRepo.ListByLeadIDs(ctx, leadIDs)
	}
	var followups []*types.LeadFollowup
	for offset := 0; ; {
		batch, total, err := uc.followupRepo.ListAll(ctx, offset, 500)
		if err != nil {
			return nil, err
		}
		followups = append(followups, filterFollowupsForLeads(batch, leadByID)...)
		offset += len(batch)
		if len(batch) == 0 || offset >= total {
			break
		}
	}
	return followups, nil
}

func filterLeadsByCreatedAt(leads []*types.Lead, start, end *time.Time) []*types.Lead {
	filtered := make([]*types.Lead, 0, len(leads))
	for _, lead := range leads {
		if start != nil && lead.CreatedAt.Before(*start) {
			continue
		}
		if end != nil && !lead.CreatedAt.Before(*end) {
			continue
		}
		filtered = append(filtered, lead)
	}
	return filtered
}

func filterLeadsBySales(leads []*types.Lead, salesUserID string) []*types.Lead {
	salesUserID = strings.TrimSpace(salesUserID)
	if salesUserID == "" {
		return leads
	}
	filtered := make([]*types.Lead, 0, len(leads))
	for _, lead := range leads {
		if lead == nil {
			continue
		}
		if leadMatchesSalesScope(lead, salesUserID) {
			filtered = append(filtered, lead)
		}
	}
	return filtered
}

func leadMatchesSalesScope(lead *types.Lead, salesUserID string) bool {
	return lead != nil && (lead.OwnerUserID == salesUserID || lead.AssignedSalesUserID == salesUserID ||
		lead.AssignedInviterUserID == salesUserID || lead.InviterUserID == salesUserID ||
		lead.CloserUserID == salesUserID || lead.CurrentOwnerUserID == salesUserID)
}

func leadMatchesManagerScope(lead *types.Lead, managerUserID string) bool {
	if lead == nil {
		return false
	}
	return lead.Status == types.LeadStatusPendingDispatch ||
		lead.DispatchManagerUserID == managerUserID ||
		lead.CreatedByRole == types.UserRoleSales ||
		lead.CreatedByRole == types.UserRoleInviter ||
		lead.CreatedByRole == types.UserRoleCloser
}

func (uc *GetDashboardStatsUseCase) buildDualStageMetrics(ctx context.Context, leads []*types.Lead, startAt, endAt *time.Time) (DualStageMetrics, error) {
	metrics := DualStageMetrics{}
	leadIDs := make(map[int64]struct{}, len(leads))
	appointmentLeadIDs := make(map[int64]struct{})
	arrivedLeadIDs := make(map[int64]struct{})
	now := time.Now()
	for _, lead := range leads {
		if lead == nil {
			continue
		}
		types.ProjectLegacyWorkflow(lead)
		leadIDs[lead.ID] = struct{}{}
		if lead.InviterClaimedAt != nil || lead.ClaimedAt != nil {
			metrics.InvitationClaimedCount++
		}
		if lead.BusinessStage == types.BusinessStageFinished && lead.ResultStatus == types.LeadResultConverted && lead.DeliveredAt != nil {
			metrics.NegotiationConvertedCount++
		}
		if lead.ResultStatus == types.LeadResultActive && lead.NextFollowupDueAt != nil {
			metrics.FollowupDueCount++
			if !lead.NextFollowupDueAt.Before(now) {
				metrics.FollowupOnTimeCount++
			}
		}
	}
	if uc.appointmentRepo != nil {
		appointments, err := uc.appointmentRepo.List(ctx, repo.AppointmentListFilter{})
		if err != nil {
			return metrics, err
		}
		elapsedScheduled := 0
		for _, item := range appointments {
			if item == nil {
				continue
			}
			if _, ok := leadIDs[item.LeadID]; !ok {
				continue
			}
			if item.CloserUserID != "" && item.Status != types.AppointmentCancelled {
				appointmentLeadIDs[item.LeadID] = struct{}{}
			}
			if item.Status == types.AppointmentArrivedDelivered {
				arrivedLeadIDs[item.LeadID] = struct{}{}
			}
			if item.CloserUserID != "" && item.Status != types.AppointmentCancelled &&
				(item.Status == types.AppointmentArrivedDelivered || !item.StartAt.After(now)) {
				elapsedScheduled++
			}
		}
		metrics.AppointmentCount = len(appointmentLeadIDs)
		metrics.ArrivedCount = len(arrivedLeadIDs)
		metrics.ScheduleUtilizationRate = ratio(metrics.ArrivedCount, elapsedScheduled)
	}
	if uc.poolRepo != nil {
		records, err := uc.poolRepo.ListAll(ctx, startAt, endAt, 20000)
		if err != nil {
			return metrics, err
		}
		for _, record := range records {
			if record == nil {
				continue
			}
			if _, ok := leadIDs[record.LeadID]; !ok {
				continue
			}
			switch record.Action {
			case types.PoolActionEnter:
				metrics.PoolEnteredCount++
			case types.PoolActionClaim:
				metrics.PoolClaimedCount++
			}
		}
	}
	metrics.InvitationRate = ratio(metrics.AppointmentCount, metrics.InvitationClaimedCount)
	metrics.ArrivalRate = ratio(metrics.ArrivedCount, metrics.AppointmentCount)
	metrics.NegotiationConversionRate = ratio(metrics.NegotiationConvertedCount, metrics.ArrivedCount)
	metrics.PoolTurnoverRate = ratio(metrics.PoolClaimedCount, metrics.PoolEnteredCount)
	metrics.CurrentFollowupTimelinessRate = ratio(metrics.FollowupOnTimeCount, metrics.FollowupDueCount)
	return metrics, nil
}

func filterFollowupsForLeads(followups []*types.LeadFollowup, leadByID map[int64]*types.Lead) []*types.LeadFollowup {
	filtered := make([]*types.LeadFollowup, 0, len(followups))
	for _, followup := range followups {
		if _, ok := leadByID[followup.LeadID]; ok {
			filtered = append(filtered, followup)
		}
	}
	return filtered
}

func buildAssignmentOverview(leads []*types.Lead, thresholds overdueThresholds) AssignmentOverview {
	now := time.Now()
	overview := AssignmentOverview{Total: len(leads)}
	for _, lead := range leads {
		if lead == nil {
			continue
		}
		if lead.AssignedAt != nil || lead.AssignedSalesUserID != "" || lead.OwnerUserID != "" {
			overview.Assigned++
		}
		if lead.Status == types.LeadStatusPendingDispatch {
			overview.PendingDispatch++
		}
		if lead.Status == types.LeadStatusAssignedPendingClaim {
			overview.PendingClaim++
			if thresholds.Claim != nil && lead.AssignedAt != nil && now.Sub(*lead.AssignedAt) > *thresholds.Claim {
				overview.OverdueClaimCount++
			}
		}
		if lead.Status == types.LeadStatusClaimed || lead.Status == types.LeadStatusFollowing {
			overview.ClaimedOrFollowing++
		}
		if lead.ClaimedAt != nil {
			overview.Claimed++
		}
		if lead.FirstFollowupAt != nil {
			overview.FirstFollowupCount++
		}
		if lead.Status == types.LeadStatusConverted {
			overview.Converted++
		}
		if lead.Status == types.LeadStatusInvalid {
			overview.Invalid++
		}
		if types.IsCurrentFollowupEligible(lead) && lead.NextFollowupDueAt.Before(now) {
			overview.OverdueFollowupCount++
		}
	}
	overview.AssignRate = ratio(overview.Assigned, overview.Total)
	overview.ClaimRate = ratio(overview.Claimed, overview.Assigned)
	overview.FirstFollowupRate = ratio(overview.FirstFollowupCount, overview.Assigned)
	overview.ConversionRate = ratio(overview.Converted, overview.Total)
	return overview
}

func buildDashboardOverview(leads []*types.Lead, thresholds overdueThresholds) DashboardOverview {
	now := time.Now()
	overview := DashboardOverview{TotalLeads: len(leads)}
	for _, lead := range leads {
		switch lead.Status {
		case types.LeadStatusPendingDispatch:
			overview.PendingDispatch++
			if thresholds.Dispatch != nil && now.Sub(lead.CreatedAt) > *thresholds.Dispatch {
				overview.OverdueDispatchCount++
			}
		case types.LeadStatusAssignedPendingClaim:
			overview.PendingClaim++
			if thresholds.Claim != nil && lead.AssignedAt != nil && now.Sub(*lead.AssignedAt) > *thresholds.Claim {
				overview.OverdueClaimCount++
			}
		case types.LeadStatusClaimed:

		case types.LeadStatusFollowing:
			overview.Following++
		case types.LeadStatusConverted:
			overview.Converted++
		case types.LeadStatusInvalid:
			overview.Invalid++
		case types.LeadStatusClosed:
			overview.Closed++
		}
		if types.IsCurrentFollowupEligible(lead) && lead.NextFollowupDueAt.Before(now) {
			overview.OverdueFollowupCount++
		}
		if lead.FirstFollowupAt != nil {
			overview.FirstFollowupCount++
		}
		if lead.ClaimedAt != nil {
			overview.Claimed++
		}
		if lead.DealAmount > 0 {
			overview.TotalDealAmount += lead.DealAmount - lead.RefundAmount
		}
	}
	overview.FirstFollowupRate = ratio(overview.FirstFollowupCount, overview.TotalLeads)
	overview.ClaimRate = ratio(overview.Claimed, overview.TotalLeads)
	overview.ConversionRate = ratio(overview.Converted, overview.TotalLeads)
	overview.InvalidRate = ratio(overview.Invalid, overview.TotalLeads)
	overview.AverageDealAmount = averageAmount(overview.TotalDealAmount, overview.Converted)
	return overview
}

func buildFunnel(leads []*types.Lead, thresholds overdueThresholds) []NameCount {
	overview := buildDashboardOverview(leads, thresholds)
	return []NameCount{
		{Name: "新进线索", Count: overview.TotalLeads},
		{Name: "已分配", Count: countAssigned(leads)},
		{Name: "已领取", Count: overview.Claimed},
		{Name: "已首跟", Count: overview.FirstFollowupCount},
		{Name: "已转化", Count: overview.Converted},
	}
}

func buildSourceStats(leads []*types.Lead) []SourceStats {
	stats := map[string]*SourceStats{}
	for _, lead := range leads {
		source, account := splitSource(lead.SourceChannel)
		key := source + "|" + account
		if stats[key] == nil {
			stats[key] = &SourceStats{Source: source, Account: account}
		}
		item := stats[key]
		item.Total++
		if lead.FirstFollowupAt != nil {
			item.FirstFollowups++
		}
		if lead.Status == types.LeadStatusConverted {
			item.Converted++
		}
		if lead.Status == types.LeadStatusInvalid {
			item.Invalid++
		}
	}
	rows := make([]SourceStats, 0, len(stats))
	for _, item := range stats {
		item.ConversionRate = ratio(item.Converted, item.Total)
		item.InvalidRate = ratio(item.Invalid, item.Total)
		rows = append(rows, *item)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Total > rows[j].Total })
	return rows
}

func buildSalesStats(leads []*types.Lead, followups []*types.LeadFollowup, calc *sla.Calculator, thresholds overdueThresholds) []SalesDashboardStats {
	stats := map[string]*SalesDashboardStats{}
	claimTimes := map[string][]time.Duration{}
	followupTimes := map[string][]time.Duration{}
	now := time.Now()
	for _, lead := range leads {
		salesID := firstNonEmpty(lead.OwnerUserID, lead.AssignedSalesUserID)
		if salesID == "" {
			continue
		}
		item := ensureSalesStats(stats, salesID)
		item.Assigned++
		if lead.Status == types.LeadStatusAssignedPendingClaim {
			item.PendingClaim++
			if thresholds.Claim != nil && lead.AssignedAt != nil && now.Sub(*lead.AssignedAt) > *thresholds.Claim {
				item.OverdueClaimCount++
			}
		}
		if lead.ClaimedAt != nil {
			item.Claimed++
		}
		if lead.Status == types.LeadStatusFollowing {
			item.Following++
		}
		if _, known := types.OwnerResponseDuration(lead); known {
			item.FirstFollowups++
		}
		if lead.Status == types.LeadStatusConverted {
			item.Converted++
		}
		if lead.Status == types.LeadStatusInvalid {
			item.Invalid++
		}
		if lead.AssignedAt != nil && lead.ClaimedAt != nil {
			claimTimes[salesID] = append(claimTimes[salesID], lead.ClaimedAt.Sub(*lead.AssignedAt))
		}
		if duration, known := types.OwnerResponseDuration(lead); known {
			followupTimes[salesID] = append(followupTimes[salesID], duration)
		} else {
			item.UnknownOwnerResponseCount++
		}
		if types.IsCurrentFollowupEligible(lead) && lead.NextFollowupDueAt.Before(now) {
			item.OverdueFollowupCount++
		}
	}
	for _, followup := range followups {
		if followup.SalesUserID == "" {
			continue
		}
		ensureSalesStats(stats, followup.SalesUserID).FollowupRecords++
	}
	rows := make([]SalesDashboardStats, 0, len(stats))
	for salesID, item := range stats {
		item.ClaimRate = ratio(item.Claimed, item.Assigned)
		item.FirstFollowupRate = ratio(item.FirstFollowups, item.Assigned)
		item.ConversionRate = ratio(item.Converted, item.Assigned)
		item.AvgClaimTime = durationStats(calc, claimTimes[salesID])
		item.AvgFirstFollowupTime = durationStats(calc, followupTimes[salesID])
		rows = append(rows, *item)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Assigned > rows[j].Assigned })
	return rows
}

func (uc *GetDashboardStatsUseCase) enrichDisplayNames(ctx context.Context, resp *DashboardStatsResponse) error {
	if resp == nil || uc.userBindingRepo == nil {
		return nil
	}

	ids := make([]string, 0, len(resp.Sales)+len(resp.Managers)+len(resp.NextTasks))
	for _, item := range resp.Sales {
		ids = append(ids, item.SalesUserID)
	}
	for _, item := range resp.Managers {
		ids = append(ids, item.ManagerUserID)
	}
	for _, item := range resp.NextTasks {
		ids = append(ids, item.SalesUserID)
	}

	names, err := loadUserDisplayNames(ctx, uc.userBindingRepo, ids)
	if err != nil {
		return err
	}
	for i := range resp.Sales {
		resp.Sales[i].SalesName = names[resp.Sales[i].SalesUserID]
	}
	for i := range resp.Managers {
		resp.Managers[i].ManagerName = names[resp.Managers[i].ManagerUserID]
	}
	for i := range resp.NextTasks {
		resp.NextTasks[i].SalesName = names[resp.NextTasks[i].SalesUserID]
	}

	return nil
}

func buildManagerStats(leads []*types.Lead, calc *sla.Calculator) []ManagerDashboardStats {
	stats := map[string]*ManagerDashboardStats{}
	dispatchTimes := map[string][]time.Duration{}
	for _, lead := range leads {
		if lead.DispatchManagerUserID == "" {
			continue
		}
		item := stats[lead.DispatchManagerUserID]
		if item == nil {
			item = &ManagerDashboardStats{ManagerUserID: lead.DispatchManagerUserID}
			stats[lead.DispatchManagerUserID] = item
		}
		item.Total++
		if lead.AssignedAt != nil {
			item.Assigned++
			dispatchTimes[lead.DispatchManagerUserID] = append(dispatchTimes[lead.DispatchManagerUserID], lead.AssignedAt.Sub(lead.CreatedAt))
		}
		if lead.Status == types.LeadStatusPendingDispatch {
			item.PendingDispatch++
		}
		if lead.Status == types.LeadStatusConverted {
			item.Converted++
		}
	}
	rows := make([]ManagerDashboardStats, 0, len(stats))
	for managerID, item := range stats {
		item.ConversionRate = ratio(item.Converted, item.Total)
		item.AvgDispatchTime = durationStats(calc, dispatchTimes[managerID])
		rows = append(rows, *item)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Total > rows[j].Total })
	return rows
}

func buildFollowupStats(leads []*types.Lead, followups []*types.LeadFollowup) FollowupQualityStats {
	now := time.Now()
	todayStart := types.BusinessDayStart(now)
	todayEnd := todayStart.Add(24 * time.Hour)
	resultCounts := map[string]int{}
	intentionCounts := map[string]int{}
	invalidCounts := map[string]int{}
	stats := FollowupQualityStats{TotalRecords: len(followups)}
	for _, followup := range followups {
		increment(resultCounts, firstNonEmpty(followup.FollowupResult, "unknown"))
		if followup.IntentionLevel != "" {
			increment(intentionCounts, followup.IntentionLevel)
		}
		if !followup.IsValid && followup.InvalidReason != "" {
			increment(invalidCounts, followup.InvalidReason)
		}
	}
	for _, lead := range leads {
		if !types.IsCurrentFollowupEligible(lead) {
			continue
		}
		due := lead.NextFollowupDueAt
		if due.Before(now) {
			stats.OverdueCount++
		}
		if !due.Before(todayStart) && due.Before(todayEnd) {
			stats.DueTodayCount++
		}
	}

	stats.AvgRecordsPerLead = ratioFloat(len(followups), len(leads))
	stats.ByResult = countsToRows(resultCounts)
	stats.ByIntention = countsToRows(intentionCounts)
	stats.InvalidReasons = countsToRows(invalidCounts)
	return stats
}

func buildPendingFollowupTasks(leads []*types.Lead, followups []*types.LeadFollowup) []PendingFollowupLead {
	now := time.Now()
	end := types.BusinessDayStart(now).AddDate(0, 0, 1)
	var tasks []PendingFollowupLead
	for _, lead := range leads {
		if !types.IsCurrentFollowupEligible(lead) || !lead.NextFollowupDueAt.Before(end) {
			continue
		}
		tasks = append(tasks, PendingFollowupLead{LeadID: lead.ID, LeadNo: lead.LeadNo, CustomerName: lead.CustomerName, ProductName: lead.ProductName, SalesUserID: lead.CurrentOwnerUserID, NextFollowupAt: *lead.NextFollowupDueAt, Overdue: lead.NextFollowupDueAt.Before(now)})
	}

	sort.Slice(tasks, func(i, j int) bool { return tasks[i].NextFollowupAt.Before(tasks[j].NextFollowupAt) })
	if len(tasks) > 20 {
		return tasks[:20]
	}
	return tasks
}

func buildBusinessStats(leads []*types.Lead) BusinessConversionStats {
	var stats BusinessConversionStats
	for _, lead := range leads {
		if lead.Status == types.LeadStatusConverted {
			stats.ConvertedCount++
			stats.TotalDealAmount += lead.DealAmount - lead.RefundAmount
		}
		if lead.Status == types.LeadStatusClosed {
			stats.ClosedCount++
		}
	}
	stats.AverageDealAmount = averageAmount(stats.TotalDealAmount, stats.ConvertedCount)
	return stats
}

func dashboardTimeRange(leads []*types.Lead, start, end *time.Time) TimeRange {
	if len(leads) == 0 {
		now := time.Now()
		if start != nil && end != nil {
			return TimeRange{Start: *start, End: *end}
		}
		return TimeRange{Start: now, End: now}
	}
	tr := TimeRange{Start: leads[0].CreatedAt, End: leads[0].CreatedAt}
	if start != nil {
		tr.Start = *start
	}
	if end != nil {
		tr.End = *end
	}
	for _, lead := range leads {
		if start == nil && lead.CreatedAt.Before(tr.Start) {
			tr.Start = lead.CreatedAt
		}
		if end == nil && lead.CreatedAt.After(tr.End) {
			tr.End = lead.CreatedAt
		}
	}
	return tr
}

func countAssigned(leads []*types.Lead) int {
	count := 0
	for _, lead := range leads {
		if lead.AssignedAt != nil {
			count++
		}
	}
	return count
}

func splitSource(sourceChannel string) (string, string) {
	value := strings.TrimSpace(sourceChannel)
	if value == "" {
		return "未知来源", ""
	}
	if !strings.HasPrefix(value, "ad:") {
		return value, ""
	}
	parts := strings.Split(value, ":")
	sourceNames := map[string]string{"baidu": "百度广告", "tencent": "腾讯广告", "xiaohongshu": "小红书", "douyin": "抖音"}
	source := parts[1]
	if name := sourceNames[source]; name != "" {
		source = name
	}
	if len(parts) > 2 {
		return source, strings.Join(parts[2:], ":")
	}
	return source, ""
}

func ensureSalesStats(stats map[string]*SalesDashboardStats, salesID string) *SalesDashboardStats {
	item := stats[salesID]
	if item == nil {
		item = &SalesDashboardStats{SalesUserID: salesID}
		stats[salesID] = item
	}
	return item
}

func durationStats(calc *sla.Calculator, durations []time.Duration) *sla.DurationStats {
	_ = calc
	if len(durations) == 0 {
		return nil
	}
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var sum time.Duration
	for _, duration := range durations {
		sum += duration
	}
	return &sla.DurationStats{
		Average: sum / time.Duration(len(durations)),
		Min:     sorted[0],
		Max:     sorted[len(sorted)-1],
		P50:     sorted[len(sorted)*50/100],
		P90:     sorted[minIndex(len(sorted), 90)],
		P95:     sorted[minIndex(len(sorted), 95)],
		Count:   len(sorted),
	}
}

func minIndex(length, percentile int) int {
	index := length * percentile / 100
	if index >= length {
		return length - 1
	}
	return index
}

func increment(counts map[string]int, key string) {
	counts[key]++
}

func countsToRows(counts map[string]int) []NameCount {
	rows := make([]NameCount, 0, len(counts))
	for name, count := range counts {
		rows = append(rows, NameCount{Name: name, Count: count})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Count > rows[j].Count })
	return rows
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func ratio(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total)
}

func ratioFloat(part, total int) float64 {
	return ratio(part, total)
}

func averageAmount(total float64, count int) float64 {
	if count == 0 {
		return 0
	}
	return total / float64(count)
}
