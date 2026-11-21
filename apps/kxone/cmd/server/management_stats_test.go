package main

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

func TestManagementStatsSeparatesRolesAndMatchesCohort(t *testing.T) {
	created := time.Date(2026, 10, 1, 16, 30, 0, 0, time.UTC)
	router := newTestRouterWithOptions(t, testRouterOptions{leadFixtures: []*types.Lead{
		{LeadNo: "MGT-1", SourceChannel: "ad:xiaohongshu:校区号", Status: types.LeadStatusConverted, AssignedInviterUserID: "inviter-a", InviterUserID: "inviter-a", CloserUserID: "closer-a", OwnerUserID: "legacy-wrong", CreatedAt: created, UpdatedAt: created},
		{LeadNo: "MGT-2", SourceChannel: "ad:xiaohongshu:校区号", Status: types.LeadStatusInvalid, AssignedInviterUserID: "inviter-a", CreatedAt: created, UpdatedAt: created},
		{LeadNo: "MGT-3", SourceChannel: "转介绍", Status: types.LeadStatusFollowing, AssignedInviterUserID: "inviter-b", CreatedAt: created.Add(-24 * time.Hour), UpdatedAt: created},
	}})
	token := issueDevToken(t, router, "admin_001", "admin")
	response := doJSON(t, router, http.MethodGet, "/api/stats/management?start_date=2026-10-02&end_date=2026-10-02", token, nil)
	if response.Code != 200 {
		t.Fatalf("report status %d: %s", response.Code, response.Body.String())
	}
	var report struct {
		Assignment struct{ Total, Converted, Invalid int }
		Teams      []struct {
			UserID              string `json:"sales_userid"`
			Role                string
			Assigned, Converted int
		}
		Sources []struct {
			Source, Account           string
			Total, Converted, Invalid int
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Assignment.Total != 2 || report.Assignment.Converted != 1 || report.Assignment.Invalid != 1 {
		t.Fatalf("wrong cohort: %+v", report)
	}
	if len(report.Teams) != 2 {
		t.Fatalf("role attribution must not use legacy-wrong or outside-cohort member: %+v", report.Teams)
	}
	for _, row := range report.Teams {
		if row.Role == "inviter" && (row.UserID != "inviter-a" || row.Assigned != 2 || row.Converted != 1) {
			t.Fatalf("inviter: %+v", row)
		}
		if row.Role == "closer" && (row.UserID != "closer-a" || row.Assigned != 1 || row.Converted != 1) {
			t.Fatalf("closer: %+v", row)
		}
	}
	if len(report.Sources) != 1 || report.Sources[0].Total != 2 || report.Sources[0].Converted != 1 || report.Sources[0].Invalid != 1 {
		t.Fatalf("sources: %+v", report.Sources)
	}
	if report.Sources[0].Source != "小红书" || report.Sources[0].Account != "校区号" {
		t.Fatalf("source/account were merged: %+v", report.Sources)
	}

	denied := doJSON(t, router, http.MethodGet, "/api/stats/management", issueDevToken(t, router, "inviter_001", "inviter"), nil)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("must deny frontline report: %d", denied.Code)
	}
}

func TestSupervisionKeepsCurrentOwnerAndUnknownDeadlinesSeparate(t *testing.T) {
	now := time.Now()
	past := now.Add(-2 * time.Hour)
	old := now.Add(-24 * time.Hour)
	followed := now.Add(-time.Hour)
	appointment := int64(99)
	makeLead := func(no string) *types.Lead {
		return &types.Lead{LeadNo: no, CustomerName: no, Status: types.LeadStatusFollowing, BusinessStage: types.BusinessStageInvitation, OwnershipState: types.OwnershipPrivate, ResultStatus: types.LeadResultActive, AssignedInviterUserID: "previous-owner", InviterUserID: "previous-owner", CurrentOwnerUserID: "current-owner", CurrentOwnerRole: types.OwnerRoleInviter, CurrentOwnerStartedAt: &past, NextFollowupDueAt: &followed, CreatedAt: old, UpdatedAt: now}
	}
	first := makeLead("SUP-FIRST")
	first.FirstFollowupAt = &old
	revisit := makeLead("SUP-REVISIT")
	revisit.CurrentOwnerFirstFollowupAt = &followed
	unknown := makeLead("SUP-UNKNOWN")
	unknown.CurrentOwnerStartedAt = nil
	missing := makeLead("SUP-NODUE")
	missing.NextFollowupDueAt = nil
	pending := makeLead("SUP-PENDING")
	pending.Status = types.LeadStatusAssignedPendingClaim
	pending.OwnershipState = types.OwnershipAssignedPendingClaim
	appointmentLead := makeLead("SUP-APPOINTMENT")
	appointmentLead.CurrentAppointmentID = &appointment
	terminal := makeLead("SUP-DONE")
	terminal.Status = types.LeadStatusConverted
	terminal.ResultStatus = types.LeadResultConverted
	pool := makeLead("SUP-POOL")
	pool.Status = types.LeadStatusPublicPool
	pool.OwnershipState = types.OwnershipPublicPool
	router := newTestRouterWithOptions(t, testRouterOptions{leadFixtures: []*types.Lead{first, revisit, unknown, missing, pending, appointmentLead, terminal, pool}})
	response := doJSON(t, router, http.MethodGet, "/api/stats/supervision", issueDevToken(t, router, "admin_001", "admin"), nil)
	if response.Code != 200 {
		t.Fatalf("supervision %d: %s", response.Code, response.Body.String())
	}
	var data struct {
		Summary struct {
			Overdue int `json:"overdue_followup"`
			First   int `json:"overdue_first_followup"`
			Unknown int `json:"unknown_first_followup"`
			Missing int `json:"missing_deadline"`
			Pending int `json:"pending_claim"`
		}
		Owners []struct {
			Userid  string
			Overdue int `json:"overdue_followup"`
			First   int `json:"overdue_first_followup"`
			Pending int `json:"pending_claim"`
		}
		Tasks []struct {
			Type    string
			LeadID  int64  `json:"lead_id"`
			OwnerID string `json:"owner_userid"`
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	s := data.Summary
	if s.Overdue != 3 || s.First != 1 || s.Unknown != 1 || s.Missing != 1 || s.Pending != 1 {
		t.Fatalf("wrong full-scope summary: %+v", s)
	}
	if len(data.Owners) != 2 || len(data.Tasks) != 5 {
		t.Fatalf("wrong owner/task scope: %+v", data)
	}
	for _, task := range data.Tasks {
		if task.LeadID == first.ID && (task.OwnerID != "current-owner" || task.Type != "overdue_first_followup") {
			t.Fatalf("handoff counted previous owner's first response: %+v", task)
		}
	}
	denied := doJSON(t, router, http.MethodGet, "/api/stats/supervision", issueDevToken(t, router, "closer_001", "closer"), nil)
	if denied.Code != 403 {
		t.Fatalf("closer must not see global supervision: %d", denied.Code)
	}
}

func TestManagementCloserFilterDrillsIntoRealLeadList(t *testing.T) {
	now := time.Now()
	router := newTestRouterWithOptions(t, testRouterOptions{leadFixtures: []*types.Lead{{LeadNo: "MGT-CLOSER", Status: types.LeadStatusConverted, AssignedInviterUserID: "inviter_001", CloserUserID: "closer_001", CreatedAt: now, UpdatedAt: now}}})
	admin := issueDevToken(t, router, "admin_001", "admin")
	created := doJSON(t, router, http.MethodPost, "/api/admin/users", admin, map[string]any{"username": "closer_001", "password": "test-only-password", "employee_name": "测试谈单", "role": "closer", "status": "active"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create closer: %d %s", created.Code, created.Body.String())
	}
	list := doJSON(t, router, http.MethodGet, "/api/leads?sales_userid=closer_001", admin, nil)
	if list.Code != 200 {
		t.Fatalf("closer drilldown must be valid: %d %s", list.Code, list.Body.String())
	}
	var data struct{ Total int }
	if err := json.Unmarshal(list.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data.Total != 1 {
		t.Fatalf("closer filter must constrain list: %+v", data)
	}
	candidates := doJSON(t, router, http.MethodGet, "/api/users/lead-owner-filters", admin, nil)
	var users struct {
		Bindings []struct {
			Role   string
			Userid string `json:"wecom_userid"`
		}
	}
	if err := json.Unmarshal(candidates.Body.Bytes(), &users); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, u := range users.Bindings {
		if u.Userid == "closer_001" && u.Role == "closer" {
			found = true
		}
	}
	if !found {
		t.Fatal("closer missing from read-only owner filters")
	}
}

func TestManagementStatsRejectsFutureHistoricalFirstResponses(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	router := newTestRouterWithOptions(t, testRouterOptions{leadFixtures: []*types.Lead{{LeadNo: "MGT-FUTURE", Status: types.LeadStatusConverted, AssignedInviterUserID: "future-person", InviterUserID: "future-person", InviterClaimedAt: &past, FirstFollowupAt: &future, CreatedAt: now, UpdatedAt: now}}})
	response := doJSON(t, router, http.MethodGet, "/api/stats/management", issueDevToken(t, router, "admin_001", "admin"), nil)
	if response.Code != 200 {
		t.Fatalf("report: %d %s", response.Code, response.Body.String())
	}
	var data struct {
		Teams []struct {
			First   int      `json:"first_followups"`
			Unknown int      `json:"unknown_first_followup"`
			Rate    *float64 `json:"first_followup_rate"`
		}
		Sources []struct {
			First int `json:"first_followups"`
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Teams) != 1 || data.Teams[0].First != 0 || data.Teams[0].Unknown != 1 || data.Teams[0].Rate != nil {
		t.Fatalf("future response counted as success: %+v", data.Teams)
	}
	if len(data.Sources) != 1 || data.Sources[0].First != 0 {
		t.Fatalf("future source response counted as completed: %+v", data.Sources)
	}
}
