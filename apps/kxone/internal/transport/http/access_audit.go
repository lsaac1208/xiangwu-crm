package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

type accessAuditWriter interface {
	Create(context.Context, *repo.AccessAudit) error
}
type auditResponse struct {
	gin.ResponseWriter
	body   bytes.Buffer
	status int
}

func (w *auditResponse) WriteHeader(code int) {
	if w.body.Len() == 0 {
		w.status = code
	}
}
func (w *auditResponse) WriteHeaderNow()                   {}
func (w *auditResponse) Write(b []byte) (int, error)       { return w.body.Write(b) }
func (w *auditResponse) WriteString(s string) (int, error) { return w.body.WriteString(s) }
func (w *auditResponse) Status() int                       { return w.status }
func (w *auditResponse) Size() int                         { return w.body.Len() }
func (w *auditResponse) Written() bool                     { return w.body.Len() > 0 }

func auditedRead(route string) bool {
	return route == "/api/recommend/sales" || route == "/api/leads" || strings.HasPrefix(route, "/api/leads/:") || route == "/api/workbench" || strings.HasPrefix(route, "/api/public-pool") || strings.HasPrefix(route, "/api/appointments") || strings.HasPrefix(route, "/api/stats/") || strings.HasPrefix(route, "/api/admin/leads") || route == "/api/admin/deleted-leads" || route == "/api/agent/conversations" || strings.HasPrefix(route, "/api/agent/conversations/:") || strings.HasPrefix(route, "/api/agent/runs/:") && !strings.HasSuffix(route, "/events") || strings.HasPrefix(route, "/api/agent/results/:") || strings.HasPrefix(route, "/api/agent/exports/:")
}

// Hold the response until its audit commits. No customer JSON is persisted in
// the audit; only typed identifiers extracted from the response are retained.
func AccessAuditing(store accessAuditWriter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if (c.Request.Method != http.MethodGet && !(c.Request.Method == http.MethodPost && c.FullPath() == "/api/recommend/sales")) || !auditedRead(c.FullPath()) {
			c.Next()
			return
		}
		original := c.Writer
		buffer := &auditResponse{ResponseWriter: original, status: 200}
		c.Writer = buffer
		defer func() { c.Writer = original }()
		c.Next()
		if c.GetInt64("account_id") == 0 {
			original.WriteHeader(buffer.status)
			original.Write(buffer.body.Bytes())
			return
		}
		objects := map[string]bool{}
		isAI := strings.HasPrefix(c.FullPath(), "/api/agent/")
		aggregate := false
		metadataOK := buffer.status >= 400
		if buffer.status < 400 {
			if isAI {
				metadataOK = true
				var payload any
				if json.Unmarshal(buffer.body.Bytes(), &payload) == nil {
					collectAuditIDs(payload, "", true, objects)
				}
				if id, err := uuid.Parse(c.Param("id")); err == nil {
					objects["resource:"+id.String()] = true
				}
			} else if raw, ok := c.Get("access_read_metadata"); ok {
				if meta, ok := raw.(accessReadMetadata); ok {
					metadataOK = true
					aggregate = meta.Aggregate
					for _, id := range meta.IDs {
						objects[id] = true
					}
				}
			}
		}

		ids := make([]string, 0, len(objects))
		for id := range objects {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		encoded, _ := json.Marshal(ids)
		outcome := "allowed"
		if buffer.status >= 400 {
			outcome = "denied"
		}
		requestID := c.GetString("request_id")
		if requestID == "" {
			requestID = uuid.NewString()
		}
		entry := &repo.AccessAudit{RequestID: requestID, AccountID: c.GetInt64("account_id"), Role: c.GetString("role"), Action: c.Request.Method + " " + c.FullPath(), Outcome: outcome, ObjectIDs: string(encoded), ObjectCount: len(ids), Aggregate: aggregate, CreatedAt: time.Now().UTC()}
		if !metadataOK || store == nil || store.Create(c.Request.Context(), entry) != nil {
			c.Writer = original
			for _, header := range []string{"Content-Length", "Content-Disposition", "Content-Type", "ETag", "Last-Modified", "Accept-Ranges", "Content-Range"} {
				c.Writer.Header().Del(header)
			}
			c.AbortWithStatusJSON(503, gin.H{"error": "access_audit_unavailable", "message": "访问记录暂时无法保存，请稍后重试"})
			return
		}
		original.WriteHeader(buffer.status)
		original.Write(buffer.body.Bytes())
	}
}
func collectAuditIDs(value any, parent string, ai bool, ids map[string]bool) {
	switch node := value.(type) {
	case []any:
		for _, v := range node {
			collectAuditIDs(v, parent, ai, ids)
		}
	case map[string]any:
		for key, v := range node {
			if ai && (key == "id" || key == "conversation_id" || key == "run_id" || key == "result_id") {
				if text, ok := v.(string); ok {
					if id, err := uuid.Parse(text); err == nil {
						ids["resource:"+id.String()] = true
					}
				}
			}
			collectAuditIDs(v, key, ai, ids)
		}
	}
}
func AccessAuditList(repository *repo.GormAccessAuditRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		f := repo.AccessAuditFilter{Limit: 50}
		valid := true
		for name, target := range map[string]*int64{"account_id": &f.AccountID, "lead_id": &f.LeadID} {
			if raw := c.Query(name); raw != "" {
				n, err := strconv.ParseInt(raw, 10, 64)
				if err != nil || n < 1 {
					valid = false
				}
				*target = n
			}
		}
		page := 1
		if raw := c.Query("page"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 1000000 {
				valid = false
			}
			page = n
		}
		if raw := c.Query("page_size"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 100 {
				valid = false
			}
			f.Limit = n
		}
		f.Offset = (page - 1) * f.Limit
		for name, target := range map[string]**time.Time{"start_date": &f.Start, "end_date": &f.End} {
			if raw := c.Query(name); raw != "" {
				d, err := time.ParseInLocation("2006-01-02", raw, types.BusinessLocation)
				if err != nil {
					valid = false
				}
				if name == "end_date" {
					d = d.AddDate(0, 0, 1)
				}
				*target = &d
			}
		}
		if !valid || f.Start != nil && f.End != nil && !f.Start.Before(*f.End) {
			c.JSON(422, gin.H{"error": "invalid_audit_filter"})
			return
		}
		if repository == nil {
			c.JSON(503, gin.H{"error": "access_audit_unavailable"})
			return
		}
		rows, total, err := repository.List(c.Request.Context(), f)
		if err != nil {
			internalError(c, 503, "access_audit_unavailable", err)
			return
		}
		c.JSON(200, gin.H{"items": rows, "total": total, "page": page, "page_size": f.Limit})
	}
}
