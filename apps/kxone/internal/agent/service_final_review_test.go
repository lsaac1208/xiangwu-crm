package agent

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func TestFinalSelectionCorrectionIsBoundedAndEveryCallIsCharged(t *testing.T) {
	for _, maximum := range []int{2, 8} {
		t.Run(string(rune('0'+maximum)), func(t *testing.T) {
			var calls atomic.Int64
			f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					finalResultFixtureReply(w, "", finalResultTool("query_data", map[string]any{"sql": "SELECT COUNT(*) AS n FROM leads"}))
					return
				}
				finalResultFixtureReply(w, "没有选择依据的回答。")
			})
			f.service.cfg.MaxCalls = maximum
			conversation := securityCreateConversation(t, f, securityAdmin)
			run, err := f.service.Start(t.Context(), securityAdmin, conversation.ID, "核对数字", uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			finished := securityWaitRun(t, f, run.ID)
			wantCalls := int64(3) // Query, unaccepted answer, one correction attempt.
			if maximum == 2 {
				wantCalls = 2
			}
			if calls.Load() != wantCalls || finished.Status == "completed" {
				t.Fatalf("selection correction bypassed existing limits: calls=%d run=%+v", calls.Load(), finished)
			}
			var charges []BudgetReservation
			if err := f.db.Where("run_id=?", run.ID).Find(&charges).Error; err != nil || int64(len(charges)) != wantCalls {
				t.Fatalf("correction calls were not accounted individually: charges=%+v err=%v", charges, err)
			}
			for _, charge := range charges {
				if charge.Status != "settled" || charge.ActualMicro <= 0 || charge.AmountMicro < charge.ActualMicro || charge.InputTokens != 10 || charge.OutputTokens != 10 {
					t.Fatalf("invalid per-call ledger: %+v", charge)
				}
			}
		})
	}
}

func TestFinalSelectionMustBeRenewedAfterAnotherSuccessfulQuery(t *testing.T) {
	var calls atomic.Int64
	var oldID, newID string
	var sawReminder atomic.Bool
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var request ModelRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		for _, message := range request.Messages {
			if message.Role == "system" && strings.Contains(message.Content, "本次回答未被接受") {
				sawReminder.Store(true)
			}
			if message.Role == "tool" {
				var result Result
				if json.Unmarshal([]byte(message.Content), &result) == nil && result.ID != "" {
					if len(result.Columns) > 0 && result.Columns[0] == "old_value" {
						oldID = result.ID
					} else if len(result.Columns) > 0 && result.Columns[0] == "new_value" {
						newID = result.ID
					}
				}
			}
		}
		switch calls.Add(1) {
		case 1:
			finalResultFixtureReply(w, "", finalResultTool("query_data", map[string]any{"sql": "SELECT 2 AS old_value"}))
		case 2:
			finalResultFixtureReply(w, "", finalResultTool("select_final_results", map[string]any{"result_ids": []string{oldID}}), finalResultTool("query_data", map[string]any{"sql": "SELECT 3 AS new_value"}))
		case 3:
			finalResultFixtureReply(w, "继续查询后尚未重新确认的回答。")
		case 4:
			finalResultFixtureReply(w, "", finalResultTool("select_final_results", map[string]any{"result_ids": []string{newID}}))
		default:
			finalResultFixtureReply(w, "", finalResultTool("finalize_answer", map[string]any{"markdown": "已重新核对。\n\n{{value}}。", "references": []AnswerReference{{Token: "value", ResultID: newID, Row: intPointer(0), Column: "new_value", Level: "cell"}}}))
		}
	})
	conversation := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, conversation.ID, "先查询再复核", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	if finished.Status != "completed" || calls.Load() != 5 || !sawReminder.Load() {
		t.Fatalf("new query did not invalidate the old choice: %+v calls=%d reminder=%v", finished, calls.Load(), sawReminder.Load())
	}
	var message Message
	if err := f.db.Where("run_id=? AND role='assistant'", run.ID).First(&message).Error; err != nil {
		t.Fatal(err)
	}
	var blocks []Block
	if err := json.Unmarshal([]byte(message.BlocksJSON), &blocks); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, block := range blocks {
		if block.Type == "sources" {
			found = len(block.ResultIDs) == 1 && block.ResultIDs[0] == newID && block.ResultIDs[0] != oldID
		}
	}
	if !found || strings.Contains(message.Content, "尚未重新确认") {
		t.Fatalf("stored final answer retained stale selection: %+v", message)
	}
}
