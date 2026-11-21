package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
	"net/http"
	"testing"
)

func loginTestAccount(t *testing.T, router *gin.Engine, username, password string) (string, string) {
	t.Helper()
	response := doJSON(t, router, http.MethodPost, "/api/auth/login", "", map[string]any{"username": username, "password": password})
	if response.Code != http.StatusOK {
		t.Fatalf("login failed: %d", response.Code)
	}
	var access, refresh string
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "kxone_access_token" {
			access = cookie.Value
		}
		if cookie.Name == "kxone_refresh_token" {
			refresh = cookie.Value
		}
	}
	if access == "" || refresh == "" {
		t.Fatal("missing session cookies")
	}
	return access, refresh
}

func TestAccountSecurityChangesRevokeBothCredentials(t *testing.T) {
	for _, change := range []string{"username", "password", "role", "department_id", "status", "self_password"} {
		t.Run(change, func(t *testing.T) {
			router := newTestRouter(t)
			admin := issueDevToken(t, router, "session_admin", "admin")
			response := doJSON(t, router, http.MethodPost, "/api/admin/users", admin, map[string]any{"username": "member", "password": "initial-password", "role": "sales", "status": "active"})
			if response.Code != http.StatusCreated {
				t.Fatalf("create: %d", response.Code)
			}
			var account types.WeComUserBinding
			decodeResponse(t, response, &account)
			access, refresh := loginTestAccount(t, router, "member", "initial-password")
			edit := map[string]any{"expected_version": account.AccountVersion, "username": "member", "role": "sales", "status": "active", "department_id": 0}
			switch change {
			case "username":
				edit[change] = "renamed"
			case "password":
				edit[change] = "updated-password"
			case "role":
				edit[change] = "manager"
			case "department_id":
				edit[change] = 2
			case "status":
				edit[change] = "disabled"
			}
			if change == "self_password" {
				response = doJSON(t, router, http.MethodPut, "/api/user/password", access, map[string]any{"current_password": "initial-password", "new_password": "updated-password"})
			} else {
				response = doJSON(t, router, http.MethodPut, fmt.Sprintf("/api/admin/users/%d", account.ID), admin, edit)
			}
			if response.Code != http.StatusOK {
				t.Fatalf("security change: %d %s", response.Code, response.Body.String())
			}
			if got := doJSON(t, router, http.MethodGet, "/api/user/info", access, nil); got.Code != http.StatusUnauthorized {
				t.Fatalf("old access accepted: %d", got.Code)
			}
			if got := doJSON(t, router, http.MethodPost, "/api/auth/refresh", "", map[string]any{"refresh_token": refresh}); got.Code != http.StatusUnauthorized {
				t.Fatalf("old refresh accepted: %d", got.Code)
			}
			if change == "username" {
				// Reusing a login name must never move an old session to the new account.
				created := doJSON(t, router, http.MethodPost, "/api/admin/users", admin, map[string]any{"username": "member", "password": "different-password", "role": "sales"})
				// Internal actor IDs also remain unique, so name reuse can legitimately be refused.
				if created.Code != http.StatusCreated && created.Code != http.StatusBadRequest {
					t.Fatalf("reuse: %d", created.Code)
				}
				if got := doJSON(t, router, http.MethodPost, "/api/auth/refresh", "", map[string]any{"refresh_token": refresh}); got.Code != 401 {
					t.Fatal("renamed session changed identity")
				}
				loginTestAccount(t, router, "renamed", "initial-password")
			}
		})
	}
}

func TestAccountStaleEditCannotOverwritePasswordOrPermissions(t *testing.T) {
	router := newTestRouter(t)
	admin := issueDevToken(t, router, "admin_security", "admin")
	response := doJSON(t, router, http.MethodPost, "/api/admin/users", admin, map[string]any{"username": "member", "password": "initial-password", "role": "sales"})
	var account types.WeComUserBinding
	decodeResponse(t, response, &account)
	access, _ := loginTestAccount(t, router, "member", "initial-password")
	response = doJSON(t, router, http.MethodPut, "/api/user/password", access, map[string]any{"current_password": "initial-password", "new_password": "updated-password"})
	if response.Code != 200 {
		t.Fatalf("change: %d", response.Code)
	}
	stale := map[string]any{"expected_version": account.AccountVersion, "username": "member", "employee_name": "stale name", "role": "sales", "status": "active"}
	if got := doJSON(t, router, http.MethodPut, fmt.Sprintf("/api/admin/users/%d", account.ID), admin, stale); got.Code != 409 {
		t.Fatalf("stale update: %d", got.Code)
	}
	value, _ := testAccountRepositories.Load(router)
	repository := value.(*repo.GormUserBindingRepository)
	current, err := repository.GetByID(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	stale["expected_version"] = current.AccountVersion
	stale["role"] = "manager"
	if got := doJSON(t, router, http.MethodPut, fmt.Sprintf("/api/admin/users/%d", account.ID), admin, stale); got.Code != 200 {
		t.Fatalf("fresh update: %d", got.Code)
	}
	stale["role"] = "sales"
	if got := doJSON(t, router, http.MethodPut, fmt.Sprintf("/api/admin/users/%d", account.ID), admin, stale); got.Code != 409 {
		t.Fatalf("stale role update: %d", got.Code)
	}
	loginTestAccount(t, router, "member", "updated-password")
	current, _ = repository.GetByID(t.Context(), account.ID)
	if current.Role != types.UserRoleManager {
		t.Fatal("stale edit replaced role")
	}
}
