package agent

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Fixed metrics: the server owns every number. A model may only choose a
// metric and whitelisted filters; SQL templates bind all values.

type MetricParams struct {
	DateFrom       string   `json:"date_from,omitempty"`
	DateTo         string   `json:"date_to,omitempty"`
	Sources        []string `json:"sources,omitempty"`
	ExcludeSources []string `json:"exclude_sources,omitempty"`
	Owner          string   `json:"owner,omitempty"`
	OwnerID        string   `json:"owner_id,omitempty"`
	Customer       string   `json:"customer,omitempty"`
	Keyword        string   `json:"keyword,omitempty"`
	DueBefore      string   `json:"due_before,omitempty"`
}

type MetricSelection struct {
	MetricID   string            `json:"metric_id"`
	Params     MetricParams      `json:"params"`
	Comparison *MetricComparison `json:"comparison,omitempty"`
	TimeBasis  string            `json:"time_basis,omitempty"`
	rankColumn string            // server-derived; never accepted from model JSON
}

type MetricInfo struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Params      []string `json:"params"`
	Required    []string `json:"required,omitempty"`
	Quick       bool     `json:"quick"`
}

type metricChart struct {
	Type string
	X    string
	Y    []string
}

type metricQuery struct {
	sql   string
	args  []any
	title string
	chart *metricChart
}

type metricDef struct {
	MetricInfo
	build func(p MetricParams) ([]metricQuery, error)
}

const ownerNameSQL = `COALESCE((SELECT b.employee_name FROM crm_wecom_user_bindings b WHERE b.wecom_userid=l.current_owner_user_id ORDER BY b.id LIMIT 1),l.current_owner_user_id)`
const uniqueOwnerNameSQL = `CASE WHEN (SELECT COUNT(DISTINCT b.wecom_userid) FROM crm_wecom_user_bindings b WHERE b.employee_name=(` + ownerNameSQL + `))>1 THEN (` + ownerNameSQL + `)||'（'||l.current_owner_user_id||'）' ELSE ` + ownerNameSQL + ` END`
const convertedSQL = `l.status='converted'`

var netYuanSQL = amountYuanSQL("l.net_deal_amount_cents")

func amountYuanSQL(column string) string {
	return `CASE WHEN COALESCE(SUM(CASE WHEN ` + convertedSQL + ` AND ` + column + ` IS NULL THEN 1 ELSE 0 END),0)>0 THEN NULL ELSE ROUND(COALESCE(SUM(CASE WHEN ` + convertedSQL + ` THEN ` + column + ` ELSE 0 END),0)/100.0,2) END`
}

const statusLabelSQL = `CASE l.status WHEN 'pending_dispatch' THEN '待主管分配' WHEN 'assigned_pending_claim' THEN '已分配待领取' WHEN 'claimed' THEN '已领取' WHEN 'following' THEN '跟进中' WHEN 'public_pool' THEN '邀约公海' WHEN 'invalid' THEN '无效线索' WHEN 'converted' THEN '已成交' WHEN 'closed' THEN '已关闭' ELSE COALESCE(NULLIF(l.status,''),'未知') END`

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
var dueBeforePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}(:\d{2})?$`)
var customerRefPattern = regexp.MustCompile(`^客户#\d{1,9}$`)

type filterBuilder struct {
	where []string
	args  []any
}

func (f *filterBuilder) add(cond string, args ...any) {
	f.where = append(f.where, cond)
	f.args = append(f.args, args...)
}

func (f *filterBuilder) clause() string {
	if len(f.where) == 0 {
		return "1=1"
	}
	return strings.Join(f.where, " AND ")
}

func inList(column string, values []string) (string, []any) {
	marks := make([]string, len(values))
	args := make([]any, len(values))
	for i, v := range values {
		marks[i] = "?"
		args[i] = v
	}
	return column + " IN (" + strings.Join(marks, ",") + ")", args
}

// leadFilters applies the shared lead filters; dateColumn chooses the date basis.
func leadFilters(p MetricParams, dateColumn string, ownerColumn string) (*filterBuilder, error) {
	f := &filterBuilder{}
	for _, d := range []struct{ v, op string }{{p.DateFrom, ">="}, {p.DateTo, "<="}} {
		if d.v == "" {
			continue
		}
		if !datePattern.MatchString(d.v) {
			return nil, errors.New("日期须为上海日期 YYYY-MM-DD")
		}
		f.add(dateColumn+d.op+"?", d.v)
	}
	if len(p.Sources) > 0 {
		cond, args := inList("COALESCE(l.source_channel,'')", p.Sources)
		f.add(cond, args...)
	}
	if len(p.ExcludeSources) > 0 {
		cond, args := inList("COALESCE(l.source_channel,'')", p.ExcludeSources)
		f.add("NOT "+cond, args...)
	}
	if p.OwnerID != "" && ownerColumn != "" {
		f.add(ownerColumn+"=?", p.OwnerID)
	} else if p.Owner != "" && ownerColumn != "" {
		f.add(ownerColumn+" IN (SELECT b.wecom_userid FROM crm_wecom_user_bindings b WHERE b.employee_name=? OR b.wecom_userid=?)", p.Owner, p.Owner)
	}
	if p.Customer != "" {
		if !customerRefPattern.MatchString(p.Customer) {
			return nil, errors.New("客户须使用“客户#编号”形式")
		}
		f.add("l.customer_ref=?", p.Customer)
	}
	return f, nil
}

var metricCatalog = []metricDef{
	{MetricInfo{"source_deals", "来源区间成交（按成交日期）", "按成交发生的上海日期及来源统计当前仍成交的单数与业务净成交额，不代表收款流水。", []string{"date_from", "date_to", "sources", "exclude_sources", "owner"}, nil, false}, buildSourceDeals},
	{MetricInfo{"owner_first_response", "当前责任周期首跟时效", "首跟时长为本轮负责人责任周期开始至本轮首次跟进的间隔；日期按本轮责任周期首次跟进发生的上海日期筛选，不按录入日期。未知时间和待首跟不填零，不用于平均值；有日期时不能将缺失时间归于该区间。", []string{"date_from", "date_to", "sources", "exclude_sources", "owner"}, nil, false}, buildOwnerFirstResponse},
	{MetricInfo{"owner_inventory", "当前名下线索（全部状态）", "按当前实际负责人统计全部状态的名下线索，含成交、无效、关闭；分配但未领取单列，不计入名下总数。日期仅用于明确指定的录入队列。", []string{"date_from", "date_to", "sources", "exclude_sources", "owner"}, nil, false}, buildOwnerInventory},
	{MetricInfo{"overview", "线索与成交总览", "线索总数、当前成交数与成交率、成交金额、退款与净成交额（元）、无效数。成交按当前状态。", []string{"date_from", "date_to", "sources", "exclude_sources", "owner"}, nil, true}, func(p MetricParams) ([]metricQuery, error) {
		f, err := leadFilters(p, "l.created_at_date_shanghai", "l.current_owner_user_id")
		if err != nil {
			return nil, err
		}
		return []metricQuery{{sql: `SELECT COUNT(*) AS "线索数",COALESCE(SUM(` + convertedSQL + `),0) AS "成交数",ROUND(100.0*COALESCE(SUM(` + convertedSQL + `),0)/NULLIF(COUNT(*),0),2) AS "成交率(%)",` + amountYuanSQL("l.deal_amount_cents") + ` AS "成交金额(元)",` + amountYuanSQL("l.refund_amount_cents") + ` AS "退款金额(元)",` + netYuanSQL + ` AS "净成交额(元)",COALESCE(SUM(l.result_status='invalid'),0) AS "无效数" FROM leads l WHERE ` + f.clause(), args: f.args}}, nil
	}},
	{MetricInfo{"lead_status", "线索状态分布", "按当前状态统计线索数。", []string{"date_from", "date_to", "sources", "exclude_sources", "owner"}, nil, false}, func(p MetricParams) ([]metricQuery, error) {
		f, err := leadFilters(p, "l.created_at_date_shanghai", "l.current_owner_user_id")
		if err != nil {
			return nil, err
		}
		return []metricQuery{{sql: `SELECT ` + statusLabelSQL + ` AS "状态",COUNT(*) AS "线索数" FROM leads l WHERE ` + f.clause() + ` GROUP BY 1 ORDER BY 2 DESC,1`, args: f.args, chart: &metricChart{"bar", "状态", []string{"线索数"}}}}, nil
	}},
	{MetricInfo{"source_performance", "来源表现", "按来源统计线索数、当前成交数、成交率与净成交额（元），全部线索为分母。", []string{"date_from", "date_to", "sources", "exclude_sources", "owner"}, nil, true}, func(p MetricParams) ([]metricQuery, error) {
		f, err := leadFilters(p, "l.created_at_date_shanghai", "l.current_owner_user_id")
		if err != nil {
			return nil, err
		}
		return []metricQuery{{sql: `SELECT COALESCE(NULLIF(l.source_channel,''),'未填写') AS "来源",COUNT(*) AS "线索数",COALESCE(SUM(` + convertedSQL + `),0) AS "成交数",ROUND(100.0*COALESCE(SUM(` + convertedSQL + `),0)/COUNT(*),2) AS "成交率(%)",` + netYuanSQL + ` AS "净成交额(元)" FROM leads l WHERE ` + f.clause() + ` GROUP BY 1 ORDER BY 2 DESC,1`, args: f.args, chart: &metricChart{"bar", "来源", []string{"线索数"}}}}, nil
	}},
	{MetricInfo{"owner_current", "负责人当前归属与净额", "按线索当前归属统计（员工/已分配待领取/公海/待分配，互斥），含成交数、无效数与净成交额（元）。日期为录入队列。", []string{"date_from", "date_to", "sources", "exclude_sources", "owner"}, nil, true}, func(p MetricParams) ([]metricQuery, error) {
		return buildOwnerCurrent(p, "")
	}},
	{MetricInfo{"followup_by_actor", "跟进按实际操作人", "按跟进记录的实际操作人统计跟进条数与涉及线索数（去重），末行为全部操作人整体去重；不按客户当前归属。日期为跟进发生的上海日期。", []string{"date_from", "date_to", "sources", "exclude_sources", "owner"}, nil, true}, func(p MetricParams) ([]metricQuery, error) {
		f, err := leadFilters(p, "f.created_at_date_shanghai", "f.sales_user_id")
		if err != nil {
			return nil, err
		}
		actor := `COALESCE((SELECT b.employee_name FROM crm_wecom_user_bindings b WHERE b.wecom_userid=f.sales_user_id ORDER BY b.id LIMIT 1),NULLIF(f.sales_user_id,''),'未知操作人')`
		base := ` FROM lead_followups f JOIN leads l ON l.id=f.lead_id WHERE ` + f.clause()
		args := append(append([]any{}, f.args...), f.args...)
		return []metricQuery{{sql: `SELECT "操作人","跟进条数","涉及线索数(去重)" FROM (SELECT ` + actor + ` AS "操作人",COUNT(*) AS "跟进条数",COUNT(DISTINCT f.lead_id) AS "涉及线索数(去重)",0 AS ord` + base + ` GROUP BY f.sales_user_id UNION ALL SELECT '全部操作人（整体去重）',COUNT(*),COUNT(DISTINCT f.lead_id),1` + base + `) ORDER BY ord,"跟进条数" DESC,"操作人"`, args: args, chart: nil}}, nil
	}},
	{MetricInfo{"followup_sla", "首跟时效与回访", "按当前负责人统计线索数、负责人已首跟数、平均首跟分钟（缺失时间不计入、不按零）、当前普通回访待办及逾期数。", []string{"date_from", "date_to", "sources", "exclude_sources", "owner"}, nil, true}, func(p MetricParams) ([]metricQuery, error) {
		f, err := leadFilters(p, "l.created_at_date_shanghai", "l.current_owner_user_id")
		if err != nil {
			return nil, err
		}
		f.add("COALESCE(l.current_owner_user_id,'')<>''")
		return []metricQuery{{sql: `SELECT ` + uniqueOwnerNameSQL + ` AS "当前负责人",COUNT(*) AS "线索数",COALESCE(SUM(l.owner_first_followup_seconds IS NOT NULL),0) AS "负责人已首跟",ROUND(AVG(l.owner_first_followup_seconds)/60.0,1) AS "负责人平均首跟(分钟)",ROUND(AVG(l.lifecycle_first_followup_seconds)/60.0,1) AS "生命周期平均首跟(分钟)",COALESCE(SUM(l.current_followup_eligible),0) AS "当前普通回访待办",COALESCE(SUM(l.current_followup_overdue),0) AS "其中已逾期" FROM leads l WHERE ` + f.clause() + ` GROUP BY l.current_owner_user_id ORDER BY 2 DESC,1`, args: f.args}}, nil
	}},
	{MetricInfo{"followup_tasks", "当前普通回访待办", "按 CRM 当前回访资格列出普通回访待办（有效预约、成交、无效、公海、待领取不计）；可限定截止时间早于某上海时刻。不还原历史时点状态。", []string{"sources", "exclude_sources", "owner", "due_before"}, nil, true}, func(p MetricParams) ([]metricQuery, error) {
		f, err := leadFilters(p, "", "l.current_owner_user_id")
		if err != nil {
			return nil, err
		}
		f.add("l.current_followup_eligible=1")
		if p.DueBefore != "" {
			if !dueBeforePattern.MatchString(p.DueBefore) {
				return nil, errors.New("截止时刻须为上海时间 YYYY-MM-DD HH:MM")
			}
			f.add("l.next_followup_due_at_shanghai<?", strings.Replace(p.DueBefore, " ", "T", 1))
		}
		return []metricQuery{
			{title: "全量汇总", sql: `SELECT COUNT(*) AS "当前普通回访待办",COALESCE(SUM(l.current_followup_overdue),0) AS "其中已逾期" FROM leads l WHERE ` + f.clause(), args: f.args},
			{sql: `SELECT l.customer_ref AS "客户",` + ownerNameSQL + ` AS "当前负责人",l.next_followup_due_at_shanghai AS "当前回访截止(上海)",CASE WHEN l.current_followup_overdue=1 THEN '已逾期' ELSE '未逾期' END AS "截至快照",'leads' AS record_type,l.id AS record_id,l.id AS lead_id FROM leads l WHERE ` + f.clause() + ` ORDER BY l.next_followup_due_at,l.id`, args: f.args}}, nil
	}},
	{MetricInfo{"lead_followup_status", "单条线索回访资格核对", "核对线索当前普通回访资格、当前普通待办数（至多一项）、历史回访计划数与是否有有效预约；历史计划不构成当前待办。", []string{"customer", "sources", "owner"}, nil, false}, func(p MetricParams) ([]metricQuery, error) {
		f, err := leadFilters(p, "", "l.current_owner_user_id")
		if err != nil {
			return nil, err
		}
		return []metricQuery{{sql: `SELECT l.customer_ref AS "客户",` + statusLabelSQL + ` AS "当前状态",CASE WHEN l.current_followup_eligible=1 THEN '是' ELSE '否' END AS "当前普通回访资格",COALESCE(l.current_followup_eligible,0) AS "当前普通待办数",(SELECT COUNT(*) FROM lead_followups f WHERE f.lead_id=l.id AND COALESCE(f.next_followup_at,'')<>'') AS "历史回访计划数",CASE WHEN EXISTS(SELECT 1 FROM lead_appointments a WHERE a.id=l.current_appointment_id AND a.lead_id=l.id AND a.status IN ('pending_assignment','scheduled')) THEN '有' ELSE '无' END AS "有效预约",'leads' AS record_type,l.id AS record_id,l.id AS lead_id FROM leads l WHERE ` + f.clause() + ` ORDER BY l.id`, args: f.args}}, nil
	}},
	{MetricInfo{"followup_keyword", "跟进关键词检索", "检索跟进记录文字中包含关键词的记录（内容已脱敏），并汇总匹配记录数、涉及线索数及其中当前已成交线索数。只说明记录提及，不证明因果。", []string{"keyword", "date_from", "date_to", "sources", "exclude_sources"}, []string{"keyword"}, false}, func(p MetricParams) ([]metricQuery, error) {
		if strings.TrimSpace(p.Keyword) == "" || len([]rune(p.Keyword)) > 20 {
			return nil, errors.New("关键词必填且不超过20字")
		}
		f, err := leadFilters(p, "f.created_at_date_shanghai", "")
		if err != nil {
			return nil, err
		}
		keyword := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(strings.TrimSpace(p.Keyword))
		f.add(`f.remark LIKE ? ESCAPE '\'`, "%"+keyword+"%")
		base := ` FROM lead_followups f JOIN leads l ON l.id=f.lead_id WHERE ` + f.clause()
		actor := `COALESCE((SELECT b.employee_name FROM crm_wecom_user_bindings b WHERE b.wecom_userid=f.sales_user_id ORDER BY b.id LIMIT 1),f.sales_user_id)`
		return []metricQuery{
			{title: "匹配汇总", sql: `SELECT COUNT(*) AS "匹配跟进记录",COUNT(DISTINCT f.lead_id) AS "涉及线索",COUNT(DISTINCT CASE WHEN ` + convertedSQL + ` THEN f.lead_id END) AS "其中当前已成交线索"` + base, args: f.args},
			{title: "匹配记录", sql: `SELECT l.customer_ref AS "客户",f.created_at_shanghai AS "跟进时间(上海)",` + actor + ` AS "操作人",f.remark AS "跟进内容",` + statusLabelSQL + ` AS "线索当前状态",'lead_followups' AS record_type,f.id AS record_id,f.lead_id AS lead_id` + base + ` ORDER BY f.created_at,f.id`, args: f.args},
		}, nil
	}},
	{MetricInfo{"followup_records", "跟进记录明细", "按条件列出跟进记录原文（已脱敏），用于查阅客户顾虑等定性内容；可按来源、操作人、客户、跟进日期筛选。只说明记录内容，不证明因果。", []string{"date_from", "date_to", "sources", "exclude_sources", "owner", "customer"}, nil, false}, func(p MetricParams) ([]metricQuery, error) {
		f, err := leadFilters(p, "f.created_at_date_shanghai", "f.sales_user_id")
		if err != nil {
			return nil, err
		}
		actor := `COALESCE((SELECT b.employee_name FROM crm_wecom_user_bindings b WHERE b.wecom_userid=f.sales_user_id ORDER BY b.id LIMIT 1),f.sales_user_id)`
		return []metricQuery{{sql: `SELECT l.customer_ref AS "客户",COALESCE(NULLIF(l.source_channel,''),'未填写') AS "来源",f.created_at_shanghai AS "跟进时间(上海)",` + actor + ` AS "操作人",f.remark AS "跟进内容",` + statusLabelSQL + ` AS "线索当前状态",'lead_followups' AS record_type,f.id AS record_id,f.lead_id AS lead_id FROM lead_followups f JOIN leads l ON l.id=f.lead_id WHERE ` + f.clause() + ` ORDER BY f.created_at,f.id`, args: f.args}}, nil
	}},
	{MetricInfo{"rating_distribution", "当前评级分布", "按线索当前星级统计；无有效评级单列，不映射为一星。", []string{"date_from", "date_to", "sources", "exclude_sources", "owner"}, nil, false}, func(p MetricParams) ([]metricQuery, error) {
		f, err := leadFilters(p, "l.created_at_date_shanghai", "l.current_owner_user_id")
		if err != nil {
			return nil, err
		}
		return []metricQuery{{sql: `SELECT CASE WHEN l.star_rating BETWEEN 1 AND 5 THEN l.star_rating||'星' ELSE '无有效评级' END AS "评级",COUNT(*) AS "线索数" FROM leads l WHERE ` + f.clause() + ` GROUP BY 1 ORDER BY 1 DESC`, args: f.args, chart: &metricChart{"bar", "评级", []string{"线索数"}}}}, nil
	}},
	{MetricInfo{"appointment_status", "预约状态分布", "按预约记录状态统计（有效=待分配/已排期，取消等历史单列）。日期为预约开始的上海日期。", []string{"date_from", "date_to"}, nil, false}, func(p MetricParams) ([]metricQuery, error) {
		f := &filterBuilder{}
		for _, d := range []struct{ v, op string }{{p.DateFrom, ">="}, {p.DateTo, "<="}} {
			if d.v == "" {
				continue
			}
			if !datePattern.MatchString(d.v) {
				return nil, errors.New("日期须为上海日期 YYYY-MM-DD")
			}
			f.add("a.start_at_date_shanghai"+d.op+"?", d.v)
		}
		return []metricQuery{{sql: `SELECT CASE a.status WHEN 'pending_assignment' THEN '有效-待分配' WHEN 'scheduled' THEN '有效-已排期' WHEN 'arrived_delivered' THEN '已到访' WHEN 'no_show' THEN '爽约' WHEN 'cancelled' THEN '已取消' WHEN 'rescheduled' THEN '已改期' ELSE COALESCE(NULLIF(a.status,''),'未知') END AS "预约状态",COUNT(*) AS "预约数" FROM lead_appointments a WHERE ` + f.clause() + ` GROUP BY 1 ORDER BY 2 DESC,1`, args: f.args}}, nil
	}},
	{MetricInfo{"lead_count", "区间新增线索数", "按线索录入的上海日期统计某段时间新进线索的总条数（如“上周共有几条线索”“昨天新增多少”）。", []string{"date_from", "date_to", "sources", "exclude_sources", "owner"}, nil, false}, func(p MetricParams) ([]metricQuery, error) {
		f, err := leadFilters(p, "l.created_at_date_shanghai", "l.current_owner_user_id")
		if err != nil {
			return nil, err
		}
		return []metricQuery{{sql: `SELECT COUNT(*) AS "新增线索数" FROM leads l WHERE ` + f.clause(), args: f.args}}, nil
	}},
	{MetricInfo{"deal_count", "区间成交（按成交日期）", "按成交（converted_at）上海日期统计当前状态为已成交的单数、成交金额与净成交额（元）；成交时间缺失的不计入日期范围。", []string{"date_from", "date_to", "sources", "exclude_sources", "owner"}, nil, false}, func(p MetricParams) ([]metricQuery, error) {
		f, err := leadFilters(p, "l.converted_at_date_shanghai", "l.current_owner_user_id")
		if err != nil {
			return nil, err
		}
		f.add(convertedSQL)
		return []metricQuery{{sql: `SELECT COUNT(*) AS "成交数",` + amountYuanSQL("l.deal_amount_cents") + ` AS "成交金额(元)",` + netYuanSQL + ` AS "净成交额(元)" FROM leads l WHERE ` + f.clause(), args: f.args}}, nil
	}},
	{MetricInfo{"daily_new_leads", "按上海日期录入", "按线索录入的上海日期统计新增线索数。", []string{"date_from", "date_to", "sources", "exclude_sources"}, nil, false}, func(p MetricParams) ([]metricQuery, error) {
		f, err := leadFilters(p, "l.created_at_date_shanghai", "")
		if err != nil {
			return nil, err
		}
		return []metricQuery{{sql: `SELECT l.created_at_date_shanghai AS "录入日期(上海)",COUNT(*) AS "线索数" FROM leads l WHERE ` + f.clause() + ` GROUP BY 1 ORDER BY 1`, args: f.args, chart: &metricChart{"line", "录入日期(上海)", []string{"线索数"}}}}, nil
	}},
}

func MetricCatalog() []MetricInfo {
	out := make([]MetricInfo, len(metricCatalog))
	for i, m := range metricCatalog {
		out[i] = m.MetricInfo
	}
	return out
}

// buildOwnerCurrent keeps person identity, ranking order and presentation on
// the same query. Generic ownership breakdowns retain their non-staff groups.
func buildOwnerCurrent(p MetricParams, rankColumn string) ([]metricQuery, error) {
	f, err := leadFilters(p, "l.created_at_date_shanghai", "l.current_owner_user_id")
	if err != nil {
		return nil, err
	}
	owner := `CASE WHEN l.ownership_state='assigned_pending_claim' OR l.status='assigned_pending_claim' THEN '已分配待领取' WHEN l.ownership_state='public_pool' OR l.status='public_pool' THEN '公海' WHEN l.ownership_state='pending_assignment' OR l.status='pending_dispatch' THEN '待主管分配' WHEN COALESCE(l.current_owner_user_id,'')='' THEN '无负责人' ELSE ` + ownerNameSQL + ` END`
	groupOrder := " GROUP BY 1 ORDER BY 2 DESC,1"
	extra, title := "", ""
	additional := ""
	chart := &metricChart{"bar", "当前归属", []string{"线索数"}}
	if rankColumn != "" {
		rank, ok := ownerRankingSpec(rankColumn)
		if !ok {
			return nil, errors.New("负责人排名字段无效")
		}
		owner = uniqueOwnerNameSQL
		extra = ` AND COALESCE(l.current_owner_user_id,'')<>'' AND COALESCE(l.ownership_state,'') NOT IN ('public_pool','assigned_pending_claim','pending_assignment') AND COALESCE(l.status,'') NOT IN ('public_pool','assigned_pending_claim','pending_dispatch')`
		groupOrder = fmt.Sprintf(" GROUP BY l.current_owner_user_id,1 ORDER BY %d DESC,1", rank.position)
		title = rank.column + "排名"
		chart.Y = []string{rank.column}
		if rank.column == "成交率(%)" {
			additional = `,ROUND(100.0*COALESCE(SUM(` + convertedSQL + `),0)/COUNT(*),2) AS "成交率(%)"`
		} else if rank.column == "成交金额(元)" {
			additional = `,` + amountYuanSQL("l.deal_amount_cents") + ` AS "成交金额(元)"`
		} else if rank.column == "退款金额(元)" {
			additional = `,` + amountYuanSQL("l.refund_amount_cents") + ` AS "退款金额(元)"`
		}
	}
	return []metricQuery{{sql: `SELECT ` + owner + ` AS "当前归属",COUNT(*) AS "线索数",COALESCE(SUM(` + convertedSQL + `),0) AS "成交数",COALESCE(SUM(l.result_status='invalid'),0) AS "无效数",` + netYuanSQL + ` AS "净成交额(元)"` + additional + ` FROM leads l WHERE ` + f.clause() + extra + groupOrder, args: f.args, title: title, chart: chart}}, nil
}

func findMetric(id string) *metricDef {
	for i := range metricCatalog {
		if metricCatalog[i].ID == id {
			return &metricCatalog[i]
		}
	}
	return nil
}

// normalizeMetricParams drops parameters the metric does not declare and
// returns the names of what was dropped.
func normalizeMetricParams(m *metricDef, p MetricParams) (MetricParams, []string) {
	allowed := map[string]bool{}
	for _, name := range m.Params {
		allowed[name] = true
	}
	var dropped []string
	clean := func(name string, set bool, clear func()) {
		if set && !allowed[name] {
			dropped = append(dropped, name)
			clear()
		}
	}
	trim := func(values []string) []string {
		out := []string{}
		for _, v := range values {
			if v = strings.TrimSpace(v); v != "" && len(out) < 20 {
				out = append(out, v)
			}
		}
		return out
	}
	if len(p.Sources) > 20 {
		dropped = append(dropped, "sources_over_limit")
	}
	if len(p.ExcludeSources) > 20 {
		dropped = append(dropped, "exclude_sources_over_limit")
	}
	p.OwnerID = strings.TrimSpace(p.OwnerID)
	p.Sources, p.ExcludeSources = trim(p.Sources), trim(p.ExcludeSources)
	p.DateFrom, p.DateTo, p.Owner, p.Customer, p.Keyword, p.DueBefore = strings.TrimSpace(p.DateFrom), strings.TrimSpace(p.DateTo), strings.TrimSpace(p.Owner), strings.TrimSpace(p.Customer), strings.TrimSpace(p.Keyword), strings.TrimSpace(p.DueBefore)
	clean("date_from", p.DateFrom != "", func() { p.DateFrom = "" })
	clean("date_to", p.DateTo != "", func() { p.DateTo = "" })
	clean("sources", len(p.Sources) > 0, func() { p.Sources = nil })
	clean("exclude_sources", len(p.ExcludeSources) > 0, func() { p.ExcludeSources = nil })
	clean("owner", p.Owner != "" || p.OwnerID != "", func() { p.Owner = ""; p.OwnerID = "" })
	clean("customer", p.Customer != "", func() { p.Customer = "" })
	clean("keyword", p.Keyword != "", func() { p.Keyword = "" })
	clean("due_before", p.DueBefore != "", func() { p.DueBefore = "" })
	sort.Strings(dropped)
	return p, dropped
}

// describeMetricParams renders the applied filters for people (and history).
func describeMetricParams(p MetricParams) string {
	parts := []string{}
	if p.DateFrom != "" || p.DateTo != "" {
		parts = append(parts, fmt.Sprintf("日期 %s 至 %s", orAll(p.DateFrom, "最早"), orAll(p.DateTo, "最新")))
	}
	if len(p.Sources) > 0 {
		parts = append(parts, "来源："+strings.Join(p.Sources, "、"))
	}
	if len(p.ExcludeSources) > 0 {
		parts = append(parts, "排除来源："+strings.Join(p.ExcludeSources, "、"))
	}
	if p.Owner != "" {
		parts = append(parts, "人员："+p.Owner)
	}
	if p.Customer != "" {
		parts = append(parts, "客户："+p.Customer)
	}
	if p.Keyword != "" {
		parts = append(parts, "关键词："+p.Keyword)
	}
	if p.DueBefore != "" {
		parts = append(parts, "截止早于："+p.DueBefore)
	}
	if len(parts) == 0 {
		return "全部数据"
	}
	return strings.Join(parts, "；")
}

func orAll(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

type metricOutput struct {
	Selection MetricSelection
	Title     string
	Results   []*Result
	Blocks    []Block
	Dropped   []string
}

// runMetricForQuestion derives presentation only from the current question.
func (s *Service) runMetricForQuestion(ctx context.Context, snapshot *Snapshot, runID string, sel MetricSelection, question string) (*metricOutput, error) {
	if sel.Comparison != nil {
		definition, err := comparisonDefinition(sel)
		if err != nil {
			return nil, err
		}
		if sel.MetricID == "owner_current" {
			sel.rankColumn = definition.column
		}
	} else if sel.MetricID == "owner_current" {
		sel.rankColumn = ownerRankingColumn(question)
	}
	return s.runMetric(ctx, snapshot, runID, sel)
}

// runMetric executes one fixed metric on the run snapshot and saves its results.
func (s *Service) runMetric(ctx context.Context, snapshot *Snapshot, runID string, sel MetricSelection) (*metricOutput, error) {
	m := findMetric(sel.MetricID)
	if m == nil {
		return nil, fmt.Errorf("未知指标 %q", sel.MetricID)
	}
	validated, err := (metricScope{}).apply(ctx, snapshot, []MetricSelection{sel})
	if err != nil {
		return nil, err
	}
	params, dropped := normalizeMetricParams(m, validated[0].Params)
	for _, name := range m.Required {
		if name == "keyword" && params.Keyword == "" {
			return nil, errors.New("该指标需要关键词")
		}
	}
	if params.Keyword != "" {
		safe, excluded := snapshot.Sanitize(params.Keyword)
		if excluded != 0 || safe != params.Keyword {
			return nil, errors.New("关键词包含无法用于查询的敏感片段，请换用业务关键词")
		}
	}
	queries, err := m.build(params)
	if m.ID == "owner_current" && sel.rankColumn != "" {
		queries, err = buildOwnerCurrent(params, sel.rankColumn)
	}
	if err != nil {
		return nil, err
	}
	if sel.Comparison != nil {
		definition, err := comparisonDefinition(sel)
		if err != nil {
			return nil, err
		}
		for i := range queries {
			queries[i].sql = `SELECT * FROM (` + queries[i].sql + `) ORDER BY "` + definition.column + `" IS NULL,"` + definition.column + `" ` + strings.ToUpper(sel.Comparison.Order) + `,"` + definition.axis + `"`
			queries[i].title = definition.subject + "比较"
			queries[i].chart = &metricChart{"bar", definition.axis, []string{definition.column}}
		}
	}
	out := &metricOutput{Selection: MetricSelection{MetricID: m.ID, Params: params, Comparison: sel.Comparison, TimeBasis: sel.TimeBasis}, Title: m.Title, Dropped: dropped}
	for _, q := range queries {
		if sel.rankColumn != "" && m.ID == "owner_current" {
			_ = s.event(runID, "answer_intent", map[string]any{"dimension": "current_owner", "rank_by": sel.rankColumn, "order": "desc"})
		}
		r, err := snapshot.QueryFixed(ctx, q.sql, q.args, 200)
		if err != nil {
			return nil, fmt.Errorf("指标“%s”读取失败：%w", m.Title, err)
		}
		full, materializeErr := snapshot.MaterializeResult(ctx, r, 100000)
		if materializeErr != nil {
			r.Coverage += " 完整导出读取未完成，当前仅保存预览。"
			full = r
		}
		if full.Truncated || int64(len(full.Rows)) < full.TotalRows {
			return nil, errors.New("结果超出完整保存范围，请缩小查询条件；未发布不完整统计")
		}
		if err := s.saveResult(ctx, runID, full); err != nil {
			return nil, err
		}
		r.MaterializedRows, r.MaterializedComplete = full.MaterializedRows, full.MaterializedComplete
		r.HasMore = len(r.Rows) < full.MaterializedRows
		title := m.Title
		if q.title != "" {
			title += "·" + q.title
		}
		title += "（" + describeMetricParams(params) + "）"
		out.Results = append(out.Results, full)
		out.Blocks = append(out.Blocks, Block{Type: "table", Title: title, ResultID: r.ID, Result: r})
		if q.chart != nil && len(r.Rows) > 1 && len(r.Rows) <= 50 {
			chart := Block{Type: "chart", Title: title, ResultID: r.ID, Result: r, ChartType: q.chart.Type, XColumn: q.chart.X, YColumns: q.chart.Y}
			if ValidateChart(chart, *r) == nil {
				out.Blocks = append(out.Blocks, chart)
			}
		}
	}
	return out, nil
}
