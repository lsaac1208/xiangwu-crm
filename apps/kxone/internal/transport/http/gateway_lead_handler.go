package http

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
	"github.com/lsaac128/kxone/internal/usecase"
	"github.com/lsaac128/kxone/pkg/types"
)

const gatewayLeadSchemaVersion = 1

type XiaohongshuSignatureRequest struct {
	AccountName string `json:"account_name"`
	Signature   string `json:"signature"`
	RawBody     string `json:"raw_body_base64"`
}

func (h *AdLeadHandler) VerifyXiaohongshuSignature(c *gin.Context) {
	if h.gatewayHMACSecret == "" || h.channelIntegrations == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"ok": false, "error": "verification_unavailable"})
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil || !h.authorizeGateway(c, body) {
		c.JSON(http.StatusUnauthorized, gin.H{"ok": false, "error": "invalid_gateway_signature"})
		return
	}
	var req XiaohongshuSignatureRequest
	if err := json.Unmarshal(body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid_json"})
		return
	}
	req.AccountName = strings.TrimSpace(req.AccountName)
	integration, err := h.channelIntegrations.GetByPlatformAccount(c.Request.Context(), types.LeadPlatformXiaohongshu, req.AccountName)
	if err != nil {
		internalError(c, http.StatusInternalServerError, "channel_integration_lookup_failed", err)
		return
	}
	if integration == nil {
		c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "account_not_found"})
		return
	}
	if !integration.Enabled {
		c.JSON(http.StatusForbidden, gin.H{"ok": false, "error": "account_disabled"})
		return
	}
	if integration.WebhookToken == "" {
		c.JSON(http.StatusOK, gin.H{"ok": true, "configured": false, "valid": false})
		return
	}
	encodedSignature := strings.TrimPrefix(strings.TrimSpace(req.Signature), "sha1=")
	rawBody, err := base64.StdEncoding.DecodeString(req.RawBody)
	if err != nil || len(encodedSignature) != sha1.Size*2 {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid_signature_payload"})
		return
	}
	mac := hmac.New(sha1.New, []byte(integration.WebhookToken))
	_, _ = mac.Write(rawBody)
	valid := verifyHexSignature(encodedSignature, fmt.Sprintf("%x", mac.Sum(nil)))
	c.JSON(http.StatusOK, gin.H{"ok": true, "configured": true, "valid": valid})
}

type GatewayLeadRequest struct {
	SchemaVersion  int    `json:"schemaVersion"`
	Source         string `json:"source"`
	ExternalLeadID string `json:"externalLeadId"`
	DedupeKey      string `json:"dedupeKey"`
	AccountName    string `json:"accountName"`
	CustomerName   string `json:"customerName"`
	Phone          string `json:"phone"`
	Wechat         string `json:"wechat"`
	Region         string `json:"region"`
	IPLocation     string `json:"ipLocation"`
	CampaignName   string `json:"campaignName"`
	AdgroupName    string `json:"adgroupName"`
	CreativeName   string `json:"creativeName"`
	FormName       string `json:"formName"`
	PageName       string `json:"pageName"`
	PageURL        string `json:"pageUrl"`
	StatusOrTag    string `json:"statusOrTag"`
	LeadCreatedAt  string `json:"leadCreatedAt"`
}

func (h *AdLeadHandler) ReceiveGateway(c *gin.Context) {
	if h.gatewayHMACSecret == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"ok": false, "error": "gateway_ingestion_disabled"})
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		if IsRequestBodyTooLarge(err) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"ok": false, "error": "request_body_too_large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "read_body_failed"})
		return
	}
	if !h.authorizeGateway(c, body) {
		c.JSON(http.StatusUnauthorized, gin.H{"ok": false, "error": "invalid_gateway_signature"})
		return
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	var request GatewayLeadRequest
	if err := decoder.Decode(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid_json"})
		return
	}
	request.normalize()
	if code := request.validate(); code != "" {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": code})
		return
	}
	integration, allowed, err := h.resolveChannelIntegration(c.Request.Context(), request.Source, request.AccountName)
	if err != nil {
		respondAdLeadError(c, http.StatusInternalServerError, "channel_integration_lookup_failed", err)
		return
	}
	if !allowed {
		h.recordChannelReceive(c.Request.Context(), integration, "rejected_disabled", nil, "channel account is disabled")
		c.JSON(http.StatusUnprocessableEntity, gin.H{"ok": false, "error": "channel_account_disabled_or_unknown"})
		return
	}

	leadNo := request.leadNo()
	if existing, err := h.leadRepo.GetByLeadNo(c.Request.Context(), leadNo); err != nil {
		respondAdLeadError(c, http.StatusInternalServerError, "query_duplicate_failed", err)
		return
	} else if existing != nil {
		h.recordChannelReceive(c.Request.Context(), integration, "duplicate", &existing.ID, "")
		c.JSON(http.StatusOK, gin.H{"ok": true, "duplicated": true, "lead_id": existing.ID, "lead_no": existing.LeadNo})
		return
	}

	result, err := h.createUseCase.Execute(c.Request.Context(), &usecase.CreateLeadRequest{
		LeadNo:                leadNo,
		CustomerName:          firstNonEmpty(request.CustomerName, "未留姓名"),
		CustomerMobile:        request.Phone,
		CustomerWechat:        request.Wechat,
		City:                  firstNonEmpty(request.Region, request.IPLocation),
		ProductName:           firstNonEmpty(request.FormName, request.PageName, gatewaySourceName(request.Source)+"线索"),
		SourceChannel:         sourceChannel(request.Source, request.AccountName),
		SourceCampaign:        request.campaignSummary(),
		LeadLevel:             "high",
		DispatchManagerUserID: h.defaultManagerUserID,
	})
	if err != nil {
		// A concurrent delivery may win after the preflight lookup.
		if existing, lookupErr := h.leadRepo.GetByLeadNo(c.Request.Context(), leadNo); lookupErr == nil && existing != nil {
			h.recordChannelReceive(c.Request.Context(), integration, "duplicate", &existing.ID, "")
			c.JSON(http.StatusOK, gin.H{"ok": true, "duplicated": true, "lead_id": existing.ID, "lead_no": existing.LeadNo})
			return
		}
		h.recordChannelReceive(c.Request.Context(), integration, "failed", nil, "create_lead_failed")
		respondAdLeadError(c, http.StatusInternalServerError, "create_lead_failed", err)
		return
	}
	h.recordChannelReceive(c.Request.Context(), integration, "accepted", &result.Lead.ID, "")

	if h.groupRobotNotifier != nil {
		notification := wecom.GroupRobotLead{
			LeadID: result.Lead.ID, BusinessVersion: result.Lead.Version,
			Source: request.Source, AccountName: request.AccountName, CustomerName: request.CustomerName,
			Phone: request.Phone, Wechat: request.Wechat, Region: firstNonEmpty(request.Region, request.IPLocation),
			FormName: request.FormName, StatusOrTag: request.StatusOrTag, CampaignName: request.CampaignName,
			AdgroupName: request.AdgroupName, CreativeName: request.CreativeName, LeadCreatedAt: request.LeadCreatedAt,
		}
		if err := h.groupRobotNotifier.NotifyLead(c.Request.Context(), notification); err != nil {
			slog.Warn("group robot lead notification failed", "lead_id", result.Lead.ID, "source", request.Source, "err", err)
		}
	}

	c.JSON(http.StatusCreated, gin.H{
		"ok":         true,
		"duplicated": false,
		"lead_id":    result.Lead.ID,
		"lead_no":    result.Lead.LeadNo,
	})
}

func (h *AdLeadHandler) resolveChannelIntegration(ctx context.Context, source, accountName string) (*types.ChannelIntegration, bool, error) {
	if h.channelIntegrations == nil || !types.IsLeadPlatform(source) {
		return nil, true, nil
	}
	count, err := h.channelIntegrations.CountByPlatform(ctx, source)
	if err != nil {
		return nil, false, err
	}
	if count == 0 {
		return nil, true, nil
	}
	integration, err := h.channelIntegrations.GetByPlatformAccount(ctx, source, accountName)
	if err != nil {
		return nil, false, err
	}
	if integration == nil || !integration.Enabled {
		return integration, false, nil
	}
	return integration, true, nil
}

func (h *AdLeadHandler) recordChannelReceive(ctx context.Context, integration *types.ChannelIntegration, status string, leadID *int64, lastError string) {
	if h.channelIntegrations == nil || integration == nil {
		return
	}
	if err := h.channelIntegrations.RecordReceive(ctx, integration.ID, status, leadID, lastError); err != nil {
		slog.Warn("record channel receive status failed", "integration_id", integration.ID, "err", err)
	}
}

func (h *AdLeadHandler) authorizeGateway(c *gin.Context, body []byte) bool {
	timestamp := strings.TrimSpace(c.GetHeader("X-KXOne-Gateway-Timestamp"))
	signature := strings.TrimSpace(c.GetHeader("X-KXOne-Gateway-Signature"))
	if timestamp == "" || signature == "" || !validWebhookTimestamp(timestamp, h.gatewayTolerance) {
		return false
	}
	payload := append([]byte(timestamp), '.')
	payload = append(payload, body...)
	return verifyHexSignature(signature, hmacHexSHA256([]byte(h.gatewayHMACSecret), payload))
}

func (r *GatewayLeadRequest) normalize() {
	r.Source = strings.ToLower(strings.TrimSpace(r.Source))
	r.ExternalLeadID = strings.TrimSpace(r.ExternalLeadID)
	r.DedupeKey = strings.TrimSpace(r.DedupeKey)
	r.AccountName = strings.TrimSpace(r.AccountName)
	r.CustomerName = strings.TrimSpace(r.CustomerName)
	r.Phone = strings.TrimSpace(r.Phone)
	r.Wechat = strings.TrimSpace(r.Wechat)
	r.Region = strings.TrimSpace(r.Region)
	r.IPLocation = strings.TrimSpace(r.IPLocation)
	r.CampaignName = strings.TrimSpace(r.CampaignName)
	r.AdgroupName = strings.TrimSpace(r.AdgroupName)
	r.CreativeName = strings.TrimSpace(r.CreativeName)
	r.FormName = strings.TrimSpace(r.FormName)
	r.PageName = strings.TrimSpace(r.PageName)
	r.PageURL = strings.TrimSpace(r.PageURL)
	r.StatusOrTag = strings.TrimSpace(r.StatusOrTag)
	r.LeadCreatedAt = strings.TrimSpace(r.LeadCreatedAt)
}

func (r GatewayLeadRequest) validate() string {
	if r.SchemaVersion != gatewayLeadSchemaVersion {
		return "unsupported_schema_version"
	}
	switch r.Source {
	case "baidu", "tencent", "xiaohongshu", "douyin", "form", "manual":
	default:
		return "invalid_source"
	}
	if r.ExternalLeadID == "" && r.DedupeKey == "" {
		return "missing_lead_identity"
	}
	if r.Phone == "" && r.Wechat == "" {
		return "missing_contact"
	}
	if len(r.ExternalLeadID) > 255 || len(r.DedupeKey) > 255 || len(r.CustomerName) > 255 || len(r.Phone) > 64 || len(r.Wechat) > 255 {
		return "field_too_long"
	}
	return ""
}

func (r GatewayLeadRequest) leadNo() string {
	identity := r.ExternalLeadID
	if identity == "" {
		identity = strings.TrimPrefix(r.DedupeKey, r.Source+":")
	}
	return r.Source + ":" + identity
}

func (r GatewayLeadRequest) campaignSummary() string {
	values := []string{r.StatusOrTag, r.CampaignName, r.AdgroupName, r.CreativeName}
	if r.Source == types.LeadPlatformTencent {
		values = []string{
			campaignSummaryField("状态", r.StatusOrTag),
			campaignSummaryField("计划", r.CampaignName),
			campaignSummaryField("广告组", r.AdgroupName),
			campaignSummaryField("创意", r.CreativeName),
		}
	}
	if r.PageURL != "" {
		values = append(values, "页面:"+r.PageURL)
	}
	if r.LeadCreatedAt != "" {
		values = append(values, "留资时间:"+formatGatewayLeadCreatedAt(r.LeadCreatedAt))
	}
	return strings.Join(uniqueNonEmpty(values...), " / ")
}

func campaignSummaryField(label, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return label + ":" + value
}

func formatGatewayLeadCreatedAt(value string) string {
	value = strings.TrimSpace(value)
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	chinaStandardTime := time.FixedZone("UTC+8", 8*60*60)
	return parsed.In(chinaStandardTime).Format("2006-01-02 15:04:05")
}

func gatewaySourceName(source string) string {
	names := map[string]string{
		"baidu":       "百度广告",
		"tencent":     "腾讯广告",
		"xiaohongshu": "小红书广告",
		"douyin":      "抖音广告",
		"form":        "表单",
		"manual":      "手工录入",
	}
	return firstNonEmpty(names[source], fmt.Sprintf("%s来源", source))
}
