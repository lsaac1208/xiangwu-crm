package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestLogoutRevokesOnlyCurrentSession(t *testing.T) {
	r := newTestRouter(t)
	one := issueDevToken(t, r, "session-device", "admin")
	two := issueDevToken(t, r, "session-device", "admin")
	if x := doJSON(t, r, http.MethodPost, "/api/auth/logout", one, nil); x.Code != 200 {
		t.Fatalf("logout: %d", x.Code)
	}
	if x := doJSON(t, r, http.MethodGet, "/api/user/info", one, nil); x.Code != 401 {
		t.Fatalf("logged out access still works: %d", x.Code)
	}
	if x := doJSON(t, r, http.MethodGet, "/api/user/info", two, nil); x.Code != 200 {
		t.Fatalf("other device revoked: %d", x.Code)
	}
}

func TestLogoutInvalidatesBothLoginCredentials(t *testing.T) {
	r := newTestRouter(t)
	admin := issueDevToken(t, r, "logout-admin", "admin")
	created := doJSON(t, r, http.MethodPost, "/api/admin/users", admin, map[string]any{"username": "logout-member", "password": "initial-password", "role": "manager", "status": "active"})
	if created.Code != 201 {
		t.Fatal(created.Code)
	}
	access, refresh := loginTestAccount(t, r, "logout-member", "initial-password")
	if out := doJSON(t, r, http.MethodPost, "/api/auth/logout", access, nil); out.Code != 200 {
		t.Fatal(out.Code)
	}
	if got := doJSON(t, r, http.MethodGet, "/api/user/info", access, nil); got.Code != 401 {
		t.Fatalf("revoked access accepted: %d", got.Code)
	}
	if got := doJSON(t, r, http.MethodPost, "/api/auth/refresh", "", map[string]any{"refresh_token": refresh}); got.Code != 401 {
		t.Fatalf("revoked refresh accepted: %d", got.Code)
	}
}

func TestLogoutWithExpiredAccessStillRevokesItsSession(t *testing.T) {
	r := newTestRouter(t)
	original := issueDevToken(t, r, "expired-logout", "admin")
	token, _, err := jwt.NewParser().ParseUnverified(original, jwt.MapClaims{})
	if err != nil {
		t.Fatal(err)
	}
	claims := token.Claims.(jwt.MapClaims)
	claims["exp"] = time.Now().Add(-time.Hour).Unix()
	expired, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if out := doJSON(t, r, http.MethodPost, "/api/auth/logout", expired, nil); out.Code != 200 {
		t.Fatal(out.Code)
	}
	if got := doJSON(t, r, http.MethodGet, "/api/user/info", original, nil); got.Code != 401 {
		t.Fatalf("expired logout left session active: %d", got.Code)
	}
}
