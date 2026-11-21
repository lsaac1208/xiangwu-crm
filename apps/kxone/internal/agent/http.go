package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lsaac128/kxone/internal/security"
	"gorm.io/gorm"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Handler struct {
	service            *Service
	historyAudit       func(*gin.Context, string, string) error
	streamSessionValid func(*gin.Context) bool
}

func (h *Handler) WithHistoryAccess(audit func(*gin.Context, string, string) error, valid func(*gin.Context) bool) *Handler {
	h.historyAudit = audit
	h.streamSessionValid = valid
	return h
}

func NewHandler(service *Service) *Handler { return &Handler{service: service} }
func (h *Handler) Register(g *gin.RouterGroup) {
	g.GET("/conversations", h.listConversations)
	g.POST("/conversations", h.createConversation)
	g.GET("/conversations/:id", h.getConversation)
	g.PATCH("/conversations/:id", h.renameConversation)
	g.DELETE("/conversations/:id", h.deleteConversation)
	g.POST("/conversations/:id/runs", h.start)
	g.POST("/conversations/:id/metric-runs", h.startMetric)
	g.GET("/metrics", h.metrics)
	g.GET("/runs/:id", h.getRun)
	g.GET("/runs/:id/events", h.events)
	g.POST("/runs/:id/cancel", h.cancel)
	g.GET("/results/:id", h.getResult)
	g.POST("/runs/:id/exports", h.export)
	g.GET("/exports/:id/download", h.download)
	g.GET("/settings", h.settings)
	g.PUT("/settings", h.updateSettings)
	g.POST("/settings/test", h.testConnection)
	g.GET("/preferences", h.preference)
	g.PUT("/preferences", h.putPreference)
	g.DELETE("/preferences", h.deletePreference)
	g.GET("/rules", h.rules)
	g.POST("/rules", h.proposeRule)
	g.POST("/rules/:id/publish", h.publishRule)
	g.POST("/rules/:id/rollback", h.publishRule)
}
func actor(c *gin.Context) Actor {
	return Actor{AccountID: c.GetInt64("account_id"), AuthVersion: c.GetInt64("auth_version"), Role: c.GetString("role")}
}
func fail(c *gin.Context, err error) {
	code := http.StatusBadRequest
	kind := "agent_error"
	message := err.Error()
	switch {
	case errors.Is(err, ErrForbidden):
		code = 403
		kind = "forbidden"
		message = "无权访问此会话或资源"
	case errors.Is(err, ErrConflict):
		code = 409
		kind = "agent_conflict"
		message = "资料已变更，输入已保留，请重新核对"
	case errors.Is(err, ErrUnavailable):
		code = 503
		kind = "agent_unavailable"
		message = "AI 分析尚未启用或模型未配置，请联系管理员"
	case errors.Is(err, ErrBudgetExceeded) || errors.Is(err, ErrRunBudgetExceeded):
		code = 429
		kind = "budget_exceeded"
		message = "模型调用额度不足，已有对话和导出仍可使用"
	case message == "agent_busy":
		code = 429
		kind = "agent_busy"
		message = "已有分析正在运行，请稍后重试"
	}
	c.JSON(code, gin.H{"error": kind, "message": message})
}
func validText(c *gin.Context) (string, bool) {
	var p struct {
		Content string `json:"content"`
	}
	if c.ShouldBindJSON(&p) != nil || len(p.Content) > 8000 {
		fail(c, errors.New("内容格式不正确或超过长度限制"))
		return "", false
	}
	return strings.TrimSpace(p.Content), true
}
func (h *Handler) admin(c *gin.Context) bool {
	if err := h.service.validate(c.Request.Context(), actor(c)); err != nil {
		fail(c, err)
		return false
	}
	if c.GetString("role") != "admin" {
		fail(c, ErrForbidden)
		return false
	}
	return true
}
func (h *Handler) listConversations(c *gin.Context) {
	a := actor(c)
	if err := h.service.validate(c.Request.Context(), a); err != nil {
		fail(c, err)
		return
	}
	q := h.service.db.Where("account_id = ?", a.AccountID)
	search := strings.TrimSpace(c.Query("search"))
	if len(search) > 500 {
		fail(c, errors.New("搜索内容过长"))
		return
	}
	if search != "" {
		q = q.Where("(title LIKE ? OR id IN (SELECT conversation_id FROM ai_messages WHERE content LIKE ?))", "%"+search+"%", "%"+search+"%")
	}
	var total int64
	q.Model(&Conversation{}).Count(&total)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	var rows []Conversation
	if err := q.Order("updated_at DESC,id").Limit(50).Offset((page - 1) * 50).Find(&rows).Error; err != nil {
		fail(c, errors.New("会话读取失败"))
		return
	}
	if rows == nil {
		rows = []Conversation{}
	}
	c.JSON(200, gin.H{"conversations": rows, "total": total})
}
func (h *Handler) createConversation(c *gin.Context) {
	a := actor(c)
	if err := h.service.validate(c.Request.Context(), a); err != nil {
		fail(c, err)
		return
	}
	var p struct {
		Title string `json:"title"`
	}
	if c.ShouldBindJSON(&p) != nil {
		p.Title = ""
	}
	title := strings.TrimSpace(p.Title)
	if title == "" {
		title = "新对话"
	}
	if len([]rune(title)) > 100 {
		fail(c, errors.New("标题最多100字"))
		return
	}
	row := Conversation{ID: uuid.NewString(), AccountID: a.AccountID, Title: title, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := h.service.db.Create(&row).Error; err != nil {
		fail(c, errors.New("创建会话失败"))
		return
	}
	c.JSON(201, row)
}
func (h *Handler) getConversation(c *gin.Context) {
	s := h.service
	row, err := s.ownedConversation(c.Request.Context(), c.Param("id"), actor(c))
	if err != nil {
		fail(c, err)
		return
	}
	var messages []Message
	query := s.db.Where("conversation_id = ?", row.ID)
	before := c.Query("before")
	if before != "" {
		t, e := time.Parse(time.RFC3339Nano, before)
		if e != nil {
			fail(c, errors.New("分页时间无效"))
			return
		}
		query = query.Where("created_at < ?", t)
	}
	if err := query.Order("created_at DESC,id DESC").Limit(100).Find(&messages).Error; err != nil {
		fail(c, errors.New("消息读取失败"))
		return
	}
	items := []any{}
	for i := len(messages) - 1; i >= 0; i-- {
		items = append(items, messages[i].DTO())
	}
	var runs []Run
	_ = s.db.Where("conversation_id = ?", row.ID).Order("created_at DESC").Limit(20).Find(&runs).Error
	c.JSON(200, gin.H{"conversation": row, "messages": items, "runs": runs, "has_more": len(messages) == 100})
}
func (h *Handler) renameConversation(c *gin.Context) {
	row, err := h.service.ownedConversation(c.Request.Context(), c.Param("id"), actor(c))
	if err != nil {
		fail(c, err)
		return
	}
	var p struct {
		Title string `json:"title"`
	}
	if c.ShouldBindJSON(&p) != nil || strings.TrimSpace(p.Title) == "" || len([]rune(p.Title)) > 100 {
		fail(c, errors.New("标题须为1至100字"))
		return
	}
	row.Title = strings.TrimSpace(p.Title)
	row.UpdatedAt = time.Now().UTC()
	if err := h.service.db.Save(row).Error; err != nil {
		fail(c, errors.New("保存失败"))
		return
	}
	c.JSON(200, row)
}
func (h *Handler) deleteConversation(c *gin.Context) {
	s := h.service
	row, err := s.ownedConversation(c.Request.Context(), c.Param("id"), actor(c))
	if err != nil {
		fail(c, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	s.db.Model(&Run{}).Where("conversation_id = ? AND status IN ?", row.ID, []string{"running", "queued"}).Count(&n)
	if n > 0 {
		fail(c, errors.New("请先停止正在运行的分析，再删除对话"))
		return
	}
	var runs []Run
	s.db.Where("conversation_id = ?", row.ID).Find(&runs)
	ids := []string{}
	for _, r := range runs {
		ids = append(ids, r.ID)
	}
	var files []ExportRecord
	s.db.Where("run_id IN ?", ids).Find(&files)
	err = s.db.Transaction(func(tx *gorm.DB) error {
		for _, m := range []any{&Event{}, &ResultRecord{}, &ExportRecord{}} {
			if err := tx.Where("run_id IN ?", ids).Delete(m).Error; err != nil {
				return err
			}
		}
		for _, m := range []any{&Message{}, &Run{}} {
			if err := tx.Where("conversation_id = ?", row.ID).Delete(m).Error; err != nil {
				return err
			}
		}
		return tx.Delete(row).Error
	})
	if err != nil {
		fail(c, errors.New("删除失败"))
		return
	}
	for _, f := range files {
		_ = os.Remove(f.Path)
	}
	c.Status(204)
}
func (h *Handler) start(c *gin.Context) {
	var p struct {
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}
	if c.ShouldBindJSON(&p) != nil {
		fail(c, errors.New("问题格式不正确"))
		return
	}
	run, err := h.service.Start(c.Request.Context(), actor(c), c.Param("id"), p.Message, p.RequestID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(202, run)
}
func (h *Handler) metrics(c *gin.Context) {
	if err := h.service.validate(c.Request.Context(), actor(c)); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"metrics": MetricCatalog(), "fixed_metrics": h.service.cfg.FixedMetrics})
}
func (h *Handler) startMetric(c *gin.Context) {
	var p struct {
		MetricID  string       `json:"metric_id"`
		Params    MetricParams `json:"params"`
		RequestID string       `json:"request_id"`
	}
	if c.ShouldBindJSON(&p) != nil {
		fail(c, errors.New("指标参数格式不正确"))
		return
	}
	run, err := h.service.StartMetric(c.Request.Context(), actor(c), c.Param("id"), MetricSelection{MetricID: p.MetricID, Params: p.Params}, p.RequestID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(202, run)
}
func (h *Handler) cancel(c *gin.Context) {
	if err := h.service.Cancel(c.Request.Context(), actor(c), c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}
func (h *Handler) getRun(c *gin.Context) {
	s := h.service
	run, err := s.ownedRun(c.Request.Context(), c.Param("id"), actor(c))
	if err != nil {
		fail(c, err)
		return
	}
	var msgs []Message
	s.db.Where("run_id = ?", run.ID).Order("created_at,id").Find(&msgs)
	items := []any{}
	for _, m := range msgs {
		items = append(items, m.DTO())
	}
	results, err := s.results(c.Request.Context(), run.ID)
	if err != nil {
		fail(c, errors.New("结果读取失败"))
		return
	}
	var finalBlocks []Block
	for _, message := range msgs {
		if message.Role == "assistant" && strings.TrimSpace(message.BlocksJSON) != "" {
			if err := json.Unmarshal([]byte(message.BlocksJSON), &finalBlocks); err != nil {
				fail(c, errors.New("回答引用读取失败"))
				return
			}
		}
	}
	selection, _, err := ResolveRunAnswerResults(run.Status, finalBlocks, results)
	if err != nil {
		fail(c, errors.New("回答引用与保存结果不一致"))
		return
	}
	for i := range results {
		results[i] = resultPage(results[i], 0, 200)
	}
	c.JSON(200, gin.H{"run": run, "messages": items, "results": results, "result_selection": selection})
}
func (h *Handler) events(c *gin.Context) {
	s := h.service
	a := actor(c)
	run, err := s.ownedRun(c.Request.Context(), c.Param("id"), a)
	if err != nil {
		fail(c, err)
		return
	}
	if h.historyAudit == nil || h.streamSessionValid == nil {
		c.JSON(503, gin.H{"error": "history_access_unavailable"})
		return
	}
	if !h.streamSessionValid(c) {
		c.JSON(401, gin.H{"error": "session_expired"})
		return
	}
	if err := h.historyAudit(c, run.ID, "allowed"); err != nil {
		c.JSON(503, gin.H{"error": "access_audit_unavailable"})
		return
	}
	after, _ := strconv.ParseInt(c.Query("after"), 10, 64)
	if after == 0 {
		after, _ = strconv.ParseInt(c.GetHeader("Last-Event-ID"), 10, 64)
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Accel-Buffering", "no")
	controller := http.NewResponseController(c.Writer)
	_ = controller.SetWriteDeadline(time.Now().Add(210 * time.Second))
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	send := func() bool {
		if !h.streamSessionValid(c) || s.validate(c.Request.Context(), a) != nil {
			return false
		}
		// Read status before the event batch. A terminal status guarantees that
		// finishRun already committed its final events; reading it after an old
		// event batch could otherwise close this stream before delivering done.
		var current Run
		if s.db.WithContext(c.Request.Context()).First(&current, "id = ?", run.ID).Error != nil {
			return false
		}
		var rows []Event
		if s.db.WithContext(c.Request.Context()).Where("run_id = ? AND seq > ?", run.ID, after).Order("seq").Limit(100).Find(&rows).Error != nil {
			return false
		}
		for _, e := range rows {
			var data any
			_ = json.Unmarshal([]byte(e.Data), &data)
			payload, _ := json.Marshal(gin.H{"seq": e.Seq, "type": e.Type, "data": data})
			if _, err := fmt.Fprintf(c.Writer, "id: %d\nevent: update\ndata: %s\n\n", e.Seq, payload); err != nil {
				return false
			}
			after = e.Seq
		}
		c.Writer.Flush()
		return current.Status == "running" || current.Status == "queued" || len(rows) == 100
	}
	if !send() {
		return
	}
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			if !send() {
				return
			}
		case <-heartbeat.C:
			if !h.streamSessionValid(c) || s.validate(c.Request.Context(), a) != nil {
				return
			}
			_, _ = fmt.Fprint(c.Writer, ": heartbeat\n\n")
			c.Writer.Flush()
		}
	}
}
func (h *Handler) getResult(c *gin.Context) {
	s := h.service
	var row ResultRecord
	if s.db.First(&row, "id = ?", c.Param("id")).Error != nil {
		fail(c, ErrForbidden)
		return
	}
	if _, err := s.ownedRun(c.Request.Context(), row.RunID, actor(c)); err != nil {
		fail(c, err)
		return
	}
	var r Result
	if json.Unmarshal([]byte(row.Data), &r) != nil {
		fail(c, errors.New("结果读取失败"))
		return
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	c.JSON(200, resultPage(r, offset, limit))
}
func (h *Handler) settings(c *gin.Context) {
	s := h.service
	if err := s.validate(c.Request.Context(), actor(c)); err != nil {
		fail(c, err)
		return
	}
	cfg, setting, err := s.runtimeConfig(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	summary, err := s.budget.Summary(c.Request.Context(), time.Now())
	if err != nil {
		fail(c, errors.New("用量读取失败"))
		return
	}
	out := gin.H{"enabled": setting.Enabled, "configured": cfg.APIKey != "" && cfg.BaseURL != "", "key_configured": cfg.APIKey != "", "key_hint": "", "model": cfg.Model, "budget_micro": setting.BudgetMicro, "spent_micro": summary.SettledMicro, "reserved_micro": summary.ReservedMicro, "month": summary.Month, "warning": summary.TotalMicro >= setting.BudgetMicro*8/10, "version": setting.Version, "input_price_cny": float64(setting.InputPriceMicro) / 1e6, "output_price_cny": float64(setting.OutputPriceMicro) / 1e6, "price_version": setting.PriceVersion, "usage": summary}
	profile, profileErr := readFallbackProfile(setting.FallbackJSON)
	if profileErr != nil {
		fail(c, profileErr)
		return
	}
	out["fallback"] = profile.dto(actor(c).Role == "admin")
	if actor(c).Role == "admin" {
		health, healthErr := runHealthSummary(c.Request.Context(), s.db, time.Now())
		if healthErr != nil {
			fail(c, errors.New("分析运行情况读取失败"))
			return
		}
		out["run_health"] = health
		out["base_url"] = setting.BaseURL
		if cfg.APIKey != "" {
			out["key_hint"] = "已配置"
		}
	}
	c.JSON(200, out)
}

type settingsInput struct {
	Fallback        *fallbackInput `json:"fallback"`
	Target          string         `json:"target"`
	BaseURL         string         `json:"base_url"`
	APIKey          string         `json:"api_key"`
	Model           string         `json:"model"`
	Enabled         bool           `json:"enabled"`
	BudgetMicro     int64          `json:"budget_micro"`
	ExpectedVersion int64          `json:"expected_version"`
	ClearAPIKey     bool           `json:"clear_api_key"`
	InputPriceCNY   *float64       `json:"input_price_cny"`
	OutputPriceCNY  *float64       `json:"output_price_cny"`
}

func (h *Handler) updateSettings(c *gin.Context) {
	if !h.admin(c) {
		return
	}
	s := h.service
	var p settingsInput
	if c.ShouldBindJSON(&p) != nil || p.ExpectedVersion < 1 || p.BudgetMicro < 1_000_000 || p.BudgetMicro > 10_000_000_000 || len(p.APIKey) > 4096 || len(p.Model) > 128 {
		fail(c, errors.New("请检查模型设置和版本"))
		return
	}
	if p.BaseURL != "" {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		if err := validateEndpoint(ctx, p.BaseURL, s.allowLocal); err != nil {
			fail(c, err)
			return
		}
	}
	if p.Enabled && (p.Model == "" || p.BaseURL == "") {
		fail(c, errors.New("启用前须填写URL和模型名称"))
		return
	}
	var current Settings
	if s.db.First(&current, 1).Error != nil {
		fail(c, errors.New("配置读取失败"))
		return
	}
	if (strings.TrimSpace(p.Model) != current.Model || strings.TrimRight(strings.TrimSpace(p.BaseURL), "/") != current.BaseURL) && (p.InputPriceCNY == nil || p.OutputPriceCNY == nil) {
		fail(c, errors.New("更换模型或URL时请核对并提交输入、输出单价"))
		return
	}
	p.APIKey = strings.TrimSpace(p.APIKey)
	updates := map[string]any{"base_url": strings.TrimRight(strings.TrimSpace(p.BaseURL), "/"), "model": strings.TrimSpace(p.Model), "enabled": p.Enabled, "budget_micro": p.BudgetMicro, "version": gorm.Expr("version + 1"), "updated_at": time.Now().UTC(), "updated_by": actor(c).AccountID}
	if p.ClearAPIKey {
		updates["key_ciphertext"] = ""
	} else if p.APIKey != "" {
		if s.protector == nil {
			fail(c, errors.New("密钥加密服务未配置"))
			return
		}
		key, err := s.protector.Protect(strings.TrimSpace(p.APIKey))
		if err != nil {
			fail(c, errors.New("密钥加密失败"))
			return
		}
		if !security.IsEncryptedField(key) {
			fail(c, errors.New("请先配置服务端字段加密服务"))
			return
		}
		updates["key_ciphertext"] = key
	}
	if p.InputPriceCNY != nil && p.OutputPriceCNY != nil {
		if *p.InputPriceCNY <= 0 || *p.OutputPriceCNY <= 0 || *p.InputPriceCNY > 10000 || *p.OutputPriceCNY > 10000 {
			fail(c, errors.New("请填写有效的输入和输出单价"))
			return
		}
		updates["input_price_micro"] = int64(*p.InputPriceCNY*1e6 + 0.5)
		updates["output_price_micro"] = int64(*p.OutputPriceCNY*1e6 + 0.5)
		updates["price_version"] = time.Now().UTC().Format(time.RFC3339)
	}
	fallbackChanged := false
	if p.Fallback != nil {
		profile, err := readFallbackProfile(current.FallbackJSON)
		if err != nil {
			fail(c, err)
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		prepared, err := s.prepareFallback(ctx, profile, *p.Fallback)
		cancel()
		if err != nil {
			fail(c, err)
			return
		}
		raw, _ := json.Marshal(prepared)
		updates["fallback_json"] = string(raw)
		fallbackChanged = string(raw) != current.FallbackJSON
	}
	result := s.db.Model(&Settings{}).Where("id = 1 AND version = ?", p.ExpectedVersion).Updates(updates)
	if result.Error != nil {
		fail(c, errors.New("配置保存失败"))
		return
	}
	if result.RowsAffected != 1 {
		fail(c, ErrConflict)
		return
	}
	if !p.Enabled || p.BudgetMicro < current.BudgetMicro || p.ClearAPIKey || p.APIKey != "" || p.Model != current.Model || p.BaseURL != current.BaseURL || fallbackChanged {
		s.Close()
	}
	h.settings(c)
}
func (h *Handler) testConnection(c *gin.Context) {
	if !h.admin(c) {
		return
	}
	s := h.service
	cfg, setting, err := s.runtimeConfig(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	var p settingsInput
	if c.ShouldBindJSON(&p) != nil {
		fail(c, errors.New("测试配置格式错误"))
		return
	}
	if p.Target != "" && p.Target != "primary" && p.Target != "fallback" {
		fail(c, errors.New("未知模型测试目标"))
		return
	}
	if p.Target == "fallback" {
		profile, err := readFallbackProfile(setting.FallbackJSON)
		if err != nil {
			fail(c, err)
			return
		}
		if p.Fallback != nil {
			draft := *p.Fallback
			draft.Enabled = true
			ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
			profile, err = s.prepareFallback(ctx, profile, draft)
			cancel()
			if err != nil {
				fail(c, err)
				return
			}
		}
		profile.Enabled = true
		withBackup, err := s.applyFallback(cfg, profile)
		if err != nil || withBackup.Fallback == nil {
			fail(c, errors.New("请补齐备用模型设置"))
			return
		}
		cfg = *withBackup.Fallback
		// The backup draft has its own prices; primary draft fields must not
		// override either the provider or its billing version.
		p.BaseURL, p.Model, p.APIKey = "", "", ""
		p.InputPriceCNY, p.OutputPriceCNY = nil, nil
	}
	if p.BaseURL != "" {
		cfg.BaseURL = p.BaseURL
	}
	if p.Model != "" {
		cfg.Model = p.Model
	}
	if p.APIKey != "" {
		cfg.APIKey = p.APIKey
	}
	if p.Target != "fallback" && (cfg.Model != setting.Model || cfg.BaseURL != setting.BaseURL) && (p.InputPriceCNY == nil || p.OutputPriceCNY == nil) {
		fail(c, errors.New("测试新的模型或URL时请确认输入和输出单价"))
		return
	}
	if p.InputPriceCNY != nil && p.OutputPriceCNY != nil {
		if *p.InputPriceCNY <= 0 || *p.OutputPriceCNY <= 0 || *p.InputPriceCNY > 10000 || *p.OutputPriceCNY > 10000 {
			fail(c, errors.New("请核对输入和输出单价"))
			return
		}
		cfg.InputPriceMicro = int64(*p.InputPriceCNY*1e6 + 0.5)
		cfg.OutputPriceMicro = int64(*p.OutputPriceCNY*1e6 + 0.5)
	}
	if cfg.APIKey == "" || cfg.Model == "" || len(cfg.APIKey) > 4096 {
		fail(c, errors.New("请填写URL、API Key和模型名称"))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	testID := "test-" + uuid.NewString()
	account := actor(c).AccountID
	s.mu.Lock()
	if len(s.running) >= 2 || s.activeAccounts[account] {
		s.mu.Unlock()
		fail(c, errors.New("agent_busy"))
		return
	}
	s.running[testID] = cancel
	s.activeAccounts[account] = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.running, testID); delete(s.activeAccounts, account); s.mu.Unlock() }()
	if err := validateEndpoint(ctx, cfg.BaseURL, s.allowLocal); err != nil {
		fail(c, err)
		return
	}
	request := ModelRequest{Model: cfg.Model, Messages: []ModelMessage{{Role: "user", Content: "仅回答 OK，用于管理员主动发起的模型连通测试。"}}, Stream: false, MaxTokens: 32, Temperature: 0}
	applyProviderPolicy(&request)
	if strings.HasPrefix(cfg.Model, "qwen") {
		no := false
		request.EnableThinking = &no
	}
	raw, bodyErr := safeModelRequestBody(request, cfg.APIKey)
	if bodyErr != nil {
		fail(c, bodyErr)
		return
	}
	reserve := modelCost(int64(len(raw)+4096), 64, cfg)
	reservation, err := s.budget.Reserve(ctx, testID, actor(c).AccountID, reserve, setting.BudgetMicro, cfg.RunBudgetMicro, cfg.PriceVersion, time.Now())
	if err != nil {
		fail(c, err)
		return
	}
	started := time.Now()
	response, err := callModel(ctx, cfg, s.allowLocal, request, nil)
	if response.HasUsage {
		cost := modelCost(response.Usage.InputTokens, response.Usage.OutputTokens, cfg)
		if e := s.budget.Settle(context.Background(), reservation, cost, response.Usage); e != nil {
			fail(c, errors.New("测试用量结算失败"))
			return
		}
	}
	if err != nil {
		fail(c, err)
		return
	}
	if response.Message.Content == "" {
		fail(c, errors.New("连接成功但模型未返回有效内容"))
		return
	}
	c.JSON(200, gin.H{"ok": true, "latency_ms": time.Since(started).Milliseconds(), "model": cfg.Model, "usage": response.Usage, "message": "模型连接成功；实际分析还需模型支持工具调用"})
}
func (h *Handler) preference(c *gin.Context) {
	if err := h.service.validate(c.Request.Context(), actor(c)); err != nil {
		fail(c, err)
		return
	}
	var p Preference
	_ = h.service.db.First(&p, actor(c).AccountID).Error
	c.JSON(200, p)
}
func (h *Handler) putPreference(c *gin.Context) {
	if err := h.service.validate(c.Request.Context(), actor(c)); err != nil {
		fail(c, err)
		return
	}
	text, ok := validText(c)
	if !ok {
		return
	}
	p := Preference{AccountID: actor(c).AccountID, Content: text, UpdatedAt: time.Now().UTC()}
	if h.service.db.Save(&p).Error != nil {
		fail(c, errors.New("偏好保存失败"))
		return
	}
	c.JSON(200, p)
}
func (h *Handler) deletePreference(c *gin.Context) {
	if err := h.service.validate(c.Request.Context(), actor(c)); err != nil {
		fail(c, err)
		return
	}
	h.service.db.Delete(&Preference{}, actor(c).AccountID)
	c.Status(204)
}
func (h *Handler) rules(c *gin.Context) {
	if err := h.service.validate(c.Request.Context(), actor(c)); err != nil {
		fail(c, err)
		return
	}
	var rows []Rule
	q := h.service.db.Order("id DESC")
	if actor(c).Role != "admin" {
		q = q.Where("account_id = ? OR status = ?", actor(c).AccountID, "published")
	}
	q.Limit(100).Find(&rows)
	if rows == nil {
		rows = []Rule{}
	}
	c.JSON(200, gin.H{"rules": rows})
}
func (h *Handler) proposeRule(c *gin.Context) {
	if err := h.service.validate(c.Request.Context(), actor(c)); err != nil {
		fail(c, err)
		return
	}
	text, ok := validText(c)
	if !ok || text == "" {
		if ok {
			fail(c, errors.New("规则不能为空"))
		}
		return
	}
	row := Rule{AccountID: actor(c).AccountID, Content: text, Status: "pending", Version: 0, CreatedAt: time.Now().UTC()}
	if h.service.db.Create(&row).Error != nil {
		fail(c, errors.New("提交失败"))
		return
	}
	c.JSON(201, row)
}
func (h *Handler) publishRule(c *gin.Context) {
	if !h.admin(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		fail(c, errors.New("规则编号无效"))
		return
	}
	var row Rule
	if h.service.db.First(&row, id).Error != nil {
		fail(c, errors.New("规则不存在"))
		return
	}
	now := time.Now().UTC()
	err = h.service.db.Transaction(func(tx *gorm.DB) error {
		var version int64
		if e := tx.Model(&Rule{}).Select("COALESCE(MAX(version),0)").Scan(&version).Error; e != nil {
			return e
		}
		if e := tx.Model(&Rule{}).Where("status = ?", "published").Update("status", "archived").Error; e != nil {
			return e
		}
		if row.Status == "pending" {
			if e := tx.Model(&Rule{}).Where("id = ? AND status = ?", id, "pending").Update("status", "archived").Error; e != nil {
				return e
			}
		}
		published := Rule{AccountID: row.AccountID, Content: row.Content, Status: "published", Version: version + 1, CreatedAt: now, PublishedAt: &now, PublishedBy: actor(c).AccountID, SourceRuleID: row.ID}
		return tx.Create(&published).Error
	})
	if err != nil {
		fail(c, errors.New("发布规则失败"))
		return
	}
	h.rules(c)
}
func (h *Handler) export(c *gin.Context) {
	s := h.service
	run, err := s.ownedRun(c.Request.Context(), c.Param("id"), actor(c))
	if err != nil {
		fail(c, err)
		return
	}
	if run.Status == "running" || run.Status == "queued" {
		fail(c, errors.New("请等待分析结束后导出"))
		return
	}
	var p struct {
		Format string `json:"format"`
	}
	if c.ShouldBindJSON(&p) != nil || (p.Format != "html" && p.Format != "xlsx") {
		fail(c, errors.New("导出格式必须为xlsx或html"))
		return
	}
	var messages []Message
	if err := s.db.Where("run_id = ?", run.ID).Order("created_at,id").Find(&messages).Error; err != nil {
		fail(c, errors.New("回答读取失败"))
		return
	}
	doc := ExportDocument{Title: "CRM 分析报告", Model: run.Model, RuleVersion: run.RuleVersion, RunID: run.ID, RunStatus: run.Status}
	if run.SnapshotAt != nil {
		doc.SnapshotAt = *run.SnapshotAt
	}
	for _, m := range messages {
		if m.Role == "user" {
			doc.Question = m.Content
		} else if m.Role == "assistant" {
			if m.BlocksJSON != "" && json.Unmarshal([]byte(m.BlocksJSON), &doc.Blocks) != nil {
				fail(c, errors.New("回答引用读取失败"))
				return
			}
			if len(doc.Blocks) == 0 && m.Content != "" {
				doc.Blocks = []Block{{Type: "markdown", Text: m.Content}}
			}
		}
	}
	doc.Results, err = s.results(c.Request.Context(), run.ID)
	if err != nil {
		fail(c, errors.New("导出结果读取失败"))
		return
	}
	doc, err = prepareExportDocument(doc)
	if err != nil {
		fail(c, errors.New("最终引用结果不完整，无法导出；请查看回答中的查询依据"))
		return
	}
	var bytes []byte
	if p.Format == "xlsx" {
		bytes, err = RenderXLSX(doc)
	} else {
		bytes, err = RenderHTML(doc)
	}
	if err != nil {
		fail(c, errors.New("生成导出失败"))
		return
	}
	id := uuid.NewString()
	path := filepath.Join(s.cfg.Directory, "export-"+id+"."+p.Format)
	if err := os.WriteFile(path, bytes, 0600); err != nil {
		fail(c, errors.New("导出文件保存失败"))
		return
	}
	sum := sha256.Sum256(bytes)
	record := ExportRecord{ID: id, RunID: run.ID, Format: p.Format, Path: path, Hash: hex.EncodeToString(sum[:]), ExpiresAt: time.Now().UTC().Add(7 * 24 * time.Hour), CreatedAt: time.Now().UTC()}
	if s.db.Create(&record).Error != nil {
		_ = os.Remove(path)
		fail(c, errors.New("导出索引保存失败"))
		return
	}
	c.JSON(201, gin.H{"id": id, "url": "/api/agent/exports/" + id + "/download", "sha256": record.Hash, "expires_at": record.ExpiresAt})
}
func (h *Handler) download(c *gin.Context) {
	s := h.service
	var row ExportRecord
	if s.db.First(&row, "id = ?", c.Param("id")).Error != nil {
		fail(c, ErrForbidden)
		return
	}
	if _, err := s.ownedRun(c.Request.Context(), row.RunID, actor(c)); err != nil {
		fail(c, err)
		return
	}
	if row.ExpiresAt.Before(time.Now()) {
		c.JSON(410, gin.H{"error": "export_expired", "message": "文件缓存已过期，请从原结果重新生成"})
		return
	}
	if _, err := os.Stat(row.Path); err != nil {
		fail(c, errors.New("文件已清理，请重新生成"))
		return
	}
	c.Header("Cache-Control", "no-store")
	if row.Format == "html" {
		c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; frame-ancestors 'none'")
	}
	c.FileAttachment(row.Path, "CRM分析报告."+row.Format)
}

func resultPage(r Result, offset, limit int) Result {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset > len(r.Rows) {
		offset = len(r.Rows)
	}
	end := offset + limit
	if end > len(r.Rows) {
		end = len(r.Rows)
	}
	r.HasMore = end < len(r.Rows)
	r.Offset = offset
	r.Rows = r.Rows[offset:end]
	r.ReturnedRows = len(r.Rows)
	r.Truncated = r.Truncated || int64(end) < r.TotalRows
	if r.RecordCoverage != nil {
		r.RecordCoverage = searchRecordCoverage(&r)
	}
	return r
}
