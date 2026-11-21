package http

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func timeRangeContext(rawQuery string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/?"+rawQuery, nil)
	return c
}

func TestParseBusinessTimeRangeUsesShanghaiCalendarDays(t *testing.T) {
	rng, err := parseBusinessTimeRange(timeRangeContext("start_date=2026-07-31&end_date=2026-08-01"))
	if err != nil {
		t.Fatalf("parse range: %v", err)
	}
	if got := rng.Start.Format(time.RFC3339); got != "2026-07-31T00:00:00+08:00" {
		t.Fatalf("start = %s", got)
	}
	if got := rng.EndExclusive.Format(time.RFC3339); got != "2026-08-02T00:00:00+08:00" {
		t.Fatalf("end exclusive = %s", got)
	}
}

func TestParseBusinessTimeRangeRejectsInvalidOrMixedParameters(t *testing.T) {
	tests := []struct {
		query string
		code  string
	}{
		{"start_date=2026-07-31", "invalid_date_range"},
		{"start_date=2026-08-01&end_date=2026-07-31", "invalid_date_range"},
		{"start_date=2026-07-31&end_date=2026-07-31&start_time=2026-07-31T00:00:00%2B08:00", "conflicting_time_range"},
		{"start_time=not-a-time", "invalid_start_time"},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			_, err := parseBusinessTimeRange(timeRangeContext(test.query))
			if err == nil || err.Code != test.code {
				t.Fatalf("error = %#v, want code %s", err, test.code)
			}
		})
	}
}

func TestParseBusinessTimeRangePreservesLegacyInclusiveEnd(t *testing.T) {
	rng, err := parseBusinessTimeRange(timeRangeContext("start_time=2026-07-30T16:00:00Z&end_time=2026-07-31T15:59:59.999999999Z"))
	if err != nil {
		t.Fatalf("parse legacy range: %v", err)
	}
	if got := rng.Start.Format(time.RFC3339); got != "2026-07-31T00:00:00+08:00" {
		t.Fatalf("legacy start = %s", got)
	}
	if got := rng.EndExclusive.Format(time.RFC3339); got != "2026-08-01T00:00:00+08:00" {
		t.Fatalf("legacy exclusive end = %s", got)
	}
}
