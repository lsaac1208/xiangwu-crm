package agent

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

// Conversational layer for fixed metrics: a deterministic question parser
// (used when the model fails, declines a common question or is blocked),
// natural period phrases, and server-side direct answers built only from
// server results. Nothing here calls a model.

type metricIntent struct {
	Greeting   bool
	Selections []MetricSelection
	// Explicit time phrase found in the question (dates were set from it).
	HasPeriod bool
	// The parsed Shanghai date range when HasPeriod.
	PeriodFrom, PeriodTo string
	// The user corrects the previous turn ("不是，单单是今天").
	Correction bool
	FollowUp   bool
	// Topic keyword matched; without it only a follow-up can be answered.
	Confident bool
	Analytic  bool
}

var (
	countQuantityPattern = regexp.MustCompile(`多少|几条|几单|几笔|几项|数量|总数|总共|一共|累计|总计|合计|有几|共几`)
	greetingPattern      = regexp.MustCompile(`^(?:你好|您好|hi|hello|嗨|哈喽|在吗|在不在|早上好|下午好|晚上好|早|谢谢|多谢|辛苦了|好的|收到)[呀啊哈～~！!。.？? ]*$`)
	followUpPattern      = regexp.MustCompile(`^(?:那|那么|再|还有|其中|另外|然后|接着|分别|按|拆|换成|改成|只看|如果)|拆开|拆分|分别|呢[？?]?$`)
	outOfScopePattern    = regexp.MustCompile(`(?i)roi|投放|成本|投入产出|花费|广告费|去重|家庭|现金|实收|到账|修改|删除|改成|清空|密码|密钥|key|token|预测|预计|下个月会`)
	analyticPattern      = regexp.MustCompile(`为什么|原因|分析|比较|对比|表现|证明|是否|怎么样|如何|建议|趋势|解读|顾虑|问题|影响|差异|说明`)
	mentionPattern       = regexp.MustCompile(`(?:提到|提及|说到|谈到|涉及|包含|关键词)[“"「]?([\p{Han}A-Za-z0-9]{1,8}?)[”"」]?(?:问题|方面|相关|的|顾虑|这类|字样|$|[，,。？?])`)
	correctionPattern    = regexp.MustCompile(`^(?:不是|不对|错了|我是说|我说的是|我问的是|我说)|单单|仅仅|只要|只算|就是说`)
	recentDaysPhrase     = regexp.MustCompile(`(?:近|最近|过去)\s*([0-9一二两三四五六七八九十]+)\s*(?:天|日)`)
	monthDayPhrase       = regexp.MustCompile(`(\d{1,2})\s*月\s*(\d{1,2})\s*[日号]?\s*(?:到|至|-|—|~|～)\s*(?:(\d{1,2})\s*月\s*)?(\d{1,2})\s*[日号]?`)
)

// This is a degraded-path completeness check, not an understanding engine:
// merely mentioning the data topic cannot justify replacing a missing plan
// with an inventory number. Normal model plans do not use this parser.
func incompleteCountFallback(question string, intent metricIntent) bool {
	return len(intent.Selections) > 0 && intent.Selections[0].MetricID == "lead_count" && !countQuantityPattern.MatchString(question)
}

func chineseSmallNumber(s string) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	digits := map[rune]int{'一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	r := []rune(s)
	switch {
	case len(r) == 1 && r[0] == '十':
		return 10
	case len(r) == 1:
		return digits[r[0]]
	case len(r) == 2 && r[0] == '十':
		return 10 + digits[r[1]]
	case len(r) == 2 && r[1] == '十':
		return digits[r[0]] * 10
	case len(r) == 3 && r[1] == '十':
		return digits[r[0]]*10 + digits[r[2]]
	}
	return 0
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.In(types.BusinessLocation).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, types.BusinessLocation)
}

func ymd(t time.Time) string { return t.Format("2006-01-02") }

// parsePeriod maps a Chinese time phrase to Shanghai dates.
func parsePeriod(q string, now time.Time) (from, to string, ok bool) {
	if dates := regexp.MustCompile(`\d{4}-\d{2}-\d{2}`).FindAllString(q, -1); len(dates) > 0 {
		if len(dates) > 2 {
			return "", "", false
		}
		for _, date := range dates {
			if !validDate(date) {
				return "", "", false
			}
		}
		if len(dates) == 1 {
			return dates[0], dates[0], true
		}
		return dates[0], dates[1], dates[0] <= dates[1]
	}
	today := dayStart(now)
	weekday := (int(today.Weekday()) + 6) % 7 // Monday=0
	monday := today.AddDate(0, 0, -weekday)
	first := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, types.BusinessLocation)
	if m := monthDayPhrase.FindStringSubmatch(q); m != nil {
		m1, _ := strconv.Atoi(m[1])
		d1, _ := strconv.Atoi(m[2])
		m2 := m1
		if m[3] != "" {
			m2, _ = strconv.Atoi(m[3])
		}
		d2, _ := strconv.Atoi(m[4])
		start := time.Date(today.Year(), time.Month(m1), d1, 0, 0, 0, 0, types.BusinessLocation)
		end := time.Date(today.Year(), time.Month(m2), d2, 0, 0, 0, 0, types.BusinessLocation)
		if start.After(today) {
			start, end = start.AddDate(-1, 0, 0), end.AddDate(-1, 0, 0)
		}
		if !end.Before(start) && int(start.Month()) == m1 && start.Day() == d1 && int(end.Month()) == m2 && end.Day() == d2 && m1 >= 1 && m1 <= 12 && m2 >= 1 && m2 <= 12 {
			return ymd(start), ymd(end), true
		}
	}
	if m := recentDaysPhrase.FindStringSubmatch(q); m != nil {
		if n := chineseSmallNumber(m[1]); n > 0 && n <= 366 {
			return ymd(today.AddDate(0, 0, -(n - 1))), ymd(today), true
		}
	}
	switch {
	case strings.Contains(q, "前天"):
		d := today.AddDate(0, 0, -2)
		return ymd(d), ymd(d), true
	case strings.Contains(q, "昨天") || strings.Contains(q, "昨日"):
		d := today.AddDate(0, 0, -1)
		return ymd(d), ymd(d), true
	case strings.Contains(q, "今天") || strings.Contains(q, "今日"):
		return ymd(today), ymd(today), true
	case strings.Contains(q, "上上周"):
		return ymd(monday.AddDate(0, 0, -14)), ymd(monday.AddDate(0, 0, -8)), true
	case strings.Contains(q, "上周") || strings.Contains(q, "上个星期") || strings.Contains(q, "上星期") || strings.Contains(q, "上礼拜") || strings.Contains(q, "上个礼拜"):
		return ymd(monday.AddDate(0, 0, -7)), ymd(monday.AddDate(0, 0, -1)), true
	case strings.Contains(q, "本周") || strings.Contains(q, "这周") || strings.Contains(q, "这个星期") || strings.Contains(q, "这星期") || strings.Contains(q, "本星期") || strings.Contains(q, "这礼拜"):
		return ymd(monday), ymd(today), true
	case strings.Contains(q, "上个月") || strings.Contains(q, "上月"):
		prev := first.AddDate(0, -1, 0)
		return ymd(prev), ymd(first.AddDate(0, 0, -1)), true
	case strings.Contains(q, "本月") || strings.Contains(q, "这个月") || strings.Contains(q, "这月") || strings.Contains(q, "当月"):
		return ymd(first), ymd(today), true
	case strings.Contains(q, "今年") || strings.Contains(q, "本年"):
		return ymd(time.Date(today.Year(), 1, 1, 0, 0, 0, 0, types.BusinessLocation)), ymd(today), true
	}
	return "", "", false
}

func containsAny(q string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(q, w) {
			return true
		}
	}
	return false
}

// parseMetricQuestion is the deterministic understanding of everyday
// questions. previous holds the last turn's selections for follow-ups.
func parseMetricQuestion(question string, now time.Time, previous []MetricSelection) metricIntent {
	q := strings.ToLower(strings.TrimSpace(question))
	intent := metricIntent{Analytic: analyticPattern.MatchString(q)}
	if greetingPattern.MatchString(q) {
		intent.Greeting = true
		return intent
	}
	from, to, hasPeriod := parsePeriod(q, now)
	intent.HasPeriod = hasPeriod
	intent.PeriodFrom, intent.PeriodTo = from, to
	intent.Correction = len(previous) > 0 && correctionPattern.MatchString(q)
	intent.FollowUp = len(previous) > 0 && (intent.Correction || followUpPattern.MatchString(q) || (!hasPeriod && len([]rune(q)) <= 14))
	metric := ""
	keyword := ""
	switch {
	case containsAny(q, "跟进", "沟通", "记录") && mentionPattern.MatchString(q):
		// "跟进里提到费用问题的线索" asks for a keyword search, never a count.
		if m := mentionPattern.FindStringSubmatch(q); m != nil {
			keyword = m[1]
		}
		if keyword != "" {
			metric = "followup_keyword"
		}
	case containsAny(q, "待办", "回访", "逾期", "该跟进", "要跟进", "首跟", "时效"):
		metric = "followup_sla"
	case containsAny(q, "跟进") && containsAny(q, "谁", "每个人", "员工", "操作人", "多少条跟进", "几条跟进", "跟进了", "跟进多少", "跟进次数", "跟进条数"):
		metric = "followup_by_actor"
	case ownerRankingQuestion(q):
		metric = "owner_current"
	case containsAny(q, "来源", "渠道"):
		metric = "source_performance"
	case containsAny(q, "负责人", "销售", "按人", "每个人", "员工", "归属", "谁的线索", "谁手上", "谁名下"):
		metric = "owner_current"
	case containsAny(q, "成交", "签单", "成单", "几单", "多少单", "开单", "出单"):
		metric = "deal_count"
		if !hasPeriod && !intent.FollowUp {
			metric = "overview"
		}
	case containsAny(q, "预约", "到访"):
		metric = "appointment_status"
	case containsAny(q, "评级", "星级", "几星"):
		metric = "rating_distribution"
	case containsAny(q, "状态", "漏斗"):
		metric = "lead_status"
	case containsAny(q, "每天", "每日", "趋势", "走势", "按天", "逐日"):
		metric = "daily_new_leads"
	case containsAny(q, "总览", "概况", "整体", "汇总", "大盘"):
		metric = "overview"
	case containsAny(q, "线索", "新增", "进来", "进了", "录入", "多少条", "几条", "新客", "来了多少"):
		metric = "lead_count"
	}
	intent.Confident = metric != "" && !outOfScopePattern.MatchString(q)
	params := MetricParams{DateFrom: from, DateTo: to, Keyword: keyword}
	if intent.FollowUp && len(previous) > 0 {
		prev := previous[0].Params
		if !hasPeriod {
			params.DateFrom, params.DateTo = prev.DateFrom, prev.DateTo
		}
		if !containsAny(q, "全部", "所有", "不限") {
			params.Sources, params.ExcludeSources, params.Owner = prev.Sources, prev.ExcludeSources, prev.Owner
		}
		if metric == "" {
			metric = previous[0].MetricID
		}
	}
	if metric == "" {
		return intent
	}
	if metric == "followup_sla" {
		// Current to-dos are a present-day state; a creation-date filter would
		// silently hide older leads that are still due.
		params.DateFrom, params.DateTo = "", ""
	}
	if m := findMetric(metric); m != nil {
		params, _ = normalizeMetricParams(m, params)
	}
	intent.Selections = []MetricSelection{{MetricID: metric, Params: params}}
	return intent
}

// inheritFollowUpContext fills filters the model left empty on a follow-up
// turn from the previous turn's selection. It returns the inherited names.
func inheritFollowUpContext(sel *MetricSelection, intent metricIntent, previous []MetricSelection, question string) []string {
	if !intent.FollowUp || len(previous) == 0 {
		return nil
	}
	m := findMetric(sel.MetricID)
	if m == nil {
		return nil
	}
	prev := previous[0].Params
	allowed := map[string]bool{}
	for _, name := range m.Params {
		allowed[name] = true
	}
	inherited := []string{}
	if !intent.HasPeriod && sel.Params.DateFrom == "" && sel.Params.DateTo == "" && (prev.DateFrom != "" || prev.DateTo != "") && allowed["date_from"] && sel.MetricID != "followup_sla" {
		sel.Params.DateFrom, sel.Params.DateTo = prev.DateFrom, prev.DateTo
		inherited = append(inherited, "dates")
	}
	if containsAny(question, "全部", "所有", "不限") {
		return inherited
	}
	if len(sel.Params.Sources) == 0 && len(sel.Params.ExcludeSources) == 0 {
		if len(prev.Sources) > 0 && allowed["sources"] {
			sel.Params.Sources = prev.Sources
			inherited = append(inherited, "sources")
		}
		if len(prev.ExcludeSources) > 0 && allowed["exclude_sources"] {
			sel.Params.ExcludeSources = prev.ExcludeSources
			inherited = append(inherited, "exclude_sources")
		}
	}
	if sel.Params.Owner == "" && prev.Owner != "" && allowed["owner"] {
		sel.Params.Owner = prev.Owner
		inherited = append(inherited, "owner")
	}
	return inherited
}

// periodPhrase renders dates the way people say them.
func periodPhrase(from, to string, now time.Time) string {
	if from == "" && to == "" {
		return ""
	}
	today := dayStart(now)
	weekday := (int(today.Weekday()) + 6) % 7
	monday := today.AddDate(0, 0, -weekday)
	first := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, types.BusinessLocation)
	short := func(s string) string {
		t, err := time.ParseInLocation("2006-01-02", s, types.BusinessLocation)
		if err != nil {
			return s
		}
		return fmt.Sprintf("%d月%d日", t.Month(), t.Day())
	}
	span := func() string {
		if from == to {
			return short(from)
		}
		a, b := short(from), short(to)
		if from[:7] == to[:7] && len(to) == 10 {
			d, _ := strconv.Atoi(to[8:])
			b = fmt.Sprintf("%d日", d)
		}
		return a + "–" + b
	}
	switch {
	case from == ymd(today) && to == ymd(today):
		return "今天（" + short(from) + "）"
	case from == ymd(today.AddDate(0, 0, -1)) && to == from:
		return "昨天（" + short(from) + "）"
	case from == ymd(today.AddDate(0, 0, -2)) && to == from:
		return "前天"
	case from == ymd(monday.AddDate(0, 0, -7)) && to == ymd(monday.AddDate(0, 0, -1)):
		return "上周（" + span() + "）"
	case from == ymd(monday) && to == ymd(today):
		return "本周"
	case from == ymd(first) && to == ymd(today):
		return "本月"
	case from == ymd(first.AddDate(0, -1, 0)) && to == ymd(first.AddDate(0, 0, -1)):
		return "上个月"
	case from != "" && to == ymd(today) && from != to:
		if t, err := time.ParseInLocation("2006-01-02", from, types.BusinessLocation); err == nil {
			return fmt.Sprintf("近%d天", int(today.Sub(t).Hours()/24)+1)
		}
	case from == "":
		return short(to) + "之前"
	case to == "":
		return short(from) + "以来"
	}
	return span()
}

func cellNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case int:
		return float64(x), true
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}
	return 0, false
}

func formatNumber(f float64) string {
	if f == math.Trunc(f) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func formatMetricYuan(r *Result, column string) string {
	value, known := firstCell(r, column)
	if !known {
		return "未知（金额资料不完整）"
	}
	return formatYuan(value)
}

func formatYuan(f float64) string {
	if f == math.Trunc(f) {
		return strconv.FormatInt(int64(f), 10) + " 元"
	}
	return strconv.FormatFloat(f, 'f', 2, 64) + " 元"
}

type namedCount struct {
	name  string
	value float64
	extra map[string]float64
}

func resultColumn(r *Result, name string) int {
	for i, c := range r.Columns {
		if c == name {
			return i
		}
	}
	return -1
}

func rankRows(r *Result, label, value string, extras ...string) []namedCount {
	li, vi := resultColumn(r, label), resultColumn(r, value)
	if li < 0 || vi < 0 {
		return nil
	}
	out := []namedCount{}
	for _, row := range r.Rows {
		if li >= len(row) || vi >= len(row) {
			continue
		}
		v, _ := cellNumber(row[vi])
		item := namedCount{name: fmt.Sprint(row[li]), value: v, extra: map[string]float64{}}
		for _, e := range extras {
			if ei := resultColumn(r, e); ei >= 0 && ei < len(row) {
				item.extra[e], _ = cellNumber(row[ei])
			}
		}
		out = append(out, item)
	}
	// Stable insertion sort, descending by value.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].value > out[j-1].value; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func firstCell(r *Result, column string) (float64, bool) {
	i := resultColumn(r, column)
	if r == nil || i < 0 || len(r.Rows) == 0 || i >= len(r.Rows[0]) {
		return 0, false
	}
	return cellNumber(r.Rows[0][i])
}

// filterPhrase renders active filters as a natural lead-in ending with "，".
func filterPhrase(p MetricParams) string {
	parts := []string{}
	if len(p.Sources) > 0 {
		parts = append(parts, "只看来源「"+strings.Join(p.Sources, "」「")+"」")
	}
	if len(p.ExcludeSources) > 0 {
		parts = append(parts, "排除「"+strings.Join(p.ExcludeSources, "」「")+"」")
	}
	if p.Owner != "" {
		parts = append(parts, "只看"+p.Owner+"名下")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "、") + "，"
}

func listTop(items []namedCount, unit string, limit int) string {
	parts := []string{}
	for i, it := range items {
		if i >= limit {
			parts = append(parts, fmt.Sprintf("等 %d 项", len(items)))
			break
		}
		parts = append(parts, fmt.Sprintf("%s %s %s", it.name, formatNumber(it.value), unit))
	}
	return strings.Join(parts, "、")
}

func topPhrase(items []namedCount, unit string) (string, bool) {
	if len(items) == 0 || (items[0].value == 0 && unit != "元") {
		return "", false
	}
	tied := []string{}
	for _, it := range items {
		if it.value == items[0].value {
			tied = append(tied, "「"+it.name+"」")
		}
	}
	if len(tied) > 1 {
		return fmt.Sprintf("%s并列最多，各 %s %s", strings.Join(tied, "和"), formatNumber(items[0].value), unit), true
	}
	text := fmt.Sprintf("「%s」最多，共 %s %s", items[0].name, formatNumber(items[0].value), unit)
	if len(items) > 1 && items[1].value > 0 {
		text += fmt.Sprintf("，其次是「%s」（%s %s）", items[1].name, formatNumber(items[1].value), unit)
	}
	return text, true
}

func withSubject(period, rest string) string {
	if period == "" {
		return rest
	}
	return period + rest
}

// A person ranking needs the grouped owner facts, even when the provider is
// unavailable. A global count cannot answer who has the largest count.
func ownerRankingQuestion(q string) bool {
	return containsAny(q, "谁", "哪位", "哪个人", "哪些人", "负责人", "员工", "销售", "每个人") &&
		containsAny(q, "最多", "最高", "最大", "排名", "排行", "谁第一", "第一名") &&
		containsAny(q, "线索", "成交", "签单", "成单", "开单", "出单", "净额", "转化率")
}

func ownerRankingColumn(q string) string {
	if !ownerRankingQuestion(q) {
		return ""
	}
	if containsAny(q, "净成交额", "净额") {
		return "净成交额(元)"
	}
	if containsAny(q, "成交率", "转化率", "比例", "占比", "成交金额", "成交额") {
		return ""
	}
	if containsAny(q, "成交", "签单", "成单", "开单", "出单") {
		return "成交数"
	}
	return "线索数"
}

type ownerRanking struct {
	column, unit, subject string
	position              int
}

func ownerRankingSpec(column string) (ownerRanking, bool) {
	switch column {
	case "线索数":
		return ownerRanking{column, "条", "当前名下线索数", 2}, true
	case "成交数":
		return ownerRanking{column, "单", "当前归属的成交数", 3}, true
	case "净成交额(元)":
		return ownerRanking{column, "元", "当前归属的净成交额", 5}, true
	case "成交率(%)":
		return ownerRanking{column, "%", "当前名下线索的成交转化率", 6}, true
	case "成交金额(元)":
		return ownerRanking{column, "元", "当前归属的成交金额", 6}, true
	case "退款金额(元)":
		return ownerRanking{column, "元", "当前归属的退款金额", 6}, true
	default:
		return ownerRanking{}, false
	}
}

// directAnswer writes one natural sentence from the first server result of a
// metric. All numbers come from the result; it never consults model text.
func directAnswer(question string, out *metricOutput, now time.Time) string {
	if out == nil || len(out.Results) == 0 {
		return ""
	}
	r := out.Results[0]
	p := out.Selection.Params
	period := periodPhrase(p.DateFrom, p.DateTo, now)
	filters := filterPhrase(p)
	if out.Selection.Comparison != nil {
		return comparisonAnswer(out, now)
	}
	switch out.Selection.MetricID {
	case "owner_inventory":
		n, _ := firstCell(r, "名下线索数")
		pending, _ := firstCell(r, "另有待领取")
		owner := p.Owner
		if owner == "" {
			owner = "全部负责人"
		}
		filterParams := p
		filterParams.Owner = ""
		prefix := filterPhrase(filterParams)
		if period != "" {
			prefix = period + "录入队列中，" + prefix
		}
		return prefix + fmt.Sprintf("%s当前名下共有 %s 条线索（含成交、无效、关闭，状态拆分见下表）；另有 %s 条已分配待领取，不计入名下总数。", owner, formatNumber(n), formatNumber(pending))
	case "lead_count":
		n, _ := firstCell(r, "新增线索数")
		if period == "" {
			return filters + fmt.Sprintf("目前累计共有 %s 条线索（未限定日期）。", formatNumber(n))
		}
		if n == 0 {
			return withSubject(period, filters+"没有新进线索。")
		}
		return withSubject(period, filters+fmt.Sprintf("一共进来 %s 条新线索。", formatNumber(n)))
	case "deal_count":
		n, _ := firstCell(r, "成交数")
		if n == 0 {
			return withSubject(period, filters+"还没有成交（按成交日期统计）。")
		}
		amount, amountKnown := firstCell(r, "成交金额(元)")
		net, netKnown := firstCell(r, "净成交额(元)")
		text := withSubject(period, filters+fmt.Sprintf("成交了 %s 单，成交金额 %s", formatNumber(n), formatMetricYuan(r, "成交金额(元)")))
		if !netKnown || (amountKnown && net != amount) {
			text += "，扣除退款后净额 " + formatMetricYuan(r, "净成交额(元)")
		}
		return text + "（按成交日期统计）。"
	case "daily_new_leads":
		items := rankRows(r, "录入日期(上海)", "线索数")
		total := 0.0
		for _, it := range items {
			total += it.value
		}
		if total == 0 {
			return withSubject(period, filters+"没有新进线索。")
		}
		return withSubject(period, filters+fmt.Sprintf("合计 %s 条新线索，最多的一天是 %s（%s 条）。", formatNumber(total), items[0].name, formatNumber(items[0].value)))
	case "overview":
		n, _ := firstCell(r, "线索数")
		deals, _ := firstCell(r, "成交数")
		rate, rateOK := firstCell(r, "成交率(%)")
		if n == 0 {
			return withSubject(period, filters+"没有线索记录。")
		}
		subject := "目前"
		if period != "" {
			subject = period + "录入的"
		}
		text := fmt.Sprintf("%s%s线索共 %s 条，其中当前已成交 %s 单", filters, subject, formatNumber(n), formatNumber(deals))
		if rateOK {
			text += "（成交率 " + formatNumber(rate) + "%）"
		}
		return text + "，净成交额 " + formatMetricYuan(r, "净成交额(元)") + "。"
	case "source_performance", "source_deals":
		value, unit, what := "线索数", "条", "线索"
		if containsAny(question, "成交", "签单", "成单", "几单") {
			value, unit, what = "成交数", "单", "成交"
		}
		items := rankRows(r, "来源", value)
		total := 0.0
		for _, it := range items {
			total += it.value
		}
		top, ok := topPhrase(items, unit)
		if !ok {
			return withSubject(period, filters+"没有"+what+"记录。")
		}
		text := withSubject(period, filters+"按来源看，"+top)
		if out.Selection.MetricID == "source_performance" && period != "" && value == "成交数" {
			text = period + "录入线索的当前表现：" + filters + "按来源看，" + top
		}
		if len(items) > 1 {
			text += fmt.Sprintf("；共 %d 个来源、%s %s%s", len(items), formatNumber(total), unit, what)
		}
		if out.Selection.MetricID == "source_deals" {
			text += "（按成交日期统计）"
		}
		return text + "。"
	case "owner_current":
		if rank, ok := ownerRankingSpec(ownerRankingColumn(question)); ok {
			prefix := filters + "按" + rank.subject + "比较，"
			if period != "" {
				prefix = period + "录入线索中，" + prefix
			}
			if rank.unit == "元" {
				index := resultColumn(r, rank.column)
				for _, row := range r.Rows {
					if index < 0 || index >= len(row) {
						return prefix + "金额资料不完整，暂时无法确定排名。"
					}
					if _, known := cellNumber(row[index]); !known {
						return prefix + "金额资料不完整，暂时无法确定排名。"
					}
				}
			}
			items := rankRows(r, "当前归属", rank.column)
			if top, ok := topPhrase(items, rank.unit); ok {
				return prefix + top + "。"
			}
			return prefix + "目前没有可排名的记录。"
		}
		items := rankRows(r, "当前归属", "线索数")
		if len(items) == 0 {
			return withSubject(period, filters+"没有线索记录。")
		}
		return withSubject(period, filters+"按当前负责人拆开："+listTop(items, "条", 6)+"。")
	case "followup_sla", "owner_first_response":
		if out.Selection.MetricID != "owner_first_response" && !containsAny(question, "首跟", "时效", "响应", "多久", "多快") {
			items := rankRows(r, "当前负责人", "当前普通回访待办", "其中已逾期")
			top, ok := topPhrase(items, "项")
			if !ok {
				return "目前没有普通回访待办。"
			}
			total, overdue := 0.0, 0.0
			for _, it := range items {
				total += it.value
				overdue += it.extra["其中已逾期"]
			}
			return fmt.Sprintf("目前普通回访待办%s；全部共 %s 项，其中已逾期 %s 项。", top, formatNumber(total), formatNumber(overdue))
		}
		items := rankRows(r, "当前负责人", "线索数")
		if len(items) == 0 {
			return "没有可统计首跟时效的线索。"
		}
		return withSubject(period, filters+"按当前负责人统计首跟与回访："+listTop(items, "条线索", 6)+"，各自的平均首跟时长见下表。")
	case "followup_by_actor":
		items := []namedCount{}
		for _, it := range rankRows(r, "操作人", "跟进条数", "涉及线索数(去重)") {
			if !strings.HasPrefix(it.name, "全部操作人") {
				items = append(items, it)
			}
		}
		top, ok := topPhrase(items, "条跟进")
		if !ok {
			return withSubject(period, filters+"没有跟进记录。")
		}
		return withSubject(period, filters+"按实际操作人看，"+top+"。")
	case "followup_tasks":
		if total, ok := firstCell(r, "当前普通回访待办"); ok {
			overdue, _ := firstCell(r, "其中已逾期")
			return fmt.Sprintf("%s目前共有 %s 项普通回访待办，其中已逾期 %s 项。", filters, formatNumber(total), formatNumber(overdue))
		}
		total := len(r.Rows)
		if r.MaterializedRows > total {
			total = r.MaterializedRows
		}
		if total == 0 {
			return "目前没有符合条件的普通回访待办。"
		}
		overdue := 0
		if i := resultColumn(r, "截至快照"); i >= 0 {
			for _, row := range r.Rows {
				if i < len(row) && fmt.Sprint(row[i]) == "已逾期" {
					overdue++
				}
			}
		}
		return fmt.Sprintf("%s目前共有 %d 项普通回访待办，其中已逾期 %d 项。", filters, total, overdue)
	case "lead_followup_status":
		if len(r.Rows) == 0 {
			return "没有找到这条线索。"
		}
		row := r.Rows[0]
		get := func(c string) string {
			if i := resultColumn(r, c); i >= 0 && i < len(row) {
				return fmt.Sprint(row[i])
			}
			return ""
		}
		state := "不是"
		if get("当前普通回访资格") == "是" {
			state = "是"
		}
		return fmt.Sprintf("%s 当前%s普通回访待办：当前普通待办 %s 项，历史回访计划 %s 条（历史计划不算当前待办），有效预约：%s。", get("客户"), state, get("当前普通待办数"), get("历史回访计划数"), get("有效预约"))
	case "followup_keyword":
		n, _ := firstCell(r, "匹配跟进记录")
		if n == 0 {
			return "跟进记录里没有提到“" + p.Keyword + "”的记录。"
		}
		leads, _ := firstCell(r, "涉及线索")
		won, _ := firstCell(r, "其中当前已成交线索")
		return fmt.Sprintf("跟进记录里提到“%s”的有 %s 条，涉及 %s 条线索，其中当前已成交 %s 条。", p.Keyword, formatNumber(n), formatNumber(leads), formatNumber(won))
	case "followup_records":
		total := len(r.Rows)
		if r.MaterializedRows > total {
			total = r.MaterializedRows
		}
		if total == 0 {
			return withSubject(period, filters+"没有符合条件的跟进记录。")
		}
		return withSubject(period, filters+fmt.Sprintf("找到 %d 条跟进记录，内容已脱敏，见下表。", total))
	case "lead_status", "rating_distribution", "appointment_status":
		label, value, unit := map[string][2]string{"lead_status": {"状态", "线索数"}, "rating_distribution": {"评级", "线索数"}, "appointment_status": {"预约状态", "预约数"}}[out.Selection.MetricID], "", "条"
		value = label[1]
		if out.Selection.MetricID == "appointment_status" {
			unit = "个"
		}
		items := rankRows(r, label[0], value)
		if len(items) == 0 {
			return withSubject(period, filters+"没有相关记录。")
		}
		return withSubject(period, filters+"按"+map[string]string{"lead_status": "当前状态", "rating_distribution": "当前评级", "appointment_status": "预约状态"}[out.Selection.MetricID]+"分布："+listTop(items, unit, 6)+"。")
	}
	return "已按条件查到结果，见下表。"
}

func greetingAnswer() string {
	return "你好！我是招生 CRM 数据助手，可以直接回答线索、来源、负责人、跟进、待办和成交方面的问题。比如可以问我：\n- 上周共有几条线索进来？\n- 本月哪个来源最多？\n- 谁的待办最多？\n- 这个月成交了几单？"
}

func notUnderstoodAnswer() string {
	return "这个问题我暂时没法用现有的统计口径直接回答。可以换个问法试试，比如“上周共有几条线索进来”“本月哪个来源最多”“按负责人拆开”“谁的待办最多”，也可以用下方常用指标查看数据。"
}

// HeuristicSelections exposes the deterministic parser (tests and tooling).
func HeuristicSelections(question string, now time.Time, previous []MetricSelection) []MetricSelection {
	return parseMetricQuestion(question, now, previous).Selections
}

// applyParserPriority makes the deterministic server parser win over the
// model for what it understood: a confidently parsed metric replaces an
// unrelated model choice, and a parsed time phrase always sets the dates.
// It reports which fields were overridden, and whether the model's dates were
// unusable with nothing to replace them (the caller must not silently answer
// over all data in that case).
func applyParserPriority(selections []MetricSelection, intent metricIntent, datesDropped bool) ([]MetricSelection, []string, bool) {
	overrides := []string{}
	if intent.Confident && len(intent.Selections) > 0 && (intent.Selections[0].MetricID == "lead_count" || intent.Selections[0].MetricID == "deal_count") {
		// Only a count question answered by the generic overview is replaced:
		// there a different metric means a different number. Other model
		// choices (e.g. lead_followup_status vs followup_sla) are legitimate.
		replace := len(selections) > 0
		for _, sel := range selections {
			if sel.MetricID != "overview" {
				replace = false
			}
		}
		if replace {
			selections = append([]MetricSelection(nil), intent.Selections...)
			overrides = append(overrides, "metric")
		}
	}
	unresolved := false
	for i := range selections {
		m := findMetric(selections[i].MetricID)
		if m == nil || selections[i].MetricID == "followup_sla" {
			continue
		}
		allowed := false
		for _, name := range m.Params {
			if name == "date_from" {
				allowed = true
			}
		}
		if !allowed {
			continue
		}
		p := &selections[i].Params
		if intent.HasPeriod {
			if p.DateFrom != intent.PeriodFrom || p.DateTo != intent.PeriodTo {
				p.DateFrom, p.DateTo = intent.PeriodFrom, intent.PeriodTo
				overrides = append(overrides, "dates")
			}
			continue
		}
		if datesDropped && p.DateFrom == "" && p.DateTo == "" {
			unresolved = true
		}
	}
	return selections, overrides, unresolved
}
