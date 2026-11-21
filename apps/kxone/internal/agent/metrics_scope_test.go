package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

func seedOwnerInventory(t *testing.T, f *agentSecurityFixture) {
	t.Helper()
	now := time.Now()
	for i, employee := range []struct{ id, name string }{{"staff-zhan", "占伟"}, {"staff-li", "李明"}, {"staff-zero", "王空"}} {
		err := repo.NewGormUserBindingRepository(f.db).Create(t.Context(), &types.WeComUserBinding{CRMUserID: int64(10 + i), WeComUserID: employee.id, Username: employee.id, EmployeeName: employee.name, Role: types.UserRoleCloser, Status: types.UserStatusActive, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
	}
	for i, status := range []string{"following", "converted", "invalid", "closed", "assigned_pending_claim", "following"} {
		owner, assigned, ownership := "staff-zhan", "staff-zhan", "private"
		result := "active"
		if i > 0 && i < 4 {
			result = status
		}
		if i == 4 {
			owner, ownership = "", "assigned_pending_claim"
		}
		if i == 5 {
			owner, assigned = "staff-li", "staff-li"
		}
		source := "好学校"
		if i == 3 {
			source = "转介绍"
		}
		err := f.db.Exec(`INSERT INTO leads(id,lead_no,customer_name,customer_mobile,customer_wechat,source_channel,status,business_stage,ownership_state,result_status,current_owner_user_id,current_owner_role,assigned_inviter_user_id,deal_amount,refund_amount,payment_status,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, 800+i, fmt.Sprintf("inventory-%d", i), "匿名客户", "", "", source, status, "invitation", ownership, result, owner, "inviter", assigned, 0, 0, "unpaid", now, now).Error
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestScopeRejectsUnknownOwnerAndUnsupportedExplicitFilters(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	for _, q := range []string{"占伟强手上有多少线索？", "占伟手上只算有效线索有多少？", "占伟手上5星的线索有多少？", "占伟手上星级为5的线索有多少？", "只看好学校和未知渠道，占伟手上有多少线索？", "占伟在2026-02-30新增多少线索？", "占伟在2月30日至3月1日新增多少线索？", "请还原2026-10-08 10:00截止时的回访待办"} {
		a, events := askScopeQuestion(t, f, q)
		found := false
		for _, event := range events {
			if event.Type == "metric_selected" {
				t.Fatalf("%s silently queried: %s", q, a.Content)
			}
			found = found || event.Type == "scope_clarification"
		}
		if !found {
			t.Fatalf("%s did not clarify: %s", q, a.Content)
		}
	}
}

func TestSourceNamesAreAtomicAndDoNotImplyAnOwner(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	if err := f.db.Exec(`UPDATE leads SET source_channel=CASE id WHEN 800 THEN '百度' WHEN 801 THEN '百度-渠道A' ELSE source_channel END`).Error; err != nil {
		t.Fatal(err)
	}
	a, _ := askScopeQuestion(t, f, "只看百度-渠道A，占伟手上有多少线索？")
	if !strings.Contains(a.Content, "1 条") {
		t.Fatal(a.Content)
	}
	if err := f.db.Exec(`UPDATE leads SET source_channel='占伟视频号（员工乙）',status='converted',result_status='converted',converted_at=? WHERE id IN (800,805)`, time.Now()).Error; err != nil {
		t.Fatal(err)
	}
	a, events := askScopeQuestion(t, f, "来源占伟视频号（员工乙）成交了几单？")
	for _, event := range events {
		if event.Type == "metric_selected" {
			var selection struct {
				Params MetricParams `json:"params"`
			}
			if err := json.Unmarshal([]byte(event.Data), &selection); err != nil {
				t.Fatal(err)
			}
			if selection.Params.OwnerID != "" || selection.Params.Owner != "" {
				t.Fatal("source implied employee:", event.Data)
			}
		}
	}
	if !strings.Contains(a.Content, "2 单") {
		t.Fatal(a.Content)
	}
}

func TestClearingSourcesPreservesExplicitCreationCohort(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	if err := f.db.Exec(`UPDATE leads SET created_at=? WHERE id=803`, time.Now().AddDate(-1, 0, 0)).Error; err != nil {
		t.Fatal(err)
	}
	c := securityCreateConversation(t, f, securityAdmin)
	askScopeInConversation(t, f, c.ID, "占伟手上本月录入的好学校线索有多少？")
	a, events := askScopeInConversation(t, f, c.ID, "那全部来源呢？")
	if !strings.Contains(a.Content, "3 条") {
		t.Fatal(a.Content)
	}
	for _, event := range events {
		if event.Type == "metric_selected" && !strings.Contains(event.Data, `"date_from"`) {
			t.Fatal(event.Data)
		}
	}
}

func TestMultiMetricFailureDoesNotPublishPartialNumbers(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []MetricSelection{{MetricID: "deal_count"}, {MetricID: "followup_keyword"}}}))
	})
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	a, _ := askScopeQuestion(t, f, "成交和跟进内容分别如何？")
	if strings.Contains(a.Content, "单") || strings.Contains(a.Content, "元") {
		t.Fatal("published incomplete requested metrics:", a.Content)
	}
	if !strings.Contains(a.Content, "条件") && !strings.Contains(a.Content, "失败") {
		t.Fatal(a.Content)
	}
}

func TestExplicitFollowupKeywordSurvivesWrongModelMetric(t *testing.T) {
	for _, modelMetric := range []string{"lead_count", "followup_records"} {
		for _, step := range []struct {
			question, summary string
			sources           []string
		}{
			{"跟进里提到费用问题的线索有哪些？是否足以证明费用造成流失？", "有 3 条，涉及 3 条线索，其中当前已成交 1 条", nil},
			{"来源是好学校，跟进里提到费用问题的线索有哪些？其中当前已成交多少？", "有 2 条，涉及 2 条线索，其中当前已成交 1 条", []string{"好学校"}},
		} {
			t.Run(modelMetric+step.question, func(t *testing.T) {
				f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
					finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []MetricSelection{{MetricID: modelMetric}}}))
				})
				f.service.cfg.FixedMetrics = true
				seedOwnerInventory(t, f)
				for i, leadID := range []int{800, 801, 803} {
					if err := f.db.Exec(`INSERT INTO lead_followups(id,lead_id,sales_user_id,followup_result,remark,is_valid,created_at) VALUES(?,?,'staff-zhan','connected','费用偏高',1,?)`, 900+i, leadID, time.Now()).Error; err != nil {
						t.Fatal(err)
					}
				}
				answer, events := askScopeQuestion(t, f, step.question)
				var selected MetricSelection
				for _, event := range events {
					if event.Type == "metric_selected" {
						if err := json.Unmarshal([]byte(event.Data), &selected); err != nil {
							t.Fatal(err)
						}
					}
				}
				if selected.MetricID != "followup_keyword" || selected.Params.Keyword != "费用" || !strings.Contains(answer.Content, step.summary) || strings.Join(selected.Params.Sources, ",") != strings.Join(step.sources, ",") {
					t.Fatalf("supported keyword question was lost: selection=%+v answer=%s", selected, answer.Content)
				}
			})
		}
	}
}

func TestCustomerCurrentTaskSurvivesModelFailure(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	if err := f.db.Exec(`UPDATE leads SET next_followup_due_at=? WHERE id=800`, time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := f.db.Exec(`INSERT INTO lead_followups(id,lead_id,sales_user_id,followup_result,remark,is_valid,next_followup_at,created_at) VALUES(?,800,'staff-zhan','connected','回访计划',1,?,?)`, 950+i, time.Now().Add(time.Hour), time.Now().Add(-time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
	}
	answer, events := askScopeQuestion(t, f, "客户#800 有多个历史回访计划，目前普通待办是否算多项？")
	var selected MetricSelection
	for _, event := range events {
		if event.Type == "metric_selected" {
			if err := json.Unmarshal([]byte(event.Data), &selected); err != nil {
				t.Fatal(err)
			}
		}
	}
	if selected.MetricID != "lead_followup_status" || selected.Params.Customer != "客户#800" || !strings.Contains(answer.Content, "当前普通待办 1 项，历史回访计划 2 条") {
		t.Fatalf("customer task fallback lost supported scope: selection=%+v answer=%s", selected, answer.Content)
	}
}

func TestCustomerOverdueIsNotSubstitutedWithTaskEligibility(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	answer, events := askScopeQuestion(t, f, "客户#800目前普通待办是否逾期？")
	declined := false
	for _, event := range events {
		if event.Type == "metric_selected" {
			t.Fatalf("overdue question was replaced by eligibility numbers: %s", answer.Content)
		}
		declined = declined || event.Type == "metric_declined"
	}
	if !declined || !strings.Contains(answer.Content, "逾期") || !strings.Contains(answer.Content, "不支持") {
		t.Fatal("unsupported customer overdue scope was not explained:", answer.Content)
	}
}

func TestOwnerInventoryVariantsZeroAndUnknown(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	for _, q := range []string{"占伟名下有多少线索？", "占伟负责多少线索？", "占伟手上有多少线索？", "现在占伟手上有多少线索？", "请问目前占伟名下有多少线索？"} {
		a, _ := askScopeQuestion(t, f, q)
		if !strings.Contains(a.Content, "4 条") {
			t.Fatalf("%s: %s", q, a.Content)
		}
	}
	a, _ := askScopeQuestion(t, f, "王空名下有多少线索？")
	if !strings.Contains(a.Content, "0 条") {
		t.Fatal(a.Content)
	}
	for _, q := range []string{"赵某某名下有多少线索？", "占伟和李明手上有多少线索？"} {
		a, events := askScopeQuestion(t, f, q)
		for _, e := range events {
			if e.Type == "metric_selected" {
				t.Fatalf("unresolved employee produced numbers: %s %+v", a.Content, e)
			}
		}
		if !strings.Contains(a.Content, "请") {
			t.Fatal(a.Content)
		}
	}
	if err := repo.NewGormUserBindingRepository(f.db).Create(t.Context(), &types.WeComUserBinding{CRMUserID: 19, WeComUserID: "other-zhan", Username: "other-zhan", EmployeeName: "占伟", Role: types.UserRoleCloser, Status: types.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	a, events := askScopeQuestion(t, f, "占伟手上有多少线索？")
	if !strings.Contains(a.Content, "同名") {
		t.Fatal(a.Content)
	}
	for _, e := range events {
		if e.Type == "metric_selected" {
			t.Fatal("ambiguous employee queried")
		}
	}
}

func TestExplicitSourceCannotDisappearFromInventory(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []MetricSelection{{MetricID: "lead_count"}}}))
	})
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	a, events := askScopeQuestion(t, f, "只看好学校，占伟手上有多少线索？")
	if !strings.Contains(a.Content, "3 条") {
		t.Fatalf("source disappeared: %s", a.Content)
	}
	for _, e := range events {
		if e.Type == "metric_selected" && !strings.Contains(e.Data, "好学校") {
			t.Fatal(e.Data)
		}
	}
}

func TestTaskSummaryCountsAllRowsBeyondPreview(t *testing.T) {
	f := newAgentSecurityFixture(t, nil)
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	now := time.Now()
	due := now.Add(-time.Hour)
	for i := 0; i < 205; i++ {
		err := f.db.Exec(`INSERT INTO leads(id,lead_no,customer_name,customer_mobile,customer_wechat,source_channel,status,business_stage,ownership_state,result_status,current_owner_user_id,current_owner_role,created_at,updated_at,next_followup_due_at) VALUES (?,?,?,?,?,?,'following','invitation','private','active','staff-zhan','inviter',?,?,?)`, 1000+i, fmt.Sprintf("task-%d", i), "匿名客户", "", "", "好学校", now, now, due).Error
		if err != nil {
			t.Fatal(err)
		}
	}
	c := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.StartMetric(t.Context(), securityAdmin, c.ID, MetricSelection{MetricID: "followup_tasks"}, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if done := securityWaitRun(t, f, run.ID); done.Status != "completed" {
		t.Fatal(done)
	}
	var answer Message
	f.db.Where("run_id=? AND role=?", run.ID, "assistant").First(&answer)
	if !strings.Contains(answer.Content, "205 项普通回访待办，其中已逾期 205 项") {
		t.Fatal(answer.Content)
	}
}

func askScopeQuestion(t *testing.T, f *agentSecurityFixture, question string) (Message, []Event) {
	t.Helper()
	c := securityCreateConversation(t, f, securityAdmin)
	return askScopeInConversation(t, f, c.ID, question)
}

func askScopeInConversation(t *testing.T, f *agentSecurityFixture, conversation, question string) (Message, []Event) {
	t.Helper()
	run, err := f.service.Start(t.Context(), securityAdmin, conversation, question, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if done := securityWaitRun(t, f, run.ID); done.Status != "completed" {
		t.Fatalf("run=%+v", done)
	}
	var answer Message
	if err := f.db.Where("run_id=? AND role=?", run.ID, "assistant").First(&answer).Error; err != nil {
		t.Fatal(err)
	}
	var events []Event
	if err := f.db.Where("run_id=?", run.ID).Order("seq").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	return answer, events
}

func TestInventoryFollowUpKeepsOwnerWhenClearingSources(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []MetricSelection{{MetricID: "lead_count"}}}))
	})
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	c := securityCreateConversation(t, f, securityAdmin)
	for _, step := range []struct{ question, number, owner string }{{"只看好学校，占伟手上有多少线索？", "3 条", "staff-zhan"}, {"那全部来源呢？", "4 条", "staff-zhan"}, {"换成李明呢？", "1 条", "staff-li"}} {
		a, events := askScopeInConversation(t, f, c.ID, step.question)
		if !strings.Contains(a.Content, step.number) {
			t.Fatalf("%s: %s", step.question, a.Content)
		}
		var selected MetricSelection
		for _, e := range events {
			if e.Type == "metric_selected" {
				_ = json.Unmarshal([]byte(e.Data), &selected)
			}
		}
		if selected.Params.OwnerID != step.owner {
			t.Fatalf("%s lost person: %+v", step.question, selected)
		}
	}
}

func TestPersonnelInventoryNeverAnswersGlobalLeadCount(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		// Reproduce the production screenshot: the model omits the employee.
		finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []MetricSelection{{MetricID: "lead_count"}}}))
	})
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	answer, events := askScopeQuestion(t, f, "占伟手上有多少线索？")
	var selected MetricSelection
	for _, event := range events {
		if event.Type == "metric_selected" {
			_ = json.Unmarshal([]byte(event.Data), &selected)
		}
	}
	if selected.MetricID != "owner_inventory" || selected.Params.OwnerID != "staff-zhan" {
		t.Fatalf("employee scope lost: selected=%+v answer=%s", selected, answer.Content)
	}
	if !strings.Contains(answer.Content, "占伟") || !strings.Contains(answer.Content, "4 条") || !strings.Contains(answer.Content, "1 条") {
		t.Fatalf("must count all four owned states and separately report pending claim: %s", answer.Content)
	}
}

func manualScopeAnswer(t *testing.T, f *agentSecurityFixture, sel MetricSelection) (Message, []Block) {
	t.Helper()
	c := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.StartMetric(t.Context(), securityAdmin, c.ID, sel, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if done := securityWaitRun(t, f, run.ID); done.Status != "completed" {
		t.Fatal(done)
	}
	var answer Message
	f.db.Where("run_id=? AND role=?", run.ID, "assistant").First(&answer)
	var blocks []Block
	if err := json.Unmarshal([]byte(answer.BlocksJSON), &blocks); err != nil {
		t.Fatal(err)
	}
	return answer, blocks
}

func TestConversionPeriodAndMissingAmountsAreNotCreationCountsOrZero(t *testing.T) {
	f := newAgentSecurityFixture(t, nil)
	f.service.cfg.FixedMetrics = true
	for _, row := range []struct {
		id                         int
		created, converted, source string
		amount                     any
	}{{901, "2026-09-01", "2026-10-05", "九月录入十月成交", 100}, {902, "2026-10-02", "2026-09-30", "十月录入九月成交", 200}} {
		err := f.db.Exec(`INSERT INTO leads(id,lead_no,customer_name,customer_mobile,customer_wechat,source_channel,status,business_stage,ownership_state,result_status,deal_amount,refund_amount,payment_status,created_at,updated_at,converted_at) VALUES (?,?,?,?,?,?,'converted','finished','private','converted',?,0,'paid',?,?,?)`, row.id, fmt.Sprintf("period-%d", row.id), "匿名客户", "", "", row.source, row.amount, row.created+" 00:00:00+00:00", row.converted+" 00:00:00+00:00", row.converted+" 00:00:00+00:00").Error
		if err != nil {
			t.Fatal(err)
		}
	}
	p := MetricParams{DateFrom: "2026-10-01", DateTo: "2026-10-31"}
	_, blocks := manualScopeAnswer(t, f, MetricSelection{MetricID: "source_deals", Params: p})
	var table *Result
	for _, b := range blocks {
		if b.Type == "table" {
			table = b.Result
			break
		}
	}
	if table == nil || len(table.Rows) != 1 || table.Rows[0][0] != "九月录入十月成交" || table.Rows[0][1] != float64(1) {
		t.Fatalf("conversion month mixed with creation month: %+v", table)
	}
	// SQLite can retain malformed legacy values despite the current API's
	// validation. Such money remains unknown in the approved projection.
	if err := f.db.Exec(`UPDATE leads SET deal_amount='missing' WHERE id=901`).Error; err != nil {
		t.Fatal(err)
	}
	a, _ := manualScopeAnswer(t, f, MetricSelection{MetricID: "deal_count", Params: p})
	if !strings.Contains(a.Content, "未知") || strings.Contains(a.Content, "金额 0 元") {
		t.Fatal(a.Content)
	}
}

func TestUnsupportedPersonFilterClarifiesInsteadOfDroppingIt(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []MetricSelection{{MetricID: "appointment_status"}}}))
	})
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	a, events := askScopeQuestion(t, f, "占伟的预约状态分布？")
	if !strings.Contains(a.Content, "条件") {
		t.Fatal(a.Content)
	}
	for _, event := range events {
		if event.Type == "metric_selected" {
			t.Fatal("unsupported owner was silently discarded")
		}
	}
}

func TestOwnerAccountWithoutDisplayNameStaysScoped(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	if err := f.db.Exec(`UPDATE crm_wecom_user_bindings SET employee_name='' WHERE wecom_userid='staff-zhan'`).Error; err != nil {
		t.Fatal(err)
	}
	a, events := askScopeQuestion(t, f, "staff-zhan手上有多少线索？")
	if !strings.Contains(a.Content, "staff-zhan当前名下共有 4 条") {
		t.Fatal(a.Content)
	}
	for _, e := range events {
		if e.Type == "metric_selected" && !strings.Contains(e.Data, `"owner_id":"staff-zhan"`) {
			t.Fatal(e.Data)
		}
	}
}

func TestCashAmountsCannotBeInventedFromDeals(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []MetricSelection{{MetricID: "deal_count"}}}))
	})
	f.service.cfg.FixedMetrics = true
	a, events := askScopeQuestion(t, f, "本月到账多少钱？")
	if !strings.Contains(a.Content, "没有收款流水") {
		t.Fatal(a.Content)
	}
	declined := false
	for _, e := range events {
		if e.Type == "metric_selected" {
			t.Fatal(e.Data)
		}
		declined = declined || e.Type == "metric_declined"
	}
	if !declined {
		t.Fatal("missing unsupported category")
	}
}

func TestOwnerFirstResponseUsesShanghaiOccurrenceDateAndExposesUnknownCycle(t *testing.T) {
	f := newAgentSecurityFixture(t, nil)
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	// UTC Sep 30 16:10 is Shanghai Oct 1. Lead creation is irrelevant.
	if err := f.db.Exec(`UPDATE leads SET created_at='2026-09-01T00:00:00Z',current_owner_started_at='2026-09-30T16:00:00Z',current_owner_first_followup_at='2026-09-30T16:10:00Z' WHERE id=800`).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec(`UPDATE leads SET current_owner_started_at=NULL,current_owner_first_followup_at='2026-10-01T01:00:00Z' WHERE id=801`).Error; err != nil {
		t.Fatal(err)
	}
	_, blocks := manualScopeAnswer(t, f, MetricSelection{MetricID: "owner_first_response", Params: MetricParams{DateFrom: "2026-10-01", DateTo: "2026-10-01", OwnerID: "staff-zhan"}})
	var result *Result
	for _, b := range blocks {
		if b.Type == "table" {
			result = b.Result
			break
		}
	}
	if result == nil || len(result.Rows) != 1 {
		t.Fatalf("%+v", result)
	}
	values := map[string]any{}
	for i, c := range result.Columns {
		values[c] = result.Rows[0][i]
	}
	if fmt.Sprint(values["线索数"]) != "2" || fmt.Sprint(values["负责人已首跟"]) != "1" || fmt.Sprint(values["责任周期未知"]) != "1" || fmt.Sprint(values["负责人平均首跟(分钟)"]) != "10" {
		t.Fatal(values)
	}
}

func TestSupportedSourceDealAndRatingQuestionsRemainAnswerable(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	for _, q := range []string{"来源是好学校的成交有多少？", "来自好学校的成交有多少？", "只看好学校的成交有多少？", "本月已成交线索有多少？", "本月星级分布如何？"} {
		a, events := askScopeQuestion(t, f, q)
		selected := false
		for _, e := range events {
			selected = selected || e.Type == "metric_selected"
		}
		if !selected {
			t.Fatalf("supported question %s rejected: %s", q, a.Content)
		}
	}
}

func TestKnownScopeOriginalBusinessQuestionsDoNotBecomeRefusals(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	for _, q := range []string{"累计成交总金额、累计退款和净成交金额分别是多少？不是现金收入。", "现在每位员工名下各有多少条线索？包含已成交和无效，待领取列为单独一类。"} {
		a, events := askScopeQuestion(t, f, q)
		selected := false
		for _, e := range events {
			selected = selected || e.Type == "metric_selected"
		}
		if !selected {
			t.Fatalf("%s rejected: %s", q, a.Content)
		}
	}
}

func TestCurrentEligibilityCanUseAnExplicitPastDueCutoff(t *testing.T) {
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	f.service.cfg.FixedMetrics = true
	seedOwnerInventory(t, f)
	if err := f.db.Exec(`UPDATE leads SET next_followup_due_at='2026-10-01T01:00:00Z' WHERE id=800`).Error; err != nil {
		t.Fatal(err)
	}
	a, events := askScopeQuestion(t, f, "以线索当前业务状态判断普通回访资格，哪些线索的当前截止时间早于上海时间2026-10-08 10:00？这不等于还原历史状态。")
	selected := false
	for _, e := range events {
		if e.Type == "metric_selected" {
			selected = true
			if !strings.Contains(e.Data, `"due_before":"2026-10-08 10:00"`) || strings.Contains(e.Data, `"date_from"`) {
				t.Fatal(e.Data)
			}
		}
	}
	if !selected || !strings.Contains(a.Content, "1 项普通回访待办") {
		t.Fatal(a.Content)
	}
}
