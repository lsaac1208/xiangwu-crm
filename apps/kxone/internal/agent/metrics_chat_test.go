package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestParseMetricQuestionEverydayPhrases(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, types.BusinessLocation) // Wednesday
	cases := []struct {
		q, metric, from, to, phrase string
	}{
		{"上周共有几条线索进来？", "lead_count", "2026-10-05", "2026-10-11", "上周（10月5日–11日）"},
		{"本月哪个来源最多", "source_performance", "2026-10-01", "2026-10-14", "本月"},
		{"昨天新增多少", "lead_count", "2026-10-13", "2026-10-13", "昨天（10月13日）"},
		{"谁的待办最多", "followup_sla", "", "", ""},
		{"这个月成交了几单", "deal_count", "2026-10-01", "2026-10-14", "本月"},
		{"最近7天每天进了多少线索", "daily_new_leads", "2026-10-08", "2026-10-14", "近7天"},
		{"10月1日到10月7日的线索状态", "lead_status", "2026-10-01", "2026-10-07", "10月1日–7日"},
	}
	for _, c := range cases {
		intent := parseMetricQuestion(c.q, now, nil)
		if !intent.Confident || len(intent.Selections) != 1 {
			t.Fatalf("%s: %+v", c.q, intent)
		}
		sel := intent.Selections[0]
		if sel.MetricID != c.metric || sel.Params.DateFrom != c.from || sel.Params.DateTo != c.to {
			t.Fatalf("%s: %+v", c.q, sel)
		}
		if got := periodPhrase(sel.Params.DateFrom, sel.Params.DateTo, now); got != c.phrase {
			t.Fatalf("%s phrase %q", c.q, got)
		}
	}
	if intent := parseMetricQuestion("你好", now, nil); !intent.Greeting {
		t.Fatal("greeting not recognized")
	}
	if intent := parseMetricQuestion("计算各来源ROI", now, nil); intent.Confident {
		t.Fatal("out-of-scope question treated as common")
	}
	// Follow-ups keep the previous period and filters.
	prev := []MetricSelection{{MetricID: "lead_count", Params: MetricParams{DateFrom: "2026-10-05", DateTo: "2026-10-11"}}}
	for q, metric := range map[string]string{"那哪个来源最多": "source_performance", "按负责人拆开": "owner_current", "那成交了几单": "deal_count"} {
		intent := parseMetricQuestion(q, now, prev)
		if !intent.FollowUp || len(intent.Selections) != 1 || intent.Selections[0].MetricID != metric || intent.Selections[0].Params.DateFrom != "2026-10-05" {
			t.Fatalf("%s: %+v", q, intent)
		}
	}
	sel := MetricSelection{MetricID: "owner_current"}
	if inherited := inheritFollowUpContext(&sel, parseMetricQuestion("按负责人拆开", now, prev), prev, "按负责人拆开"); len(inherited) != 1 || sel.Params.DateTo != "2026-10-11" {
		t.Fatalf("inherit %v %+v", inherited, sel)
	}
	explicit := MetricSelection{MetricID: "source_performance", Params: MetricParams{DateFrom: "2026-10-13", DateTo: "2026-10-13"}}
	if inherited := inheritFollowUpContext(&explicit, parseMetricQuestion("那昨天哪个来源最多", now, prev), prev, "那昨天哪个来源最多"); len(inherited) != 0 || explicit.Params.DateFrom != "2026-10-13" {
		t.Fatalf("explicit period overridden %v %+v", inherited, explicit)
	}
}

func TestMeaningfulContactThreshold(t *testing.T) {
	for _, v := range []string{"*", "202", "无", "138****01", "12345"} {
		if meaningfulContact(v) {
			t.Fatalf("%q treated as a contact", v)
		}
	}
	for _, v := range []string{"13800138001", "test_parent_0001", "wx-ab12cd"} {
		if !meaningfulContact(v) {
			t.Fatalf("%q dropped", v)
		}
	}
}

func TestDirectAnswerTemplates(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, types.BusinessLocation)
	out := func(id string, p MetricParams, cols []string, rows ...[]any) *metricOutput {
		return &metricOutput{Selection: MetricSelection{MetricID: id, Params: p}, Results: []*Result{{Columns: cols, Rows: rows}}}
	}
	week := MetricParams{DateFrom: "2026-10-05", DateTo: "2026-10-11"}
	for _, c := range []struct {
		q    string
		o    *metricOutput
		want string
	}{
		{"上周共有几条线索进来", out("lead_count", week, []string{"新增线索数"}, []any{int64(5)}), "上周（10月5日–11日）一共进来 5 条新线索。"},
		{"昨天新增多少", out("lead_count", MetricParams{DateFrom: "2026-10-13", DateTo: "2026-10-13"}, []string{"新增线索数"}, []any{int64(0)}), "昨天（10月13日）没有新进线索。"},
		{"本月哪个来源最多", out("source_performance", MetricParams{DateFrom: "2026-10-01", DateTo: "2026-10-14"}, []string{"来源", "线索数"}, []any{"抖音", int64(2)}, []any{"好学校", int64(4)}), "本月按来源看，「好学校」最多，共 4 条，其次是「抖音」（2 条）；共 2 个来源、6 条线索。"},
		{"哪个来源最多", out("source_performance", MetricParams{}, []string{"来源", "线索数"}, []any{"A", int64(1)}, []any{"B", int64(1)}), "按来源看，「A」和「B」并列最多，各 1 条；共 2 个来源、2 条线索。"},
		{"谁的待办最多", out("followup_sla", MetricParams{}, []string{"当前负责人", "当前普通回访待办", "其中已逾期"}, []any{"小宇", int64(2), int64(1)}, []any{"员工乙", int64(3), int64(1)}), "目前普通回访待办「员工乙」最多，共 3 项，其次是「小宇」（2 项）；全部共 5 项，其中已逾期 2 项。"},
		{"这个月成交了几单", out("deal_count", MetricParams{DateFrom: "2026-10-01", DateTo: "2026-10-14"}, []string{"成交数", "成交金额(元)", "净成交额(元)"}, []any{int64(2), 130.0, 130.0}), "本月成交了 2 单，成交金额 130 元（按成交日期统计）。"},
		{"只看好学校按负责人拆开", out("owner_current", MetricParams{Sources: []string{"好学校"}, ExcludeSources: []string{"转介绍"}}, []string{"当前归属", "线索数"}, []any{"小宇", int64(1)}, []any{"员工乙", int64(1)}), "只看来源「好学校」、排除「转介绍」，按当前负责人拆开：小宇 1 条、员工乙 1 条。"},
		{"上周好学校几条", out("lead_count", MetricParams{DateFrom: "2026-10-05", DateTo: "2026-10-11", Sources: []string{"好学校"}}, []string{"新增线索数"}, []any{int64(2)}), "上周（10月5日–11日）只看来源「好学校」，一共进来 2 条新线索。"},
		{"按负责人拆开", out("owner_current", week, []string{"当前归属", "线索数"}, []any{"员工乙", int64(3)}, []any{"小宇", int64(1)}), "上周（10月5日–11日）按当前负责人拆开：员工乙 3 条、小宇 1 条。"},
	} {
		if got := directAnswer(c.q, c.o, now); got != c.want {
			t.Fatalf("%s:\n got %s\nwant %s", c.q, got, c.want)
		}
	}
}

func seedMetricLeads(t *testing.T, f *agentSecurityFixture, now time.Time) {
	t.Helper()
	today := dayStart(now)
	monday := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
	lastWeek := monday.AddDate(0, 0, -6).Add(10 * time.Hour) // last Tuesday 10:00
	// Short placeholder contacts reproduce the production false positive:
	// "202" occurs inside every 2026 date argument.
	for i, contact := range [][2]string{{"*", "无"}, {"202", "*"}, {"无", ""}, {"13900139012", "real_wechat_id"}} {
		created := lastWeek.Add(time.Duration(i) * time.Hour).UTC()
		if err := f.db.Exec(`INSERT INTO leads(id,lead_no,customer_name,customer_mobile,customer_wechat,source_channel,status,business_stage,ownership_state,result_status,current_owner_user_id,current_owner_role,deal_amount,refund_amount,payment_status,created_at,updated_at,current_owner_started_at,next_followup_due_at) VALUES (?,?,?,?,?,?,'following','invitation','private','active','admin-a','inviter',0,0,'unpaid',?,?,?,?)`,
			100+i, "chat-lead-"+string(rune('a'+i)), "测试客户", contact[0], contact[1], []string{"好学校", "好学校", "抖音", "好学校"}[i], created, created, created, created.Add(48*time.Hour)).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestShortContactsDoNotBlockFixedMetricSelection(t *testing.T) {
	now := time.Now().In(types.BusinessLocation)
	today := dayStart(now)
	monday := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
	from, to := ymd(monday.AddDate(0, 0, -7)), ymd(monday.AddDate(0, 0, -1))
	var calls atomic.Int32
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []any{map[string]any{"metric_id": "lead_count", "params": map[string]any{"date_from": from, "date_to": to, "owner": "13900139012"}}}}))
		case 2:
			// Follow-up: the model forgets the period; the server inherits it.
			finalResultFixtureReply(w, "", metricToolCall("select_metrics", map[string]any{"selections": []any{map[string]any{"metric_id": "source_performance"}}}))
		default:
			t.Error("unexpected extra model call (simple questions need no interpretation)")
			w.WriteHeader(500)
		}
	})
	f.service.cfg.FixedMetrics = true
	seedMetricLeads(t, f, now)
	c := securityCreateConversation(t, f, securityAdmin)
	run, err := f.service.Start(t.Context(), securityAdmin, c.ID, "上周共有几条线索进来？", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	done := securityWaitRun(t, f, run.ID)
	var answer Message
	f.db.Where("run_id=? AND role=?", run.ID, "assistant").First(&answer)
	if done.Status != "completed" || !strings.HasPrefix(answer.Content, "上周（") || !strings.Contains(answer.Content, "一共进来 4 条新线索。") {
		t.Fatalf("run %+v answer %s", done, answer.Content)
	}
	var events []Event
	f.db.Where("run_id=? AND type IN ?", run.ID, []string{"provider_privacy_rejection", "metric_params_dropped"}).Find(&events)
	if len(events) != 1 || events[0].Type != "metric_params_dropped" || !strings.Contains(events[0].Data, `"param":"owner"`) || !strings.Contains(events[0].Data, `"rule":"known_contact"`) || strings.Contains(events[0].Data, "13900139012") {
		t.Fatalf("events %+v", events)
	}
	second, err := f.service.Start(t.Context(), securityAdmin, c.ID, "那哪个来源最多", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if done := securityWaitRun(t, f, second.ID); done.Status != "completed" {
		t.Fatalf("follow-up %+v", done)
	}
	answer = Message{}
	f.db.Where("run_id=? AND role=?", second.ID, "assistant").First(&answer)
	if !strings.Contains(answer.Content, "按来源看，「好学校」最多，共 3 条") || !strings.Contains(answer.Content, "上周（") {
		t.Fatalf("follow-up answer %s", answer.Content)
	}
	var inherited Event
	if f.db.Where("run_id=? AND type=?", second.ID, "context_inherited").First(&inherited).Error != nil || !strings.Contains(inherited.Data, "dates") {
		t.Fatal("follow-up did not inherit the period")
	}
}

func TestDeclinedOrBlockedCommonQuestionStillAnswers(t *testing.T) {
	now := time.Now().In(types.BusinessLocation)
	var calls atomic.Int32
	f := newAgentSecurityFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req ModelRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		// A refusal whose reason leaks a known contact: the reason is dropped
		// and the server parser answers the common question instead.
		finalResultFixtureReply(w, "", metricToolCall("cannot_answer", map[string]any{"reason": "请联系13900139012"}))
	})
	f.service.cfg.FixedMetrics = true
	seedMetricLeads(t, f, now)
	c := securityCreateConversation(t, f, securityAdmin)
	run, _ := f.service.Start(t.Context(), securityAdmin, c.ID, "上周共有几条线索进来？", uuid.NewString())
	done := securityWaitRun(t, f, run.ID)
	var answer Message
	f.db.Where("run_id=? AND role=?", run.ID, "assistant").First(&answer)
	if done.Status != "completed" || !strings.Contains(answer.Content, "一共进来 4 条新线索") || strings.Contains(answer.Content, "13900139012") {
		t.Fatalf("%+v %s", done, answer.Content)
	}
	greet, _ := f.service.Start(t.Context(), securityAdmin, c.ID, "你好", uuid.NewString())
	if done := securityWaitRun(t, f, greet.ID); done.Status != "completed" || calls.Load() != 1 {
		t.Fatalf("greeting %+v calls=%d", done, calls.Load())
	}
	answer = Message{}
	f.db.Where("run_id=? AND role=?", greet.ID, "assistant").First(&answer)
	if !strings.HasPrefix(answer.Content, "你好！") {
		t.Fatalf("greeting %s", answer.Content)
	}
}

func TestShortNamesNeverRewriteDates(t *testing.T) {
	r := newRedactor()
	for i, name := range []string{"20", "26", "10", "14", "李", "Amy"} {
		r.addName(name, fmt.Sprintf("客户#%d", 580+i))
	}
	r.addName("张小明", "客户#9")
	r.prepare()
	for _, text := range []string{"2026-10-14", "今天（10月14日）", "2026-10-14 10:44:19", "当前上海时间2026-10-14T14:05:00+08:00", "Amya", "共20条线索"} {
		if got, _ := r.sanitize(text); got != text {
			t.Errorf("%q rewritten to %q", text, got)
		}
	}
	for text, want := range map[string]string{"客户20来电": "客户客户#580来电", "20": "客户#580", "联系人 Amy 已到访": "联系人 客户#585 已到访", "张小明报名": "客户#9报名"} {
		if got, _ := r.sanitize(text); got != want {
			t.Errorf("%q => %q, want %q", text, got, want)
		}
	}
}

func TestCorrectionFollowUpAndParserPriority(t *testing.T) {
	now := time.Date(2026, 10, 14, 14, 5, 0, 0, types.BusinessLocation)
	prev := []MetricSelection{{MetricID: "lead_count"}}
	intent := parseMetricQuestion("不是，我说说今天，单单是今天", now, prev)
	if !intent.FollowUp || !intent.Correction || len(intent.Selections) != 1 || intent.Selections[0].MetricID != "lead_count" || intent.Selections[0].Params.DateFrom != "2026-10-14" || intent.Selections[0].Params.DateTo != "2026-10-14" {
		t.Fatalf("correction intent %+v", intent)
	}
	today := parseMetricQuestion("今天有几条线索进来？", now, nil)
	// The model's dates were dropped (or wrong): the parser's period wins.
	got, overrides, unresolved := applyParserPriority([]MetricSelection{{MetricID: "lead_count"}}, today, true)
	if unresolved || len(overrides) != 1 || got[0].Params.DateFrom != "2026-10-14" || got[0].Params.DateTo != "2026-10-14" {
		t.Fatalf("today override %+v %v %v", got, overrides, unresolved)
	}
	got, overrides, _ = applyParserPriority([]MetricSelection{{MetricID: "lead_count", Params: MetricParams{DateFrom: "2026-10-01", DateTo: "2026-10-14"}}}, today, false)
	if len(overrides) != 1 || got[0].Params.DateFrom != "2026-10-14" {
		t.Fatalf("model dates must not override a parsed period %+v", got)
	}
	// An unrelated model metric is replaced by the confident parser metric.
	got, overrides, _ = applyParserPriority([]MetricSelection{{MetricID: "overview"}}, today, false)
	if got[0].MetricID != "lead_count" || overrides[0] != "metric" {
		t.Fatalf("metric override %+v %v", got, overrides)
	}
	// Other legitimate model choices are kept (M13: lead_followup_status).
	todo := parseMetricQuestion("哪些线索该回访了", now, nil)
	got, overrides, _ = applyParserPriority([]MetricSelection{{MetricID: "lead_followup_status"}}, todo, false)
	if got[0].MetricID != "lead_followup_status" || len(overrides) != 0 {
		t.Fatalf("legitimate model metric replaced %+v %v", got, overrides)
	}
	// Dates dropped and no parsed period: never silently answer over all data.
	vague := parseMetricQuestion("那段时间几条线索", now, nil)
	if _, _, unresolved := applyParserPriority([]MetricSelection{{MetricID: "lead_count"}}, vague, true); !unresolved {
		t.Fatal("dropped dates without a parsed period must be unresolved")
	}
	for q, want := range map[string][2]string{"昨天新增多少": {"2026-10-13", "2026-10-13"}, "上周共有几条线索进来": {"2026-10-05", "2026-10-11"}, "本月新增多少线索": {"2026-10-01", "2026-10-14"}, "最近7天进了多少线索": {"2026-10-08", "2026-10-14"}} {
		in := parseMetricQuestion(q, now, nil)
		if !in.HasPeriod || in.PeriodFrom != want[0] || in.PeriodTo != want[1] {
			t.Errorf("%s => %s..%s", q, in.PeriodFrom, in.PeriodTo)
		}
	}
}

func TestParserFollowupKeywordFallback(t *testing.T) {
	now := time.Date(2026, 10, 14, 14, 5, 0, 0, types.BusinessLocation)
	for q, want := range map[string]string{"跟进里提到费用问题的线索有哪些？是否足以证明费用造成流失？": "费用", "跟进记录中提及“学费”的有几条": "学费", "跟进里说到顾虑的线索": "顾虑"} {
		in := parseMetricQuestion(q, now, nil)
		if len(in.Selections) != 1 || in.Selections[0].MetricID != "followup_keyword" || in.Selections[0].Params.Keyword != want {
			t.Errorf("%s => %+v", q, in.Selections)
		}
	}
	if in := parseMetricQuestion("今天有几条线索进来？", now, nil); in.Selections[0].MetricID != "lead_count" {
		t.Fatalf("count question changed: %+v", in.Selections)
	}
}
