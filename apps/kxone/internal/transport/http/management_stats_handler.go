package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/usecase"
)

func (h *SLAHandler) WithManagementStats(uc *usecase.ManagementStatsUseCase) *SLAHandler {
	h.managementStats = uc
	return h
}
func (h *SLAHandler) GetManagementStats(c *gin.Context) {
	timeRange, err := parseBusinessTimeRange(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Code, err)
		return
	}
	result, executeErr := h.managementStats.Report(c.Request.Context(), repo.LeadStatsFilter{SalesUserID: c.Query("sales_userid"), StartTime: timeRange.Start, EndTime: timeRange.EndExclusive})
	if executeErr != nil {
		respondInternalError(c, "get_management_stats_failed", executeErr)
		return
	}
	RecordAggregateRead(c)
	c.JSON(http.StatusOK, result)
}

func (h *SLAHandler) GetFollowupSupervision(c *gin.Context) {
	result, err := h.managementStats.Supervision(c.Request.Context())
	if err != nil {
		respondInternalError(c, "get_followup_supervision_failed", err)
		return
	}
	ids := make([]int64, 0, len(result.Tasks))
	for _, item := range result.Tasks {
		ids = append(ids, item.LeadID)
	}
	RecordAggregateRead(c, ids...)
	c.JSON(http.StatusOK, result)
}
