package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// Optional local replay reads only the existing sanitized outbound audit. Raw
// provider replies/reasoning, API configuration and credentials are not inputs.
func TestCheckpointReplaySanitizedV27Outbound(t *testing.T) {
	path := os.Getenv("CRM_AGENT_CONTEXT_AUDIT")
	if path == "" {
		t.Skip("set CRM_AGENT_CONTEXT_AUDIT to the preserved sanitized v27 outbound audit")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	type auditLine struct {
		Bytes   int            `json:"bytes"`
		Request map[string]any `json:"synthetic_request"`
	}
	var lines []auditLine
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		var line auditLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 13 {
		t.Fatalf("wrong preserved campaign: %d requests", len(lines))
	}
	request := lines[9].Request
	messages := request["messages"].([]any)
	backgroundMessage := messages[1].(map[string]any)
	content := backgroundMessage["content"].(string)
	start := strings.Index(content, "{")
	var background map[string]any
	if err := json.Unmarshal([]byte(content[start:]), &background); err != nil {
		t.Fatal(err)
	}
	old := background["observed_evidence"].(map[string]any)
	if old["omitted_cells"].(float64) != 36 {
		t.Fatal("wrong checkpoint counterexample")
	}
	results := map[string]*Result{}
	for _, entry := range background["result_catalog"].([]any) {
		id := entry.(map[string]any)["id"].(string)
		results[id] = nil
	}
	reads := newEvidenceReadState()
	expectedCells, expectedFacts := 0, 0
	for _, rawMessage := range lines[8].Request["messages"].([]any) {
		message := rawMessage.(map[string]any)
		if message["role"] != "tool" {
			continue
		}
		var result Result
		if json.Unmarshal([]byte(message["content"].(string)), &result) != nil || result.ID == "" {
			continue
		}
		if _, wanted := results[result.ID]; !wanted {
			continue
		}
		if int64(len(result.Rows)) != result.TotalRows {
			t.Fatal("replay must not invent undelivered rows")
		}
		results[result.ID] = &result
		page, err := modelResultPage(&result, 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		reads.observe(page)
		expectedCells += len(result.Columns) * len(result.Rows)
		expectedFacts += len(result.Facts)
	}
	for id, result := range results {
		if result == nil {
			t.Fatalf("missing actual tool evidence: %s", id)
		}
	}
	checkpoint := reads.checkpoint(results, 12<<10)
	if checkpoint["omitted_cells"].(int) != 0 || checkpoint["omitted_facts"].(int) != 0 {
		t.Fatalf("small real campaign evidence still evicted: %+v", checkpoint)
	}
	carriedCells, carriedFacts := 0, 0
	for _, result := range checkpoint["results"].([]*checkpointResult) {
		for _, row := range result.Rows {
			carriedCells += len(row.Cells)
		}
		carriedFacts += len(result.Facts)
	}
	if carriedCells != expectedCells || carriedFacts != expectedFacts || carriedCells != 103 || carriedFacts != 12 {
		t.Fatalf("evidence changed: cells=%d/%d facts=%d/%d", carriedCells, expectedCells, carriedFacts, expectedFacts)
	}
	background["observed_evidence"] = checkpoint
	encodedBackground, _ := json.Marshal(background)
	backgroundMessage["content"] = content[:start] + string(encodedBackground)
	encodedRequest, _ := json.Marshal(request)
	if len(encodedRequest) >= 44<<10 {
		t.Fatalf("checkpoint metadata remained too large: %d", len(encodedRequest))
	}
	encodedCheckpoint, _ := json.Marshal(checkpoint)
	t.Logf("same archived request, only evidence replaced: before=%d after=%d checkpoint=%d all_cells=%d all_facts=%d", lines[9].Bytes, len(encodedRequest), len(encodedCheckpoint), carriedCells, carriedFacts)
}

func TestCheckpointFairlyRetainsSmallResultBesideLongDetails(t *testing.T) {
	at := time.Now().UTC()
	large := &Result{ID: "a-large", Columns: []string{"note"}, SnapshotAt: at, TotalRows: 100}
	for i := 0; i < 100; i++ {
		large.Rows = append(large.Rows, []any{strings.Repeat("长文本", 30)})
	}
	small := &Result{ID: "z-summary", Columns: []string{"owner", "count"}, Rows: [][]any{{"summary-owner-a", 2}, {"summary-owner-b", 1}}, TotalRows: 2, SnapshotAt: at}
	reads := newEvidenceReadState()
	for _, result := range []*Result{large, small} {
		reads.observe(map[string]any{"id": result.ID, "columns": result.Columns, "rows": result.Rows, "offset": 0})
	}
	page := reads.checkpoint(map[string]*Result{large.ID: large, small.ID: small}, 4096)
	raw, _ := json.Marshal(page)
	if len(raw) > 4096 || !strings.Contains(string(raw), "summary-owner-a") || !strings.Contains(string(raw), "summary-owner-b") {
		t.Fatalf("one result starved the other result: bytes=%d %s", len(raw), raw)
	}
	if page["omitted_cells"].(int) == 0 {
		t.Fatal("omitted detail cells must remain disclosed")
	}
}
