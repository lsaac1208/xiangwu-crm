package agent

import (
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type revokingEventWriter struct {
	*httptest.ResponseRecorder
	valid *atomic.Bool
}

func (w *revokingEventWriter) Flush() { w.ResponseRecorder.Flush(); w.valid.Store(false) }
func historyTestRouter(f *agentSecurityFixture, audit func(*gin.Context, string, string) error, valid func(*gin.Context) bool) *gin.Engine {
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("account_id", securityAdmin.AccountID)
		c.Set("auth_version", securityAdmin.AuthVersion)
		c.Set("role", securityAdmin.Role)
	})
	NewHandler(f.service).WithHistoryAccess(audit, valid).Register(r.Group("/api/agent"))
	return r
}
func TestHistoryStreamStopsAfterSessionRevocation(t *testing.T) {
	f := newAgentSecurityFixture(t, nil)
	run := seedFinalizationRun(t, f)
	if err := f.service.event(run.ID, "message", map[string]any{"text": "fixture-only"}); err != nil {
		t.Fatal(err)
	}
	var active atomic.Bool
	active.Store(true)
	var audits atomic.Int64
	r := historyTestRouter(f, func(_ *gin.Context, id, outcome string) error {
		if id != run.ID || outcome != "allowed" {
			t.Error("incorrect resource audit")
		}
		audits.Add(1)
		return nil
	}, func(*gin.Context) bool { return active.Load() })
	w := &revokingEventWriter{httptest.NewRecorder(), &active}
	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/agent/runs/"+run.ID+"/events", nil))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("revoked stream kept sending")
	}
	if w.Code != 200 || audits.Load() != 1 || !strings.Contains(w.Body.String(), "fixture-only") {
		t.Fatalf("stream audit/output status=%d count=%d", w.Code, audits.Load())
	}
}
func TestHistoryStreamAuditFailurePreventsFirstFrame(t *testing.T) {
	f := newAgentSecurityFixture(t, nil)
	run := seedFinalizationRun(t, f)
	f.service.event(run.ID, "message", map[string]any{"text": "must-not-disclose"})
	r := historyTestRouter(f, func(*gin.Context, string, string) error { return errors.New("offline") }, func(*gin.Context) bool { return true })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/agent/runs/"+run.ID+"/events", nil))
	if w.Code != 503 || strings.Contains(w.Body.String(), "must-not-disclose") || strings.Contains(w.Body.String(), "event:") {
		t.Fatalf("unaudited stream disclosed: %d %s", w.Code, w.Body.String())
	}
}
