package sla

import (
	"math"
	"sort"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

// Calculator SLA 计算器（纯函数）
type Calculator struct{}

// NewCalculator 创建 SLA 计算器
func NewCalculator() *Calculator {
	return &Calculator{}
}

// LeadSLA 单个线索的 SLA 指标
type LeadSLA struct {
	LeadID                int64            `json:"lead_id"`
	LeadNo                string           `json:"lead_no"`
	DispatchResponseTime  *time.Duration   `json:"dispatch_response_time,omitempty"` // 派发响应时长
	ClaimResponseTime     *time.Duration   `json:"claim_response_time,omitempty"`    // 领取响应时长
	FirstFollowupTime     *time.Duration   `json:"first_followup_time,omitempty"`    // 首跟响应时长
	TotalResponseTime     *time.Duration   `json:"total_response_time,omitempty"`    // 总响应时长（创建到首跟）
	DispatchManagerUserID string           `json:"dispatch_manager_userid,omitempty"`
	SalesUserID           string           `json:"sales_userid,omitempty"`
	Status                types.LeadStatus `json:"status"`
	CreatedAt             time.Time        `json:"created_at"`
}

// CalculateLeadSLA 计算单个线索的 SLA 指标
func (c *Calculator) CalculateLeadSLA(lead *types.Lead) *LeadSLA {
	sla := &LeadSLA{
		LeadID:                lead.ID,
		LeadNo:                lead.LeadNo,
		DispatchManagerUserID: lead.DispatchManagerUserID,
		SalesUserID:           lead.OwnerUserID,
		Status:                lead.Status,
		CreatedAt:             lead.CreatedAt,
	}

	// 派发响应时长：创建 → 分配
	if lead.AssignedAt != nil && !lead.AssignedAt.Before(lead.CreatedAt) {
		duration := lead.AssignedAt.Sub(lead.CreatedAt)
		sla.DispatchResponseTime = &duration
	}

	// 领取响应时长：分配 → 领取
	if lead.AssignedAt != nil && lead.ClaimedAt != nil && !lead.ClaimedAt.Before(*lead.AssignedAt) {
		duration := lead.ClaimedAt.Sub(*lead.AssignedAt)
		sla.ClaimResponseTime = &duration
	}

	// 首跟响应时长：领取 → 首次跟进
	if duration, known := types.OwnerResponseDuration(lead); known {
		sla.FirstFollowupTime = &duration
	}

	// 总响应时长：创建 → 首次跟进
	if lead.FirstFollowupAt != nil && !lead.FirstFollowupAt.Before(lead.CreatedAt) {
		duration := lead.FirstFollowupAt.Sub(lead.CreatedAt)
		sla.TotalResponseTime = &duration
	}

	return sla
}

// AggregatedSLA 聚合 SLA 统计
type AggregatedSLA struct {
	UnknownOwnerResponseCount int                    `json:"unknown_owner_response_count"`
	TotalLeads                int                    `json:"total_leads"`
	CompletedLeads            int                    `json:"completed_leads"` // 已完成首跟的线索数
	AvgDispatchTime           *DurationStats         `json:"avg_dispatch_time,omitempty"`
	AvgClaimTime              *DurationStats         `json:"avg_claim_time,omitempty"`
	AvgFirstFollowupTime      *DurationStats         `json:"avg_first_followup_time,omitempty"`
	AvgTotalResponseTime      *DurationStats         `json:"avg_total_response_time,omitempty"`
	ByStatus                  map[string]int         `json:"by_status"`
	ByManager                 map[string]*ManagerSLA `json:"by_manager,omitempty"`
	BySales                   map[string]*SalesSLA   `json:"by_sales,omitempty"`
}

// DurationStats 时长统计
type DurationStats struct {
	Average time.Duration `json:"average"`
	Min     time.Duration `json:"min"`
	Max     time.Duration `json:"max"`
	P50     time.Duration `json:"p50"` // 中位数
	P90     time.Duration `json:"p90"`
	P95     time.Duration `json:"p95"`
	Count   int           `json:"count"`
}

// ManagerSLA 主管 SLA 统计
type ManagerSLA struct {
	ManagerUserID   string         `json:"manager_userid"`
	TotalAssigned   int            `json:"total_assigned"`
	AvgDispatchTime *DurationStats `json:"avg_dispatch_time,omitempty"`
}

// SalesSLA 销售 SLA 统计
type SalesSLA struct {
	SalesUserID          string         `json:"sales_userid"`
	TotalClaimed         int            `json:"total_claimed"`
	TotalFollowedUp      int            `json:"total_followed_up"`
	AvgClaimTime         *DurationStats `json:"avg_claim_time,omitempty"`
	AvgFirstFollowupTime *DurationStats `json:"avg_first_followup_time,omitempty"`
}

// AggregateSLA 聚合多个线索的 SLA 统计
func (c *Calculator) AggregateSLA(leads []*types.Lead) *AggregatedSLA {
	agg := &AggregatedSLA{
		TotalLeads: len(leads),
		ByStatus:   make(map[string]int),
		ByManager:  make(map[string]*ManagerSLA),
		BySales:    make(map[string]*SalesSLA),
	}

	if len(leads) == 0 {
		return agg
	}

	var dispatchTimes, claimTimes, followupTimes, totalTimes []time.Duration
	managerDispatches := make(map[string][]time.Duration)
	salesClaims := make(map[string][]time.Duration)
	salesFollowups := make(map[string][]time.Duration)
	salesFollowupCounts := make(map[string]int)

	for _, lead := range leads {
		// 状态统计
		agg.ByStatus[string(lead.Status)]++
		if _, known := types.OwnerResponseDuration(lead); !known {
			agg.UnknownOwnerResponseCount++
		}

		// 派发时长
		if lead.AssignedAt != nil && !lead.AssignedAt.Before(lead.CreatedAt) {
			duration := lead.AssignedAt.Sub(lead.CreatedAt)
			dispatchTimes = append(dispatchTimes, duration)

			// 主管维度统计
			if lead.DispatchManagerUserID != "" {
				managerDispatches[lead.DispatchManagerUserID] = append(
					managerDispatches[lead.DispatchManagerUserID], duration)
			}
		}

		// 领取时长
		if lead.AssignedAt != nil && lead.ClaimedAt != nil && !lead.ClaimedAt.Before(*lead.AssignedAt) {
			duration := lead.ClaimedAt.Sub(*lead.AssignedAt)
			claimTimes = append(claimTimes, duration)

			// 销售维度统计
			if lead.OwnerUserID != "" {
				salesClaims[lead.OwnerUserID] = append(salesClaims[lead.OwnerUserID], duration)
			}
		}

		// 首跟时长
		if duration, known := types.OwnerResponseDuration(lead); known {
			followupTimes = append(followupTimes, duration)

			// 销售维度统计
			if lead.OwnerUserID != "" {
				salesFollowups[lead.OwnerUserID] = append(salesFollowups[lead.OwnerUserID], duration)
				salesFollowupCounts[lead.OwnerUserID]++
			}

			agg.CompletedLeads++
		}

		// 总响应时长
		if lead.FirstFollowupAt != nil && !lead.FirstFollowupAt.Before(lead.CreatedAt) {
			duration := lead.FirstFollowupAt.Sub(lead.CreatedAt)
			totalTimes = append(totalTimes, duration)
		}
	}

	// 计算全局统计
	agg.AvgDispatchTime = c.calculateDurationStats(dispatchTimes)
	agg.AvgClaimTime = c.calculateDurationStats(claimTimes)
	agg.AvgFirstFollowupTime = c.calculateDurationStats(followupTimes)
	agg.AvgTotalResponseTime = c.calculateDurationStats(totalTimes)

	// 计算主管维度统计
	for managerID, durations := range managerDispatches {
		agg.ByManager[managerID] = &ManagerSLA{
			ManagerUserID:   managerID,
			TotalAssigned:   len(durations),
			AvgDispatchTime: c.calculateDurationStats(durations),
		}
	}

	// 计算销售维度统计
	for salesID := range salesClaims {
		agg.BySales[salesID] = &SalesSLA{
			SalesUserID:          salesID,
			TotalClaimed:         len(salesClaims[salesID]),
			TotalFollowedUp:      salesFollowupCounts[salesID],
			AvgClaimTime:         c.calculateDurationStats(salesClaims[salesID]),
			AvgFirstFollowupTime: c.calculateDurationStats(salesFollowups[salesID]),
		}
	}

	return agg
}

// calculateDurationStats 计算时长统计指标
func (c *Calculator) calculateDurationStats(durations []time.Duration) *DurationStats {
	if len(durations) == 0 {
		return nil
	}

	// 排序以计算百分位数
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	stats := &DurationStats{
		Min:   sorted[0],
		Max:   sorted[len(sorted)-1],
		Count: len(sorted),
	}

	// 计算平均值
	var sum time.Duration
	for _, d := range durations {
		sum += d
	}
	stats.Average = sum / time.Duration(len(durations))

	// 计算百分位数
	stats.P50 = percentile(sorted, 50)
	stats.P90 = percentile(sorted, 90)
	stats.P95 = percentile(sorted, 95)

	return stats
}

func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	rank := int(math.Ceil(float64(p)/100.0*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}
