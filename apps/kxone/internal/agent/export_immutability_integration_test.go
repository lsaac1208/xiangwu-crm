package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

// A31 checks the entire persisted-result/export boundary against a real SQLite
// business mutation. It makes no model call, and never substitutes an in-memory
// ExportDocument for the saved result read by authenticated HTTP handlers.
func TestHistoricalHTTPExportsRemainImmutableAfterBusinessChangesAndCacheExpiry(t *testing.T) {
	var modelCalls atomic.Int32
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		modelCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	stamp := time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC)
	for _, lead := range []repo.LeadModel{
		{ID: 701, LeadNo: "IMMUTABLE-701", CustomerName: "隔离客户", SourceChannel: "好学校", Status: "completed", BusinessStage: "closed", OwnershipState: "private", ResultStatus: "won", DealAmount: 100, PaymentStatus: "paid", CreatedAt: stamp, UpdatedAt: stamp},
		{ID: 702, LeadNo: "IMMUTABLE-702", CustomerName: "隔离客户", SourceChannel: "好学校", Status: "completed", BusinessStage: "closed", OwnershipState: "private", ResultStatus: "won", DealAmount: 70, RefundAmount: 20, PaymentStatus: "paid", CreatedAt: stamp, UpdatedAt: stamp},
		{ID: 703, LeadNo: "IMMUTABLE-703", CustomerName: "隔离客户", SourceChannel: "转介绍", Status: "completed", BusinessStage: "closed", OwnershipState: "private", ResultStatus: "won", DealAmount: 40, PaymentStatus: "paid", CreatedAt: stamp, UpdatedAt: stamp},
	} {
		if err := f.db.Create(&lead).Error; err != nil {
			t.Fatal(err)
		}
	}
	const query = "SELECT source_channel, COUNT(*) AS lead_count, SUM(net_deal_amount_cents) AS net_cents FROM leads GROUP BY source_channel ORDER BY source_channel"
	snapshot, err := BuildSnapshot(t.Context(), f.db, f.service.cfg.Directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = snapshot.Close() })
	preview, err := snapshot.Query(t.Context(), query, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	full, err := snapshot.MaterializeResult(t.Context(), preview, 100000)
	if err != nil {
		t.Fatal(err)
	}
	wantRows := [][]any{{"好学校", int64(2), int64(15000)}, {"转介绍", int64(1), int64(4000)}}
	if !reflect.DeepEqual(full.Rows, wantRows) || len(preview.Rows) != 1 || preview.TotalRows != 2 || !preview.Truncated || full.Truncated {
		t.Fatalf("original snapshot differs from the independent fixture expectation: %+v", full)
	}
	conversation := securityCreateConversation(t, f, securityAdmin)
	run := Run{ID: uuid.NewString(), ConversationID: conversation.ID, AccountID: securityAdmin.AccountID, RequestID: uuid.NewString(), AuthVersion: 1, Status: "running", Model: "no-model-export-integration", RuleVersion: 0, SnapshotAt: &snapshot.At, CreatedAt: stamp}
	if err := f.db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	for _, message := range []Message{
		{ID: uuid.NewString(), ConversationID: conversation.ID, RunID: run.ID, Role: "user", Content: "按来源展示线索数和净成交金额，并保留本次分析。", Status: "completed", CreatedAt: stamp},
		{ID: uuid.NewString(), ConversationID: conversation.ID, RunID: run.ID, Role: "assistant", Status: "running", CreatedAt: stamp.Add(time.Second)},
	} {
		if err := f.db.Create(&message).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := f.service.saveResult(t.Context(), run.ID, full); err != nil {
		t.Fatal(err)
	}
	blocks := []Block{
		{Type: "markdown", Text: "按回答时保存的来源分组结果展示；净成交金额不等同实际现金流。"},
		{Type: "table", Title: "来源数量和净成交金额（分）", ResultID: full.ID, Result: preview},
		{Type: "chart", Title: "原回答的来源数量预览", ResultID: full.ID, Result: preview, ChartType: "bar", XColumn: "source_channel", YColumns: []string{"lead_count"}},
		{Type: "sources", ResultIDs: []string{full.ID}},
	}
	if err := f.service.finishRun(t.Context(), run.ID, "completed", "", blocks[0].Text, blocks); err != nil {
		t.Fatal(err)
	}
	var originalRecord ResultRecord
	if err := f.db.First(&originalRecord, "id=?", full.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(snapshot.path); !os.IsNotExist(err) {
		t.Fatal("original temporary snapshot must be gone before export checks")
	}

	type artifact struct {
		ID string `json:"id"`
	}
	cache := []ExportRecord{}
	createAndDownload := func(format string) []byte {
		t.Helper()
		created := securityRequest(t, f, http.MethodPost, "/api/agent/runs/"+run.ID+"/exports", map[string]any{"format": format}, securityAdmin)
		var exported artifact
		if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &exported) != nil || exported.ID == "" {
			t.Fatalf("HTTP %s export failed: %d %s", format, created.Code, created.Body)
		}
		var row ExportRecord
		if err := f.db.First(&row, "id=?", exported.ID).Error; err != nil {
			t.Fatal(err)
		}
		for _, old := range cache {
			if row.ID == old.ID || row.Path == old.Path {
				t.Fatal("regeneration reused the earlier file instead of saved answer data")
			}
		}
		cache = append(cache, row)
		download := securityRequest(t, f, http.MethodGet, "/api/agent/exports/"+exported.ID+"/download", nil, securityAdmin)
		if download.Code != http.StatusOK {
			t.Fatalf("HTTP %s download failed: %d", format, download.Code)
		}
		return append([]byte(nil), download.Body.Bytes()...)
	}
	checkSavedAPI := func() {
		t.Helper()
		response := securityRequest(t, f, http.MethodGet, "/api/agent/results/"+full.ID, nil, securityAdmin)
		var result Result
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil {
			t.Fatal("saved result API failed")
		}
		wantJSON, _ := json.Marshal(wantRows)
		gotJSON, _ := json.Marshal(result.Rows)
		if !bytes.Equal(wantJSON, gotJSON) || result.TotalRows != 2 || !result.SnapshotAt.Equal(full.SnapshotAt) || result.Query != query || !result.MaterializedComplete {
			t.Fatalf("historical result API followed current business data: %s", response.Body)
		}
		second := securityRequest(t, f, http.MethodGet, "/api/agent/results/"+full.ID+"?offset=1&limit=1", nil, securityAdmin)
		if second.Code != http.StatusOK || json.Unmarshal(second.Body.Bytes(), &result) != nil || result.Offset != 1 || len(result.Rows) != 1 || result.Rows[0][0] != "转介绍" || fmt.Sprint(result.Rows[0][2]) != "4000" {
			t.Fatal("historical paginated result changed")
		}
		var record ResultRecord
		if err := f.db.First(&record, "id=?", full.ID).Error; err != nil || record.Data != originalRecord.Data {
			t.Fatal("persisted historical result was rewritten")
		}
	}
	checkXLSX := func(body []byte) {
		t.Helper()
		book, err := excelize.OpenReader(bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer book.Close()
		rows, err := book.GetRows("结果01")
		want := [][]string{{"source_channel", "lead_count", "net_cents"}, {"好学校", "2", "15000"}, {"转介绍", "1", "4000"}}
		if err != nil || !reflect.DeepEqual(rows, want) || len(book.GetSheetList()) != 3 {
			t.Fatalf("new Excel did not use all original saved rows: %v %v", rows, err)
		}
		metadata, err := book.GetRows("口径说明")
		if err != nil {
			t.Fatal(err)
		}
		text := fmt.Sprint(metadata)
		for _, expected := range []string{run.ID, full.ID, exportTime(full.SnapshotAt), query} {
			if !strings.Contains(text, expected) {
				t.Fatalf("Excel lost original evidence metadata %q", expected)
			}
		}
	}
	chartOf := func(body []byte) string {
		t.Helper()
		text := string(body)
		start, end := strings.Index(text, "<svg "), strings.Index(text, "</svg>")
		if start < 0 || end <= start {
			t.Fatal("HTTP report is missing the real saved-result chart")
		}
		return text[start : end+len("</svg>")]
	}
	checkHTML := func(body []byte, originalChart string) {
		t.Helper()
		text := string(body)
		for _, expected := range []string{"<td>好学校</td><td>2</td><td>15000</td>", "<td>转介绍</td><td>1</td><td>4000</td>", "第1至1行", "本报告展示 2 行", full.ID, run.ID} {
			if !strings.Contains(text, expected) {
				t.Fatalf("HTML lost saved data/coverage %q", expected)
			}
		}
		for _, changed := range []string{"自己上门的", "<td>34000</td>", "<td>5000</td>"} {
			if strings.Contains(text, changed) {
				t.Fatalf("current business data leaked into historical report: %q", changed)
			}
		}
		chart := chartOf(body)
		if !strings.Contains(chart, "好学校") || strings.Contains(chart, "转介绍") || (originalChart != "" && chart != originalChart) {
			t.Fatal("report chart changed its original saved values or fixed one-row range")
		}
	}
	checkSavedAPI()
	initialHTML := createAndDownload("html")
	checkHTML(initialHTML, "")
	originalChart := chartOf(initialHTML)
	checkXLSX(createAndDownload("xlsx"))

	// Commit real writes to the business table, after the original snapshot was
	// destroyed. Both the aggregate values and the source set must change.
	if err := f.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&repo.LeadModel{}).Where("id=?", 701).Updates(map[string]any{"source_channel": "自己上门的", "deal_amount": 999, "refund_amount": 9, "updated_at": stamp.Add(time.Hour)}).Error; err != nil {
			return err
		}
		return tx.Create(&repo.LeadModel{ID: 704, LeadNo: "IMMUTABLE-704", CustomerName: "新增隔离客户", SourceChannel: "转介绍", Status: "completed", BusinessStage: "closed", OwnershipState: "private", ResultStatus: "won", DealAmount: 300, PaymentStatus: "paid", CreatedAt: stamp.Add(time.Hour), UpdatedAt: stamp.Add(time.Hour)}).Error
	}); err != nil {
		t.Fatal(err)
	}
	currentSnapshot, err := BuildSnapshot(t.Context(), f.db, f.service.cfg.Directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = currentSnapshot.Close() })
	current, err := currentSnapshot.Query(t.Context(), query, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	wantCurrent := [][]any{{"好学校", int64(1), int64(5000)}, {"自己上门的", int64(1), int64(99000)}, {"转介绍", int64(2), int64(34000)}}
	if !reflect.DeepEqual(current.Rows, wantCurrent) || !current.SnapshotAt.After(full.SnapshotAt) {
		t.Fatalf("new snapshot did not independently observe committed business changes: %+v", current)
	}
	if err := currentSnapshot.Close(); err != nil {
		t.Fatal(err)
	}
	checkSavedAPI()
	checkHTML(createAndDownload("html"), originalChart)
	checkXLSX(createAndDownload("xlsx"))

	// Expire all previous indexes and physically remove all generated files.
	// A later export cannot succeed by serving an earlier byte cache.
	for _, exported := range cache {
		if err := f.db.Model(&ExportRecord{}).Where("id=?", exported.ID).Update("expires_at", time.Now().UTC().Add(-time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
		old := securityRequest(t, f, http.MethodGet, "/api/agent/exports/"+exported.ID+"/download", nil, securityAdmin)
		if old.Code != http.StatusGone {
			t.Fatal("expired export still served")
		}
		if err := os.Remove(exported.Path); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(exported.Path); !os.IsNotExist(err) {
			t.Fatal("old export cache still exists")
		}
	}
	checkSavedAPI()
	checkHTML(createAndDownload("html"), originalChart)
	checkXLSX(createAndDownload("xlsx"))
	if modelCalls.Load() != 0 {
		t.Fatal("historical export unexpectedly called the model")
	}
}
