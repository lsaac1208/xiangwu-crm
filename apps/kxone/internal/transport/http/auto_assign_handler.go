package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/usecase"
)

// AutoAssignHandler 自动分配处理器
type AutoAssignHandler struct {
	autoAssignUseCase *usecase.AutoAssignLeadUseCase
}

func NewAutoAssignHandler(
	autoAssignUseCase *usecase.AutoAssignLeadUseCase,
) *AutoAssignHandler {
	return &AutoAssignHandler{
		autoAssignUseCase: autoAssignUseCase,
	}
}

// AutoAssignLead 自动分配线索
func (h *AutoAssignHandler) AutoAssignLead(c *gin.Context) {
	leadID, ok := parseLeadIDParam(c)
	if !ok {
		return
	}

	// 获取当前用户（主管）
	managerUserID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	role, err := GetRole(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req struct {
		CandidateUsers []string `json:"candidate_users,omitempty"`
		TopN           int      `json:"top_n,omitempty"`
		Remark         string   `json:"remark,omitempty"`
	}

	// 允许空 body（使用默认参数）
	_ = c.ShouldBindJSON(&req)

	result, err := h.autoAssignUseCase.Execute(c.Request.Context(), &usecase.AutoAssignLeadRequest{
		LeadID:         leadID,
		ManagerUserID:  managerUserID,
		ActorRole:      role,
		CandidateUsers: req.CandidateUsers,
		TopN:           req.TopN,
		Remark:         req.Remark,
	})

	if err != nil {
		respondUseCaseError(c, err, "auto_assign_failed")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":               true,
		"message":               "线索已自动分配",
		"assigned_sales_userid": result.AssignedSalesUserID,
		"assigned_sales_name":   result.AssignedSalesName,
		"score":                 result.Score,
		"reason":                result.Reason,
	})
}
