package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// v27 run ad6a46ff: after a capacity checkpoint the owner table was dropped
// and the model re-read it. Every observed result must keep whole rows.
func TestV27CheckpointKeepsWholeRowsForEveryResult(t *testing.T) {
	at := time.Now().UTC()
	followups := &Result{ID: "a-followups", Columns: []string{"record_id", "actor", "note"}, SnapshotAt: at, TotalRows: 60}
	for i := 0; i < 60; i++ {
		followups.Rows = append(followups.Rows, []any{i, "sales-a", strings.Repeat("顾虑文本", 12)})
	}
	owners := &Result{ID: "z-owners", Columns: []string{"owner", "lead_count", "net_amount"}, Rows: [][]any{{"sales-a", 1, 0.0}, {"sales-b", 1, 8000.0}}, TotalRows: 2, SnapshotAt: at}
	reads := newEvidenceReadState()
	for _, r := range []*Result{followups, owners} {
		reads.observe(map[string]any{"id": r.ID, "columns": r.Columns, "rows": r.Rows, "offset": 0})
	}
	page := reads.checkpoint(map[string]*Result{followups.ID: followups, owners.ID: owners}, 6<<10, []string{followups.ID})
	raw, _ := json.Marshal(page)
	if len(raw) > 6<<10 {
		t.Fatalf("budget exceeded: %d", len(raw))
	}
	seen := map[string]bool{}
	for _, r := range page["results"].([]*checkpointResult) {
		seen[r.ID] = true
		for _, row := range r.Rows {
			if len(row.Cells) != 3 {
				t.Fatalf("partial row carried for %s row %d: %v", r.ID, row.Row, row.Cells)
			}
		}
		if len(r.Rows) == 0 {
			t.Fatalf("result %s carried no complete row", r.ID)
		}
	}
	if !seen[owners.ID] || !strings.Contains(string(raw), "sales-b") || !strings.Contains(string(raw), "8000") {
		t.Fatalf("owner table starved: %s", raw)
	}
	if page["omitted_cells"].(int) == 0 {
		t.Fatal("omitted detail rows must remain disclosed")
	}
}

// v27 second run used calls 1-2 on describe_data for column names only.
func TestCompactSchemaIsBoundedAndDropsEnumsFirst(t *testing.T) {
	full := SnapshotDescription{Tables: map[string][]string{"leads": {"id", "status", "source_channel"}, "lead_followups": {"id", "lead_id", "next_followup_at"}}, Enums: map[string]map[string]string{"leads.status": {"new": "新线索", "won": "成交"}}}
	cols, enums := compactSchema(full, 4096)
	if len(cols) != 2 || len(enums["leads.status"]) != 2 || enums["leads.status"][0] != "new" {
		t.Fatalf("schema incomplete: %v %v", cols, enums)
	}
	colsOnly, _ := json.Marshal(cols)
	if c, e := compactSchema(full, len(colsOnly)+5); c == nil || e != nil {
		t.Fatal("enums must be dropped before columns")
	}
	if c, _ := compactSchema(full, 10); c != nil {
		t.Fatal("oversized schema must be omitted, not truncated")
	}
}

func TestDefaultLimitsRaiseCallsButKeepCostAndTime(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxCalls != 12 || cfg.MaxTools != 12 || cfg.RunBudgetMicro != 3_000_000 || cfg.MonthlyBudgetMicro != 300_000_000 || cfg.Timeout != 180*time.Second {
		t.Fatalf("unexpected limits: %+v", cfg)
	}
}

// DeepSeek as primary (AGENT_MODEL=deepseek-*) uses the official non-thinking
// tool mode from the first request, as the v27 fallback did.
func TestDeepSeekPrimaryUsesNonThinkingMode(t *testing.T) {
	req := ModelRequest{Model: "deepseek-v4-pro", ReasoningEffort: "high"}
	applyProviderPolicy(&req)
	if req.Thinking == nil || req.Thinking.Type != "disabled" || req.ReasoningEffort != "" {
		t.Fatalf("deepseek primary policy: %+v", req)
	}
}
