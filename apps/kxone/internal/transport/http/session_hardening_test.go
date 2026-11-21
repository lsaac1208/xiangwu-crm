package http

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/lsaac128/kxone/pkg/types"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuthMiddlewareRequiresAccountRepository(t *testing.T) {
	router := gin.New()
	router.Use(AuthMiddleware("test-secret"))
	router.GET("/protected", func(c *gin.Context) { c.Status(200) })
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+signedAccessToken(t, "test-secret", "admin"))
	result := httptest.NewRecorder()
	router.ServeHTTP(result, req)
	if result.Code < 400 {
		t.Fatalf("missing account repository must fail closed: %d", result.Code)
	}
}

func TestAuthMiddlewareRejectsChangedSessionVersion(t *testing.T) {
	router := gin.New()
	router.Use(AuthMiddleware("test-secret", &staticAuthBindingReader{binding: &types.WeComUserBinding{ID: 5, CRMUserID: 5, AuthVersion: 2, Status: types.UserStatusActive, Role: types.UserRoleAdmin}}))
	router.GET("/protected", func(c *gin.Context) { c.Status(200) })
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "Bearer "+signedAccessToken(t, "test-secret", "admin"))
	result := httptest.NewRecorder()
	router.ServeHTTP(result, request)
	if result.Code != 401 {
		t.Fatalf("revoked session accepted: %d", result.Code)
	}
	for _, cookie := range result.Result().Cookies() {
		if cookie.MaxAge >= 0 {
			t.Fatal("revoked credentials not cleared")
		}
	}
}

func TestAuthMiddlewareRejectsLegacyCredentialsAndLookupFailure(t *testing.T) {
	for _, scenario := range []string{"legacy", "lookup_failure", "deleted"} {
		t.Run(scenario, func(t *testing.T) {
			reader := &staticAuthBindingReader{binding: &types.WeComUserBinding{ID: 5, AuthVersion: 1, Status: types.UserStatusActive}}
			token := signedAccessToken(t, "test-secret", "admin")
			if scenario == "legacy" {
				old := jwt.NewWithClaims(jwt.SigningMethodHS256, JWTClaims{UserID: 5, WeComUserID: "tw", Role: "admin", TokenType: tokenTypeAccess, RegisteredClaims: jwt.RegisteredClaims{Issuer: jwtIssuer, Audience: jwt.ClaimStrings{jwtAudienceAPI}, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}})
				var err error
				token, err = old.SignedString([]byte("test-secret"))
				if err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "lookup_failure" {
				reader.err = errors.New("database unavailable")
			}
			if scenario == "deleted" {
				reader.binding = nil
			}
			router := gin.New()
			router.Use(AuthMiddleware("test-secret", reader))
			router.GET("/protected", func(c *gin.Context) { c.Status(200) })
			request := httptest.NewRequest(http.MethodGet, "/protected", nil)
			request.Header.Set("Authorization", "Bearer "+token)
			result := httptest.NewRecorder()
			router.ServeHTTP(result, request)
			want := 401
			if scenario == "lookup_failure" {
				want = 503
			}
			if result.Code != want {
				t.Fatalf("got %d want %d", result.Code, want)
			}
		})
	}
}
