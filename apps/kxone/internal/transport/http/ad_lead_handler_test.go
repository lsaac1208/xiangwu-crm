package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestAdLeadWebhookAllowsTrustedGatewayForward(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewAdLeadHandler(nil, nil, "").
		WithWebhookSecret("ad-secret", time.Minute).
		WithTrustedGatewayIPs([]string{"203.0.113.50"})

	c := newAdLeadTestContext("203.0.113.50:45678", map[string]string{
		"X-KXOne-Manager": "StaffC",
	})

	if !handler.authorizeWebhook(c, []byte(`{"lead_id":"1"}`)) {
		t.Fatal("trusted gateway forward should be authorized")
	}
}

func TestAdLeadWebhookRejectsUntrustedGatewayForward(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewAdLeadHandler(nil, nil, "").
		WithWebhookSecret("ad-secret", time.Minute).
		WithTrustedGatewayIPs([]string{"203.0.113.50"})

	c := newAdLeadTestContext("203.0.113.10:45678", map[string]string{
		"X-KXOne-Manager": "StaffC",
	})

	if handler.authorizeWebhook(c, []byte(`{"lead_id":"1"}`)) {
		t.Fatal("untrusted gateway forward should be rejected")
	}
}

func TestAdLeadWebhookTrustedGatewayRequiresManagerHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewAdLeadHandler(nil, nil, "").
		WithWebhookSecret("ad-secret", time.Minute).
		WithTrustedGatewayIPs([]string{"203.0.113.50"})

	c := newAdLeadTestContext("203.0.113.50:45678", nil)

	if handler.authorizeWebhook(c, []byte(`{"lead_id":"1"}`)) {
		t.Fatal("trusted gateway forward without manager header should be rejected")
	}
}

func TestPublicFormReturnsUnavailableWithoutDefaultManager(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewAdLeadHandler(nil, nil, "").WithPublicFormOrigins("https://kxone.example.com")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/public/leads/form", strings.NewReader(`{"studentName":"网站学生","phone":"13912345678"}`))
	c.Request.Header.Set("Origin", "https://kxone.example.com")

	handler.ReceivePublicForm(c)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("public form status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"error":"service_unavailable"`) {
		t.Fatalf("public form body = %s", recorder.Body.String())
	}
}

func TestNormalizeWebsiteFormKeepsContentAttributionSeparateFromChannel(t *testing.T) {
	lead := normalizeFormLead(map[string]any{"studentName": "学生", "phone": "13912345678", "accountName": "学校官网", "contentEntryId": "21", "contentRevisionId": float64(34), "contentPath": "/zixun/example/"}, "")
	if lead.SourceChannel != "form:学校官网" {
		t.Fatalf("channel=%q", lead.SourceChannel)
	}
	if lead.ContentEntryID == nil || *lead.ContentEntryID != 21 || lead.ContentRevisionID == nil || *lead.ContentRevisionID != 34 || lead.ContentPath != "/zixun/example/" {
		t.Fatalf("attribution=%+v", lead)
	}
	invalid := normalizeFormLead(map[string]any{"phone": "13912345678", "contentPath": "https://evil.example/"}, "")
	if invalid.ContentPath != "" {
		t.Fatalf("invalid content path accepted: %q", invalid.ContentPath)
	}
}

func newAdLeadTestContext(remoteAddr string, headers map[string]string) *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest("POST", "/api/leads/tencent", strings.NewReader(`{}`))
	req.RemoteAddr = remoteAddr
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	c.Request = req
	return c
}
