package types

import "time"

const MaxCardRetryCount = 3

// CardScene 卡片场景
type CardScene string

const (
	CardSceneSupervisorPendingDispatch    CardScene = "supervisor_pending_dispatch"    // 主管待分配
	CardSceneSupervisorWaitingClaim       CardScene = "supervisor_waiting_claim"       // 主管等待领取
	CardSceneSupervisorClaimed            CardScene = "supervisor_claimed"             // 主管已领取
	CardSceneSalesPendingClaim            CardScene = "sales_pending_claim"            // 销售待领取
	CardSceneSalesClaimed                 CardScene = "sales_claimed"                  // 销售已领取
	CardSceneAppointmentPendingAssignment CardScene = "appointment_pending_assignment" // 预约待分配谈单师
	CardSceneAppointmentAssigned          CardScene = "appointment_assigned"           // 预约已分配谈单师
	CardSceneAppointmentRescheduled       CardScene = "appointment_rescheduled"        // 预约已改期
	CardSceneAppointmentCancelled         CardScene = "appointment_cancelled"          // 预约已取消
	CardSceneAppointmentNoShow            CardScene = "appointment_no_show"            // 预约爽约
	CardSceneAppointmentArrivedDelivered  CardScene = "appointment_arrived_delivered"  // 已到访交付
)

// CardStatus 卡片状态
type CardStatus string

const (
	CardStatusSent         CardStatus = "sent"          // 已发送
	CardStatusUpdating     CardStatus = "updating"      // 更新中
	CardStatusUpdated      CardStatus = "updated"       // 已更新
	CardStatusUpdateFailed CardStatus = "update_failed" // 更新失败
	CardStatusExpired      CardStatus = "expired"       // response_code 已过期
	CardStatusReplaced     CardStatus = "replaced"      // 已被新卡片替代
)

// WeComCardTask 企业微信卡片任务
type WeComCardTask struct {
	ID             int64     `json:"id"`
	LeadID         int64     `json:"lead_id"`
	CardScene      CardScene `json:"card_scene"`
	ReceiverUserID string    `json:"receiver_userid"`

	TaskID       string `json:"task_id"`
	MsgID        string `json:"msgid,omitempty"`
	ResponseCode string `json:"response_code,omitempty"`

	CardStatus  CardStatus `json:"card_status"`
	RetryCount  int        `json:"retry_count"`
	LastError   string     `json:"last_error,omitempty"`
	NextRetryAt *time.Time `json:"next_retry_at,omitempty"`

	SentAt               time.Time  `json:"sent_at"`
	UpdatedAt            *time.Time `json:"updated_at,omitempty"`
	ResponseCodeExpireAt *time.Time `json:"response_code_expire_at,omitempty"`
}

// WeComCallbackLog 企业微信回调日志
type WeComCallbackLog struct {
	ID            int64     `json:"id"`
	EventID       string    `json:"event_id,omitempty"`
	TaskID        string    `json:"task_id,omitempty"`
	LeadID        *int64    `json:"lead_id,omitempty"`
	FromUserID    string    `json:"from_userid,omitempty"`
	EventKey      string    `json:"event_key,omitempty"`
	RawPayload    string    `json:"raw_payload"`
	ProcessStatus string    `json:"process_status"` // success, failed, duplicate
	ErrorMessage  string    `json:"error_message,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// WeComCardContent 卡片内容（用于构造企业微信卡片 JSON）
type WeComCardContent struct {
	MainTitle    string            `json:"main_title"`
	MainDesc     string            `json:"main_desc,omitempty"`
	ContentItems []CardContentItem `json:"content_items"`
	Buttons      []CardButton      `json:"buttons"`
	ActionURL    string            `json:"action_url,omitempty"`
}

// CardContentItem 卡片内容项
type CardContentItem struct {
	KeyName string `json:"key_name"`
	Value   string `json:"value"`
}

// CardButton 卡片按钮
type CardButton struct {
	Text  string `json:"text"`
	Key   string `json:"key"`
	Style int    `json:"style"` // 1: 强调, 2: 次要
	Type  int    `json:"type,omitempty"`
	URL   string `json:"url,omitempty"`
}
