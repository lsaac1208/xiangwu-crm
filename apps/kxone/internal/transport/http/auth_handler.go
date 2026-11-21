package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/lsaac128/kxone/internal/security"
	"github.com/lsaac128/kxone/pkg/types"
	"golang.org/x/crypto/bcrypt"
)

// AuthHandler implements native CRM username/password authentication.
type AuthHandler struct {
	protection    security.LoginProtection
	loginKey      string
	sessions      security.SessionStore
	users         credentialUserReader
	jwtSecret     string
	accessExpiry  int
	refreshExpiry int
}

type credentialUserReader interface {
	GetByUsername(ctx context.Context, username string) (*types.WeComUserBinding, error)
	GetByID(ctx context.Context, id int64) (*types.WeComUserBinding, error)
	ChangePassword(ctx context.Context, id, authVersion int64, oldHash, newHash string) error
}

func NewAuthHandler(users credentialUserReader, jwtSecret string, accessExpiry, refreshExpiry int) *AuthHandler {
	sessions, _ := users.(security.SessionStore)
	return &AuthHandler{sessions: sessions, users: users, jwtSecret: jwtSecret, accessExpiry: min(accessExpiry, 7200), refreshExpiry: min(refreshExpiry, 604800)}
}

func (h *AuthHandler) WithLoginProtection(p security.LoginProtection, key string) *AuthHandler {
	h.protection = p
	h.loginKey = key
	return h
}
func (h *AuthHandler) loginGuard(c *gin.Context, identity, op string) bool {
	if h.protection == nil {
		c.AbortWithStatusJSON(503, gin.H{"error": "login_protection_unavailable", "message": "登录服务暂时不可用，请稍后重试"})
		return false
	}
	mac := hmac.New(sha256.New, []byte(h.loginKey))
	mac.Write([]byte("crm-login-v1\x00" + identity))
	wait, err := h.protection.LoginAttempt(c.Request.Context(), "login:account:"+hex.EncodeToString(mac.Sum(nil)), op)
	if err != nil {
		logHTTPError(c, "login_protection_unavailable", err)
		c.AbortWithStatusJSON(503, gin.H{"error": "login_protection_unavailable", "message": "登录服务暂时不可用，请稍后重试"})
		return false
	}
	if wait > 0 {
		c.Header("Retry-After", strconv.FormatInt(wait, 10))
		c.AbortWithStatusJSON(429, gin.H{"error": "login_cooldown", "message": fmt.Sprintf("登录尝试过多，请在 %d 秒后重试", wait)})
		return false
	}
	return true
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required"`
}

type AuthResponse struct {
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in"`
	UserID       int64  `json:"user_id"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name"`
	Role         string `json:"role"`
}

var invalidPasswordHash = []byte("$2a$10$7EqJtq98hPqEX7fNZaFWoOe5H5N8qK1O0Hf3Bx2fL3pYwVQYpGf5K")

func (h *AuthHandler) HandleLogin(c *gin.Context) {
	if h.users == nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondInvalidRequest(c, err)
		return
	}
	username := strings.TrimSpace(req.Username)
	user, err := h.users.GetByUsername(c.Request.Context(), username)
	if err != nil {
		internalError(c, http.StatusInternalServerError, "login_failed", err)
		return
	}
	identity := "username:" + strings.ToLower(username)
	if user != nil {
		identity = fmt.Sprintf("account:%d", user.ID)
	}
	if !h.loginGuard(c, identity, "check") {
		return
	}
	hash := invalidPasswordHash
	if user != nil && user.PasswordHash != "" {
		hash = []byte(user.PasswordHash)
	}
	passwordOK := bcrypt.CompareHashAndPassword(hash, []byte(req.Password)) == nil
	if user == nil || !passwordOK {
		if !h.loginGuard(c, identity, "failure") {
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials", "message": "账号或密码错误"})
		return
	}
	if user.Status != types.UserStatusActive {
		c.JSON(http.StatusForbidden, gin.H{"error": "user_disabled", "message": "账号已被禁用"})
		return
	}

	if !h.loginGuard(c, identity, "success") {
		return
	}
	now := time.Now()
	if h.sessions == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "session_service_unavailable"})
		return
	}
	sid := uuid.NewString()
	session := &security.Session{ID: sid, AccountID: user.ID, AuthVersion: user.AuthVersion, CreatedAt: now.UTC(), ExpiresAt: now.Add(time.Duration(h.refreshExpiry) * time.Second).UTC()}
	if err := h.sessions.CreateSession(c.Request.Context(), session); err != nil {
		internalError(c, 503, "session_create_failed", err)
		return
	}
	accessToken, err := h.generateToken(user, now, h.accessExpiry, tokenTypeAccess, sid)
	if err != nil {
		internalError(c, http.StatusInternalServerError, "generate_token_failed", err)
		return
	}
	refreshToken, err := h.generateToken(user, now, h.refreshExpiry, tokenTypeRefresh, sid)
	if err != nil {
		internalError(c, http.StatusInternalServerError, "generate_refresh_token_failed", err)
		return
	}
	setAuthCookies(c, accessToken, refreshToken, h.accessExpiry, h.refreshExpiry)
	c.JSON(http.StatusOK, AuthResponse{
		ExpiresIn: h.accessExpiry, UserID: user.CRMUserID, Username: user.Username,
		DisplayName: user.EmployeeName, Role: string(user.Role),
	})
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandler) HandleRefreshToken(c *gin.Context) {
	var req RefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		respondInvalidRequest(c, err)
		return
	}
	refreshToken := strings.TrimSpace(req.RefreshToken)
	fromCookie := refreshToken == ""
	if fromCookie {
		refreshToken, _ = c.Cookie("kxone_refresh_token")
	}
	claims, err := h.parseRefreshToken(strings.TrimSpace(refreshToken))
	if err != nil {
		clearAuthCookies(c)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_refresh_token"})
		return
	}
	accountID, parseErr := strconv.ParseInt(claims.Subject, 10, 64)
	if parseErr != nil || accountID <= 0 || claims.AuthVersion < 1 {
		rejectSession(c)
		return
	}
	if h.users == nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	user, err := h.users.GetByID(c.Request.Context(), accountID)
	if err != nil {
		internalError(c, http.StatusServiceUnavailable, "query_user_failed", err)
		return
	}
	if user == nil || user.ID != accountID || user.Status != types.UserStatusActive || user.AuthVersion != claims.AuthVersion {
		rejectSession(c)
		return
	}

	if !requireLiveSession(c, h.sessions, claims, accountID) {
		return
	}
	accessExpiry := min(h.accessExpiry, int(time.Until(claims.ExpiresAt.Time).Seconds()))
	if accessExpiry <= 0 {
		rejectSession(c)
		return
	}
	accessToken, err := h.generateToken(user, time.Now(), accessExpiry, tokenTypeAccess, claims.SessionID)
	if err != nil {
		internalError(c, http.StatusInternalServerError, "generate_token_failed", err)
		return
	}
	setAccessTokenCookie(c, accessToken, accessExpiry)
	response := gin.H{"expires_in": accessExpiry}
	if !fromCookie {
		response["access_token"] = accessToken
	}
	c.JSON(http.StatusOK, response)
}

func (h *AuthHandler) HandleLogout(c *gin.Context) {
	// Try the refresh credential first: access may already have expired.
	raw, _ := c.Cookie("kxone_refresh_token")
	var claims *JWTClaims
	if raw != "" {
		claims, _ = h.parseRefreshToken(raw)
	}
	if claims == nil {
		raw = strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if raw == "" {
			raw, _ = c.Cookie("kxone_access_token")
		}
		token, err := jwt.ParseWithClaims(raw, &JWTClaims{}, func(*jwt.Token) (interface{}, error) { return []byte(h.jwtSecret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(jwtIssuer), jwt.WithAudience(jwtAudienceAPI), jwt.WithoutClaimsValidation())
		if err == nil {
			claims, _ = tokenClaims(token)
			if claims != nil && (claims.TokenType != tokenTypeAccess || claims.Issuer != jwtIssuer || !slices.Contains(claims.Audience, jwtAudienceAPI)) {
				claims = nil
			}
		}
	}
	if claims != nil && claims.SessionID != "" {
		account, err := strconv.ParseInt(claims.Subject, 10, 64)
		if err != nil || account <= 0 {
			rejectSession(c)
			return
		}
		if h.sessions == nil {
			c.AbortWithStatusJSON(503, gin.H{"error": "session_service_unavailable"})
			return
		}
		if err := h.sessions.RevokeSession(c.Request.Context(), claims.SessionID, account, time.Now()); err != nil {
			internalError(c, 503, "session_revoke_failed", err)
			return
		}
	}
	clearAuthCookies(c)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *AuthHandler) HandleChangePassword(c *gin.Context) {
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondInvalidRequest(c, err)
		return
	}
	accountID := c.GetInt64("account_id")
	authVersion := c.GetInt64("auth_version")
	if accountID <= 0 || authVersion < 1 {
		rejectSession(c)
		return
	}
	if h.users == nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}

	if len(req.NewPassword) < 8 || len(req.NewPassword) > 72 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_new_password", "message": "新密码长度必须为 8-72 位"})
		return
	}
	user, err := h.users.GetByID(c.Request.Context(), accountID)
	if err != nil {
		internalError(c, http.StatusInternalServerError, "query_user_failed", err)
		return
	}
	if user == nil || user.ID != accountID || user.Status != types.UserStatusActive || user.AuthVersion != authVersion {
		rejectSession(c)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_current_password", "message": "当前密码不正确"})
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.NewPassword)) == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password_unchanged", "message": "新密码不能与当前密码相同"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		internalError(c, http.StatusInternalServerError, "hash_password_failed", err)
		return
	}
	if err := h.users.ChangePassword(c.Request.Context(), accountID, authVersion, user.PasswordHash, string(hash)); err != nil {
		if errors.Is(err, types.ErrAccountVersionConflict) {
			rejectSession(c)
			return
		}
		internalError(c, http.StatusInternalServerError, "update_password_failed", err)
		return
	}
	clearAuthCookies(c)
	slog.Info("account password changed", "account_id", accountID, "previous_auth_version", authVersion)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "密码已修改，请重新登录"})
}

func (h *AuthHandler) generateToken(user *types.WeComUserBinding, now time.Time, expirySeconds int, tokenType string, sessionIDs ...string) (string, error) {
	if user.ID <= 0 || user.AuthVersion < 1 {
		return "", errors.New("invalid account identity")
	}
	audience := jwtAudienceAPI
	if tokenType == tokenTypeRefresh {
		audience = jwtAudienceRefresh
	}
	sid := ""
	if len(sessionIDs) > 0 {
		sid = sessionIDs[0]
	}
	claims := JWTClaims{SessionID: sid,
		AuthVersion: user.AuthVersion, UserID: user.CRMUserID, Username: user.Username, WeComUserID: user.WeComUserID,
		Role: string(user.Role), DepartmentID: user.DepartmentID, TokenType: tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(user.ID, 10),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(expirySeconds) * time.Second)),
			IssuedAt:  jwt.NewNumericDate(now), Issuer: jwtIssuer, Audience: jwt.ClaimStrings{audience},
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(h.jwtSecret))
}

func (h *AuthHandler) parseRefreshToken(raw string) (*JWTClaims, error) {
	if raw == "" {
		return nil, errors.New("missing refresh token")
	}
	token, err := jwt.ParseWithClaims(raw, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(h.jwtSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(jwtIssuer), jwt.WithAudience(jwtAudienceRefresh))
	claims, ok := tokenClaims(token)
	if err != nil || !ok || claims.TokenType != tokenTypeRefresh {
		return nil, errors.New("invalid refresh token")
	}
	return claims, nil
}

func setAuthCookies(c *gin.Context, accessToken, refreshToken string, accessExpiry, refreshExpiry int) {
	c.SetSameSite(http.SameSiteLaxMode)
	secure := requestUsesHTTPS(c)
	c.SetCookie("kxone_access_token", accessToken, accessExpiry, "/", "", secure, true)
	c.SetCookie("kxone_refresh_token", refreshToken, refreshExpiry, "/api/auth", "", secure, true)
	c.SetCookie("kxone_session", "1", accessExpiry, "/", "", secure, false)
}

func setAccessTokenCookie(c *gin.Context, token string, expiry int) {
	c.SetSameSite(http.SameSiteLaxMode)
	secure := requestUsesHTTPS(c)
	c.SetCookie("kxone_access_token", token, expiry, "/", "", secure, true)
	c.SetCookie("kxone_session", "1", expiry, "/", "", secure, false)
}

func clearAuthCookies(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	secure := requestUsesHTTPS(c)
	c.SetCookie("kxone_access_token", "", -1, "/", "", secure, true)
	c.SetCookie("kxone_refresh_token", "", -1, "/api/auth", "", secure, true)
	c.SetCookie("kxone_session", "", -1, "/", "", secure, false)
}

func requestUsesHTTPS(c *gin.Context) bool {
	return c.Request.TLS != nil || strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")), "https")
}
