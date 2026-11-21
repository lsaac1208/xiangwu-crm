package http

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestIntakeBodyCapsConsumeChunkedTrailingBytes(t *testing.T) {
	for _, tc := range []struct {
		path  string
		limit int
	}{{"/api/public/leads/form", 16 << 10}, {"/api/leads", 64 << 10}, {"/api/leads/tencent", 256 << 10}, {"/api/internal/leads/gateway", 256 << 10}} {
		for _, chunked := range []bool{false, true} {
			r := gin.New()
			r.Use(LeadIntakeBodyLimit())
			handled := false
			r.POST(tc.path, func(c *gin.Context) { handled = true; c.JSON(201, gin.H{}) })
			req := httptest.NewRequest("POST", tc.path, strings.NewReader(`{}`+strings.Repeat(" ", tc.limit)))
			if chunked {
				req.ContentLength = -1
			}
			response := httptest.NewRecorder()
			r.ServeHTTP(response, req)
			if response.Code != 413 || handled {
				t.Fatalf("%s chunked=%t status=%d handled=%t", tc.path, chunked, response.Code, handled)
			}
		}
	}
}
