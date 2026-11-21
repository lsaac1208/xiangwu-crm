package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPrometheusTextUsesValidMetricFamilies(t *testing.T) {
	m := &Metrics{}
	m.RecordCardSend(true, 75*time.Millisecond)
	m.RecordLeadClaim(false)
	m.SetStaleCardUpdating(3)

	text := m.prometheusText()
	for _, want := range []string{
		"# HELP kxone_card_send_total Total card send attempts.",
		"# TYPE kxone_card_send_total counter",
		"kxone_card_send_total 1",
		"# TYPE kxone_card_send_duration_seconds histogram",
		"kxone_card_send_duration_seconds_bucket{le=\"0.1\"} 1",
		"kxone_card_send_duration_seconds_count 1",
		"# TYPE kxone_lead_claim_failed_total counter",
		"kxone_lead_claim_failed_total 1",
		"# TYPE kxone_card_updating_stale gauge",
		"kxone_card_updating_stale 3",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("prometheus text missing %q:\n%s", want, text)
		}
	}
}

func TestPrometheusHTTPHandlerContentType(t *testing.T) {
	m := &Metrics{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)

	m.HTTPHandler().ServeHTTP(rec, req)

	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") || !strings.Contains(contentType, "version=0.0.4") {
		t.Fatalf("content-type = %q", contentType)
	}
}
