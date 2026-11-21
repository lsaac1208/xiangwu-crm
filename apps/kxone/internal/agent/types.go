package agent

import "time"

// Config contains server-only credentials. Credentials never enter messages or logs.
type Config struct {
	Fallback *Config `json:"-"`
	Enabled  bool
	// FixedMetrics selects the phase-two mode: the model only chooses fixed
	// server metrics and writes number-free interpretation. Free SQL analysis
	// remains available only when this is false.
	FixedMetrics bool
	// MetricSelectPreferFallback sends the select stage to the configured
	// fallback provider first (KXONE_AGENT_METRIC_SELECT_MODEL=fallback).
	MetricSelectPreferFallback bool
	BaseURL                    string
	APIKey                     string `json:"-"`
	Model                      string
	Directory                  string
	MonthlyBudgetMicro         int64
	RunBudgetMicro             int64
	InputPriceMicro            int64
	OutputPriceMicro           int64
	PriceVersion               string
	MaxCalls                   int
	MaxTools                   int
	MaxOutputTokens            int
	Timeout                    time.Duration
	// RequestAudit is an optional local acceptance guard. It sees a copy of the
	// sanitized JSON body before transmission, never the Authorization header.
	RequestAudit func([]byte) error `json:"-"`
	// ProviderErrorAudit is only wired by explicit isolated acceptance tests.
	// It receives at most the first 8 KiB of a non-200 response or an HTTP 200
	// stream that failed parsing/completion, and no headers. Only an enabled
	// callback buffers stream bytes. Normal server configuration cannot enable
	// it; the callback must redact before writing private evidence. Its failures
	// never change the public error or whether model usage is known.
	ProviderErrorAudit func(status int, body []byte) `json:"-"`
	// AnswerValidationAudit is opt-in isolated acceptance diagnostics. It sees
	// only privacy-approved, whitelisted final proposals and validation errors,
	// never reasoning. Production configuration leaves it nil.
	AnswerValidationAudit func(AnswerValidationAuditRecord) `json:"-"`
}

func DefaultConfig() Config {
	return Config{Model: "qwen3.8-max", Directory: "data/agent", MonthlyBudgetMicro: 300_000_000, RunBudgetMicro: 3_000_000, InputPriceMicro: 12_000_000, OutputPriceMicro: 36_000_000, PriceVersion: "2026-10-08", MaxCalls: 12, MaxTools: 12, MaxOutputTokens: 4096, Timeout: 180 * time.Second}
}

type Actor struct {
	AccountID   int64
	AuthVersion int64
	Role        string
}

type Source struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	LeadID     int64  `json:"lead_id,omitempty"`
	Label      string `json:"label"`
	OccurredAt string `json:"occurred_at,omitempty"`
}

// QueryParams is nil when a historical result did not save bindings. A new
// query with no bindings stores an empty slice; all saved values are sanitized.
type Result struct {
	ID                   string          `json:"id"`
	Columns              []string        `json:"columns"`
	Rows                 [][]any         `json:"rows"`
	TotalRows            int64           `json:"total_rows"`
	ReturnedRows         int             `json:"returned_rows"`
	Truncated            bool            `json:"truncated"`
	SnapshotAt           time.Time       `json:"snapshot_at"`
	Query                string          `json:"query,omitempty"`
	QueryParams          []any           `json:"query_params"`
	Sources              []Source        `json:"sources,omitempty"`
	Coverage             string          `json:"coverage,omitempty"`
	MaterializedRows     int             `json:"materialized_rows"`
	MaterializedComplete bool            `json:"materialized_complete"`
	Offset               int             `json:"offset"`
	HasMore              bool            `json:"has_more"`
	RecordCoverage       *RecordCoverage `json:"record_coverage"`
	Facts                []MeasuredFact  `json:"facts,omitempty"`
	MeasureScope         *MeasureScope   `json:"measure_scope,omitempty"`
}

type SearchRequest struct {
	Keyword string `json:"keyword"`
	LeadID  int64  `json:"lead_id"`
	ActorID string `json:"actor_id"`
	From    string `json:"from"`
	To      string `json:"to"`
	Stage   string `json:"stage"`
	Offset  int    `json:"offset"`
	Limit   int    `json:"limit"`
}

type Block struct {
	Type      string   `json:"type"`
	Text      string   `json:"text,omitempty"`
	Title     string   `json:"title,omitempty"`
	ResultID  string   `json:"result_id,omitempty"`
	ChartType string   `json:"chart_type,omitempty"`
	XColumn   string   `json:"x_column,omitempty"`
	YColumns  []string `json:"y_columns,omitempty"`
	Sources   []Source `json:"sources,omitempty"`
	Result    *Result  `json:"result,omitempty"`
	// Only a sources block uses ResultIDs as the final result manifest. Nil
	// means an older answer did not declare it; [] explicitly selects none.
	ResultIDs       []string            `json:"result_ids"`
	Verification    *AnswerVerification `json:"verification,omitempty"`
	AnalysisContext *AnalysisContext    `json:"analysis_context,omitempty"`
}

type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type ExportDocument struct {
	Title           string
	Question        string
	Blocks          []Block
	Results         []Result
	SnapshotAt      time.Time
	Model           string
	RuleVersion     int64
	RunID           string
	ResultSelection *AnswerResultSelection
	RunStatus       string
}
