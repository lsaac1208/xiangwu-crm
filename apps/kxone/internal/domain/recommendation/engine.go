package recommendation

import (
	"sort"

	"github.com/lsaac128/kxone/pkg/types"
)

// Engine 推荐引擎（纯函数）
type Engine struct{}

// NewEngine 创建推荐引擎
func NewEngine() *Engine {
	return &Engine{}
}

// SalesProfile 销售画像
type SalesProfile struct {
	FirstFollowupSamples int      `json:"first_followup_samples"`
	UserID               string   `json:"user_id"`
	Name                 string   `json:"name"`
	PreferredCities      []string `json:"preferred_cities,omitempty"`   // 擅长城市
	PreferredProducts    []string `json:"preferred_products,omitempty"` // 擅长产品
	CurrentLeadsCount    int      `json:"current_leads_count"`          // 当前待处理线索数
	AvgClaimTime         float64  `json:"avg_claim_time"`               // 平均领取时长（秒）
	AvgFirstFollowup     float64  `json:"avg_first_followup"`           // 平均首跟时长（秒）
	ConversionRate       float64  `json:"conversion_rate"`              // 转化率
	TotalClaimed         int      `json:"total_claimed"`                // 累计领取数
	TotalConverted       int      `json:"total_converted"`              // 累计转化数
}

// RecommendationScore 推荐评分
type RecommendationScore struct {
	SalesUserID       string  `json:"sales_userid"`
	SalesName         string  `json:"sales_name,omitempty"`
	TotalScore        float64 `json:"total_score"`         // 总分（0-100）
	CityMatchScore    float64 `json:"city_match_score"`    // 城市匹配分
	ProductMatchScore float64 `json:"product_match_score"` // 产品匹配分
	LoadScore         float64 `json:"load_score"`          // 负载均衡分
	PerformanceScore  float64 `json:"performance_score"`   // 绩效分
	Reason            string  `json:"reason,omitempty"`    // 推荐理由
}

// RecommendationRequest 推荐请求
type RecommendationRequest struct {
	Lead          *types.Lead     `json:"lead"`
	SalesProfiles []*SalesProfile `json:"sales_profiles"`
	Weights       *ScoreWeights   `json:"weights,omitempty"`
}

// ScoreWeights 评分权重
type ScoreWeights struct {
	City        float64 `json:"city"`        // 城市匹配权重
	Product     float64 `json:"product"`     // 产品匹配权重
	Load        float64 `json:"load"`        // 负载均衡权重
	Performance float64 `json:"performance"` // 绩效权重
}

// DefaultWeights 默认权重
func DefaultWeights() *ScoreWeights {
	return &ScoreWeights{
		City:        0.3, // 30%
		Product:     0.2, // 20%
		Load:        0.3, // 30%
		Performance: 0.2, // 20%
	}
}

// Recommend 推荐销售
func (e *Engine) Recommend(req *RecommendationRequest) []*RecommendationScore {
	if req == nil || req.Lead == nil || len(req.SalesProfiles) == 0 {
		return []*RecommendationScore{}
	}
	if req.Weights == nil {
		req.Weights = DefaultWeights()
	}

	scores := make([]*RecommendationScore, 0, len(req.SalesProfiles))

	for _, profile := range req.SalesProfiles {
		if profile == nil || profile.UserID == "" {
			continue
		}
		score := e.calculateScore(req.Lead, profile, req.Weights)
		score.Reason = e.generateReason(score, profile)
		scores = append(scores, score)
	}

	// 按总分降序排序
	sortByScore(scores)

	return scores
}

// calculateScore 计算单个销售的推荐分数
func (e *Engine) calculateScore(lead *types.Lead, profile *SalesProfile, weights *ScoreWeights) *RecommendationScore {
	score := &RecommendationScore{
		SalesUserID: profile.UserID,
		SalesName:   profile.Name,
	}

	// 1. 城市匹配分（0-100）
	score.CityMatchScore = e.calculateCityMatch(lead.City, profile.PreferredCities)

	// 2. 产品匹配分（0-100）
	score.ProductMatchScore = e.calculateProductMatch(lead.ProductName, profile.PreferredProducts)

	// 3. 负载均衡分（0-100）
	score.LoadScore = e.calculateLoadScore(profile.CurrentLeadsCount)

	// 4. 绩效分（0-100）
	score.PerformanceScore = e.calculatePerformanceScore(profile)

	// 加权总分
	score.TotalScore = score.CityMatchScore*weights.City +
		score.ProductMatchScore*weights.Product +
		score.LoadScore*weights.Load +
		score.PerformanceScore*weights.Performance

	return score
}

// calculateCityMatch 计算城市匹配分
func (e *Engine) calculateCityMatch(leadCity string, preferredCities []string) float64 {
	if leadCity == "" {
		return 50.0 // 中性分
	}

	// 精确匹配
	for _, city := range preferredCities {
		if city == leadCity {
			return 100.0
		}
	}

	// 无匹配记录但不应该得 0 分（可能是新销售）
	if len(preferredCities) == 0 {
		return 50.0
	}

	// 不在擅长城市列表中
	return 20.0
}

// calculateProductMatch 计算产品匹配分
func (e *Engine) calculateProductMatch(leadProduct string, preferredProducts []string) float64 {
	if leadProduct == "" {
		return 50.0
	}

	// 精确匹配
	for _, product := range preferredProducts {
		if product == leadProduct {
			return 100.0
		}
	}

	// 无匹配记录
	if len(preferredProducts) == 0 {
		return 50.0
	}

	// 不在擅长产品列表中
	return 20.0
}

// calculateLoadScore 计算负载均衡分
func (e *Engine) calculateLoadScore(currentLeads int) float64 {
	// 负载越低，分数越高
	// 0 个线索 = 100 分
	// 5 个线索 = 50 分
	// 10+ 个线索 = 10 分

	if currentLeads == 0 {
		return 100.0
	}
	if currentLeads >= 10 {
		return 10.0
	}

	// 线性递减
	return 100.0 - float64(currentLeads)*9.0
}

// calculatePerformanceScore 计算绩效分
func (e *Engine) calculatePerformanceScore(profile *SalesProfile) float64 {
	// 综合考虑转化率、响应速度、经验值

	// 1. 转化率分（0-40）
	conversionScore := profile.ConversionRate * 40.0 // 假设转化率 0-1

	// 2. 响应速度分（0-30）
	// 平均领取时长：< 5 分钟 = 30 分，> 30 分钟 = 0 分
	// Unknown response times receive a neutral score, never the fastest score.
	speedScore := 15.0
	if profile.FirstFollowupSamples > 0 {
		minutes := profile.AvgFirstFollowup / 60.0
		speedScore = 0
		if minutes >= 0 && minutes < 5 {
			speedScore = 30
		} else if minutes >= 5 && minutes < 30 {
			speedScore = 30 - (minutes-5)*1.2
		}
	}

	// 3. 经验分（0-30）
	// 累计领取数越多，经验越丰富
	experienceScore := 0.0
	if profile.TotalClaimed >= 100 {
		experienceScore = 30.0
	} else if profile.TotalClaimed >= 50 {
		experienceScore = 20.0
	} else if profile.TotalClaimed >= 20 {
		experienceScore = 15.0
	} else if profile.TotalClaimed >= 10 {
		experienceScore = 10.0
	} else if profile.TotalClaimed > 0 {
		experienceScore = 5.0
	}

	return conversionScore + speedScore + experienceScore
}

// generateReason 生成推荐理由
func (e *Engine) generateReason(score *RecommendationScore, profile *SalesProfile) string {
	reasons := []string{}

	if score.CityMatchScore >= 80 {
		reasons = append(reasons, "擅长该城市")
	}
	if score.ProductMatchScore >= 80 {
		reasons = append(reasons, "擅长该产品")
	}
	if score.LoadScore >= 70 {
		reasons = append(reasons, "当前负载较低")
	}
	if profile.ConversionRate >= 0.3 {
		reasons = append(reasons, "转化率高")
	}
	if profile.AvgClaimTime > 0 && profile.AvgClaimTime < 300 {
		reasons = append(reasons, "响应迅速")
	}

	if len(reasons) == 0 {
		return "综合评分匹配"
	}

	result := ""
	for i, r := range reasons {
		if i > 0 {
			result += "、"
		}
		result += r
	}
	return result
}

// sortByScore 按总分降序排序。
func sortByScore(scores []*RecommendationScore) {
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].TotalScore > scores[j].TotalScore
	})
}
