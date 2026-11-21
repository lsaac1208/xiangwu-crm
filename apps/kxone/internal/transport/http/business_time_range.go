package http

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const businessDateLayout = "2006-01-02"

var businessLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

type businessTimeRange struct {
	Start        *time.Time
	EndExclusive *time.Time
}

type timeRangeQueryError struct {
	Code string
	Err  error
}

func (e *timeRangeQueryError) Error() string {
	return e.Err.Error()
}

// parseBusinessTimeRange accepts preferred Beijing calendar dates or the
// legacy RFC3339 timestamps. Date ranges are inclusive by calendar date and
// represented internally as [start, endExclusive).
func parseBusinessTimeRange(c *gin.Context) (businessTimeRange, *timeRangeQueryError) {
	startDate := strings.TrimSpace(c.Query("start_date"))
	endDate := strings.TrimSpace(c.Query("end_date"))
	startTime := strings.TrimSpace(c.Query("start_time"))
	endTime := strings.TrimSpace(c.Query("end_time"))

	hasDateRange := startDate != "" || endDate != ""
	hasLegacyRange := startTime != "" || endTime != ""
	if hasDateRange && hasLegacyRange {
		return businessTimeRange{}, &timeRangeQueryError{
			Code: "conflicting_time_range",
			Err:  fmt.Errorf("date and timestamp range parameters cannot be mixed"),
		}
	}
	if hasDateRange {
		if startDate == "" || endDate == "" {
			return businessTimeRange{}, &timeRangeQueryError{Code: "invalid_date_range", Err: fmt.Errorf("start_date and end_date are both required")}
		}
		start, err := time.ParseInLocation(businessDateLayout, startDate, businessLocation)
		if err != nil || start.Format(businessDateLayout) != startDate {
			return businessTimeRange{}, &timeRangeQueryError{Code: "invalid_start_date", Err: fmt.Errorf("invalid start_date %q", startDate)}
		}
		end, err := time.ParseInLocation(businessDateLayout, endDate, businessLocation)
		if err != nil || end.Format(businessDateLayout) != endDate {
			return businessTimeRange{}, &timeRangeQueryError{Code: "invalid_end_date", Err: fmt.Errorf("invalid end_date %q", endDate)}
		}
		if end.Before(start) {
			return businessTimeRange{}, &timeRangeQueryError{Code: "invalid_date_range", Err: fmt.Errorf("end_date is before start_date")}
		}
		endExclusive := end.AddDate(0, 0, 1)
		return businessTimeRange{Start: &start, EndExclusive: &endExclusive}, nil
	}

	var result businessTimeRange
	if startTime != "" {
		parsed, err := time.Parse(time.RFC3339Nano, startTime)
		if err != nil {
			return businessTimeRange{}, &timeRangeQueryError{Code: "invalid_start_time", Err: err}
		}
		parsed = parsed.In(businessLocation)
		result.Start = &parsed
	}
	if endTime != "" {
		parsed, err := time.Parse(time.RFC3339Nano, endTime)
		if err != nil {
			return businessTimeRange{}, &timeRangeQueryError{Code: "invalid_end_time", Err: err}
		}
		// Preserve the legacy inclusive end_time contract while all repositories
		// use an exclusive upper boundary.
		parsed = parsed.In(businessLocation).Add(time.Nanosecond)
		result.EndExclusive = &parsed
	}
	if result.Start != nil && result.EndExclusive != nil && !result.EndExclusive.After(*result.Start) {
		return businessTimeRange{}, &timeRangeQueryError{Code: "invalid_time_range", Err: fmt.Errorf("end_time is before start_time")}
	}
	return result, nil
}
