package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

func TestRequestByteBoundaryIsExactAndDoesNotFakeReasoning(t *testing.T) {
	for _, target := range []int{65535, 65536, 65537} {
		r := ModelRequest{Messages: []ModelMessage{{Role: "user", Content: "x"}}}
		base, _ := json.Marshal(r)
		r.Messages[0].Content = strings.Repeat("x", target-len(base)+1)
		body, err := safeModelRequestBody(r, "")
		if target <= modelRequestBytes {
			if err != nil || len(body) != target {
				t.Fatalf("boundary %d rejected: %d %v", target, len(body), err)
			}
		} else if err == nil {
			t.Fatal("oversized request accepted")
		}
	}
	r := ModelRequest{Model: "deepseek-v4-pro", Thinking: &ModelThinking{Type: "enabled"}, Tools: agentTools(), Messages: []ModelMessage{{Role: "assistant", Content: "old answer without native protocol"}}}
	applyProviderPolicy(&r)
	if r.Messages[0].ReasoningContent != nil {
		t.Fatal("missing native reasoning was fabricated")
	}
	if _, err := safeModelRequestBody(r, ""); err == nil {
		t.Fatal("broken thinking history accepted")
	}
}

func TestBoundedHistoryKeepsExplicitConditionsAndQueryEvidence(t *testing.T) {
	s := measuredFixture(t)
	history := []Message{}
	for i := 0; i < 50; i++ {
		history = append(history, Message{ID: fmt.Sprintf("message-%d", i), Role: "user", Content: strings.Repeat("历史背景", 1000), CreatedAt: time.Unix(int64(i), 0)})
	}
	history[0].Content = "只统计2026-09-01到2026-09-30，好学校，员工乙。"
	history[49].Content = "继续按操作人分析。"
	state := AnalysisContext{Version: 1, Conditions: []AnalysisCondition{{Kind: "date", Text: "2026-09-01到2026-09-30", SourceMessageID: "message-0"}, {Kind: "source", Text: "好学校", SourceMessageID: "message-0"}}}
	r, err := s.Query(t.Context(), `SELECT COUNT(*) AS n FROM lead_followups WHERE sales_user_id='sales-a'`, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := buildSegment("rules", history, state, map[string]*Result{r.ID: r}, []string{r.ID}, s)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(msgs)
	if len(raw) > 20<<10 || !strings.Contains(string(raw), "2026-09-01到2026-09-30") || !strings.Contains(string(raw), "sales-a") || !strings.Contains(string(raw), r.ID) {
		t.Fatal("checkpoint grew without bound or lost explicit scope/evidence")
	}
	for _, m := range msgs {
		if m.Role == "assistant" || m.Role == "tool" || m.ReasoningContent != nil {
			t.Fatal("historical context pretended to be native provider history")
		}
	}
	history[49].Content = "来源改为转介绍，日期继续沿用。"
	history[49].RunID = "current"
	updated, err := mergeConditions(state, []AnalysisCondition{{Kind: "source", Text: "转介绍", SourceMessageID: "message-49"}}, history, s, "current")
	if err != nil || len(updated.Conditions) != 2 || updated.Conditions[0].Kind != "date" {
		t.Fatalf("source correction erased date: %+v %v", updated, err)
	}
	if _, err := mergeConditions(state, []AnalysisCondition{{Kind: "date", Text: "2026-01-01", SourceMessageID: "message-49"}}, history, s, "current"); err == nil {
		t.Fatal("invented date accepted")
	}
}

func TestModelResultPagesAndTextSegmentsKeepCoverageHonest(t *testing.T) {
	text := strings.Repeat("费用需要核对。\\\"", 1200)
	r := &Result{ID: "result", Columns: []string{"id", "remark"}, Rows: [][]any{{int64(1), text}}, TotalRows: 1, MaterializedComplete: true}
	page, err := modelResultPage(r, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(page)
	if len(raw) > modelPageBytes || page["text_complete"] != false || len(page["text_segments"].([]map[string]any)) != 1 {
		t.Fatal("long text falsely marked fully read")
	}
	var joined strings.Builder
	offset := 0
	for {
		part, err := boundedTextPage(map[string]any{"id": r.ID}, text, offset)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(part)
		if len(raw) > modelPageBytes || !utf8.ValidString(part["text"].(string)) {
			t.Fatal("text page exceeded capacity or cut UTF8")
		}
		joined.WriteString(part["text"].(string))
		offset = part["next_text_offset"].(int)
		if !part["has_more"].(bool) {
			break
		}
	}
	if joined.String() != text {
		t.Fatal("text segment missing or repeated")
	}
	reads := newEvidenceReadState()
	reads.observe(page)
	if err := reads.validate([]AnswerReference{{Token: "value", ResultID: r.ID, Row: intPointer(0), Column: "remark"}}); err == nil {
		t.Fatal("truncated cell accepted as fully read")
	}
	if err := reads.validate([]AnswerReference{{Token: "value", ResultID: r.ID, Row: intPointer(0), Column: "id"}}); err != nil {
		t.Fatal(err)
	}
}

func TestSearchPreviewKeepsAbsoluteSavedRowPositions(t *testing.T) {
	r := &Result{ID: "paged", Columns: []string{"record_id"}, Rows: [][]any{{int64(101)}, {int64(102)}}, TotalRows: 200, Offset: 100}
	page, err := modelResultPage(r, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if page["offset"] != 100 || page["next_offset"] != 102 {
		t.Fatalf("preview shifted saved evidence row positions: %v/%v", page["offset"], page["next_offset"])
	}
	reads := newEvidenceReadState()
	reads.observe(page)
	req := FinalizeRequest{Markdown: "{{record}}", References: []AnswerReference{{Token: "record", ResultID: r.ID, Row: intPointer(100), Column: "record_id", Level: "cell"}}}
	text, _, err := ValidateAnswer(req, []string{r.ID}, map[string]*Result{r.ID: r}, reads)
	if err != nil || !strings.Contains(text, "record_id：101") {
		t.Fatalf("published cell shifted from saved export row: %s %v", text, err)
	}
	if err := reads.validate([]AnswerReference{{Token: "unread", ResultID: r.ID, Row: intPointer(0), Column: "record_id"}}); err == nil {
		t.Fatal("unread earlier page was accepted")
	}
}

func TestContextCheckpointCompletesWithoutRequeryOrPrivateDraft(t *testing.T) {
	const hidden = "PRIVATE_CHAIN_SEGMENT_MARKER"
	var calls atomic.Int64
	var f *agentSecurityFixture
	var resultID string
	f = newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var request ModelRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		body, _ := json.Marshal(request)
		if len(body) > modelRequestBytes {
			t.Error("oversized request sent")
		}
		call := calls.Add(1)
		if call == 1 {
			tool := finalResultTool("query_data", map[string]any{"sql": "SELECT 7 AS checked_value"})
			delta := map[string]any{"reasoning_content": hidden, "content": "覆盖四条线索的待核对草稿" + strings.Repeat("x", 41000), "tool_calls": []any{map[string]any{"index": 0, "id": tool.ID, "type": "function", "function": tool.Function}}}
			raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
			fmt.Fprintf(w, "data: %s\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":30,\"completion_tokens\":50}}\n\ndata: [DONE]\n\n", raw)
			return
		}
		if call == 2 {
			if strings.Contains(string(body), hidden) {
				t.Error("old private native history crossed checkpoint")
			}
			var saved ResultRecord
			if err := f.db.Order("created_at DESC").First(&saved).Error; err != nil {
				t.Error(err)
			}
			resultID = saved.ID
			finalResultFixtureReply(w, "", finalResultTool("select_final_results", map[string]any{"result_ids": []string{resultID}}), finalResultTool("finalize_answer", FinalizeRequest{Markdown: "已核对。\n\n{{value}}。", References: []AnswerReference{{Token: "value", ResultID: resultID, Row: intPointer(0), Column: "checked_value", Level: "cell"}}}))
			return
		}
		t.Error("unexpected model repeat")
		w.WriteHeader(500)
	})
	if err := f.db.Model(&Settings{}).Where("id=1").Update("model", "deepseek-v4-pro").Error; err != nil {
		t.Fatal(err)
	}
	c := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "继续核对本轮数据", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	if finished.Status != "completed" || calls.Load() != 2 {
		t.Fatalf("checkpoint failed %+v calls=%d", finished, calls.Load())
	}
	var events []Event
	_ = f.db.Where("run_id=?", run.ID).Find(&events).Error
	checkpoints := 0
	for _, event := range events {
		if event.Type == "context_checkpoint" {
			checkpoints++
		}
		if event.Type == "delta" || strings.Contains(event.Data, hidden) || strings.Contains(event.Data, "覆盖四条") {
			t.Fatal("unverified/private draft reached event storage")
		}
	}
	if checkpoints != 1 {
		t.Fatalf("checkpoint count %d", checkpoints)
	}
	var charges, queries int64
	_ = f.db.Model(&BudgetReservation{}).Where("run_id=?", run.ID).Count(&charges).Error
	_ = f.db.Model(&ResultRecord{}).Where("run_id=?", run.ID).Count(&queries).Error
	if charges != 2 || queries != 1 {
		t.Fatalf("checkpoint duplicated calls/results: %d %d", charges, queries)
	}
}
