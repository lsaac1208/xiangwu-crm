package types

import "time"

// LeadStatus 线索状态
type LeadStatus string

const (
	// LeadStatusPendingDispatch 是线索的初始状态（创建时直接进入此状态）。
	// LeadStatusNew 已移除：历史上规划过 new → pending_dispatch 的两步流转，
	// 但实际创建逻辑从未使用该中间状态，属于死代码，已清理。
	LeadStatusPendingDispatch      LeadStatus = "pending_dispatch"       // 待主管分配
	LeadStatusAssignedPendingClaim LeadStatus = "assigned_pending_claim" // 已分配待领取
	LeadStatusClaimed              LeadStatus = "claimed"                // 已领取
	LeadStatusFollowing            LeadStatus = "following"              // 跟进中
	LeadStatusPublicPool           LeadStatus = "public_pool"            // 邀约公海
	LeadStatusInvalid              LeadStatus = "invalid"                // 无效线索
	LeadStatusConverted            LeadStatus = "converted"              // 已转化
	LeadStatusClosed               LeadStatus = "closed"                 // 已关闭
)

// DispatcherLeadScope controls which part of a dispatcher's read-only history
// is returned by the lead list endpoint.
type DispatcherLeadScope string

const (
	DispatcherLeadScopePending DispatcherLeadScope = "pending"
	DispatcherLeadScopeHandled DispatcherLeadScope = "handled"
	DispatcherLeadScopeAll     DispatcherLeadScope = "all"
)

func IsValidDispatcherLeadScope(scope DispatcherLeadScope) bool {
	return scope == DispatcherLeadScopePending || scope == DispatcherLeadScopeHandled || scope == DispatcherLeadScopeAll
}

// IsTerminalLeadStatus reports whether a lead status is a workflow terminal.
func IsTerminalLeadStatus(status LeadStatus) bool {
	return status == LeadStatusInvalid ||
		status == LeadStatusConverted ||
		status == LeadStatusClosed
}

// Lead 线索实体
type Lead struct {
	ID                   int64            `json:"id"`
	LeadNo               string           `json:"lead_no"`
	CustomerName         string           `json:"customer_name"`
	CustomerMobile       string           `json:"customer_mobile"`        // 仓储层加密存储
	CustomerMobileMasked string           `json:"customer_mobile_masked"` // 脱敏展示
	CustomerMobileHash   string           `json:"-"`                      // 仓储层 HMAC 索引
	CustomerWechat       string           `json:"customer_wechat"`        // 仓储层加密存储
	CustomerWechatMasked string           `json:"customer_wechat_masked"` // 脱敏展示
	City                 string           `json:"city"`
	ProductName          string           `json:"product_name"`
	SourceChannel        string           `json:"source_channel"`
	SourceCampaign       string           `json:"source_campaign"`
	ContentEntryID       *int64           `json:"content_entry_id,omitempty"`
	ContentRevisionID    *int64           `json:"content_revision_id,omitempty"`
	ContentPath          string           `json:"content_path,omitempty"`
	LeadLevel            string           `json:"lead_level"`
	BusinessStage        BusinessStage    `json:"business_stage,omitempty"`
	OwnershipState       OwnershipState   `json:"ownership_state,omitempty"`
	ResultStatus         LeadResultStatus `json:"result_status,omitempty"`
	StarRating           int              `json:"star_rating,omitempty"`

	Status         LeadStatus `json:"status"`
	Version        int        `json:"version"`         // 业务流程乐观锁
	ProfileVersion int        `json:"profile_version"` // 客户资料乐观锁

	DispatchManagerUserID string    `json:"dispatch_manager_userid,omitempty"`
	AssignedSalesUserID   string    `json:"assigned_sales_userid,omitempty"`
	OwnerUserID           string    `json:"owner_userid,omitempty"`
	AssignedInviterUserID string    `json:"assigned_inviter_userid,omitempty"`
	InviterUserID         string    `json:"inviter_userid,omitempty"`
	CloserUserID          string    `json:"closer_userid,omitempty"`
	CurrentOwnerUserID    string    `json:"current_owner_userid,omitempty"`
	CurrentOwnerRole      OwnerRole `json:"current_owner_role,omitempty"`
	CurrentAppointmentID  *int64    `json:"current_appointment_id,omitempty"`
	CreatedByUserID       string    `json:"created_by_userid,omitempty"`
	CreatedByRole         UserRole  `json:"created_by_role,omitempty"`

	DispatchManagerName string `json:"dispatch_manager_name,omitempty"`
	AssignedSalesName   string `json:"assigned_sales_name,omitempty"`
	OwnerName           string `json:"owner_name,omitempty"`
	AssignedInviterName string `json:"assigned_inviter_name,omitempty"`
	InviterName         string `json:"inviter_name,omitempty"`
	CloserName          string `json:"closer_name,omitempty"`
	CurrentOwnerName    string `json:"current_owner_name,omitempty"`
	CreatedByName       string `json:"created_by_name,omitempty"`

	LastDispatchManagerUserID string     `json:"last_dispatch_manager_userid,omitempty"`
	LastDispatchManagerName   string     `json:"last_dispatch_manager_name,omitempty"`
	LastDispatchActionType    string     `json:"last_dispatch_action_type,omitempty"`
	LastDispatchedAt          *time.Time `json:"last_dispatched_at,omitempty"`

	CreatedAt                   time.Time  `json:"created_at"`
	AssignedAt                  *time.Time `json:"assigned_at,omitempty"`
	ClaimedAt                   *time.Time `json:"claimed_at,omitempty"`
	FirstFollowupAt             *time.Time `json:"first_followup_at,omitempty"`
	CurrentOwnerStartedAt       *time.Time `json:"current_owner_started_at,omitempty"`
	CurrentOwnerFirstFollowupAt *time.Time `json:"current_owner_first_followup_at,omitempty"`
	LastFollowupAt              *time.Time `json:"last_followup_at,omitempty"`
	NextFollowupDueAt           *time.Time `json:"next_followup_due_at,omitempty"`
	InviterAssignedAt           *time.Time `json:"inviter_assigned_at,omitempty"`
	InviterClaimedAt            *time.Time `json:"inviter_claimed_at,omitempty"`
	ArrivedAt                   *time.Time `json:"arrived_at,omitempty"`
	DeliveredAt                 *time.Time `json:"delivered_at,omitempty"`
	PoolEnteredAt               *time.Time `json:"pool_entered_at,omitempty"`
	PoolReason                  string     `json:"pool_reason,omitempty"`
	ConvertedAt                 *time.Time `json:"converted_at,omitempty"`
	ClosedAt                    *time.Time `json:"closed_at,omitempty"`
	DealAmount                  float64    `json:"deal_amount,omitempty"`
	PaymentStatus               string     `json:"payment_status"`
	RefundAmount                float64    `json:"refund_amount"`
	CloseReason                 string     `json:"close_reason,omitempty"`
	UpdatedAt                   time.Time  `json:"updated_at"`
}

// LeadEvent 线索事件（状态转换触发器）
type LeadEvent string

const (
	// LeadEventDispatchRequested 已移除：配套 LeadStatusNew 的死代码，已清理。
	LeadEventAssignedToSales   LeadEvent = "assigned_to_sales"  // 已分配给销售
	LeadEventClaimedBySales    LeadEvent = "claimed_by_sales"   // 销售已领取
	LeadEventFollowupSubmitted LeadEvent = "followup_submitted" // 提交跟进
	LeadEventMarkedInvalid     LeadEvent = "marked_invalid"     // 标记无效
	LeadEventConverted         LeadEvent = "converted"          // 转化成交
	LeadEventReassigned        LeadEvent = "reassigned"         // 重新分配
	LeadEventClosed            LeadEvent = "closed"             // 关闭
)

// LeadDispatchRecord 分配记录
type LeadDispatchRecord struct {
	ID            int64     `json:"id"`
	LeadID        int64     `json:"lead_id"`
	ManagerUserID string    `json:"manager_userid"`
	SalesUserID   string    `json:"sales_userid"`
	ManagerName   string    `json:"manager_name,omitempty" gorm:"-"`
	SalesName     string    `json:"sales_name,omitempty" gorm:"-"`
	Remark        string    `json:"remark"`
	ActionType    string    `json:"action_type"` // assign, claim, reassign, cancel_assign
	CreatedAt     time.Time `json:"created_at"`
}

// LeadFollowup 跟进记录
type LeadFollowup struct {
	ID             int64         `json:"id"`
	LeadID         int64         `json:"lead_id"`
	SalesUserID    string        `json:"sales_userid"`
	SalesName      string        `json:"sales_name,omitempty" gorm:"-"`
	FollowupResult string        `json:"followup_result"`
	IntentionLevel string        `json:"intention_level,omitempty"`
	BusinessStage  BusinessStage `json:"business_stage,omitempty"`
	ActorRole      OwnerRole     `json:"actor_role,omitempty"`
	StarRating     int           `json:"star_rating,omitempty"`
	DueAtSnapshot  *time.Time    `json:"due_at_snapshot,omitempty"`
	Remark         string        `json:"remark"`
	NextFollowupAt *time.Time    `json:"next_followup_at,omitempty"`
	IsValid        bool          `json:"is_valid"`
	InvalidReason  string        `json:"invalid_reason,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
}

func IsValidStarRating(rating int) bool {
	return rating >= 1 && rating <= 5
}

func LegacyIntentionStarRating(level string) int {
	switch level {
	case "high":
		return 5
	case "medium":
		return 3
	case "low":
		return 1
	default:
		return 0
	}
}
