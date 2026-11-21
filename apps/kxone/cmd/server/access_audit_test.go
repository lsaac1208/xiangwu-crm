package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestCustomerReadsAreAuditableByAdminOnly(t *testing.T) {
	r := newTestRouter(t)
	admin := issueDevToken(t, r, "audit-admin", "admin")
	member := issueDevToken(t, r, "audit-member", "inviter")
	created := doJSON(t, r, http.MethodPost, "/api/leads", admin, map[string]any{"customer_name": "审计样本", "customer_mobile": "13912345678", "source_channel": "好学校"})
	var fresh struct {
		Lead struct {
			ID int64 `json:"id"`
		} `json:"lead"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &fresh); err != nil || fresh.Lead.ID <= 0 {
		t.Fatal("fixture create failed")
	}
	got := doJSON(t, r, http.MethodGet, "/api/leads?keyword=13912345678&ignored=private-marker", admin, nil)
	if got.Code != 200 {
		t.Fatal(got.Code)
	}
	got = doJSON(t, r, http.MethodGet, "/api/admin/access-audits", admin, nil)
	if got.Code != 200 || !strings.Contains(got.Body.String(), "GET /api/leads") {
		t.Fatalf("missing read audit: %d %s", got.Code, got.Body.String())
	}
	if strings.Contains(got.Body.String(), "private-marker") || strings.Contains(got.Body.String(), "13912345678") || strings.Contains(got.Body.String(), admin) {
		t.Fatal("audit leaked input or credentials")
	}
	filtered := doJSON(t, r, http.MethodGet, fmt.Sprintf("/api/admin/access-audits?lead_id=%d", fresh.Lead.ID), admin, nil)
	if filtered.Code != 200 || !strings.Contains(filtered.Body.String(), fmt.Sprintf("lead:%d", fresh.Lead.ID)) {
		t.Fatalf("listed lead missing in audit: %s", filtered.Body.String())
	}
	if denied := doJSON(t, r, http.MethodGet, "/api/admin/access-audits", member, nil); denied.Code != 403 {
		t.Fatalf("non-admin read audit: %d", denied.Code)
	}
}

func TestPostRecommendationReadIsAudited(t *testing.T) {
	r := newTestRouter(t)
	admin := issueDevToken(t, r, "recommend-audit", "admin")
	created := doJSON(t, r, http.MethodPost, "/api/leads", admin, map[string]any{"customer_name": "推荐审计", "customer_mobile": "13912345678", "source_channel": "好学校"})
	var body struct {
		Lead struct {
			ID int64 `json:"id"`
		} `json:"lead"`
	}
	decodeResponse(t, created, &body)
	response := doJSON(t, r, http.MethodPost, "/api/recommend/sales", admin, map[string]any{"lead_id": body.Lead.ID})
	if response.Code != 200 {
		t.Fatalf("recommend %d %s", response.Code, response.Body.String())
	}
	audits := doJSON(t, r, http.MethodGet, fmt.Sprintf("/api/admin/access-audits?lead_id=%d", body.Lead.ID), admin, nil)
	if audits.Code != 200 || !strings.Contains(audits.Body.String(), "POST /api/recommend/sales") {
		t.Fatalf("POST customer read not audited: %s", audits.Body.String())
	}
}
