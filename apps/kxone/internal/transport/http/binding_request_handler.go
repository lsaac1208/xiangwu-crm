package http

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/usecase"
	"github.com/lsaac128/kxone/pkg/types"
)

// BindingRequestHandler 用户绑定申请处理器。
type BindingRequestHandler struct {
	useCase *usecase.UserBindingRequestsUseCase
}

func NewBindingRequestHandler(useCase *usecase.UserBindingRequestsUseCase) *BindingRequestHandler {
	return &BindingRequestHandler{useCase: useCase}
}

func (h *BindingRequestHandler) Submit(c *gin.Context) {
	var req struct {
		EmployeeName string `json:"employee_name" binding:"required"`
		Mobile       string `json:"mobile"`
		Department   string `json:"department"`
		Remark       string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondInvalidRequest(c, err)
		return
	}
	wecomUserID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_binding_token"})
		return
	}

	request, err := h.useCase.Submit(c.Request.Context(), &usecase.SubmitBindingRequest{
		WeComUserID:  wecomUserID,
		EmployeeName: req.EmployeeName,
		Mobile:       req.Mobile,
		Department:   req.Department,
		Remark:       req.Remark,
	})
	if err != nil {
		status := http.StatusBadRequest
		code := "submit_binding_request_failed"
		if errors.Is(err, usecase.ErrUserAlreadyBound) {
			status = http.StatusConflict
			code = "user_already_bound"
		}
		internalError(c, status, code, err)
		return
	}
	c.JSON(http.StatusCreated, request)
}

func (h *BindingRequestHandler) GetLatest(c *gin.Context) {
	wecomUserID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_binding_token"})
		return
	}
	request, err := h.useCase.LatestByWeComUserID(c.Request.Context(), wecomUserID)
	if err != nil {
		internalError(c, http.StatusInternalServerError, "query_binding_request_failed", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"request": request})
}

func (h *BindingRequestHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	result, err := h.useCase.List(c.Request.Context(), &usecase.ListBindingRequestsRequest{
		Status:   c.Query("status"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		internalError(c, http.StatusInternalServerError, "list_binding_requests_failed", err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *BindingRequestHandler) Review(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request_id"})
		return
	}

	var req struct {
		Action       string         `json:"action" binding:"required"`
		CRMUserID    int64          `json:"crm_user_id"`
		EmployeeName string         `json:"employee_name"`
		Role         types.UserRole `json:"role"`
		DepartmentID int64          `json:"department_id"`
		RejectReason string         `json:"reject_reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondInvalidRequest(c, err)
		return
	}

	reviewedBy, _ := GetWeComUserID(c)
	request, err := h.useCase.Review(c.Request.Context(), &usecase.ReviewBindingRequest{
		ID:           id,
		Action:       req.Action,
		CRMUserID:    req.CRMUserID,
		EmployeeName: req.EmployeeName,
		Role:         req.Role,
		DepartmentID: req.DepartmentID,
		RejectReason: req.RejectReason,
		ReviewedBy:   reviewedBy,
	})
	if err != nil {
		status := http.StatusBadRequest
		code := "review_binding_request_failed"
		if errors.Is(err, usecase.ErrBindingRequestNotFound) {
			status = http.StatusNotFound
			code = "binding_request_not_found"
		} else if errors.Is(err, usecase.ErrBindingRequestReviewed) {
			code = "binding_request_already_reviewed"
		}
		internalError(c, status, code, err)
		return
	}
	c.JSON(http.StatusOK, request)
}
