package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

const exportFormatVersion = "crm-agent-export-v2"

// RenderXLSX consumes persisted results only. It has no database handle and
// cannot accidentally recompute a historical answer against today's data.
func RenderXLSX(doc ExportDocument) ([]byte, error) {
	doc, err := prepareExportDocument(doc)
	if err != nil {
		return nil, err
	}
	results, err := exportResults(doc)
	if err != nil {
		return nil, err
	}
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetSheetName("Sheet1", "口径说明"); err != nil {
		return nil, err
	}
	metadata := [][]string{
		{"字段", "值"}, {"导出格式", exportFormatVersion}, {"标题", doc.Title}, {"问题", doc.Question},
		{"运行 ID", doc.RunID}, {"模型", doc.Model}, {"公共规则版本", strconv.FormatInt(doc.RuleVersion, 10)},
		{"数据时点", exportTime(doc.SnapshotAt)}, {"导出时间", exportTime(time.Now())},
		{"结果数量", strconv.Itoa(len(results))}, {"口径", "数据取自回答时保存的结果；不重新查询当前业务库。截断结果不能作为全量明细。"},
		{"结果采用方式", doc.ResultSelection.Status}, {"最终采用结果 ID", strings.Join(doc.ResultSelection.ResultIDs, ", ")},
		{"未作为结论导出的过程查询数", strconv.Itoa(len(doc.ResultSelection.ProcessResultIDs))}, {"结果引用说明", doc.ResultSelection.Notice},
	}
	for _, r := range results {
		prefix := "结果 " + r.ID + " / "
		params, err := exportQueryParams(r.QueryParams)
		if err != nil {
			return nil, err
		}
		metadata = append(metadata, []string{prefix + "数据时点", exportTime(r.SnapshotAt)}, []string{prefix + "查询", r.Query},
			[]string{prefix + "绑定参数（JSON）", params},
			[]string{prefix + "匹配总行数", strconv.FormatInt(r.TotalRows, 10)}, []string{prefix + "导出行数", strconv.Itoa(len(r.Rows))},
			[]string{prefix + "截断", strconv.FormatBool(r.Truncated)}, []string{prefix + "覆盖说明", r.Coverage})
		for _, s := range r.Sources {
			metadata = append(metadata, []string{prefix + "来源", sourceText(s)})
		}
	}
	for i, row := range metadata {
		for j, value := range row {
			if err := exportCell(f, "口径说明", i+1, j+1, value); err != nil {
				return nil, err
			}
		}
	}
	if err := f.SetColWidth("口径说明", "A", "A", 32); err != nil {
		return nil, err
	}
	if err := f.SetColWidth("口径说明", "B", "B", 100); err != nil {
		return nil, err
	}
	answerSheet := "回答内容"
	if _, err := f.NewSheet(answerSheet); err != nil {
		return nil, err
	}
	answerRows := [][]string{{"类型", "标题", "内容/结果 ID"}}
	for _, b := range doc.Blocks {
		content := b.Text
		if b.ResultID != "" {
			content = b.ResultID
		}
		if b.Type == "sources" {
			parts := []string{}
			for _, s := range b.Sources {
				parts = append(parts, sourceText(s))
			}
			content = strings.Join(parts, "\n")
		}
		answerRows = append(answerRows, []string{b.Type, b.Title, content})
	}
	for i, row := range answerRows {
		for j, value := range row {
			if err := exportCell(f, answerSheet, i+1, j+1, value); err != nil {
				return nil, err
			}
		}
	}
	_ = f.SetColWidth(answerSheet, "A", "A", 14)
	_ = f.SetColWidth(answerSheet, "B", "B", 32)
	_ = f.SetColWidth(answerSheet, "C", "C", 100)
	for i, r := range results {
		if len(r.Columns) > 16384 || len(r.Rows) > 1048575 {
			return nil, errors.New("result exceeds Excel worksheet limits")
		}
		name := fmt.Sprintf("结果%02d", i+1)
		if _, err := f.NewSheet(name); err != nil {
			return nil, err
		}
		for j, col := range r.Columns {
			if err := exportCell(f, name, 1, j+1, col); err != nil {
				return nil, err
			}
		}
		for rowIndex, row := range r.Rows {
			for colIndex, value := range row {
				if err := exportCell(f, name, rowIndex+2, colIndex+1, value); err != nil {
					return nil, err
				}
			}
		}
		if len(r.Columns) > 0 {
			last, _ := excelize.ColumnNumberToName(len(r.Columns))
			if err := f.SetColWidth(name, "A", last, 24); err != nil {
				return nil, err
			}
		}
		_ = f.SetPanes(name, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func exportCell(f *excelize.File, sheet string, row, col int, value any) error {
	axis, err := excelize.CoordinatesToCellName(col, row)
	if err != nil {
		return err
	}
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		if utf8.RuneCountInString(v) > 32767 {
			return fmt.Errorf("%s!%s exceeds Excel cell text limit", sheet, axis)
		}
		// SetCellStr, never SetCellFormula, preserves =, +, -, @ and tab-prefixed
		// customer text without turning it into an executable formula.
		return f.SetCellStr(sheet, axis, v)
	case []byte:
		return exportCell(f, sheet, row, col, string(v))
	case bool:
		return f.SetCellBool(sheet, axis, v)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		// Integers beyond Excel's 15-digit precision are preserved as text.
		s := fmt.Sprint(v)
		if len(strings.TrimPrefix(s, "-")) > 15 {
			return f.SetCellStr(sheet, axis, s)
		}
		return f.SetCellValue(sheet, axis, v)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("non-finite number in export")
		}
		return f.SetCellFloat(sheet, axis, v, -1, 64)
	case float32:
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return errors.New("non-finite number in export")
		}
		return f.SetCellFloat(sheet, axis, float64(v), -1, 32)
	case time.Time:
		return f.SetCellStr(sheet, axis, exportTime(v))
	default:
		return exportCell(f, sheet, row, col, exportValue(v))
	}
}

func prepareExportDocument(doc ExportDocument) (ExportDocument, error) {
	status := doc.RunStatus
	if status == "" {
		status = "completed"
	}
	selection, selected, err := ResolveRunAnswerResults(status, doc.Blocks, doc.Results)
	if err != nil {
		return doc, err
	}
	// The HTTP handler may already have selected the same saved rows. Keep its
	// process count for disclosure, but never use metadata to select row values.
	if doc.ResultSelection == nil {
		doc.ResultSelection = &selection
	}
	doc.Results = selected
	allowedSources := map[string]Source{}
	for _, result := range selected {
		for _, source := range result.Sources {
			allowedSources[source.Type+":"+source.ID] = source
		}
	}
	blocks := append([]Block{}, doc.Blocks...)
	if status != "completed" {
		blocks = []Block{}
		for _, block := range doc.Blocks {
			if block.Type == "markdown" {
				blocks = append(blocks, Block{Type: "markdown", Text: block.Text})
			}
		}
	}
	for i := range blocks {
		if blocks[i].Type != "sources" {
			continue
		}
		// Old automatic sources mixed all queried records together. Only retain
		// references evidenced by the adopted results, with stored source values.
		var sources []Source
		seen := map[string]bool{}
		for _, original := range blocks[i].Sources {
			key := original.Type + ":" + original.ID
			if source, ok := allowedSources[key]; ok && !seen[key] {
				seen[key] = true
				sources = append(sources, source)
			}
		}
		blocks[i].Sources = sources
	}
	doc.Blocks = blocks
	return doc, nil
}

func exportResults(doc ExportDocument) ([]Result, error) {
	results := []Result{}
	seen := map[string]bool{}
	add := func(r Result) error {
		if r.ID == "" {
			return errors.New("export result missing ID")
		}
		for _, row := range r.Rows {
			if len(row) != len(r.Columns) {
				return fmt.Errorf("result %s has inconsistent columns", r.ID)
			}
		}
		if r.TotalRows < int64(len(r.Rows)) {
			return fmt.Errorf("result %s has invalid total rows", r.ID)
		}
		if seen[r.ID] {
			return nil
		}
		seen[r.ID] = true
		results = append(results, r)
		return nil
	}
	for _, r := range doc.Results {
		if err := add(r); err != nil {
			return nil, err
		}
	}
	for _, b := range doc.Blocks {
		if b.Result != nil {
			if b.ResultID == "" || b.ResultID != b.Result.ID {
				return nil, errors.New("block result reference mismatch")
			}
			if !seen[b.ResultID] {
				return nil, errors.New("inline block result is not an adopted saved result")
			}
		}
	}
	for _, b := range doc.Blocks {
		if (b.Type == "chart" || b.Type == "table") && !seen[b.ResultID] {
			return nil, fmt.Errorf("saved result %s is unavailable", b.ResultID)
		}
	}
	return results, nil
}

func exportValue(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case []byte:
		return string(val)
	case time.Time:
		return exportTime(val)
	case json.Number:
		return val.String()
	}
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return fmt.Sprint(v)
}

const unavailableQueryParams = "未保存：该结果缺少绑定参数，含占位符的筛选值无法核对。"

func exportQueryParams(params []any) (string, error) {
	if params == nil {
		return unavailableQueryParams, nil
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return "", fmt.Errorf("无法导出查询绑定参数: %w", err)
	}
	return string(encoded), nil
}

func exportTime(t time.Time) string {
	if t.IsZero() {
		return "未知"
	}
	return t.In(budgetShanghai).Format(time.RFC3339)
}
func sourceText(s Source) string {
	return strings.TrimSpace(fmt.Sprintf("%s:%s %s %s", s.Type, s.ID, s.Label, s.OccurredAt))
}

type htmlExportResult struct {
	ID          string
	Columns     []string
	Rows        [][]string
	TotalRows   int64
	Truncated   bool
	Coverage    string
	Query       string
	QueryParams string
	Snapshot    string
	Sources     []string
}
type htmlExportBlock struct {
	Type          string
	Title         string
	Text          string
	ResultID      string
	SVG           template.HTML
	ChartCoverage string
	Sources       []string
}
type htmlExportDocument struct {
	Title           string
	Question        string
	RunID           string
	Model           string
	RuleVersion     int64
	Snapshot        string
	Generated       string
	FormatVersion   string
	Blocks          []htmlExportBlock
	Results         []htmlExportResult
	SelectionNotice string
	ProcessCount    int
}

// RenderHTML is a printable, standalone report. The only trusted markup is SVG
// constructed here from validated numbers and escaped labels; model text never
// becomes raw HTML and there are no scripts or remote resources.
func RenderHTML(doc ExportDocument) ([]byte, error) {
	doc, err := prepareExportDocument(doc)
	if err != nil {
		return nil, err
	}
	results, err := exportResults(doc)
	if err != nil {
		return nil, err
	}
	view := htmlExportDocument{Title: doc.Title, Question: doc.Question, RunID: doc.RunID, Model: doc.Model, RuleVersion: doc.RuleVersion, Snapshot: exportTime(doc.SnapshotAt), Generated: exportTime(time.Now()), FormatVersion: exportFormatVersion}
	view.SelectionNotice = doc.ResultSelection.Notice
	view.ProcessCount = len(doc.ResultSelection.ProcessResultIDs)
	byID := map[string]Result{}
	for _, r := range results {
		byID[r.ID] = r
		params, err := exportQueryParams(r.QueryParams)
		if err != nil {
			return nil, err
		}
		hr := htmlExportResult{ID: r.ID, Columns: r.Columns, TotalRows: r.TotalRows, Truncated: r.Truncated, Coverage: r.Coverage, Query: r.Query, QueryParams: params, Snapshot: exportTime(r.SnapshotAt)}
		for _, row := range r.Rows {
			values := []string{}
			for _, v := range row {
				values = append(values, exportValue(v))
			}
			hr.Rows = append(hr.Rows, values)
		}
		for _, s := range r.Sources {
			hr.Sources = append(hr.Sources, sourceText(s))
		}
		view.Results = append(view.Results, hr)
	}
	for _, b := range doc.Blocks {
		hb := htmlExportBlock{Type: b.Type, Title: b.Title, Text: b.Text, ResultID: b.ResultID}
		if b.Type == "chart" {
			chartResult, err := savedChartResult(b, byID[b.ResultID])
			if err != nil {
				return nil, err
			}
			if int64(len(chartResult.Rows)) < chartResult.TotalRows {
				hb.ChartCoverage = fmt.Sprintf("图表展示原回答结果第%d至%d行；共%d行，已保存结果见下表。", chartResult.Offset+1, chartResult.Offset+len(chartResult.Rows), chartResult.TotalRows)
			}
			svg, err := exportChart(b, chartResult)
			if err != nil {
				return nil, err
			}
			hb.SVG = template.HTML(svg)
		}
		for _, s := range b.Sources {
			hb.Sources = append(hb.Sources, sourceText(s))
		}
		view.Blocks = append(view.Blocks, hb)
	}
	t, err := template.New("report").Parse(htmlReportTemplate)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := t.Execute(&out, view); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// The answer preview fixes only the chart range. Row values are taken from the
// same saved result used by Excel and tables, never a second embedded dataset.
func savedChartResult(block Block, saved Result) (Result, error) {
	start, count := 0, min(len(saved.Rows), 500)
	if block.Result != nil {
		preview := block.Result
		if preview.ID != saved.ID || !preview.SnapshotAt.Equal(saved.SnapshotAt) || len(preview.Columns) != len(saved.Columns) {
			return Result{}, errors.New("chart preview does not match saved result")
		}
		for i, col := range preview.Columns {
			if col != saved.Columns[i] {
				return Result{}, errors.New("chart preview columns do not match saved result")
			}
		}
		start, count = preview.Offset-saved.Offset, min(len(preview.Rows), 500)
		if start < 0 || start > len(saved.Rows) || count > len(saved.Rows)-start {
			return Result{}, errors.New("chart preview range is not available in saved result")
		}
	}
	chart := saved
	chart.Offset = saved.Offset + start
	chart.Rows = saved.Rows[start : start+count]
	chart.ReturnedRows = count
	return chart, nil
}

const htmlReportTemplate = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'"><title>{{.Title}}</title><style>
body{margin:0;background:#f4f6fa;color:#172338;font:15px/1.7 system-ui,-apple-system,"PingFang SC",sans-serif}main{max-width:1100px;margin:30px auto;padding:32px;background:white;border-radius:14px}h1{font-size:28px}h2{font-size:21px;margin-top:28px}h3{font-size:17px}p,.answer{white-space:pre-wrap;overflow-wrap:anywhere}.meta{color:#526075;font-size:13px}.scroll{overflow-x:auto}table{border-collapse:collapse;width:100%;margin:12px 0;font-size:13px}th,td{padding:9px 12px;border:1px solid #dce2ec;text-align:left;vertical-align:top;white-space:pre-wrap;overflow-wrap:anywhere}th{background:#f1f5fb}svg{width:100%;max-height:420px}details{font-size:12px}pre{white-space:pre-wrap;overflow-wrap:anywhere}.notice{padding:10px 14px;background:#fff6df}.sources{color:#526075}footer{border-top:1px solid #dce2ec;margin-top:30px;padding-top:12px;font-size:12px;color:#526075}@media(max-width:600px){main{margin:0;padding:18px;border-radius:0}}@media print{body{background:white}main{max-width:none;margin:0;padding:0;border-radius:0}svg,tr{break-inside:avoid}details{display:block}.scroll{overflow:visible}th,td{padding:5px;font-size:11px}h2,h3{break-after:avoid}}
</style></head><body><main><h1>{{.Title}}</h1><p>{{.Question}}</p><div class="meta">数据时点：{{.Snapshot}} · 模型：{{.Model}} · 公共规则版本：{{.RuleVersion}}<br>运行：{{.RunID}} · 导出：{{.Generated}}</div>
{{if .SelectionNotice}}<p class="notice">{{.SelectionNotice}}</p>{{end}}{{range .Blocks}}<section>{{if .Title}}<h2>{{.Title}}</h2>{{end}}{{if .Text}}<div class="answer">{{.Text}}</div>{{end}}{{if .SVG}}{{.SVG}}{{end}}{{if .ChartCoverage}}<p class="meta">{{.ChartCoverage}}</p>{{end}}{{if .ResultID}}<p class="meta">引用保存结果：{{.ResultID}}</p>{{end}}{{if .Sources}}<ul class="sources">{{range .Sources}}<li>{{.}}</li>{{end}}</ul>{{end}}</section>{{end}}
<h2>最终采用的分析结果</h2>{{if .ProcessCount}}<p class="meta">另有 {{.ProcessCount}} 个查询过程结果未作为结论导出，可在原会话中核对。</p>{{end}}{{range .Results}}<section><h3>结果 {{.ID}}</h3><p class="meta">数据时点：{{.Snapshot}} · 匹配 {{.TotalRows}} 行 · 本报告展示 {{len .Rows}} 行</p>{{if .Truncated}}<p class="notice">该明细结果已截断，不能视为全部匹配记录。</p>{{end}}{{if .Coverage}}<p>{{.Coverage}}</p>{{end}}<div class="scroll"><table><thead><tr>{{range .Columns}}<th>{{.}}</th>{{end}}</tr></thead><tbody>{{range .Rows}}<tr>{{range .}}<td>{{.}}</td>{{end}}</tr>{{else}}<tr><td>没有匹配记录</td></tr>{{end}}</tbody></table></div>{{if .Sources}}<ul class="sources">{{range .Sources}}<li>{{.}}</li>{{end}}</ul>{{end}}{{if .Query}}<details><summary>查询口径</summary><pre aria-label="查询依据">{{.Query}}</pre><p>绑定参数（按绑定顺序，JSON；[] 表示无绑定参数）：</p><pre aria-label="查询绑定参数">{{.QueryParams}}</pre></details>{{end}}</section>{{end}}
<footer>{{.FormatVersion}} · 本报告来自回答时保存的结果，未重新查询当前业务库。使用浏览器打印功能可保存 PDF。</footer></main></body></html>`

func exportChart(block Block, r Result) (string, error) {
	xIndex := -1
	for i, col := range r.Columns {
		if col == block.XColumn {
			xIndex = i
			break
		}
	}
	if xIndex < 0 || len(block.YColumns) == 0 {
		return "", errors.New("chart columns are unavailable")
	}
	yIndices := []int{}
	for _, name := range block.YColumns {
		idx := -1
		for i, col := range r.Columns {
			if col == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return "", errors.New("chart numeric column unavailable")
		}
		yIndices = append(yIndices, idx)
	}
	if len(r.Rows) == 0 {
		return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 80" role="img" aria-label="没有图表数据"><text x="30" y="45">没有图表数据</text></svg>`, nil
	}
	if len(r.Rows) > 500 {
		return "", errors.New("chart exceeds 500 rows; narrow the saved result")
	}
	values := make([][]float64, len(yIndices))
	maxValue := 0.0
	minValue := 0.0
	for s, idx := range yIndices {
		for _, row := range r.Rows {
			v, err := exportNumber(row[idx])
			if err != nil {
				return "", err
			}
			values[s] = append(values[s], v)
			maxValue = math.Max(maxValue, v)
			minValue = math.Min(minValue, v)
		}
	}
	colors := []string{"#2463eb", "#12a888", "#f29935", "#8959cc", "#e05b7d"}
	var out strings.Builder
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 400" role="img" aria-label="%s"><rect width="800" height="400" fill="white"/>`, html.EscapeString(block.Title))
	if block.ChartType == "pie" {
		if len(yIndices) != 1 {
			return "", errors.New("pie chart requires one series")
		}
		total := 0.0
		for _, v := range values[0] {
			if v < 0 {
				return "", errors.New("pie chart cannot contain negative values")
			}
			total += v
		}
		if total <= 0 || math.IsInf(total, 0) {
			return "", errors.New("pie chart requires positive total")
		}
		angle := -math.Pi / 2
		for i, v := range values[0] {
			if v == 0 {
				continue
			}
			next := angle + v/total*2*math.Pi
			color := colors[i%len(colors)]
			if v == total {
				fmt.Fprintf(&out, `<circle cx="225" cy="185" r="130" fill="%s"/>`, color)
			} else {
				large := 0
				if next-angle > math.Pi {
					large = 1
				}
				fmt.Fprintf(&out, `<path d="M 225 185 L %.3f %.3f A 130 130 0 %d 1 %.3f %.3f Z" fill="%s"/>`, 225+130*math.Cos(angle), 185+130*math.Sin(angle), large, 225+130*math.Cos(next), 185+130*math.Sin(next), color)
			}
			angle = next
			if i < 12 {
				fmt.Fprintf(&out, `<rect x="430" y="%d" width="12" height="12" fill="%s"/><text x="450" y="%d" font-size="12">%s: %s</text>`, 45+i*25, color, 56+i*25, html.EscapeString(shortLabel(exportValue(r.Rows[i][xIndex]), 25)), html.EscapeString(strconv.FormatFloat(v, 'f', -1, 64)))
			}
		}
	} else if block.ChartType == "bar" || block.ChartType == "line" || block.ChartType == "scatter" {
		if maxValue == minValue {
			maxValue = minValue + 1
		}
		if math.IsInf(maxValue-minValue, 0) {
			return "", errors.New("chart numeric range is too large")
		}
		height := 260.0
		plotWidth := 650.0
		y := func(v float64) float64 { return 310 - (v-minValue)/(maxValue-minValue)*height }
		n := float64(len(r.Rows))
		x := func(i int) float64 { return 85 + (float64(i)+0.5)/n*plotWidth }
		xValues := []float64{}
		minX, maxX := 0.0, 0.0
		if block.ChartType == "scatter" {
			minX = math.Inf(1)
			maxX = math.Inf(-1)
			for _, row := range r.Rows {
				v, err := exportNumber(row[xIndex])
				if err != nil {
					return "", err
				}
				xValues = append(xValues, v)
				minX = math.Min(minX, v)
				maxX = math.Max(maxX, v)
			}
			if maxX == minX {
				maxX = minX + 1
			}
			if math.IsInf(maxX-minX, 0) {
				return "", errors.New("scatter numeric range is too large")
			}
			x = func(i int) float64 { return 85 + (xValues[i]-minX)/(maxX-minX)*plotWidth }
		}
		fmt.Fprintf(&out, `<path d="M 85 50 V 310 H 735" fill="none" stroke="#94a3b8"/><text x="12" y="58" font-size="12">%s</text><text x="12" y="310" font-size="12">%s</text>`, html.EscapeString(strconv.FormatFloat(maxValue, 'g', 4, 64)), html.EscapeString(strconv.FormatFloat(minValue, 'g', 4, 64)))
		for s, series := range values {
			color := colors[s%len(colors)]
			points := []string{}
			for i, v := range series {
				px, py := x(i), y(v)
				if block.ChartType == "bar" {
					width := math.Max(0.1, plotWidth/n*0.75/float64(len(values)))
					left := px - plotWidth/n*0.375 + float64(s)*width
					zeroY := y(0)
					fmt.Fprintf(&out, `<rect x="%.3f" y="%.3f" width="%.3f" height="%.3f" fill="%s"/>`, left, math.Min(py, zeroY), width, math.Abs(zeroY-py), color)
				} else if block.ChartType == "scatter" {
					fmt.Fprintf(&out, `<circle cx="%.3f" cy="%.3f" r="3" fill="%s"/>`, px, py, color)
				} else {
					points = append(points, fmt.Sprintf("%.3f,%.3f", px, py))
				}
			}
			if block.ChartType == "line" {
				fmt.Fprintf(&out, `<polyline points="%s" fill="none" stroke="%s" stroke-width="2"/>`, strings.Join(points, " "), color)
			}
			fmt.Fprintf(&out, `<text x="%d" y="390" font-size="12" fill="%s">%s</text>`, 85+s*140, color, html.EscapeString(shortLabel(block.YColumns[s], 18)))
		}
		step := int(math.Ceil(n / 8))
		for i := 0; i < len(r.Rows); i += step {
			fmt.Fprintf(&out, `<text x="%.3f" y="337" font-size="11" text-anchor="middle">%s</text>`, x(i), html.EscapeString(shortLabel(exportValue(r.Rows[i][xIndex]), 12)))
		}
	} else {
		return "", errors.New("unsupported chart type")
	}
	out.WriteString(`</svg>`)
	return out.String(), nil
}

// ValidateChart shares the rendering contract with the presentation tool so
// invalid chart choices can be corrected before persisting an answer.
func ValidateChart(block Block, r Result) error {
	if r.MeasureScope != nil && r.MeasureScope.GroupBy != "" {
		return errors.New("统计结果含整体和分组，图表不能混合相加；请query_data读取仅分组的真实结果再展示")
	}
	_, err := exportChart(block, r)
	return err
}

func exportNumber(v any) (float64, error) {
	var n float64
	var err error
	switch x := v.(type) {
	case float64:
		n = x
	case float32:
		n = float64(x)
	case int:
		n = float64(x)
	case int64:
		n = float64(x)
	case int32:
		n = float64(x)
	case json.Number:
		n, err = x.Float64()
	default:
		err = errors.New("chart values must be numeric result columns")
	}
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, errors.New("chart values must be finite numbers")
	}
	return n, nil
}
func shortLabel(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}
