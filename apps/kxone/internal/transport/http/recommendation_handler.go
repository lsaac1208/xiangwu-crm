package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/domain/recommendation"
	"github.com/lsaac128/kxone/internal/usecase"
)

// RecommendationHandler 推荐处理器
type RecommendationHandler struct {
	recommendSalesUseCase *usecase.RecommendSalesUseCase
}

func NewRecommendationHandler(
	recommendSalesUseCase *usecase.RecommendSalesUseCase,
) *RecommendationHandler {
	return &RecommendationHandler{
		recommendSalesUseCase: recommendSalesUseCase,
	}
}

// RecommendSalesRequest HTTP 请求
type RecommendSalesRequest struct {
	LeadID         *int64                       `json:"lead_id,omitempty"`
	LeadCity       string                       `json:"lead_city,omitempty"`
	LeadProduct    string                       `json:"lead_product,omitempty"`
	CandidateUsers []string                     `json:"candidate_users,omitempty"`
	TopN           int                          `json:"top_n,omitempty"`
	Weights        *recommendation.ScoreWeights `json:"weights,omitempty"`
}

// RecommendSales 推荐销售
func (h *RecommendationHandler) RecommendSales(c *gin.Context) {
	var req RecommendSalesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondInvalidRequest(c, err)
		return
	}

	// 默认返回前 5 个推荐
	topN := req.TopN
	if topN <= 0 {
		topN = 5
	}

	leadID := int64(0)
	if req.LeadID != nil {
		leadID = *req.LeadID
	}

	result, err := h.recommendSalesUseCase.Execute(c.Request.Context(), &usecase.RecommendSalesRequest{
		LeadID:         leadID,
		LeadCity:       req.LeadCity,
		LeadProduct:    req.LeadProduct,
		CandidateUsers: req.CandidateUsers,
		TopN:           topN,
		Weights:        req.Weights,
	})

	if err != nil {
		respondUseCaseError(c, err, "recommend_sales_failed")
		return
	}

	RecordLeadRead(c, result.Lead)
	c.JSON(http.StatusOK, result)
}

// RecommendForLead 为特定线索推荐销售（GET 接口）
func (h *RecommendationHandler) RecommendForLead(c *gin.Context) {
	leadID, ok := parseLeadIDParam(c)
	if !ok {
		return
	}

	topNStr := c.DefaultQuery("top_n", "5")
	topN, _ := strconv.Atoi(topNStr)
	if topN <= 0 {
		topN = 5
	}

	result, err := h.recommendSalesUseCase.Execute(c.Request.Context(), &usecase.RecommendSalesRequest{
		LeadID: leadID,
		TopN:   topN,
	})

	if err != nil {
		respondUseCaseError(c, err, "recommend_sales_failed")
		return
	}

	RecordLeadReadIDs(c, leadID)
	c.JSON(http.StatusOK, result)
}
