package agent

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

func correctedCountDocument() ExportDocument {
	bad := Result{ID: "exploratory-cartesian", Columns: []string{"线索数"}, Rows: [][]any{{int64(49)}}, TotalRows: 1, Query: "SELECT COUNT(*) FROM leads a JOIN leads b ON 1=1", QueryParams: []any{}}
	good := Result{ID: "final-corrected", Columns: []string{"线索数"}, Rows: [][]any{{int64(7)}}, TotalRows: 1, Query: "SELECT COUNT(*) FROM leads", QueryParams: []any{}}
	return ExportDocument{Title: "纠正后的线索数", Question: "共多少条线索？", Blocks: []Block{{Type: "markdown", Text: "已纠正重复关联，共7条。"}, {Type: "table", ResultID: good.ID}}, Results: []Result{bad, good}}
}

func TestExportDoesNotMixExploratoryCountsWithFinalAnswer(t *testing.T) {
	for _, mode := range []string{"legacy_table", "explicit_markdown", "explicit_empty", "legacy_markdown"} {
		t.Run(mode, func(t *testing.T) {
			doc := correctedCountDocument()
			wantResults := 1
			switch mode {
			case "explicit_markdown":
				doc.Blocks = []Block{doc.Blocks[0], {Type: "sources", ResultIDs: []string{"final-corrected"}}}
			case "explicit_empty":
				doc.Blocks = []Block{{Type: "markdown", Text: "现有数据无法证明此结论。"}, {Type: "sources", ResultIDs: []string{}}}
				wantResults = 0
			case "legacy_markdown":
				doc.Blocks = doc.Blocks[:1]
				wantResults = 0
			}
			html, err := RenderHTML(doc)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(html, []byte("exploratory-cartesian")) || bytes.Contains(html, []byte("ON 1=1")) || bytes.Contains(html, []byte("<td>49</td>")) {
				t.Fatal("exploratory wrong answer was exported as an analysis result")
			}
			if got := strings.Count(string(html), "<h3>结果 "); got != wantResults {
				t.Fatalf("HTML result count=%d want=%d", got, wantResults)
			}
			xlsx, err := RenderXLSX(doc)
			if err != nil {
				t.Fatal(err)
			}
			book, err := excelize.OpenReader(bytes.NewReader(xlsx))
			if err != nil {
				t.Fatal(err)
			}
			defer book.Close()
			if got := len(book.GetSheetList()) - 2; got != wantResults {
				t.Fatalf("Excel result count=%d want=%d", got, wantResults)
			}
			if wantResults == 1 {
				value, _ := book.GetCellValue("结果01", "A2")
				if value != "7" {
					t.Fatalf("final corrected number=%s", value)
				}
			}
			if mode == "legacy_markdown" && !bytes.Contains(html, []byte("未保存最终引用结果")) {
				t.Fatal("historical missing-reference limitation not explained")
			}
		})
	}
}

func TestAnswerResultSelectionRejectsInvalidReferencesAndPreservesExplicitEmpty(t *testing.T) {
	doc := correctedCountDocument()
	byID := map[string]*Result{doc.Results[1].ID: &doc.Results[1]}
	for _, ids := range [][]string{nil, {""}, {"foreign-result"}, {"final-corrected", "final-corrected"}} {
		if ValidateFinalResultIDs(ids, byID) == nil {
			t.Fatalf("invalid selection accepted: %#v", ids)
		}
	}
	if err := ValidateFinalResultIDs([]string{}, byID); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]string{nil, {}} {
		data, err := json.Marshal(Block{Type: "sources", ResultIDs: ids})
		if err != nil {
			t.Fatal(err)
		}
		var loaded Block
		if err = json.Unmarshal(data, &loaded); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(loaded.ResultIDs, ids) {
			t.Fatalf("nil/empty manifest collapsed: %s", data)
		}
	}
	for _, blocks := range [][]Block{
		{{Type: "sources", ResultIDs: []string{"foreign-result"}}},
		{{Type: "sources", ResultIDs: []string{}}, {Type: "sources", ResultIDs: []string{}}},
		{{Type: "table", ResultID: "exploratory-cartesian"}, {Type: "sources", ResultIDs: []string{"final-corrected"}}},
	} {
		if _, _, err := ResolveAnswerResults(blocks, doc.Results); err == nil {
			t.Fatalf("invalid final blocks accepted: %+v", blocks)
		}
	}
}

func TestExportUnfinishedRunCannotPromotePresentedExploration(t *testing.T) {
	for _, status := range []string{"failed", "cancelled", "interrupted", "budget_exceeded"} {
		doc := correctedCountDocument()
		doc.RunStatus = status
		doc.Blocks = []Block{{Type: "markdown", Text: "已经保存部分内容。"}, {Type: "table", ResultID: doc.Results[0].ID}}
		html, err := RenderHTML(doc)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(html, []byte("本轮分析未完成")) || bytes.Contains(html, []byte("<td>49</td>")) || bytes.Contains(html, []byte("<h3>结果 ")) {
			t.Fatalf("%s exported an unconfirmed conclusion", status)
		}
	}
}

func TestExportSourcesCannotReintroduceDiscardedQueryEvidence(t *testing.T) {
	doc := correctedCountDocument()
	bad := Source{Type: "lead_followups", ID: "49", Label: "discarded-evidence"}
	good := Source{Type: "lead_followups", ID: "7", Label: "adopted-evidence"}
	doc.Results[0].Sources = []Source{bad}
	doc.Results[1].Sources = []Source{good}
	doc.Blocks = append(doc.Blocks, Block{Type: "sources", Sources: []Source{bad, good}})
	html, err := RenderHTML(doc)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(html, []byte("discarded-evidence")) || !bytes.Contains(html, []byte("adopted-evidence")) {
		t.Fatal("legacy source cards mixed discarded query evidence into final export")
	}
}

func TestAgentHTTPExportsOnlyFinalSavedResultsAndClassifiesProcess(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("export must not call a model")
		w.WriteHeader(500)
	})
	conversation := securityCreateConversation(t, f, securityAdmin)
	now := time.Now().UTC()
	run := Run{ID: uuid.NewString(), ConversationID: conversation.ID, AccountID: securityAdmin.AccountID, RequestID: uuid.NewString(), AuthVersion: 1, Status: "completed", CreatedAt: now, FinishedAt: &now}
	if err := f.db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	doc := correctedCountDocument()
	doc.Blocks = append(doc.Blocks, Block{Type: "sources", ResultIDs: []string{"final-corrected"}})
	blocks, _ := json.Marshal(doc.Blocks)
	for _, message := range []Message{
		{ID: uuid.NewString(), ConversationID: conversation.ID, RunID: run.ID, Role: "user", Content: doc.Question, Status: "completed", CreatedAt: now},
		{ID: uuid.NewString(), ConversationID: conversation.ID, RunID: run.ID, Role: "assistant", Content: doc.Blocks[0].Text, BlocksJSON: string(blocks), Status: "completed", CreatedAt: now.Add(time.Second)},
	} {
		if err := f.db.Create(&message).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, result := range doc.Results {
		if err := f.service.saveResult(t.Context(), run.ID, &result); err != nil {
			t.Fatal(err)
		}
	}
	get := securityRequest(t, f, http.MethodGet, "/api/agent/runs/"+run.ID, nil, securityAdmin)
	var detail struct {
		Selection AnswerResultSelection `json:"result_selection"`
		Results   []Result              `json:"results"`
	}
	if get.Code != 200 || json.Unmarshal(get.Body.Bytes(), &detail) != nil {
		t.Fatalf("get run=%d %s", get.Code, get.Body)
	}
	if len(detail.Results) != 2 || detail.Selection.Status != "explicit" || !reflect.DeepEqual(detail.Selection.ResultIDs, []string{"final-corrected"}) || !reflect.DeepEqual(detail.Selection.ProcessResultIDs, []string{"exploratory-cartesian"}) {
		t.Fatalf("missing final/process distinction: %+v", detail)
	}
	for _, format := range []string{"html", "xlsx"} {
		response := securityRequest(t, f, http.MethodPost, "/api/agent/runs/"+run.ID+"/exports", map[string]any{"format": format}, securityAdmin)
		var artifact struct {
			ID string `json:"id"`
		}
		if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &artifact) != nil {
			t.Fatalf("export=%d %s", response.Code, response.Body)
		}
		path := "/api/agent/exports/" + artifact.ID + "/download"
		denied := securityRequest(t, f, http.MethodGet, path, nil, securityManager)
		if denied.Code != 403 {
			t.Fatalf("foreign download=%d", denied.Code)
		}
		download := securityRequest(t, f, http.MethodGet, path, nil, securityAdmin)
		if download.Code != 200 {
			t.Fatalf("download=%d", download.Code)
		}
		if format == "html" {
			if bytes.Contains(download.Body.Bytes(), []byte("exploratory-cartesian")) || !bytes.Contains(download.Body.Bytes(), []byte("<td>7</td>")) {
				t.Fatal("HTTP HTML included wrong process result")
			}
		} else {
			book, err := excelize.OpenReader(bytes.NewReader(download.Body.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			defer book.Close()
			value, _ := book.GetCellValue("结果01", "A2")
			if len(book.GetSheetList()) != 3 || value != "7" {
				t.Fatal("HTTP Excel mixed process/final rows")
			}
		}
	}
	if err := f.db.Model(&Message{}).Where("run_id=? AND role='assistant'", run.ID).Update("blocks_json", `[{"type":"sources","result_ids":["foreign-result"]}]`).Error; err != nil {
		t.Fatal(err)
	}
	denied := securityRequest(t, f, http.MethodPost, "/api/agent/runs/"+run.ID+"/exports", map[string]any{"format": "html"}, securityAdmin)
	if denied.Code == 201 {
		t.Fatal("an unowned/unavailable final result was exported")
	}
}
