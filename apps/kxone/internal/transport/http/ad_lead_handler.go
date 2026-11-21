package http

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
	"github.com/lsaac128/kxone/internal/usecase"
	"github.com/lsaac128/kxone/pkg/types"
)

type AdLeadHandler struct {
	createUseCase        *usecase.CreateLeadUseCase
	leadRepo             repo.LeadRepository
	defaultManagerUserID string
	webhookSecret        string
	signatureTolerance   time.Duration
	trustedGatewayIPs    []netip.Prefix
	publicFormOrigins    map[string]struct{}
	gatewayHMACSecret    string
	gatewayTolerance     time.Duration
	groupRobotNotifier   wecom.GroupRobotNotifier
	channelIntegrations  repo.ChannelIntegrationRepository
	xiangwuFormKey       string
	xiangwuFormRepo      repo.XiangwuFormSubmissionRepository
}

func (h *AdLeadHandler) WithXiangwuForms(key string, repository repo.XiangwuFormSubmissionRepository) *AdLeadHandler {
	h.xiangwuFormKey = strings.TrimSpace(key)
	h.xiangwuFormRepo = repository
	return h
}

func (h *AdLeadHandler) WithChannelIntegrations(repository repo.ChannelIntegrationRepository) *AdLeadHandler {
	h.channelIntegrations = repository
	return h
}

func (h *AdLeadHandler) WithGroupRobotNotifier(notifier wecom.GroupRobotNotifier) *AdLeadHandler {
	h.groupRobotNotifier = notifier
	return h
}

func (h *AdLeadHandler) WithPublicFormOrigins(values ...string) *AdLeadHandler {
	h.publicFormOrigins = map[string]struct{}{}
	for _, value := range values {
		if origin := normalizePublicFormOrigin(value); origin != "" {
			h.publicFormOrigins[origin] = struct{}{}
		}
	}
	return h
}

func NewAdLeadHandler(createUseCase *usecase.CreateLeadUseCase, leadRepo repo.LeadRepository, defaultManagerUserID string) *AdLeadHandler {
	defaultManagerUserID = strings.TrimSpace(defaultManagerUserID)
	return &AdLeadHandler{
		createUseCase:        createUseCase,
		leadRepo:             leadRepo,
		defaultManagerUserID: defaultManagerUserID,
	}
}

func (h *AdLeadHandler) WithWebhookSecret(secret string, tolerance time.Duration) *AdLeadHandler {
	h.webhookSecret = strings.TrimSpace(secret)
	if tolerance <= 0 {
		tolerance = 5 * time.Minute
	}
	h.signatureTolerance = tolerance
	return h
}

func (h *AdLeadHandler) WithTrustedGatewayIPs(values []string) *AdLeadHandler {
	h.trustedGatewayIPs = parseTrustedGatewayIPs(values)
	return h
}

func (h *AdLeadHandler) WithGatewayHMAC(secret string, tolerance time.Duration) *AdLeadHandler {
	h.gatewayHMACSecret = strings.TrimSpace(secret)
	if tolerance <= 0 {
		tolerance = 5 * time.Minute
	}
	h.gatewayTolerance = tolerance
	return h
}

func (h *AdLeadHandler) ReceiveBaidu(c *gin.Context) {
	h.receive(c, "baidu", normalizeBaiduAdLead)
}

func (h *AdLeadHandler) ReceiveTencent(c *gin.Context) {
	h.receive(c, "tencent", normalizeTencentAdLead)
}

func (h *AdLeadHandler) ReceiveXiaohongshu(c *gin.Context) {
	h.receive(c, "xiaohongshu", normalizeXiaohongshuAdLead)
}

func (h *AdLeadHandler) ReceiveDouyin(c *gin.Context) {
	h.receive(c, "douyin", normalizeDouyinAdLead)
}

func (h *AdLeadHandler) ReceiveForm(c *gin.Context) {
	h.receive(c, "form", normalizeFormLead)
}

func (h *AdLeadHandler) ReceivePublicForm(c *gin.Context) {
	if !h.allowPublicFormOrigin(c) {
		c.JSON(http.StatusForbidden, gin.H{"ok": false, "error": "origin_not_allowed"})
		return
	}
	if h.defaultManagerUserID == "" {
		respondAdLeadError(c, http.StatusServiceUnavailable, "service_unavailable", fmt.Errorf("AD_LEAD_DEFAULT_MANAGER_USERID is not configured"))
		return
	}
	h.receiveWithoutWebhookAuth(c, "website_form", normalizeFormLead)
}

func (h *AdLeadHandler) ReceiveManual(c *gin.Context) {
	h.receive(c, "manual", normalizeManualLead)
}

func (h *AdLeadHandler) receive(c *gin.Context, source string, normalize func(map[string]any, string) normalizedAdLead) {
	h.receiveRequest(c, source, normalize, true)
}

func (h *AdLeadHandler) receiveWithoutWebhookAuth(c *gin.Context, source string, normalize func(map[string]any, string) normalizedAdLead) {
	h.receiveRequest(c, source, normalize, false)
}

func (h *AdLeadHandler) receiveRequest(c *gin.Context, source string, normalize func(map[string]any, string) normalizedAdLead, requireWebhookAuth bool) {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		if IsRequestBodyTooLarge(err) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"ok": false, "error": "request_body_too_large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "read_body_failed"})
		return
	}
	accountName := normalizeAccountName(c.Query("account"))
	var integration *types.ChannelIntegration
	if requireWebhookAuth {
		var managed bool
		var authStatus int
		var authCode string
		integration, managed, authStatus, authCode, err = h.authorizeDirectPlatform(c, bodyBytes, source, accountName)
		if err != nil {
			respondAdLeadError(c, http.StatusInternalServerError, "channel_integration_lookup_failed", err)
			return
		}
		if managed && authCode != "" {
			receiveStatus := "failed"
			if authCode == "channel_account_disabled" {
				receiveStatus = "rejected_disabled"
			}
			h.recordChannelReceive(c.Request.Context(), integration, receiveStatus, nil, authCode)
			c.JSON(authStatus, gin.H{"ok": false, "error": authCode})
			return
		}
		if !managed && !h.authorizeWebhook(c, bodyBytes) {
			c.JSON(http.StatusUnauthorized, gin.H{"ok": false, "error": "invalid_signature"})
			return
		}
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	var raw map[string]any
	if err := c.ShouldBindJSON(&raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"ok": false, "error": "invalid_json"})
		return
	}
	if !requireWebhookAuth && strings.TrimSpace(firstString(raw, "website", "company_website")) != "" {
		c.JSON(http.StatusOK, gin.H{"ok": true, "invalid": true})
		return
	}

	lead := normalize(raw, accountName)
	if !hasContact(lead) {
		h.recordChannelReceive(c.Request.Context(), integration, "failed", nil, "missing_contact")
		c.JSON(http.StatusUnprocessableEntity, gin.H{"ok": false, "error": "invalid_lead_profile"})
		return
	}
	if lead.ExternalLeadID == "" {
		lead.ExternalLeadID = hashPayload(raw)
	}
	if lead.CustomerName == "" {
		lead.CustomerName = "未留姓名"
	}

	leadNo := fmt.Sprintf("%s:%s", source, lead.ExternalLeadID)
	if existing, err := h.leadRepo.GetByLeadNo(c.Request.Context(), leadNo); err != nil {
		h.recordChannelReceive(c.Request.Context(), integration, "failed", nil, "query_duplicate_failed")
		respondAdLeadError(c, http.StatusInternalServerError, "query_duplicate_failed", err)
		return
	} else if existing != nil {
		h.recordChannelReceive(c.Request.Context(), integration, "duplicate", &existing.ID, "")
		c.JSON(http.StatusOK, gin.H{"ok": true, "code": 0, "message": "success", "duplicated": true, "lead_id": existing.ID})
		return
	}

	dispatchManagerUserID := h.defaultManagerUserID
	if requireWebhookAuth {
		dispatchManagerUserID = h.dispatchManagerUserID(c, raw)
	}

	result, err := h.createUseCase.Execute(c.Request.Context(), &usecase.CreateLeadRequest{
		LeadNo:                leadNo,
		CustomerName:          lead.CustomerName,
		CustomerMobile:        lead.Phone,
		CustomerWechat:        lead.Wechat,
		City:                  lead.Region,
		ProductName:           lead.ProductName,
		SourceChannel:         lead.SourceChannel,
		SourceCampaign:        lead.SourceCampaign,
		ContentEntryID:        lead.ContentEntryID,
		ContentRevisionID:     lead.ContentRevisionID,
		ContentPath:           lead.ContentPath,
		LeadLevel:             "high",
		DispatchManagerUserID: dispatchManagerUserID,
	})
	if err != nil {
		h.recordChannelReceive(c.Request.Context(), integration, "failed", nil, "create_lead_failed")
		if errors.Is(err, types.ErrInvalidLeadProfile) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"ok": false, "error": "invalid_lead_profile"})
			return
		}
		respondAdLeadError(c, http.StatusBadRequest, "create_lead_failed", err)
		return
	}
	h.recordChannelReceive(c.Request.Context(), integration, "accepted", &result.Lead.ID, "")
	if h.groupRobotNotifier != nil {
		notification := wecom.GroupRobotLead{
			LeadID: result.Lead.ID, BusinessVersion: result.Lead.Version,
			Source: source, AccountName: accountName, CustomerName: lead.CustomerName,
			Phone: lead.Phone, Wechat: lead.Wechat, Region: lead.Region,
			FormName: lead.ProductName, CampaignName: lead.SourceCampaign,
		}
		if err := h.groupRobotNotifier.NotifyLead(c.Request.Context(), notification); err != nil {
			log.Printf("企业微信群机器人新线索通知失败: lead_id=%d source=%s err=%v", result.Lead.ID, source, err)
		}
	}

	c.JSON(http.StatusCreated, gin.H{
		"ok":         true,
		"code":       0,
		"message":    "success",
		"lead_id":    result.Lead.ID,
		"lead_no":    result.Lead.LeadNo,
		"duplicated": false,
	})
}

func (h *AdLeadHandler) authorizeDirectPlatform(c *gin.Context, body []byte, source, accountName string) (*types.ChannelIntegration, bool, int, string, error) {
	if h.channelIntegrations == nil || !types.IsLeadPlatform(source) {
		return nil, false, 0, "", nil
	}
	count, err := h.channelIntegrations.CountByPlatform(c.Request.Context(), source)
	if err != nil {
		return nil, true, 0, "", err
	}
	if count == 0 {
		return nil, false, 0, "", nil
	}
	if accountName == "" {
		return nil, true, http.StatusBadRequest, "missing_account", nil
	}
	integration, err := h.channelIntegrations.GetByPlatformAccount(c.Request.Context(), source, accountName)
	if err != nil {
		return nil, true, 0, "", err
	}
	if integration == nil {
		return nil, true, http.StatusNotFound, "channel_account_not_found", nil
	}
	if !integration.Enabled {
		return integration, true, http.StatusForbidden, "channel_account_disabled", nil
	}
	switch integration.AuthMode {
	case "platform_compatible":
		if source == types.LeadPlatformBaidu || source == types.LeadPlatformTencent || source == types.LeadPlatformDouyin {
			return integration, true, 0, "", nil
		}
	case "hmac_sha1":
		if integration.WebhookToken == "" {
			return integration, true, http.StatusServiceUnavailable, "channel_token_not_configured", nil
		}
		signature := strings.TrimPrefix(strings.TrimSpace(c.GetHeader("X-Red-Signature")), "sha1=")
		if len(signature) != sha1.Size*2 {
			return integration, true, http.StatusUnauthorized, "invalid_platform_signature", nil
		}
		mac := hmac.New(sha1.New, []byte(integration.WebhookToken))
		_, _ = mac.Write(body)
		if verifyHexSignature(signature, hex.EncodeToString(mac.Sum(nil))) {
			return integration, true, 0, "", nil
		}
		return integration, true, http.StatusUnauthorized, "invalid_platform_signature", nil
	case "feiyu_signature":
		if integration.WebhookToken == "" {
			return integration, true, http.StatusServiceUnavailable, "channel_token_not_configured", nil
		}
		if verifyFeiyuSignature(c, body, integration.WebhookToken) {
			return integration, true, 0, "", nil
		}
		return integration, true, http.StatusUnauthorized, "invalid_platform_signature", nil
	}
	return integration, true, http.StatusUnauthorized, "unsupported_channel_auth_mode", nil
}

func (h *AdLeadHandler) allowPublicFormOrigin(c *gin.Context) bool {
	origin := normalizePublicFormOrigin(c.GetHeader("Origin"))
	if origin == "" {
		origin = normalizePublicFormOrigin(c.GetHeader("Referer"))
	}
	if origin == "" || len(h.publicFormOrigins) == 0 {
		return false
	}
	_, allowed := h.publicFormOrigins[origin]
	return allowed
}

func normalizePublicFormOrigin(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Scheme + "://" + parsed.Host)
}

func respondAdLeadError(c *gin.Context, status int, code string, err error) {
	logHTTPError(c, code, err)
	c.JSON(status, gin.H{"ok": false, "error": code})
}

func (h *AdLeadHandler) authorizeWebhook(c *gin.Context, body []byte) bool {
	if h.webhookSecret == "" {
		return true
	}
	if h.isTrustedGatewayForward(c) {
		return true
	}

	for _, candidate := range []string{
		bearerToken(c.GetHeader("Authorization")),
		c.GetHeader("X-KXOne-Webhook-Secret"),
		c.Query("secret"),
	} {
		if constantTimeStringEqual(candidate, h.webhookSecret) {
			return true
		}
	}

	signature := firstNonEmpty(c.GetHeader("X-KXOne-Signature"), c.Query("signature"))
	timestamp := firstNonEmpty(c.GetHeader("X-KXOne-Timestamp"), c.Query("timestamp"))
	if signature == "" || timestamp == "" || !validWebhookTimestamp(timestamp, h.signatureTolerance) {
		return false
	}

	payload := append([]byte(timestamp), '.')
	payload = append(payload, body...)
	expected := hmacHexSHA256([]byte(h.webhookSecret), payload)
	return verifyHexSignature(signature, expected)
}

func (h *AdLeadHandler) isTrustedGatewayForward(c *gin.Context) bool {
	if strings.TrimSpace(c.GetHeader("X-KXOne-Manager")) == "" || len(h.trustedGatewayIPs) == 0 {
		return false
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(c.ClientIP()))
	if err != nil {
		return false
	}
	for _, prefix := range h.trustedGatewayIPs {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func parseTrustedGatewayIPs(values []string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(value); err == nil {
			prefixes = append(prefixes, prefix)
			continue
		}
		addr, err := netip.ParseAddr(value)
		if err != nil {
			log.Printf("invalid AD_LEAD_TRUSTED_GATEWAY_IPS entry %q: %v", value, err)
			continue
		}
		prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return prefixes
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if len(header) > 7 && strings.EqualFold(header[:7], "Bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return ""
}

func validWebhookTimestamp(raw string, tolerance time.Duration) bool {
	unixSeconds, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return false
	}
	if tolerance <= 0 {
		tolerance = 5 * time.Minute
	}
	ts := time.Unix(unixSeconds, 0)
	return time.Since(ts) <= tolerance && time.Until(ts) <= tolerance
}

func hmacHexSHA256(secret, payload []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyHexSignature(signature, expected string) bool {
	signature = strings.TrimSpace(signature)
	signature = strings.TrimPrefix(signature, "sha256=")
	if len(signature) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(signature)), []byte(expected)) == 1
}

func constantTimeStringEqual(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" || len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

type normalizedAdLead struct {
	ExternalLeadID    string
	CustomerName      string
	Phone             string
	Wechat            string
	Region            string
	ProductName       string
	SourceChannel     string
	SourceCampaign    string
	ContentEntryID    *int64
	ContentRevisionID *int64
	ContentPath       string
}

func normalizeBaiduAdLead(raw map[string]any, accountName string) normalizedAdLead {
	phone := firstString(raw, "cluePhoneNumber", "phone", "mobile", "tel")
	createdAt := firstString(raw, "commitTime", "createdAt", "create_time")
	externalID := firstString(raw, "clueId", "id", "lead_id")
	if externalID == "" {
		externalID = shortHash(phone + ":" + createdAt)
	}
	campaign := adCampaignSummary(
		firstString(raw, "connectName", "statusOrTag", "status_or_tag"),
		firstString(raw, "campaignName", "campaign_name"),
		firstString(raw, "adgroupName", "adgroup_name"),
		firstString(raw, "creativeName", "creative_name"),
	)
	return normalizedAdLead{
		ExternalLeadID: externalID,
		CustomerName:   firstString(raw, "name", "customerName", "customer_name", "clueName"),
		Phone:          phone,
		Wechat:         firstString(raw, "wechatAccount", "wechat", "weixin"),
		Region:         firstString(raw, "region", "city", "area"),
		ProductName:    firstNonEmpty(firstString(raw, "formName", "pageName", "product_name"), "百度广告线索"),
		SourceChannel:  sourceChannel("baidu", accountName),
		SourceCampaign: campaign,
	}
}

func normalizeTencentAdLead(raw map[string]any, accountName string) normalizedAdLead {
	formName := parseBundleFormName(firstString(raw, "bundle"))
	if formName == "" {
		formName = firstString(raw, "component_name", "form_name")
	}
	campaign := adCampaignSummary(
		firstString(raw, "leads_tags", "leads_follow_tag", "status_or_tag"),
		firstString(raw, "adgroup_name", "adgroupName"),
		firstString(raw, "campaign_name", "campaignName"),
		firstString(raw, "creative_name", "adcreative_name", "ad_name"),
	)
	return normalizedAdLead{
		ExternalLeadID: firstString(raw, "leads_id", "id", "lead_id"),
		CustomerName:   firstString(raw, "leads_name", "name", "customer_name"),
		Phone:          firstString(raw, "leads_tel", "phone", "mobile", "tel"),
		Wechat:         firstString(raw, "leads_wechat", "wechat", "weixin"),
		Region:         firstString(raw, "tel_location", "region", "city", "area"),
		ProductName:    firstNonEmpty(formName, "腾讯广告线索"),
		SourceChannel:  sourceChannel("tencent", firstNonEmpty(accountName, firstString(raw, "agency_name"))),
		SourceCampaign: campaign,
	}
}

func normalizeXiaohongshuAdLead(raw map[string]any, accountName string) normalizedAdLead {
	data, _ := raw["data"].(map[string]any)
	if data == nil {
		data = raw
	}
	campaign := adCampaignSummary(
		firstString(data, "leads_tag", "status_or_tag"),
		firstString(data, "campaign_name", "campaignName"),
		firstString(data, "unit_name", "adgroup_name"),
		firstString(data, "creativity_name", "creative_name"),
	)
	return normalizedAdLead{
		ExternalLeadID: firstString(data, "id", "lead_id"),
		CustomerName:   firstString(data, "nick_name", "name", "customer_name"),
		Phone:          firstString(data, "phone_num", "phone", "mobile", "tel"),
		Wechat:         firstString(data, "wechat", "weixin"),
		Region:         firstString(data, "area", "region", "city"),
		ProductName:    firstNonEmpty(firstString(data, "source", "note_title", "form_name"), "小红书广告线索"),
		SourceChannel:  sourceChannel("xiaohongshu", firstNonEmpty(accountName, firstString(data, "ad_account"))),
		SourceCampaign: campaign,
	}
}

func normalizeDouyinAdLead(raw map[string]any, accountName string) normalizedAdLead {
	data := flattenFeiyuPayload(raw)
	campaign := adCampaignSummary(
		firstString(data, "app_name", "appname", "leads_tag", "status_or_tag", "clue_state_name"),
		firstString(data, "ad_name", "ad_plan_name", "campaign_name", "campaignName"),
		firstString(data, "promotion_name", "unit_name", "adgroup_name", "adgroupName"),
		firstString(data, "creative_name", "creativeName", "site_name", "dealer_name"),
		firstString(data, "remark"),
		firstString(data, "form_remark"),
		formatFeiyuRemarkDict(data["remark_dict"]),
	)
	return normalizedAdLead{
		ExternalLeadID: firstString(data, "id", "clue_id", "clueId", "lead_id", "external_lead_id", "externalLeadId", "req_id"),
		CustomerName:   firstString(data, "name", "customer_name", "customerName", "clue_name", "clueName", "nick_name"),
		Phone:          firstString(data, "telphone", "telephone", "phone", "mobile", "tel", "phone_num", "cluePhoneNumber"),
		Wechat:         firstString(data, "weixin", "wechat", "wechatAccount", "qq"),
		Region:         feiyuRegion(data),
		ProductName:    firstNonEmpty(firstString(data, "module_name", "site_name", "form_remark", "form_name", "formName", "page_name", "pageName", "product_name"), "抖音广告线索"),
		SourceChannel:  sourceChannel("douyin", firstNonEmpty(accountName, firstString(data, "adv_name", "advertiser_name", "account_name", "accountName", "ad_account"))),
		SourceCampaign: campaign,
	}
}

func feiyuRegion(data map[string]any) string {
	province := firstString(data, "province_name")
	city := firstString(data, "city_name", "city")
	if province != "" && city != "" && province != city && !strings.HasPrefix(city, province) {
		return province + city
	}
	if city != "" {
		return city
	}
	return firstString(data, "location", "province_name", "region", "area", "country_name", "address")
}

func flattenFeiyuPayload(raw map[string]any) map[string]any {
	merged := make(map[string]any, len(raw)+8)
	mergeContext := func(container map[string]any) {
		ctxMap, ok := container["context"].(map[string]any)
		if !ok {
			return
		}
		for _, section := range []string{"dealer_info", "ad_info", "basic_info"} {
			if sub, ok := ctxMap[section].(map[string]any); ok {
				for k, v := range sub {
					merged[k] = v
				}
			}
		}
	}
	mergeContext(raw)
	if dataMap, ok := raw["data"].(map[string]any); ok {
		mergeContext(dataMap)
		for k, v := range dataMap {
			merged[k] = v
		}
	}
	for k, v := range raw {
		merged[k] = v
	}
	return merged
}

func formatFeiyuRemarkDict(value any) string {
	var dict map[string]any
	switch v := value.(type) {
	case map[string]any:
		dict = v
	case string:
		trimmed := strings.TrimSpace(v)
		if strings.HasPrefix(trimmed, "{") {
			_ = json.Unmarshal([]byte(trimmed), &dict)
		} else {
			return normalizeAdString(trimmed)
		}
	}
	if len(dict) == 0 {
		return ""
	}
	keys := make([]string, 0, len(dict))
	for k := range dict {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		val := firstString(dict, k)
		if val != "" {
			parts = append(parts, k+":"+val)
		}
	}
	return strings.Join(parts, " / ")
}

func verifyFeiyuSignature(c *gin.Context, body []byte, secret string) bool {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return false
	}
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)
	flat := flattenFeiyuPayload(raw)

	signature := firstNonEmpty(
		c.GetHeader("Signature"),
		c.GetHeader("X-Feiyu-Signature"),
		firstString(flat, "signature"),
	)
	timestamp := firstNonEmpty(
		c.GetHeader("Timestamp"),
		c.GetHeader("X-Feiyu-Timestamp"),
		firstString(flat, "timestamp"),
	)
	accessToken := firstNonEmpty(
		c.GetHeader("Access-Token"),
		c.GetHeader("X-Feiyu-Token"),
		firstString(flat, "token", "access_token"),
	)
	if signature == "" || timestamp == "" {
		return false
	}

	clueIDs := uniqueNonEmpty(
		firstString(raw, "clue_id", "id", "clueId", "lead_id", "external_lead_id", "externalLeadId"),
		firstString(flat, "id", "clue_id", "clueId", "lead_id", "external_lead_id", "externalLeadId"),
	)
	if len(clueIDs) == 0 {
		clueIDs = []string{""}
	}

	tokens := uniqueNonEmpty(accessToken)
	if len(tokens) == 0 {
		tokens = []string{""}
	}

	for _, clueID := range clueIDs {
		for _, tok := range tokens {
			if verifyFeiyuMessageSignature(signature, secret, clueID+"|"+tok+"|"+timestamp) {
				return true
			}
		}
	}
	return false
}

func verifyFeiyuMessageSignature(signature, secret, message string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	sum := mac.Sum(nil)
	hexDigest := hex.EncodeToString(sum)
	for _, expected := range []string{
		base64.StdEncoding.EncodeToString(sum),
		base64.StdEncoding.EncodeToString([]byte(hexDigest)),
		hexDigest,
	} {
		if constantTimeStringEqual(signature, expected) {
			return true
		}
	}
	return false
}

func normalizeFormLead(raw map[string]any, accountName string) normalizedAdLead {
	phone := firstString(raw, "phone", "mobile", "tel")
	name := firstString(raw, "studentName", "student_name", "name", "customerName", "customer_name")
	grade := firstString(raw, "grade", "statusOrTag", "status_or_tag")
	message := firstString(raw, "message", "remark", "formName", "form_name")
	contentPath := firstString(raw, "contentPath", "content_path", "source")
	if !strings.HasPrefix(contentPath, "/") || strings.ContainsAny(contentPath, "?#\\") {
		contentPath = ""
	}
	return normalizedAdLead{
		ExternalLeadID:    firstNonEmpty(firstString(raw, "externalLeadId", "external_lead_id", "id", "lead_id"), shortHash("form:"+phone+":"+name+":"+grade+":"+message)),
		CustomerName:      name,
		Phone:             phone,
		Wechat:            firstString(raw, "wechat", "weixin"),
		Region:            firstString(raw, "region", "city", "area"),
		ProductName:       firstNonEmpty(firstString(raw, "formName", "form_name"), "官网表单线索"),
		SourceChannel:     sourceChannel("form", firstNonEmpty(accountName, firstString(raw, "accountName", "account_name"), "公众号表单")),
		SourceCampaign:    adCampaignSummary(grade, message),
		ContentEntryID:    optionalPositiveInt64(raw, "contentEntryId", "content_entry_id"),
		ContentRevisionID: optionalPositiveInt64(raw, "contentRevisionId", "content_revision_id"),
		ContentPath:       contentPath,
	}
}

func optionalPositiveInt64(raw map[string]any, keys ...string) *int64 {
	value, err := strconv.ParseInt(firstString(raw, keys...), 10, 64)
	if err != nil || value <= 0 {
		return nil
	}
	return &value
}

func normalizeManualLead(raw map[string]any, accountName string) normalizedAdLead {
	channel := firstNonEmpty(firstString(raw, "channelPlatform", "channel_platform", "accountName", "account_name"), accountName)
	contact := firstString(raw, "contact", "wechat", "weixin", "phone", "mobile", "tel")
	phone := firstNonEmpty(firstString(raw, "phone", "mobile", "tel"), extractPhone(contact))
	remark := firstString(raw, "remark", "message", "formName", "form_name")
	return normalizedAdLead{
		ExternalLeadID: firstNonEmpty(firstString(raw, "externalLeadId", "external_lead_id", "id", "lead_id"), shortHash("manual:"+channel+":"+contact+":"+remark)),
		CustomerName:   firstString(raw, "name", "customerName", "customer_name"),
		Phone:          phone,
		Wechat:         contact,
		Region:         firstString(raw, "region", "city", "area"),
		ProductName:    "手动录入线索",
		SourceChannel:  sourceChannel("manual", channel),
		SourceCampaign: remark,
	}
}

func (h *AdLeadHandler) dispatchManagerUserID(c *gin.Context, raw map[string]any) string {
	for _, value := range []string{
		c.Query("manager_userid"),
		c.Query("manager"),
		c.GetHeader("X-KXOne-Manager"),
		firstString(raw, "dispatch_manager_userid", "manager_userid", "manager"),
		h.defaultManagerUserID,
	} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return h.defaultManagerUserID
}

func hasContact(lead normalizedAdLead) bool {
	return strings.TrimSpace(lead.Phone) != "" || strings.TrimSpace(lead.Wechat) != ""
}

func sourceChannel(source, accountName string) string {
	prefix := source
	if types.IsLeadPlatform(source) {
		prefix = "ad:" + source
	}
	if strings.TrimSpace(accountName) == "" {
		return prefix
	}
	return prefix + ":" + strings.TrimSpace(accountName)
}

func normalizeAccountName(value string) string {
	return strings.TrimSpace(value)
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case string:
			if normalized := normalizeAdString(v); normalized != "" {
				return normalized
			}
		case json.Number:
			return v.String()
		case float64:
			if v == float64(int64(v)) {
				return fmt.Sprintf("%d", int64(v))
			}
			return fmt.Sprintf("%v", v)
		case int:
			return fmt.Sprintf("%d", v)
		case int64:
			return fmt.Sprintf("%d", v)
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if normalized := normalizeAdString(value); normalized != "" {
			return normalized
		}
	}
	return ""
}

func normalizeAdString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "-" || value == "--" || value == "null" || value == "NULL" || value == "未知" {
		return ""
	}
	return value
}

var phonePattern = regexp.MustCompile(`1[3-9]\d{9}`)

func extractPhone(value string) string {
	return phonePattern.FindString(value)
}

func adCampaignSummary(values ...string) string {
	return strings.Join(uniqueNonEmpty(values...), " / ")
}

func uniqueNonEmpty(values ...string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func parseBundleFormName(bundle string) string {
	if bundle == "" {
		return ""
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(bundle), &data); err != nil {
		return ""
	}
	return firstString(data, "表单名称", "form_name", "formName")
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}

func hashPayload(raw map[string]any) string {
	data, err := json.Marshal(raw)
	if err != nil {
		return shortHash(fmt.Sprintf("%v", raw))
	}
	return shortHash(string(data))
}
