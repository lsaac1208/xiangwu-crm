package http

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/internal/security"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestGatewayRejectsDisabledConfiguredAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := repo.NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "channels.db"), MaxConns: 1})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := repo.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	channelRepo := repo.NewGormChannelIntegrationRepository(db)
	now := time.Now()
	item := &types.ChannelIntegration{Platform: "tencent", AccountName: "停用账户", CallbackPath: "/api/leads/tencent", AuthMode: "platform_compatible", Enabled: false, CreatedBy: "admin", UpdatedBy: "admin", CreatedAt: now, UpdatedAt: now}
	if err := channelRepo.Create(t.Context(), item); err != nil {
		t.Fatalf("create integration: %v", err)
	}

	secret := "gateway-secret-with-more-than-32-characters"
	handler := NewAdLeadHandler(nil, nil, "manager").WithGatewayHMAC(secret, time.Minute).WithChannelIntegrations(channelRepo)
	body := `{"schemaVersion":1,"source":"tencent","externalLeadId":"tx-disabled-1","accountName":"停用账户","phone":"13900000000"}`
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "." + body))
	req := httptest.NewRequest(http.MethodPost, "/api/internal/leads/gateway", strings.NewReader(body))
	req.Header.Set("X-KXOne-Gateway-Timestamp", timestamp)
	req.Header.Set("X-KXOne-Gateway-Signature", hex.EncodeToString(mac.Sum(nil)))
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = req
	handler.ReceiveGateway(c)
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "channel_account_disabled_or_unknown") {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestXiaohongshuSignatureIsIsolatedByAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := repo.NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "channels.db"), MaxConns: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	protector, err := security.NewAESGCMFieldProtector("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	channelRepo := repo.NewGormChannelIntegrationRepository(db).WithFieldProtector(protector)
	now := time.Now()
	for _, item := range []*types.ChannelIntegration{
		{Platform: "xiaohongshu", AccountName: "账户A", CallbackPath: "/api/leads/xiaohongshu", AuthMode: "hmac_sha1", WebhookToken: "token-for-account-a", Enabled: true, CreatedBy: "admin", UpdatedBy: "admin", CreatedAt: now, UpdatedAt: now},
		{Platform: "xiaohongshu", AccountName: "账户B", CallbackPath: "/api/leads/xiaohongshu", AuthMode: "hmac_sha1", WebhookToken: "token-for-account-b", Enabled: true, CreatedBy: "admin", UpdatedBy: "admin", CreatedAt: now, UpdatedAt: now},
	} {
		if err := channelRepo.Create(t.Context(), item); err != nil {
			t.Fatal(err)
		}
	}

	raw := []byte(`{"data":{"id":1}}`)
	xhsMAC := hmac.New(sha1.New, []byte("token-for-account-a"))
	xhsMAC.Write(raw)
	xhsSignature := "sha1=" + hex.EncodeToString(xhsMAC.Sum(nil))
	gatewaySecret := "gateway-secret-with-more-than-32-characters"
	handler := NewAdLeadHandler(nil, nil, "manager").WithGatewayHMAC(gatewaySecret, time.Minute).WithChannelIntegrations(channelRepo)

	verify := func(account string) string {
		body := `{"account_name":"` + account + `","signature":"` + xhsSignature + `","raw_body_base64":"` + base64.StdEncoding.EncodeToString(raw) + `"}`
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(gatewaySecret))
		mac.Write([]byte(timestamp + "." + body))
		req := httptest.NewRequest(http.MethodPost, "/api/internal/leads/xiaohongshu/verify-signature", strings.NewReader(body))
		req.Header.Set("X-KXOne-Gateway-Timestamp", timestamp)
		req.Header.Set("X-KXOne-Gateway-Signature", hex.EncodeToString(mac.Sum(nil)))
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = req
		handler.VerifyXiaohongshuSignature(c)
		if recorder.Code != http.StatusOK {
			t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
		}
		return recorder.Body.String()
	}
	if body := verify("账户A"); !strings.Contains(body, `"valid":true`) {
		t.Fatalf("account A = %s", body)
	}
	if body := verify("账户B"); !strings.Contains(body, `"valid":false`) {
		t.Fatalf("account B = %s", body)
	}
}

func TestGatewayCampaignSummaryFormatsRFC3339LeadTimeInChinaTimezone(t *testing.T) {
	req := GatewayLeadRequest{StatusOrTag: "高考复读", LeadCreatedAt: "2026-07-30T01:12:43.656Z"}
	if got := req.campaignSummary(); got != "高考复读 / 留资时间:2026-07-30 09:12:43" {
		t.Fatalf("campaign summary = %q", got)
	}

	req.LeadCreatedAt = "2026-07-29 20:50:14"
	if got := req.campaignSummary(); got != "高考复读 / 留资时间:2026-07-29 20:50:14" {
		t.Fatalf("local campaign summary = %q", got)
	}
}

func TestGatewayTencentCampaignSummaryLabelsSourceFields(t *testing.T) {
	req := GatewayLeadRequest{
		Source:        "tencent",
		StatusOrTag:   "参与赔付",
		CampaignName:  "campaign_v3_1784706608318096198_1109",
		AdgroupName:   "0722艾米-复读",
		CreativeName:  "0727-0708复读混剪4_play_1785125202042_202607271206260",
		PageURL:       "https://h5.gdt.qq.com/xjviewer/nemo/87227586",
		LeadCreatedAt: "2026-07-31 08:23:23",
	}
	want := "状态:参与赔付 / 计划:campaign_v3_1784706608318096198_1109 / 广告组:0722艾米-复读 / 创意:0727-0708复读混剪4_play_1785125202042_202607271206260 / 页面:https://h5.gdt.qq.com/xjviewer/nemo/87227586 / 留资时间:2026-07-31 08:23:23"
	if got := req.campaignSummary(); got != want {
		t.Fatalf("campaign summary = %q, want %q", got, want)
	}
}

func TestGatewayNonTencentCampaignSummaryRemainsUnlabeled(t *testing.T) {
	for _, source := range []string{"baidu", "xiaohongshu", "form", "manual"} {
		t.Run(source, func(t *testing.T) {
			req := GatewayLeadRequest{Source: source, StatusOrTag: "来源标签", CampaignName: "高考复读"}
			if got := req.campaignSummary(); got != "来源标签 / 高考复读" {
				t.Fatalf("campaign summary = %q", got)
			}
		})
	}
}

func TestGatewayTencentCampaignSummaryOmitsEmptyFields(t *testing.T) {
	req := GatewayLeadRequest{Source: "tencent", AdgroupName: " 0722艾米-复读 "}
	if got := req.campaignSummary(); got != "广告组:0722艾米-复读" {
		t.Fatalf("campaign summary = %q", got)
	}
}
