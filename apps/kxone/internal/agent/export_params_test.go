package agent

import (
	"bytes"
	"encoding/json"
	"html"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestExportQueryParametersPreserveSavedBindingsAndLegacyUnknown(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params []any
	}{
		{"bound", []any{"客户#1", "%费用%", float64(10), nil, true, `<script>not executable</script>`, "=1+1"}},
		{"no-bindings", []any{}},
		{"legacy-unknown", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := exportFixture()
			doc.Results[0].Query = "SELECT id FROM leads WHERE customer_ref=?"
			doc.Results[0].QueryParams = tc.params
			body, err := RenderXLSX(doc)
			if err != nil {
				t.Fatal(err)
			}
			workbook, err := excelize.OpenReader(bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			defer workbook.Close()
			rows, err := workbook.GetRows("口径说明")
			if err != nil {
				t.Fatal(err)
			}
			var value string
			for index, row := range rows {
				if len(row) >= 2 && row[0] == "结果 r-saved / 绑定参数（JSON）" {
					value = row[1]
					cell, _ := excelize.CoordinatesToCellName(2, index+1)
					formula, err := workbook.GetCellFormula("口径说明", cell)
					if err != nil || formula != "" {
						t.Fatalf("parameter became a formula: %q %v", formula, err)
					}
				}
			}
			if tc.params == nil {
				if value != unavailableQueryParams {
					t.Fatalf("legacy parameters invented: %q", value)
				}
			} else {
				var actual []any
				if err = json.Unmarshal([]byte(value), &actual); err != nil || !reflect.DeepEqual(actual, tc.params) {
					t.Fatalf("XLSX changed bindings: %q %v", value, err)
				}
			}
			report, err := RenderHTML(doc)
			if err != nil {
				t.Fatal(err)
			}
			text := string(report)
			if strings.Contains(text, "<script>") {
				t.Fatal("query binding rendered as executable HTML")
			}
			marker := `<pre aria-label="查询绑定参数">`
			start := strings.Index(text, marker)
			if start < 0 {
				t.Fatal("HTML report omitted query bindings")
			}
			end := strings.Index(text[start+len(marker):], "</pre>")
			if end < 0 {
				t.Fatal("HTML parameter block incomplete")
			}
			got := html.UnescapeString(text[start+len(marker) : start+len(marker)+end])
			if got != value {
				t.Fatalf("HTML/XLSX disagree on saved parameters: %q != %q", got, value)
			}
		})
	}
}

func TestExportInvalidQueryParametersFailRatherThanOmitScope(t *testing.T) {
	doc := exportFixture()
	doc.Results[0].QueryParams = []any{math.Inf(1)}
	if _, err := RenderXLSX(doc); err == nil {
		t.Fatal("non-finite binding silently omitted from XLSX")
	}
	if _, err := RenderHTML(doc); err == nil {
		t.Fatal("non-finite binding silently omitted from HTML")
	}
}
