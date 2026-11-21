package worker

// UpdateCardPayload 更新卡片任务载荷
type UpdateCardPayload struct {
	LeadID    int64  `json:"lead_id"`
	CardScene string `json:"card_scene"` // 目标场景
	Attempt   int    `json:"attempt"`
}

// SendCardPayload 发送卡片任务载荷
type SendCardPayload struct {
	LeadID         int64  `json:"lead_id"`
	ReceiverUserID string `json:"receiver_userid"`
	CardScene      string `json:"card_scene"`
	EventKey       string `json:"event_key,omitempty"`
}

// RemindDispatchPayload 提醒主管分配载荷
type RemindDispatchPayload struct {
	LeadID         int64  `json:"lead_id"`
	ManagerUserID  string `json:"manager_userid"`
	CreatedMinutes int    `json:"created_minutes"` // 已创建多少分钟
}

// RemindClaimPayload 提醒销售领取载荷
type RemindClaimPayload struct {
	LeadID          int64  `json:"lead_id"`
	SalesUserID     string `json:"sales_userid"`
	AssignedMinutes int    `json:"assigned_minutes"` // 已分配多少分钟
}

// TimeoutDispatchReminderPayload 超时未分配提醒
type TimeoutDispatchReminderPayload struct {
	LeadID         int64 `json:"lead_id"`
	CreatedMinutes int   `json:"created_minutes"`
}

// TimeoutClaimReminderPayload 超时未领取提醒
type TimeoutClaimReminderPayload struct {
	LeadID          int64 `json:"lead_id"`
	AssignedMinutes int   `json:"assigned_minutes"`
}

// CardRetryPayload 卡片重试载荷
type CardRetryPayload struct {
	CardTaskID int64 `json:"card_task_id"`
}

// 任务类型常量
const (
	TypeUpdateCard              = "card:update"
	TypeSendCard                = "card:send"
	TypeTimeoutDispatchReminder = "lead:timeout_dispatch_reminder"
	TypeTimeoutClaimReminder    = "lead:timeout_claim_reminder"
	TypeCardRetry               = "card:retry"
	TypeScanFailedCards         = "card:scan_failed"
	TypeScanFollowupSLA         = "lead:scan_followup_sla"
	TypeScanGroupRobotRules     = "notification:scan_group_robot_rules"
)
