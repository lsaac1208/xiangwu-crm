package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/usecase"
)

type LeadUIPreferencesHandler struct {
	useCase *usecase.LeadUIPreferencesUseCase
}

func NewLeadUIPreferencesHandler(useCase *usecase.LeadUIPreferencesUseCase) *LeadUIPreferencesHandler {
	return &LeadUIPreferencesHandler{useCase: useCase}
}

func (h *LeadUIPreferencesHandler) GetPreference(c *gin.Context) {
	userID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	result, err := h.useCase.GetPreference(c.Request.Context(), userID)
	if err != nil {
		respondUseCaseError(c, err, "get_lead_preference_failed")
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *LeadUIPreferencesHandler) PutPreference(c *gin.Context) {
	userID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var body usecase.LeadListPreference
	if err := c.ShouldBindJSON(&body); err != nil {
		respondInvalidRequest(c, err)
		return
	}
	result, err := h.useCase.PutPreference(c.Request.Context(), userID, body)
	if err != nil {
		respondUseCaseError(c, err, "save_lead_preference_failed")
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *LeadUIPreferencesHandler) ListViews(c *gin.Context) {
	userID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	result, err := h.useCase.ListViews(c.Request.Context(), userID)
	if err != nil {
		respondUseCaseError(c, err, "list_lead_views_failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": result})
}

func (h *LeadUIPreferencesHandler) CreateView(c *gin.Context) {
	userID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var body usecase.LeadSavedView
	if err := c.ShouldBindJSON(&body); err != nil {
		respondInvalidRequest(c, err)
		return
	}
	result, err := h.useCase.CreateView(c.Request.Context(), userID, body)
	if err != nil {
		respondUseCaseError(c, err, "create_lead_view_failed")
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *LeadUIPreferencesHandler) UpdateView(c *gin.Context) {
	userID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		invalidRequest(c)
		return
	}
	var body usecase.LeadSavedView
	if err := c.ShouldBindJSON(&body); err != nil {
		respondInvalidRequest(c, err)
		return
	}
	result, err := h.useCase.UpdateView(c.Request.Context(), userID, id, body)
	if err != nil {
		respondUseCaseError(c, err, "update_lead_view_failed")
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *LeadUIPreferencesHandler) DeleteView(c *gin.Context) {
	userID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		invalidRequest(c)
		return
	}
	if err := h.useCase.DeleteView(c.Request.Context(), userID, id); err != nil {
		respondUseCaseError(c, err, "delete_lead_view_failed")
		return
	}
	c.Status(http.StatusNoContent)
}
