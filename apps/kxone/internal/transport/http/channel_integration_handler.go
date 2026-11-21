package http

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/usecase"
	"gorm.io/gorm"
)

type ChannelIntegrationHandler struct {
	useCase *usecase.ChannelIntegrationsUseCase
}

func NewChannelIntegrationHandler(uc *usecase.ChannelIntegrationsUseCase) *ChannelIntegrationHandler {
	return &ChannelIntegrationHandler{useCase: uc}
}

func (h *ChannelIntegrationHandler) List(c *gin.Context) {
	result, err := h.useCase.List(c.Request.Context())
	if err != nil {
		internalError(c, http.StatusInternalServerError, "list_channel_integrations_failed", err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *ChannelIntegrationHandler) Create(c *gin.Context) {
	req, ok := bindChannelIntegration(c)
	if !ok {
		return
	}
	req.ActorUserID, _ = GetWeComUserID(c)
	item, err := h.useCase.Create(c.Request.Context(), req)
	if err != nil {
		if usecase.IsChannelConflict(err) {
			internalError(c, http.StatusConflict, "channel_integration_conflict", err)
			return
		}
		internalError(c, http.StatusBadRequest, "create_channel_integration_failed", err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *ChannelIntegrationHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_channel_integration_id"})
		return
	}
	req, ok := bindChannelIntegration(c)
	if !ok {
		return
	}
	req.ID, req.ActorUserID = id, func() string { value, _ := GetWeComUserID(c); return value }()
	item, err := h.useCase.Update(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "channel_integration_not_found"})
			return
		}
		internalError(c, http.StatusBadRequest, "update_channel_integration_failed", err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *ChannelIntegrationHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_channel_integration_id"})
		return
	}
	actor, _ := GetWeComUserID(c)
	if err := h.useCase.Delete(c.Request.Context(), id, actor); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "channel_integration_not_found"})
			return
		}
		internalError(c, http.StatusInternalServerError, "delete_channel_integration_failed", err)
		return
	}
	c.Status(http.StatusNoContent)
}

func bindChannelIntegration(c *gin.Context) (*usecase.SaveChannelIntegrationRequest, bool) {
	var req usecase.SaveChannelIntegrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondInvalidRequest(c, err)
		return nil, false
	}
	return &req, true
}
