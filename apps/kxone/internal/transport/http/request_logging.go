package http

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequestLogging records route templates rather than raw URLs. Query strings,
// path parameters, headers and panic values can contain customer data or keys.
func RequestLogging(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := uuid.NewString()
		c.Set("request_id", requestID)
		c.Header("X-Request-ID", requestID)
		started := time.Now()
		defer func() {
			route := c.FullPath()
			if route == "" {
				route = "unmatched"
			}
			if failure := recover(); failure != nil {
				logger.Error("http_panic", "route", route, "panic_type", fmt.Sprintf("%T", failure), "stack", string(debug.Stack()))
				if !c.Writer.Written() {
					c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal_server_error"})
				} else {
					c.Abort()
				}
			}
			logger.Info("http_request", "request_id", requestID, "account_id", c.GetInt64("account_id"), "method", c.Request.Method, "route", route,
				"status", c.Writer.Status(), "duration_ms", time.Since(started).Milliseconds())
		}()
		c.Next()
	}
}
