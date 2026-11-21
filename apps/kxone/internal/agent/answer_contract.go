package agent

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"
)

type AnswerReference struct {
	Token      string `json:"token"`
	FactID     string `json:"fact_id"`
	ResultID   string `json:"result_id"`
	Row        *int   `json:"row"`
	Column     string `json:"column"`
	Level      string `json:"level"`
	Comparison string `json:"comparison,omitempty"`
}
type FinalizeRequest struct {
	ResultIDs      []string            `json:"result_ids"`
	TableResultIDs []string            `json:"table_result_ids,omitempty"`
	Markdown       string              `json:"markdown"`
	References     []AnswerReference   `json:"references"`
	Conditions     []AnalysisCondition `json:"conditions"`
}
type AnswerVerification struct {
	Version    int               `json:"version"`
	Status     string            `json:"status"`
	References []AnswerReference `json:"references"`
}

var answerToken = regexp.MustCompile(`\{\{([a-zA-Z][a-zA-Z0-9_]{0,39})\}\}`)
var validReferenceToken = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,39}$`)
var safeIdentity = regexp.MustCompile(`(?:客户|员工)#[0-9]+`)
var dateLiteral = regexp.MustCompile(`[0-9]{4}-[0-9]{2}-[0-9]{2}(?:T[0-9:.+Z-]+)?`)
var answerNumber = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?|[零〇一二两三四五六七八九十百千万亿]+\s*(?:条|个|人|次|项|笔|份|户|组|位|名|件|单|批|家|元|分|秒|小时|天|成|％|%)`)
var markdownOrdinal = regexp.MustCompile(`(?m)^\s*[0-9]+[.)、]\s+`)

// unsupportedScope still guards SQL column aliases (see ValidateAnswer).
var unsupportedScope = regexp.MustCompile(`全库|全表|全局|合计|总计|互不重叠|无重叠|没有重叠|从未|不曾|全部|所有|无遗漏|(?:无|没有)未匹配|均能归`)

// Mapping-completeness claims cannot be proven by any single result, so they
// remain hard rejections in prose.
var hardScopeClaim = regexp.MustCompile(`互不重叠|无重叠|没有重叠|无遗漏|(?:无|没有)未匹配|均能归`)

// Ordinary totality words (v27 second run was rejected only for “全部”).
// They are accepted when every adopted result is complete; the server then
// appends an explicit scope note. They are rejected only when an adopted
// result is truncated or only partially materialized.
var softScopeClaim = regexp.MustCompile(`全库|全表|全局|合计|总计|从未|不曾|全部|所有`)
var causalAssertion = regexp.MustCompile(`(?:导致|造成|影响).{0,10}(?:流失|退款|取消)|(?:未|没有|不会)(?:导致|造成|影响)`)

// A conservative publication gate, not a proof of arbitrary natural-language
// semantics. Statistic values and scope labels are rendered below from evidence.
func validateNarrative(text string) error {
	return validateNarrativeWith(text, nil)
}

func validateNarrativeWith(text string, bound *boundNumbers) error {
	plain := norm.NFKC.String(text)
	problems := []string{}
	if strings.Contains(plain, "|") {
		problems = append(problems, "正文不接受模型数据表，请使用present_result")
	}
	if regexp.MustCompile(`\}\}\s*[+*/=]\s*\{\{`).MatchString(plain) {
		problems = append(problems, "不可在事实令牌之间自行进行计算，请查询实际计算结果")
	}
	plain = answerToken.ReplaceAllString(plain, "")
	if strings.Contains(plain, "{{") || strings.Contains(plain, "}}") {
		problems = append(problems, "事实令牌格式无效")
	}
	plain = markdownOrdinal.ReplaceAllString(plain, "")
	plain = safeIdentity.ReplaceAllString(plain, "")
	plain = dateLiteral.ReplaceAllString(plain, "")
	if numbers := uniqueAnswerIssues(unboundNumberLiterals(plain, bound)); len(numbers) > 0 {
		problems = append(problems, fmt.Sprintf("正文发现未绑定数量 %q；数字和中文数量都须引用事实或结果单元格，或删除没有证据的附加结论", numbers))
	}
	if unboundTemporalClaim.MatchString(plain) {
		problems = append(problems, "正文发现未绑定时间关系；请引用已读取时间单元格并设置comparison=snapshot，由服务端比较，正文不能从scheduled或其他状态推断时间先后")
	}
	scopeIssues := []string{}
	for _, clause := range strings.FieldsFunc(plain, func(r rune) bool { return strings.ContainsRune("。；;\n！？", r) }) {
		if hardScopeClaim.MatchString(clause) || causalAssertion.MatchString(clause) {
			if strings.Contains(clause, "无法证明") || strings.Contains(clause, "不能证明") || strings.Contains(clause, "不足以证明") || strings.Contains(clause, "不能据此") || strings.Contains(clause, "不代表") || strings.Contains(clause, "不等于") {
				continue
			}
			scopeIssues = append(scopeIssues, hardScopeClaim.FindAllString(clause, -1)...)
			scopeIssues = append(scopeIssues, causalAssertion.FindAllString(clause, -1)...)
		}
	}
	if scopes := uniqueAnswerIssues(scopeIssues); len(scopes) > 0 {
		problems = append(problems, fmt.Sprintf("文字中的 %q 缺少受控依据，请改用事实引用或明确证据不足；分组相加相等不能证明人员映射无遗漏", scopes))
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "；"))
	}
	return nil
}

func uniqueAnswerIssues(issues []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, issue := range issues {
		if !seen[issue] && len(out) < 12 {
			seen[issue] = true
			out = append(out, issue)
		}
	}
	return out
}

func ValidateAnswer(req FinalizeRequest, selected []string, results map[string]*Result, readViews ...*evidenceReadState) (string, *AnswerVerification, error) {
	if req.ResultIDs != nil {
		selected = req.ResultIDs
	}
	if selected == nil {
		return "", nil, errors.New("先调用select_final_results明确采用的依据，无数据时选择[]")
	}
	if err := ValidateFinalResultIDs(selected, results); err != nil {
		return "", nil, err
	}
	if len(req.TableResultIDs) > 10 {
		return "", nil, errors.New("最终展示表格过多")
	}
	selectedSet := map[string]bool{}
	for _, id := range selected {
		selectedSet[id] = true
	}
	for _, id := range req.TableResultIDs {
		if !selectedSet[id] || results[id] == nil {
			return "", nil, errors.New("最终表格只能引用本轮最终采用的真实结果")
		}
	}
	if len(req.Markdown) > 32768 || len(req.References) > 100 {
		return "", nil, errors.New("答案或事实引用过多")
	}
	publicationErrors := []string{}
	req.References, publicationErrors = normalizeAnswerReferences(req.Markdown, req.References)
	if err := validateNarrativeWith(req.Markdown, collectBoundNumbers(req.References, results, readViews...)); err != nil {
		publicationErrors = append(publicationErrors, err.Error())
	}
	if err := validateReferencePlacement(req.Markdown, req.References); err != nil {
		publicationErrors = append(publicationErrors, err.Error())
	}
	scopeNote, scopeErr := softScopeReview(req.Markdown, selected, results)
	if scopeErr != nil {
		publicationErrors = append(publicationErrors, scopeErr.Error())
	}
	if len(publicationErrors) > 0 {
		return "", nil, errors.New(strings.Join(publicationErrors, "；"))
	}
	allowed := map[string]bool{}
	for _, id := range selected {
		allowed[id] = true
	}
	replacements := map[string]string{}
	for _, ref := range req.References {
		if ref.Comparison != "" && (ref.Comparison != "snapshot" || ref.FactID != "") {
			return "", nil, errors.New("时间比较仅允许已读取单元格的comparison=snapshot，不能用于事实或其他比较方式")
		}
		if ref.FactID != "" {
			if ref.ResultID != "" || ref.Row != nil || ref.Column != "" {
				return "", nil, errors.New("事实与单元格引用不能混用")
			}
			found := false
			for id, r := range results {
				if !allowed[id] {
					continue
				}
				for _, f := range r.Facts {
					if f.ID != ref.FactID {
						continue
					}
					if r.MeasureScope == nil || f.ResultID != id || f.ScopeID != r.MeasureScope.ID || !f.SnapshotAt.Equal(r.SnapshotAt) || ref.Level != f.Level {
						return "", nil, errors.New("事实范围、层级或快照不匹配")
					}
					metric := measuredMetricLabel(f)
					if metric == "" {
						return "", nil, errors.New("事实指标未知")
					}
					scope := "本次筛选集合"
					if r.MeasureScope.Kind == "whole_source" {
						scope = "原表 " + r.MeasureScope.Source + "（" + r.MeasureScope.Visibility + "）"
					}
					if f.Level == "group" {
						groupName := map[string]string{"actor_user_id": "操作人", "record_type": "记录类型", "business_stage": "业务阶段", "occurred_date_shanghai": "上海日期", "source_channel": "来源"}[r.MeasureScope.GroupBy]
						label := f.GroupLabel
						if label == "" {
							label = f.Group
						}
						scope += "；" + groupName + "=" + label
					} else if f.Level != "global" {
						return "", nil, errors.New("事实层级未知")
					}
					replacements[ref.Token] = fmt.Sprintf("**%s：%d**（%s）", metric, f.Value, escapeAnswerLabel(scope))
					found = true
				}
			}
			if !found {
				return "", nil, errors.New("事实不属于本轮最终选定结果")
			}
		} else {
			r := results[ref.ResultID]
			if r == nil || !allowed[ref.ResultID] || ref.Row == nil || *ref.Row < 0 {
				return "", nil, errors.New("单元格引用不属于本轮最终结果或已读取范围")
			}
			column := -1
			for i, c := range r.Columns {
				if c == ref.Column {
					column = i
					break
				}
			}
			if column < 0 {
				return "", nil, errors.New("单元格列不存在")
			}
			if ref.Level != "" && ref.Level != "cell" {
				return "", nil, errors.New("普通SQL单元格不能声明全局去重事实")
			}
			if unsupportedScope.MatchString(norm.NFKC.String(ref.Column)) || strings.Contains(strings.ToLower(ref.Column), "distinct_lead") || strings.Contains(ref.Column, "去重线索") {
				return "", nil, errors.New("全局或去重线索主张不能通过SQL列别名代替实体事实，请使用measure_record_set")
			}
			localRow := *ref.Row - r.Offset
			rowValues := map[string]any{}
			if len(readViews) > 0 {
				if err := readViews[0].validate([]AnswerReference{ref}); err != nil {
					return "", nil, err
				}
				rowValues = readViews[0].values[ref.ResultID][*ref.Row]
			} else {
				if localRow < 0 || localRow >= len(r.Rows) || column >= len(r.Rows[localRow]) {
					return "", nil, errors.New("引用单元格超出已读取范围")
				}
				for i, v := range r.Rows[localRow] {
					if i < len(r.Columns) {
						rowValues[r.Columns[i]] = v
					}
				}
			}
			value := rowValues[ref.Column]
			text := textValue(value)
			if value == nil {
				text = "未知"
			}
			if len(text) > 512 {
				return "", nil, errors.New("长文字应引用来源记录，不能作为数字单元格")
			}
			labels := []string{}
			for i, c := range r.Columns {
				v := rowValues[c]
				if i != column {
					if s, ok := v.(string); ok && len(s) <= 80 {
						labels = append(labels, r.Columns[i]+"="+s)
					}
				}
			}
			replacements[ref.Token] = "**查询指标 " + escapeAnswerLabel(ref.Column) + "：" + escapeAnswerLabel(text) + "**（查询结果行" + strconv.Itoa(*ref.Row+1) + "；" + escapeAnswerLabel(strings.Join(labels, "，")) + "）"
			if ref.Comparison == "snapshot" {
				comparison, err := compareCellToSnapshot(value, r.SnapshotAt)
				if err != nil {
					return "", nil, err
				}
				replacements[ref.Token] += "\n\n" + comparison
			}
		}
	}
	used := map[string]bool{}
	missing := false
	out := answerToken.ReplaceAllStringFunc(req.Markdown, func(token string) string {
		key := answerToken.FindStringSubmatch(token)[1]
		value, ok := replacements[key]
		if !ok {
			missing = true
			return ""
		}
		used[key] = true
		return value
	})
	if missing || len(used) != len(replacements) {
		// All references may omit textual placeholders altogether. The server
		// then appends the complete typed statements, avoiding a model-authored
		// noun or unit around a correctly calculated but different statistic.
		statements := []string{}
		for _, ref := range req.References {
			if !used[ref.Token] {
				statements = append(statements, replacements[ref.Token])
				used[ref.Token] = true
			}
		}
		if len(statements) > 0 {
			out += "\n\n**核对数据**\n\n" + strings.Join(statements, "\n\n")
		}
		if missing || len(used) != len(replacements) {
			return "", nil, errors.New("正文事实令牌与引用清单不一致")
		}
	}
	if strings.TrimSpace(out) == "" {
		return "", nil, errors.New("最终回答不能为空")
	}
	if scopeNote != "" {
		out += "\n\n" + scopeNote
	}
	return out, &AnswerVerification{Version: 6, Status: "validated", References: append([]AnswerReference{}, req.References...)}, nil
}

// A token is only an explicit text-template binding. When there is no text
// template, the server can safely assign presentation IDs in reference order;
// result/fact identity still comes exclusively from the validated fields.
// Never repair a duplicate supplied name, or guess any placeholder binding.
func normalizeAnswerReferences(markdown string, input []AnswerReference) ([]AnswerReference, []string) {
	refs := append([]AnswerReference{}, input...)
	problems := []string{}
	used := map[string]bool{}
	duplicates := []string{}
	hasTemplate := strings.Contains(markdown, "{{") || strings.Contains(markdown, "}}")
	for i, ref := range refs {
		if ref.Token == "" {
			if hasTemplate {
				problems = append(problems, fmt.Sprintf("第%d个引用缺少token，正文使用占位符时不能猜测其绑定；可移除全部正文占位符并让服务端自动列出引用", i+1))
			}
		} else if !validReferenceToken.MatchString(ref.Token) {
			problems = append(problems, fmt.Sprintf("第%d个引用token格式无效；须字母开头、仅字母数字下划线、最长40字符", i+1))
		} else {
			if used[ref.Token] {
				duplicates = append(duplicates, ref.Token)
			}
			used[ref.Token] = true
		}
		if ref.FactID != "" {
			if ref.ResultID != "" || ref.Row != nil || ref.Column != "" {
				problems = append(problems, fmt.Sprintf("第%d个引用混用了事实与单元格字段", i+1))
			}
			if ref.Level != "global" && ref.Level != "group" {
				problems = append(problems, fmt.Sprintf("第%d个事实引用level须为global或group且匹配已读取事实", i+1))
			}
		} else {
			if ref.ResultID == "" || ref.Row == nil || (ref.Row != nil && *ref.Row < 0) || ref.Column == "" {
				problems = append(problems, fmt.Sprintf("第%d个单元格引用须提供result_id、非负row和column；全局/分组事实须用fact_id", i+1))
			}
			if ref.Level != "" && ref.Level != "cell" {
				problems = append(problems, fmt.Sprintf("第%d个单元格引用level只能为cell，不能冒充全局/分组事实", i+1))
			}
		}
	}
	if len(duplicates) > 0 {
		problems = append(problems, fmt.Sprintf("引用token重复 %q；每个显式token只能绑定一个引用。无需分别引用同行姓名，服务端会附行标签；也可移除正文占位符并省略token", uniqueAnswerIssues(duplicates)))
	}
	missing := []string{}
	for _, token := range answerToken.FindAllStringSubmatch(markdown, -1) {
		if !used[token[1]] {
			missing = append(missing, token[1])
		}
	}
	if len(missing) > 0 {
		problems = append(problems, fmt.Sprintf("正文占位符 %q 缺少唯一引用绑定；不可猜测，请提供对应token或移除正文占位符", uniqueAnswerIssues(missing)))
	}
	if !hasTemplate {
		for i := range refs {
			if refs[i].Token != "" {
				continue
			}
			for n := i + 1; ; n++ {
				token := fmt.Sprintf("reference_%d", n)
				if !used[token] {
					refs[i].Token = token
					used[token] = true
					break
				}
			}
		}
	}
	return refs, problems
}

func validateReferencePlacement(markdown string, references []AnswerReference) error {
	tokens := map[string]bool{}
	for _, ref := range references {
		tokens[ref.Token] = true
	}
	for _, paragraph := range regexp.MustCompile(`\n\s*\n`).Split(markdown, -1) {
		hasReference := false
		for _, token := range answerToken.FindAllStringSubmatch(paragraph, -1) {
			hasReference = hasReference || tokens[token[1]]
		}
		if !hasReference {
			continue
		}
		remaining := answerToken.ReplaceAllString(paragraph, "")
		remaining = markdownOrdinal.ReplaceAllString(remaining, "")
		remaining = strings.Trim(remaining, " \n\r\t*_`-•.。,:：，;；()（）")
		if remaining != "" {
			return errors.New("事实及单元格引用必须独立成段，不能在令牌前后附加指标名称、实体或量词；可从markdown移除令牌，保留references，由服务端自动列出完整数据陈述。有效示例：markdown=已核对查询结果。，references保留真实引用；或markdown={{n}}。解释另起空行。SQL列名仅为查询指标，不代表业务语义已获证明")
		}
	}
	return nil
}

func measuredMetricLabel(f MeasuredFact) string {
	entity := "业务记录"
	if len(f.RecordTypes) == 1 {
		labels := map[string]string{"leads": "线索记录", "lead_followups": "跟进记录", "lead_dispatch_records": "分配转派记录", "lead_appointments": "预约记录", "lead_appointment_events": "预约事件", "closer_availability": "排班记录", "lead_pool_records": "公海流转记录", "lead_activity_records": "业务活动记录", "lead_reminder_records": "提醒记录", "crm_wecom_user_bindings": "员工账号记录", "channel_integrations": "渠道接入记录", "channel_integration_audits": "渠道审计记录", "group_robot_notifications": "通知记录", "wecom_card_tasks": "旧企微卡片记录", "wecom_callback_logs": "旧企微回调记录", "lead_deletion_events": "删除恢复事件", "xiangwu_form_submissions": "官网表单记录"}
		if label := labels[f.RecordTypes[0]]; label != "" {
			entity = label
		}
	}
	switch f.Metric {
	case "distinct_records":
		return "去重" + entity + "数"
	case "distinct_leads":
		return "去重线索数"
	case "records_without_lead":
		return "未关联线索的" + entity + "数"
	case "historical_followup_plan_records":
		if containsRecordType(f.RecordTypes, "lead_followups") {
			return "有历史回访计划的跟进记录数"
		}
	}
	return ""
}

func escapeAnswerLabel(s string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "*", "\\*", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "`", "\\`", "\n", " ", "\r", " ")
	return replacer.Replace(s)
}

// softScopeReview returns a server-authored scope note for ordinary totality
// words, or an error only when such a word is used over an incomplete result.
// Negated/limiting clauses (“不能证明全部…”) are ignored.
func softScopeReview(markdown string, selected []string, results map[string]*Result) (string, error) {
	plain := answerToken.ReplaceAllString(norm.NFKC.String(markdown), "")
	terms := []string{}
	for _, clause := range strings.FieldsFunc(plain, func(r rune) bool { return strings.ContainsRune("。；;\n！？", r) }) {
		if !softScopeClaim.MatchString(clause) || scopeLimitingClause(clause) {
			continue
		}
		terms = append(terms, softScopeClaim.FindAllString(clause, -1)...)
	}
	terms = uniqueAnswerIssues(terms)
	if len(terms) == 0 {
		return "", nil
	}
	parts := []string{}
	for _, id := range selected {
		r := results[id]
		if r == nil {
			continue
		}
		if !resultComplete(r) {
			return "", fmt.Errorf("正文使用 %q 声称全量，但采用结果 %s 已截断或未完整物化（已有%d行/共%d行）；请删去全量措辞或重新查询完整结果", terms, id, len(r.Rows), r.TotalRows)
		}
		parts = append(parts, fmt.Sprintf("结果 %s 共 %d 行", id, r.TotalRows))
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("正文使用 %q 声称全量，但没有采用任何结果作为范围依据", terms)
	}
	return fmt.Sprintf("> 范围说明：正文中的%s仅指本次采用结果在数据快照内的完整范围（%s），不代表快照之外的数据。", "“"+strings.Join(terms, "”“")+"”", strings.Join(parts, "；")), nil
}

func scopeLimitingClause(clause string) bool {
	for _, marker := range []string{"无法证明", "不能证明", "不足以证明", "不能据此", "不代表", "不等于", "未读完", "并非"} {
		if strings.Contains(clause, marker) {
			return true
		}
	}
	return false
}

func resultComplete(r *Result) bool {
	if r.Truncated || r.Offset != 0 {
		return false
	}
	if r.MeasureScope != nil && !r.MaterializedComplete {
		return false
	}
	return int64(len(r.Rows)) >= r.TotalRows
}
