package main

import (
	"github.com/lsaac128/kxone/pkg/types"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in loopback server for the real-browser regression suite. It uses an
// isolated temporary SQLite DB and the same HTTP handlers as the API tests.
func TestBrowserRegressionServer(t *testing.T) {
	if os.Getenv("KXONE_BROWSER_REGRESSION") != "1" {
		t.Skip("browser regression server is opt-in")
	}
	fixtures := []*types.Lead{}
	for _, width := range []string{"375", "1440"} {
		for _, source := range []string{"", "主动来电（老王）"} {
			fixtures = append(fixtures, &types.Lead{LeadNo: "SOURCE-" + width + source, CustomerName: "补来源" + width + source, CustomerMobile: "13912345678", SourceChannel: source, Status: types.LeadStatusPendingDispatch, BusinessStage: types.BusinessStageInvitation, ResultStatus: types.LeadResultActive, ProfileVersion: 1, Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()})
		}
	}

	now := time.Now()
	started := now.Add(-2 * time.Hour)
	due := now.Add(-time.Hour)
	contact := started.Add(30 * time.Minute)
	fixtures = append(fixtures,
		&types.Lead{LeadNo: "FOLLOWUP-RETRY-CLOSER", CustomerName: "跟进响应丢失谈单", SourceChannel: "好学校", Status: types.LeadStatusFollowing, BusinessStage: types.BusinessStageNegotiation, OwnershipState: types.OwnershipPrivate, ResultStatus: types.LeadResultActive, AssignedInviterUserID: "inviter_001", InviterUserID: "inviter_001", OwnerUserID: "closer_001", CloserUserID: "closer_001", CurrentOwnerUserID: "closer_001", CurrentOwnerRole: types.OwnerRoleCloser, CurrentOwnerStartedAt: &started, DeliveredAt: &contact, NextFollowupDueAt: &due, CreatedAt: now, UpdatedAt: now},
		&types.Lead{LeadNo: "STAT-CONVERTED", CustomerName: "看板回归成交", SourceChannel: "ad:xiaohongshu:统计校区", Status: types.LeadStatusConverted, AssignedInviterUserID: "inviter_001", InviterUserID: "inviter_001", OwnerUserID: "inviter_001", CloserUserID: "closer_001", CurrentOwnerUserID: "closer_001", CurrentOwnerRole: types.OwnerRoleCloser, CurrentOwnerStartedAt: &started, CurrentOwnerFirstFollowupAt: &contact, FirstFollowupAt: &contact, InviterClaimedAt: &started, CreatedAt: now, UpdatedAt: now},
		&types.Lead{LeadNo: "STAT-FIRST", CustomerName: "看板回归首跟", SourceChannel: "ad:xiaohongshu:统计校区", Status: types.LeadStatusFollowing, AssignedInviterUserID: "inviter_001", InviterUserID: "inviter_001", OwnerUserID: "inviter_001", CurrentOwnerUserID: "inviter_001", CurrentOwnerRole: types.OwnerRoleInviter, CurrentOwnerStartedAt: &started, NextFollowupDueAt: &due, CreatedAt: now, UpdatedAt: now},
		&types.Lead{LeadNo: "STAT-REVISIT", CustomerName: "看板回归回访", SourceChannel: "转介绍", Status: types.LeadStatusFollowing, AssignedInviterUserID: "inviter_002", InviterUserID: "inviter_002", OwnerUserID: "inviter_002", CurrentOwnerUserID: "inviter_002", CurrentOwnerRole: types.OwnerRoleInviter, CurrentOwnerStartedAt: &started, CurrentOwnerFirstFollowupAt: &contact, FirstFollowupAt: &contact, NextFollowupDueAt: &due, CreatedAt: now, UpdatedAt: now},
	)
	model := httptest.NewServer(http.HandlerFunc(browserAgentModelHandler))
	t.Cleanup(model.Close)
	router := newTestRouterWithOptions(t, testRouterOptions{agentModelURL: model.URL, strictAssignments: true, leadFixtures: fixtures, appointmentsEnabled: true})
	adminToken := issueDevToken(t, router, "admin_001", "admin")
	created := doJSON(t, router, http.MethodPost, "/api/admin/users", adminToken, map[string]any{"username": "manager_001", "password": "browser-test-only", "employee_name": "回归主管", "role": "manager", "status": "active"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create browser user: %s", created.Body.String())
	}
	createdOther := doJSON(t, router, http.MethodPost, "/api/admin/users", adminToken, map[string]any{"username": "manager_002", "password": "browser-test-only", "employee_name": "接收主管", "role": "manager", "status": "active"})
	if createdOther.Code != http.StatusCreated {
		t.Fatalf("create second browser manager: %s", createdOther.Body.String())
	}
	for _, u := range []struct{ id, role string }{{"inviter_001", "inviter"}, {"inviter_002", "inviter"}, {"closer_001", "closer"}} {
		response := doJSON(t, router, http.MethodPost, "/api/admin/users", adminToken, map[string]any{"username": u.id, "password": "browser-test-only", "employee_name": u.id, "role": u.role, "status": "active"})
		if response.Code != http.StatusCreated {
			t.Fatal(response.Body.String())
		}
	}
	_ = issueDevToken(t, router, "manager_002", "manager")
	_ = issueDevToken(t, router, "manager_001", "manager")
	tokens := map[string]string{}
	for _, u := range []struct{ id, role string }{{"inviter_001", "inviter"}, {"inviter_002", "inviter"}, {"closer_001", "closer"}} {
		tokens[u.id] = issueDevToken(t, router, u.id, u.role)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/test-session", func(w http.ResponseWriter, r *http.Request) {
		userID, role := "manager_001", "manager"
		switch r.URL.Query().Get("user") {
		case "admin":
			userID, role = "admin_001", "admin"
		case "manager_002":
			userID = "manager_002"
		case "inviter_001", "inviter_002":
			userID, role = r.URL.Query().Get("user"), "inviter"
		case "closer_001":
			userID, role = "closer_001", "closer"
		}
		token := issueDevToken(t, router, userID, role)
		http.SetCookie(w, &http.Cookie{Name: "kxone_refresh_token", Value: "", Path: "/api/auth", MaxAge: -1, HttpOnly: true})

		http.SetCookie(w, &http.Cookie{Name: "kxone_access_token", Value: token, Path: "/", HttpOnly: true})
		http.SetCookie(w, &http.Cookie{Name: "kxone_session", Value: "1", Path: "/"})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/api/", router)
	dist, err := filepath.Abs("../../web/dist")
	if err != nil {
		t.Fatal(err)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			http.FileServer(http.Dir(dist)).ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dist, "index.html"))
	})
	listener, err := net.Listen("tcp", "127.0.0.1:4181")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	t.Log("browser regression server ready on 127.0.0.1:4181")
	if err := http.Serve(listener, mux); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
