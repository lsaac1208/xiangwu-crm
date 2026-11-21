package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/internal/usecase"
)

func TestXiangwuFormIdempotencyAndAttribution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := repo.NewDB(&config.DatabaseConfig{Driver: "sqlite", Database: filepath.Join(t.TempDir(), "forms.db"), MaxConns: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&repo.LeadModel{}, &repo.XiangwuFormSubmission{}, &repo.LeadDeletionEventModel{}, &repo.LeadReminderRecordModel{}, &repo.GroupRobotNotificationModel{}); err != nil {
		t.Fatal(err)
	}
	leads := repo.NewGormLeadRepository(db)
	submissions := repo.NewGormXiangwuFormSubmissionRepository(db)
	create := usecase.NewCreateLeadUseCase(leads, nil).WithRoleAwareCreation(nil, nil, nil, repo.NewGormTransactionManager(db))
	handler := NewAdLeadHandler(create, leads, "manager-1").WithXiangwuForms("test-secret", submissions)
	post := func(path string, body map[string]any, key string) (int, map[string]any) {
		t.Helper()
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(payload)))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Request.Header.Set("X-Xiangwu-Form-Key", key)
		if path == "/website" {
			handler.ReceiveXiangwuWebsiteForm(c)
		} else {
			handler.ReceiveXiangwuLeadForm(c)
		}
		var result map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return recorder.Code, result
	}
	firstID := uuid.NewString()
	website := map[string]any{
		"externalLeadId": firstID, "studentName": "测试学生", "phone": "13912345678",
		"grade": "高三", "score": "420", "interest": "入学测评", "visitDate": "2026-10-01",
		"message": "希望周末联系", "pagePath": "/baoming/", "contentEntryId": 42,
		"contentRevisionId": 53, "accountName": "浏览器伪造渠道",
	}
	if code, _ := post("/website", website, "wrong"); code != http.StatusUnauthorized {
		t.Fatalf("invalid key status=%d", code)
	}
	code, created := post("/website", website, "test-secret")
	if code != http.StatusCreated || created["ok"] != true || created["duplicated"] != false {
		t.Fatalf("create status=%d response=%v", code, created)
	}
	code, duplicate := post("/website", website, "test-secret")
	if code != http.StatusOK || duplicate["duplicated"] != true || duplicate["lead_id"] != created["lead_id"] {
		t.Fatalf("duplicate status=%d response=%v", code, duplicate)
	}
	website["score"] = "430"
	if code, _ := post("/website", website, "test-secret"); code != http.StatusConflict {
		t.Fatalf("changed payload status=%d", code)
	}
	website["externalLeadId"] = uuid.NewString()
	if code, fresh := post("/website", website, "test-secret"); code != http.StatusCreated || fresh["lead_id"] == created["lead_id"] {
		t.Fatalf("new consultation status=%d response=%v", code, fresh)
	}
	lead, err := leads.GetByLeadNo(context.Background(), "xiangwu_website_form:"+firstID)
	if err != nil || lead == nil {
		t.Fatalf("lead lookup: %v", err)
	}
	if lead.SourceChannel != "官网报名咨询" || lead.ProductName != "官网报名咨询" || lead.ContentPath != "/baoming/" || lead.ContentEntryID != nil || lead.ContentRevisionID != nil || !strings.Contains(lead.SourceCampaign, "官网 CMS 内容编号：42") || !strings.Contains(lead.SourceCampaign, "计划到校：2026-10-01") {
		t.Fatalf("attribution incorrect: %+v", lead)
	}
	stored, err := submissions.Get(context.Background(), "xiangwu_website_form", firstID)
	if err != nil || stored == nil || stored.LeadID != lead.ID || !strings.Contains(stored.Details, "家长留言：希望周末联系") {
		t.Fatalf("stored details: %+v err=%v", stored, err)
	}
	legacy := map[string]any{"studentName": "兼容提交", "phone": "13912345678", "grade": "高二", "message": "咨询", "pagePath": "/form/"}
	c1, a := post("/lead", legacy, "test-secret")
	c2, b := post("/lead", legacy, "test-secret")
	if c1 != http.StatusCreated || c2 != http.StatusCreated || a["lead_id"] == b["lead_id"] {
		t.Fatalf("legacy submissions %d/%d %v/%v", c1, c2, a, b)
	}
	lifecycle := repo.NewGormLeadLifecycleRepository(db, nil)
	if err := lifecycle.Delete(context.Background(), lead.ID, lead.LeadNo, lead.Version, lead.ProfileVersion, "admin", "清理测试"); err != nil {
		t.Fatal(err)
	}
	website["externalLeadId"] = firstID
	website["score"] = "420"
	if code, deleted := post("/website", website, "test-secret"); code != http.StatusGone || deleted["error"] != "submission_deleted" {
		t.Fatalf("deleted submission retry status=%d response=%v", code, deleted)
	}
	if err := lifecycle.Restore(context.Background(), lead.ID, "admin", "恢复测试"); err != nil {
		t.Fatal(err)
	}
	if code, restored := post("/website", website, "test-secret"); code != http.StatusOK || restored["lead_id"] != created["lead_id"] {
		t.Fatalf("restored submission retry status=%d response=%v", code, restored)
	}
}
