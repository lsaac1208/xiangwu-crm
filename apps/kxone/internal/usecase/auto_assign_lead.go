package usecase

import (
	"context"
	"errors"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/domain/recommendation"
)

// AutoAssignLeadUseCase 自动分配线索用例
type AutoAssignLeadUseCase struct {
	leadRepo              repo.LeadRepository
	followupRepo          repo.FollowupRepository
	recommendSalesUseCase *RecommendSalesUseCase
	assignLeadUseCase     *AssignLeadUseCase
}

func NewAutoAssignLeadUseCase(
	leadRepo repo.LeadRepository,
	followupRepo repo.FollowupRepository,
	recommendSalesUseCase *RecommendSalesUseCase,
	assignLeadUseCase *AssignLeadUseCase,
) *AutoAssignLeadUseCase {
	return &AutoAssignLeadUseCase{
		leadRepo:              leadRepo,
		followupRepo:          followupRepo,
		recommendSalesUseCase: recommendSalesUseCase,
		assignLeadUseCase:     assignLeadUseCase,
	}
}

// AutoAssignLeadRequest 自动分配线索请求
type AutoAssignLeadRequest struct {
	LeadID         int64    `json:"lead_id"`
	ManagerUserID  string   `json:"manager_userid"`            // 执行分配的主管
	ActorRole      string   `json:"actor_role,omitempty"`      // 当前操作者角色
	CandidateUsers []string `json:"candidate_users,omitempty"` // 可选：候选销售列表
	TopN           int      `json:"top_n,omitempty"`           // 考虑前 N 个推荐
	Remark         string   `json:"remark,omitempty"`
}

// AutoAssignLeadResponse 自动分配线索响应
type AutoAssignLeadResponse struct {
	AssignedSalesUserID string  `json:"assigned_sales_userid"`
	AssignedSalesName   string  `json:"assigned_sales_name,omitempty"`
	Score               float64 `json:"score"`
	Reason              string  `json:"reason"`
}

// Execute 执行自动分配
func (uc *AutoAssignLeadUseCase) Execute(ctx context.Context, req *AutoAssignLeadRequest) (*AutoAssignLeadResponse, error) {
	// 1. 获取推荐列表
	topN := req.TopN
	if topN <= 0 {
		topN = 3 // 默认考虑前 3 个推荐
	}

	recommendations, err := uc.recommendSalesUseCase.Execute(ctx, &RecommendSalesRequest{
		LeadID:         req.LeadID,
		CandidateUsers: req.CandidateUsers,
		TopN:           topN,
	})

	if err != nil {
		return nil, err
	}

	// 2. 如果没有推荐结果，返回错误
	if len(recommendations.Recommendations) == 0 {
		return nil, ErrNoSalesAvailable
	}

	// 3. 选择得分最高的销售
	topRecommendation := recommendations.Recommendations[0]

	// 4. 执行分配
	err = uc.assignLeadUseCase.Execute(ctx, &AssignLeadRequest{
		LeadID:        req.LeadID,
		ManagerUserID: req.ManagerUserID,
		ActorRole:     req.ActorRole,
		SalesUserID:   topRecommendation.SalesUserID,
		Remark:        buildAutoAssignRemark(req.Remark, topRecommendation),
	})

	if err != nil {
		return nil, err
	}

	return &AutoAssignLeadResponse{
		AssignedSalesUserID: topRecommendation.SalesUserID,
		AssignedSalesName:   topRecommendation.SalesName,
		Score:               topRecommendation.TotalScore,
		Reason:              topRecommendation.Reason,
	}, nil
}

// buildAutoAssignRemark 构建自动分配备注
func buildAutoAssignRemark(userRemark string, recommendation *recommendation.RecommendationScore) string {
	remark := "【自动分配】"
	if recommendation.Reason != "" {
		remark += recommendation.Reason
	}
	if userRemark != "" {
		remark += " | " + userRemark
	}
	return remark
}

// ErrNoSalesAvailable 无可用销售错误
var ErrNoSalesAvailable = errors.New("无可用销售进行分配")
