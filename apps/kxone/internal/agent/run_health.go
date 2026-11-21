package agent

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// RunHealthSummary is an aggregate of retained analysis tasks, not model calls.
// It contains no account, conversation, prompt, error or result identifiers.
type RunHealthSummary struct {
	Month                string   `json:"month"`
	Scope                string   `json:"scope"`
	Total                int64    `json:"total"`
	Completed            int64    `json:"completed"`
	Failed               int64    `json:"failed"`
	Interrupted          int64    `json:"interrupted"`
	Cancelled            int64    `json:"cancelled"`
	BudgetExceeded       int64    `json:"budget_exceeded"`
	Queued               int64    `json:"queued"`
	Running              int64    `json:"running"`
	FailureRate          *float64 `json:"failure_rate"`
	FailureSampleCount   int64    `json:"failure_sample_count"`
	AverageDurationMS    *float64 `json:"average_duration_ms"`
	DurationSampleCount  int64    `json:"duration_sample_count"`
	UnknownDurationCount int64    `json:"unknown_duration_count"`
}

func runHealthSummary(ctx context.Context, db *gorm.DB, now time.Time) (RunHealthSummary, error) {
	local := now.In(budgetShanghai)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, budgetShanghai)
	end := start.AddDate(0, 1, 0)
	var out RunHealthSummary
	// SQLite normalizes offsets before comparing calendar boundaries. Duration
	// is only meaningful for successful tasks with a trustworthy end timestamp;
	// restart-interrupted tasks can include downtime and are deliberately omitted.
	err := db.WithContext(ctx).Raw(`SELECT COUNT(*) AS total,
		COALESCE(SUM(CASE WHEN status='completed' THEN 1 ELSE 0 END),0) AS completed,
		COALESCE(SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END),0) AS failed,
		COALESCE(SUM(CASE WHEN status='interrupted' THEN 1 ELSE 0 END),0) AS interrupted,
		COALESCE(SUM(CASE WHEN status='cancelled' THEN 1 ELSE 0 END),0) AS cancelled,
		COALESCE(SUM(CASE WHEN status='budget_exceeded' THEN 1 ELSE 0 END),0) AS budget_exceeded,
		COALESCE(SUM(CASE WHEN status='queued' THEN 1 ELSE 0 END),0) AS queued,
		COALESCE(SUM(CASE WHEN status='running' THEN 1 ELSE 0 END),0) AS running,
		COALESCE(SUM(CASE WHEN status='completed' AND julianday(finished_at)>=julianday(created_at) THEN 1 ELSE 0 END),0) AS duration_sample_count,
		AVG(CASE WHEN status='completed' AND julianday(finished_at)>=julianday(created_at)
			THEN ROUND((julianday(finished_at)-julianday(created_at))*86400000) END) AS average_duration_ms
		FROM ai_runs WHERE julianday(created_at)>=julianday(?) AND julianday(created_at)<julianday(?)`,
		start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano)).Scan(&out).Error
	if err != nil {
		return out, err
	}
	out.Month = start.Format("2006-01")
	out.Scope = "本月保留会话中的分析任务，删除会话后不再计入；按上海时区的任务创建月份统计"
	out.FailureSampleCount = out.Completed + out.Failed + out.Interrupted
	if out.FailureSampleCount > 0 {
		rate := float64(out.Failed+out.Interrupted) / float64(out.FailureSampleCount)
		out.FailureRate = &rate
	}
	out.UnknownDurationCount = out.Completed - out.DurationSampleCount
	return out, nil
}
