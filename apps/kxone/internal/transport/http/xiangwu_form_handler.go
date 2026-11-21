package http

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
	"github.com/lsaac128/kxone/internal/usecase"
)

var xiangwuPhonePattern = regexp.MustCompile(`^1[3-9][0-9]{9}$`)

func (h *AdLeadHandler) ReceiveXiangwuWebsiteForm(c *gin.Context) {
	h.receiveXiangwuForm(c, "xiangwu_website_form", "官网报名咨询", "官网报名咨询")
}

func (h *AdLeadHandler) ReceiveXiangwuLeadForm(c *gin.Context) {
	h.receiveXiangwuForm(c, "xiangwu_lead_form", "独立咨询表单", "独立咨询表单")
}

type xiangwuFormPayload struct {
	StudentName       string `json:"studentName"`
	Phone             string `json:"phone"`
	Grade             string `json:"grade"`
	Score             string `json:"score"`
	Interest          string `json:"interest"`
	VisitDate         string `json:"visitDate"`
	Message           string `json:"message"`
	PagePath          string `json:"pagePath"`
	ContentPath       string `json:"contentPath"`
	ContentEntryID    any    `json:"contentEntryId"`
	ContentRevisionID any    `json:"contentRevisionId"`
	PageKey           string `json:"pageKey"`
	ExternalLeadID    string `json:"externalLeadId"`
	Website           string `json:"website"`
}

func (h *AdLeadHandler) receiveXiangwuForm(c *gin.Context, source, channel, product string) {
	started := time.Now()
	externalID := ""
	status := http.StatusInternalServerError
	leadID := int64(0)
	defer func() {
		log.Printf("xiangwu_form source=%s submission_id=%s status=%d duration_ms=%d lead_id=%d", source, externalID, status, time.Since(started).Milliseconds(), leadID)
	}()
	fail := func(code int, reason string) {
		status = code
		c.JSON(code, gin.H{"ok": false, "error": reason})
	}
	if h.xiangwuFormKey == "" || h.xiangwuFormRepo == nil || h.defaultManagerUserID == "" {
		fail(http.StatusServiceUnavailable, "service_unavailable")
		return
	}
	supplied := c.GetHeader("X-Xiangwu-Form-Key")
	if subtle.ConstantTimeCompare([]byte(supplied), []byte(h.xiangwuFormKey)) != 1 {
		fail(http.StatusUnauthorized, "invalid_form_key")
		return
	}
	if c.ContentType() != "application/json" {
		fail(http.StatusUnsupportedMediaType, "json_required")
		return
	}
	var raw xiangwuFormPayload
	if err := c.ShouldBindJSON(&raw); err != nil {
		fail(http.StatusBadRequest, "invalid_json")
		return
	}
	if strings.TrimSpace(raw.Website) != "" {
		fail(http.StatusUnprocessableEntity, "invalid_submission")
		return
	}
	raw.StudentName = strings.TrimSpace(raw.StudentName)
	raw.Phone = strings.TrimSpace(raw.Phone)
	raw.Grade = strings.TrimSpace(raw.Grade)
	raw.Score = strings.TrimSpace(raw.Score)
	raw.Interest = strings.TrimSpace(raw.Interest)
	raw.VisitDate = strings.TrimSpace(raw.VisitDate)
	raw.Message = strings.TrimSpace(raw.Message)
	raw.PagePath = strings.TrimSpace(raw.PagePath)
	raw.ContentPath = strings.TrimSpace(raw.ContentPath)
	raw.PageKey = strings.TrimSpace(raw.PageKey)
	if raw.StudentName == "" || len([]rune(raw.StudentName)) > 64 || !xiangwuPhonePattern.MatchString(raw.Phone) || raw.Grade == "" || len([]rune(raw.Grade)) > 64 || len([]rune(raw.Score)) > 80 || len([]rune(raw.Interest)) > 80 || len([]rune(raw.VisitDate)) > 32 || len([]rune(raw.Message)) > 240 {
		fail(http.StatusUnprocessableEntity, "invalid_form_fields")
		return
	}
	path := raw.PagePath
	if path == "" {
		path = raw.ContentPath
	}
	if path == "" && source == "xiangwu_lead_form" {
		if raw.PageKey == "xiangwu-school" {
			path = "/form-xiangwu/"
		} else {
			path = "/form/"
		}
	}
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\\\n\r") || len(path) > 512 {
		fail(http.StatusUnprocessableEntity, "invalid_page_path")
		return
	}
	raw.PagePath = path
	// Browser-supplied account and form names are ignored. CMS references are
	// kept as labelled website references, never as local CRM foreign keys.
	details := []string{"当前阶段：" + raw.Grade}
	appendDetail := func(label, value string) {
		if value != "" {
			details = append(details, label+"："+value)
		}
	}
	appendDetail("当前分数", raw.Score)
	appendDetail("咨询方向", raw.Interest)
	appendDetail("计划到校", raw.VisitDate)
	if source != "xiangwu_website_form" || (raw.Score == "" && raw.Interest == "" && raw.VisitDate == "") || !strings.HasPrefix(raw.Message, "咨询方向：") {
		appendDetail("家长留言", raw.Message)
	}
	appendDetail("提交页面", path)
	if raw.ContentEntryID != nil {
		appendDetail("官网 CMS 内容编号", fmt.Sprint(raw.ContentEntryID))
	}
	if raw.ContentRevisionID != nil {
		appendDetail("官网 CMS 版本编号", fmt.Sprint(raw.ContentRevisionID))
	}
	detailText := strings.Join(details, "；")
	if len([]rune(detailText)) > 480 {
		// The CRM source_campaign column is varchar(512). Reject, never crop.
		fail(http.StatusUnprocessableEntity, "form_details_too_long")
		return
	}
	compatibility := false
	if raw.ExternalLeadID == "" {
		raw.ExternalLeadID = uuid.NewString()
		compatibility = true
	}
	parsedID, err := uuid.Parse(raw.ExternalLeadID)
	if err != nil || parsedID.Version() != 4 {
		fail(http.StatusUnprocessableEntity, "invalid_external_lead_id")
		return
	}
	externalID = parsedID.String()
	raw.ExternalLeadID = ""
	raw.Website = ""
	raw.ContentPath = ""
	canonical, err := json.Marshal(struct {
		Source  string `json:"source"`
		Name    string `json:"name"`
		Phone   string `json:"phone"`
		Details string `json:"details"`
	}{source, raw.StudentName, raw.Phone, detailText})
	if err != nil {
		fail(http.StatusInternalServerError, "encoding_failed")
		return
	}
	hash := sha256.Sum256(canonical)
	hashText := hex.EncodeToString(hash[:])
	existing, err := h.xiangwuFormRepo.Get(c.Request.Context(), source, externalID)
	if err != nil {
		fail(http.StatusServiceUnavailable, "lookup_failed")
		return
	}
	if existing != nil {
		h.respondXiangwuDuplicate(c, existing, hashText, &status, &leadID)
		return
	}
	item := &repo.XiangwuFormSubmission{
		Source: source, ExternalLeadID: externalID, PayloadHash: hashText,
		Details: detailText, SourcePath: path, CompatibilityRequest: compatibility,
		CreatedAt: time.Now(),
	}
	result, err := h.createUseCase.Execute(c.Request.Context(), &usecase.CreateLeadRequest{
		LeadNo:                source + ":" + externalID,
		CustomerName:          raw.StudentName,
		CustomerMobile:        raw.Phone,
		ProductName:           product,
		SourceChannel:         channel,
		SourceCampaign:        detailText,
		ContentPath:           path,
		LeadLevel:             "high",
		DispatchManagerUserID: h.defaultManagerUserID,
		XiangwuFormSubmission: item,
	})
	if err != nil {
		// A concurrent request can win the unique index after our lookup.
		for attempt := 0; attempt < 5; attempt++ {
			time.Sleep(time.Duration(attempt+1) * 50 * time.Millisecond)
			existing, lookupErr := h.xiangwuFormRepo.Get(c.Request.Context(), source, externalID)
			if lookupErr == nil && existing != nil {
				h.respondXiangwuDuplicate(c, existing, hashText, &status, &leadID)
				return
			}
		}
		fail(http.StatusServiceUnavailable, "save_failed")
		return
	}
	leadID = result.Lead.ID
	if h.groupRobotNotifier != nil {
		notification := wecom.GroupRobotLead{
			LeadID: leadID, BusinessVersion: result.Lead.Version,
			Source: source, CustomerName: raw.StudentName, Phone: raw.Phone,
			FormName: product, CampaignName: detailText,
		}
		// The durable lead and manager task already exist. Bound the secondary
		// notification enqueue so it cannot hold the form response past the proxy
		// deadline; a failed wake leaves an existing pending task for the worker.
		notifyContext, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 2*time.Second)
		defer cancel()
		if err := h.groupRobotNotifier.NotifyLead(notifyContext, notification); err != nil {
			log.Printf("xiangwu_form_notification_failed source=%s submission_id=%s lead_id=%d", source, externalID, leadID)
		}
	}
	status = http.StatusCreated
	c.JSON(status, gin.H{"ok": true, "lead_id": leadID, "lead_no": result.Lead.LeadNo, "duplicated": false})
}

func (h *AdLeadHandler) respondXiangwuDuplicate(c *gin.Context, item *repo.XiangwuFormSubmission, hash string, status *int, leadID *int64) {
	if subtle.ConstantTimeCompare([]byte(item.PayloadHash), []byte(hash)) != 1 {
		*status = http.StatusConflict
		c.JSON(*status, gin.H{"ok": false, "error": "submission_content_conflict"})
		return
	}
	lead, err := h.leadRepo.GetByID(c.Request.Context(), item.LeadID)
	if err != nil {
		*status = http.StatusServiceUnavailable
		c.JSON(*status, gin.H{"ok": false, "error": "lookup_failed"})
		return
	}
	if lead == nil {
		*status = http.StatusGone
		c.JSON(*status, gin.H{"ok": false, "error": "submission_deleted"})
		return
	}
	*leadID = item.LeadID
	*status = http.StatusOK
	c.JSON(*status, gin.H{"ok": true, "lead_id": item.LeadID, "lead_no": item.Source + ":" + item.ExternalLeadID, "duplicated": true})
}
