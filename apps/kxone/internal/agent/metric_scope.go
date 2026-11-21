package agent

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

// metricScope is resolved locally against the same snapshot as the query.
// Explicit constraints are requirements, including on model-error fallbacks.
type metricScope struct {
	params                               MetricParams
	metric                               string
	problem                              string
	unsupported                          bool
	clearOwner, clearSources, clearDates bool
}

type metricEmployee struct{ id, name string }

var simpleOwnerInventory = regexp.MustCompile(`^(?:请问|请帮我查一下|请帮我查|请查|帮我查|我想知道)*(?:现在|目前|当前)*(?:员工)?@owner@(?:(?:手上|名下)(?:的)?(?:一共|总共)?(?:有|共有)?(?:多少|几)(?:条)?线索|负责(?:了)?(?:多少|几)(?:条)?线索)[？?。.！!]*$`)

// Only this complete grammar bypasses selection: a unique employee and a
// simple current inventory count. Extra clauses still go through the model and
// the scope guard; a partial keyword match is never sufficient.
func (scope metricScope) directInventory(question string) bool {
	p := scope.params
	if scope.problem != "" || scope.metric != "owner_inventory" || p.OwnerID == "" || p.DateFrom != "" || p.DateTo != "" || len(p.Sources)+len(p.ExcludeSources) > 0 || p.Customer != "" || p.Keyword != "" || p.DueBefore != "" {
		return false
	}
	q := strings.Join(strings.Fields(question), "")
	for _, name := range []string{p.Owner, p.OwnerID} {
		if name != "" && simpleOwnerInventory.MatchString(strings.Replace(q, name, "@owner@", 1)) {
			return true
		}
	}
	return false
}

func metricEmployees(ctx context.Context, snapshot *Snapshot) ([]metricEmployee, error) {
	rows, err := snapshot.db.QueryContext(ctx, `SELECT wecom_userid,COALESCE(employee_name,'') FROM crm_wecom_user_bindings ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var employees []metricEmployee
	for rows.Next() {
		var e metricEmployee
		if err := rows.Scan(&e.id, &e.name); err != nil {
			return nil, err
		}
		employees = append(employees, e)
	}
	return employees, rows.Err()
}

func resolveMetricOwner(employees []metricEmployee, owner string) (metricEmployee, error) {
	var matched []metricEmployee
	for _, e := range employees {
		if e.id == owner {
			if e.name == "" {
				e.name = e.id
			}
			return e, nil
		}
		if e.name != "" && e.name == owner {
			matched = append(matched, e)
		}
	}
	if len(matched) == 1 {
		return matched[0], nil
	}
	if len(matched) > 1 {
		return metricEmployee{}, errors.New("找到同名员工，请补充员工账号以确定查询对象。")
	}
	return metricEmployee{}, errors.New("没有找到该员工，请确认姓名或员工账号；尚未查询线索数量。")
}

var namedOwnerPhrase = regexp.MustCompile(`([\p{Han}A-Za-z0-9_\-]{2,20}?)(?:手上|名下|负责的线索|负责多少|负责了多少|的跟进)`)
var changedOwnerPhrase = regexp.MustCompile(`(?:换成|换为|改查)([\p{Han}A-Za-z0-9_\-]{2,20})`)
var explicitDueTime = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})[ T](\d{2}:\d{2}(?::\d{2})?)`)
var explicitCustomer = regexp.MustCompile(`客户#\d+`)
var explicitSource = regexp.MustCompile(`(?:只看|来自|来源是|来源为|排除|不含|不算)[「“"]?([^，,。？?「」“”"]{1,100})`)

var unsupportedMetricFilter = regexp.MustCompile(`(?:有效|无效|已关闭|关闭|未成交)(?:的)?线索|[1-5一二三四五][星★]|(?:星级|评级|等级)(?:为|是)?[A-Za-z0-9一二三四五]`)

type metricMention struct {
	value      string
	start, end int
}

func findMetricMentions(text, value string) []metricMention {
	var out []metricMention
	for offset := 0; offset < len(text); {
		i := strings.Index(text[offset:], value)
		if i < 0 {
			break
		}
		start := offset + i
		out = append(out, metricMention{value, start, start + len(value)})
		offset = start + len(value)
	}
	return out
}

func longestMetricMentions(mentions []metricMention) []metricMention {
	sort.SliceStable(mentions, func(i, j int) bool { return len(mentions[i].value) > len(mentions[j].value) })
	var accepted []metricMention
	for _, candidate := range mentions {
		overlaps := false
		for _, other := range accepted {
			if candidate.start < other.end && candidate.end > other.start {
				overlaps = true
				break
			}
		}
		if !overlaps {
			accepted = append(accepted, candidate)
		}
	}
	return accepted
}

func maskMetricMentions(text string, mentions []metricMention) string {
	masked := []byte(text)
	for _, mention := range mentions {
		for i := mention.start; i < mention.end; i++ {
			masked[i] = ' '
		}
	}
	return string(masked)
}

func metricSourceNames(ctx context.Context, snapshot *Snapshot) (map[string]bool, error) {
	rows, err := snapshot.db.QueryContext(ctx, `SELECT DISTINCT source_channel FROM leads WHERE COALESCE(source_channel,'')<>''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sources := map[string]bool{}
	for rows.Next() {
		var source string
		if err := rows.Scan(&source); err != nil {
			return nil, err
		}
		sources[source] = true
	}
	return sources, rows.Err()
}

func inventoryQuestion(q string) bool {
	return containsAny(q, "手上", "名下", "负责多少", "负责的线索", "负责了多少")
}

func clearMetricDimensions(q string) (owner, sources, dates bool) {
	return containsAny(q, "所有人", "全部员工", "所有员工", "每位员工", "每个员工", "不限人员", "全部负责人", "不限负责人"),
		containsAny(q, "全部来源", "所有来源", "不限来源", "不限制来源"),
		containsAny(q, "全部时间", "不限日期", "不限时间", "所有日期", "不限定日期")
}

func resolveMetricScope(ctx context.Context, snapshot *Snapshot, question string, intent metricIntent, previous []MetricSelection) metricScope {
	return resolveMetricQuestionScope(ctx, snapshot, question, intent, previous, false)
}

// The model plan owns the business meaning. Both paths share identity and
// explicit-filter validation; only the degraded legacy path pins metrics.
func resolveMetricQuestionScope(ctx context.Context, snapshot *Snapshot, question string, intent metricIntent, previous []MetricSelection, filtersOnly bool) metricScope {
	scope := metricScope{}
	cashQuestion := strings.NewReplacer("不是现金收入", "", "不是现金到账", "", "并非现金收入", "", "不代表现金到账", "", "不等于现金收入", "").Replace(question)
	if containsAny(cashQuestion, "现金", "到账", "收款", "实收", "回款", "实际收到", "实际收了", "当作收入") {
		scope.problem = "当前 CRM 没有收款流水，无法核对现金到账金额。可以查询成交金额、退款或净成交额。"
		scope.unsupported = true
		return scope
	}
	scope.clearOwner, scope.clearSources, scope.clearDates = clearMetricDimensions(question)
	if intent.FollowUp && len(previous) > 0 {
		scope.params = previous[0].Params
	}
	if scope.clearOwner {
		scope.params.Owner, scope.params.OwnerID = "", ""
	}
	if scope.clearSources {
		scope.params.Sources, scope.params.ExcludeSources = nil, nil
	}
	if scope.clearDates {
		scope.params.DateFrom, scope.params.DateTo = "", ""
	}
	if intent.HasPeriod {
		scope.params.DateFrom, scope.params.DateTo = intent.PeriodFrom, intent.PeriodTo
	}
	if len(intent.Selections) > 0 {
		if keyword := intent.Selections[0].Params.Keyword; keyword != "" {
			scope.params.Keyword = keyword
		}
	}
	keywordSearch := intent.Confident && len(intent.Selections) > 0 && intent.Selections[0].MetricID == "followup_keyword" && scope.params.Keyword != ""
	safeQuestion, _ := snapshot.Sanitize(question)
	customers := map[string]bool{}
	for _, customer := range explicitCustomer.FindAllString(safeQuestion, -1) {
		customers[customer] = true
	}
	if len(customers) > 1 {
		scope.problem = "请一次指定一条客户线索，或明确要做整体统计。"
		return scope
	}
	for customer := range customers {
		scope.params.Customer = customer
	}
	knownSources, err := metricSourceNames(ctx, snapshot)
	if err != nil {
		scope.problem = "来源目录读取失败，请重试；尚未执行统计。"
		return scope
	}
	// Source labels are atomic. Remove their spans before resolving people: a
	// source such as “占伟视频号（员工乙）” does not constrain the current owner.
	var sourceMentions []metricMention
	for source := range knownSources {
		if scope.params.Keyword == source && !containsAny(question, "来源", "渠道", "只看", "排除") {
			continue
		}
		sourceMentions = append(sourceMentions, findMetricMentions(question, source)...)
	}
	sourceMentions = longestMetricMentions(sourceMentions)
	var included, excluded []string
	for _, mention := range sourceMentions {
		prefix := question[:mention.start]
		if split := strings.LastIndexAny(prefix, "，,；;。"); split >= 0 {
			prefix = prefix[split+1:]
		}
		if containsAny(prefix, "排除", "不含", "不算", "不要", "除了") {
			excluded = append(excluded, mention.value)
		} else {
			included = append(included, mention.value)
		}
	}
	if len(included) > 0 {
		sort.Strings(included)
		scope.params.Sources = included
	}
	if len(excluded) > 0 {
		sort.Strings(excluded)
		scope.params.ExcludeSources = excluded
	}
	employees, err := metricEmployees(ctx, snapshot)
	if err != nil {
		scope.problem = "员工目录读取失败，请重试；尚未执行统计。"
		return scope
	}
	ownerQuestion := maskMetricMentions(question, sourceMentions)
	if intent.FollowUp && scope.params.OwnerID != "" && !intent.HasPeriod {
		if match := changedOwnerPhrase.FindStringSubmatch(ownerQuestion); len(match) > 1 {
			candidate := strings.TrimRight(match[1], "呢啊呀吧的")
			if _, err := resolveMetricOwner(employees, candidate); err != nil {
				scope.problem = err.Error()
				return scope
			}
		}
	}
	// Check all residual text in an explicit source clause, not just a known
	// prefix; mixed known + unknown sources must not become a partial filter.
	for _, match := range explicitSource.FindAllStringSubmatchIndex(question, -1) {
		raw := question[match[2]:match[3]]
		residual := ownerQuestion[match[2]:match[3]]
		recognized := residual != raw
		for _, employee := range employees {
			for _, value := range []string{employee.name, employee.id} {
				if value != "" && strings.Contains(residual, value) {
					residual = strings.ReplaceAll(residual, value, "")
					recognized = true
				}
			}
		}
		if containsAny(raw, "全部来源", "所有来源", "当前负责人", "全部员工") || (intent.HasPeriod && containsAny(raw, "今天", "昨天", "上个月", "本月", "上周", "本周")) {
			recognized = true
		}
		for _, word := range []string{"全部来源", "所有来源", "当前负责人", "全部员工", "今天", "昨天", "上个月", "本月", "上周", "本周", "线索", "来源", "渠道", "手上", "名下", "负责", "成交", "签单", "成单", "新增", "录入", "有多少", "多少", "几条", "几单", "的", "和", "及", "与", "、", " ", "（", "）", "(", ")"} {
			residual = strings.ReplaceAll(residual, word, "")
		}
		if !recognized || residual != "" {
			scope.problem = "无法确认指定来源，请使用来源全称；尚未扩大查询范围。"
			return scope
		}
	}
	// A known short name is not evidence for an unknown longer subject.
	for _, match := range namedOwnerPhrase.FindAllStringSubmatch(ownerQuestion, -1) {
		candidate := match[1]
		if _, err := resolveMetricOwner(employees, candidate); err == nil {
			continue
		}
		for _, prefix := range []string{"请问", "请帮我查一下", "请帮我查", "请统计", "请看看", "请查", "帮我看看", "帮我看下", "帮我查", "我想知道", "我想看", "想知道", "只看", "再看", "看看", "查下", "那", "现在", "目前", "当前", "员工", "的"} {
			candidate = strings.TrimPrefix(candidate, prefix)
		}
		if containsAny(candidate, "谁", "每个", "每位", "所有", "全部", "当前") || scope.clearOwner {
			continue
		}
		if _, err := resolveMetricOwner(employees, candidate); err != nil {
			scope.problem = err.Error()
			return scope
		}
	}
	var mentions []metricMention
	for _, e := range employees {
		for _, value := range []string{e.name, e.id} {
			if value != "" {
				mentions = append(mentions, findMetricMentions(ownerQuestion, value)...)
			}
		}
	}
	people := map[string]metricEmployee{}
	for _, mention := range longestMetricMentions(mentions) {
		e, err := resolveMetricOwner(employees, mention.value)
		if err != nil {
			scope.problem = err.Error()
			return scope
		}
		people[e.id] = e
	}
	if len(people) > 1 {
		scope.problem = "请先指定一位员工，或选择按全部负责人拆分。"
		return scope
	}
	for _, e := range people {
		scope.params.Owner, scope.params.OwnerID = e.name, e.id
	}
	if unsupportedMetricFilter.MatchString(question) || (inventoryQuestion(question) && containsAny(question, "已成交线索", "已成交的线索")) {
		scope.problem = "当前指标尚不支持这个状态或评级筛选条件，请明确其他查询口径；尚未按全部状态发布数字。"
		return scope
	}
	if filtersOnly {
		return scope
	}
	if inventoryQuestion(question) || (intent.FollowUp && len(previous) > 0 && previous[0].MetricID == "owner_inventory" && !containsAny(question, "新增", "录入", "成交", "跟进", "回访")) {
		scope.metric = "owner_inventory"
		if intent.HasPeriod && !containsAny(question, "录入", "新增") {
			scope.problem = "当前名下数量反映本轮数据时点。请确认是按录入日期筛选，还是查询过去时点的归属；后者目前无法还原。"
			return scope
		}
		if !intent.HasPeriod && !(intent.FollowUp && len(previous) > 0 && previous[0].MetricID == "owner_inventory") {
			scope.params.DateFrom, scope.params.DateTo = "", ""
			scope.clearDates = true
		}
	}
	if scope.metric == "owner_inventory" && scope.params.OwnerID == "" {
		if containsAny(question, "谁", "每个", "每位", "按人", "各人") {
			scope.metric = "owner_current"
		} else if !containsAny(question, "全部", "所有") {
			scope.problem = "请明确员工姓名或账号，或说明查询全部负责人；尚未查询全库数量。"
			return scope
		}
	}
	if scope.params.Owner != "" && scope.params.OwnerID == "" {
		e, err := resolveMetricOwner(employees, scope.params.Owner)
		if err != nil {
			scope.problem = err.Error()
			return scope
		}
		scope.params.Owner, scope.params.OwnerID = e.name, e.id
	}
	previousDeals := len(previous) > 0 && (previous[0].MetricID == "source_deals" || previous[0].MetricID == "deal_count")
	if intent.FollowUp && previousDeals && !containsAny(question, "录入", "新增", "这批", "转化率", "成交率", "不看成交", "不要成交") {
		// Pin the occurrence basis for ordinary filter/person follow-ups,
		// regardless of a model's selection. A recognized new breakdown must
		// have a compatible metric; owner_current uses the creation cohort.
		if len(intent.Selections) > 0 && intent.Selections[0].MetricID != "source_deals" && intent.Selections[0].MetricID != "deal_count" && intent.Selections[0].MetricID != "source_performance" {
			scope.problem = "当前尚不支持这个成交日期统计的拆分方式；不能替换为按录入日期的线索统计。可以继续按来源或指定员工筛选区间成交，或明确改查录入队列的表现。"
			scope.unsupported = true
			return scope
		}
		scope.metric = previous[0].MetricID
		if len(intent.Selections) > 0 && intent.Selections[0].MetricID == "source_performance" {
			scope.metric = "source_deals"
		}
	}
	if !keywordSearch && !ownerRankingQuestion(question) && containsAny(question, "来源", "渠道") && containsAny(question, "成交", "签单", "成单", "转化率") {
		cohort := containsAny(question, "录入", "新增", "这批", "转化率", "成交率")
		// Preserve the original all-time conversion-performance question's
		// full denominator. Explicit occurrence periods/counts use deal dates;
		// a follow-up to occurrence metrics must keep that basis.
		if cohort || (!intent.HasPeriod && containsAny(question, "表现") && (!intent.FollowUp || !previousDeals)) {
			scope.metric = "source_performance"
		} else {
			scope.metric = "source_deals"
		}
	}
	if containsAny(question, "首跟", "首响", "响应时效") {
		scope.metric = "owner_first_response"
	}
	if containsAny(question, "回访", "待办", "逾期") && !containsAny(question, "首跟", "首响", "时效") && intent.HasPeriod {
		cutoff := explicitDueTime.FindStringSubmatch(question)
		wantsPastState := strings.Contains(question, "还原") && !containsAny(question, "不等于还原", "不代表还原", "不还原", "无需还原", "不能还原", "不是还原")
		if wantsPastState {
			scope.problem = "当前快照无法还原过去时点的回访资格或归属；请确认是否按当前状态筛选截止时间。"
			return scope
		}
		if len(cutoff) > 0 && containsAny(question, "截止", "早于", "到期") && containsAny(question, "当前状态", "当前业务状态", "当前资格", "当前截止", "当前待办") {
			// A cutoff applied to current eligibility is not a reconstruction of
			// the past state. Keep its time, and do not add a creation-date filter.
			scope.params.DueBefore = cutoff[1] + " " + cutoff[2]
		} else {
			if intent.PeriodFrom != ymd(dayStart(snapshot.At)) || intent.PeriodTo != intent.PeriodFrom {
				scope.problem = "普通回访待办只能核对当前状态，无法还原过去日期的待办。请确认是否改查当前待办，或明确回访截止时间。"
				return scope
			}
			scope.params.DueBefore = dayStart(snapshot.At).AddDate(0, 0, 1).Format("2006-01-02 15:04")
		}
		scope.metric = "followup_tasks"
		scope.params.DateFrom, scope.params.DateTo = "", ""
		scope.clearDates = true
	}

	if !intent.HasPeriod && (containsAny(question, "去年", "前年", "上季度", "那天", "当时", "截至上", "过去时点") || regexp.MustCompile(`\d{4}-\d{2}-\d{2}|\d{4}年|\d{1,2}月|\d{1,2}[日号]`).MatchString(ownerQuestion)) {
		scope.problem = "请明确日期范围及要统计的发生时间；当前快照无法还原过去时点的状态。"
		return scope
	}
	if scope.metric == "" && keywordSearch {
		// An explicit request to search followup text has one supported metric.
		// Keep the parsed keyword and every other scope constraint when the
		// model selects an unrelated count or record metric. Unsupported extra
		// constraints still fail in apply; they are never removed to make it fit.
		scope.metric = "followup_keyword"
	}
	if ownerRankingQuestion(question) && len(intent.Selections) > 0 && intent.Selections[0].MetricID == "owner_current" {
		if ownerRankingColumn(question) == "" {
			scope.problem = "当前负责人排名可按名下线索数、成交数或净成交额比较；这项金额或比例口径尚不支持，不能用其他字段替代。请明确需要的比较指标。"
			scope.unsupported = true
			return scope
		}
		if intent.HasPeriod && ownerRankingColumn(question) != "线索数" && !containsAny(question, "录入", "新增", "这批") {
			scope.problem = "当前负责人排名按线索当前归属统计；尚不支持按成交发生日期做负责人排名。可以明确查看该期间录入线索的当前成交表现。"
			scope.unsupported = true
			return scope
		}
		// A supported person ranking cannot degrade to a global scalar count.
		// apply merges the actual owner/source/date constraints field by field.
		scope.metric = "owner_current"
	}
	customerTask := scope.params.Customer != "" && len(intent.Selections) > 0 && intent.Selections[0].MetricID == "followup_sla" && !containsAny(question, "首跟", "首响", "时效")
	if customerTask && containsAny(question, "当前", "目前", "现在") && containsAny(question, "逾期", "到期", "截止") {
		scope.problem = "当前固定指标暂不支持按单个客户核对逾期或截止条件，请在客户详情核对回访截止时间；尚未发布替代数字。"
		scope.unsupported = true
		return scope
	}
	if customerTask && containsAny(question, "当前", "目前", "现在") && containsAny(question, "普通待办", "回访资格", "待办资格") {
		// The aggregate SLA metric cannot filter a customer. The customer
		// status metric answers this same current-task question and separates
		// historical plans from the single current ordinary task. Keep every
		// parsed condition so unsupported dates/filters still require clarity.
		scope.metric = "lead_followup_status"
	}
	return scope
}

func (scope metricScope) apply(ctx context.Context, snapshot *Snapshot, selections []MetricSelection) ([]MetricSelection, error) {
	if scope.problem != "" {
		return nil, errors.New(scope.problem)
	}
	employees, err := metricEmployees(ctx, snapshot)
	if err != nil {
		return nil, errors.New("员工目录读取失败，请重试。")
	}
	if scope.metric != "" {
		selections = []MetricSelection{{MetricID: scope.metric}}
	}
	out := make([]MetricSelection, 0, len(selections))
	for _, sel := range selections {
		p := sel.Params
		if scope.clearOwner {
			p.Owner, p.OwnerID = "", ""
		}
		if scope.clearSources {
			p.Sources, p.ExcludeSources = nil, nil
		}
		if scope.clearDates {
			p.DateFrom, p.DateTo = "", ""
		}
		r := scope.params
		if r.Owner != "" || r.OwnerID != "" {
			p.Owner, p.OwnerID = r.Owner, r.OwnerID
		}
		if r.DateFrom != "" || r.DateTo != "" {
			p.DateFrom, p.DateTo = r.DateFrom, r.DateTo
		}
		if r.Sources != nil {
			p.Sources = append([]string(nil), r.Sources...)
		}
		if r.ExcludeSources != nil {
			p.ExcludeSources = append([]string(nil), r.ExcludeSources...)
		}
		if r.Customer != "" {
			p.Customer = r.Customer
		}
		if r.Keyword != "" {
			p.Keyword = r.Keyword
		}
		if r.DueBefore != "" {
			p.DueBefore = r.DueBefore
		}
		if p.Owner != "" || p.OwnerID != "" {
			key := p.Owner
			if p.OwnerID != "" {
				key = p.OwnerID
			}
			e, err := resolveMetricOwner(employees, key)
			if err != nil {
				return nil, err
			}
			if p.Owner != "" && p.Owner != e.name && p.Owner != e.id {
				return nil, errors.New("员工姓名与账号不一致，请确认查询对象。")
			}
			p.Owner, p.OwnerID = e.name, e.id
		}
		m := findMetric(sel.MetricID)
		if m == nil {
			return nil, errors.New("该问题暂未匹配到可核对的统计指标，请补充查询范围。")
		}
		for _, required := range m.Required {
			if required == "keyword" && strings.TrimSpace(p.Keyword) == "" {
				return nil, errors.New("跟进内容检索缺少关键词条件，请补充后再统计。")
			}
		}
		_, dropped := normalizeMetricParams(m, p)
		if len(dropped) > 0 {
			sort.Strings(dropped)
			return nil, fmt.Errorf("当前指标不支持所需条件（%s），请明确查询口径；尚未扩大范围查询。", strings.Join(dropped, "、"))
		}
		for _, d := range []string{p.DateFrom, p.DateTo} {
			if d != "" && !validDate(d) {
				return nil, errors.New("日期格式无效，请使用真实的上海日期。")
			}
		}
		if p.DateFrom != "" && p.DateTo != "" && p.DateFrom > p.DateTo {
			return nil, errors.New("日期范围起止顺序有误，请确认。")
		}
		if p.DueBefore != "" {
			layout := "2006-01-02 15:04"
			if len(p.DueBefore) == 19 {
				layout = "2006-01-02 15:04:05"
			}
			if _, err := time.ParseInLocation(layout, strings.Replace(p.DueBefore, "T", " ", 1), types.BusinessLocation); err != nil {
				return nil, errors.New("回访截止时间无效，请确认上海时间。")
			}
		}
		sel.Params = p
		out = append(out, sel)
	}
	return out, nil
}
