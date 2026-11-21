package http

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/adapter/repo"
)

type failingAuditStore struct{}

func (failingAuditStore) Create(context.Context, *repo.AccessAudit) error {
	return errors.New("storage offline")
}
func TestAuditFailureDoesNotDiscloseCustomerResponseOrDownloadHeaders(t *testing.T) {
	r := gin.New()
	r.Use(AccessAuditing(failingAuditStore{}))
	r.GET("/api/leads", func(c *gin.Context) {
		c.Set("account_id", int64(1))
		RecordLeadReadIDs(c, 1)
		c.Header("Content-Disposition", "attachment; filename=customer.xlsx")
		c.Header("Content-Type", "application/pdf")
		c.JSON(200, gin.H{"leads": []gin.H{{"id": 1, "customer_mobile": "13912345678"}}})
	})
	result := httptest.NewRecorder()
	r.ServeHTTP(result, httptest.NewRequest("GET", "/api/leads", nil))
	if result.Code != 503 || strings.Contains(result.Body.String(), "13912345678") || result.Header().Get("Content-Disposition") != "" || !strings.HasPrefix(result.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("unsafe audit failure %d %v %s", result.Code, result.Header(), result.Body.String())
	}
}

func TestPostReadAuditFailurePreventsCustomerDisclosure(t *testing.T) {
	r := gin.New()
	r.Use(AccessAuditing(failingAuditStore{}))
	r.POST("/api/recommend/sales", func(c *gin.Context) {
		c.Set("account_id", int64(1))
		RecordLeadReadIDs(c, 7)
		c.JSON(200, gin.H{"lead": gin.H{"id": 7, "customer_mobile": "13912345678"}})
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/recommend/sales", nil))
	if w.Code != 503 || strings.Contains(w.Body.String(), "13912345678") {
		t.Fatalf("POST read disclosed: %d %s", w.Code, w.Body.String())
	}
}
