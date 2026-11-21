package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
)

func repairFixtureReply(w http.ResponseWriter, call ToolCall, reasoning string) {
	repairFixtureReplyWithContent(w, call, reasoning, "")
}

func repairFixtureReplyWithContent(w http.ResponseWriter, call ToolCall, reasoning, content string) {
	w.Header().Set("Content-Type", "text/event-stream")
	delta := map[string]any{"content": content, "reasoning_content": reasoning, "tool_calls": []any{map[string]any{"index": 0, "id": call.ID, "type": "function", "function": call.Function}}}
	raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
	fmt.Fprintf(w, "data: %s\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":30,\"completion_tokens\":50}}\n\ndata: [DONE]\n\n", raw)
}

func TestNumericCorrectionProvidesRevalidatedProposalAcrossCheckpoint(t *testing.T) {
	for _, checkpoint := range []bool{false, true} {
		t.Run(fmt.Sprint(checkpoint), func(t *testing.T) {
			var calls atomic.Int32
			var original FinalizeRequest
			f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
				var req ModelRequest
				_ = json.NewDecoder(r.Body).Decode(&req)
				switch calls.Add(1) {
				case 1:
					repairFixtureReply(w, finalResultTool("query_data", map[string]any{"sql": "SELECT 3 AS checked_count"}), "Check evidence.")
				case 2:
					var id string
					for _, m := range req.Messages {
						var result Result
						if m.Role == "tool" && json.Unmarshal([]byte(m.Content), &result) == nil && result.ID != "" {
							id = result.ID
						}
					}
					original = FinalizeRequest{ResultIDs: []string{id}, TableResultIDs: []string{id}, Markdown: "当前为两个；历史记录为两条，缺失为0。", References: []AnswerReference{{Token: "n", ResultID: id, Row: intPointer(0), Column: "checked_count", Level: "cell"}}}
					content := ""
					if checkpoint {
						content = "UNVERIFIED_DRAFT_CAPACITY_" + strings.Repeat("x", 41000)
					}
					repairFixtureReplyWithContent(w, finalResultTool("finalize_answer", original), "REPAIR_PRIVATE_REASONING_MARKER", content)
				case 3:
					var proposal struct {
						ProposalID string          `json:"proposal_id"`
						Request    FinalizeRequest `json:"finalize_answer"`
						Withheld   string          `json:"withheld_markdown"`
					}
					found := false
					for _, m := range req.Messages {
						var feedback struct {
							Repair json.RawMessage `json:"repair_proposal"`
						}
						if m.Role == "tool" && json.Unmarshal([]byte(m.Content), &feedback) == nil && len(feedback.Repair) > 0 {
							found = json.Unmarshal(feedback.Repair, &proposal) == nil
						}
						const marker = "待确认修复提案JSON：\n"
						if i := strings.Index(m.Content, marker); i >= 0 {
							found = json.Unmarshal([]byte(m.Content[i+len(marker):]), &proposal) == nil
						}
					}
					if !found {
						t.Error("correction has no concrete revalidated proposal")
						finalResultFixtureReply(w, "仍有两个和两条。")
						return
					}
					if !reflect.DeepEqual(proposal.Request.References, original.References) || !reflect.DeepEqual(proposal.Request.ResultIDs, original.ResultIDs) || !reflect.DeepEqual(proposal.Request.TableResultIDs, original.TableResultIDs) || proposal.Withheld != norm.NFKC.String(original.Markdown) || !strings.Contains(proposal.Request.Markdown, "待核对") {
						t.Errorf("repair lost original claims, references or result selection: request=%+v original=%+v withheld=%q", proposal.Request, original, proposal.Withheld)
					}
					if checkpoint {
						raw, _ := json.Marshal(req)
						if strings.Contains(string(raw), "REPAIR_PRIVATE_REASONING_MARKER") || !strings.Contains(req.Messages[0].Content, "纠正已使用=true") {
							t.Error("checkpoint leaked reasoning or reset correction")
						}
					}
					repairFixtureReply(w, finalResultTool("accept_answer_repair", map[string]any{"proposal_id": proposal.ProposalID}), "Submit after review.")
				default:
					t.Error("extra correction model call")
					w.WriteHeader(500)
				}
			})
			f.service.cfg.MaxCalls = 3
			if err := f.db.Model(&Settings{}).Where("id=1").Update("model", "deepseek-v4-pro").Error; err != nil {
				t.Fatal(err)
			}
			audits := make(chan AnswerValidationAuditRecord, 2)
			f.service.cfg.AnswerValidationAudit = func(record AnswerValidationAuditRecord) { audits <- record }
			c := securityCreateConversation(t, f, securityAdmin)
			run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "核对查询数据", uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			finished := securityWaitRun(t, f, run.ID)
			if finished.Status != "completed" || calls.Load() != 3 {
				t.Fatalf("repair failed: %+v calls=%d", finished, calls.Load())
			}
			if len(audits) != 2 {
				t.Fatalf("missing failed/final diagnostic pair: %d", len(audits))
			}
			first, last := <-audits, <-audits
			if first.Status != "rejected" || last.Status != "accepted" || !strings.Contains(first.ValidationError, "两个") || !strings.Contains(first.ValidationError, "两条") || !strings.Contains(first.Arguments, "缺失") || strings.Contains(first.Arguments+last.Arguments, "REPAIR_PRIVATE_REASONING_MARKER") {
				t.Fatalf("invalid audit pairing: %+v %+v", first, last)
			}
			var checkpoints int64
			_ = f.db.Model(&Event{}).Where("run_id=? AND type='context_checkpoint'", run.ID).Count(&checkpoints).Error
			if (checkpoint && checkpoints != 1) || (!checkpoint && checkpoints != 0) {
				t.Fatalf("test did not execute intended checkpoint path: %d", checkpoints)
			}
		})
	}
}

func TestAnswerRepairRequiresValidEvidenceConditionsAndCompletePriorNarrative(t *testing.T) {
	snapshot := measuredFixture(t)
	r, err := snapshot.Query(t.Context(), "SELECT 3 AS checked_count", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	results := map[string]*Result{r.ID: r}
	reads := newEvidenceReadState()
	page, _ := modelResultPage(r, 0, 20)
	reads.observe(page)
	condition := AnalysisCondition{Kind: "source", Text: "好学校", SourceMessageID: "user-message"}
	history := []Message{{ID: "user-message", RunID: "current", Role: "user", Content: "来源好学校"}}
	original := FinalizeRequest{ResultIDs: []string{r.ID}, TableResultIDs: []string{r.ID}, Markdown: "有两条。", References: []AnswerReference{{ResultID: r.ID, Row: intPointer(0), Column: "checked_count", Level: "cell"}}, Conditions: []AnalysisCondition{condition}}
	proposal, err := buildAnswerRepair(original, nil, results, reads, AnalysisContext{}, history, snapshot, "current")
	if err != nil || proposal == nil || !reflect.DeepEqual(proposal.Request.Conditions, original.Conditions) || !reflect.DeepEqual(proposal.Request.References, original.References) {
		t.Fatalf("valid complete proposal unavailable: %+v %v", proposal, err)
	}
	for _, test := range []struct {
		name string
		edit func(*FinalizeRequest)
	}{
		{"forged result", func(p *FinalizeRequest) { p.ResultIDs = []string{"missing"} }},
		{"unread row", func(p *FinalizeRequest) { p.References[0].Row = intPointer(9) }},
		{"forged condition", func(p *FinalizeRequest) { p.Conditions[0].Text = "转介绍" }},
		{"duplicate token", func(p *FinalizeRequest) {
			p.References[0].Token = "a"
			p.References = append(p.References, p.References[0])
		}},
		{"oversized original", func(p *FinalizeRequest) { p.Markdown += strings.Repeat("原拟解释。", 1000) }},
		{"no references", func(p *FinalizeRequest) { p.References = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, _ := json.Marshal(original)
			var invalid FinalizeRequest
			_ = json.Unmarshal(raw, &invalid)
			test.edit(&invalid)
			if repair, err := buildAnswerRepair(invalid, nil, results, reads, AnalysisContext{}, history, snapshot, "current"); err == nil || repair != nil {
				t.Fatalf("unsafe proposal generated: %+v %v", repair, err)
			}
		})
	}
}

func TestAnswerRepairCannotPublishWithoutExplicitValidResubmission(t *testing.T) {
	for _, second := range []string{"plain text", "repeated invalid finalize"} {
		t.Run(second, func(t *testing.T) {
			var calls atomic.Int32
			var resultID string
			f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
				var req ModelRequest
				_ = json.NewDecoder(r.Body).Decode(&req)
				for _, m := range req.Messages {
					var result Result
					if m.Role == "tool" && json.Unmarshal([]byte(m.Content), &result) == nil && result.ID != "" {
						resultID = result.ID
					}
				}
				switch calls.Add(1) {
				case 1:
					finalResultFixtureReply(w, "", finalResultTool("query_data", map[string]any{"sql": "SELECT 3 AS checked_count"}))
				case 2:
					finalResultFixtureReply(w, "", finalResultTool("finalize_answer", FinalizeRequest{ResultIDs: []string{resultID}, Markdown: "有两条。", References: []AnswerReference{{ResultID: resultID, Row: intPointer(0), Column: "checked_count", Level: "cell"}}}))
				case 3:
					if second == "plain text" {
						finalResultFixtureReply(w, "我确认修复提案。")
					} else {
						finalResultFixtureReply(w, "", finalResultTool("finalize_answer", FinalizeRequest{ResultIDs: []string{resultID}, Markdown: "仍有两个。"}))
					}
				default:
					t.Error("correction limit reset")
					w.WriteHeader(500)
				}
			})
			c := securityCreateConversation(t, f, securityAdmin)
			run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "核对查询数据", uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			finished := securityWaitRun(t, f, run.ID)
			if finished.Status == "completed" || calls.Load() != 3 {
				t.Fatalf("proposal became completed without valid explicit finalization: %+v calls=%d", finished, calls.Load())
			}
			var m Message
			_ = f.db.Where("run_id=? AND role='assistant'", run.ID).First(&m).Error
			if m.Content != finished.Error || m.BlocksJSON != "[]" {
				t.Fatal("unvalidated proposal published")
			}
		})
	}
}
