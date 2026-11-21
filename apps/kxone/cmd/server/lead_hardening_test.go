package main

import (
	"fmt"
	"github.com/lsaac128/kxone/pkg/types"
	"net/http"
	"testing"
	"time"
)

func TestFollowupPreservesRatingAndTransitionsAfterReassignment(t *testing.T) {
	router := newTestRouter(t)
	manager := issueDevToken(t, router, "cycle_manager", "manager")
	a := issueDevToken(t, router, "cycle_a", "sales")
	b := issueDevToken(t, router, "cycle_b", "sales")
	created := doJSON(t, router, http.MethodPost, "/api/leads", manager, map[string]any{"customer_name": "责任周期回归", "customer_mobile": "13912345678", "source_channel": "好学校", "star_rating": 5})
	if created.Code != 201 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var body struct {
		Lead types.Lead `json:"lead"`
	}
	decodeResponse(t, created, &body)
	id := body.Lead.ID
	path := fmt.Sprintf("/api/leads/%d", id)
	if r := doJSON(t, router, http.MethodPost, path+"/assign", manager, map[string]any{"sales_userid": "cycle_a"}); r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	var lifecycleFirst *time.Time
	for i, actor := range []struct{ name, token string }{{"cycle_a", a}, {"cycle_b", b}, {"cycle_a", a}} {
		if i > 0 {
			if r := doJSON(t, router, http.MethodPost, path+"/reassign", manager, map[string]any{"sales_userid": actor.name, "remark": "交接回归"}); r.Code != 200 {
				t.Fatalf("transfer: %d %s", r.Code, r.Body.String())
			}
		}
		if r := doJSON(t, router, http.MethodPost, path+"/claim", actor.token, nil); r.Code != 200 {
			t.Fatal(r.Body.String())
		}
		followup := map[string]any{"followup_result": "connected", "remark": "保留当前星级", "intention_level": "medium", "is_valid": true}
		if i == 0 {
			followup["star_rating"] = 5
		}
		if r := doJSON(t, router, http.MethodPost, path+"/followups", actor.token, followup); r.Code != 200 {
			t.Fatal(r.Body.String())
		}
		decodeResponse(t, doJSON(t, router, http.MethodGet, path, manager, nil), &body)
		if body.Lead.CurrentOwnerStartedAt == nil || body.Lead.CurrentOwnerFirstFollowupAt == nil || body.Lead.CurrentOwnerFirstFollowupAt.Before(*body.Lead.CurrentOwnerStartedAt) {
			t.Fatalf("invalid owner cycle: %+v", body.Lead)
		}
		if i == 0 {
			lifecycleFirst = body.Lead.FirstFollowupAt
		} else if !lifecycleFirst.Equal(*body.Lead.FirstFollowupAt) {
			t.Fatal("lifecycle first followup was overwritten")
		}
		if body.Lead.StarRating != 5 {
			t.Fatalf("rating changed without request: %d", body.Lead.StarRating)
		}
		if body.Lead.Status != types.LeadStatusFollowing {
			t.Fatalf("cycle %d status=%s", i, body.Lead.Status)
		}
	}
}

func TestBatchSameOwnerRejectsWholeBatch(t *testing.T) {
	router := newTestRouter(t)
	manager := issueDevToken(t, router, "batch_manager", "manager")
	a := issueDevToken(t, router, "batch_a", "sales")
	var leads []types.Lead
	for _, name := range []string{"批量甲", "批量乙"} {
		r := doJSON(t, router, http.MethodPost, "/api/leads", manager, map[string]any{"customer_name": name, "customer_mobile": "13912345678", "source_channel": "好学校"})
		var body struct {
			Lead types.Lead `json:"lead"`
		}
		decodeResponse(t, r, &body)
		path := fmt.Sprintf("/api/leads/%d", body.Lead.ID)
		doJSON(t, router, http.MethodPost, path+"/assign", manager, map[string]any{"sales_userid": "batch_a"})
		doJSON(t, router, http.MethodPost, path+"/claim", a, nil)
		decodeResponse(t, doJSON(t, router, http.MethodGet, path, manager, nil), &body)
		leads = append(leads, body.Lead)
	}
	items := []map[string]any{}
	for _, lead := range leads {
		items = append(items, map[string]any{"lead_id": lead.ID, "version": lead.Version, "profile_version": lead.ProfileVersion})
	}
	r := doJSON(t, router, http.MethodPost, "/api/leads/batch-reassign", manager, map[string]any{"items": items, "target_user_id": "batch_a", "business_stage": "invitation", "remark": "同人应拒绝"})
	if r.Code != 409 {
		t.Fatalf("same owner batch: %d %s", r.Code, r.Body.String())
	}
	for _, lead := range leads {
		var body struct {
			Lead types.Lead `json:"lead"`
		}
		decodeResponse(t, doJSON(t, router, http.MethodGet, fmt.Sprintf("/api/leads/%d", lead.ID), manager, nil), &body)
		if body.Lead.Version != lead.Version || body.Lead.OwnerUserID != lead.OwnerUserID {
			t.Fatal("rejected batch changed ownership")
		}
	}
}

func TestManualContactValidationPreservesUnchangedLegacyPhone(t *testing.T) {
	fixture := &types.Lead{LeadNo: "LEGACY-CONTACT", CustomerName: "旧客户", CustomerMobile: "abc", Status: types.LeadStatusPendingDispatch, Version: 1, ProfileVersion: 1}
	router := newTestRouterWithOptions(t, testRouterOptions{leadFixtures: []*types.Lead{fixture}})
	manager := issueDevToken(t, router, "contact_manager", "manager")
	invalid := doJSON(t, router, http.MethodPost, "/api/leads", manager, map[string]any{"customer_name": "新客户", "customer_mobile": "abc", "source_channel": "好学校"})
	if invalid.Code != 422 {
		t.Fatalf("invalid new phone accepted: %d", invalid.Code)
	}
	payload := map[string]any{"version": 1, "profile_version": 1, "city": "襄阳", "customer_mobile": "abc"}
	updated := doJSON(t, router, http.MethodPatch, fmt.Sprintf("/api/leads/%d/profile", fixture.ID), manager, payload)
	if updated.Code != 200 {
		t.Fatalf("unchanged legacy phone blocked edit: %d %s", updated.Code, updated.Body.String())
	}
	payload["profile_version"] = 2
	payload["customer_mobile"] = "new-invalid"
	updated = doJSON(t, router, http.MethodPatch, fmt.Sprintf("/api/leads/%d/profile", fixture.ID), manager, payload)
	if updated.Code != 422 {
		t.Fatalf("changed invalid phone accepted: %d", updated.Code)
	}
}
