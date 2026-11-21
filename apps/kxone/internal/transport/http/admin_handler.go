package http

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
	"github.com/lsaac128/kxone/internal/usecase"
	"github.com/lsaac128/kxone/pkg/types"
)

// AdminHandler 后台管理处理器。
type AdminHandler struct {
	settingsUseCase       *usecase.AdminSettingsUseCase
	userBindingsUseCase   *usecase.AdminUserBindingsUseCase
	groupRobotSender      wecom.GroupRobotSender
	groupRobotDiagnostics repo.GroupRobotDiagnosticsReader
}

func (h *AdminHandler) WithGroupRobotDiagnostics(reader repo.GroupRobotDiagnosticsReader) *AdminHandler {
	h.groupRobotDiagnostics = reader
	return h
}

func (h *AdminHandler) GroupRobotHealth(c *gin.Context) {
	if h.groupRobotDiagnostics == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "group_robot_health_unavailable"})
		return
	}
	health, err := h.groupRobotDiagnostics.DeliveryHealth(c.Request.Context(), time.Now().UTC())
	if err != nil {
		internalError(c, http.StatusInternalServerError, "group_robot_health_failed", err)
		return
	}
	c.JSON(http.StatusOK, health)
}

func (h *AdminHandler) WithGroupRobotSender(sender wecom.GroupRobotSender) *AdminHandler {
	h.groupRobotSender = sender
	return h
}

func (h *AdminHandler) SendGroupRobotTest(c *gin.Context) {
	if h.groupRobotSender == nil {
		internalError(c, http.StatusServiceUnavailable, "group_robot_sender_unavailable", nil)
		return
	}
	users, err := h.userBindingsUseCase.List(c.Request.Context(), &usecase.ListUserBindingsRequest{Status: string(types.UserStatusActive), Page: 1, PageSize: 100})
	if err != nil {
		internalError(c, http.StatusInternalServerError, "list_group_robot_test_users_failed", err)
		return
	}
	mentions := make([]string, 0, len(users.Bindings))
	names := make([]string, 0, len(users.Bindings))
	for _, user := range users.Bindings {
		if user == nil || strings.TrimSpace(user.GroupRobotMentionUserID) == "" {
			continue
		}
		mentions = append(mentions, strings.TrimSpace(user.GroupRobotMentionUserID))
		name := strings.TrimSpace(user.EmployeeName)
		if name == "" {
			name = user.Username
		}
		names = append(names, "@"+name)
	}
	content := "【CRM 群机器人配置测试】\n这是一条测试消息，不会创建线索或业务提醒。\n测试 @：" + strings.Join(names, "、")
	if err := h.groupRobotSender.Send(c.Request.Context(), wecom.GroupRobotMessage{Content: content, MentionUserIDs: mentions}); err != nil {
		internalError(c, http.StatusBadGateway, "group_robot_test_failed", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "mention_count": len(mentions)})
}

func NewAdminHandler(
	settingsUseCase *usecase.AdminSettingsUseCase,
	userBindingsUseCase *usecase.AdminUserBindingsUseCase,
) *AdminHandler {
	return &AdminHandler{
		settingsUseCase:     settingsUseCase,
		userBindingsUseCase: userBindingsUseCase,
	}
}

func (h *AdminHandler) ListSettings(c *gin.Context) {
	result, err := h.settingsUseCase.List(c.Request.Context(), &usecase.ListSettingsRequest{
		Group: c.Query("group"),
	})
	if err != nil {
		internalError(c, http.StatusInternalServerError, "list_settings_failed", err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *AdminHandler) UpdateSettings(c *gin.Context) {
	var req struct {
		Settings []usecase.UpdateSettingInput `json:"settings" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondInvalidRequest(c, err)
		return
	}

	updatedBy, _ := GetWeComUserID(c)
	if err := h.settingsUseCase.Update(c.Request.Context(), &usecase.UpdateSettingsRequest{
		Settings:  req.Settings,
		UpdatedBy: updatedBy,
	}); err != nil {
		if strings.Contains(err.Error(), "must be a comma-separated list of positive integers") ||
			strings.Contains(err.Error(), "must be a comma-separated list of non-negative integers") ||
			strings.Contains(err.Error(), "must be a positive integer") ||
			strings.Contains(err.Error(), "must be true or false") ||
			strings.Contains(err.Error(), "must be an enterprise WeChat group robot webhook URL") ||
			strings.Contains(err.Error(), " requires ") {
			internalError(c, http.StatusBadRequest, "invalid_setting_value", err)
			return
		}
		internalError(c, http.StatusInternalServerError, "update_settings_failed", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *AdminHandler) ListUserBindings(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	result, err := h.userBindingsUseCase.List(c.Request.Context(), &usecase.ListUserBindingsRequest{
		Keyword:  c.Query("keyword"),
		Status:   c.Query("status"),
		Role:     c.Query("role"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		internalError(c, http.StatusInternalServerError, "list_user_bindings_failed", err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *AdminHandler) ListSalesUsers(c *gin.Context) {
	result, err := h.userBindingsUseCase.ListAssignableInviters(c.Request.Context())
	if err != nil {
		internalError(c, http.StatusInternalServerError, "list_sales_users_failed", err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *AdminHandler) ListLeadOwnerFilters(c *gin.Context) {
	result, err := h.userBindingsUseCase.ListLeadOwnerFilters(c.Request.Context())
	if err != nil {
		internalError(c, http.StatusInternalServerError, "list_lead_owner_filters_failed", err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *AdminHandler) ListCloserUsers(c *gin.Context) {
	result, err := h.userBindingsUseCase.ListAssignableClosers(c.Request.Context())
	if err != nil {
		internalError(c, http.StatusInternalServerError, "list_closer_users_failed", err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *AdminHandler) CreateUserBinding(c *gin.Context) {
	req, ok := bindUserBindingRequest(c)
	if !ok {
		return
	}

	binding, err := h.userBindingsUseCase.Create(c.Request.Context(), req)
	if err != nil {
		internalError(c, http.StatusBadRequest, "create_user_binding_failed", err)
		return
	}
	c.JSON(http.StatusCreated, binding)
}

func (h *AdminHandler) UpdateUserBinding(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_binding_id"})
		return
	}

	req, ok := bindUserBindingRequest(c)
	if !ok {
		return
	}
	req.ID = id
	if req.ExpectedVersion < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected_version_required", "message": "请重新打开账号资料后保存"})
		return
	}

	binding, err := h.userBindingsUseCase.Update(c.Request.Context(), req)
	if err != nil {
		status := http.StatusBadRequest
		code := "update_user_binding_failed"
		if err == types.ErrAccountVersionConflict {
			c.JSON(http.StatusConflict, gin.H{"error": "account_version_conflict", "message": "账号已被其他操作修改，输入已保留，请核对最新资料"})
			return
		}
		if err == types.ErrUserBindingNotFound {
			status = http.StatusNotFound
			code = "user_binding_not_found"
		}
		internalError(c, status, code, err)
		return
	}
	slog.Info("account updated", "actor_account_id", c.GetInt64("account_id"), "account_id", binding.ID, "account_version", binding.AccountVersion, "auth_version", binding.AuthVersion)
	c.JSON(http.StatusOK, binding)
}

func bindUserBindingRequest(c *gin.Context) (*usecase.SaveUserBindingRequest, bool) {
	var req struct {
		ExpectedVersion         int64            `json:"expected_version"`
		CRMUserID               int64            `json:"crm_user_id"`
		Username                string           `json:"username" binding:"required"`
		Password                string           `json:"password"`
		EmployeeName            string           `json:"employee_name"`
		Role                    types.UserRole   `json:"role" binding:"required"`
		DepartmentID            int64            `json:"department_id"`
		Status                  types.UserStatus `json:"status"`
		GroupRobotMentionUserID string           `json:"group_robot_mention_userid"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		respondInvalidRequest(c, err)
		return nil, false
	}
	if !types.IsValidUserRole(req.Role) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_role"})
		return nil, false
	}
	if req.Status != "" && req.Status != types.UserStatusActive && req.Status != types.UserStatusDisabled && req.Status != types.UserStatusDeleted {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_status"})
		return nil, false
	}

	return &usecase.SaveUserBindingRequest{
		ExpectedVersion:         req.ExpectedVersion,
		CRMUserID:               req.CRMUserID,
		Username:                req.Username,
		Password:                req.Password,
		EmployeeName:            req.EmployeeName,
		Role:                    req.Role,
		DepartmentID:            req.DepartmentID,
		Status:                  req.Status,
		GroupRobotMentionUserID: req.GroupRobotMentionUserID,
	}, true
}
