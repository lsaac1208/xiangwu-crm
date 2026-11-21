package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

func TestNeedsActionAPIRespectsActorAndAllExistingFilters(t *testing.T) {
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	fixtures := []*types.Lead{
		{LeadNo: "api-pending", CustomerName: "triage pending", Status: types.LeadStatusPendingDispatch},
		{LeadNo: "api-claim-a", CustomerName: "triage claim", Status: types.LeadStatusAssignedPendingClaim, AssignedSalesUserID: "triage-a", AssignedInviterUserID: "triage-a", InviterUserID: "triage-b"},
		{LeadNo: "api-due-a", CustomerName: "triage overdue", Status: types.LeadStatusFollowing, OwnerUserID: "triage-a", CurrentOwnerUserID: "triage-a", AssignedSalesUserID: "triage-b", NextFollowupDueAt: &past},
		{LeadNo: "api-notdue-a", CustomerName: "triage future", Status: types.LeadStatusFollowing, OwnerUserID: "triage-a", CurrentOwnerUserID: "triage-a", NextFollowupDueAt: &future},
	}
	for _, lead := range fixtures {
		lead.CreatedAt, lead.UpdatedAt = time.Now(), time.Now()
		types.ProjectLegacyWorkflow(lead)
	}
	router := newTestRouterWithOptions(t, testRouterOptions{leadFixtures: fixtures})
	admin := issueDevToken(t, router, "triage-admin", "admin")
	a := issueDevToken(t, router, "triage-a", "inviter")
	b := issueDevToken(t, router, "triage-b", "inviter")
	for _, tc := range []struct {
		token, path string
		total       int
	}{{admin, "/api/leads?status=needs_action", 3}, {a, "/api/leads?status=needs_action", 2}, {b, "/api/leads?status=needs_action", 0}, {admin, "/api/leads?status=needs_action&keyword=not-a-customer", 0}, {admin, "/api/leads?status=needs_action&sales_userid=triage-a", 2}, {admin, "/api/leads?status=needs_action&start_date=2000-01-01&end_date=2000-01-01", 0}} {
		response := doJSON(t, router, http.MethodGet, tc.path, tc.token, nil)
		if response.Code != 200 {
			t.Fatalf("%s status=%d body=%s", tc.path, response.Code, response.Body.String())
		}
		var body struct {
			Total int
			Leads []types.Lead
		}
		decodeResponse(t, response, &body)
		if body.Total != tc.total || len(body.Leads) != tc.total {
			t.Fatalf("%s total=%d rows=%d want=%d", tc.path, body.Total, len(body.Leads), tc.total)
		}
	}
	workbench := doJSON(t, router, http.MethodGet, "/api/workbench", a, nil)
	var summary struct {
		Tasks []struct {
			LeadNo       string `json:"lead_no"`
			CustomerName string `json:"customer_name"`
		}
	}
	decodeResponse(t, workbench, &summary)
	if len(summary.Tasks) == 0 {
		t.Fatal("missing tasks")
	}
	for _, task := range summary.Tasks {
		if task.LeadNo == "" || task.CustomerName == "" {
			t.Fatal("tasks cannot distinguish customers")
		}
	}
}
