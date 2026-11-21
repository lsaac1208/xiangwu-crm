package http

import (
	"errors"
	"github.com/lsaac128/kxone/pkg/types"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lsaac128/kxone/internal/usecase"
)

// FollowupHandler 跟进记录处理器
type FollowupHandler struct {
	createFollowupUseCase *usecase.CreateFollowupUseCase
	getLeadUseCase        *usecase.GetLeadUseCase
}

func NewFollowupHandler(
	createFollowupUseCase *usecase.CreateFollowupUseCase,
	getLeadUseCase *usecase.GetLeadUseCase,
) *FollowupHandler {
	return &FollowupHandler{
		createFollowupUseCase: createFollowupUseCase,
		getLeadUseCase:        getLeadUseCase,
	}
}

// CreateFollowupRequest 创建跟进请求
type CreateFollowupRequest struct {
	FollowupResult string  `json:"followup_result" binding:"required"`
	IntentionLevel string  `json:"intention_level"`
	Remark         string  `json:"remark" binding:"required"`
	NextFollowupAt *string `json:"next_followup_at"` // RFC3339 格式
	IsValid        *bool   `json:"is_valid"`
	InvalidReason  string  `json:"invalid_reason"`
	StarRating     *int    `json:"star_rating"`
	ReturnToPool   bool    `json:"return_to_pool"`
}

// CreateFollowup 创建跟进记录
func (h *FollowupHandler) CreateFollowup(c *gin.Context) {
	leadID, ok := parseLeadIDParam(c)
	if !ok {
		return
	}

	key := c.GetHeader("Idempotency-Key")
	parsed, err := uuid.Parse(key)
	if err != nil || parsed.String() != key {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_idempotency_key", "message": "页面提交标识无效，请先保留填写内容，再更新页面重试"})
		return
	}
	var req CreateFollowupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondInvalidRequest(c, err)
		return
	}

	// 获取当前用户
	salesUserID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	// 解析下次跟进时间
	var nextFollowupAt *time.Time
	if req.NextFollowupAt != nil && *req.NextFollowupAt != "" {
		t, err := time.Parse(time.RFC3339, *req.NextFollowupAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_next_followup_at"})
			return
		}
		nextFollowupAt = &t
	}

	isValid := true
	if req.IsValid != nil {
		isValid = *req.IsValid
	}
	invalidReason := strings.TrimSpace(req.InvalidReason)
	if !isValid && invalidReason == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_reason_required"})
		return
	}

	rating := 0
	if req.StarRating != nil {
		rating = *req.StarRating
	}
	// 执行创建跟进
	err = h.createFollowupUseCase.Execute(c.Request.Context(), &usecase.CreateFollowupRequest{
		AccountID:      c.GetInt64("account_id"),
		IdempotencyKey: key,
		LeadID:         leadID,
		SalesUserID:    salesUserID,
		FollowupResult: req.FollowupResult,
		IntentionLevel: req.IntentionLevel,
		Remark:         req.Remark,
		NextFollowupAt: nextFollowupAt,
		IsValid:        isValid,
		InvalidReason:  invalidReason,
		StarRating:     rating, StarRatingProvided: req.StarRating != nil,
		ReturnToPool: req.ReturnToPool,
	})

	if err != nil {
		if errors.Is(err, types.ErrIdempotencyConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": "idempotency_conflict", "message": "上次跟进可能已保存，请先核对跟进记录；当前内容已保留"})
			return
		}
		respondUseCaseError(c, err, "create_followup_failed")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "跟进记录已创建",
	})
}
