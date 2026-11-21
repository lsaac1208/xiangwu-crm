package usecase

import (
	"context"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/domain/sla"
	"github.com/lsaac128/kxone/pkg/types"
)

// GetSLAStatsUseCase 获取 SLA 统计用例
type GetSLAStatsUseCase struct {
	leadRepo repo.LeadRepository
	slaCalc  *sla.Calculator
}

func NewGetSLAStatsUseCase(
	leadRepo repo.LeadRepository,
) *GetSLAStatsUseCase {
	return &GetSLAStatsUseCase{
		leadRepo: leadRepo,
		slaCalc:  sla.NewCalculator(),
	}
}

// GetSLAStatsRequest 获取 SLA 统计请求
type GetSLAStatsRequest struct {
	CurrentUserID string
	Role          string
	Status        string     // 可选：按状态筛选
	ManagerUserID string     // 可选：按主管筛选
	SalesUserID   string     // 可选：按销售筛选
	StartTime     *time.Time // 可选：开始时间
	EndTime       *time.Time // 可选：结束时间
	Limit         int        // 可选：限制数量（默认 1000）
}

// GetSLAStatsResponse 获取 SLA 统计响应
type GetSLAStatsResponse struct {
	Stats      *sla.AggregatedSLA `json:"stats"`
	LeadSLAs   []*sla.LeadSLA     `json:"lead_slas,omitempty"`
	TimeRange  TimeRange          `json:"time_range"`
	TotalCount int                `json:"total_count"`
}

// TimeRange 时间范围
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// Execute 执行 SLA 统计
func (uc *GetSLAStatsUseCase) Execute(ctx context.Context, req *GetSLAStatsRequest) (*GetSLAStatsResponse, error) {
	// 设置默认值
	if req.Limit <= 0 || req.Limit > 10000 {
		req.Limit = 1000
	}

	filteredLeads, err := uc.listLeads(ctx, req)
	if err != nil {
		return nil, err
	}

	// 计算 SLA 指标
	aggregatedStats := uc.slaCalc.AggregateSLA(filteredLeads)

	// 计算每个线索的 SLA
	leadSLAs := make([]*sla.LeadSLA, len(filteredLeads))
	for i, lead := range filteredLeads {
		leadSLAs[i] = uc.slaCalc.CalculateLeadSLA(lead)
	}

	// 计算时间范围
	timeRange := uc.calculateTimeRange(filteredLeads, req)

	return &GetSLAStatsResponse{
		Stats:      aggregatedStats,
		LeadSLAs:   leadSLAs,
		TimeRange:  timeRange,
		TotalCount: len(filteredLeads),
	}, nil
}

func (uc *GetSLAStatsUseCase) listLeads(ctx context.Context, req *GetSLAStatsRequest) ([]*types.Lead, error) {
	if statsRepo, ok := uc.leadRepo.(repo.LeadStatsRepository); ok {
		return statsRepo.ListForStats(ctx, repo.LeadStatsFilter{
			Status:        req.Status,
			ManagerUserID: req.ManagerUserID,
			SalesUserID:   req.SalesUserID,
			StartTime:     req.StartTime,
			EndTime:       req.EndTime,
		}, req.Limit)
	}
	leads, _, err := uc.leadRepo.ListAll(ctx, req.Status, 0, req.Limit)
	if err != nil {
		return nil, err
	}
	return uc.filterLeads(leads, req), nil
}

// filterLeads 过滤线索
func (uc *GetSLAStatsUseCase) filterLeads(leads []*types.Lead, req *GetSLAStatsRequest) []*types.Lead {
	filtered := make([]*types.Lead, 0, len(leads))

	for _, lead := range leads {
		// 按主管筛选
		if req.ManagerUserID != "" && !leadMatchesManagerScope(lead, req.ManagerUserID) {
			continue
		}

		// 按销售筛选
		if req.SalesUserID != "" && !leadMatchesSalesScope(lead, req.SalesUserID) {
			continue
		}

		// 按时间范围筛选
		if req.StartTime != nil && lead.CreatedAt.Before(*req.StartTime) {
			continue
		}
		if req.EndTime != nil && !lead.CreatedAt.Before(*req.EndTime) {
			continue
		}

		filtered = append(filtered, lead)
	}

	return filtered
}

// calculateTimeRange 计算时间范围
func (uc *GetSLAStatsUseCase) calculateTimeRange(leads []*types.Lead, req *GetSLAStatsRequest) TimeRange {
	if len(leads) == 0 {
		now := time.Now()
		return TimeRange{Start: now, End: now}
	}

	// 使用请求指定的时间范围，或从线索数据推断
	start := leads[0].CreatedAt
	end := leads[0].CreatedAt

	if req.StartTime != nil {
		start = *req.StartTime
	}
	if req.EndTime != nil {
		end = *req.EndTime
	}

	// 从线索数据推断时间范围
	for _, lead := range leads {
		if req.StartTime == nil && lead.CreatedAt.Before(start) {
			start = lead.CreatedAt
		}
		if req.EndTime == nil && lead.CreatedAt.After(end) {
			end = lead.CreatedAt
		}
	}

	return TimeRange{Start: start, End: end}
}
