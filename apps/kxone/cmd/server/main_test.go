package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
	"github.com/lsaac128/kxone/internal/agent"
	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/internal/security"
	httpTransport "github.com/lsaac128/kxone/internal/transport/http"
	"github.com/lsaac128/kxone/internal/usecase"
	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

func TestLeadHTTPWorkflowWithReassign(t *testing.T) {
	router := newTestRouter(t)

	managerToken := issueDevToken(t, router, "manager_001", "manager")
	salesOneToken := issueDevToken(t, router, "sales_001", "sales")
	salesTwoToken := issueDevToken(t, router, "sales_002", "sales")

	createResp := doJSON(t, router, http.MethodPost, "/api/leads", managerToken, map[string]any{
		"customer_name":   "Zhao",
		"customer_mobile": "13912345678",
		"city":            "Hangzhou",
		"product_name":    "Service B",
		"source_channel":  "主动来电（员工丙）",
		"lead_level":      "high",
	})
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create lead status = %d, body = %s", createResp.Code, createResp.Body.String())
	}

	var created struct {
		Lead types.Lead `json:"lead"`
	}
	decodeResponse(t, createResp, &created)
	if created.Lead.Status != types.LeadStatusPendingDispatch {
		t.Fatalf("created status = %s", created.Lead.Status)
	}
	if created.Lead.CustomerMobileMasked != "139****5678" {
		t.Fatalf("masked mobile = %s", created.Lead.CustomerMobileMasked)
	}

	assignResp := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/leads/%d/assign", created.Lead.ID), managerToken, map[string]any{
		"sales_userid": "sales_001",
		"remark":       "first assignment",
	})
	if assignResp.Code != http.StatusOK {
		t.Fatalf("assign status = %d, body = %s", assignResp.Code, assignResp.Body.String())
	}

	reassignResp := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/leads/%d/reassign", created.Lead.ID), managerToken, map[string]any{
		"sales_userid": "sales_002",
		"remark":       "sales_001 busy",
	})
	if reassignResp.Code != http.StatusOK {
		t.Fatalf("reassign status = %d, body = %s", reassignResp.Code, reassignResp.Body.String())
	}

	oldClaimResp := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/leads/%d/claim", created.Lead.ID), salesOneToken, nil)
	if oldClaimResp.Code != http.StatusBadRequest {
		t.Fatalf("old sales claim status = %d, body = %s", oldClaimResp.Code, oldClaimResp.Body.String())
	}

	newClaimResp := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/leads/%d/claim", created.Lead.ID), salesTwoToken, nil)
	if newClaimResp.Code != http.StatusOK {
		t.Fatalf("new sales claim status = %d, body = %s", newClaimResp.Code, newClaimResp.Body.String())
	}

	followupResp := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/leads/%d/followups", created.Lead.ID), salesTwoToken, map[string]any{
		"followup_result": "connected",
		"intention_level": "high",
		"remark":          "Customer answered.",
	})
	if followupResp.Code != http.StatusOK {
		t.Fatalf("followup status = %d, body = %s", followupResp.Code, followupResp.Body.String())
	}

	detailResp := doJSON(t, router, http.MethodGet, fmt.Sprintf("/api/leads/%d", created.Lead.ID), salesTwoToken, nil)
	if detailResp.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", detailResp.Code, detailResp.Body.String())
	}

	var detail struct {
		Lead            types.Lead                 `json:"lead"`
		Followups       []types.LeadFollowup       `json:"followups"`
		DispatchRecords []types.LeadDispatchRecord `json:"dispatch_records"`
	}
	decodeResponse(t, detailResp, &detail)

	if detail.Lead.Status != types.LeadStatusFollowing {
		t.Fatalf("final status = %s", detail.Lead.Status)
	}
	if detail.Lead.AssignedSalesUserID != "sales_002" || detail.Lead.OwnerUserID != "sales_002" {
		t.Fatalf("final owner mismatch: assigned=%s owner=%s", detail.Lead.AssignedSalesUserID, detail.Lead.OwnerUserID)
	}
	if detail.Lead.CustomerMobile != "13912345678" {
		t.Fatalf("sales detail mobile = %q, want full mobile", detail.Lead.CustomerMobile)
	}
	if len(detail.Followups) != 1 {
		t.Fatalf("followups count = %d", len(detail.Followups))
	}
	if !detail.Followups[0].IsValid {
		t.Fatalf("followup default is_valid = false")
	}
	if len(detail.DispatchRecords) != 3 {
		t.Fatalf("dispatch records count = %d", len(detail.DispatchRecords))
	}
	if detail.DispatchRecords[0].ActionType != "claim" || detail.DispatchRecords[1].ActionType != "reassign" || detail.DispatchRecords[2].ActionType != "assign" {
		t.Fatalf("dispatch action order = %s, %s", detail.DispatchRecords[0].ActionType, detail.DispatchRecords[1].ActionType)
	}
}

func TestAdminLeadRecycleBinAccess(t *testing.T) {
	router := newTestRouter(t)
	adminToken := issueDevToken(t, router, "admin_001", "admin")
	managerToken := issueDevToken(t, router, "manager_001", "manager")
	createdResp := doJSON(t, router, http.MethodPost, "/api/leads", adminToken, map[string]any{
		"customer_name": "删除测试", "customer_mobile": "13912345678", "product_name": "测试",
		"source_channel": "主动来电（员工丙）", "lead_level": "high",
	})
	if createdResp.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", createdResp.Code, createdResp.Body.String())
	}
	var created struct {
		Lead types.Lead `json:"lead"`
	}
	decodeResponse(t, createdResp, &created)
	path := fmt.Sprintf("/api/leads/%d", created.Lead.ID)
	request := map[string]any{"lead_no": created.Lead.LeadNo, "expected_version": created.Lead.Version, "expected_profile_version": created.Lead.ProfileVersion, "reason": "清理验收测试"}
	if response := doJSON(t, router, http.MethodDelete, path, managerToken, request); response.Code != http.StatusForbidden {
		t.Fatalf("manager delete: %d", response.Code)
	}
	wrong := map[string]any{"lead_no": "wrong", "expected_version": created.Lead.Version, "expected_profile_version": created.Lead.ProfileVersion, "reason": "清理验收测试"}
	if response := doJSON(t, router, http.MethodDelete, path, adminToken, wrong); response.Code != http.StatusConflict {
		t.Fatalf("wrong lead no: %d %s", response.Code, response.Body.String())
	}
	if response := doJSON(t, router, http.MethodDelete, path, adminToken, request); response.Code != http.StatusNoContent {
		t.Fatalf("admin delete: %d %s", response.Code, response.Body.String())
	}
	if response := doJSON(t, router, http.MethodGet, path, adminToken, nil); response.Code != http.StatusNotFound {
		t.Fatalf("deleted detail: %d %s", response.Code, response.Body.String())
	}
	if response := doJSON(t, router, http.MethodGet, "/api/admin/deleted-leads", managerToken, nil); response.Code != http.StatusForbidden {
		t.Fatalf("manager recycle bin: %d", response.Code)
	}
	listed := doJSON(t, router, http.MethodGet, "/api/admin/deleted-leads", adminToken, nil)
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), created.Lead.LeadNo) {
		t.Fatalf("admin recycle bin: %d %s", listed.Code, listed.Body.String())
	}
	restorePath := fmt.Sprintf("/api/admin/deleted-leads/%d/restore", created.Lead.ID)
	if response := doJSON(t, router, http.MethodPost, restorePath, managerToken, map[string]any{"reason": "误删"}); response.Code != http.StatusForbidden {
		t.Fatalf("manager restore: %d", response.Code)
	}
	if response := doJSON(t, router, http.MethodPost, restorePath, adminToken, map[string]any{"reason": "误删"}); response.Code != http.StatusOK {
		t.Fatalf("admin restore: %d %s", response.Code, response.Body.String())
	}
	if response := doJSON(t, router, http.MethodGet, path, adminToken, nil); response.Code != http.StatusOK {
		t.Fatalf("restored detail: %d %s", response.Code, response.Body.String())
	}
}

func TestLeadProfileHTTPUpdateAuditConflictAndDispatcherDenial(t *testing.T) {
	router := newTestRouter(t)
	managerToken := issueDevToken(t, router, "manager_profile", string(types.UserRoleManager))
	dispatcherToken := issueDevToken(t, router, "dispatcher_profile", string(types.UserRoleDispatcher))
	createdResp := doJSON(t, router, http.MethodPost, "/api/leads", managerToken, map[string]any{
		"customer_name": "原客户", "customer_mobile": "13912345678", "city": "仙桃", "product_name": "旧项目", "source_channel": "主动来电（员工丙）",
	})
	if createdResp.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createdResp.Code, createdResp.Body.String())
	}
	var created struct {
		Lead types.Lead `json:"lead"`
	}
	decodeResponse(t, createdResp, &created)

	updateResp := doJSON(t, router, http.MethodPatch, fmt.Sprintf("/api/leads/%d/profile", created.Lead.ID), managerToken, map[string]any{
		"version": created.Lead.Version, "profile_version": created.Lead.ProfileVersion,
		"customer_name": "新客户", "customer_mobile": "+86 138-0013-8000", "city": "武汉", "product_name": "高考复读",
	})
	if updateResp.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateResp.Code, updateResp.Body.String())
	}
	var updated usecase.UpdateLeadProfileResult
	decodeResponse(t, updateResp, &updated)
	if updated.Lead.Version != created.Lead.Version || updated.Lead.ProfileVersion != created.Lead.ProfileVersion+1 || updated.Lead.CustomerMobile != "13800138000" {
		t.Fatalf("updated lead=%+v", updated.Lead)
	}

	staleResp := doJSON(t, router, http.MethodPatch, fmt.Sprintf("/api/leads/%d/profile", created.Lead.ID), managerToken, map[string]any{
		"version": created.Lead.Version, "profile_version": created.Lead.ProfileVersion, "city": "北京",
	})
	if staleResp.Code != http.StatusConflict || !strings.Contains(staleResp.Body.String(), "lead_version_conflict") {
		t.Fatalf("stale status=%d body=%s", staleResp.Code, staleResp.Body.String())
	}

	dispatcherResp := doJSON(t, router, http.MethodPatch, fmt.Sprintf("/api/leads/%d/profile", created.Lead.ID), dispatcherToken, map[string]any{
		"version": updated.Lead.Version, "profile_version": updated.Lead.ProfileVersion, "city": "上海",
	})
	if dispatcherResp.Code != http.StatusForbidden {
		t.Fatalf("dispatcher status=%d body=%s", dispatcherResp.Code, dispatcherResp.Body.String())
	}

	detailResp := doJSON(t, router, http.MethodGet, fmt.Sprintf("/api/leads/%d", created.Lead.ID), managerToken, nil)
	var detail struct {
		Activities []types.LeadActivityRecord `json:"activities"`
	}
	decodeResponse(t, detailResp, &detail)
	if len(detail.Activities) != 1 || detail.Activities[0].ActivityType != types.LeadActivityProfileUpdated || strings.Contains(detail.Activities[0].ToValueJSON, "13800138000") {
		t.Fatalf("activities=%+v", detail.Activities)
	}
}

func TestDispatcherRoleHTTPPermissionsScopeMaskingAndImmutableHistory(t *testing.T) {
	router := newTestRouter(t)
	adminToken := issueDevToken(t, router, "admin_dispatcher_test", string(types.UserRoleAdmin))

	createDispatcher := doJSON(t, router, http.MethodPost, "/api/admin/users", adminToken, map[string]any{
		"username":      "dispatcher_login",
		"password":      "dispatcher-password",
		"employee_name": "线索调度员",
		"role":          string(types.UserRoleDispatcher),
		"status":        string(types.UserStatusActive),
	})
	if createDispatcher.Code != http.StatusCreated {
		t.Fatalf("create dispatcher status=%d body=%s", createDispatcher.Code, createDispatcher.Body.String())
	}
	var dispatcherBinding types.WeComUserBinding
	decodeResponse(t, createDispatcher, &dispatcherBinding)
	if dispatcherBinding.ID <= 0 || dispatcherBinding.WeComUserID != "dispatcher_login" || dispatcherBinding.Role != types.UserRoleDispatcher {
		t.Fatalf("dispatcher binding=%+v", dispatcherBinding)
	}

	createInviter := doJSON(t, router, http.MethodPost, "/api/admin/users", adminToken, map[string]any{
		"username":      "dispatcher_inviter",
		"password":      "inviter-password",
		"employee_name": "测试邀约师",
		"role":          string(types.UserRoleInviter),
		"status":        string(types.UserStatusActive),
	})
	if createInviter.Code != http.StatusCreated {
		t.Fatalf("create inviter status=%d body=%s", createInviter.Code, createInviter.Body.String())
	}

	login := func(username string) *httptest.ResponseRecorder {
		return doJSON(t, router, http.MethodPost, "/api/auth/login", "", map[string]any{
			"username": username,
			"password": "dispatcher-password",
		})
	}
	accessToken := func(response *httptest.ResponseRecorder) string {
		t.Helper()
		for _, cookie := range response.Result().Cookies() {
			if cookie.Name == "kxone_access_token" {
				return cookie.Value
			}
		}
		t.Fatalf("access token cookie missing: %v", response.Result().Cookies())
		return ""
	}

	loginResponse := login("dispatcher_login")
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("dispatcher login status=%d body=%s", loginResponse.Code, loginResponse.Body.String())
	}
	dispatcherToken := accessToken(loginResponse)

	createLead := doJSON(t, router, http.MethodPost, "/api/leads", dispatcherToken, map[string]any{
		"customer_name":          "调度员新建客户",
		"customer_mobile":        "13912345678",
		"customer_mobile_masked": "13912345678",
		"customer_wechat":        "wx_dispatcher_customer",
		"customer_wechat_masked": "wx_dispatcher_customer",
		"city":                   "武汉",
		"product_name":           "课程咨询",
		"source_channel":         "主动来电（员工丙）",
	})
	if createLead.Code != http.StatusCreated {
		t.Fatalf("dispatcher create lead status=%d body=%s", createLead.Code, createLead.Body.String())
	}
	var created struct {
		Lead types.Lead `json:"lead"`
	}
	decodeResponse(t, createLead, &created)
	if created.Lead.Status != types.LeadStatusPendingDispatch || created.Lead.CreatedByUserID != "dispatcher_login" || created.Lead.CreatedByRole != types.UserRoleDispatcher {
		t.Fatalf("created lead workflow=%+v", created.Lead)
	}
	if created.Lead.DispatchManagerUserID != "" {
		t.Fatalf("dispatcher became business supervisor: %+v", created.Lead)
	}
	if created.Lead.CustomerMobile != "" || created.Lead.CustomerWechat != "" || created.Lead.CustomerMobileMasked == "" || created.Lead.CustomerWechatMasked == "" {
		t.Fatalf("dispatcher create response leaked contacts: %+v", created.Lead)
	}
	if created.Lead.CustomerMobileMasked != "139****5678" || created.Lead.CustomerWechatMasked != "wx****er" {
		t.Fatalf("dispatcher masks were not derived server-side: %+v", created.Lead)
	}

	inviters := doJSON(t, router, http.MethodGet, "/api/users/inviters", dispatcherToken, nil)
	if inviters.Code != http.StatusOK || !strings.Contains(inviters.Body.String(), "dispatcher_inviter") {
		t.Fatalf("dispatcher inviter list status=%d body=%s", inviters.Code, inviters.Body.String())
	}
	pending := doJSON(t, router, http.MethodGet, "/api/leads?dispatcher_scope=pending", dispatcherToken, nil)
	if pending.Code != http.StatusOK {
		t.Fatalf("dispatcher pending status=%d body=%s", pending.Code, pending.Body.String())
	}
	var pendingList struct {
		Leads []types.Lead `json:"Leads"`
		Total int          `json:"Total"`
	}
	decodeResponse(t, pending, &pendingList)
	if pendingList.Total != 1 || len(pendingList.Leads) != 1 || pendingList.Leads[0].CustomerMobile != "" || pendingList.Leads[0].CustomerWechat != "" {
		t.Fatalf("dispatcher pending response=%+v", pendingList)
	}
	invalidScope := doJSON(t, router, http.MethodGet, "/api/leads?dispatcher_scope=invalid", dispatcherToken, nil)
	if invalidScope.Code != http.StatusBadRequest {
		t.Fatalf("invalid dispatcher scope status=%d body=%s", invalidScope.Code, invalidScope.Body.String())
	}
	nonDispatcherScope := doJSON(t, router, http.MethodGet, "/api/leads?dispatcher_scope=pending", adminToken, nil)
	if nonDispatcherScope.Code != http.StatusBadRequest {
		t.Fatalf("non-dispatcher scope status=%d body=%s", nonDispatcherScope.Code, nonDispatcherScope.Body.String())
	}

	assign := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/leads/%d/assign", created.Lead.ID), dispatcherToken, map[string]any{
		"sales_userid": "dispatcher_inviter",
		"remark":       "调度员首次分配",
	})
	if assign.Code != http.StatusOK {
		t.Fatalf("dispatcher assign status=%d body=%s", assign.Code, assign.Body.String())
	}
	otherDispatcherToken := issueDevToken(t, router, "dispatcher_other", string(types.UserRoleDispatcher))
	secondAssign := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/leads/%d/assign", created.Lead.ID), otherDispatcherToken, map[string]any{
		"sales_userid": "dispatcher_inviter",
	})
	if secondAssign.Code != http.StatusBadRequest {
		t.Fatalf("second dispatcher assignment status=%d body=%s", secondAssign.Code, secondAssign.Body.String())
	}
	otherDetail := doJSON(t, router, http.MethodGet, fmt.Sprintf("/api/leads/%d", created.Lead.ID), otherDispatcherToken, nil)
	if otherDetail.Code != http.StatusForbidden {
		t.Fatalf("other dispatcher detail status=%d body=%s", otherDetail.Code, otherDetail.Body.String())
	}

	for _, endpoint := range []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodPost, fmt.Sprintf("/api/leads/%d/self-claim", created.Lead.ID), nil},
		{http.MethodPost, fmt.Sprintf("/api/leads/%d/reassign", created.Lead.ID), map[string]any{"sales_userid": "dispatcher_inviter"}},
		{http.MethodPost, fmt.Sprintf("/api/leads/%d/claim", created.Lead.ID), nil},
		{http.MethodPost, fmt.Sprintf("/api/leads/%d/followups", created.Lead.ID), map[string]any{"followup_result": "test"}},
		{http.MethodPost, fmt.Sprintf("/api/leads/%d/convert", created.Lead.ID), nil},
		{http.MethodPost, fmt.Sprintf("/api/leads/%d/close", created.Lead.ID), nil},
		{http.MethodPost, fmt.Sprintf("/api/leads/%d/reassign-closer", created.Lead.ID), map[string]any{"closer_userid": "closer-1"}},
		{http.MethodGet, fmt.Sprintf("/api/leads/%d/recommend-sales", created.Lead.ID), nil},
		{http.MethodPost, fmt.Sprintf("/api/leads/%d/auto-assign", created.Lead.ID), nil},
		{http.MethodPost, "/api/recommend/sales", map[string]any{"lead_id": created.Lead.ID}},
		{http.MethodGet, "/api/users/sales", nil},
		{http.MethodGet, "/api/public-pool", nil},
		{http.MethodPost, fmt.Sprintf("/api/public-pool/%d/claim", created.Lead.ID), nil},
		{http.MethodPost, fmt.Sprintf("/api/public-pool/%d/enter", created.Lead.ID), map[string]any{"reason": "manual"}},
		{http.MethodGet, "/api/appointments", nil},
		{http.MethodPost, "/api/appointments", map[string]any{"lead_id": created.Lead.ID}},
		{http.MethodGet, "/api/channel-integrations", nil},
		{http.MethodPost, "/api/channel-integrations", map[string]any{"platform": "baidu", "account_name": "forbidden"}},
		{http.MethodGet, "/api/stats/dashboard", nil},
		{http.MethodGet, "/api/admin/settings", nil},
	} {
		response := doJSON(t, router, endpoint.method, endpoint.path, dispatcherToken, endpoint.body)
		if response.Code != http.StatusForbidden {
			t.Fatalf("dispatcher forbidden endpoint %s %s status=%d body=%s", endpoint.method, endpoint.path, response.Code, response.Body.String())
		}
	}

	rename := doJSON(t, router, http.MethodPut, fmt.Sprintf("/api/admin/users/%d", dispatcherBinding.ID), adminToken, map[string]any{
		"expected_version": dispatcherBinding.AccountVersion,
		"username":         "dispatcher_renamed",
		"employee_name":    "线索调度员（改名）",
		"role":             string(types.UserRoleDispatcher),
		"status":           string(types.UserStatusActive),
	})
	if rename.Code != http.StatusOK {
		t.Fatalf("rename dispatcher status=%d body=%s", rename.Code, rename.Body.String())
	}
	var renamed types.WeComUserBinding
	decodeResponse(t, rename, &renamed)
	if renamed.Username != "dispatcher_renamed" || renamed.WeComUserID != "dispatcher_login" {
		t.Fatalf("renamed dispatcher changed immutable account id: %+v", renamed)
	}

	newLogin := login("dispatcher_renamed")
	if newLogin.Code != http.StatusOK {
		t.Fatalf("renamed dispatcher login status=%d body=%s", newLogin.Code, newLogin.Body.String())
	}
	renamedToken := accessToken(newLogin)
	handled := doJSON(t, router, http.MethodGet, "/api/leads?dispatcher_scope=handled", renamedToken, nil)
	if handled.Code != http.StatusOK {
		t.Fatalf("renamed dispatcher handled status=%d body=%s", handled.Code, handled.Body.String())
	}
	var handledList struct {
		Leads []types.Lead `json:"Leads"`
		Total int          `json:"Total"`
	}
	decodeResponse(t, handled, &handledList)
	if handledList.Total != 1 || len(handledList.Leads) != 1 || handledList.Leads[0].ID != created.Lead.ID {
		t.Fatalf("renamed dispatcher lost history: %+v", handledList)
	}
	defaultAll := doJSON(t, router, http.MethodGet, "/api/leads", renamedToken, nil)
	if defaultAll.Code != http.StatusOK || !strings.Contains(defaultAll.Body.String(), created.Lead.LeadNo) {
		t.Fatalf("dispatcher default all status=%d body=%s", defaultAll.Code, defaultAll.Body.String())
	}
	detail := doJSON(t, router, http.MethodGet, fmt.Sprintf("/api/leads/%d", created.Lead.ID), renamedToken, nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("renamed dispatcher detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	var detailBody struct {
		Lead types.Lead `json:"lead"`
	}
	decodeResponse(t, detail, &detailBody)
	if detailBody.Lead.CustomerMobile != "" || detailBody.Lead.CustomerWechat != "" || detailBody.Lead.CustomerMobileMasked == "" || detailBody.Lead.CustomerWechatMasked == "" {
		t.Fatalf("dispatcher detail leaked contacts: %+v", detailBody.Lead)
	}
	oldLogin := login("dispatcher_login")
	if oldLogin.Code != http.StatusUnauthorized {
		t.Fatalf("old username login status=%d body=%s", oldLogin.Code, oldLogin.Body.String())
	}
}

func TestLeadHTTPWorkflowCreateAssignClaimFollowupConvert(t *testing.T) {
	router := newTestRouter(t)

	managerToken := issueDevToken(t, router, "manager_001", "manager")
	salesToken := issueDevToken(t, router, "sales_001", "sales")

	createResp := doJSON(t, router, http.MethodPost, "/api/leads", managerToken, map[string]any{
		"customer_name":   "Full Flow",
		"customer_mobile": "13912345678",
		"city":            "Hangzhou",
		"product_name":    "Service A",
		"source_channel":  "主动来电（员工丙）",
		"lead_level":      "high",
	})
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create lead status = %d, body = %s", createResp.Code, createResp.Body.String())
	}
	var created struct {
		Lead types.Lead `json:"lead"`
	}
	decodeResponse(t, createResp, &created)

	assignResp := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/leads/%d/assign", created.Lead.ID), managerToken, map[string]any{
		"sales_userid": "sales_001",
	})
	if assignResp.Code != http.StatusOK {
		t.Fatalf("assign status = %d, body = %s", assignResp.Code, assignResp.Body.String())
	}

	claimResp := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/leads/%d/claim", created.Lead.ID), salesToken, nil)
	if claimResp.Code != http.StatusOK {
		t.Fatalf("claim status = %d, body = %s", claimResp.Code, claimResp.Body.String())
	}

	followupResp := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/leads/%d/followups", created.Lead.ID), salesToken, map[string]any{
		"followup_result": "interested",
		"intention_level": "high",
		"remark":          "Customer is interested.",
	})
	if followupResp.Code != http.StatusOK {
		t.Fatalf("followup status = %d, body = %s", followupResp.Code, followupResp.Body.String())
	}

	convertResp := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/leads/%d/convert", created.Lead.ID), salesToken, map[string]any{
		"deal_amount": 8800,
	})
	if convertResp.Code != http.StatusOK {
		t.Fatalf("convert status = %d, body = %s", convertResp.Code, convertResp.Body.String())
	}

	detailResp := doJSON(t, router, http.MethodGet, fmt.Sprintf("/api/leads/%d", created.Lead.ID), managerToken, nil)
	if detailResp.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", detailResp.Code, detailResp.Body.String())
	}
	var detail struct {
		Lead      types.Lead           `json:"lead"`
		Followups []types.LeadFollowup `json:"followups"`
	}
	decodeResponse(t, detailResp, &detail)
	if detail.Lead.Status != types.LeadStatusConverted || detail.Lead.DealAmount != 8800 || detail.Lead.ConvertedAt == nil {
		t.Fatalf("converted lead mismatch: %+v", detail.Lead)
	}
	if len(detail.Followups) != 1 || detail.Lead.FirstFollowupAt == nil {
		t.Fatalf("followup state mismatch: lead=%+v followups=%+v", detail.Lead, detail.Followups)
	}
}

func TestAdminCanAssignManagerLead(t *testing.T) {
	router := newTestRouter(t)

	managerToken := issueDevToken(t, router, "manager_001", "manager")
	adminToken := issueDevToken(t, router, "admin_001", "admin")

	createResp := doJSON(t, router, http.MethodPost, "/api/leads", managerToken, map[string]any{
		"customer_name":   "Admin Assign",
		"customer_mobile": "13912345678",
		"city":            "Hangzhou",
		"product_name":    "Service B",
		"source_channel":  "主动来电（员工丙）",
		"lead_level":      "high",
	})
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create lead status = %d, body = %s", createResp.Code, createResp.Body.String())
	}

	var created struct {
		Lead types.Lead `json:"lead"`
	}
	decodeResponse(t, createResp, &created)
	if created.Lead.DispatchManagerUserID != "manager_001" {
		t.Fatalf("dispatch manager = %s", created.Lead.DispatchManagerUserID)
	}

	assignResp := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/leads/%d/assign", created.Lead.ID), adminToken, map[string]any{
		"sales_userid": "sales_001",
		"remark":       "admin assignment",
	})
	if assignResp.Code != http.StatusOK {
		t.Fatalf("admin assign status = %d, body = %s", assignResp.Code, assignResp.Body.String())
	}

	detailResp := doJSON(t, router, http.MethodGet, fmt.Sprintf("/api/leads/%d", created.Lead.ID), adminToken, nil)
	if detailResp.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", detailResp.Code, detailResp.Body.String())
	}
	var detail struct {
		Lead types.Lead `json:"lead"`
	}
	decodeResponse(t, detailResp, &detail)
	if detail.Lead.Status != types.LeadStatusAssignedPendingClaim || detail.Lead.AssignedSalesUserID != "sales_001" {
		t.Fatalf("lead assignment mismatch: %+v", detail.Lead)
	}
	if detail.Lead.CustomerMobile != "13912345678" {
		t.Fatalf("admin detail mobile = %q, want full mobile", detail.Lead.CustomerMobile)
	}
}

func TestPublicWebsiteLeadFormRequiresAllowedOrigin(t *testing.T) {
	router := newTestRouter(t)
	body := []byte(`{"studentName":"网站学生","phone":"13912345678","grade":"高三应届","message":"咨询方向：高三文化课","website":"","manager":"attacker-selected-manager","contentEntryId":501,"contentRevisionId":601,"contentPath":"/form-preview-test/"}`)
	withoutOrigin := doRaw(t, router, http.MethodPost, "/api/public/leads/form", "", body, map[string]string{"Content-Type": "application/json"})
	if withoutOrigin.Code != http.StatusForbidden {
		t.Fatalf("public form without origin status = %d, body = %s", withoutOrigin.Code, withoutOrigin.Body.String())
	}
	allowed := doRaw(t, router, http.MethodPost, "/api/public/leads/form", "", body, map[string]string{"Content-Type": "application/json", "Origin": "https://kxone.example.com"})
	if allowed.Code != http.StatusCreated || !strings.Contains(allowed.Body.String(), `"ok":true`) {
		t.Fatalf("public form status = %d, body = %s", allowed.Code, allowed.Body.String())
	}
	var created struct {
		LeadID int64 `json:"lead_id"`
	}
	decodeResponse(t, allowed, &created)
	adminToken := issueDevToken(t, router, "admin_001", "admin")
	detail := doJSON(t, router, http.MethodGet, fmt.Sprintf("/api/leads/%d", created.LeadID), adminToken, nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("public form lead detail status = %d, body = %s", detail.Code, detail.Body.String())
	}
	var leadDetail struct {
		Lead types.Lead `json:"lead"`
	}
	decodeResponse(t, detail, &leadDetail)
	if leadDetail.Lead.DispatchManagerUserID != "manager1" {
		t.Fatalf("public form manager = %q, want configured default", leadDetail.Lead.DispatchManagerUserID)
	}
	if leadDetail.Lead.ContentEntryID == nil || *leadDetail.Lead.ContentEntryID != 501 || leadDetail.Lead.ContentRevisionID == nil || *leadDetail.Lead.ContentRevisionID != 601 || leadDetail.Lead.ContentPath != "/form-preview-test/" {
		t.Fatalf("public form attribution = %+v", leadDetail.Lead)
	}
	botBody := []byte(`{"studentName":"Bot","phone":"13900000000","website":"https://spam.example"}`)
	honeypot := doRaw(t, router, http.MethodPost, "/api/public/leads/form", "", botBody, map[string]string{"Content-Type": "application/json", "Origin": "https://kxone.example.com"})
	if honeypot.Code != http.StatusOK || !strings.Contains(honeypot.Body.String(), `"invalid":true`) {
		t.Fatalf("public form honeypot status = %d, body = %s", honeypot.Code, honeypot.Body.String())
	}
}

func issueAccountAccessToken(t *testing.T, user *types.WeComUserBinding, sid string) string {
	t.Helper()
	claims := &httpTransport.JWTClaims{SessionID: sid,
		AuthVersion: user.AuthVersion,
		UserID:      user.CRMUserID,
		WeComUserID: user.WeComUserID,
		Role:        string(user.Role),
		TokenType:   "access",
		RegisteredClaims: jwt.RegisteredClaims{Subject: fmt.Sprint(user.ID),
			Issuer:    "kxone",
			Audience:  jwt.ClaimStrings{"kxone-api"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("sign bound access token: %v", err)
	}
	return token
}

func TestMetricsRequiresAdminAuth(t *testing.T) {
	router := newTestRouter(t)

	noTokenResp := doJSON(t, router, http.MethodGet, "/metrics", "", nil)
	if noTokenResp.Code != http.StatusUnauthorized {
		t.Fatalf("metrics without token status = %d, body = %s", noTokenResp.Code, noTokenResp.Body.String())
	}

	managerToken := issueDevToken(t, router, "manager_001", "manager")
	managerResp := doJSON(t, router, http.MethodGet, "/metrics", managerToken, nil)
	if managerResp.Code != http.StatusForbidden {
		t.Fatalf("metrics with manager token status = %d, body = %s", managerResp.Code, managerResp.Body.String())
	}

	adminToken := issueDevToken(t, router, "admin_001", "admin")
	adminResp := doJSON(t, router, http.MethodGet, "/metrics", adminToken, nil)
	if adminResp.Code != http.StatusOK {
		t.Fatalf("metrics with admin token status = %d, body = %s", adminResp.Code, adminResp.Body.String())
	}

	adminAPIResp := doJSON(t, router, http.MethodGet, "/api/admin/metrics", adminToken, nil)
	if adminAPIResp.Code != http.StatusOK {
		t.Fatalf("admin metrics endpoint status = %d, body = %s", adminAPIResp.Code, adminAPIResp.Body.String())
	}
}

func TestAdminSettingsErrorsAreSanitized(t *testing.T) {
	router := newTestRouter(t)
	adminToken := issueDevToken(t, router, "admin_001", "admin")

	resp := doJSON(t, router, http.MethodPut, "/api/admin/settings", adminToken, map[string]any{
		"settings": []map[string]any{
			{
				"key":   "reminder.dispatch_minutes",
				"value": "abc",
				"group": "reminder",
			},
		},
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("update settings status = %d, body = %s", resp.Code, resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), "comma-separated") || strings.Contains(resp.Body.String(), "details") {
		t.Fatalf("admin error leaked implementation detail: %s", resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "invalid_setting_value") {
		t.Fatalf("expected sanitized error code, got %s", resp.Body.String())
	}
}

func TestLeadResponsesUseBoundEmployeeName(t *testing.T) {
	router := newTestRouter(t)

	adminToken := issueDevToken(t, router, "admin_001", "admin")
	managerToken := issueDevToken(t, router, "manager_001", "manager")

	bindingResp := doJSON(t, router, http.MethodPost, "/api/admin/users", adminToken, map[string]any{
		"username":      "sales_001",
		"password":      "test-password",
		"employee_name": "王二",
		"role":          "sales",
	})
	if bindingResp.Code != http.StatusCreated {
		t.Fatalf("create binding status = %d, body = %s", bindingResp.Code, bindingResp.Body.String())
	}

	salesToken := issueDevToken(t, router, "sales_001", "sales")
	createResp := doJSON(t, router, http.MethodPost, "/api/leads", managerToken, map[string]any{
		"customer_name":   "Name Display",
		"customer_mobile": "13912345678",
		"city":            "杭州",
		"product_name":    "课程",
		"source_channel":  "主动来电（员工丙）",
	})
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create lead status = %d, body = %s", createResp.Code, createResp.Body.String())
	}

	var created struct {
		Lead types.Lead `json:"lead"`
	}
	decodeResponse(t, createResp, &created)

	assignResp := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/leads/%d/assign", created.Lead.ID), managerToken, map[string]any{
		"sales_userid": "sales_001",
	})
	if assignResp.Code != http.StatusOK {
		t.Fatalf("assign status = %d, body = %s", assignResp.Code, assignResp.Body.String())
	}

	claimResp := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/leads/%d/claim", created.Lead.ID), salesToken, nil)
	if claimResp.Code != http.StatusOK {
		t.Fatalf("claim status = %d, body = %s", claimResp.Code, claimResp.Body.String())
	}

	detailResp := doJSON(t, router, http.MethodGet, fmt.Sprintf("/api/leads/%d", created.Lead.ID), salesToken, nil)
	if detailResp.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", detailResp.Code, detailResp.Body.String())
	}
	var detail struct {
		Lead types.Lead `json:"lead"`
	}
	decodeResponse(t, detailResp, &detail)
	if detail.Lead.AssignedSalesName != "王二" || detail.Lead.OwnerName != "王二" {
		t.Fatalf("lead display names mismatch: assigned=%q owner=%q lead=%+v", detail.Lead.AssignedSalesName, detail.Lead.OwnerName, detail.Lead)
	}

	listResp := doJSON(t, router, http.MethodGet, "/api/leads?status=claimed", salesToken, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listResp.Code, listResp.Body.String())
	}
	var list struct {
		Leads []types.Lead `json:"Leads"`
	}
	decodeResponse(t, listResp, &list)
	if len(list.Leads) != 1 || list.Leads[0].OwnerName != "王二" {
		t.Fatalf("list display name mismatch: %+v", list.Leads)
	}

	recommendResp := doJSON(t, router, http.MethodGet, fmt.Sprintf("/api/leads/%d/recommend-sales", created.Lead.ID), managerToken, nil)
	if recommendResp.Code != http.StatusOK {
		t.Fatalf("recommend status = %d, body = %s", recommendResp.Code, recommendResp.Body.String())
	}
	var recommend struct {
		Recommendations []struct {
			SalesUserID string `json:"sales_userid"`
			SalesName   string `json:"sales_name"`
		} `json:"recommendations"`
	}
	decodeResponse(t, recommendResp, &recommend)
	if len(recommend.Recommendations) == 0 || recommend.Recommendations[0].SalesUserID != "sales_001" || recommend.Recommendations[0].SalesName != "王二" {
		t.Fatalf("recommend display name mismatch: %+v", recommend.Recommendations)
	}
}

func TestListLeadsUsesLatestDispatchRecordForDispatcher(t *testing.T) {
	router := newTestRouter(t)

	adminToken := issueDevToken(t, router, "admin_001", "admin")
	managerToken := issueDevToken(t, router, "manager_001", "manager")

	setTestAccountName(t, router, "admin_001", "管理员")
	setTestAccountName(t, router, "manager_001", "李主管")
	for _, binding := range []map[string]any{
		{
			"username":      "sales_001",
			"password":      "test-password",
			"employee_name": "王二",
			"role":          "sales",
		},
		{
			"username":      "sales_002",
			"password":      "test-password",
			"employee_name": "张三",
			"role":          "sales",
		},
	} {
		resp := doJSON(t, router, http.MethodPost, "/api/admin/users", adminToken, binding)
		if resp.Code != http.StatusCreated {
			t.Fatalf("create binding status = %d, body = %s", resp.Code, resp.Body.String())
		}
	}

	createResp := doJSON(t, router, http.MethodPost, "/api/leads", managerToken, map[string]any{
		"customer_name":   "Latest Dispatcher",
		"customer_mobile": "13912345678",
		"city":            "杭州",
		"product_name":    "课程",
		"source_channel":  "主动来电（员工丙）",
	})
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create lead status = %d, body = %s", createResp.Code, createResp.Body.String())
	}
	var created struct {
		Lead types.Lead `json:"lead"`
	}
	decodeResponse(t, createResp, &created)

	assignResp := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/leads/%d/assign", created.Lead.ID), managerToken, map[string]any{
		"sales_userid": "sales_001",
	})
	if assignResp.Code != http.StatusOK {
		t.Fatalf("assign status = %d, body = %s", assignResp.Code, assignResp.Body.String())
	}

	reassignResp := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/leads/%d/reassign", created.Lead.ID), adminToken, map[string]any{
		"sales_userid": "sales_002",
		"remark":       "admin reassign",
	})
	if reassignResp.Code != http.StatusOK {
		t.Fatalf("reassign status = %d, body = %s", reassignResp.Code, reassignResp.Body.String())
	}

	listResp := doJSON(t, router, http.MethodGet, "/api/leads?status=assigned_pending_claim", adminToken, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listResp.Code, listResp.Body.String())
	}
	var list struct {
		Leads []types.Lead `json:"Leads"`
	}
	decodeResponse(t, listResp, &list)
	if len(list.Leads) != 1 {
		t.Fatalf("list leads count = %d, leads = %+v", len(list.Leads), list.Leads)
	}
	lead := list.Leads[0]
	if lead.DispatchManagerName != "李主管" {
		t.Fatalf("dispatch manager name = %q, want 李主管", lead.DispatchManagerName)
	}
	if lead.LastDispatchManagerUserID != "admin_001" || lead.LastDispatchManagerName != "管理员" || lead.LastDispatchActionType != "reassign" {
		t.Fatalf("latest dispatcher mismatch: %+v", lead)
	}
	if lead.AssignedSalesName != "张三" {
		t.Fatalf("assigned sales name = %q, want 张三", lead.AssignedSalesName)
	}
	if lead.LastDispatchedAt == nil {
		t.Fatalf("last_dispatched_at is nil")
	}
}

func TestListLeadsCanSearchByKeyword(t *testing.T) {
	router := newTestRouter(t)

	managerToken := issueDevToken(t, router, "manager_001", "manager")

	createLead := func(name, mobile, city, product, source, campaign string) types.Lead {
		resp := doJSON(t, router, http.MethodPost, "/api/leads", managerToken, map[string]any{
			"customer_name":          name,
			"customer_mobile":        mobile,
			"city":                   city,
			"product_name":           product,
			"source_channel":         source,
			"source_campaign":        campaign,
			"customer_mobile_masked": maskMobileForTest(mobile),
		})
		if resp.Code != http.StatusCreated {
			t.Fatalf("create lead status = %d, body = %s", resp.Code, resp.Body.String())
		}
		var created struct {
			Lead types.Lead `json:"lead"`
		}
		decodeResponse(t, resp, &created)
		return created.Lead
	}

	createLead("搜索目标客户", "13912345678", "杭州", "英语", "员工丁抖音（员工乙）", "暑期搜索计划")
	createLead("普通客户", "13800000000", "上海", "数学", "主动来电（员工丙）", "常规计划")

	listResp := doJSON(t, router, http.MethodGet, "/api/leads?status=pending_dispatch&keyword="+url.QueryEscape("目标"), managerToken, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list by name keyword status = %d, body = %s", listResp.Code, listResp.Body.String())
	}
	var list struct {
		Leads []types.Lead `json:"Leads"`
		Total int          `json:"Total"`
	}
	decodeResponse(t, listResp, &list)
	if list.Total != 1 || len(list.Leads) != 1 || list.Leads[0].CustomerName != "搜索目标客户" {
		t.Fatalf("name keyword result mismatch: %+v", list)
	}
	if list.Leads[0].CustomerMobile != "13912345678" {
		t.Fatalf("manager list mobile = %q, want full mobile", list.Leads[0].CustomerMobile)
	}

	mobileResp := doJSON(t, router, http.MethodGet, "/api/leads?keyword=5678", managerToken, nil)
	if mobileResp.Code != http.StatusOK {
		t.Fatalf("list by mobile tail status = %d, body = %s", mobileResp.Code, mobileResp.Body.String())
	}
	decodeResponse(t, mobileResp, &list)
	if list.Total != 1 || len(list.Leads) != 1 || list.Leads[0].CustomerName != "搜索目标客户" {
		t.Fatalf("mobile keyword result mismatch: %+v", list)
	}

	fullMobileResp := doJSON(t, router, http.MethodGet, "/api/leads?keyword="+url.QueryEscape("13912345678"), managerToken, nil)
	if fullMobileResp.Code != http.StatusOK {
		t.Fatalf("list by full mobile status = %d, body = %s", fullMobileResp.Code, fullMobileResp.Body.String())
	}
	decodeResponse(t, fullMobileResp, &list)
	if list.Total != 1 || len(list.Leads) != 1 || list.Leads[0].CustomerName != "搜索目标客户" {
		t.Fatalf("full mobile keyword result mismatch: %+v", list)
	}

	countryCodeMobileResp := doJSON(t, router, http.MethodGet, "/api/leads?keyword="+url.QueryEscape("+86 13912345678"), managerToken, nil)
	if countryCodeMobileResp.Code != http.StatusOK {
		t.Fatalf("list by country code mobile status = %d, body = %s", countryCodeMobileResp.Code, countryCodeMobileResp.Body.String())
	}
	decodeResponse(t, countryCodeMobileResp, &list)
	if list.Total != 1 || len(list.Leads) != 1 || list.Leads[0].CustomerName != "搜索目标客户" {
		t.Fatalf("country code mobile keyword result mismatch: %+v", list)
	}
}

func TestDashboardStatsCanFilterBySales(t *testing.T) {
	router := newTestRouter(t)

	adminToken := issueDevToken(t, router, "admin_001", "admin")
	managerToken := issueDevToken(t, router, "manager_001", "manager")

	for _, binding := range []map[string]any{
		{
			"username":      "sales_001",
			"password":      "test-password",
			"employee_name": "王二",
			"role":          "sales",
		},
		{
			"username":      "sales_002",
			"password":      "test-password",
			"employee_name": "张三",
			"role":          "sales",
		},
	} {
		resp := doJSON(t, router, http.MethodPost, "/api/admin/users", adminToken, binding)
		if resp.Code != http.StatusCreated {
			t.Fatalf("create binding status = %d, body = %s", resp.Code, resp.Body.String())
		}
	}

	salesOneToken := issueDevToken(t, router, "sales_001", "sales")
	createLead := func(name string) types.Lead {
		resp := doJSON(t, router, http.MethodPost, "/api/leads", managerToken, map[string]any{
			"customer_name":   name,
			"customer_mobile": "13912345678",
			"city":            "杭州",
			"product_name":    "课程",
			"source_channel":  "主动来电（员工丙）",
		})
		if resp.Code != http.StatusCreated {
			t.Fatalf("create lead status = %d, body = %s", resp.Code, resp.Body.String())
		}
		var created struct {
			Lead types.Lead `json:"lead"`
		}
		decodeResponse(t, resp, &created)
		return created.Lead
	}

	firstLead := createLead("Stats Sales One")
	secondLead := createLead("Stats Sales Two")

	assignFirstResp := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/leads/%d/assign", firstLead.ID), managerToken, map[string]any{
		"sales_userid": "sales_001",
	})
	if assignFirstResp.Code != http.StatusOK {
		t.Fatalf("assign first status = %d, body = %s", assignFirstResp.Code, assignFirstResp.Body.String())
	}
	assignSecondResp := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/leads/%d/assign", secondLead.ID), managerToken, map[string]any{
		"sales_userid": "sales_002",
	})
	if assignSecondResp.Code != http.StatusOK {
		t.Fatalf("assign second status = %d, body = %s", assignSecondResp.Code, assignSecondResp.Body.String())
	}
	claimResp := doJSON(t, router, http.MethodPost, fmt.Sprintf("/api/leads/%d/claim", firstLead.ID), salesOneToken, nil)
	if claimResp.Code != http.StatusOK {
		t.Fatalf("claim status = %d, body = %s", claimResp.Code, claimResp.Body.String())
	}

	dashboardResp := doJSON(t, router, http.MethodGet, "/api/stats/dashboard?sales_userid=sales_001", managerToken, nil)
	if dashboardResp.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, body = %s", dashboardResp.Code, dashboardResp.Body.String())
	}
	var dashboard struct {
		Assignment usecase.AssignmentOverview    `json:"assignment"`
		Sales      []usecase.SalesDashboardStats `json:"sales"`
	}
	decodeResponse(t, dashboardResp, &dashboard)

	if dashboard.Assignment.Total != 1 || dashboard.Assignment.Assigned != 1 || dashboard.Assignment.ClaimedOrFollowing != 1 {
		t.Fatalf("assignment mismatch: %+v", dashboard.Assignment)
	}
	if len(dashboard.Sales) != 1 {
		t.Fatalf("sales rows = %d, rows = %+v", len(dashboard.Sales), dashboard.Sales)
	}
	row := dashboard.Sales[0]
	if row.SalesUserID != "sales_001" || row.SalesName != "王二" || row.Assigned != 1 || row.Claimed != 1 {
		t.Fatalf("sales row mismatch: %+v", row)
	}
}

func TestAdLeadWebhookCreatesAndDeduplicatesLead(t *testing.T) {
	router := newTestRouter(t)

	payload := map[string]any{
		"leads_id":          "tx-001",
		"leads_name":        "广告客户",
		"leads_tel":         "13912345678",
		"tel_location":      "杭州",
		"campaign_name":     "暑期投放",
		"component_name":    "体验课表单",
		"manager_userid":    "manager_001",
		"leads_create_time": "2026-07-02 10:00:00",
	}

	resp := doJSON(t, router, http.MethodPost, "/api/leads/tencent?account=test-account", "", payload)
	if resp.Code != http.StatusCreated {
		t.Fatalf("ad lead status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var body struct {
		OK     bool   `json:"ok"`
		LeadID int64  `json:"lead_id"`
		LeadNo string `json:"lead_no"`
	}
	decodeResponse(t, resp, &body)
	if !body.OK || body.LeadID == 0 || body.LeadNo != "tencent:tx-001" {
		t.Fatalf("ad lead response mismatch: %+v", body)
	}

	adminToken := issueDevToken(t, router, "admin_001", "admin")
	detailResp := doJSON(t, router, http.MethodGet, fmt.Sprintf("/api/leads/%d", body.LeadID), adminToken, nil)
	if detailResp.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", detailResp.Code, detailResp.Body.String())
	}
	var detail struct {
		Lead types.Lead `json:"lead"`
	}
	decodeResponse(t, detailResp, &detail)
	if detail.Lead.SourceChannel != "ad:tencent:test-account" ||
		detail.Lead.SourceCampaign != "暑期投放" ||
		detail.Lead.DispatchManagerUserID != "manager_001" {
		t.Fatalf("created lead mismatch: %+v", detail.Lead)
	}

	duplicateResp := doJSON(t, router, http.MethodPost, "/api/leads/tencent?account=test-account", "", payload)
	if duplicateResp.Code != http.StatusOK {
		t.Fatalf("duplicate status = %d, body = %s", duplicateResp.Code, duplicateResp.Body.String())
	}
	var duplicateBody struct {
		OK         bool  `json:"ok"`
		Duplicated bool  `json:"duplicated"`
		LeadID     int64 `json:"lead_id"`
	}
	decodeResponse(t, duplicateResp, &duplicateBody)
	if !duplicateBody.OK || !duplicateBody.Duplicated || duplicateBody.LeadID != body.LeadID {
		t.Fatalf("duplicate response mismatch: %+v", duplicateBody)
	}
}

func TestGatewayLeadCreatesAndDeduplicatesCanonicalLead(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	router := newTestRouterWithOptions(t, testRouterOptions{
		gatewayEnabled: true,
		gatewaySecret:  secret,
	})
	body := []byte(`{"schemaVersion":1,"source":"baidu","externalLeadId":"gateway-001","dedupeKey":"baidu:gateway-001","accountName":"百度襄五","customerName":"张同学","phone":"13912345678","wechat":"student-wechat","region":"武汉","campaignName":"暑期招生","formName":"百度咨询表单","leadCreatedAt":"2026-07-21T10:00:00+08:00"}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	headers := map[string]string{
		"Content-Type":              "application/json",
		"X-KXOne-Gateway-Timestamp": timestamp,
		"X-KXOne-Gateway-Signature": signAdLeadPayload(secret, timestamp, body),
	}

	created := doRaw(t, router, http.MethodPost, "/api/internal/leads/gateway", "", body, headers)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"lead_no":"baidu:gateway-001"`) || !strings.Contains(created.Body.String(), `"duplicated":false`) {
		t.Fatalf("gateway create status = %d, body = %s", created.Code, created.Body.String())
	}
	var createdBody struct {
		LeadID int64 `json:"lead_id"`
	}
	decodeResponse(t, created, &createdBody)
	adminToken := issueDevToken(t, router, "admin_gateway", "admin")
	detail := doJSON(t, router, http.MethodGet, fmt.Sprintf("/api/leads/%d", createdBody.LeadID), adminToken, nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("gateway detail status = %d, body = %s", detail.Code, detail.Body.String())
	}
	var detailBody struct {
		Lead types.Lead `json:"lead"`
	}
	decodeResponse(t, detail, &detailBody)
	if detailBody.Lead.CustomerWechat != "student-wechat" || detailBody.Lead.CustomerWechatMasked == "" || detailBody.Lead.SourceChannel != "ad:baidu:百度襄五" {
		t.Fatalf("gateway contact mapping mismatch: %+v", detailBody.Lead)
	}

	duplicate := doRaw(t, router, http.MethodPost, "/api/internal/leads/gateway", "", body, headers)
	if duplicate.Code != http.StatusOK || !strings.Contains(duplicate.Body.String(), `"duplicated":true`) {
		t.Fatalf("gateway duplicate status = %d, body = %s", duplicate.Code, duplicate.Body.String())
	}
}

func TestGatewayLeadRejectsInvalidSignatureAndPayload(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	router := newTestRouterWithOptions(t, testRouterOptions{
		gatewayEnabled: true,
		gatewaySecret:  secret,
	})
	body := []byte(`{"schemaVersion":1,"source":"baidu","externalLeadId":"gateway-002","phone":"13912345678"}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	unauthorized := doRaw(t, router, http.MethodPost, "/api/internal/leads/gateway", "", body, map[string]string{
		"Content-Type":              "application/json",
		"X-KXOne-Gateway-Timestamp": timestamp,
		"X-KXOne-Gateway-Signature": "invalid",
	})
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("gateway unauthorized status = %d, body = %s", unauthorized.Code, unauthorized.Body.String())
	}

	invalidBody := []byte(`{"schemaVersion":2,"source":"baidu","externalLeadId":"gateway-002","phone":"13912345678"}`)
	invalid := doRaw(t, router, http.MethodPost, "/api/internal/leads/gateway", "", invalidBody, map[string]string{
		"Content-Type":              "application/json",
		"X-KXOne-Gateway-Timestamp": timestamp,
		"X-KXOne-Gateway-Signature": signAdLeadPayload(secret, timestamp, invalidBody),
	})
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), `"error":"unsupported_schema_version"`) {
		t.Fatalf("gateway invalid payload status = %d, body = %s", invalid.Code, invalid.Body.String())
	}
}

func TestGatewayLeadAcceptsAllAdvertisingSources(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	router := newTestRouterWithOptions(t, testRouterOptions{
		gatewayEnabled: true,
		gatewaySecret:  secret,
	})
	for _, source := range []string{"baidu", "tencent", "xiaohongshu", "douyin"} {
		t.Run(source, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"schemaVersion":1,"source":%q,"externalLeadId":"%s-001","dedupeKey":"%s:%s-001","customerName":"%s客户","phone":"13900000000","leadCreatedAt":"2026-07-21T10:00:00+08:00"}`, source, source, source, source, source))
			timestamp := strconv.FormatInt(time.Now().Unix(), 10)
			response := doRaw(t, router, http.MethodPost, "/api/internal/leads/gateway", "", body, map[string]string{
				"Content-Type":              "application/json",
				"X-KXOne-Gateway-Timestamp": timestamp,
				"X-KXOne-Gateway-Signature": signAdLeadPayload(secret, timestamp, body),
			})
			if response.Code != http.StatusCreated {
				t.Fatalf("source=%s status = %d, body = %s", source, response.Code, response.Body.String())
			}
		})
	}
}

func TestAdLeadWebhookRequiresConfiguredSignature(t *testing.T) {
	router := newTestRouterWithOptions(t, testRouterOptions{
		adLeadWebhookSecret: "ad-secret",
	})
	payload := []byte(`{"leads_id":"signed-001","leads_name":"签名客户","leads_tel":"13912345678","manager_userid":"manager_001"}`)

	missingResp := doRaw(t, router, http.MethodPost, "/api/leads/tencent", "", payload, nil)
	if missingResp.Code != http.StatusUnauthorized {
		t.Fatalf("missing signature status = %d, body = %s", missingResp.Code, missingResp.Body.String())
	}

	timestamp := fmt.Sprint(time.Now().Unix())
	headers := map[string]string{
		"Content-Type":        "application/json",
		"X-KXOne-Timestamp":   timestamp,
		"X-KXOne-Signature":   "sha256=" + signAdLeadPayload("ad-secret", timestamp, payload),
		"X-KXOne-Webhook-Id":  "ignored",
		"X-KXOne-Webhook-Src": "test",
	}
	signedResp := doRaw(t, router, http.MethodPost, "/api/leads/tencent", "", payload, headers)
	if signedResp.Code != http.StatusCreated {
		t.Fatalf("signed webhook status = %d, body = %s", signedResp.Code, signedResp.Body.String())
	}
}

func TestTencentPlatformCallbackAcceptsEnabledConfiguredAccount(t *testing.T) {
	account := "武汉明壹致行教育科技有限公司"
	router := newTestRouterWithOptions(t, testRouterOptions{
		adLeadWebhookSecret: "internal-ad-secret",
		channelIntegrations: []*types.ChannelIntegration{{
			Platform: types.LeadPlatformTencent, AccountName: account,
			CallbackPath: "/api/leads/tencent", AuthMode: "platform_compatible", Enabled: true,
		}},
	})
	payload := []byte(`{"leads_id":717559065,"leads_name":"腾讯测试客户","leads_tel":"13912345678","campaign_name":"暑期招生","component_name":"咨询表单"}`)
	response := doRaw(t, router, http.MethodPost, "/api/leads/tencent?account="+url.QueryEscape(account), "", payload, map[string]string{"Content-Type": "application/json"})
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"ok":true`) {
		t.Fatalf("platform callback status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDeleteChannelIntegrationRequiresAdminAndRemovesAccount(t *testing.T) {
	account := "待删除平台账户"
	router := newTestRouterWithOptions(t, testRouterOptions{
		channelIntegrations: []*types.ChannelIntegration{{
			Platform: types.LeadPlatformTencent, AccountName: account,
			CallbackPath: "/api/leads/tencent", AuthMode: "platform_compatible", Enabled: true,
		}},
	})
	managerToken := issueDevToken(t, router, "manager_delete_channel", "manager")
	forbidden := doJSON(t, router, http.MethodDelete, "/api/channel-integrations/1", managerToken, nil)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("manager delete status=%d body=%s", forbidden.Code, forbidden.Body.String())
	}
	adminToken := issueDevToken(t, router, "admin_delete_channel", "admin")
	deleted := doJSON(t, router, http.MethodDelete, "/api/channel-integrations/1", adminToken, nil)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("admin delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	listed := doJSON(t, router, http.MethodGet, "/api/channel-integrations", adminToken, nil)
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), account) {
		t.Fatalf("list after delete status=%d body=%s", listed.Code, listed.Body.String())
	}
}

func TestBaiduPlatformCallbackAcceptsEnabledConfiguredAccount(t *testing.T) {
	account := "百度地图"
	router := newTestRouterWithOptions(t, testRouterOptions{
		adLeadWebhookSecret: "internal-ad-secret",
		channelIntegrations: []*types.ChannelIntegration{{
			Platform: types.LeadPlatformBaidu, AccountName: account,
			CallbackPath: "/api/leads/baidu", AuthMode: "platform_compatible", Enabled: true,
		}},
	})
	payload := []byte(`{"clueId":"baidu-platform-1","cluePhoneNumber":"13912345678","commitTime":"2026-08-05 11:20:00","formName":"百度咨询表单"}`)
	response := doRaw(t, router, http.MethodPost, "/api/leads/baidu?account="+url.QueryEscape(account), "", payload, map[string]string{"Content-Type": "application/json"})
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"ok":true`) {
		t.Fatalf("platform callback status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestTencentPlatformCallbackRejectsUnknownOrDisabledAccount(t *testing.T) {
	router := newTestRouterWithOptions(t, testRouterOptions{
		adLeadWebhookSecret: "internal-ad-secret",
		channelIntegrations: []*types.ChannelIntegration{{
			Platform: types.LeadPlatformTencent, AccountName: "停用账户",
			CallbackPath: "/api/leads/tencent", AuthMode: "platform_compatible", Enabled: false,
		}},
	})
	payload := []byte(`{"leads_id":717559066,"leads_tel":"13912345678"}`)
	for _, tc := range []struct {
		account string
		status  int
	}{
		{"未知账户", http.StatusNotFound},
		{"停用账户", http.StatusForbidden},
	} {
		response := doRaw(t, router, http.MethodPost, "/api/leads/tencent?account="+url.QueryEscape(tc.account), "", payload, map[string]string{"Content-Type": "application/json"})
		if response.Code != tc.status {
			t.Fatalf("account=%s status=%d body=%s", tc.account, response.Code, response.Body.String())
		}
	}
}

func TestXiaohongshuPlatformCallbackUsesAccountToken(t *testing.T) {
	const token = "xiaohongshu-account-token"
	account := "小红书测试账户"
	router := newTestRouterWithOptions(t, testRouterOptions{
		adLeadWebhookSecret: "internal-ad-secret",
		channelIntegrations: []*types.ChannelIntegration{{
			Platform: types.LeadPlatformXiaohongshu, AccountName: account,
			CallbackPath: "/api/leads/xiaohongshu", AuthMode: "hmac_sha1", WebhookToken: token, Enabled: true,
		}},
	})
	payload := []byte(`{"data":{"id":991,"nick_name":"红薯客户","phone_num":"13912345678","source":"咨询表单"}}`)
	mac := hmac.New(sha1.New, []byte(token))
	_, _ = mac.Write(payload)
	response := doRaw(t, router, http.MethodPost, "/api/leads/xiaohongshu?account="+url.QueryEscape(account), "", payload, map[string]string{
		"Content-Type": "application/json", "X-Red-Signature": "sha1=" + hex.EncodeToString(mac.Sum(nil)),
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("valid signature status=%d body=%s", response.Code, response.Body.String())
	}
	invalid := doRaw(t, router, http.MethodPost, "/api/leads/xiaohongshu?account="+url.QueryEscape(account), "", payload, map[string]string{
		"Content-Type": "application/json", "X-Red-Signature": "sha1=0000000000000000000000000000000000000000",
	})
	if invalid.Code != http.StatusUnauthorized || !strings.Contains(invalid.Body.String(), "invalid_platform_signature") {
		t.Fatalf("invalid signature status=%d body=%s", invalid.Code, invalid.Body.String())
	}
}

func TestDouyinPlatformCallbackSupportsCompatibleAndFeiyuSignatureModes(t *testing.T) {
	compatibleAccount := "襄五抖音"
	signedAccount := "襄武抖音签名号"
	const feiyuSecret = "feiyu-static-secret-key"
	router := newTestRouterWithOptions(t, testRouterOptions{
		adLeadWebhookSecret: "internal-ad-secret",
		channelIntegrations: []*types.ChannelIntegration{
			{
				Platform: types.LeadPlatformDouyin, AccountName: compatibleAccount,
				CallbackPath: "/api/leads/douyin", AuthMode: "platform_compatible", Enabled: true,
			},
			{
				Platform: types.LeadPlatformDouyin, AccountName: signedAccount,
				CallbackPath: "/api/leads/douyin", AuthMode: "feiyu_signature", SecretConfigured: true, WebhookToken: feiyuSecret, Enabled: true,
			},
		},
	})

	flatPayload := []byte(`{"id":"feiyu-1001","name":"李同学","telphone":"13812345678","weixin":"wx_li","city_name":"武汉","module_name":"高考复读预约表单","app_name":"抖音","ad_name":"高考冲刺计划","promotion_name":"复读单元A","remark_dict":{"年级":"高三","科目":"理科"}}`)
	resp := doRaw(t, router, http.MethodPost, "/api/leads/douyin?account="+url.QueryEscape(compatibleAccount), "", flatPayload, map[string]string{"Content-Type": "application/json"})
	if resp.Code != http.StatusCreated || !strings.Contains(resp.Body.String(), `"code":0`) || !strings.Contains(resp.Body.String(), `"message":"success"`) {
		t.Fatalf("douyin compatible status=%d body=%s", resp.Code, resp.Body.String())
	}
	var created struct {
		LeadID int64  `json:"lead_id"`
		LeadNo string `json:"lead_no"`
	}
	decodeResponse(t, resp, &created)
	if created.LeadNo != "douyin:feiyu-1001" {
		t.Fatalf("lead_no = %q", created.LeadNo)
	}
	adminToken := issueDevToken(t, router, "admin_douyin", "admin")
	detailResp := doJSON(t, router, http.MethodGet, fmt.Sprintf("/api/leads/%d", created.LeadID), adminToken, nil)
	var detail struct {
		Lead types.Lead `json:"lead"`
	}
	decodeResponse(t, detailResp, &detail)
	if detail.Lead.SourceChannel != "ad:douyin:襄五抖音" || detail.Lead.City != "武汉" || detail.Lead.ProductName != "高考复读预约表单" || !strings.Contains(detail.Lead.SourceCampaign, "高考冲刺计划") || !strings.Contains(detail.Lead.SourceCampaign, "复读单元A") || !strings.Contains(detail.Lead.SourceCampaign, "年级:高三") {
		t.Fatalf("douyin detail mismatch: %+v", detail.Lead)
	}

	signedPayload := []byte(`{"clue_id":"feiyu-2002","context":{"basic_info":{"id":"feiyu-2002","name":"王同学","phone":"13987654321","city":"襄阳"},"ad_info":{"ad_id":"88","ad_name":"抖音投放计划"}}}`)
	timestamp := "2026-11-17 15:30:00"
	accessToken := "feiyu-token-01"
	mac := hmac.New(sha256.New, []byte(feiyuSecret))
	_, _ = mac.Write([]byte("feiyu-2002|" + accessToken + "|" + timestamp))
	rawSig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	signedResp := doRaw(t, router, http.MethodPost, "/api/leads/douyin?account="+url.QueryEscape(signedAccount), "", signedPayload, map[string]string{
		"Content-Type": "application/json",
		"Signature":    rawSig,
		"Access-Token": accessToken,
		"Timestamp":    timestamp,
	})
	if signedResp.Code != http.StatusCreated {
		t.Fatalf("douyin signed callback status=%d body=%s", signedResp.Code, signedResp.Body.String())
	}

	invalidResp := doRaw(t, router, http.MethodPost, "/api/leads/douyin?account="+url.QueryEscape(signedAccount), "", signedPayload, map[string]string{
		"Content-Type": "application/json",
		"Signature":    "invalid-signature",
		"Access-Token": accessToken,
		"Timestamp":    timestamp,
	})
	if invalidResp.Code != http.StatusUnauthorized || !strings.Contains(invalidResp.Body.String(), "invalid_platform_signature") {
		t.Fatalf("douyin invalid signature status=%d body=%s", invalidResp.Code, invalidResp.Body.String())
	}
}

func TestAdLeadRoutesUseSharedRateLimit(t *testing.T) {
	router := newTestRouterWithOptions(t, testRouterOptions{
		rateLimitCache: newFakeCacheWithInitialValue("ratelimit:ad_leads", 120),
	})

	resp := doJSON(t, router, http.MethodPost, "/api/leads/tencent", "", map[string]any{
		"leads_id":   "rate-limited",
		"leads_name": "限流客户",
		"leads_tel":  "13912345678",
	})
	if resp.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limited status = %d, body = %s", resp.Code, resp.Body.String())
	}
}

func TestAuthRoutesUseSharedRateLimit(t *testing.T) {
	router := newTestRouterWithOptions(t, testRouterOptions{
		rateLimitCache: newFakeCacheWithInitialValue("ratelimit:auth", 300),
	})

	resp := doJSON(t, router, http.MethodPost, "/api/auth/login", "", map[string]any{
		"username": "admin_001",
		"password": "test-password",
	})
	if resp.Code != http.StatusTooManyRequests {
		t.Fatalf("auth rate limited status = %d, body = %s", resp.Code, resp.Body.String())
	}
}

func TestRemovedWeComIdentityRoutesAreNotRegistered(t *testing.T) {
	router := newTestRouter(t)
	for _, path := range []string{"/api/binding-requests", "/api/wecom/jsapi-config", "/api/auth/wecom/authorize"} {
		resp := doJSON(t, router, http.MethodGet, path, "", nil)
		if resp.Code != http.StatusNotFound {
			t.Fatalf("removed route %s status = %d", path, resp.Code)
		}
	}
}

func TestRemovedCMSRoutesAreNotRegistered(t *testing.T) {
	router := newTestRouter(t)
	for _, path := range []string{"/api/cms/v1/entries", "/api/public/cms/redirect", "/api/internal/cms/releases/1/manifest"} {
		resp := doJSON(t, router, http.MethodGet, path, "", nil)
		if resp.Code != http.StatusNotFound {
			t.Fatalf("removed route %s status = %d", path, resp.Code)
		}
	}
}

func TestAdminSettingsAndUserBindings(t *testing.T) {
	router := newTestRouter(t)

	adminToken := issueDevToken(t, router, "admin_001", "admin")
	managerToken := issueDevToken(t, router, "manager_001", "manager")

	userInfoResp := doJSON(t, router, http.MethodGet, "/api/user/info", adminToken, nil)
	if userInfoResp.Code != http.StatusOK {
		t.Fatalf("user info status = %d, body = %s", userInfoResp.Code, userInfoResp.Body.String())
	}
	var userInfo struct {
		WeComUserID string `json:"wecom_userid"`
		Role        string `json:"role"`
	}
	decodeResponse(t, userInfoResp, &userInfo)
	if userInfo.WeComUserID != "admin_001" || userInfo.Role != "admin" {
		t.Fatalf("user info mismatch: user=%s role=%s", userInfo.WeComUserID, userInfo.Role)
	}

	forbiddenResp := doJSON(t, router, http.MethodGet, "/api/admin/settings", managerToken, nil)
	if forbiddenResp.Code != http.StatusForbidden {
		t.Fatalf("manager admin settings status = %d, body = %s", forbiddenResp.Code, forbiddenResp.Body.String())
	}

	updateSettingsResp := doJSON(t, router, http.MethodPut, "/api/admin/settings", adminToken, map[string]any{
		"settings": []map[string]any{
			{
				"key":          "custom.label",
				"value":        "CRM",
				"value_type":   "string",
				"group":        "notification",
				"description":  "企业 ID",
				"is_sensitive": false,
			},
			{
				"key":          "notification.group_robot_webhook_url",
				"value":        "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=test-key",
				"value_type":   "string",
				"group":        "notification",
				"description":  "应用 Secret",
				"is_sensitive": true,
			},
		},
	})
	if updateSettingsResp.Code != http.StatusOK {
		t.Fatalf("update settings status = %d, body = %s", updateSettingsResp.Code, updateSettingsResp.Body.String())
	}

	settingsResp := doJSON(t, router, http.MethodGet, "/api/admin/settings?group=notification", adminToken, nil)
	if settingsResp.Code != http.StatusOK {
		t.Fatalf("list settings status = %d, body = %s", settingsResp.Code, settingsResp.Body.String())
	}
	var settingsBody struct {
		Settings []types.SystemSetting `json:"settings"`
	}
	decodeResponse(t, settingsResp, &settingsBody)
	if len(settingsBody.Settings) != 2 {
		t.Fatalf("settings count = %d", len(settingsBody.Settings))
	}
	for _, setting := range settingsBody.Settings {
		if setting.Key == "notification.group_robot_webhook_url" && setting.Value != "******" {
			t.Fatalf("sensitive setting value not masked: %q", setting.Value)
		}
	}

	updateSensitiveMetaResp := doJSON(t, router, http.MethodPut, "/api/admin/settings", adminToken, map[string]any{
		"settings": []map[string]any{
			{
				"key":          "notification.group_robot_webhook_url",
				"value":        "",
				"value_type":   "string",
				"group":        "notification",
				"description":  "企业微信应用 Secret",
				"is_sensitive": true,
			},
		},
	})
	if updateSensitiveMetaResp.Code != http.StatusOK {
		t.Fatalf("update sensitive meta status = %d, body = %s", updateSensitiveMetaResp.Code, updateSensitiveMetaResp.Body.String())
	}
	settingsResp = doJSON(t, router, http.MethodGet, "/api/admin/settings?group=notification", adminToken, nil)
	if settingsResp.Code != http.StatusOK {
		t.Fatalf("list settings after meta update status = %d, body = %s", settingsResp.Code, settingsResp.Body.String())
	}
	decodeResponse(t, settingsResp, &settingsBody)
	for _, setting := range settingsBody.Settings {
		if setting.Key == "notification.group_robot_webhook_url" && setting.Value != "******" {
			t.Fatalf("sensitive setting value lost after empty update: %q", setting.Value)
		}
	}

	createBindingResp := doJSON(t, router, http.MethodPost, "/api/admin/users", adminToken, map[string]any{
		"crm_user_id":   1001,
		"username":      "sales_001",
		"password":      "test-password",
		"employee_name": "销售一",
		"role":          "sales",
		"status":        "active",
	})
	if createBindingResp.Code != http.StatusCreated {
		t.Fatalf("create binding status = %d, body = %s", createBindingResp.Code, createBindingResp.Body.String())
	}
	var createdBinding types.WeComUserBinding
	decodeResponse(t, createBindingResp, &createdBinding)
	if createdBinding.ID == 0 {
		t.Fatal("created binding id is empty")
	}

	updateBindingResp := doJSON(t, router, http.MethodPut, fmt.Sprintf("/api/admin/users/%d", createdBinding.ID), adminToken, map[string]any{
		"expected_version": createdBinding.AccountVersion,
		"crm_user_id":      1001,
		"username":         "sales_001",
		"password":         "test-password",
		"employee_name":    "销售一",
		"role":             "sales",
		"status":           "disabled",
	})
	if updateBindingResp.Code != http.StatusOK {
		t.Fatalf("update binding status = %d, body = %s", updateBindingResp.Code, updateBindingResp.Body.String())
	}

	listBindingsResp := doJSON(t, router, http.MethodGet, "/api/admin/users?status=disabled", adminToken, nil)
	if listBindingsResp.Code != http.StatusOK {
		t.Fatalf("list bindings status = %d, body = %s", listBindingsResp.Code, listBindingsResp.Body.String())
	}
	var bindingsBody struct {
		Bindings []types.WeComUserBinding `json:"bindings"`
		Total    int                      `json:"total"`
	}
	decodeResponse(t, listBindingsResp, &bindingsBody)
	if bindingsBody.Total != 1 || len(bindingsBody.Bindings) != 1 {
		t.Fatalf("disabled bindings total=%d len=%d", bindingsBody.Total, len(bindingsBody.Bindings))
	}
}

func TestPasswordLoginForManagedUser(t *testing.T) {
	router := newTestRouter(t)
	adminToken := issueDevToken(t, router, "admin_001", "admin")
	managerToken := issueDevToken(t, router, "manager_001", "manager")

	createBindingResp := doJSON(t, router, http.MethodPost, "/api/admin/users", adminToken, map[string]any{
		"crm_user_id":   1002,
		"username":      "oauth_user",
		"password":      "test-password",
		"employee_name": "OAuth User",
		"role":          "sales",
		"department_id": 12,
		"status":        "active",
	})
	if createBindingResp.Code != http.StatusCreated {
		t.Fatalf("create binding status = %d, body = %s", createBindingResp.Code, createBindingResp.Body.String())
	}

	salesResp := doJSON(t, router, http.MethodGet, "/api/users/sales", managerToken, nil)
	if salesResp.Code != http.StatusOK {
		t.Fatalf("sales list status = %d, body = %s", salesResp.Code, salesResp.Body.String())
	}
	var salesBody struct {
		Bindings []types.WeComUserBinding `json:"bindings"`
	}
	decodeResponse(t, salesResp, &salesBody)
	if len(salesBody.Bindings) != 2 || salesBody.Bindings[0].WeComUserID != "manager_001" || salesBody.Bindings[1].WeComUserID != "oauth_user" {
		t.Fatalf("sales list mismatch: %+v", salesBody.Bindings)
	}

	loginResp := doJSON(t, router, http.MethodPost, "/api/auth/login", "", map[string]any{
		"username": "oauth_user", "password": "test-password",
	})
	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginResp.Code, loginResp.Body.String())
	}
	assertOAuthTokenCookies(t, loginResp)
}

func assertOAuthTokenCookies(t *testing.T, resp *httptest.ResponseRecorder) {
	t.Helper()
	cookies := resp.Result().Cookies()
	values := make(map[string]string, len(cookies))
	for _, cookie := range cookies {
		values[cookie.Name] = cookie.Value
	}
	for _, name := range []string{
		"kxone_access_token",
		"kxone_refresh_token",
		"kxone_session",
	} {
		if values[name] == "" {
			t.Fatalf("missing auth cookie %s in %v", name, cookies)
		}
	}
	for _, cookie := range cookies {
		switch cookie.Name {
		case "kxone_access_token", "kxone_refresh_token":
			if !cookie.HttpOnly {
				t.Fatalf("auth cookie %s should be HttpOnly", cookie.Name)
			}
		case "kxone_session":
			if cookie.HttpOnly {
				t.Fatalf("oauth session marker should be readable by frontend")
			}
		}
	}
}

type testRouterOptions struct {
	agentModelURL  string
	agentFixtureDB **gorm.DB
	createLead     **usecase.CreateLeadUseCase
	// Live acceptance retains the real account repository and production model
	// transport while replacing only the business database with synthetic data.
	agentConfig      *agent.Config
	agentPublicModel bool
	// agentFieldKey replays a server-local production copy whose contacts are
	// encrypted with the production key (kept in process memory only).
	agentFieldKey       string
	agentServiceOptions []agent.ServiceOption
	agentPrepareFixture func(*testing.T, *gorm.DB)
	// Only the restart acceptance constructor can supply a verified, private
	// persistent fixture. All ordinary tests keep their temporary lifecycle.
	agentRestartFixture *agentRestartFixture

	leadFixtures        []*types.Lead
	strictAssignments   bool
	appointmentsEnabled bool
	adLeadWebhookSecret string
	rateLimitCache      *fakeCache
	gatewayEnabled      bool
	gatewaySecret       string
	channelIntegrations []*types.ChannelIntegration
	groupRobotNotifier  wecom.GroupRobotNotifier
}

func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	return newTestRouterWithOptions(t, testRouterOptions{})
}

func newTestRouterWithOptions(t *testing.T, options testRouterOptions) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dbPath := filepath.Join(t.TempDir(), "kxone-test.db")
	if options.agentRestartFixture != nil {
		dbPath = options.agentRestartFixture.database
	}
	db, err := repo.NewDB(&config.DatabaseConfig{
		Driver:   "sqlite",
		Database: dbPath,
		MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if options.agentFixtureDB != nil {
		*options.agentFixtureDB = db
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	if err := repo.AutoMigrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	if err := db.AutoMigrate(&repo.LeadListPreferenceRecord{}, &repo.LeadSavedViewRecord{}); err != nil {
		t.Fatal(err)
	}
	fieldKey := "0123456789abcdef0123456789abcdef"
	if options.agentRestartFixture != nil {
		fieldKey = options.agentRestartFixture.fieldKey
	} else if options.agentFieldKey != "" {
		fieldKey = options.agentFieldKey
	} else if options.agentPublicModel {
		// Real cloud credentials in an isolated test database must never use
		// the publicly known unit-test key. Keep this fresh key in memory only.
		secretKey := make([]byte, 32)
		if _, err := cryptorand.Read(secretKey); err != nil {
			t.Fatal("generate ephemeral acceptance field key: ", err)
		}
		fieldKey = base64.StdEncoding.EncodeToString(secretKey)
		clear(secretKey)
	}
	channelProtector, err := security.NewAESGCMFieldProtector(fieldKey)
	if err != nil {
		t.Fatalf("new channel protector: %v", err)
	}
	channelIntegrationRepo := repo.NewGormChannelIntegrationRepository(db).WithFieldProtector(channelProtector)
	for _, item := range options.channelIntegrations {
		now := time.Now()
		item.CreatedBy, item.UpdatedBy, item.CreatedAt, item.UpdatedAt = "test", "test", now, now
		if err := channelIntegrationRepo.Create(t.Context(), item); err != nil {
			t.Fatalf("create channel integration: %v", err)
		}
	}

	mobileHasher, err := security.NewHMACMobileHasher("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("new mobile hasher: %v", err)
	}
	leadRepo := repo.NewGormLeadRepository(db).WithMobileHasher(mobileHasher)
	for _, fixture := range options.leadFixtures {
		if err := leadRepo.Create(context.Background(), fixture); err != nil {
			t.Fatal(err)
		}
	}
	dispatchRepo := repo.NewGormDispatchRecordRepository(db)
	followupRepo := repo.NewGormFollowupRepository(db)
	activityRepo := repo.NewGormLeadActivityRepository(db)
	notificationDiagnostics := repo.NewGormGroupRobotNotificationRepository(db)
	cardRepo := repo.NewGormCardTaskRepository(db)
	userBindingRepo := repo.NewGormUserBindingRepository(db)
	settingRepo := repo.NewGormSystemSettingRepository(db)
	if options.appointmentsEnabled {
		for _, key := range []string{runtime.SettingWorkflowV2Enabled, runtime.SettingAppointmentEnabled} {
			if err := settingRepo.Upsert(context.Background(), &types.SystemSetting{Key: key, Value: "true", ValueType: "bool"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	runtimeSettings := runtime.NewDatabaseRuntimeSettings(settingRepo, 0)
	appointmentRepo := repo.NewGormAppointmentRepository(db)
	availabilityRepo := repo.NewGormCloserAvailabilityRepository(db)
	reminderRepo := repo.NewGormLeadReminderRepository(db)
	txManager := repo.NewGormTransactionManager(db).WithLeadSecurity(nil, mobileHasher)
	wecomClient := &fakeWeComClient{}
	cache := newFakeCache()
	rateLimitCache := options.rateLimitCache
	if rateLimitCache == nil {
		rateLimitCache = newFakeCache()
	}

	createLeadUseCase := usecase.NewCreateLeadUseCase(leadRepo, nil).WithIdempotencySecret("0123456789abcdef0123456789abcdef").WithRoleAwareCreation(userBindingRepo, dispatchRepo, reminderRepo, txManager)
	if options.createLead != nil {
		*options.createLead = createLeadUseCase
	}
	assignUseCase := usecase.NewAssignLeadUseCase(leadRepo, dispatchRepo, cardRepo, wecomClient, cache, nil).WithTransactionManager(txManager)
	reassignUseCase := usecase.NewReassignLeadUseCase(leadRepo, dispatchRepo, cardRepo, nil).WithTransactionManager(txManager)
	if options.strictAssignments {
		assignUseCase.WithUserBindingRepository(userBindingRepo)
		reassignUseCase.WithUserBindingRepository(userBindingRepo)
	}
	claimUseCase := usecase.NewClaimLeadUseCase(leadRepo, cardRepo, wecomClient, cache, nil).WithDispatchHistory(dispatchRepo, txManager).WithStage3Workflow(runtimeSettings, runtimeSettings, reminderRepo)
	completeLeadUseCase := usecase.NewCompleteLeadUseCase(leadRepo).WithAudit(activityRepo, txManager)
	createFollowupUseCase := usecase.NewCreateFollowupUseCase(leadRepo, followupRepo).WithIdempotencySecret("0123456789abcdef0123456789abcdef").WithTransactionManager(txManager)
	profileUseCase := usecase.NewUpdateLeadProfileUseCase(leadRepo, activityRepo).WithTransactionManager(txManager)
	getLeadUseCase := usecase.NewGetLeadUseCase(leadRepo, followupRepo, dispatchRepo).
		WithUserBindingRepository(userBindingRepo).
		WithActivityRepository(activityRepo).
		WithNotificationDiagnostics(notificationDiagnostics)
	listLeadsUseCase := usecase.NewListLeadsUseCase(leadRepo).
		WithDispatchRecordRepository(dispatchRepo).
		WithUserBindingRepository(userBindingRepo)
	getSLAStatsUseCase := usecase.NewGetSLAStatsUseCase(leadRepo)
	getDashboardStatsUseCase := usecase.NewGetDashboardStatsUseCase(leadRepo, followupRepo).
		WithUserBindingRepository(userBindingRepo)
	recommendSalesUseCase := usecase.NewRecommendSalesUseCase(leadRepo, followupRepo).
		WithUserBindingRepository(userBindingRepo)
	autoAssignLeadUseCase := usecase.NewAutoAssignLeadUseCase(leadRepo, followupRepo, recommendSalesUseCase, assignUseCase)
	adminSettingsUseCase := usecase.NewAdminSettingsUseCase(settingRepo)
	adminUserBindingsUseCase := usecase.NewAdminUserBindingsUseCase(userBindingRepo)
	channelIntegrationsUseCase := usecase.NewChannelIntegrationsUseCase(channelIntegrationRepo, "https://kxone.example.com")
	cfg := &config.Config{
		Server: config.ServerConfig{
			Env:           "dev",
			PublicBaseURL: "https://kxone.example.com",
		},
		JWT: config.JWTConfig{
			Secret:             "test-secret",
			AccessTokenExpiry:  7200,
			RefreshTokenExpiry: 604800,
		},
		AdLead: config.AdLeadConfig{
			GatewayEnabled:    options.gatewayEnabled,
			GatewayHMACSecret: options.gatewaySecret,
			GatewayTolerance:  300,
		},
	}

	leadHandler := httpTransport.NewLeadHandler(createLeadUseCase, assignUseCase, reassignUseCase, claimUseCase, completeLeadUseCase, getLeadUseCase, listLeadsUseCase).
		WithBatchUseCase(usecase.NewBatchLeadsUseCase(txManager)).
		WithProfileUpdateUseCase(profileUseCase).
		WithGroupRobotNotifier(options.groupRobotNotifier)
	adLeadHandler := httpTransport.NewAdLeadHandler(createLeadUseCase, leadRepo, "manager1").
		WithWebhookSecret(options.adLeadWebhookSecret, 5*time.Minute).
		WithGatewayHMAC(options.gatewaySecret, 5*time.Minute).
		WithChannelIntegrations(channelIntegrationRepo).
		WithPublicFormOrigins(cfg.Server.PublicBaseURL)
	followupHandler := httpTransport.NewFollowupHandler(createFollowupUseCase, getLeadUseCase)
	authHandler := httpTransport.NewAuthHandler(
		userBindingRepo,
		cfg.JWT.Secret,
		cfg.JWT.AccessTokenExpiry,
		cfg.JWT.RefreshTokenExpiry,
	).WithLoginProtection(testLoginProtection{}, cfg.JWT.Secret)
	slaHandler := httpTransport.NewSLAHandler(getSLAStatsUseCase, getDashboardStatsUseCase).WithManagementStats(usecase.NewManagementStatsUseCase(leadRepo, userBindingRepo))
	recommendationHandler := httpTransport.NewRecommendationHandler(recommendSalesUseCase)
	autoAssignHandler := httpTransport.NewAutoAssignHandler(autoAssignLeadUseCase)
	adminHandler := httpTransport.NewAdminHandler(adminSettingsUseCase, adminUserBindingsUseCase).WithGroupRobotDiagnostics(notificationDiagnostics)
	channelIntegrationHandler := httpTransport.NewChannelIntegrationHandler(channelIntegrationsUseCase)

	appointmentUseCase := usecase.NewAppointmentUseCase(appointmentRepo, availabilityRepo, leadRepo, userBindingRepo, txManager, runtimeSettings, runtimeSettings).WithStage5Delivery(runtimeSettings, activityRepo, reminderRepo)
	if err := agent.MigrateForTest(db); err != nil {
		t.Fatal(err)
	}
	if options.agentPrepareFixture != nil {
		options.agentPrepareFixture(t, db)
	}
	agentCfg := agent.DefaultConfig()
	agentCfg.Directory = t.TempDir()
	agentCfg.BaseURL = options.agentModelURL
	agentCfg.Enabled = options.agentModelURL != ""
	if agentCfg.Enabled {
		agentCfg.APIKey = "isolated-model-fixture"
	}
	if options.agentConfig != nil {
		agentCfg = *options.agentConfig
	}
	agentService, err := agent.NewService(db, agentCfg, userBindingRepo, channelProtector, !options.agentPublicModel, options.agentServiceOptions...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(agentService.Close)
	router := setupRouter(&serverContainer{
		workflowFlags:   runtimeSettings,
		userBindingRepo: userBindingRepo,
		cfg:             cfg,
		handlers: handlerContainer{
			agent:              agent.NewHandler(agentService).WithHistoryAccess(httpTransport.StreamAccessAudit(repo.NewGormAccessAuditRepository(db)), httpTransport.StreamSessionValid(userBindingRepo)),
			appointment:        httpTransport.NewAppointmentHandler(appointmentUseCase, usecase.NewAvailabilityUseCase(availabilityRepo, userBindingRepo, runtimeSettings)),
			workbench:          httpTransport.NewWorkbenchHandler(usecase.NewGetWorkbenchUseCase(leadRepo, appointmentRepo)),
			leadPool:           httpTransport.NewLeadPoolHandler(usecase.NewLeadPoolUseCase(leadRepo, repo.NewGormLeadPoolRepository(db), runtimeSettings, runtimeSettings).WithDependencies(userBindingRepo, txManager)),
			leadUIPreferences:  httpTransport.NewLeadUIPreferencesHandler(usecase.NewLeadUIPreferencesUseCase(repo.NewGormLeadUIPreferenceRepository(db))),
			lead:               leadHandler,
			leadLifecycle:      httpTransport.NewLeadLifecycleHandler(repo.NewGormLeadLifecycleRepository(db, runtimeSettings)),
			adLead:             adLeadHandler,
			followup:           followupHandler,
			auth:               authHandler,
			sla:                slaHandler,
			recommendation:     recommendationHandler,
			autoAssign:         autoAssignHandler,
			admin:              adminHandler,
			channelIntegration: channelIntegrationHandler,
		},
		rateLimitCache: rateLimitCache,
		accessAudits:   repo.NewGormAccessAuditRepository(db),
	})
	testAccountRepositories.Store(router, userBindingRepo)
	t.Cleanup(func() { testAccountRepositories.Delete(router) })
	return router
}

var testAccountRepositories sync.Map

func issueDevToken(t *testing.T, router http.Handler, userID, role string) string {
	t.Helper()
	value, ok := testAccountRepositories.Load(router)
	if !ok {
		t.Fatal("test router must use a real account repository")
	}
	repository := value.(*repo.GormUserBindingRepository)
	user, err := repository.GetByWeComUserID(t.Context(), userID)
	if err != nil {
		t.Fatal(err)
	}
	if user == nil {
		user = &types.WeComUserBinding{Username: userID, WeComUserID: userID, Role: types.UserRole(role), Status: types.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := repository.Create(t.Context(), user); err != nil {
			t.Fatal(err)
		}
	}
	sid := fmt.Sprintf("test-session-%d-%d", user.ID, time.Now().UnixNano())
	if err := repository.CreateSession(t.Context(), &security.Session{ID: sid, AccountID: user.ID, AuthVersion: user.AuthVersion, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(7 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	return issueAccountAccessToken(t, user, sid)
}

func doJSON(t *testing.T, router http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}

	req := httptest.NewRequest(method, path, &payload)
	if method == http.MethodPost && (path == "/api/leads" || strings.HasSuffix(path, "/followups")) {
		req.Header.Set("Idempotency-Key", uuid.NewString())
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func doRaw(t *testing.T, router http.Handler, method, path, token string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func doMultipart(t *testing.T, router http.Handler, path, token, fieldName, filename string, file []byte, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	part, err := writer.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(file); err != nil {
		t.Fatal(err)
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &payload)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func signAdLeadPayload(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func decodeResponse(t *testing.T, rec *httptest.ResponseRecorder, dst any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
}

func maskMobileForTest(mobile string) string {
	if len(mobile) < 7 {
		return mobile
	}
	return mobile[:3] + "****" + mobile[len(mobile)-4:]
}

type fakeWeComClient struct {
	mu      sync.Mutex
	counter int
}

func (c *fakeWeComClient) SendCard(ctx context.Context, req *wecom.SendCardRequest) (*wecom.SendCardResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counter++
	return &wecom.SendCardResponse{
		MsgID:        fmt.Sprintf("msg-%d", c.counter),
		ResponseCode: fmt.Sprintf("response-%d", c.counter),
	}, nil
}

func (c *fakeWeComClient) UpdateCard(ctx context.Context, req *wecom.UpdateCardRequest) (*wecom.UpdateCardResponse, error) {
	return &wecom.UpdateCardResponse{Success: true}, nil
}

func (c *fakeWeComClient) SendText(ctx context.Context, req *wecom.SendTextRequest) error {
	return nil
}

func (c *fakeWeComClient) GetAccessToken(ctx context.Context) (string, error) {
	return "access-token", nil
}

func (c *fakeWeComClient) VerifyCallbackURL(msgSignature, timestamp, nonce, echoStr string) (string, error) {
	return echoStr, nil
}

func (c *fakeWeComClient) DecryptCallback(msgSignature, timestamp, nonce string, encryptedData []byte) ([]byte, error) {
	return encryptedData, nil
}

func (c *fakeWeComClient) GetUserIDByCode(ctx context.Context, code string) (string, error) {
	return code, nil
}

func (c *fakeWeComClient) GetJSAPIConfig(ctx context.Context, url string) (*wecom.JSAPIConfig, error) {
	return &wecom.JSAPIConfig{
		AppID:     "CRM",
		Timestamp: time.Now().Unix(),
		NonceStr:  "nonce-test",
		Signature: "signature-test",
	}, nil
}

type fakeCache struct {
	mu              sync.Mutex
	values          map[string]string
	initialByPrefix map[string]int64
}

func newFakeCache() *fakeCache {
	return &fakeCache{values: make(map[string]string)}
}

func newFakeCacheWithInitialValue(prefix string, value int64) *fakeCache {
	return &fakeCache{
		values: make(map[string]string),
		initialByPrefix: map[string]int64{
			prefix: value,
		},
	}
}

func (c *fakeCache) Set(ctx context.Context, key string, value any, expiration time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = fmt.Sprint(value)
	return nil
}

func (c *fakeCache) Get(ctx context.Context, key string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.values[key], nil
}

func (c *fakeCache) SetNX(ctx context.Context, key string, value any, expiration time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.values[key]; exists {
		return false, nil
	}
	c.values[key] = fmt.Sprint(value)
	return true, nil
}

func (c *fakeCache) Del(ctx context.Context, keys ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, key := range keys {
		delete(c.values, key)
	}
	return nil
}

func (c *fakeCache) Exists(ctx context.Context, keys ...string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var count int64
	for _, key := range keys {
		if _, exists := c.values[key]; exists {
			count++
		}
	}
	return count, nil
}

func (c *fakeCache) Incr(ctx context.Context, key string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	current := int64(0)
	if value := c.values[key]; value != "" {
		_, _ = fmt.Sscan(value, &current)
	} else {
		for prefix, value := range c.initialByPrefix {
			if strings.HasPrefix(key, prefix) {
				current = value
				break
			}
		}
	}
	current++
	c.values[key] = fmt.Sprint(current)
	return current, nil
}

func (c *fakeCache) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return nil
}

func setTestAccountName(t *testing.T, router http.Handler, actor, name string) {
	t.Helper()
	value, _ := testAccountRepositories.Load(router)
	repository := value.(*repo.GormUserBindingRepository)
	user, err := repository.GetByWeComUserID(t.Context(), actor)
	if err != nil || user == nil {
		t.Fatal("missing fixture account")
	}
	user.EmployeeName = name
	user.PasswordHash = ""
	if err := repository.Update(t.Context(), user); err != nil {
		t.Fatal(err)
	}
}

type testLoginProtection struct{}

func (testLoginProtection) LoginAttempt(context.Context, string, string) (int64, error) {
	return 0, nil
}
