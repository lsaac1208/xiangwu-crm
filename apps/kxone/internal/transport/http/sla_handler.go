package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/usecase"
	"github.com/lsaac128/kxone/pkg/types"
)

// SLAHandler SLA 统计处理器
type SLAHandler struct {
	managementStats          *usecase.ManagementStatsUseCase
	getSLAStatsUseCase       *usecase.GetSLAStatsUseCase
	getDashboardStatsUseCase *usecase.GetDashboardStatsUseCase
}

func NewSLAHandler(
	getSLAStatsUseCase *usecase.GetSLAStatsUseCase,
	getDashboardStatsUseCase *usecase.GetDashboardStatsUseCase,
) *SLAHandler {
	return &SLAHandler{
		getSLAStatsUseCase:       getSLAStatsUseCase,
		getDashboardStatsUseCase: getDashboardStatsUseCase,
	}
}

// GetSLAStats 获取 SLA 统计
func (h *SLAHandler) GetSLAStats(c *gin.Context) {
	currentUserID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	role, ok := c.Get("role")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	roleName, ok := role.(string)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	status := c.Query("status")
	managerUserID := c.Query("manager_userid")
	if roleName == string(types.UserRoleManager) {
		managerUserID = currentUserID
	}
	salesUserID := c.Query("sales_userid")

	timeRange, rangeErr := parseBusinessTimeRange(c)
	if rangeErr != nil {
		respondError(c, http.StatusBadRequest, rangeErr.Code, rangeErr)
		return
	}

	result, err := h.getSLAStatsUseCase.Execute(c.Request.Context(), &usecase.GetSLAStatsRequest{
		CurrentUserID: currentUserID,
		Role:          roleName,
		Status:        status,
		ManagerUserID: managerUserID,
		SalesUserID:   salesUserID,
		StartTime:     timeRange.Start,
		EndTime:       timeRange.EndExclusive,
	})

	if err != nil {
		respondInternalError(c, "get_sla_stats_failed", err)
		return
	}

	ids := make([]int64, 0, len(result.LeadSLAs))
	for _, item := range result.LeadSLAs {
		ids = append(ids, item.LeadID)
	}
	RecordAggregateRead(c, ids...)
	c.JSON(http.StatusOK, result)
}

func (h *SLAHandler) GetDashboardStats(c *gin.Context) {
	currentUserID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	roleValue, _ := c.Get("role")
	role := roleValue.(string)
	salesUserID := c.Query("sales_userid")

	timeRange, rangeErr := parseBusinessTimeRange(c)
	if rangeErr != nil {
		respondError(c, http.StatusBadRequest, rangeErr.Code, rangeErr)
		return
	}

	result, err := h.getDashboardStatsUseCase.Execute(c.Request.Context(), &usecase.GetDashboardStatsRequest{
		CurrentUserID: currentUserID,
		Role:          role,
		SalesUserID:   salesUserID,
		StartTime:     timeRange.Start,
		EndTime:       timeRange.EndExclusive,
	})
	if err != nil {
		respondInternalError(c, "get_dashboard_stats_failed", err)
		return
	}
	ids := make([]int64, 0, len(result.NextTasks))
	for _, item := range result.NextTasks {
		ids = append(ids, item.LeadID)
	}
	RecordAggregateRead(c, ids...)
	c.JSON(http.StatusOK, result)
}
