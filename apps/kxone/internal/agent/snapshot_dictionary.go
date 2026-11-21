package agent

import (
	"strings"

	"github.com/lsaac128/kxone/pkg/types"
)

// Dictionary relationships list each verified business key explicitly. Numeric
// account IDs and similarly named business identifiers are not interchangeable.
type SnapshotRelationship struct {
	FromTable   string   `json:"from_table"`
	FromColumns []string `json:"from_columns"`
	ToTable     string   `json:"to_table"`
	ToColumn    string   `json:"to_column"`
	Cardinality string   `json:"cardinality"`
	Separator   string   `json:"separator,omitempty"`
	Meaning     string   `json:"meaning,omitempty"`
}

type SnapshotMetricDefinition struct {
	Table      string `json:"table"`
	FilterSQL  string `json:"filter_sql"`
	ValueSQL   string `json:"value_sql"`
	DateColumn string `json:"date_column,omitempty"`
	Meaning    string `json:"meaning"`
}

type SnapshotUnavailableFact struct {
	MissingEvidence []string `json:"missing_evidence"`
	InvalidProxies  []string `json:"invalid_proxies"`
	Meaning         string   `json:"meaning"`
}

func snapshotEnums() map[string]map[string]string {
	return map[string]map[string]string{
		"leads.status": {
			string(types.LeadStatusPendingDispatch): "待分配", string(types.LeadStatusAssignedPendingClaim): "已分配待领取",
			string(types.LeadStatusClaimed): "已领取", string(types.LeadStatusFollowing): "跟进中", string(types.LeadStatusPublicPool): "公海",
			string(types.LeadStatusConverted): "当前已成交", string(types.LeadStatusInvalid): "无效", string(types.LeadStatusClosed): "已关闭/未成交",
		},
		"business_stage":      {string(types.BusinessStageInvitation): "邀约", string(types.BusinessStageNegotiation): "谈单", string(types.BusinessStageFinished): "已终结，包含成交、无效和关闭"},
		"leads.result_status": {string(types.LeadResultActive): "进行中", string(types.LeadResultConverted): "成交", string(types.LeadResultInvalid): "无效", string(types.LeadResultClosed): "关闭/未成交"},
		"leads.ownership_state": {
			string(types.OwnershipPendingAssignment): "待分配", string(types.OwnershipAssignedPendingClaim): "待领取", string(types.OwnershipPrivate): "已归属",
			string(types.OwnershipPublicPool): "公海", string(types.OwnershipAppointmentPending): "预约待分配", string(types.OwnershipAppointmentScheduled): "预约已安排",
		},
		"leads.current_owner_role / lead_followups.actor_role": {string(types.OwnerRoleInviter): "邀约师", string(types.OwnerRoleCloser): "谈单师"},
		"lead_appointments.status": {
			string(types.AppointmentPendingAssignment): "待分配谈单师", string(types.AppointmentScheduled): "已安排",
			string(types.AppointmentArrivedDelivered): "已到访交付", string(types.AppointmentNoShow): "爽约",
			string(types.AppointmentCancelled): "已取消", string(types.AppointmentRescheduled): "已改期",
		},
		"leads.payment_status": {"partial": "部分付款标记", "paid": "已付款标记", "refunded": "退款标记", "": "未填写/未知"},
	}
}

func snapshotFieldMeanings() map[string]string {
	return map[string]string{
		"UTC absolute time columns":                                  "原始绝对时间列是 UTC 文本，固定9位小数并以 Z 结尾：YYYY-MM-DDTHH:mm:ss.000000000Z。SQL 比较字面量和参数必须同格式，先把输入时区换算为 UTC；不可混用无小数 Z、空格分隔或 +08:00 与 UTC 文本比较。例如严格早于 2026-10-08T02:00:00.000000000Z，只包含其前1纳秒，不含该时刻或后1纳秒；省略小数会产生字典序边界错误，不能算边界通过，也不静默重写 SQL。上海日期统计优先用 *_date_shanghai。",
		"snapshot_at / snapshot_at_shanghai":                         "同一数据时点的 UTC 与上海时间表示。回答中的截至日期和今日使用 snapshot_at_shanghai（Asia/Shanghai）；不能截取 UTC 日期冒充上海日期。跨日时两者日期可不同，绝对时刻相同。",
		"leads.current_owner_user_id":                                "当前已领取负责人的业务字符串 ID。归属分类先区分待领取、公海，再统计实际当前负责人，最后列明确无负责人；空值不是员工关联失败。assigned_sales_user_id/assigned_inviter_user_id 是待接收者，不代表已领取或已接手。使用 current_ownership_category 互斥分类后仅对 owner: 类别关联员工；已成交和无效若有当前负责人仍计入其名下。状态与归属字段矛盾须披露异常。",
		"leads.current_followup_eligible / current_followup_overdue": "当前普通回访资格和截至快照的逾期标志，复用 CRM 判断；0 只说明不符合资格，不能独自证明排除原因。解释原因须读取当前 status/result_status、ownership_state、current_owner_user_id、next_followup_due_at、current_appointment_id，并按预约 id 与 lead_id 回查 lead_appointments 的有效状态 pending_assignment/scheduled。预约指针残留但记录取消/爽约/缺失时须披露异常，不能声称有有效预约。提醒记录缺失或存在都不是资格证据；历史跟进计划不是独立当前待办。",
		"leads.status":                                            "当前业务状态的权威字段；当前成交按 status='converted'。不能以时间、金额非空或 finished 代替。",
		"leads.result_status":                                     "双阶段结果字段；历史空值由 CRM 按 status 投影，分析快照保留原值。与 status 不一致的非空值要单列异常，不用 OR 合并、静默覆盖或猜测修复。",
		"leads.converted_at":                                      "登记成交的时间，只用于成交发生日期分析。当前成交可能缺失日期，非成交记录也可能有历史残留；缺失日期不能排除当前成交，残留日期不能证明当前成交。",
		"leads.payment_status":                                    "当前付款状态标记，无实际到账金额或到账时间；枚举为现行写入规则，历史其他值保留原样并说明未知含义，空值不等于未付或零收入。",
		"leads.net_deal_amount_cents":                             "当前成交金额减当前退款金额，单位分；不是支付流水、到账金额或某期间退款流水。",
		"lead_followups.next_followup_at":                         "该次跟进记录中保存的历史回访计划时间；非空才计为有历史计划的跟进记录。跟进备注或记录存在本身不能证明安排了回访；is_valid等历史有效性需按问题另行筛选。计划条数不等于当前待办数，当前待办以线索当前资格与截止时间判断。",
		"crm_wecom_user_bindings.wecom_userid":                    "业务使用的字符串员工标识，是 relationships 所列人员字段的关联目标；手机号式标识脱敏后仍保持同一关联值。",
		"crm_wecom_user_bindings.id / crm_user_id":                "数字账号标识，不是业务人员字符串 ID；不可与 leads/followups/dispatch 等人员字段关联或强制转换后关联。",
		"crm_wecom_user_bindings.employee_name":                   "通过业务字符串键关联后的员工展示名。用 LEFT JOIN 保留空值、system、历史失配者，注明未匹配；不能因此删除记录或推断该人员没有业绩。历史分析不因账号现已停用而排除。",
		"lead_reminder_records.receiver_user_id":                  "接收者集合，不是操作人；回访升级提醒可用 | 分隔负责人和主管，预约提醒通常每人一条。先按 | 拆分、去空白/空项，每个标识关联员工 wecom_userid；不能整串直接等值关联或按无边界子串匹配。统计提醒条数按提醒 id 去重，接收人数另计。",
		"analysis_records.id / record_type / record_id":           "id=record_type:record_id；record_id 只在原表内唯一，不等于 lead_id。证据定位用表名和原表主键，跨表关联客户用明确的 lead_id。",
		"analysis_records.occurred_at":                            "检索索引时间通常取原记录 created_at，删除/恢复记录取 occurred_at；不统一代表所有业务动作发生时间。状态表内容是快照当前值，不能把当前负责人、状态、取消原因等归到创建时点。无可信索引时间的记录为 NULL，日期筛选不覆盖它们。精确业务时间查询原表的相应时间列。",
		"analysis_records.business_stage":                         "仅保留原记录明确存储的 business_stage：leads 是当前阶段，lead_followups 是提交时阶段；没有该字段的原表保持 NULL。按阶段检索会排除阶段未知的事件，须说明覆盖限制，不能用线索当前阶段回填历史活动。",
		"analysis_records.event_type":                             "原表事件/动作/提醒类别的检索字段，不统一表示业务执行成功。提醒类别须结合 status/sent_at 判断执行；企微回调的来源 event_key 与 from_user_id 仅表明收到何种回调及发起人，须结合 process_status 和真实业务记录确认结果，不能把 failed 回调算作成功领取。",
		"lead_appointments.created_at / start_at / arrived_at":    "分别为预约记录创建时间、当前计划到访时间、实际登记到访时间；改期/取消等动作看 lead_appointment_events.created_at。当前 inviter_user_id/closer_user_id 是当前参与者，不能当原始创建者；原始创建者只取该预约 created 事件的 actor_user_id，缺失则未知。",
		"lead_reminder_records.created_at / due_at / sent_at":     "分别为提醒记录创建、计划触发、执行记录时间；状态与 sent_at 共同用于分析发送。reminder_type 是提醒类别，不证明已发生/已发送，created_at 不能代替 due_at 或 sent_at。",
		"group_robot_notifications.created_at / due_at / sent_at": "created_at 是通知业务来源基准时间，补偿扫描可能沿用线索创建/分配或原提醒创建时间；不必等于通知行入库或执行时间。due_at 是计划触发时间，发送状态与 sent_at 用于执行分析。",
		"wecom_card_tasks.sent_at":                                "旧企微卡片字段；发送前占位、入队失败补偿也会写入该时间，非空不证明发送成功。该表没有 created_at/occurred_at，当前通用索引时间为 NULL；按时间分析须查询原表并结合 card_status 说明字段限制，不能把它当准确成功送达时间。",
		"analysis_records.actor_user_id":                          "按 record_type 和动作提取有明确证据的操作者；空值为未知，不从当前负责人或参与者推断。跟进取 sales_user_id；分配/转派取 manager_user_id，claim 领取取 sales_user_id，未知分配动作留空。预约状态行留空，创建/改期等操作者查 lead_appointment_events.actor_user_id；inviter/closer/assigned_by 仍是原表业务参与字段。提醒和卡片的 receiver 不是操作者。线索/排班/渠道状态行只保留其明确创建者，不代表当前字段修改人。record_id 仅在 record_type 内唯一。",
	}
}

func snapshotMetricDefinitions() map[string]SnapshotMetricDefinition {
	return map[string]SnapshotMetricDefinition{
		"current_ownership_category": {
			Table: "leads", FilterSQL: "1=1",
			ValueSQL: "CASE WHEN status='assigned_pending_claim' OR ownership_state='assigned_pending_claim' THEN 'pending_claim' WHEN status='public_pool' OR ownership_state='public_pool' THEN 'public_pool' WHEN NULLIF(TRIM(current_owner_user_id),'') IS NOT NULL THEN 'owner:' || current_owner_user_id ELSE 'unowned' END",
			Meaning:  "当前归属互斥分类表达式；按表达式分组 COUNT(*)，每条线索只计一次。pending_claim=待领取，public_pool=公海，owner: 后为当前负责人业务 ID，unowned=明确无负责人。待接收者不算已领取负责人；前两类不能称为未匹配员工。仅非空当前负责人关联失败才标人员未匹配。终态按当前负责人计入，除非用户指定排除；矛盾状态另列异常，不能用分类优先级隐藏。",
		},
		"current_converted_leads": {
			Table: "leads", FilterSQL: "status = '" + string(types.LeadStatusConverted) + "'", ValueSQL: "COUNT(*)", DateColumn: "converted_at",
			Meaning: "默认排除回收站的当前成交线索数。成交率分母依用户人群条件明确；日期仅在用户要求成交期间时过滤。缺失成交日期单独计数并说明不能归入期间，不能当零或用录入时间补齐。历史曾经成交需另查事件，不能由当前状态倒推。",
		},
		"current_net_converted_amount": {
			Table: "leads", FilterSQL: "status = '" + string(types.LeadStatusConverted) + "'", ValueSQL: "SUM(net_deal_amount_cents)", DateColumn: "converted_at",
			Meaning: "当前成交集合的净成交金额，单位分。先保持每条线索一行再聚合；空值须另计缺失，不能声称实收现金。",
		},
	}
}

func snapshotUnavailableFacts() map[string]SnapshotUnavailableFact {
	return map[string]SnapshotUnavailableFact{
		"cash_receipts_by_period": {
			MissingEvidence: []string{"独立收款/退款流水", "实际到账金额", "实际到账时间"},
			InvalidProxies:  []string{"leads.payment_status", "leads.converted_at", "leads.created_at", "leads.updated_at", "lead_activity_records.created_at", "leads.net_deal_amount_cents"},
			Meaning:         "无法计算或判断某月实收。成交月份、资料更新时间、付款标记/其变更时间都不证明到账月份；既不能据此计入，也不能据此排除某月收款。未知不是零。可另列当前标记或成交口径，但必须明确其不回答现金流问题。",
		},
	}
}

func snapshotRelationships() []SnapshotRelationship {
	var relationships []SnapshotRelationship
	add := func(from, columns, to, key string) {
		relationships = append(relationships, SnapshotRelationship{FromTable: from, FromColumns: strings.Fields(columns), ToTable: to, ToColumn: key, Cardinality: "many_to_zero_or_one_per_column"})
	}
	// Verified against business models and GetByWeComUserID. These columns
	// contain business userid strings, including historical/system identifiers.
	for _, source := range []struct{ table, columns string }{
		{"leads", "dispatch_manager_user_id assigned_sales_user_id owner_user_id assigned_inviter_user_id inviter_user_id closer_user_id current_owner_user_id created_by_user_id"},
		{"lead_followups", "sales_user_id"}, {"lead_dispatch_records", "manager_user_id sales_user_id"},
		{"lead_appointments", "inviter_user_id closer_user_id assigned_by_user_id"}, {"lead_appointment_events", "actor_user_id"},
		{"closer_availability", "closer_user_id created_by"}, {"lead_pool_records", "previous_owner_user_id actor_user_id"},
		{"lead_activity_records", "actor_user_id"},
		{"channel_integrations", "created_by updated_by"}, {"channel_integration_audits", "actor_user_id"},
		{"wecom_card_tasks", "receiver_user_id"}, {"wecom_callback_logs", "from_user_id"},
		{"lead_deletion_events", "actor_userid"}, {"analysis_records", "actor_user_id"},
	} {
		add(source.table, source.columns, "crm_wecom_user_bindings", "wecom_userid")
	}
	// Follow-up escalation can store owner|manager, while appointment reminders
	// use one recipient per row. Never advertise the whole value as an atomic FK.
	relationships = append(relationships, SnapshotRelationship{
		FromTable: "lead_reminder_records", FromColumns: []string{"receiver_user_id"}, ToTable: "crm_wecom_user_bindings", ToColumn: "wecom_userid",
		Cardinality: "one_record_to_zero_or_many_employees", Separator: "|",
		Meaning: "拆分并 trim 每个非空接收者标识后分别等值关联；保留未匹配者。接收者不是 actor；提醒数量按原记录 id 去重，不能因多接收者倍增。",
	})
	for _, spec := range snapshotTables {
		if hasField(spec, "lead_id") {
			add(spec.name, "lead_id", "leads", "id")
		}
	}
	add("analysis_records", "lead_id", "leads", "id")
	add("entity_index", "lead_id", "leads", "id")
	add("entity_index", "actor_user_id", "crm_wecom_user_bindings", "wecom_userid")
	add("leads", "current_appointment_id", "lead_appointments", "id")
	add("lead_appointment_events", "appointment_id", "lead_appointments", "id")
	add("lead_reminder_records", "appointment_id", "lead_appointments", "id")
	add("channel_integration_audits", "integration_id", "channel_integrations", "id")
	add("channel_integrations", "last_lead_id", "leads", "id")
	add("group_robot_notifications", "reminder_id", "lead_reminder_records", "id")
	return relationships
}
