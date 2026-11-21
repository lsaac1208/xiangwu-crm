package http

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestLoggingOmitsQueriesCredentialsAndPanicValues(t *testing.T) {
	var output bytes.Buffer
	router := gin.New()
	router.Use(RequestLogging(slog.New(slog.NewJSONHandler(&output, nil))))
	router.GET("/api/leads/:id", func(c *gin.Context) { c.Status(200) })
	router.GET("/panic", func(c *gin.Context) { panic("private-panic-value") })
	for _, path := range []string{"/api/leads/private-path-value?keyword=13800138000&secret=private-secret", "/panic?secret=private-secret", "/missing-private-path?secret=private-secret"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer private-token")
		req.Header.Set("Cookie", "session=private-cookie")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if strings.HasPrefix(path, "/panic") && response.Code != 500 {
			t.Fatalf("panic status = %d", response.Code)
		}
	}
	for _, sensitive := range []string{"13800138000", "private-secret", "private-token", "private-cookie", "private-panic-value", "private-path-value", "missing-private-path"} {
		if strings.Contains(output.String(), sensitive) {
			t.Fatalf("request log exposed %q", sensitive)
		}
	}
	if !strings.Contains(output.String(), "/api/leads/:id") || !strings.Contains(output.String(), "http_panic") {
		t.Fatalf("missing useful diagnostic context: %s", output.String())
	}
}
