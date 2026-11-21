package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestProviderUUIDPhonePatternProbe(t *testing.T) {
	s := &Snapshot{At: time.Now().UTC(), redactor: newRedactor()}
	s.redactor.prepare()
	const generatedID = "aaaaaaaa-aaaa-4aaa-aaaa-13800138001a"
	if _, err := uuid.Parse(generatedID); err != nil {
		t.Fatal(err)
	}
	safe, _ := s.Sanitize(generatedID)
	if safe == generatedID {
		t.Fatal("probe requires a valid generated UUID whose digits trigger contact detection")
	}
}

func TestProviderUUIDShieldRequiresVerifiedCurrentResultOrFact(t *testing.T) {
	s := &Snapshot{At: time.Now().UTC(), redactor: newRedactor()}
	s.redactor.prepare()
	const resultID = "aaaaaaaa-aaaa-4aaa-aaaa-13800138001a"
	const factID = "bbbbbbbb-bbbb-4bbb-bbbb-13800138002b"
	r := &Result{ID: resultID, SnapshotAt: s.At, MeasureScope: &MeasureScope{ID: "local-scope"}, Facts: []MeasuredFact{{ID: factID, ResultID: resultID, ScopeID: "local-scope", SnapshotAt: s.At}}}
	results := map[string]*Result{resultID: r}
	thought := factID
	message := ModelMessage{Content: resultID, ReasoningContent: &thought, ToolCalls: []ToolCall{finalResultTool("read_result", map[string]any{"result_id": resultID})}}
	if !providerTurnSafe(s, message, results) {
		t.Fatal("verified local identifiers were mistaken for phone numbers")
	}
	if providerTurnSafe(s, message) {
		t.Fatal("arbitrary UUIDs were exempted without current-result proof")
	}
	stale := *r
	stale.SnapshotAt = s.At.Add(-time.Second)
	if providerTurnSafe(s, message, map[string]*Result{resultID: &stale}) {
		t.Fatal("another snapshot's identifiers were shielded")
	}
	r.Facts[0].ScopeID = "forged-scope"
	if providerTurnSafe(s, message, results) {
		t.Fatal("invalid fact ownership was shielded")
	}
	r.Facts[0].ScopeID = "local-scope"
	for _, kind := range []string{"credential", "customer", "contact"} {
		t.Run(kind, func(t *testing.T) {
			redactor := newRedactor()
			switch kind {
			case "credential":
				redactor.credentials = []string{resultID}
			case "customer":
				redactor.addName(resultID, "客户#1")
			case "contact":
				redactor.addContact("13800138001", "客户#1")
			}
			redactor.prepare()
			guard := &Snapshot{At: s.At, redactor: redactor}
			if providerTurnSafe(guard, message, results) {
				t.Fatal("identifier shielding bypassed known private data")
			}
		})
	}
}

func assertPrivateTurnNotPersisted(t *testing.T, f *agentSecurityFixture, runID string, private string) {
	t.Helper()
	var events []Event
	if err := f.db.Where("run_id=?", runID).Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if strings.Contains(event.Data, private) || event.Type == "delta" {
			t.Fatal("unaccepted provider content reached persisted stream events")
		}
		if event.Type == "provider_privacy_rejection" {
			var diagnostic struct {
				Findings []map[string]any `json:"findings"`
			}
			if err := json.Unmarshal([]byte(event.Data), &diagnostic); err != nil || len(diagnostic.Findings) == 0 {
				t.Fatal("privacy rejection is missing bounded diagnostics")
			}
			for _, finding := range diagnostic.Findings {
				if len(finding) != 4 || finding["count"].(float64) < 1 || finding["bytes"].(float64) < 1 {
					t.Fatal("privacy diagnostics include unexpected values")
				}
				if field, ok := finding["field"].(string); !ok || (field != "content" && field != "reasoning" && field != "tool_arguments") {
					t.Fatal("privacy diagnostics include a dynamic field")
				}
				if rule, ok := finding["rule"].(string); !ok || !strings.Contains("|known_identity|known_contact|credential|phone|wechat|email|id_card|unknown_name|oversized|sanitization|", "|"+rule+"|") {
					t.Fatal("privacy diagnostics include a dynamic rule")
				}
			}
		}
	}
	var messages []Message
	if err := f.db.Where("run_id=?", runID).Find(&messages).Error; err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if strings.Contains(message.Content, private) || strings.Contains(message.BlocksJSON, private) {
			t.Fatal("unaccepted provider content reached persisted messages")
		}
	}
	var results []ResultRecord
	if err := f.db.Where("run_id=?", runID).Find(&results).Error; err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if strings.Contains(result.Data, private) {
			t.Fatal("unaccepted provider content reached persisted results")
		}
	}
}

func TestPrivateProviderTurnCannotReachNextRequestOrAnswer(t *testing.T) {
	for _, location := range []string{"plain_answer", "tool_content", "tool_arguments"} {
		for _, kind := range []string{"customer_name", "known_wechat", "fallback_credential"} {
			t.Run(location+"/"+kind, func(t *testing.T) {
				const customer = "陆青岚"
				const wechat = "parent_fixture_alpha"
				const backupKey = "OTHER_OPAQUE_CREDENTIAL_WITHOUT_SECRET_PREFIX"
				private := map[string]string{"customer_name": customer, "known_wechat": wechat, "fallback_credential": backupKey}[kind]
				var calls atomic.Int64
				f := newAgentSecurityFixture(t, func(w http.ResponseWriter, request *http.Request) {
					raw, _ := io.ReadAll(request.Body)
					if strings.Contains(string(raw), private) {
						t.Error("private provider content escaped through the next outbound request")
					}
					if calls.Add(1) == 1 {
						switch location {
						case "plain_answer":
							finalResultFixtureReply(w, "核对内容："+private)
						case "tool_content":
							finalResultFixtureReply(w, "核对内容："+private, finalResultTool("query_data", map[string]any{"sql": "SELECT 99 AS unaccepted_value"}))
						case "tool_arguments":
							finalResultFixtureReply(w, "", finalResultTool("query_data", map[string]any{"sql": "SELECT '" + private + "' AS unaccepted_value"}))
						}
						return
					}
					finalResultFixtureReply(w, "没有可确认的分析结论。")
				})
				createSnapshotLead(t, f.db, 1)
				if err := f.db.Exec("UPDATE leads SET customer_name=?, customer_wechat=? WHERE id=1", customer, wechat).Error; err != nil {
					t.Fatal(err)
				}
				response := securityRequest(t, f, http.MethodPut, "/api/agent/settings", map[string]any{"base_url": f.service.cfg.BaseURL, "model": f.service.cfg.Model, "enabled": true, "budget_micro": 300000000, "expected_version": 1, "fallback": map[string]any{"enabled": true, "base_url": f.model.URL, "model": "deepseek-flash", "api_key": backupKey, "input_price_cny": 2.0, "output_price_cny": 8.0}}, securityAdmin)
				if response.Code != 200 {
					t.Fatal("isolated backup setup failed")
				}
				conversation := securityCreateConversation(t, f, securityAdmin)
				run, err := f.service.Start(t.Context(), securityAdmin, conversation.ID, "核对业务记录", uuid.NewString())
				if err != nil {
					t.Fatal(err)
				}
				finished := securityWaitRun(t, f, run.ID)
				var charges, results, switches int64
				f.db.Model(&BudgetReservation{}).Where("run_id=?", run.ID).Count(&charges)
				f.db.Model(&ResultRecord{}).Where("run_id=?", run.ID).Count(&results)
				f.db.Model(&Event{}).Where("run_id=? AND type='provider_switch'", run.ID).Count(&switches)
				if finished.Status != "completed" || calls.Load() != 2 || charges != 2 || results != 0 || switches != 0 {
					t.Fatalf("private turn accepted or repeated: status=%s error=%s calls=%d charges=%d results=%d switches=%d", finished.Status, finished.Error, calls.Load(), charges, results, switches)
				}
				assertPrivateTurnNotPersisted(t, f, run.ID, private)
				var privacyEvents int64
				f.db.Model(&Event{}).Where("run_id=? AND type='provider_privacy_rejection'", run.ID).Count(&privacyEvents)
				if privacyEvents != 1 {
					t.Fatal("privacy rejection did not record one text-free diagnostic")
				}
			})
		}
	}
}

func TestProviderPrivacyDiagnosticsClassifyWithoutPrivateText(t *testing.T) {
	s := &Snapshot{At: time.Now().UTC(), redactor: newRedactor()}
	s.redactor.addName("陆青岚", "客户#1")
	s.redactor.addContact("parent_fixture_alpha", "客户#1")
	s.redactor.credentials = []string{"OPAQUE_BACKUP_TEST_CREDENTIAL"}
	s.redactor.prepare()
	for _, tc := range []struct{ rule, value string }{
		{"known_identity", "陆青岚"},
		{"known_contact", "parent_fixture_alpha"},
		{"credential", "OPAQUE_BACKUP_TEST_CREDENTIAL"},
		{"credential", "token: untrusted_secret_value"},
		{"phone", "13800138001"},
		{"wechat", "微信：unknown_parent_id"},
		{"email", "parent@example.test"},
		{"unknown_name", "史小明"},
		{"oversized", strings.Repeat("x", 65537)},
	} {
		t.Run(tc.rule+"/"+fmt.Sprint(len(tc.value)), func(t *testing.T) {
			thought := tc.value
			message := ModelMessage{Content: tc.value, ReasoningContent: &thought, ToolCalls: []ToolCall{finalResultTool("query_data", map[string]any{"sql": "SELECT '" + tc.value + "'"})}}
			findings := providerPrivacyFindings(s, message)
			if len(findings) != 3 {
				t.Fatalf("expected one finding per rejected field, got %d", len(findings))
			}
			for i, field := range []string{"content", "reasoning", "tool_arguments"} {
				if findings[i].Field != field || findings[i].Rule != tc.rule || findings[i].Count < 1 || findings[i].Bytes < len(tc.value) {
					t.Fatalf("unexpected diagnostic enum/count: %+v", findings[i])
				}
			}
			serialized, _ := json.Marshal(findings)
			if strings.Contains(string(serialized), tc.value) {
				t.Fatal("privacy diagnosis serialized the matched private value")
			}
		})
	}
}

// This records deterministic ambiguity of the existing heuristics without
// treating ordinary vocabulary as proof that any real provider turn was safe.
func TestProviderPrivacyBusinessLanguageDiagnosticProbe(t *testing.T) {
	s := &Snapshot{At: time.Now().UTC(), redactor: newRedactor()}
	s.redactor.prepare()
	for _, tc := range []struct{ text, rule string }{
		{"token: followups", "credential"},
	} {
		findings := providerPrivacyFindings(s, ModelMessage{Content: tc.text})
		if len(findings) != 1 || findings[0].Rule != tc.rule {
			t.Fatalf("business-language probe changed, review its intended treatment: findings=%+v", findings)
		}
	}
	for _, safe := range []string{"历史记录中的负责人不等于当前负责人。", "范围：当前筛选集合。", "方法：按记录主键去重。", `{"token":"followups"}`, "{{followups}}"} {
		if !providerTurnSafe(s, ModelMessage{Content: safe}) {
			t.Fatal("existing safe business metadata was rejected")
		}
	}
}

func TestProviderPrivacyScopeMethodVocabularyPreservesKnownPrivateData(t *testing.T) {
	for _, word := range []string{"范围", "方法"} {
		t.Run(word, func(t *testing.T) {
			r := newRedactor()
			r.addName(word, "客户#1")
			r.addName("陆青岚", "客户#2")
			r.addContact(word+"_parent_wechat", "客户#3")
			r.credentials = []string{word + "_PRIMARY_OPAQUE_KEY", word + "_FALLBACK_OPAQUE_KEY"}
			r.prepare()
			s := &Snapshot{At: time.Now().UTC(), redactor: r}
			clean, _ := s.Sanitize("家长姓名：" + word)
			if strings.Contains(clean, word) || !strings.Contains(clean, "客户#1") {
				t.Fatal("business vocabulary shielded a registered customer's actual name")
			}
			for _, text := range []string{
				word + "：当前筛选集合。",
				"家长姓名：陆青岚",
				"家长姓名：史小明",
				"微信：" + word + "_parent_wechat",
				word + "_PRIMARY_OPAQUE_KEY",
				word + "_FALLBACK_OPAQUE_KEY",
				"token: followups",
			} {
				if providerTurnSafe(s, ModelMessage{Content: text}) {
					t.Fatal("business vocabulary bypassed a private identity, contact, or credential guard")
				}
			}
		})
	}
	// Test unrelated private values alongside the now-safe headings without a
	// registered vocabulary collision deciding the whole check first.
	r := newRedactor()
	r.addName("陆青岚", "客户#1")
	r.addContact("test_parent_wechat", "客户#1")
	r.credentials = []string{"PRIMARY_OPAQUE_KEY", "FALLBACK_OPAQUE_KEY"}
	r.prepare()
	s := &Snapshot{At: time.Now().UTC(), redactor: r}
	for _, text := range []string{"家长姓名：陆青岚", "家长姓名：史小明", "微信：test_parent_wechat", "PRIMARY_OPAQUE_KEY", "FALLBACK_OPAQUE_KEY", "token: followups"} {
		if providerTurnSafe(s, ModelMessage{Content: "范围：当前筛选集合。\n方法：按记录主键去重。\n" + text}) {
			t.Fatal("safe scope/method headings concealed another private value")
		}
	}
}

func TestPrivacyRejectionCannotRestartAfterCapacityCheckpoint(t *testing.T) {
	const private = "api_key=synthetic-rejected-after-capacity"
	const priorThought = "PREVIOUS_NATIVE_REASONING_"
	var calls atomic.Int64
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		if strings.Contains(string(raw), private) {
			t.Error("private turn was replayed after the capacity checkpoint")
		}
		call := calls.Add(1)
		thought := priorThought + strings.Repeat("x", 41000)
		tool := finalResultTool("query_data", map[string]any{"sql": "SELECT 7 AS checked_value"})
		if call == 2 {
			if strings.Contains(string(raw), priorThought) {
				t.Error("capacity checkpoint replayed the previous native segment")
			}
			thought = private
			tool = finalResultTool("query_data", map[string]any{"sql": "SELECT 99 AS rejected_value"})
		} else if call > 2 {
			t.Error("privacy created a second proactive checkpoint")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": thought, "reasoning_content": "UNEXPECTED_DISABLED_REASONING", "tool_calls": []any{map[string]any{"index": 0, "id": tool.ID, "type": "function", "function": tool.Function}}}}}})
		fmt.Fprintf(w, "data: %s\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":50}}\n\ndata: [DONE]\n\n", payload)
	})
	if err := f.db.Model(&Settings{}).Where("id=1").Update("model", "deepseek-v4-pro").Error; err != nil {
		t.Fatal(err)
	}
	conversation := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, conversation.ID, "核对保存结果", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	finished := securityWaitRun(t, f, run.ID)
	var charges, results int64
	f.db.Model(&BudgetReservation{}).Where("run_id=?", run.ID).Count(&charges)
	f.db.Model(&ResultRecord{}).Where("run_id=?", run.ID).Count(&results)
	var checkpoints []Event
	f.db.Where("run_id=? AND type='context_checkpoint'", run.ID).Find(&checkpoints)
	if finished.Status != "failed" || finished.ErrorCode != "provider_privacy" || calls.Load() != 2 || charges != 2 || results != 1 || len(checkpoints) != 1 || !strings.Contains(checkpoints[0].Data, `"capacity"`) {
		t.Fatalf("checkpoint allowance reset: run=%+v calls=%d charges=%d results=%d checkpoints=%+v", finished, calls.Load(), charges, results, checkpoints)
	}
	assertPrivateTurnNotPersisted(t, f, run.ID, private)
}

func TestPrivateContentRecoveryRejectsWholeTurnAndKeepsAccounting(t *testing.T) {
	for _, repeated := range []bool{false, true} {
		t.Run(map[bool]string{false: "recovers", true: "one_checkpoint_only"}[repeated], func(t *testing.T) {
			const private = "api_key=synthetic-private-reasoning-credential"
			var calls atomic.Int64
			var id string
			f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				if strings.Contains(string(raw), private) {
					t.Error("rejected private reasoning was sent to provider")
				}
				var request ModelRequest
				_ = json.Unmarshal(raw, &request)
				for _, message := range request.Messages {
					if message.Role == "tool" {
						var result Result
						_ = json.Unmarshal([]byte(message.Content), &result)
						if result.ID != "" {
							id = result.ID
						}
					}
				}
				call := calls.Add(1)
				thought := "Use verified saved evidence."
				tool := finalResultTool("query_data", map[string]any{"sql": "SELECT 7 AS checked_value"})
				if call == 2 || repeated && call == 3 {
					thought = private
					tool = finalResultTool("query_data", map[string]any{"sql": "SELECT 99 AS forbidden_unaccepted_query"})
				} else if call == 3 {
					if !strings.Contains(string(raw), id) || strings.Contains(string(raw), "forbidden_unaccepted_query") {
						t.Error("recovery did not keep only verified evidence")
					}
					tool = finalResultTool("finalize_answer", FinalizeRequest{ResultIDs: []string{id}, Markdown: "{{checked}}", References: []AnswerReference{{Token: "checked", ResultID: id, Row: intPointer(0), Column: "checked_value", Level: "cell"}}})
				} else if call > 3 {
					t.Error("privacy recovery exceeded one proactive checkpoint")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				delta := map[string]any{"content": thought, "reasoning_content": "UNEXPECTED_DISABLED_REASONING", "tool_calls": []any{map[string]any{"index": 0, "id": tool.ID, "type": "function", "function": tool.Function}}}
				payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
				fmt.Fprintf(w, "data: %s\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":50}}\n\ndata: [DONE]\n\n", payload)
			})
			f.service.cfg.Model = "deepseek-v4-pro"
			if err := f.db.Model(&Settings{}).Where("id = 1").Update("model", "deepseek-v4-pro").Error; err != nil {
				t.Fatal(err)
			}
			c := securityCreateConversation(t, f, securityAdmin)
			run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "核对保存的数据", uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			finished := securityWaitRun(t, f, run.ID)
			var charges, results int64
			f.db.Model(&BudgetReservation{}).Where("run_id=?", run.ID).Count(&charges)
			f.db.Model(&ResultRecord{}).Where("run_id=?", run.ID).Count(&results)
			var events []Event
			f.db.Where("run_id=?", run.ID).Find(&events)
			for _, event := range events {
				if strings.Contains(event.Data, private) {
					t.Fatal("private response persisted in event")
				}
			}
			var messages []Message
			f.db.Where("run_id=?", run.ID).Find(&messages)
			for _, message := range messages {
				if strings.Contains(message.Content, private) || strings.Contains(message.BlocksJSON, private) {
					t.Fatal("private response persisted in answer")
				}
			}
			if calls.Load() != 3 || charges != 3 || results != 1 {
				t.Fatalf("recovery invariants failed calls=%d charges=%d results=%d", calls.Load(), charges, results)
			}
			if repeated {
				if finished.Status != "failed" || finished.ErrorCode != "provider_privacy" {
					t.Fatalf("repeated privacy rejection was not correctly classified: %+v", finished)
				}
			} else if finished.Status != "completed" {
				t.Fatalf("safe native restart did not complete: %+v", finished)
			}
		})
	}
}

func TestPrivacyErrorIsNotContextCapacity(t *testing.T) {
	if runErrorCode("供应商推理上下文含不能发送的敏感片段，分析已停止") != "provider_privacy" || runErrorCode(ErrSnapshotSourceTimeout.Error()) != "snapshot_timeout" {
		t.Fatal("distinct failure conditions were misclassified")
	}
}
