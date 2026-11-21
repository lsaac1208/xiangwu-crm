package agent

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestFinalAnswerRejectsUnboundSnapshotTemporalClaim(t *testing.T) {
	r := &Result{ID: "appointment", SnapshotAt: time.Date(2026, 10, 13, 6, 0, 0, 0, time.UTC), Columns: []string{"status", "start_at_date_shanghai"}, Rows: [][]any{{"scheduled", "2026-10-09"}}}
	ref := AnswerReference{ResultID: r.ID, Row: intPointer(0), Column: "start_at_date_shanghai", Level: "cell"}
	for _, text := range []string{
		"该线索当前存在有效预约（scheduled,计划到访日期在快照之后），因此不产生普通回访待办。",
		"预约时间还没到，所以有有效预约。",
		"到访日期晚于当前时间。",
		"预约时间早于本次快照。",
		"scheduled不代表未来，但计划日期在快照之后。",
		"有效预约安排在将来。",
		"计划到访日期晚于本轮数据时点。",
	} {
		_, _, err := ValidateAnswer(FinalizeRequest{Markdown: text, References: []AnswerReference{ref}}, []string{r.ID}, map[string]*Result{r.ID: r})
		if err == nil || !strings.Contains(err.Error(), "时间") {
			t.Errorf("unbound relative time claim accepted: %q: %v", text, err)
		}
	}
}

func TestTimeCorrectionUsesOriginalSingleOpportunityAndPersistsComparison(t *testing.T) {
	var calls atomic.Int32
	var resultID string
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var request ModelRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		for _, message := range request.Messages {
			var result Result
			if message.Role == "tool" && json.Unmarshal([]byte(message.Content), &result) == nil && result.ID != "" {
				resultID = result.ID
			}
		}
		switch calls.Add(1) {
		case 1:
			finalResultFixtureReply(w, "", finalResultTool("query_data", map[string]any{"sql": "SELECT '2000-01-01' AS registered_date"}))
		case 2:
			finalResultFixtureReply(w, "", finalResultTool("finalize_answer", FinalizeRequest{ResultIDs: []string{resultID}, Markdown: "登记日期在快照之后。", References: []AnswerReference{{ResultID: resultID, Row: intPointer(0), Column: "registered_date", Level: "cell", Comparison: "snapshot"}}}))
		case 3:
			for _, message := range request.Messages {
				var feedback struct {
					Repair *answerRepairProposal `json:"repair_proposal"`
				}
				if message.Role == "tool" && json.Unmarshal([]byte(message.Content), &feedback) == nil && feedback.Repair != nil {
					if feedback.Repair.Request.References[0].Comparison != "snapshot" || !strings.Contains(feedback.Repair.WithheldMarkdown, "快照之后") {
						t.Error("repair lost comparison or original rejected claim")
					}
					// Explicit resubmission remains mandatory; the discarded claim
					// is not automatically replaced or published by the service.
					feedback.Repair.Request.Markdown = "日期与业务状态分别核对。"
					finalResultFixtureReply(w, "", finalResultTool("accept_answer_repair", map[string]any{"proposal_id": feedback.Repair.ProposalID, "explanation": feedback.Repair.Request.Markdown}))
					return
				}
			}
			t.Error("missing single-opportunity time correction")
			w.WriteHeader(500)
		default:
			t.Error("extra correction call")
			w.WriteHeader(500)
		}
	})
	f.service.cfg.MaxCalls = 3
	conversation := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, conversation.ID, "核对登记时间", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	if finished.Status != "completed" || calls.Load() != 3 {
		t.Fatalf("time recovery: %+v calls=%d", finished, calls.Load())
	}
	var answer Message
	if err := f.db.Where("run_id=? AND role='assistant'", run.ID).First(&answer).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer.Content, "早于快照的上海日期") || strings.Contains(answer.Content, "登记日期在快照之后") || !strings.Contains(answer.BlocksJSON, `"comparison":"snapshot"`) || !strings.Contains(answer.BlocksJSON, `"version":6`) {
		t.Fatalf("unverified temporal claim published or comparison lost: %s", answer.Content)
	}
	var events []Event
	if err := f.db.Where("run_id=? AND type IN ('message','delta')", run.ID).Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if strings.Contains(event.Data, "登记日期在快照之后") {
			t.Fatal("rejected temporal draft entered SSE")
		}
	}
}

func TestTemporalNarrativeDoesNotConfuseAttendanceWithClockTime(t *testing.T) {
	for _, text := range []string{"预约未到访。", "预约尚未到场。", "scheduled不代表未来。", "计划时间过去不会自动失效。"} {
		if err := validateNarrative(text); err != nil {
			t.Errorf("business-state/rule statement confused with relative time: %s: %v", text, err)
		}
	}
}

func TestSnapshotComparisonUsesActualInstantAndShanghaiDatePrecision(t *testing.T) {
	snapshot := time.Date(2026, 10, 7, 16, 5, 0, 0, time.UTC)
	for _, tc := range []struct{ value, want string }{
		{"2026-10-08T00:05:00+08:00", "同一时刻"},
		{"2026-10-07T16:04:59.999999999Z", "早于本次快照时间"},
		{"2026-10-08T00:05:00.000000001+08:00", "晚于本次快照时间"},
		{"2026-10-07", "早于快照的上海日期"},
		{"2026-10-08", "无法判断具体时刻先后"},
		{"2026-10-09", "晚于快照的上海日期"},
	} {
		r := &Result{ID: "r", SnapshotAt: snapshot, Columns: []string{"value"}, Rows: [][]any{{tc.value}}}
		req := FinalizeRequest{Markdown: "日期与业务状态分别核对。", References: []AnswerReference{{ResultID: r.ID, Row: intPointer(0), Column: "value", Level: "cell", Comparison: "snapshot"}}}
		out, verification, err := ValidateAnswer(req, []string{r.ID}, map[string]*Result{r.ID: r})
		if err != nil || !strings.Contains(out, tc.want) || !strings.Contains(out, "本轮快照上海时间=2026-10-08T00:05:00+08:00") || !strings.Contains(out, "不据此推断业务状态") {
			t.Fatalf("%s: %s %v", tc.value, out, err)
		}
		encoded, _ := json.Marshal(verification)
		if verification.Version != 6 || !strings.Contains(string(encoded), `"comparison":"snapshot"`) {
			t.Fatalf("comparison provenance missing: %s", encoded)
		}
	}
}

func TestSnapshotComparisonRejectsMissingAmbiguousOrUnownedEvidence(t *testing.T) {
	snapshot := time.Date(2026, 10, 13, 6, 0, 0, 0, time.UTC)
	for _, value := range []any{nil, "", 1791871200, true, "2026-10-13 06:00:00", "2026-10-13T06:00:00", "2026-02-30", "2026-10-13T6:00:00Z", "2026-10-13T06:00:00+24:00", "2026-10-13T06:00:00+08:60"} {
		if _, err := compareCellToSnapshot(value, snapshot); err == nil {
			t.Errorf("unsafe time accepted: %v", value)
		}
	}
	if _, err := compareCellToSnapshot("2026-10-09", time.Time{}); err == nil {
		t.Fatal("missing snapshot accepted")
	}
	r := &Result{ID: "r", SnapshotAt: snapshot, Columns: []string{"when"}, Rows: [][]any{{"2026-10-09"}}}
	ref := AnswerReference{ResultID: r.ID, Row: intPointer(0), Column: "when", Level: "cell", Comparison: "snapshot"}
	req := FinalizeRequest{Markdown: "已核对日期。", References: []AnswerReference{ref}}
	if _, _, err := ValidateAnswer(req, []string{r.ID}, map[string]*Result{r.ID: r}, newEvidenceReadState()); err == nil {
		t.Fatal("unread time compared")
	}
	if _, _, err := ValidateAnswer(req, []string{}, map[string]*Result{r.ID: r}); err == nil {
		t.Fatal("discarded result compared")
	}
	req.References[0].Comparison = "future"
	if _, _, err := ValidateAnswer(req, []string{r.ID}, map[string]*Result{r.ID: r}); err == nil {
		t.Fatal("unrecognized comparison accepted")
	}
	req.References = []AnswerReference{{FactID: "fact", Level: "global", Comparison: "snapshot"}}
	if _, _, err := ValidateAnswer(req, []string{r.ID}, map[string]*Result{r.ID: r}); err == nil || !strings.Contains(err.Error(), "时间比较") {
		t.Fatalf("fact accepted as time: %v", err)
	}
}
