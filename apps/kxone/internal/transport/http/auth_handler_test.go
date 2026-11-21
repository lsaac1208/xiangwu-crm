package http

import (
	"context"
	"fmt"
	"github.com/alicebob/miniredis/v2"
	"github.com/lsaac128/kxone/internal/adapter/cache"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/security"
	"github.com/lsaac128/kxone/pkg/types"
	"golang.org/x/crypto/bcrypt"
	"time"
)

type credentialReaderStub struct {
	users map[string]*types.WeComUserBinding
}

func (r credentialReaderStub) GetByUsername(_ context.Context, username string) (*types.WeComUserBinding, error) {
	return r.users[username], nil
}

func (r credentialReaderStub) GetByID(_ context.Context, id int64) (*types.WeComUserBinding, error) {
	for _, user := range r.users {
		if user.ID == id {
			return user, nil
		}
	}
	return nil, nil
}
func (r credentialReaderStub) ChangePassword(ctx context.Context, id, version int64, oldHash, newHash string) error {
	user, _ := r.GetByID(ctx, id)
	if user == nil || user.AuthVersion != version || user.PasswordHash != oldHash || user.Status != types.UserStatusActive {
		return types.ErrAccountVersionConflict
	}
	user.PasswordHash = newHash
	user.AuthVersion++
	user.AccountVersion++
	return nil
}

func TestPasswordLoginSetsHttpOnlySessionCookies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	handler := newTestAuthHandler(credentialReaderStub{users: map[string]*types.WeComUserBinding{
		"admin": {ID: 1, AuthVersion: 1, AccountVersion: 1, CRMUserID: 1, Username: "admin", WeComUserID: "admin", PasswordHash: string(hash), EmployeeName: "管理员", Role: types.UserRoleAdmin, Status: types.UserStatusActive},
	}}, "test-secret", 3600, 86400)
	router := gin.New()
	router.POST("/api/auth/login", handler.HandleLogin)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"correct-password"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	cookies := resp.Result().Cookies()
	seenAccess, seenRefresh, allSecure := false, false, true
	for _, cookie := range cookies {
		allSecure = allSecure && cookie.Secure
		if cookie.Name == "kxone_access_token" {
			seenAccess = cookie.HttpOnly
		}
		if cookie.Name == "kxone_refresh_token" {
			seenRefresh = cookie.HttpOnly
		}
	}
	if !seenAccess || !seenRefresh || !allSecure {
		t.Fatalf("missing secure HttpOnly auth cookies: %#v", cookies)
	}
}

func TestPasswordLoginRejectsInvalidCredentialsAndDisabledUser(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	handler := newTestAuthHandler(credentialReaderStub{users: map[string]*types.WeComUserBinding{
		"disabled": {Username: "disabled", PasswordHash: string(hash), Status: types.UserStatusDisabled},
	}}, "test-secret", 3600, 86400)
	for _, tc := range []struct {
		body string
		want int
	}{
		{`{"username":"missing","password":"wrong-pass"}`, http.StatusUnauthorized},
		{`{"username":"disabled","password":"correct-password"}`, http.StatusForbidden},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
		c.Request.Header.Set("Content-Type", "application/json")
		handler.HandleLogin(c)
		if w.Code != tc.want {
			t.Fatalf("body=%s status=%d want=%d", tc.body, w.Code, tc.want)
		}
	}
}

func TestUserCanChangeOwnPassword(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("current-password"), bcrypt.MinCost)
	users := credentialReaderStub{users: map[string]*types.WeComUserBinding{
		"sales": {ID: 1, AuthVersion: 1, AccountVersion: 1, Username: "sales", PasswordHash: string(hash), Status: types.UserStatusActive},
	}}
	handler := newTestAuthHandler(users, "test-secret", 3600, 86400)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("account_id", int64(1))
	c.Set("auth_version", int64(1))
	c.Request = httptest.NewRequest(http.MethodPut, "/api/user/password", strings.NewReader(`{"current_password":"current-password","new_password":"new-secure-password"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	handler.HandleChangePassword(c)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if bcrypt.CompareHashAndPassword([]byte(users.users["sales"].PasswordHash), []byte("new-secure-password")) != nil {
		t.Fatal("new password was not saved")
	}
}

func TestChangePasswordRejectsWrongCurrentAndWeakNewPassword(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("current-password"), bcrypt.MinCost)
	handler := newTestAuthHandler(credentialReaderStub{users: map[string]*types.WeComUserBinding{
		"member": {ID: 1, AuthVersion: 1, AccountVersion: 1, Username: "member", PasswordHash: string(hash), Status: types.UserStatusActive},
	}}, "test-secret", 3600, 86400)
	for _, body := range []string{
		`{"current_password":"wrong-password","new_password":"new-secure-password"}`,
		`{"current_password":"current-password","new_password":"short"}`,
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("account_id", int64(1))
		c.Set("auth_version", int64(1))
		c.Request = httptest.NewRequest(http.MethodPut, "/api/user/password", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		handler.HandleChangePassword(c)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d response=%s", body, w.Code, w.Body.String())
		}
	}
}

type testSessionStore struct{ items map[string]*security.Session }

func (s *testSessionStore) CreateSession(_ context.Context, v *security.Session) error {
	s.items[v.ID] = v
	return nil
}
func (s *testSessionStore) GetSession(_ context.Context, id string) (*security.Session, error) {
	return s.items[id], nil
}
func (s *testSessionStore) RevokeSession(_ context.Context, id string, account int64, now time.Time) error {
	if v := s.items[id]; v != nil && v.AccountID == account {
		v.RevokedAt = &now
	}
	return nil
}
func newTestAuthHandler(users credentialUserReader, key string, a, b int) *AuthHandler {
	h := NewAuthHandler(users, key, a, b)
	h.protection = testLoginProtection{}
	h.loginKey = "test-secret"
	h.sessions = &testSessionStore{items: map[string]*security.Session{}}
	return h
}

type testLoginProtection struct{}

func (testLoginProtection) LoginAttempt(context.Context, string, string) (int64, error) {
	return 0, nil
}

func TestLoginProtectionSharesAccountAcrossIPsAndFailsClosed(t *testing.T) {
	server := miniredis.RunT(t)
	protection := cache.NewRedisCache(server.Addr(), "", 0)
	t.Cleanup(func() { protection.Close() })
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	users := credentialReaderStub{users: map[string]*types.WeComUserBinding{"guard-member": {ID: 42, CRMUserID: 42, AuthVersion: 1, Username: "guard-member", PasswordHash: string(hash), Status: types.UserStatusActive}}}
	h := newTestAuthHandler(users, "guard-test-secret", 7200, 604800).WithLoginProtection(protection, "guard-test-secret")
	login := func(i int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(r)
		c.Request = httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"guard-member","password":"wrong-password"}`))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Request.RemoteAddr = fmt.Sprintf("192.0.2.%d:12000", i)
		h.HandleLogin(c)
		return r
	}
	for i := 1; i <= 9; i++ {
		if r := login(i); r.Code != 401 {
			t.Fatalf("attempt %d status %d", i, r.Code)
		}
	}
	denied := login(10)
	if denied.Code != 429 || denied.Header().Get("Retry-After") != "30" {
		t.Fatalf("cooldown %d %v", denied.Code, denied.Header())
	}
	for _, key := range server.Keys() {
		if strings.Contains(key, "guard-member") {
			t.Fatal("username retained in Redis key")
		}
	}
	server.Close()
	if r := login(11); r.Code != 503 {
		t.Fatalf("Redis outage allowed login: %d", r.Code)
	}
}
