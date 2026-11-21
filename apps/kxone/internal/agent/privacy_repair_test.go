package agent

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestExplicitCustomerIdentityPrecedesBusinessVocabulary(t *testing.T) {
	r := newRedactor()
	r.employees["员工乙"] = true
	r.addName("陆青岚", "客户#7")
	r.prepare()
	s := &Snapshot{At: time.Now().UTC(), redactor: r}
	for _, label := range []string{"家长姓名：", "客户姓名=", "姓名：", "学生姓名: ", "家长姓名\n：", "家长：", "客户=", "联系人: "} {
		for _, name := range []string{"方法", "范围", "员工乙", "史小明"} {
			input := label + name
			clean, count := s.Sanitize(input)
			if strings.Contains(clean, name) || count == 0 || providerTurnSafe(s, ModelMessage{Content: input}) {
				t.Errorf("explicit customer identity escaped: %q => %q count=%d", input, clean, count)
			}
		}
	}
	for _, input := range []string{"范围：当前筛选集合。", "方法：按记录主键去重。", "员工姓名：员工乙"} {
		clean, count := s.Sanitize(input)
		if clean != normalizedSensitiveText(input) || count != 0 || !providerTurnSafe(s, ModelMessage{Content: input}) {
			t.Errorf("business meaning changed: %q => %q", input, clean)
		}
	}
	clean, _ := s.Sanitize("家长姓名：陆青岚")
	if strings.Contains(clean, "陆青岚") || !strings.Contains(clean, "客户#7") {
		t.Fatalf("known customer binding lost: %q", clean)
	}
	for _, alias := range []string{"家长", "姓名", "范"} {
		r := newRedactor()
		r.addName(alias, "客户#9")
		r.prepare()
		clean, count := r.sanitize("家长姓名：范围")
		if strings.Contains(clean, "范围") || count == 0 {
			t.Errorf("label or partial alias bypassed explicit identity: alias=%q clean=%q", alias, clean)
		}
	}
}

func TestPrivacyRepairCarriesOnlySanitizedTypedEvidence(t *testing.T) {
	s := measuredFixture(t)
	s.redactor.addName("陆青岚", "客户#7")
	s.redactor.addContact("parent_fixture_alpha", "客户#7")
	s.redactor.credentials = []string{"OPAQUE_PRIVATE_TEST_KEY"}
	s.redactor.prepare()
	r, err := s.Query(t.Context(), "SELECT 3 AS checked_count", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	// Valid generated identifiers can contain phone-like digit sequences.
	r.ID = "aaaaaaaa-aaaa-4aaa-aaaa-13800138001a"
	results := map[string]*Result{r.ID: r}
	reads := newEvidenceReadState()
	page, _ := modelResultPage(r, 0, 20)
	reads.observe(page)
	history := []Message{{ID: "bbbbbbbb-bbbb-4bbb-bbbb-13800138002b", RunID: "run", Role: "user", Content: "来源好学校；客户陆青岚"}}
	req := FinalizeRequest{ResultIDs: []string{r.ID}, TableResultIDs: []string{r.ID}, Markdown: "家长姓名：方法。陆青岚；微信parent_fixture_alpha；OPAQUE_PRIVATE_TEST_KEY。历史记录有两条。", References: []AnswerReference{{Token: "n", ResultID: r.ID, Row: intPointer(0), Column: "checked_count", Level: "cell"}}, Conditions: []AnalysisCondition{{Kind: "source", Text: "好学校", SourceMessageID: history[0].ID}}}
	tool := finalResultTool("finalize_answer", req)
	var untyped map[string]any
	_ = json.Unmarshal([]byte(tool.Function.Arguments), &untyped)
	untyped["reasoning_content"] = "PRIVATE_REASONING_UNKNOWN_FIELD"
	untyped["extra"] = "PRIVATE_UNKNOWN_FIELD"
	raw, _ := json.Marshal(untyped)
	tool.Function.Arguments = string(raw)
	thought := "PRIVATE_NATIVE_REASONING"
	message := ModelMessage{Content: "PRIVATE_CONTENT_IGNORED", ReasoningContent: &thought, ToolCalls: []ToolCall{tool}}
	proposal, err := buildPrivacyRepair(message, nil, results, reads, AnalysisContext{}, history, s, "run")
	if err != nil || proposal == nil {
		t.Fatalf("safe repair unavailable: %+v %v", proposal, err)
	}
	encoded, _ := json.Marshal(proposal)
	for _, private := range []string{"方法", "陆青岚", "parent_fixture_alpha", "OPAQUE_PRIVATE_TEST_KEY", "PRIVATE_REASONING", "PRIVATE_NATIVE", "PRIVATE_UNKNOWN", "PRIVATE_CONTENT"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("private/untyped material recovered: %q", private)
		}
	}
	if !reflect.DeepEqual(proposal.Request.References, req.References) || !reflect.DeepEqual(proposal.Request.Conditions, req.Conditions) || !reflect.DeepEqual(proposal.Request.ResultIDs, req.ResultIDs) || !reflect.DeepEqual(proposal.Request.TableResultIDs, req.TableResultIDs) {
		t.Fatal("typed structural evidence changed")
	}
	if !strings.Contains(proposal.WithheldMarkdown, "两条") || !strings.Contains(proposal.WithheldMarkdown, "客户#7") || !strings.Contains(proposal.Instruction, "敏感片段已省略") {
		t.Fatal("withheld claims or coverage notice lost")
	}
	if _, _, err := ValidateAnswer(FinalizeRequest{Markdown: proposal.WithheldMarkdown, ResultIDs: req.ResultIDs, References: req.References}, nil, results, reads); err == nil {
		t.Fatal("unbound original narrative became a validated answer")
	}
}

func TestPrivacyRepairRejectsUnverifiedOrUnboundedStructure(t *testing.T) {
	s := measuredFixture(t)
	r, err := s.Query(t.Context(), "SELECT 3 AS checked_count", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	results := map[string]*Result{r.ID: r}
	reads := newEvidenceReadState()
	page, _ := modelResultPage(r, 0, 20)
	reads.observe(page)
	history := []Message{{ID: "message", RunID: "run", Role: "user", Content: "来源好学校"}}
	original := FinalizeRequest{ResultIDs: []string{r.ID}, TableResultIDs: []string{r.ID}, Markdown: "按阶段检索会排除阶段未知的事件。", References: []AnswerReference{{Token: "n", ResultID: r.ID, Row: intPointer(0), Column: "checked_count", Level: "cell"}}, Conditions: []AnalysisCondition{{Kind: "source", Text: "好学校", SourceMessageID: "message"}}}
	for _, tc := range []struct {
		name string
		edit func(*FinalizeRequest, map[string]*Result, *evidenceReadState)
	}{
		{"unknown result", func(p *FinalizeRequest, _ map[string]*Result, _ *evidenceReadState) {
			p.ResultIDs = []string{"missing"}
		}},
		{"stale snapshot", func(_ *FinalizeRequest, rs map[string]*Result, _ *evidenceReadState) {
			copy := *r
			copy.SnapshotAt = s.At.Add(-time.Second)
			rs[r.ID] = &copy
		}},
		{"unread cell", func(p *FinalizeRequest, _ map[string]*Result, _ *evidenceReadState) {
			p.References[0].Row = intPointer(99)
		}},
		{"unread result", func(_ *FinalizeRequest, _ map[string]*Result, rd *evidenceReadState) { *rd = *newEvidenceReadState() }},
		{"unknown column", func(p *FinalizeRequest, _ map[string]*Result, _ *evidenceReadState) {
			p.References[0].Column = "missing"
		}},
		{"private token", func(p *FinalizeRequest, _ map[string]*Result, _ *evidenceReadState) {
			p.References[0].Token = "n13800138001"
		}},
		{"forged fact", func(p *FinalizeRequest, _ map[string]*Result, _ *evidenceReadState) {
			p.References = []AnswerReference{{FactID: "missing", Level: "global"}}
		}},
		{"fake condition", func(p *FinalizeRequest, _ map[string]*Result, _ *evidenceReadState) {
			p.Conditions[0].Text = "转介绍"
		}},
		{"fake message", func(p *FinalizeRequest, _ map[string]*Result, _ *evidenceReadState) {
			p.Conditions[0].SourceMessageID = "missing"
		}},
		{"oversized", func(p *FinalizeRequest, _ map[string]*Result, _ *evidenceReadState) {
			p.Markdown = strings.Repeat("解释仍待核对。", 1000)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(original)
			var req FinalizeRequest
			_ = json.Unmarshal(raw, &req)
			rs := map[string]*Result{r.ID: r}
			rd := newEvidenceReadState()
			rd.observe(page)
			tc.edit(&req, rs, rd)
			proposal, err := buildPrivacyRepair(ModelMessage{ToolCalls: []ToolCall{finalResultTool("finalize_answer", req)}}, nil, rs, rd, AnalysisContext{}, history, s, "run")
			if err == nil || proposal != nil {
				t.Fatalf("unverified repair accepted: %+v %v", proposal, err)
			}
		})
	}
	for _, calls := range [][]ToolCall{
		{finalResultTool("query_data", map[string]any{"sql": "SELECT 1"})},
		{finalResultTool("present_result", map[string]any{}), finalResultTool("finalize_answer", original)},
		{finalResultTool("finalize_answer", original), finalResultTool("finalize_answer", original)},
	} {
		if proposal, _ := buildPrivacyRepair(ModelMessage{ToolCalls: calls}, nil, results, reads, AnalysisContext{}, history, s, "run"); proposal != nil {
			t.Fatal("mixed/unaccepted native tools became recovery material")
		}
	}
}

func TestPrivacyRepairRequiresExplicitResubmissionAndPreservesLimits(t *testing.T) {
	for _, scenario := range []string{"valid resubmission", "plain confirmation", "numeric correction", "second privacy failure", "provider fallback"} {
		t.Run(scenario, func(t *testing.T) {
			const private = "陆青岚"
			const nativeReasoning = "PRIVATE_REASONING_DO_NOT_REPLAY"
			var calls atomic.Int32
			var original FinalizeRequest
			var recovered answerRepairProposal
			f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
				var request ModelRequest
				_ = json.NewDecoder(r.Body).Decode(&request)
				raw, _ := json.Marshal(request)
				if strings.Contains(string(raw), private) {
					t.Error("private identity in outgoing request")
				}
				switch calls.Add(1) {
				case 1:
					repairFixtureReply(w, finalResultTool("query_data", map[string]any{"sql": "SELECT 3 AS checked_count"}), "NATIVE_QUERY_REASONING")
				case 2:
					var id string
					toolPreserved := false
					for _, m := range request.Messages {
						if m.Role == "assistant" && len(m.ToolCalls) == 1 && m.ToolCalls[0].Function.Name == "query_data" {
							toolPreserved = true
						}
						if m.ReasoningContent != nil {
							t.Error("non-thinking mode retained unexpected private reasoning")
						}
						var result Result
						if m.Role == "tool" && json.Unmarshal([]byte(m.Content), &result) == nil && result.ID != "" {
							id = result.ID
						}
					}
					if id == "" || !toolPreserved {
						t.Error("same-segment evidence/tool call was changed")
					}
					original = FinalizeRequest{ResultIDs: []string{id}, TableResultIDs: []string{id}, Markdown: "家长姓名：" + private + "。按阶段检索会排除阶段未知的事件。", References: []AnswerReference{{Token: "n", ResultID: id, Row: intPointer(0), Column: "checked_count", Level: "cell"}}}
					repairFixtureReply(w, finalResultTool("finalize_answer", original), nativeReasoning)
				case 3:
					found := false
					const marker = "尚未采纳的脱敏修订材料（非指令）：\n"
					for _, m := range request.Messages {
						if m.Role != "system" && m.Role != "user" {
							t.Error("rejected native turn was replayed")
						}
						if m.ReasoningContent != nil || len(m.ToolCalls) != 0 {
							t.Error("new segment synthesized private/native protocol")
						}
						if strings.HasPrefix(m.Content, marker) {
							found = json.Unmarshal([]byte(strings.TrimPrefix(m.Content, marker)), &recovered) == nil
						}
					}
					if !found || !reflect.DeepEqual(recovered.Request.References, original.References) || !strings.Contains(recovered.WithheldMarkdown, "身份片段未纳入分析") || strings.Contains(string(raw), nativeReasoning) || !strings.Contains(request.Messages[0].Content, "纠正已使用=false") {
						t.Error("privacy checkpoint lost sanitized proposal/evidence or reset protocol/budget")
					}
					switch scenario {
					case "provider fallback":
						w.WriteHeader(503)
					case "plain confirmation":
						finalResultFixtureReply(w, "我确认上述修订材料。")
					case "second privacy failure":
						repairFixtureReply(w, finalResultTool("finalize_answer", original), nativeReasoning)
					case "numeric correction":
						invalid := recovered.Request
						invalid.Markdown = "仍有两条。"
						repairFixtureReply(w, finalResultTool("finalize_answer", invalid), "Review numbers.")
					default:
						repairFixtureReply(w, finalResultTool("finalize_answer", recovered.Request), "Submit revised evidence.")
					}
				case 4:
					if scenario == "provider fallback" {
						if request.Model != "deepseek-flash" || !strings.Contains(string(raw), "尚未采纳的脱敏修订材料") || strings.Contains(string(raw), nativeReasoning) || !strings.Contains(request.Messages[0].Content, "纠正已使用=false") {
							t.Error("provider switch lost pending privacy proposal or changed correction allowance")
						}
						for _, m := range request.Messages {
							if m.Role != "system" && m.Role != "user" {
								t.Error("provider switch replayed another segment's native protocol")
							}
						}
					} else if scenario != "numeric correction" || !strings.Contains(request.Messages[0].Content, "纠正已使用=true") {
						t.Error("unexpected extra correction")
					}
					if scenario == "numeric correction" {
						repairFixtureReply(w, finalResultTool("accept_answer_repair", map[string]any{"proposal_id": repairIDFromTools(t, request)}), "Correct after validation.")
					} else {
						repairFixtureReply(w, finalResultTool("finalize_answer", recovered.Request), "Correct after validation.")
					}
				default:
					t.Error("model call limit was reset")
					w.WriteHeader(500)
				}
			})
			f.service.cfg.MaxCalls = 4
			if scenario == "provider fallback" {
				configureTestBackup(t, f, f.model.URL)
			}
			if err := f.db.Model(&Settings{}).Where("id=1").Update("model", "deepseek-v4-pro").Error; err != nil {
				t.Fatal(err)
			}
			createSnapshotLead(t, f.db, 1)
			if err := f.db.Exec("UPDATE leads SET customer_name=? WHERE id=1", private).Error; err != nil {
				t.Fatal(err)
			}
			conversation := securityCreateConversation(t, f, securityAdmin)
			run, err := f.service.Start(t.Context(), securityAdmin, conversation.ID, "核对查询数据", uuid.NewString())
			if err != nil {
				t.Fatal(err)
			}
			finished := securityWaitRun(t, f, run.ID)
			wantComplete := scenario == "valid resubmission" || scenario == "numeric correction" || scenario == "provider fallback"
			wantCalls := int64(3)
			if scenario == "numeric correction" || scenario == "provider fallback" {
				wantCalls = 4
			}
			var charges, checkpoints, resultCount int64
			f.db.Model(&BudgetReservation{}).Where("run_id=?", run.ID).Count(&charges)
			f.db.Model(&Event{}).Where("run_id=? AND type='context_checkpoint'", run.ID).Count(&checkpoints)
			f.db.Model(&ResultRecord{}).Where("run_id=?", run.ID).Count(&resultCount)
			if (finished.Status == "completed") != wantComplete || int64(calls.Load()) != wantCalls || charges != wantCalls || checkpoints != 1 || resultCount != 1 {
				t.Fatalf("privacy recovery result/limits mismatch: %+v calls=%d charges=%d checkpoints=%d results=%d", finished, calls.Load(), charges, checkpoints, resultCount)
			}
			assertPrivateTurnNotPersisted(t, f, run.ID, private)
			assertPrivateTurnNotPersisted(t, f, run.ID, nativeReasoning)
			if scenario == "provider fallback" {
				summary, err := f.service.budget.Summary(t.Context(), finished.CreatedAt)
				if err != nil || summary.PendingCalls != 1 || summary.ReservedMicro <= 0 || summary.SettledCalls != 3 {
					t.Fatalf("fallback lost unknown usage reservation: %+v %v", summary, err)
				}
			}
			if !wantComplete {
				var message Message
				f.db.Where("run_id=? AND role='assistant'", run.ID).First(&message)
				if message.Content != finished.Error || message.BlocksJSON != "[]" {
					t.Fatal("unaccepted proposal published")
				}
			}
		})
	}
}

// Preserve conservative handling of ambiguous ordinary language. Recovery
// works from sanitized text instead of expanding a global surname whitelist.
func TestPrivacyRepairHandlesDictionaryAmbiguityWithoutWhitelisting(t *testing.T) {
	s := measuredFixture(t)
	r, err := s.Query(t.Context(), "SELECT 3 AS checked_count", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	results := map[string]*Result{r.ID: r}
	reads := newEvidenceReadState()
	page, _ := modelResultPage(r, 0, 20)
	reads.observe(page)
	for _, text := range []string{
		"仅保留原记录明确存储的 business_stage：leads 是当前阶段，lead_followups 是提交时阶段；没有该字段的原表保持 NULL。按阶段检索会排除阶段未知的事件，须说明覆盖限制，不能用线索当前阶段回填历史活动。",
		"已知姓名和联系方式在问题中替换为客户编号；同名或共用联系方式会给出多个编号，必须包含这些线索而非任选一条。客户姓名不在快照中；可用 lead_id 或 customer_ref 查询。手机号形式的员工 userid 被映射为员工#账号ID，所有关系仍可关联。",
		"Search.record_coverage 只统计当前返回页的原始记录，distinct_records 与 distinct_leads 不同。complete_query_output=false 时不代表全量；来源卡片数量不是覆盖数。任意 SQL 的 record_coverage=null 表示无法确认，不是零；需自行使用 COUNT(DISTINCT 原表主键) 与 COUNT(DISTINCT lead_id) 分别核对记录数和线索数。analysis_records 是原记录的索引，不是新增业务事件，合并原表必须按 record_type+record_id 去重，不能重复计算或目测。",
		"客户：当前已成交。",
		"周期：当前责任周期。",
		"周期内已联系。",
	} {
		req := FinalizeRequest{ResultIDs: []string{r.ID}, Markdown: text, References: []AnswerReference{{Token: "n", ResultID: r.ID, Row: intPointer(0), Column: "checked_count", Level: "cell"}}}
		message := ModelMessage{ToolCalls: []ToolCall{finalResultTool("finalize_answer", req)}}
		if providerTurnSafe(s, message, results) {
			t.Fatalf("ambiguity guard was silently relaxed: %q", text)
		}
		proposal, err := buildPrivacyRepair(message, nil, results, reads, AnalysisContext{}, nil, s, "run")
		if err != nil || proposal == nil || proposal.WithheldMarkdown == text {
			t.Fatalf("sanitized draft could not recover: %+v %v", proposal, err)
		}
		if !providerTurnSafe(s, ModelMessage{Content: proposal.WithheldMarkdown}) {
			t.Fatal("recovered text still fails privacy")
		}
	}
}

func TestPrivacyRepairKeepsMeasuredScopeAndDeliveredEmptyTable(t *testing.T) {
	s := measuredFixture(t)
	r, err := s.Measure(t.Context(), MeasureRequest{Source: "lead_followups"})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := s.Query(t.Context(), "SELECT 3 AS checked_count WHERE 0", nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	r.Facts[0].ID = "cccccccc-cccc-4ccc-accc-13800138003c"
	results := map[string]*Result{r.ID: r, empty.ID: empty}
	reads := newEvidenceReadState()
	page, _ := modelResultPage(r, 0, 20)
	reads.observe(page)
	emptyPage, _ := modelResultPage(empty, 0, 20)
	reads.observe(emptyPage)
	req := FinalizeRequest{ResultIDs: []string{r.ID, empty.ID}, TableResultIDs: []string{empty.ID}, Markdown: "按阶段检索会排除阶段未知的事件。", References: []AnswerReference{{FactID: r.Facts[0].ID, Level: "global"}}}
	check := func() (*answerRepairProposal, error) {
		return buildPrivacyRepair(ModelMessage{ToolCalls: []ToolCall{finalResultTool("finalize_answer", req)}}, nil, results, reads, AnalysisContext{}, nil, s, "run")
	}
	proposal, err := check()
	if err != nil || proposal == nil || proposal.Request.References[0].FactID != r.Facts[0].ID || len(proposal.Request.TableResultIDs) != 1 {
		t.Fatalf("valid scope/empty table lost: %+v %v", proposal, err)
	}
	for _, mutation := range []string{"fact snapshot", "fact scope", "group as global", "unread table", "known secret identifier"} {
		t.Run(mutation, func(t *testing.T) {
			fact := r.Facts[0]
			switch mutation {
			case "fact snapshot":
				r.Facts[0].SnapshotAt = s.At.Add(-time.Second)
			case "fact scope":
				r.Facts[0].ScopeID = "other-scope"
			case "group as global":
				r.Facts[0].Level = "group"
			case "unread table":
				delete(reads.results, empty.ID)
			case "known secret identifier":
				s.redactor.credentials = []string{r.Facts[0].ID}
			}
			if proposal, err := check(); err == nil || proposal != nil {
				t.Fatalf("invalid metadata recovered: %+v %v", proposal, err)
			}
			r.Facts[0] = fact
			reads.results[empty.ID] = true
			s.redactor.credentials = nil
		})
	}
}
