package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Reproduce the live rejection: even after a concrete repair proposal, an
// unconstrained finalize tool simply repeats the same bare zero in prose.
func TestRepairAcceptanceAvoidsRepeatingRejectedNumericProposal(t *testing.T) {
	var calls atomic.Int32
	var original FinalizeRequest
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var req ModelRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch calls.Add(1) {
		case 1:
			finalResultFixtureReply(w, "", finalResultTool("query_data", map[string]any{"sql": "SELECT 1 AS current_followup_eligible, 1 AS current_followup_overdue"}))
		case 2:
			var id string
			for _, m := range req.Messages {
				var result Result
				if m.Role == "tool" && json.Unmarshal([]byte(m.Content), &result) == nil && result.ID != "" {
					id = result.ID
				}
			}
			original = FinalizeRequest{ResultIDs: []string{id}, Markdown: "当前资格与逾期标志均为 0。", References: []AnswerReference{{ResultID: id, Row: intPointer(0), Column: "current_followup_eligible", Level: "cell"}, {ResultID: id, Row: intPointer(0), Column: "current_followup_overdue", Level: "cell"}}}
			finalResultFixtureReply(w, "未核验的中途正文", finalResultTool("finalize_answer", original))
		case 3:
			var proposalID string
			for _, tool := range req.Tools {
				item := tool.(map[string]any)["function"].(map[string]any)
				if item["name"] == "accept_answer_repair" {
					props := item["parameters"].(map[string]any)["properties"].(map[string]any)
					proposalID = props["proposal_id"].(map[string]any)["enum"].([]any)[0].(string)
				}
			}
			if proposalID == "" {
				finalResultFixtureReply(w, "", finalResultTool("finalize_answer", original))
				return
			}
			if len(req.Tools) != 1 {
				t.Error("repair mode still offers unconstrained final rewrite")
			}
			finalResultFixtureReply(w, "不会作为正文发布的草稿", finalResultTool("accept_answer_repair", map[string]any{"proposal_id": proposalID, "explanation": "当前不符合普通回访资格。"}))
		default:
			t.Error("repair added an extra call")
			w.WriteHeader(500)
		}
	})
	f.service.cfg.MaxCalls = 3
	c := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "核对当前回访资格", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	done := securityWaitRun(t, f, run.ID)
	if done.Status != "completed" || calls.Load() != 3 {
		t.Fatalf("explicit repair did not complete: %+v calls=%d", done, calls.Load())
	}
	var answer Message
	f.db.Where("run_id=? AND role='assistant'", run.ID).First(&answer)
	if strings.Contains(answer.Content, "均为 0") || strings.Contains(answer.Content, "草稿") || !strings.Contains(answer.Content, "current_followup_eligible：1") {
		t.Fatal("withheld or missing bound answer")
	}
	var reservations int64
	f.db.Model(&BudgetReservation{}).Where("run_id=?", run.ID).Count(&reservations)
	if reservations != 3 {
		t.Fatal("repair changed charge count")
	}
}

func repairIDFromTools(t *testing.T, request ModelRequest) string {
	t.Helper()
	for _, item := range request.Tools {
		fn := item.(map[string]any)["function"].(map[string]any)
		if fn["name"] == "accept_answer_repair" {
			return fn["parameters"].(map[string]any)["properties"].(map[string]any)["proposal_id"].(map[string]any)["enum"].([]any)[0].(string)
		}
	}
	t.Error("missing repair-only schema")
	return ""
}

func TestAnswerRepairGrantBindsEvidenceAndRejectsChangedState(t *testing.T) {
	for _, scenario := range []string{"valid", "forged id", "cross run", "changed result", "changed snapshot", "changed selection", "changed condition", "changed read value", "unread", "unknown field", "two JSON values", "unbound number", "unbound time", "unbound scope", "private explanation", "cancelled", "closed snapshot", "reused grant"} {
		t.Run(scenario, func(t *testing.T) {
			snapshot := measuredFixture(t)
			result, err := snapshot.Query(t.Context(), "SELECT 7 AS checked_count", nil, 20)
			if err != nil {
				t.Fatal(err)
			}
			results := map[string]*Result{result.ID: result}
			selected := []string{result.ID}
			reads := newEvidenceReadState()
			page, _ := modelResultPage(result, 0, 20)
			reads.observe(page)
			conditions := AnalysisContext{Version: 1}
			runID := "run"
			original := FinalizeRequest{ResultIDs: selected, Markdown: "当前为0。", References: []AnswerReference{{ResultID: result.ID, Row: intPointer(0), Column: "checked_count", Level: "cell"}}}
			proposal, err := buildAnswerRepair(original, selected, results, reads, conditions, nil, snapshot, runID)
			if err != nil || proposal == nil {
				t.Fatal(err)
			}
			grant, err := newAnswerRepairGrant(proposal, selected, results, reads, conditions, nil, snapshot, runID)
			if err != nil {
				t.Fatal(err)
			}
			arguments := map[string]any{"proposal_id": grant.id, "explanation": "按当前口径核对。"}
			ctx := t.Context()
			switch scenario {
			case "forged id":
				arguments["proposal_id"] = uuid.NewString()
			case "cross run":
				runID = "another-run"
			case "changed result":
				result.Rows[0][0] = int64(1)
			case "changed snapshot":
				snapshot.At = snapshot.At.Add(time.Second)
			case "changed selection":
				selected = []string{}
			case "changed condition":
				conditions.Conditions = []AnalysisCondition{{Kind: "source", Text: "另一个渠道"}}
			case "changed read value":
				reads.values[result.ID][0]["checked_count"] = int64(8)
			case "unread":
				delete(reads.cells[result.ID][0], "checked_count")
			case "unknown field":
				arguments["result_ids"] = []string{result.ID}
			case "unbound number":
				arguments["explanation"] = "当前为0。"
			case "unbound time":
				arguments["explanation"] = "预约在快照之后。"
			case "unbound scope":
				arguments["explanation"] = "人员映射无遗漏，记录均能归属。"
			case "private explanation":
				arguments["explanation"] = "电话13800138001"
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "closed snapshot":
				snapshot.Close()
			case "reused grant":
				grant.used = true
			}
			raw, _ := json.Marshal(arguments)
			if scenario == "two JSON values" {
				raw = append(raw, []byte(` {}`)...)
			}
			accepted, err := grant.accept(ctx, string(raw), runID, selected, results, reads, conditions, nil, snapshot)
			if scenario != "valid" {
				if err == nil {
					t.Fatalf("unsafe grant accepted: %s %+v", scenario, accepted)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(accepted.References, original.References) || !reflect.DeepEqual(accepted.ResultIDs, original.ResultIDs) || strings.Contains(accepted.Markdown, "当前为0") {
				t.Fatal("repair changed evidence or included withheld draft")
			}
			if _, err := grant.accept(ctx, string(raw), runID, selected, results, reads, conditions, nil, snapshot); err == nil {
				t.Fatal("grant reused")
			}
			// Public proposal mutations after issuance cannot alter the private request.
			proposal.Request.References[0].Column = "forged"
			if strings.Contains(string(grant.requestJSON), "forged") {
				t.Fatal("grant shares proposal memory")
			}
		})
	}
}

func TestRepairAcceptanceLimitsCancellationAndPartialDraftRemainUnpublished(t *testing.T) {
	for _, scenario := range []string{"call limit", "tool limit", "plain confirmation", "repeated original", "invalid explanation", "injected results", "unrelated tool", "partial failure", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			var calls atomic.Int32
			started := make(chan struct{})
			release := make(chan struct{})
			defer close(release)
			var original FinalizeRequest
			f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
				var req ModelRequest
				_ = json.NewDecoder(r.Body).Decode(&req)
				switch calls.Add(1) {
				case 1:
					finalResultFixtureReply(w, "", finalResultTool("query_data", map[string]any{"sql": "SELECT 7 AS checked_count"}))
				case 2:
					var id string
					for _, m := range req.Messages {
						var result Result
						if m.Role == "tool" && json.Unmarshal([]byte(m.Content), &result) == nil && result.ID != "" {
							id = result.ID
						}
					}
					original = FinalizeRequest{ResultIDs: []string{id}, Markdown: "有0条。", References: []AnswerReference{{ResultID: id, Row: intPointer(0), Column: "checked_count", Level: "cell"}}}
					finalResultFixtureReply(w, "不得发布的中途草稿", finalResultTool("finalize_answer", original))
				case 3:
					id := repairIDFromTools(t, req)
					switch scenario {
					case "plain confirmation":
						finalResultFixtureReply(w, "我确认采用该提案。")
					case "repeated original":
						finalResultFixtureReply(w, "", finalResultTool("finalize_answer", original))
					case "invalid explanation":
						finalResultFixtureReply(w, "", finalResultTool("accept_answer_repair", map[string]any{"proposal_id": id, "explanation": "有0条。"}))
					case "injected results":
						// Keep this protocol-injection case deterministic. Random UUID
						// digits can resemble a private contact and enter the separate
						// privacy-checkpoint path before strict argument validation.
						finalResultFixtureReply(w, "", finalResultTool("accept_answer_repair", map[string]any{"proposal_id": id, "result_ids": []string{"ffffffff-ffff-4fff-bfff-ffffffffffff"}}))
					case "unrelated tool":
						finalResultFixtureReply(w, "", finalResultTool("query_data", map[string]any{"sql": "SELECT 99 AS must_not_execute"}))
					case "partial failure":
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"不得发布的中途草稿\"}}]}\n\ndata: {\"error\":{\"message\":\"synthetic failure\"}}\n\n")
					case "cancel":
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"不得发布的中途草稿\"}}]}\n\n")
						w.(http.Flusher).Flush()
						close(started)
						select {
						case <-r.Context().Done():
						case <-release:
						}
					default:
						t.Error("limit did not stop before paid correction")
						w.WriteHeader(500)
					}
				default:
					t.Error("additional correction call")
					w.WriteHeader(500)
				}
			})
			if scenario == "call limit" {
				f.service.cfg.MaxCalls = 2
			}
			if scenario == "tool limit" {
				f.service.cfg.MaxTools = 2
			}
			c := securityCreateConversation(t, f, securityAdmin)
			run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "核对当前数据", uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "cancel" {
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Fatal("cancel call not started")
				}
				if err = f.service.Cancel(t.Context(), securityAdmin, run.ID); err != nil {
					t.Fatal(err)
				}
			}
			done := securityWaitRun(t, f, run.ID)
			wantCalls := int32(3)
			if scenario == "call limit" || scenario == "tool limit" {
				wantCalls = 2
			}
			if done.Status == "completed" || calls.Load() != wantCalls {
				t.Fatalf("unexpected completion/calls: %+v %d", done, calls.Load())
			}
			if scenario == "cancel" && done.Status != "cancelled" {
				t.Fatalf("cancel status %s", done.Status)
			}
			var message Message
			f.db.Where("run_id=? AND role='assistant'", run.ID).First(&message)
			if message.Content != done.Error || message.BlocksJSON != "[]" {
				t.Fatal("unaccepted/partial draft published")
			}
			var charges, results int64
			f.db.Model(&BudgetReservation{}).Where("run_id=?", run.ID).Count(&charges)
			f.db.Model(&ResultRecord{}).Where("run_id=?", run.ID).Count(&results)
			if charges != int64(wantCalls) || results != 1 {
				t.Fatalf("limits changed: charges=%d results=%d", charges, results)
			}
			var events []Event
			f.db.Where("run_id=?", run.ID).Find(&events)
			for _, e := range events {
				if e.Type == "delta" || strings.Contains(e.Data, "不得发布的中途草稿") {
					t.Fatal("unvalidated draft event")
				}
			}
		})
	}
}

// v27 browser run ad6a46ff (second "好学校" question): the draft was
// semantically correct but was rejected solely for the word “全部”, consuming
// the only correction. Over a complete adopted result it is now accepted with a
// server-authored scope note; over a truncated result it is still rejected.
func TestV27OrdinaryTotalityWordAcceptedWithScopeNote(t *testing.T) {
	snapshot := measuredFixture(t)
	result, err := snapshot.Query(t.Context(), "SELECT 2 AS lead_count", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	selected := []string{result.ID}
	results := map[string]*Result{result.ID: result}
	reads := newEvidenceReadState()
	page, _ := modelResultPage(result, 0, 20)
	reads.observe(page)
	original := FinalizeRequest{ResultIDs: selected, TableResultIDs: selected, Markdown: "按本次明确的“全部历史线索”范围查询，只看好学校。当前负责人和历史操作人分开统计；当前净成交金额不是实收现金。", References: []AnswerReference{{ResultID: result.ID, Row: intPointer(0), Column: "lead_count", Level: "cell"}}}
	out, verification, err := ValidateAnswer(original, selected, results, reads)
	if err != nil || verification == nil {
		t.Fatalf("v27 wording still rejected: %v", err)
	}
	if !strings.Contains(out, "范围说明") || !strings.Contains(out, result.ID) {
		t.Fatalf("missing server scope note: %s", out)
	}
	result.Truncated = true
	if _, _, err := ValidateAnswer(original, selected, results, reads); err == nil || !strings.Contains(err.Error(), "已截断") {
		t.Fatalf("totality over truncated result accepted: %v", err)
	}
	result.Truncated = false
	result.TotalRows = int64(len(result.Rows)) + 5
	if _, _, err := ValidateAnswer(original, selected, results, reads); err == nil {
		t.Fatal("totality over partial rows accepted")
	}
	hard := original
	hard.Markdown = "人员映射无遗漏。"
	result.TotalRows = int64(len(result.Rows))
	if _, _, err := ValidateAnswer(hard, selected, results, reads); err == nil {
		t.Fatal("mapping completeness claim accepted")
	}
}
