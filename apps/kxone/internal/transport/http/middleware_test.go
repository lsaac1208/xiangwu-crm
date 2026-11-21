package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/lsaac128/kxone/internal/security"
	"github.com/lsaac128/kxone/pkg/types"
)

type staticAuthBindingReader struct {
	binding *types.WeComUserBinding
	err     error
}

func (r *staticAuthBindingReader) GetByID(context.Context, int64) (*types.WeComUserBinding, error) {
	return r.binding, r.err
}

func signedAccessToken(t *testing.T, secret, role string) string {
	t.Helper()
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, JWTClaims{
		SessionID: "test-session", AuthVersion: 1, UserID: 5,
		WeComUserID: "tw",
		Role:        role,
		TokenType:   tokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{Subject: "5",
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    jwtIssuer,
			Audience:  jwt.ClaimStrings{jwtAudienceAPI},
		},
	})
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestAuthMiddlewareUsesLatestBindingRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const secret = "test-secret"
	router := gin.New()
	router.Use(AuthMiddleware(secret, &staticAuthBindingReader{binding: &types.WeComUserBinding{
		ID: 5, AuthVersion: 1, CRMUserID: 5, WeComUserID: "tw", Role: types.UserRoleAdmin, Status: types.UserStatusActive,
	}}))
	router.GET("/role", func(c *gin.Context) {
		role, _ := GetRole(c)
		c.JSON(http.StatusOK, gin.H{"role": role})
	})

	req := httptest.NewRequest(http.MethodGet, "/role", nil)
	req.Header.Set("Authorization", "Bearer "+signedAccessToken(t, secret, string(types.UserRoleManager)))
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"role":"admin"`) {
		t.Fatalf("latest binding role not applied: status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestAuthMiddlewareRejectsDisabledBindingImmediately(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const secret = "test-secret"
	router := gin.New()
	router.Use(AuthMiddleware(secret, &staticAuthBindingReader{binding: &types.WeComUserBinding{
		ID: 5, AuthVersion: 1, CRMUserID: 5, WeComUserID: "tw", Role: types.UserRoleAdmin, Status: types.UserStatusDisabled,
	}}))
	router.GET("/role", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/role", nil)
	req.Header.Set("Authorization", "Bearer "+signedAccessToken(t, secret, string(types.UserRoleAdmin)))
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized || !strings.Contains(resp.Body.String(), "session_revoked") {
		t.Fatalf("disabled binding accepted: status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestSecurityHeadersAllowEmbeddedIconFonts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(SecurityHeaders(nil))
	router.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	router.ServeHTTP(resp, req)

	csp := resp.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "font-src 'self' data:") {
		t.Fatalf("CSP should allow Vant embedded icon fonts, got %q", csp)
	}
	if !strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "object-src 'none'") {
		t.Fatalf("CSP should deny framing and plugins, got %q", csp)
	}
	if got := resp.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options=%q, want DENY", got)
	}
}

func TestLimitRequestBodyRejectsOversizedJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(LimitRequestBody(16))
	router.POST("/body", func(c *gin.Context) {
		var body map[string]any
		if err := c.ShouldBindJSON(&body); err != nil {
			respondInvalidRequest(c, err)
			return
		}
		c.JSON(http.StatusOK, body)
	})

	payload, _ := json.Marshal(map[string]string{"value": strings.Repeat("x", 64)})
	req := httptest.NewRequest(http.MethodPost, "/body", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusRequestEntityTooLarge || !strings.Contains(resp.Body.String(), "request_body_too_large") {
		t.Fatalf("oversized request status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestLimitRequestBodyTreatsTruncatedJSONAsBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(LimitRequestBody(1024))
	router.POST("/body", func(c *gin.Context) {
		var body map[string]any
		if err := c.ShouldBindJSON(&body); err != nil {
			respondInvalidRequest(c, err)
			return
		}
		c.JSON(http.StatusOK, body)
	})

	req := httptest.NewRequest(http.MethodPost, "/body", strings.NewReader(`{"value":`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), "invalid_request") {
		t.Fatalf("truncated request status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func (r *staticAuthBindingReader) CreateSession(context.Context, *security.Session) error { return nil }
func (r *staticAuthBindingReader) RevokeSession(context.Context, string, int64, time.Time) error {
	return nil
}
func (r *staticAuthBindingReader) GetSession(context.Context, string) (*security.Session, error) {
	return &security.Session{ID: "test-session", AccountID: 5, AuthVersion: 1, ExpiresAt: time.Now().Add(time.Hour)}, r.err
}
