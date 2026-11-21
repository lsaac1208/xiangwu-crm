package usecase

import (
	"context"
	"log"
	"sort"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/domain/recommendation"
	"github.com/lsaac128/kxone/pkg/types"
)

// RecommendSalesUseCase 推荐销售用例
type RecommendSalesUseCase struct {
	leadRepo                  repo.LeadRepository
	followupRepo              repo.FollowupRepository
	userBindingRepo           repo.UserBindingRepository
	engine                    *recommendation.Engine
	maxLeadsForRecommendation int
}

func NewRecommendSalesUseCase(
	leadRepo repo.LeadRepository,
	followupRepo repo.FollowupRepository,
) *RecommendSalesUseCase {
	return &RecommendSalesUseCase{
		leadRepo:                  leadRepo,
		followupRepo:              followupRepo,
		engine:                    recommendation.NewEngine(),
		maxLeadsForRecommendation: 5000,
	}
}

func (uc *RecommendSalesUseCase) WithUserBindingRepository(userBindingRepo repo.UserBindingRepository) *RecommendSalesUseCase {
	uc.userBindingRepo = userBindingRepo
	return uc
}

func (uc *RecommendSalesUseCase) WithMaxLeads(limit int) *RecommendSalesUseCase {
	if limit > 0 {
		uc.maxLeadsForRecommendation = limit
	}
	return uc
}

// RecommendSalesRequest 推荐销售请求
type RecommendSalesRequest struct {
	LeadID         int64                        `json:"lead_id,omitempty"`         // 可选：为特定线索推荐
	LeadCity       string                       `json:"lead_city,omitempty"`       // 可选：直接指定城市
	LeadProduct    string                       `json:"lead_product,omitempty"`    // 可选：直接指定产品
	CandidateUsers []string                     `json:"candidate_users,omitempty"` // 可选：候选销售列表
	TopN           int                          `json:"top_n,omitempty"`           // 返回前 N 个推荐
	Weights        *recommendation.ScoreWeights `json:"weights,omitempty"`         // 可选：自定义权重
}

// RecommendSalesResponse 推荐销售响应
type RecommendSalesResponse struct {
	Lead            *types.Lead                           `json:"lead,omitempty"`
	Recommendations []*recommendation.RecommendationScore `json:"recommendations"`
	ProfilesUsed    int                                   `json:"profiles_used"`
}

// Execute 执行销售推荐
func (uc *RecommendSalesUseCase) Execute(ctx context.Context, req *RecommendSalesRequest) (*RecommendSalesResponse, error) {
	// 1. 获取线索信息
	var lead *types.Lead
	var err error

	if req.LeadID > 0 {
		lead, err = uc.leadRepo.GetByID(ctx, req.LeadID)
		if err != nil {
			return nil, err
		}
		if lead == nil {
			return nil, types.ErrLeadNotFound
		}
	} else {
		// 构造虚拟线索用于推荐
		lead = &types.Lead{
			City:        req.LeadCity,
			ProductName: req.LeadProduct,
		}
	}

	// 2. 构建候选销售画像
	profiles, err := uc.buildSalesProfiles(ctx, req.CandidateUsers)
	if err != nil {
		return nil, err
	}

	if len(profiles) == 0 {
		return &RecommendSalesResponse{
			Lead:            lead,
			Recommendations: []*recommendation.RecommendationScore{},
			ProfilesUsed:    0,
		}, nil
	}

	// 3. 执行推荐
	scores := uc.engine.Recommend(&recommendation.RecommendationRequest{
		Lead:          lead,
		SalesProfiles: profiles,
		Weights:       req.Weights,
	})

	// 4. 截取前 N 个
	topN := req.TopN
	if topN <= 0 || topN > len(scores) {
		topN = len(scores)
	}

	return &RecommendSalesResponse{
		Lead:            lead,
		Recommendations: scores[:topN],
		ProfilesUsed:    len(profiles),
	}, nil
}

// buildSalesProfiles 构建销售画像
func (uc *RecommendSalesUseCase) buildSalesProfiles(ctx context.Context, candidateUsers []string) ([]*recommendation.SalesProfile, error) {
	limit := uc.maxLeadsForRecommendation
	eligibleInviters, err := uc.loadEligibleInviters(ctx)
	if err != nil {
		return nil, err
	}

	allLeads, total, err := uc.leadRepo.ListAll(ctx, "", 0, limit)
	if err != nil {
		return nil, err
	}
	// 如果实际总量超过上限，当前批次数据不完整，记录告警便于监控
	if total > limit {
		log.Printf("[recommend_sales] WARNING: total leads=%d exceeds fetch limit=%d, recommendation profiles may be incomplete", total, limit)
	}

	// 按销售聚合统计
	salesStats := make(map[string]*salesAggregation)

	for _, lead := range allLeads {
		salesUserID := lead.OwnerUserID
		if salesUserID == "" {
			continue
		}
		if eligibleInviters != nil {
			if _, ok := eligibleInviters[salesUserID]; !ok {
				continue
			}
		}

		// 如果指定了候选列表，跳过不在列表中的销售
		if len(candidateUsers) > 0 && !contains(candidateUsers, salesUserID) {
			continue
		}

		if _, exists := salesStats[salesUserID]; !exists {
			salesStats[salesUserID] = &salesAggregation{
				UserID:             salesUserID,
				Cities:             make(map[string]int),
				Products:           make(map[string]int),
				TotalClaimed:       0,
				TotalConverted:     0,
				CurrentLeads:       0,
				ClaimTimes:         []float64{},
				FirstFollowupTimes: []float64{},
			}
		}

		stats := salesStats[salesUserID]
		stats.TotalClaimed++

		// 统计城市和产品
		if lead.City != "" {
			stats.Cities[lead.City]++
		}
		if lead.ProductName != "" {
			stats.Products[lead.ProductName]++
		}

		// 统计转化
		if lead.Status == types.LeadStatusConverted {
			stats.TotalConverted++
		}

		// 统计当前待处理线索
		if lead.Status == types.LeadStatusClaimed || lead.Status == types.LeadStatusFollowing {
			stats.CurrentLeads++
		}

		// 统计响应时长
		if lead.AssignedAt != nil && lead.ClaimedAt != nil && !lead.ClaimedAt.Before(*lead.AssignedAt) {
			claimDuration := lead.ClaimedAt.Sub(*lead.AssignedAt).Seconds()
			stats.ClaimTimes = append(stats.ClaimTimes, claimDuration)
		}

		if duration, known := types.OwnerResponseDuration(lead); known {
			followupDuration := duration.Seconds()
			stats.FirstFollowupTimes = append(stats.FirstFollowupTimes, followupDuration)
		}
	}

	// 转换为推荐引擎需要的画像格式
	profiles := make([]*recommendation.SalesProfile, 0, len(salesStats))
	userIDs := make([]string, 0, len(salesStats))
	for salesUserID := range salesStats {
		userIDs = append(userIDs, salesUserID)
	}
	names, err := loadUserDisplayNames(ctx, uc.userBindingRepo, userIDs)
	if err != nil {
		return nil, err
	}
	for _, stats := range salesStats {
		profile := &recommendation.SalesProfile{
			UserID:               stats.UserID,
			Name:                 displayNameOrID(names, stats.UserID),
			PreferredCities:      getTopKeys(stats.Cities, 3),
			PreferredProducts:    getTopKeys(stats.Products, 3),
			CurrentLeadsCount:    stats.CurrentLeads,
			AvgClaimTime:         average(stats.ClaimTimes),
			AvgFirstFollowup:     average(stats.FirstFollowupTimes),
			FirstFollowupSamples: len(stats.FirstFollowupTimes),
			TotalClaimed:         stats.TotalClaimed,
			TotalConverted:       stats.TotalConverted,
		}

		// 计算转化率
		if profile.TotalClaimed > 0 {
			profile.ConversionRate = float64(profile.TotalConverted) / float64(profile.TotalClaimed)
		}

		profiles = append(profiles, profile)
	}

	return profiles, nil
}

func (uc *RecommendSalesUseCase) loadEligibleInviters(ctx context.Context) (map[string]struct{}, error) {
	if uc.userBindingRepo == nil {
		return nil, nil
	}
	result := make(map[string]struct{})
	for _, role := range []types.UserRole{types.UserRoleInviter, types.UserRoleSales} {
		bindings, _, err := uc.userBindingRepo.List(ctx, string(types.UserStatusActive), string(role), 0, 1000)
		if err != nil {
			return nil, err
		}
		for _, binding := range bindings {
			result[binding.WeComUserID] = struct{}{}
		}
	}
	return result, nil
}

// salesAggregation 销售聚合统计
type salesAggregation struct {
	UserID             string
	Cities             map[string]int
	Products           map[string]int
	TotalClaimed       int
	TotalConverted     int
	CurrentLeads       int
	ClaimTimes         []float64
	FirstFollowupTimes []float64
}

// getTopKeys 获取 map 中出现次数最多的前 N 个 key
func getTopKeys(m map[string]int, n int) []string {
	type kv struct {
		key   string
		value int
	}

	// 转换为 slice
	pairs := make([]kv, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, kv{k, v})
	}

	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].value == pairs[j].value {
			return pairs[i].key < pairs[j].key
		}
		return pairs[i].value > pairs[j].value
	})

	// 取前 N 个
	result := make([]string, 0, n)
	for i := 0; i < len(pairs) && i < n; i++ {
		result = append(result, pairs[i].key)
	}

	return result
}

// average 计算平均值
func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}

	return sum / float64(len(values))
}

// contains 检查字符串是否在切片中
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
