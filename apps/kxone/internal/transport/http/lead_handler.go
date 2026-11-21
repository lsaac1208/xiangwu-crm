package http

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
	"github.com/lsaac128/kxone/internal/usecase"
	"github.com/lsaac128/kxone/pkg/types"
)

// LeadHandler 线索处理器
type LeadHandler struct {
	createUseCase      *usecase.CreateLeadUseCase
	assignUseCase      *usecase.AssignLeadUseCase
	reassignUseCase    *usecase.ReassignLeadUseCase
	claimUseCase       *usecase.ClaimLeadUseCase
	completeUseCase    *usecase.CompleteLeadUseCase
	getLeadUseCase     *usecase.GetLeadUseCase
	listLeadsUseCase   *usecase.ListLeadsUseCase
	profileUseCase     *usecase.UpdateLeadProfileUseCase
	negotiationUseCase *usecase.NegotiationUseCase
	groupRobotNotifier wecom.GroupRobotNotifier
	batchUseCase       *usecase.BatchLeadsUseCase
}

func (h *LeadHandler) WithBatchUseCase(batch *usecase.BatchLeadsUseCase) *LeadHandler {
	h.batchUseCase = batch
	return h
}

func (h *LeadHandler) WithNegotiationUseCase(negotiation *usecase.NegotiationUseCase) *LeadHandler {
	h.negotiationUseCase = negotiation
	return h
}

func (h *LeadHandler) WithGroupRobotNotifier(notifier wecom.GroupRobotNotifier) *LeadHandler {
	h.groupRobotNotifier = notifier
	return h
}

func (h *LeadHandler) WithProfileUpdateUseCase(profile *usecase.UpdateLeadProfileUseCase) *LeadHandler {
	h.profileUseCase = profile
	return h
}

func NewLeadHandler(
	createUseCase *usecase.CreateLeadUseCase,
	assignUseCase *usecase.AssignLeadUseCase,
	reassignUseCase *usecase.ReassignLeadUseCase,
	claimUseCase *usecase.ClaimLeadUseCase,
	completeUseCase *usecase.CompleteLeadUseCase,
	getLeadUseCase *usecase.GetLeadUseCase,
	listLeadsUseCase *usecase.ListLeadsUseCase,
) *LeadHandler {
	return &LeadHandler{
		createUseCase:    createUseCase,
		assignUseCase:    assignUseCase,
		reassignUseCase:  reassignUseCase,
		claimUseCase:     claimUseCase,
		completeUseCase:  completeUseCase,
		getLeadUseCase:   getLeadUseCase,
		listLeadsUseCase: listLeadsUseCase,
	}
}

func (h *LeadHandler) BatchAssign(c *gin.Context)   { h.batchLeadOperation(c, false) }
func (h *LeadHandler) BatchReassign(c *gin.Context) { h.batchLeadOperation(c, true) }

func (h *LeadHandler) batchLeadOperation(c *gin.Context, reassign bool) {
	if h.batchUseCase == nil {
		respondInternalError(c, "batch_leads_not_configured", nil)
		return
	}
	var body usecase.BatchLeadRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		respondInvalidRequest(c, err)
		return
	}
	actor, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	role, err := GetRole(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	body.ActorUserID, body.ActorRole = actor, role
	var result *usecase.BatchLeadResult
	if reassign {
		result, err = h.batchUseCase.Reassign(c.Request.Context(), &body)
	} else {
		result, err = h.batchUseCase.Assign(c.Request.Context(), &body)
	}
	if err != nil {
		var conflict *usecase.BatchLeadConflict
		if errors.As(err, &conflict) {
			c.JSON(http.StatusConflict, gin.H{"error": "batch_lead_conflict", "lead_nos": conflict.LeadNos})
			return
		}
		respondUseCaseError(c, err, "batch_leads_failed")
		return
	}
	c.JSON(http.StatusOK, result)
}

// CreateLeadRequest 创建线索请求。
type CreateLeadRequest struct {
	LeadNo                string `json:"lead_no"`
	CustomerName          string `json:"customer_name" binding:"required"`
	CustomerMobile        string `json:"customer_mobile"`
	CustomerMobileMasked  string `json:"customer_mobile_masked"`
	CustomerWechat        string `json:"customer_wechat"`
	City                  string `json:"city"`
	ProductName           string `json:"product_name"`
	SourceChannel         string `json:"source_channel"`
	SourceCampaign        string `json:"source_campaign"`
	LeadLevel             string `json:"lead_level"`
	StarRating            int    `json:"star_rating"`
	DispatchManagerUserID string `json:"dispatch_manager_userid"`
	AssignedInviterUserID string `json:"assigned_inviter_userid"`
}

type UpdateLeadProfileRequest struct {
	Version        int     `json:"version"`
	ProfileVersion int     `json:"profile_version"`
	CustomerName   *string `json:"customer_name"`
	CustomerMobile *string `json:"customer_mobile"`
	City           *string `json:"city"`
	ProductName    *string `json:"product_name"`
	SourceChannel  *string `json:"source_channel"`
}

// CreateLead 创建线索并进入主管派发流程。
func (h *LeadHandler) CreateLead(c *gin.Context) {
	if _, err := uuid.Parse(c.GetHeader("Idempotency-Key")); err != nil {
		c.JSON(422, gin.H{"error": "invalid_idempotency_key", "message": "请刷新页面后重试录入"})
		return
	}
	var req CreateLeadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if IsRequestBodyTooLarge(err) {
			respondInvalidRequest(c, err)
		} else {
			c.JSON(422, gin.H{"error": "invalid_lead_profile", "message": "请检查客户资料填写格式"})
		}
		return
	}

	currentUserID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	role, err := GetRole(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	result, err := h.createUseCase.ExecuteManual(c.Request.Context(), &usecase.CreateLeadRequest{
		AccountID:             c.GetInt64("account_id"),
		IdempotencyKey:        c.GetHeader("Idempotency-Key"),
		LeadNo:                req.LeadNo,
		CustomerName:          req.CustomerName,
		CustomerMobile:        req.CustomerMobile,
		CustomerMobileMasked:  req.CustomerMobileMasked,
		CustomerWechat:        req.CustomerWechat,
		City:                  req.City,
		ProductName:           req.ProductName,
		SourceChannel:         req.SourceChannel,
		SourceCampaign:        req.SourceCampaign,
		LeadLevel:             req.LeadLevel,
		StarRating:            req.StarRating,
		DispatchManagerUserID: currentUserID,
		ActorUserID:           currentUserID,
		ActorRole:             role,
		AssignedInviterUserID: req.AssignedInviterUserID,
	})
	if err != nil {
		if errors.Is(err, types.ErrInvalidManualLeadSource) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid_request", "message": "请选择有效的来源渠道。若页面仍显示旧选项，请先复制已填资料，再刷新页面；如刚提交过，请先在线索列表确认。"})
			return
		}
		if errors.Is(err, types.ErrIdempotencyConflict) {
			c.JSON(409, gin.H{"error": "idempotency_conflict", "message": "上次录入请求已处理不同内容，请先到列表确认结果"})
			return
		}
		respondUseCaseError(c, err, "create_lead_failed")
		return
	}
	if h.groupRobotNotifier != nil && !result.Replayed {
		lead := result.Lead
		if err := h.groupRobotNotifier.NotifyLead(c.Request.Context(), wecom.GroupRobotLead{
			LeadID: lead.ID, BusinessVersion: lead.Version, Source: "manual", CustomerName: lead.CustomerName,
			Phone: lead.CustomerMobileMasked, Wechat: lead.CustomerWechatMasked, Region: lead.City,
			FormName: lead.ProductName, CampaignName: lead.SourceCampaign,
		}); err != nil {
			slog.Warn("queue group robot lead notification failed", "lead_id", lead.ID, "err", err)
			result.Warnings = append(result.Warnings, "notification_failed")
		}
	}

	c.JSON(http.StatusCreated, result)
}

func (h *LeadHandler) SelfClaimLead(c *gin.Context) {
	leadID, ok := parseLeadIDParam(c)
	if !ok {
		return
	}
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
	if err := h.assignUseCase.SelfClaim(c.Request.Context(), leadID, managerUserID, role); err != nil {
		respondUseCaseError(c, err, "self_claim_lead_failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已由您亲自跟进"})
}

// AssignLeadRequest 分配线索请求
type AssignLeadRequest struct {
	SalesUserID string `json:"sales_userid" binding:"required"`
	Remark      string `json:"remark"`
}

// AssignLead 主管分配线索
func (h *LeadHandler) AssignLead(c *gin.Context) {
	leadID, ok := parseLeadIDParam(c)
	if !ok {
		return
	}

	var req AssignLeadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondInvalidRequest(c, err)
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

	// 执行分配逻辑
	err = h.assignUseCase.Execute(c.Request.Context(), &usecase.AssignLeadRequest{
		LeadID:        leadID,
		ManagerUserID: managerUserID,
		ActorRole:     role,
		SalesUserID:   req.SalesUserID,
		Remark:        req.Remark,
	})

	if err != nil {
		respondUseCaseError(c, err, "assign_lead_failed")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "线索已分配",
	})
}

// ReassignLeadRequest 转派线索请求。
type ReassignLeadRequest struct {
	SalesUserID string `json:"sales_userid" binding:"required"`
	Remark      string `json:"remark"`
}

// ReassignLead 主管转派线索。
func (h *LeadHandler) ReassignLead(c *gin.Context) {
	leadID, ok := parseLeadIDParam(c)
	if !ok {
		return
	}

	var req ReassignLeadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondInvalidRequest(c, err)
		return
	}

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

	err = h.reassignUseCase.Execute(c.Request.Context(), &usecase.ReassignLeadRequest{
		LeadID:         leadID,
		ManagerUserID:  managerUserID,
		ActorRole:      role,
		NewSalesUserID: req.SalesUserID,
		Remark:         req.Remark,
	})
	if err != nil {
		respondUseCaseError(c, err, "reassign_lead_failed")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "线索已转派",
	})
}

// ClaimLead 销售领取线索
func (h *LeadHandler) ClaimLead(c *gin.Context) {
	leadID, ok := parseLeadIDParam(c)
	if !ok {
		return
	}

	// 获取当前用户（销售）
	salesUserID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	// 执行领取逻辑
	err = h.claimUseCase.Execute(c.Request.Context(), &usecase.ClaimLeadRequest{
		LeadID:      leadID,
		SalesUserID: salesUserID,
		Source:      "h5", // H5 页面主动领取
	})

	if err != nil {
		respondUseCaseError(c, err, "claim_lead_failed")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "领取成功",
	})
}

type CompleteLeadRequest struct {
	DealAmount      float64 `json:"deal_amount"`
	CloseReason     string  `json:"close_reason"`
	PaymentStatus   string  `json:"payment_status"`
	RefundAmount    float64 `json:"refund_amount"`
	ExpectedVersion *int    `json:"expected_version"`
	Reason          string  `json:"reason"`
}

func (h *LeadHandler) ConvertLead(c *gin.Context) {
	h.completeLead(c, "convert")
}

func (h *LeadHandler) UpdateDeal(c *gin.Context)     { h.completeLead(c, "update_deal") }
func (h *LeadHandler) RestoreInvalid(c *gin.Context) { h.completeLead(c, "restore_invalid") }

func (h *LeadHandler) CloseLead(c *gin.Context) {
	h.completeLead(c, "close")
}

func (h *LeadHandler) ReassignCloser(c *gin.Context) {
	leadID, ok := parseLeadIDParam(c)
	if !ok {
		return
	}
	var body struct {
		CloserUserID    string `json:"closer_userid" binding:"required"`
		ExpectedVersion int    `json:"expected_version"`
		Reason          string `json:"reason" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		respondInvalidRequest(c, err)
		return
	}
	actor, role, ok := actorIdentity(c)
	if !ok {
		return
	}
	if h.negotiationUseCase == nil {
		respondInternalError(c, "negotiation_not_configured", nil)
		return
	}
	if err := h.negotiationUseCase.ReassignCloser(c.Request.Context(), &usecase.ReassignNegotiationCloserRequest{LeadID: leadID, ActorUserID: actor, ActorRole: role, CloserUserID: body.CloserUserID, ExpectedVersion: body.ExpectedVersion, Reason: body.Reason}); err != nil {
		respondUseCaseError(c, err, "reassign_closer_failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *LeadHandler) completeLead(c *gin.Context, action string) {
	leadID, ok := parseLeadIDParam(c)
	if !ok {
		return
	}

	var req CompleteLeadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondInvalidRequest(c, err)
		return
	}

	actorUserID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	role, err := GetRole(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	lead, err := h.completeUseCase.Execute(c.Request.Context(), &usecase.CompleteLeadRequest{
		LeadID:          leadID,
		ActorUserID:     actorUserID,
		ActorRole:       role,
		Action:          action,
		DealAmount:      req.DealAmount,
		CloseReason:     req.CloseReason,
		PaymentStatus:   req.PaymentStatus,
		RefundAmount:    req.RefundAmount,
		ExpectedVersion: req.ExpectedVersion,
		Reason:          req.Reason,
	})
	if err != nil {
		respondUseCaseError(c, err, "complete_lead_failed")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"lead":    lead,
	})
}

// GetLead 获取线索详情
func (h *LeadHandler) GetLead(c *gin.Context) {
	leadID, ok := parseLeadIDParam(c)
	if !ok {
		return
	}

	wecomUserID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	role, err := GetRole(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	result, err := h.getLeadUseCase.Execute(c.Request.Context(), leadID, wecomUserID, role)

	if err != nil {
		respondUseCaseError(c, err, "get_lead_failed")
		return
	}

	RecordLeadRead(c, result.Lead)
	c.JSON(http.StatusOK, result)
}

func (h *LeadHandler) UpdateProfile(c *gin.Context) {
	leadID, ok := parseLeadIDParam(c)
	if !ok {
		return
	}
	if h.profileUseCase == nil {
		respondInternalError(c, "lead_profile_not_configured", nil)
		return
	}
	var body UpdateLeadProfileRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		respondInvalidRequest(c, err)
		return
	}
	actorUserID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	role, err := GetRole(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	result, err := h.profileUseCase.Execute(c.Request.Context(), &usecase.UpdateLeadProfileRequest{
		LeadID:                  leadID,
		ActorUserID:             actorUserID,
		ActorRole:               role,
		ExpectedBusinessVersion: body.Version,
		ExpectedProfileVersion:  body.ProfileVersion,
		CustomerName:            body.CustomerName,
		CustomerMobile:          body.CustomerMobile,
		City:                    body.City,
		ProductName:             body.ProductName,
		SourceChannel:           body.SourceChannel,
	})
	if err != nil {
		respondUseCaseError(c, err, "update_lead_profile_failed")
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListLeads 查询线索列表
func (h *LeadHandler) ListLeads(c *gin.Context) {
	// 解析查询参数
	status := c.Query("status")
	keyword := c.Query("keyword")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	salesUserID := strings.TrimSpace(c.Query("sales_userid"))
	sortBy := strings.TrimSpace(c.DefaultQuery("sort_by", "updated_at"))
	sortOrder := strings.ToLower(strings.TrimSpace(c.DefaultQuery("sort_order", "desc")))
	timeRange, rangeErr := parseBusinessTimeRange(c)
	if rangeErr != nil {
		respondError(c, http.StatusBadRequest, rangeErr.Code, rangeErr)
		return
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	wecomUserID, err := GetWeComUserID(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	role, err := GetRole(c)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	dispatcherScopeRaw, dispatcherScopeProvided := c.GetQuery("dispatcher_scope")
	dispatcherScope := types.DispatcherLeadScope(strings.TrimSpace(dispatcherScopeRaw))
	if dispatcherScopeProvided && role != string(types.UserRoleDispatcher) {
		respondError(c, http.StatusBadRequest, "invalid_dispatcher_scope", types.ErrInvalidDispatcherScope)
		return
	}
	if role == string(types.UserRoleDispatcher) {
		if !dispatcherScopeProvided {
			dispatcherScope = types.DispatcherLeadScopeAll
		}
		if !types.IsValidDispatcherLeadScope(dispatcherScope) {
			respondError(c, http.StatusBadRequest, "invalid_dispatcher_scope", types.ErrInvalidDispatcherScope)
			return
		}
	}

	result, err := h.listLeadsUseCase.Execute(c.Request.Context(), &usecase.ListLeadsRequest{
		CurrentUser:     wecomUserID,
		Role:            role,
		Status:          status,
		Keyword:         keyword,
		SalesUserID:     salesUserID,
		SortBy:          sortBy,
		SortOrder:       sortOrder,
		DispatcherScope: dispatcherScope,
		StartTime:       timeRange.Start,
		EndTime:         timeRange.EndExclusive,
		Page:            page,
		PageSize:        pageSize,
	})

	if err != nil {
		respondUseCaseError(c, err, "list_leads_failed")
		return
	}

	RecordLeadRead(c, result.Leads...)
	c.JSON(http.StatusOK, result)
}

func parseLeadIDParam(c *gin.Context) (int64, bool) {
	leadID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || leadID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_lead_id"})
		return 0, false
	}
	return leadID, true
}
