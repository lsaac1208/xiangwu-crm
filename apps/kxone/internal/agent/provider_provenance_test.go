package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The exact UUID came from the failed isolated A13-3 run. Its legitimate
// server-generated suffix matches a telephone pattern; it is not client data.
func TestProviderPrivacyAcceptsProvenMessageIDCondition(t *testing.T) {
	const sourceID = "89b87ec2-f60c-4eb2-97a6-140129812730"
	var calls atomic.Int32
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var request ModelRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		if calls.Add(1) == 1 {
			repairFixtureReply(w, finalResultTool("query_data", map[string]any{"sql": "SELECT 2 AS checked_count"}), "Read evidence.")
			return
		}
		var id string
		for _, m := range request.Messages {
			var result Result
			if m.Role == "tool" && json.Unmarshal([]byte(m.Content), &result) == nil && result.ID != "" {
				id = result.ID
			}
		}
		if id == "" {
			t.Error("result page not received")
		}
		repairFixtureReply(w, finalResultTool("finalize_answer", FinalizeRequest{
			ResultIDs: []string{id}, Markdown: "以下为核对后的查询数据。",
			References: []AnswerReference{{ResultID: id, Row: intPointer(0), Column: "checked_count", Level: "cell"}},
			Conditions: []AnalysisCondition{{Kind: "source", Text: "好学校", SourceMessageID: sourceID}},
		}), "Submit evidence.")
	})
	f.service.cfg.MaxCalls = 2
	c := securityCreateConversation(t, f, securityAdmin)
	oldRun := Run{ID: uuid.NewString(), ConversationID: c.ID, AccountID: securityAdmin.AccountID, AuthVersion: 1, Status: "completed", RequestID: uuid.NewString(), CreatedAt: time.Now().Add(-time.Hour)}
	if err := f.db.Create(&oldRun).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&Message{ID: sourceID, RunID: oldRun.ID, ConversationID: c.ID, Role: "user", Content: "来源好学校", Status: "completed", CreatedAt: oldRun.CreatedAt}).Error; err != nil {
		t.Fatal(err)
	}
	run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "按刚才来源核对数据", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	if finished.Status != "completed" || calls.Load() != 2 {
		t.Fatalf("verified source message was rejected or retried: %+v calls=%d", finished, calls.Load())
	}
	var reservations, rejected int64
	f.db.Model(&BudgetReservation{}).Where("run_id=?", run.ID).Count(&reservations)
	f.db.Model(&Event{}).Where("run_id=? AND type='provider_privacy_rejection'", run.ID).Count(&rejected)
	if reservations != 2 || rejected != 0 {
		t.Fatalf("wrong charging/privacy outcome: reservations=%d rejected=%d", reservations, rejected)
	}
}

func TestProviderPrivacyExtendsOnlyAfterAuthorizedReadHistory(t *testing.T) {
	const oldMessageID = "abcdefab-cdef-4abc-aabc-abcdefabcdef"
	const oldRunID = "22222222-2222-4222-a222-13800138002b"
	var calls atomic.Int32
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		var request ModelRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		raw, _ := json.Marshal(request)
		if calls.Add(1) == 1 {
			if strings.Contains(string(raw), oldMessageID) || strings.Contains(string(raw), oldRunID) {
				t.Error("old source unexpectedly entered initial recent history")
			}
			finalResultFixtureReply(w, "", finalResultTool("query_data", map[string]any{"sql": "SELECT 2 AS checked_count"}), finalResultTool("read_history", map[string]any{"message_id": oldMessageID}))
			return
		}
		var resultID string
		readHistorical := false
		for _, m := range request.Messages {
			if m.Role != "tool" {
				continue
			}
			var page map[string]any
			_ = json.Unmarshal([]byte(m.Content), &page)
			if id, ok := page["id"].(string); ok {
				resultID = id
			}
			readHistorical = readHistorical || page["message_id"] == oldMessageID
		}
		if resultID == "" || !readHistorical {
			t.Error("authorized history or query was not delivered")
		}
		finalResultFixtureReply(w, "Historical run reference: "+oldRunID, finalResultTool("finalize_answer", FinalizeRequest{
			ResultIDs: []string{resultID}, Markdown: "以下为核对后的查询数据。",
			References: []AnswerReference{{ResultID: resultID, Row: intPointer(0), Column: "checked_count", Level: "cell"}},
			Conditions: []AnalysisCondition{{Kind: "source", Text: "好学校", SourceMessageID: oldMessageID}},
		}))
	})
	f.service.cfg.MaxCalls = 2
	c := securityCreateConversation(t, f, securityAdmin)
	old := Run{ID: oldRunID, ConversationID: c.ID, AccountID: 1, Status: "completed", RequestID: uuid.NewString(), CreatedAt: time.Now().Add(-2 * time.Hour)}
	filler := Run{ID: uuid.NewString(), ConversationID: c.ID, AccountID: 1, Status: "completed", RequestID: uuid.NewString(), CreatedAt: time.Now().Add(-time.Hour)}
	for _, run := range []Run{old, filler} {
		if err := f.db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.Create(&Message{ID: oldMessageID, RunID: old.ID, ConversationID: c.ID, Role: "user", Content: "来源好学校", Status: "completed", CreatedAt: old.CreatedAt}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		if err := f.db.Create(&Message{ID: uuid.NewString(), RunID: filler.ID, ConversationID: c.ID, Role: "user", Content: "其他历史问题", Status: "completed", CreatedAt: filler.CreatedAt.Add(time.Duration(i) * time.Second)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "读取原来源条件并核对数据", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	if finished.Status != "completed" || calls.Load() != 2 {
		t.Fatalf("successfully read history was not registered: %+v calls=%d", finished, calls.Load())
	}
	var reservations int64
	f.db.Model(&BudgetReservation{}).Where("run_id=?", run.ID).Count(&reservations)
	if reservations != 2 {
		t.Fatalf("metadata registration caused extra charges: %d", reservations)
	}
}

func TestProviderHistoryExtensionRequiresSuccessfulOwnedRead(t *testing.T) {
	f := newAgentSecurityFixture(t, nil)
	run := Run{ID: uuid.NewString(), ConversationID: uuid.NewString(), AccountID: 1, Status: "running", RequestID: uuid.NewString()}
	oldRun := Run{ID: "aaaaaaaa-aaaa-4aaa-aaaa-13800138001a", ConversationID: run.ConversationID, AccountID: 1, Status: "completed", RequestID: uuid.NewString()}
	foreignRun := Run{ID: uuid.NewString(), ConversationID: uuid.NewString(), AccountID: 1, Status: "completed", RequestID: uuid.NewString()}
	wrongAccountRun := Run{ID: uuid.NewString(), ConversationID: run.ConversationID, AccountID: 3, Status: "completed", RequestID: uuid.NewString()}
	for _, r := range []Run{run, oldRun, foreignRun, wrongAccountRun} {
		if err := f.db.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
	}
	snapshot := &Snapshot{At: time.Now().UTC(), redactor: newRedactor()}
	snapshot.redactor.prepare()
	for _, tc := range []struct {
		name string
		row  Message
		ok   bool
	}{
		{"owned", Message{ID: "bbbbbbbb-bbbb-4bbb-bbbb-13800138002b", RunID: oldRun.ID, ConversationID: run.ConversationID, Role: "user", Status: "completed"}, true},
		{"foreign conversation", Message{ID: uuid.NewString(), RunID: foreignRun.ID, ConversationID: foreignRun.ConversationID, Role: "user", Status: "completed"}, false},
		{"wrong account", Message{ID: uuid.NewString(), RunID: wrongAccountRun.ID, ConversationID: run.ConversationID, Role: "user", Status: "completed"}, false},
		{"missing run", Message{ID: uuid.NewString(), RunID: uuid.NewString(), ConversationID: run.ConversationID, Role: "user", Status: "completed"}, false},
		{"unfinished answer", Message{ID: uuid.NewString(), RunID: oldRun.ID, ConversationID: run.ConversationID, Role: "assistant", Status: "running"}, false},
		{"unsupported role", Message{ID: uuid.NewString(), RunID: oldRun.ID, ConversationID: run.ConversationID, Role: "system", Status: "completed"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := f.db.Create(&tc.row).Error; err != nil {
				t.Fatal(err)
			}
			known := providerProvenance{run.ID: true, run.ConversationID: true}
			before := len(known)
			_, message, err := f.service.readModelHistory(t.Context(), run.ConversationID, tc.row.ID, 0, snapshot)
			if err == nil {
				err = f.service.extendProviderProvenance(t.Context(), known, run, message)
			}
			if (err == nil) != tc.ok {
				t.Fatalf("unexpected history authorization: %v", err)
			}
			if tc.ok {
				if !known[tc.row.ID] || !known[oldRun.ID] || len(providerPrivacyFindingsWithProvenance(snapshot, ModelMessage{Content: tc.row.ID}, known)) != 0 {
					t.Fatal("successfully read phone-shaped history ID not protected")
				}
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				fresh := providerProvenance{run.ID: true, run.ConversationID: true}
				if err := f.service.extendProviderProvenance(ctx, fresh, run, message); err == nil || len(fresh) != before {
					t.Fatal("failed metadata read granted new ID provenance")
				}
			} else if len(known) != before {
				t.Fatal("failed/foreign/unproven history read granted identifier provenance")
			}
		})
	}
}

func TestProviderProvenanceRequiresOwnedStoredMetadata(t *testing.T) {
	f := newAgentSecurityFixture(t, nil)
	const conversationID = "11111111-1111-4111-a111-13800138001a"
	const runID = "22222222-2222-4222-a222-13800138002b"
	const oldRunID = "33333333-3333-4333-a333-13800138003c"
	const recentID = "44444444-4444-4444-a444-13800138004d"
	const oldUserID = "55555555-5555-4555-a555-13800138005e"
	const oldAssistantID = "66666666-6666-4666-a666-13800138006f"
	const foreignID = "77777777-7777-4777-a777-13800138007a"
	const wrongAccountID = "88888888-8888-4888-a888-13800138008b"
	const orphanID = "99999999-9999-4999-a999-13800138009c"
	const requestID = "aaaaaaaa-aaaa-4aaa-aaaa-13800138010d"
	run := Run{ID: runID, ConversationID: conversationID, AccountID: 1, Status: "running", RequestID: requestID}
	oldRun := Run{ID: oldRunID, ConversationID: conversationID, AccountID: 1, Status: "completed", RequestID: uuid.NewString()}
	foreignRun := Run{ID: uuid.NewString(), ConversationID: uuid.NewString(), AccountID: 1, Status: "completed", RequestID: uuid.NewString()}
	wrongAccountRun := Run{ID: uuid.NewString(), ConversationID: conversationID, AccountID: 3, Status: "completed", RequestID: uuid.NewString()}
	for _, r := range []Run{run, oldRun, foreignRun, wrongAccountRun} {
		if err := f.db.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
	}
	recent := Message{ID: recentID, ConversationID: conversationID, RunID: runID, Role: "user", Content: "不得通过读取本文建立隐私豁免"}
	for _, m := range []Message{
		recent,
		{ID: oldUserID, ConversationID: conversationID, RunID: oldRunID, Role: "user"},
		{ID: oldAssistantID, ConversationID: conversationID, RunID: oldRunID, Role: "assistant"},
		{ID: foreignID, ConversationID: foreignRun.ConversationID, RunID: foreignRun.ID, Role: "user"},
		{ID: wrongAccountID, ConversationID: conversationID, RunID: wrongAccountRun.ID, Role: "user"},
		{ID: orphanID, ConversationID: conversationID, RunID: uuid.NewString(), Role: "user"},
	} {
		if err := f.db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}
	conditions := AnalysisContext{}
	for _, id := range []string{oldUserID, oldAssistantID, foreignID, wrongAccountID, orphanID, requestID} {
		conditions.Conditions = append(conditions.Conditions, AnalysisCondition{Kind: "source", Text: "来源", SourceMessageID: id})
	}
	known, err := f.service.loadProviderProvenance(t.Context(), run, []Message{recent}, conditions)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := &Snapshot{At: time.Now().UTC(), redactor: newRedactor()}
	snapshot.redactor.prepare()
	for _, id := range []string{conversationID, runID, oldRunID, recentID, oldUserID} {
		if !known[id] || len(providerPrivacyFindingsWithProvenance(snapshot, ModelMessage{Content: id}, known)) != 0 {
			t.Errorf("owned persisted identifier rejected: %s", id)
		}
	}
	for _, id := range []string{oldAssistantID, foreignID, wrongAccountID, orphanID, requestID} {
		if known[id] || len(providerPrivacyFindingsWithProvenance(snapshot, ModelMessage{Content: id}, known)) == 0 {
			t.Errorf("unproven/incorrectly sourced identifier exempted: %s", id)
		}
	}
	// A recent real assistant message may be quoted as history; it still cannot
	// become a condition source because mergeConditions requires a user record.
	known, err = f.service.loadProviderProvenance(t.Context(), run, []Message{{ID: oldAssistantID, ConversationID: conversationID}}, AnalysisContext{})
	if err != nil || !known[oldAssistantID] {
		t.Fatalf("ordinary same-conversation message provenance lost: %v", err)
	}
	if _, err := mergeConditions(AnalysisContext{}, []AnalysisCondition{{Kind: "other", Text: "条件", SourceMessageID: oldAssistantID}}, []Message{{ID: oldAssistantID, Role: "assistant", Content: "条件"}}, snapshot, runID); err == nil {
		t.Fatal("identifier provenance replaced user-text condition authorization")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if known, err := f.service.loadProviderProvenance(ctx, run, []Message{recent}, conditions); err == nil || known != nil {
		t.Fatal("failed database verification did not fail closed")
	}
	for _, excess := range []struct {
		history    []Message
		conditions AnalysisContext
	}{{history: make([]Message, 13)}, {conditions: AnalysisContext{Conditions: make([]AnalysisCondition, 21)}}} {
		if _, err := f.service.loadProviderProvenance(t.Context(), run, excess.history, excess.conditions); err == nil {
			t.Fatal("unbounded provenance lookup accepted")
		}
	}
}

func TestProviderProvenanceProtectsExactLocalIDsWithoutHidingPII(t *testing.T) {
	const runID = "11111111-1111-4111-a111-13800138001a"
	const resultID = "22222222-2222-4222-a222-13800138002b"
	const factID = "33333333-3333-4333-a333-13800138003c"
	scopeID := strings.Repeat("a", 16) + "13800138004" + strings.Repeat("b", 37)
	selectionID := strings.Repeat("b", 18) + "13800138005" + strings.Repeat("a", 35)
	s := &Snapshot{At: time.Now().UTC(), redactor: newRedactor()}
	s.redactor.prepare()
	r := &Result{ID: resultID, SnapshotAt: s.At, MeasureScope: &MeasureScope{ID: scopeID, SelectionHash: selectionID}, Facts: []MeasuredFact{{ID: factID, ResultID: resultID, ScopeID: scopeID, SnapshotAt: s.At}}}
	results := map[string]*Result{resultID: r}
	known := providerProvenance{runID: true}
	for _, id := range []string{runID, resultID, factID, scopeID, selectionID} {
		thought := "内部依据标识：" + id
		message := ModelMessage{Content: id, ReasoningContent: &thought, ToolCalls: []ToolCall{finalResultTool("finalize_answer", map[string]any{"reference_id": id})}}
		if findings := providerPrivacyFindingsWithProvenance(s, message, known, results); len(findings) != 0 {
			t.Errorf("verified identifier mistaken for PII: %s %+v", id, findings)
		}
		if message.Content != id || *message.ReasoningContent != thought {
			t.Fatal("native response was rewritten")
		}
		for _, text := range []string{id + "x", "x" + id, id + "_unknown", id + " 手机13800138006", id + " 微信 test_parent_9999", id + " 家长姓名：方法"} {
			if len(providerPrivacyFindingsWithProvenance(s, ModelMessage{Content: text}, known, results)) == 0 {
				t.Errorf("unknown identifier extension or real PII bypassed check: %q", text)
			}
		}
		for _, privateKind := range []string{"credential", "name", "contact", "embedded_contact"} {
			redactor := newRedactor()
			switch privateKind {
			case "credential":
				redactor.credentials = []string{id}
			case "name":
				redactor.addName(id, "客户#1")
			case "contact":
				redactor.addContact(id, "客户#1")
			case "embedded_contact":
				phone := phonePattern.FindString(id)
				if phone == "" {
					t.Fatal("test identifier must contain a phone-shaped sequence")
				}
				redactor.addContact(phone, "客户#1")
			}
			redactor.prepare()
			guard := &Snapshot{At: s.At, redactor: redactor}
			if len(providerPrivacyFindingsWithProvenance(guard, message, known, results)) == 0 {
				t.Errorf("identifier shielding bypassed known %s", privateKind)
			}
		}
	}
	for _, text := range []string{scopeID, selectionID, factID, resultID} {
		stale := *r
		stale.SnapshotAt = s.At.Add(-time.Second)
		if len(providerPrivacyFindingsWithProvenance(s, ModelMessage{Content: text}, known, map[string]*Result{resultID: &stale})) == 0 {
			t.Errorf("another snapshot identifier shielded: %s", text)
		}
	}
	unknown := "99999999-9999-4999-a999-13800138009c"
	if len(providerPrivacyFindingsWithProvenance(s, ModelMessage{Content: unknown}, known, results)) == 0 {
		t.Fatal("unknown phone-shaped UUID exempted")
	}
}
