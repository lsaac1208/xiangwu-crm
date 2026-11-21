package agent

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func exportFixture() ExportDocument {
	stamp := time.Date(2026, 10, 8, 2, 30, 0, 0, time.UTC)
	r := Result{ID: "r-saved", Columns: []string{"来源", "线索数", "净成交额"}, Rows: [][]any{{"=HYPERLINK(\"https://example.invalid\",\"x\")", int64(7), 12500.25}, {"+cmd|'/C calc'!A0", int64(2), -30.0}}, TotalRows: 2, ReturnedRows: 2, SnapshotAt: stamp, Query: "SELECT source, count(*), sum(deal-refund) FROM leads GROUP BY source", Coverage: "全量聚合；金额不是实际收款", Sources: []Source{{Type: "lead", ID: "17", Label: "客户[17]", OccurredAt: stamp.Format(time.RFC3339)}}}
	return ExportDocument{Title: "来源分析", Question: "这周哪些来源成交好？", Blocks: []Block{{Type: "markdown", Text: "这是回答时的分析结果。"}, {Type: "table", Title: "渠道表现", ResultID: r.ID}}, Results: []Result{r}, SnapshotAt: stamp, Model: "qwen3.8-max-test", RuleVersion: 3, RunID: "run-saved"}
}

func TestExportXLSXStoredRowsMetadataAndFormulaSafety(t *testing.T) {
	doc := exportFixture()
	body, err := RenderXLSX(doc)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := f.GetRows("结果01")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[1][0] != doc.Results[0].Rows[0][0] || rows[1][1] != "7" || rows[1][2] != "12500.25" {
		t.Fatalf("saved rows not preserved: %#v", rows)
	}
	for _, cell := range []string{"A2", "A3"} {
		formula, err := f.GetCellFormula("结果01", cell)
		if err != nil || formula != "" {
			t.Fatalf("formula executed at %s: %q %v", cell, formula, err)
		}
	}
	metadata, err := f.GetRows("口径说明")
	if err != nil {
		t.Fatal(err)
	}
	all := []string{}
	for _, row := range metadata {
		all = append(all, strings.Join(row, " "))
	}
	text := strings.Join(all, "\n")
	for _, expected := range []string{"crm-agent-export-v2", "run-saved", "qwen3.8-max-test", "公共规则版本 3", "2026-10-08T10:30:00+08:00", "全量聚合；金额不是实际收款", doc.Results[0].Query, "lead:17 客户[17]"} {
		if !strings.Contains(text, expected) {
			t.Errorf("metadata missing %q: %s", expected, text)
		}
	}
	// Editing the in-memory object later cannot alter the returned workbook.
	doc.Results[0].Rows[0][1] = int64(999)
	got, _ := f.GetCellValue("结果01", "B2")
	if got != "7" {
		t.Fatalf("historical result changed: %s", got)
	}
}

func TestExportXLSXIncludesEveryAdoptedResultAndPreservesLongIdentifiers(t *testing.T) {
	doc := exportFixture()
	doc.Results = append(doc.Results, Result{ID: "other", Columns: []string{"文本", "编号"}, Rows: [][]any{{"\t=SUM(1,2)", int64(1234567890123456789)}}, TotalRows: 1, ReturnedRows: 1})
	doc.Blocks = append(doc.Blocks, Block{Type: "sources", ResultIDs: []string{"r-saved", "other"}})
	body, err := RenderXLSX(doc)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, _ := f.GetCellValue("结果02", "B2")
	if got != "1234567890123456789" {
		t.Fatalf("identifier precision lost: %q", got)
	}
	formula, _ := f.GetCellFormula("结果02", "A2")
	if formula != "" {
		t.Fatalf("tab formula executed: %s", formula)
	}
	text, _ := f.GetCellValue("结果02", "A2")
	if text != "\t=SUM(1,2)" {
		t.Fatalf("text rewritten: %q", text)
	}
}

func TestExportHTMLHostileModelAndResultTextAreEscaped(t *testing.T) {
	doc := exportFixture()
	doc.Title = `<script>alert("title")</script>`
	doc.Question = `<img src=x onerror=alert(1)>`
	doc.Blocks = append(doc.Blocks, Block{Type: "markdown", Text: `<iframe src="https://evil.invalid"></iframe>`}, Block{Type: "chart", Title: `<script>chart</script>`, ResultID: "r-saved", ChartType: "bar", XColumn: "来源", YColumns: []string{"线索数"}})
	doc.Results[0].Rows[0][0] = `<script>row</script>`
	body, err := RenderHTML(doc)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, unsafe := range []string{"<script", "<iframe", "<img src=x", "https://evil.invalid\"></iframe>"} {
		if strings.Contains(s, unsafe) {
			t.Fatalf("untrusted HTML rendered: %s", unsafe)
		}
	}
	for _, safe := range []string{"&lt;script&gt;", "&lt;iframe", "Content-Security-Policy", "<svg", "12500.25", "2026-10-08T10:30:00&#43;08:00", "run-saved"} {
		if !strings.Contains(s, safe) {
			t.Errorf("missing escaped content/metadata %q", safe)
		}
	}
	if strings.Contains(s, "<script src=") {
		t.Fatal("report uses executable JavaScript")
	}
}

func TestExportHTMLChartsAreBoundToSavedNumericResults(t *testing.T) {
	for _, kind := range []string{"bar", "line", "pie", "scatter"} {
		t.Run(kind, func(t *testing.T) {
			doc := exportFixture()
			r := Result{ID: "chart-result", Columns: []string{"x", "y"}, Rows: [][]any{{int64(1), float64(8)}, {int64(2), float64(3)}, {int64(3), float64(6)}}, TotalRows: 3, ReturnedRows: 3}
			doc.Results = []Result{r}
			doc.Blocks = []Block{{Type: "chart", ResultID: r.ID, ChartType: kind, XColumn: "x", YColumns: []string{"y"}}}
			body, err := RenderHTML(doc)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(body, []byte("<svg")) {
				t.Fatal("missing saved-result chart")
			}
		})
	}
	doc := exportFixture()
	doc.Blocks = append(doc.Blocks, Block{Type: "chart", ResultID: "invented", ChartType: "bar", XColumn: "来源", YColumns: []string{"线索数"}})
	if _, err := RenderHTML(doc); err == nil {
		t.Fatal("fabricated result accepted")
	}
	if _, err := RenderXLSX(doc); err == nil {
		t.Fatal("fabricated Excel result accepted")
	}
	doc = exportFixture()
	doc.Blocks = append(doc.Blocks, Block{Type: "chart", ResultID: "r-saved", ChartType: "script", XColumn: "来源", YColumns: []string{"线索数"}})
	if _, err := RenderHTML(doc); err == nil {
		t.Fatal("arbitrary chart type accepted")
	}
	bad := Result{ID: "invalid-chart", Columns: []string{"x", "y"}, Rows: [][]any{{float64(1), math.Inf(1)}}, TotalRows: 1}
	if err := ValidateChart(Block{Type: "chart", ChartType: "line", XColumn: "x", YColumns: []string{"y"}}, bad); err == nil {
		t.Fatal("non-finite chart accepted")
	}
	bad.Rows = [][]any{{float64(1), float64(-1)}}
	if err := ValidateChart(Block{Type: "chart", ChartType: "pie", XColumn: "x", YColumns: []string{"y"}}, bad); err == nil {
		t.Fatal("negative pie chart accepted")
	}
}

func TestExportCoverageAndInvalidResultDoNotSilentlyDisappear(t *testing.T) {
	doc := exportFixture()
	doc.Results[0].TotalRows = 99
	doc.Results[0].Truncated = true
	doc.Results[0].Coverage = "仅展示前两行，共 99 行；不代表全量明细"
	body, err := RenderHTML(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("已截断")) || !bytes.Contains(body, []byte("匹配 99 行")) {
		t.Fatal("truncated result presented as complete")
	}
	doc.Results[0].Rows[0] = []any{"missing-column"}
	if _, err := RenderXLSX(doc); err == nil {
		t.Fatal("column mismatch silently exported")
	}
	if _, err := RenderHTML(doc); err == nil {
		t.Fatal("column mismatch silently reported")
	}
	doc = exportFixture()
	doc.Blocks[0].Text = strings.Repeat("中", 32768)
	if _, err := RenderXLSX(doc); err == nil {
		t.Fatal("Excel text was silently truncated")
	}
}

func TestExportChartsKeepOriginalPreviewWhileTablesAndXLSXUseMaterializedRows(t *testing.T) {
	doc := exportFixture()
	full := Result{ID: "many-rows", Columns: []string{"x", "count"}, Rows: [][]any{}, TotalRows: 600, ReturnedRows: 600, SnapshotAt: doc.SnapshotAt, MaterializedRows: 600, MaterializedComplete: true}
	for i := 0; i < 600; i++ {
		full.Rows = append(full.Rows, []any{int64(i), int64(i + 1)})
	}
	preview := full
	preview.Rows = full.Rows[:2]
	preview.ReturnedRows = 2
	preview.Truncated = true
	doc.Results = []Result{full}
	doc.Blocks = []Block{{Type: "chart", Title: "预览", ResultID: full.ID, ChartType: "bar", XColumn: "x", YColumns: []string{"count"}, Result: &preview}}
	body, err := RenderHTML(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("第1至2行")) || !bytes.Contains(body, []byte("本报告展示 600 行")) {
		t.Fatal("chart preview/full result coverage missing")
	}
	xlsx, err := RenderXLSX(doc)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(xlsx))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := f.GetRows("结果01")
	if err != nil || len(rows) != 601 {
		t.Fatalf("XLSX exported only preview: %d %v", len(rows), err)
	}
	preview.SnapshotAt = preview.SnapshotAt.Add(time.Second)
	if _, err := RenderHTML(doc); err == nil {
		t.Fatal("chart preview from a different snapshot accepted")
	}
}

func TestChartRangeReadsSavedValuesAndPreservesOffset(t *testing.T) {
	stamp := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	saved := Result{ID: "fixed-chart", SnapshotAt: stamp, Columns: []string{"label", "count"}, Rows: [][]any{{"A", 1}, {"B", 2}, {"C", 3}}, TotalRows: 3}
	preview := saved
	preview.Offset = 1
	preview.Rows = [][]any{{"invented", 999}}
	block := Block{Type: "chart", ResultID: saved.ID, Result: &preview}
	fixed, err := savedChartResult(block, saved)
	if err != nil || fixed.Offset != 1 || len(fixed.Rows) != 1 || fixed.Rows[0][0] != "B" || fixed.Rows[0][1] != 2 {
		t.Fatalf("chart used embedded values or lost the fixed slice: %+v %v", fixed, err)
	}
	preview.Offset = 3
	if _, err := savedChartResult(block, saved); err == nil {
		t.Fatal("out-of-range chart was silently changed")
	}
	preview.Offset = -1
	if _, err := savedChartResult(block, saved); err == nil {
		t.Fatal("negative chart range accepted")
	}
}
