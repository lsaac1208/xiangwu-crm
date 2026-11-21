package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/usecase"
)

type LeadPoolHandler struct{ useCase *usecase.LeadPoolUseCase }

func NewLeadPoolHandler(useCase *usecase.LeadPoolUseCase) *LeadPoolHandler {
	return &LeadPoolHandler{useCase: useCase}
}

func (h *LeadPoolHandler) List(c *gin.Context) {
	userID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	role, err := GetRole(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	result, err := h.useCase.List(c.Request.Context(), &usecase.ListPublicPoolRequest{CurrentUser: userID, Role: role, Keyword: c.Query("keyword"), Page: page, PageSize: pageSize})
	if err != nil {
		respondUseCaseError(c, err, "list_public_pool_failed")
		return
	}
	RecordLeadRead(c, result.Leads...)
	c.JSON(http.StatusOK, result)
}

func (h *LeadPoolHandler) Enter(c *gin.Context) {
	leadID, ok := parseLeadIDParam(c)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondInvalidRequest(c, err)
		return
	}
	userID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	role, err := GetRole(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if err := h.useCase.Enter(c.Request.Context(), leadID, userID, role, body.Reason); err != nil {
		respondUseCaseError(c, err, "enter_public_pool_failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *LeadPoolHandler) Claim(c *gin.Context) {
	leadID, ok := parseLeadIDParam(c)
	if !ok {
		return
	}
	userID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	role, err := GetRole(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if err := h.useCase.Claim(c.Request.Context(), leadID, userID, role); err != nil {
		respondUseCaseError(c, err, "claim_public_pool_failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
