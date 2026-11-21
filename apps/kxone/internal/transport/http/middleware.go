package http

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/lsaac128/kxone/internal/adapter/cache"
	"github.com/lsaac128/kxone/internal/security"
	"github.com/lsaac128/kxone/pkg/types"
)

const defaultMaxRequestBodyBytes int64 = 1 << 20

// JWTClaims JWT 载荷
type JWTClaims struct {
	SessionID    string `json:"sid"`
	AuthVersion  int64  `json:"auth_version"`
	UserID       int64  `json:"user_id"`
	Username     string `json:"username"`
	WeComUserID  string `json:"wecom_userid"`
	Role         string `json:"role"`
	DepartmentID int64  `json:"department_id,omitempty"`
	TokenType    string `json:"token_type"`
	jwt.RegisteredClaims
}

const (
	jwtIssuer          = "kxone"
	jwtAudienceAPI     = "kxone-api"
	jwtAudienceRefresh = "kxone-refresh"
	jwtAudienceBinding = "kxone-binding"
	tokenTypeAccess    = "access"
	tokenTypeRefresh   = "refresh"
	tokenTypeBinding   = "binding"
)

type authUserBindingReader interface {
	GetByID(ctx context.Context, id int64) (*types.WeComUserBinding, error)
}

// AuthMiddleware JWT 认证中间件
func AuthMiddleware(jwtSecret string, bindingReaders ...authUserBindingReader) gin.HandlerFunc {
	var bindingReader authUserBindingReader
	if len(bindingReaders) > 0 {
		bindingReader = bindingReaders[0]
	}
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		tokenString := ""
		if authHeader != "" {
			tokenString = strings.TrimPrefix(authHeader, "Bearer ")
			if tokenString == authHeader {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_token_format"})
				c.Abort()
				return
			}
		}
		if tokenString == "" {
			if cookieToken, err := c.Cookie("kxone_access_token"); err == nil {
				tokenString = strings.TrimSpace(cookieToken)
			}
		}
		if tokenString == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing_token"})
			c.Abort()
			return
		}

		token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		},
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			jwt.WithIssuer(jwtIssuer),
			jwt.WithAudience(jwtAudienceAPI),
		)

		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
			c.Abort()
			return
		}

		if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid && claims.TokenType == tokenTypeAccess {
			accountID, parseErr := strconv.ParseInt(claims.Subject, 10, 64)
			if parseErr != nil || accountID <= 0 || claims.AuthVersion < 1 {
				rejectSession(c)
				return
			}
			if bindingReader == nil {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "account_service_unavailable"})
				return
			}
			binding, bindErr := bindingReader.GetByID(c.Request.Context(), accountID)
			if bindErr != nil {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "query_binding_failed"})
				return
			}
			if binding == nil || binding.ID != accountID || binding.AuthVersion != claims.AuthVersion || binding.Status != types.UserStatusActive {
				rejectSession(c)
				return
			}
			sessions, _ := bindingReader.(security.SessionStore)
			if !requireLiveSession(c, sessions, claims, accountID) {
				return
			}
			c.Set("session_id", claims.SessionID)
			c.Set("account_id", binding.ID)
			c.Set("auth_version", binding.AuthVersion)
			c.Set("user_id", binding.CRMUserID)
			c.Set("username", binding.Username)
			name := binding.EmployeeName
			if strings.TrimSpace(name) == "" {
				name = binding.Username
			}
			c.Set("employee_name", name)
			c.Set("wecom_userid", binding.WeComUserID)
			c.Set("role", string(binding.Role))
			c.Set("department_id", binding.DepartmentID)
			c.Next()
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
			c.Abort()
		}
	}
}

// BindingAuthMiddleware authenticates the short-lived credential issued only
// after a successful WeCom OAuth callback for an unbound account.
func BindingAuthMiddleware(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
		if tokenString == "" || tokenString == c.GetHeader("Authorization") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing_binding_token"})
			c.Abort()
			return
		}
		token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
			return []byte(jwtSecret), nil
		},
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			jwt.WithIssuer(jwtIssuer),
			jwt.WithAudience(jwtAudienceBinding),
		)
		claims, ok := tokenClaims(token)
		if err != nil || !ok || claims.TokenType != tokenTypeBinding || claims.WeComUserID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_binding_token"})
			c.Abort()
			return
		}
		c.Set("wecom_userid", claims.WeComUserID)
		c.Next()
	}
}

func tokenClaims(token *jwt.Token) (*JWTClaims, bool) {
	if token == nil || !token.Valid {
		return nil, false
	}
	claims, ok := token.Claims.(*JWTClaims)
	return claims, ok
}

func SecurityHeaders(allowedOrigins []string) gin.HandlerFunc {
	originAllowList := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			originAllowList[origin] = struct{}{}
		}
	}

	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Header("Content-Security-Policy", strings.Join([]string{
			"default-src 'self'",
			"base-uri 'self'",
			"form-action 'self'",
			"frame-ancestors 'none'",
			"object-src 'none'",
			"font-src 'self' data:",
			"img-src 'self' data: https:",
			"style-src 'self' 'unsafe-inline'",
			"script-src 'self'",
			"connect-src 'self'",
		}, "; "))

		origin := c.GetHeader("Origin")
		if _, ok := originAllowList[origin]; ok {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Dev-Token-Secret")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
		}

		c.Next()
	}
}

func LimitRequestBody(maxBytes int64) gin.HandlerFunc {
	if maxBytes <= 0 {
		maxBytes = defaultMaxRequestBodyBytes
	}
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}

func IsRequestBodyTooLarge(err error) bool {
	var maxBytesError *http.MaxBytesError
	return errors.As(err, &maxBytesError)
}

// RateLimit is process-local. Use a shared Redis or edge limiter before running
// multiple KXOne server instances behind a load balancer.
func RateLimit(maxRequests int, window time.Duration) gin.HandlerFunc {
	if maxRequests <= 0 || window <= 0 {
		return func(c *gin.Context) { c.Next() }
	}

	const maxBuckets = 10000 // 内存上限：防止无界 IP 累积导致 OOM

	type bucket struct {
		windowStart time.Time
		count       int
	}

	var mu sync.Mutex
	buckets := make(map[string]*bucket)
	requestCount := 0 // 每 N 次请求触发一次清理，避免每次 O(n) 扫描

	return func(c *gin.Context) {
		now := time.Now()
		key := c.ClientIP()
		if key == "" {
			key = c.Request.RemoteAddr
		}

		mu.Lock()
		item := buckets[key]
		if item == nil || now.Sub(item.windowStart) >= window {
			// 插入新 IP 前检查容量上限
			if item == nil && len(buckets) >= maxBuckets {
				// 已达上限：触发全量清理，清理过期窗口释放空间
				for ip, existing := range buckets {
					if now.Sub(existing.windowStart) >= window {
						delete(buckets, ip)
					}
				}
				// 清理后仍超限：对新 IP 直接放行（宁可漏检，不丢合法请求）
				if len(buckets) >= maxBuckets {
					mu.Unlock()
					c.Next()
					return
				}
			}
			item = &bucket{windowStart: now}
			buckets[key] = item
		}
		item.count++
		allowed := item.count <= maxRequests

		// 每 512 次请求做一次后台清理，均摊 O(n) 代价
		requestCount++
		if requestCount%512 == 0 {
			for ip, existing := range buckets {
				if now.Sub(existing.windowStart) > 2*window {
					delete(buckets, ip)
				}
			}
		}
		mu.Unlock()

		if !allowed {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate_limited"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RedisRateLimit provides a shared fixed-window limiter. If Redis is
// temporarily unavailable, it falls back to the local limiter to keep the
// endpoint protected.
func RedisRateLimit(redisCache cache.Cache, maxRequests int, window time.Duration, namespace string) gin.HandlerFunc {
	localFallback := RateLimit(maxRequests, window)
	if redisCache == nil || maxRequests <= 0 || window <= 0 {
		return localFallback
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = "default"
	}

	return func(c *gin.Context) {
		key := c.ClientIP()
		if key == "" {
			key = c.Request.RemoteAddr
		}
		bucket := time.Now().Unix() / int64(window.Seconds())
		redisKey := fmt.Sprintf("ratelimit:%s:%d:%s", namespace, bucket, key)
		count, err := redisCache.Incr(c.Request.Context(), redisKey)
		if err != nil {
			localFallback(c)
			return
		}
		if count == 1 {
			_ = redisCache.Expire(c.Request.Context(), redisKey, window+time.Minute)
		}
		if count > int64(maxRequests) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate_limited"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func ConstantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// RequireRole 角色验证中间件
func RequireRole(allowedRoles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusForbidden, gin.H{"error": "role_not_found"})
			c.Abort()
			return
		}

		roleStr, ok := role.(string)
		if !ok || roleStr == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "role_not_found"})
			c.Abort()
			return
		}

		allowed := false
		for _, r := range allowedRoles {
			if roleStr == r {
				allowed = true
				break
			}
		}

		if !allowed {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient_permissions"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// ForbidRole prevents a deliberately narrow CRM role from regaining access
// through a secondary permission system (for example, a legacy CMS grant).
func ForbidRole(blockedRoles ...string) gin.HandlerFunc {
	blocked := make(map[string]struct{}, len(blockedRoles))
	for _, role := range blockedRoles {
		if role = strings.TrimSpace(role); role != "" {
			blocked[role] = struct{}{}
		}
	}
	return func(c *gin.Context) {
		value, exists := c.Get("role")
		role, ok := value.(string)
		if !exists || !ok || strings.TrimSpace(role) == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "role_not_found"})
			c.Abort()
			return
		}
		if _, denied := blocked[role]; denied {
			c.JSON(http.StatusForbidden, gin.H{"error": "insufficient_permissions"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// GetUserID 从上下文获取用户 ID
func GetUserID(c *gin.Context) (int64, error) {
	userID, exists := c.Get("user_id")
	if !exists {
		return 0, types.ErrUnauthorized
	}
	value, ok := userID.(int64)
	if !ok {
		return 0, types.ErrUnauthorized
	}
	return value, nil
}

// GetWeComUserID 从上下文获取企业微信 UserID
func GetWeComUserID(c *gin.Context) (string, error) {
	wecomUserID, exists := c.Get("wecom_userid")
	if !exists {
		return "", types.ErrUnauthorized
	}
	value, ok := wecomUserID.(string)
	if !ok || value == "" {
		return "", types.ErrUnauthorized
	}
	return value, nil
}

// GetRole 从上下文获取当前角色。
func GetRole(c *gin.Context) (string, error) {
	role, exists := c.Get("role")
	if !exists {
		return "", types.ErrUnauthorized
	}
	value, ok := role.(string)
	if !ok || value == "" {
		return "", types.ErrUnauthorized
	}
	return value, nil
}

func rejectSession(c *gin.Context) {
	clearAuthCookies(c)
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "session_revoked", "message": "账号或安全设置已变更，请重新登录"})
}

// LeadIntakeBodyLimit also caps bodies without Content-Length (chunked requests).
func LeadIntakeBodyLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		var limit int64
		switch c.Request.URL.Path {
		case "/api/public/leads/form", "/api/integrations/xiangwu/website-form", "/api/integrations/xiangwu/lead-form":
			limit = 16 << 10
		case "/api/leads":
			limit = 64 << 10
		case "/api/leads/baidu", "/api/leads/tencent", "/api/leads/xiaohongshu", "/api/leads/douyin", "/api/leads/form", "/api/leads/manual", "/api/internal/leads/gateway":
			limit = 256 << 10
		}
		if limit > 0 {
			if c.Request.ContentLength > limit {
				c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request_body_too_large"})
				return
			}
			body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, limit))
			if err != nil {
				if IsRequestBodyTooLarge(err) {
					c.AbortWithStatusJSON(413, gin.H{"error": "request_body_too_large"})
				} else {
					c.AbortWithStatusJSON(400, gin.H{"error": "read_body_failed"})
				}
				return
			}
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
		}
		c.Next()
	}
}

func requireLiveSession(c *gin.Context, store security.SessionStore, claims *JWTClaims, account int64) bool {
	if claims.SessionID == "" {
		rejectSession(c)
		return false
	}
	if store == nil {
		c.AbortWithStatusJSON(503, gin.H{"error": "session_service_unavailable"})
		return false
	}
	s, err := store.GetSession(c.Request.Context(), claims.SessionID)
	if err != nil {
		internalError(c, 503, "session_lookup_failed", err)
		c.Abort()
		return false
	}
	if s == nil || s.AccountID != account || s.AuthVersion != claims.AuthVersion || s.RevokedAt != nil || !s.ExpiresAt.After(time.Now()) {
		rejectSession(c)
		return false
	}
	return true
}
