package agent

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func TestUnverifiedGlobalDistinctNeverCompletesOrStreams(t *testing.T) {
	var calls atomic.Int64
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var request ModelRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		var id string
		for _, m := range request.Messages {
			if m.Role == "tool" {
				var result Result
				_ = json.Unmarshal([]byte(m.Content), &result)
				if result.ID != "" {
					id = result.ID
				}
			}
		}
		switch calls.Add(1) {
		case 1:
			finalResultFixtureReply(w, "", finalResultTool("query_data", map[string]any{"sql": "SELECT sales_user_id, COUNT(*) AS followups, COUNT(DISTINCT lead_id) AS leads FROM lead_followups GROUP BY sales_user_id"}))
		case 2:
			finalResultFixtureReply(w, "", finalResultTool("select_final_results", map[string]any{"result_ids": []string{id}}))
		default:
			finalResultFixtureReply(w, "覆盖4条线索，人员不同所以两组互不重叠。")
		}
	})
	conversation := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, conversation.ID, "统计员工跟进", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	if finished.Status == "completed" {
		t.Fatal("unverified group sum was accepted as a final answer")
	}
	var events []Event
	if err := f.db.Where("run_id=?", run.ID).Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if strings.Contains(event.Data, "覆盖4条") {
			t.Fatal("unverified answer escaped through SSE or persisted message")
		}
	}
}

func TestModelDictionaryIsBoundedBeforeAnotherCall(t *testing.T) {
	var calls atomic.Int64
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var request ModelRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		if calls.Add(1) == 1 {
			finalResultFixtureReply(w, "", finalResultTool("describe_data", map[string]any{}))
			return
		}
		for _, m := range request.Messages {
			if m.Role == "tool" && len([]byte(m.Content)) > 8<<10 {
				t.Errorf("dictionary response exceeds model page: %d bytes", len([]byte(m.Content)))
			}
		}
		finalResultFixtureReply(w, "本轮未查询业务数字。")
	})
	conversation := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, conversation.ID, "了解数据", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	securityWaitRun(t, f, run.ID)
}
