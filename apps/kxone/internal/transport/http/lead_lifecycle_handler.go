package http

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/adapter/repo"
)

type LeadLifecycleHandler struct {
	lifecycle *repo.GormLeadLifecycleRepository
}

func NewLeadLifecycleHandler(lifecycle *repo.GormLeadLifecycleRepository) *LeadLifecycleHandler {
	return &LeadLifecycleHandler{lifecycle: lifecycle}
}

type deleteLeadRequest struct {
	LeadNo                 string `json:"lead_no"`
	ExpectedVersion        int    `json:"expected_version"`
	ExpectedProfileVersion int    `json:"expected_profile_version"`
	Reason                 string `json:"reason"`
}

type restoreLeadRequest struct {
	Reason string `json:"reason"`
}

func (h *LeadLifecycleHandler) Delete(c *gin.Context) {
	id, ok := parseLeadIDParam(c)
	if !ok {
		return
	}
	actor, ok := lifecycleActor(c)
	if !ok {
		return
	}
	var body deleteLeadRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		respondInvalidRequest(c, err)
		return
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if strings.TrimSpace(body.LeadNo) == "" || body.ExpectedVersion < 0 || body.ExpectedProfileVersion < 1 || !repo.ValidLeadLifecycleReason(body.Reason) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_delete_request"})
		return
	}
	if err := h.lifecycle.Delete(c.Request.Context(), id, body.LeadNo, body.ExpectedVersion, body.ExpectedProfileVersion, actor, body.Reason); err != nil {
		respondLeadLifecycleError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *LeadLifecycleHandler) ListDeleted(c *gin.Context) {
	if _, ok := lifecycleActor(c); !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	items, total, err := h.lifecycle.ListDeleted(c.Request.Context(), (page-1)*pageSize, pageSize)
	if err != nil {
		respondInternalError(c, "list_deleted_leads_failed", err)
		return
	}
	ids := make([]int64, 0, len(items))
	for _, lead := range items {
		ids = append(ids, lead.ID)
	}
	RecordLeadReadIDs(c, ids...)
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func (h *LeadLifecycleHandler) Restore(c *gin.Context) {
	id, ok := parseLeadIDParam(c)
	if !ok {
		return
	}
	actor, ok := lifecycleActor(c)
	if !ok {
		return
	}
	var body restoreLeadRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		respondInvalidRequest(c, err)
		return
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if !repo.ValidLeadLifecycleReason(body.Reason) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_restore_reason"})
		return
	}
	if err := h.lifecycle.Restore(c.Request.Context(), id, actor, body.Reason); err != nil {
		respondLeadLifecycleError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"restored": true, "lead_id": id})
}

func lifecycleActor(c *gin.Context) (string, bool) {
	role, err := GetRole(c)
	if err != nil || role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient_permissions"})
		return "", false
	}
	actor, err := GetWeComUserID(c)
	if err != nil || strings.TrimSpace(actor) == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return "", false
	}
	return actor, true
}

func respondLeadLifecycleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, repo.ErrLeadLifecycleNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "lead_not_found"})
	case errors.Is(err, repo.ErrLeadLifecycleConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "lead_version_conflict"})
	default:
		respondInternalError(c, "lead_lifecycle_failed", err)
	}
}
