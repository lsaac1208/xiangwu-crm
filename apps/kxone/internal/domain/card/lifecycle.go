package card

import (
	"fmt"
	"strings"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

// Lifecycle 卡片生命周期管理（纯函数）
type Lifecycle struct {
	retryIntervals []time.Duration
	baseURL        string
}

// NewLifecycle 创建生命周期管理实例
func NewLifecycle() *Lifecycle {
	return NewLifecycleWithRetryIntervals(nil)
}

func NewLifecycleWithRetryIntervals(intervals []time.Duration) *Lifecycle {
	return &Lifecycle{
		retryIntervals: normalizeRetryIntervals(intervals),
	}
}

func (lc *Lifecycle) WithBaseURL(baseURL string) *Lifecycle {
	if normalized := normalizeBaseURL(baseURL); normalized != "" {
		lc.baseURL = normalized
	}
	return lc
}

// ShouldUpdateCard 判断是否需要更新卡片
func (lc *Lifecycle) ShouldUpdateCard(cardTask *types.WeComCardTask, currentTime time.Time) bool {
	// 如果卡片还未发送，不需要更新
	if cardTask.CardStatus == "" {
		return false
	}

	// 如果 response_code 已过期，需要重新发送而不是更新
	if lc.IsResponseCodeExpired(cardTask, currentTime) {
		return false
	}

	// 如果卡片状态允许更新，则可以更新
	return cardTask.CardStatus == types.CardStatusSent ||
		cardTask.CardStatus == types.CardStatusUpdated
}

// ShouldResendCard 判断是否需要重新发送卡片
func (lc *Lifecycle) ShouldResendCard(cardTask *types.WeComCardTask, currentTime time.Time) bool {
	// response_code 已过期
	if lc.IsResponseCodeExpired(cardTask, currentTime) {
		return true
	}

	// 更新失败且重试次数已达上限
	if cardTask.CardStatus == types.CardStatusUpdateFailed && cardTask.RetryCount >= types.MaxCardRetryCount {
		return true
	}

	return false
}

// IsResponseCodeExpired 判断 response_code 是否过期
func (lc *Lifecycle) IsResponseCodeExpired(cardTask *types.WeComCardTask, currentTime time.Time) bool {
	if cardTask.ResponseCodeExpireAt == nil {
		return true
	}

	return currentTime.After(*cardTask.ResponseCodeExpireAt)
}

// CalculateResponseCodeExpireTime 计算 response_code 过期时间
// 企业微信 response_code 有效期约 1 小时，这里设置为 50 分钟保险
func (lc *Lifecycle) CalculateResponseCodeExpireTime(sentAt time.Time) time.Time {
	return sentAt.Add(50 * time.Minute)
}

// CalculateNextRetryTime 计算下次重试时间（指数退避）
func (lc *Lifecycle) CalculateNextRetryTime(currentTime time.Time, retryCount int) time.Time {
	intervals := lc.retryIntervals
	if len(intervals) == 0 {
		intervals = defaultRetryIntervals()
	}

	if retryCount >= len(intervals) {
		retryCount = len(intervals) - 1
	}

	return currentTime.Add(intervals[retryCount])
}

func normalizeRetryIntervals(intervals []time.Duration) []time.Duration {
	var result []time.Duration
	for _, interval := range intervals {
		if interval <= 0 {
			continue
		}
		result = append(result, interval)
	}
	if len(result) == 0 {
		return defaultRetryIntervals()
	}
	return result
}

func defaultRetryIntervals() []time.Duration {
	return []time.Duration{
		10 * time.Second,
		30 * time.Second,
		60 * time.Second,
	}
}

// ShouldRetry 判断是否应该重试
func (lc *Lifecycle) ShouldRetry(cardTask *types.WeComCardTask, currentTime time.Time) bool {
	// 状态不是失败，不需要重试
	if cardTask.CardStatus != types.CardStatusUpdateFailed {
		return false
	}

	// 重试次数超限
	if cardTask.RetryCount >= types.MaxCardRetryCount {
		return false
	}

	// 还没到重试时间
	if cardTask.NextRetryAt != nil && currentTime.Before(*cardTask.NextRetryAt) {
		return false
	}

	return true
}

// BuildCardContent 构造卡片内容（根据场景和线索信息）
func (lc *Lifecycle) BuildCardContent(scene types.CardScene, lead *types.Lead) *types.WeComCardContent {
	switch scene {
	case types.CardSceneSupervisorPendingDispatch:
		return lc.buildSupervisorPendingDispatchCard(lead)
	case types.CardSceneSupervisorWaitingClaim:
		return lc.buildSupervisorWaitingClaimCard(lead)
	case types.CardSceneSupervisorClaimed:
		return lc.buildSupervisorClaimedCard(lead)
	case types.CardSceneSalesPendingClaim:
		return lc.buildSalesPendingClaimCard(lead)
	case types.CardSceneSalesClaimed:
		return lc.buildSalesClaimedCard(lead)
	case types.CardSceneAppointmentPendingAssignment:
		return lc.buildAppointmentCard(lead, "预约待分配谈单师", "请主管尽快安排谈单师", "查看预约")
	case types.CardSceneAppointmentAssigned:
		return lc.buildAppointmentCard(lead, "预约已分配谈单师", "请邀约师与谈单师完成线下交接", "查看预约")
	case types.CardSceneAppointmentRescheduled:
		return lc.buildAppointmentCard(lead, "预约时间已调整", "请及时确认新的到校安排", "查看预约")
	case types.CardSceneAppointmentCancelled:
		return lc.buildAppointmentCard(lead, "预约已取消", "请查看预约记录和取消原因", "查看预约")
	case types.CardSceneAppointmentNoShow:
		return lc.buildAppointmentCard(lead, "客户未按预约到访", "邀约师需继续回访，主管可查看异常", "查看线索")
	case types.CardSceneAppointmentArrivedDelivered:
		return lc.buildAppointmentCard(lead, "客户已到访并完成交付", "线索已进入谈单阶段，由谈单师持续跟进", "查看线索")
	default:
		return nil
	}
}

func (lc *Lifecycle) buildAppointmentCard(lead *types.Lead, title, description, button string) *types.WeComCardContent {
	return &types.WeComCardContent{
		MainTitle: title,
		MainDesc:  description,
		ContentItems: []types.CardContentItem{
			{KeyName: "客户", Value: lead.CustomerName},
			{KeyName: "校区", Value: lead.City},
			{KeyName: "业务阶段", Value: string(lead.BusinessStage)},
		},
		Buttons:   []types.CardButton{{Text: button, Style: 1, Type: 1, URL: lc.appointmentsURL()}},
		ActionURL: lc.appointmentsURL(),
	}
}

// buildSupervisorPendingDispatchCard 构造主管待分配卡片
func (lc *Lifecycle) buildSupervisorPendingDispatchCard(lead *types.Lead) *types.WeComCardContent {
	return &types.WeComCardContent{
		MainTitle: "新付费线索待分配",
		MainDesc:  "请尽快分配销售跟进",
		ContentItems: []types.CardContentItem{
			{KeyName: "来源", Value: lead.SourceChannel},
			{KeyName: "城市", Value: lead.City},
			{KeyName: "产品", Value: lead.ProductName},
			{KeyName: "客户", Value: lead.CustomerName},
			{KeyName: "手机", Value: lead.CustomerMobileMasked},
			{KeyName: "微信", Value: lead.CustomerWechatMasked},
		},
		Buttons: []types.CardButton{
			{Text: "分配线索", Style: 1, Type: 1, URL: lc.leadDetailURL(lead.ID)},
		},
		ActionURL: lc.leadDetailURL(lead.ID),
	}
}

// buildSupervisorWaitingClaimCard 构造主管等待领取卡片
func (lc *Lifecycle) buildSupervisorWaitingClaimCard(lead *types.Lead) *types.WeComCardContent {
	// 优先显示真实姓名，回退 UserID（Worker 中 lead 对象可能未填充名字字段）
	salesDisplay := nameOrID(lead.AssignedSalesName, lead.AssignedSalesUserID)
	return &types.WeComCardContent{
		MainTitle: "线索已分配，等待销售领取",
		MainDesc:  "分配给：" + salesDisplay,
		ContentItems: []types.CardContentItem{
			{KeyName: "客户", Value: lead.CustomerName},
			{KeyName: "城市", Value: lead.City},
			{KeyName: "产品", Value: lead.ProductName},
		},
		Buttons: []types.CardButton{
			{Text: "查看详情", Style: 2, Type: 1, URL: lc.leadDetailURL(lead.ID)},
		},
		ActionURL: lc.leadDetailURL(lead.ID),
	}
}

// buildSupervisorClaimedCard 构造主管已领取卡片
func (lc *Lifecycle) buildSupervisorClaimedCard(lead *types.Lead) *types.WeComCardContent {
	ownerDisplay := nameOrID(lead.OwnerName, lead.OwnerUserID)
	return &types.WeComCardContent{
		MainTitle: "线索已领取",
		MainDesc:  "销售：" + ownerDisplay,
		ContentItems: []types.CardContentItem{
			{KeyName: "客户", Value: lead.CustomerName},
			{KeyName: "当前状态", Value: string(lead.Status)},
		},
		Buttons: []types.CardButton{
			{Text: "查看线索", Style: 2, Type: 1, URL: lc.leadDetailURL(lead.ID)},
		},
		ActionURL: lc.leadDetailURL(lead.ID),
	}
}

// buildSalesPendingClaimCard 构造销售待领取卡片
func (lc *Lifecycle) buildSalesPendingClaimCard(lead *types.Lead) *types.WeComCardContent {
	return &types.WeComCardContent{
		MainTitle: "你有一条新分配线索",
		MainDesc:  "请在 5 分钟内领取",
		ContentItems: []types.CardContentItem{
			{KeyName: "来源", Value: lead.SourceChannel},
			{KeyName: "城市", Value: lead.City},
			{KeyName: "产品", Value: lead.ProductName},
			{KeyName: "客户", Value: lead.CustomerName},
			{KeyName: "手机", Value: lead.CustomerMobileMasked},
			{KeyName: "微信", Value: lead.CustomerWechatMasked},
		},
		Buttons: []types.CardButton{
			{Text: "领取", Key: "CLAIM_LEAD", Style: 1},
		},
		ActionURL: lc.leadDetailURL(lead.ID),
	}
}

// buildSalesClaimedCard 构造销售已领取卡片
func (lc *Lifecycle) buildSalesClaimedCard(lead *types.Lead) *types.WeComCardContent {
	return &types.WeComCardContent{
		MainTitle: "你已领取该线索",
		MainDesc:  "请尽快联系客户",
		ContentItems: []types.CardContentItem{
			{KeyName: "客户", Value: lead.CustomerName},
			{KeyName: "手机", Value: lead.CustomerMobileMasked},
			{KeyName: "微信", Value: lead.CustomerWechatMasked},
			{KeyName: "当前状态", Value: "待首次联系"},
		},
		Buttons: []types.CardButton{
			{Text: "填写跟进", Style: 1, Type: 1, URL: lc.followupURL(lead.ID)},
		},
		ActionURL: lc.followupURL(lead.ID),
	}
}

func (lc *Lifecycle) leadDetailURL(leadID int64) string {
	return fmt.Sprintf("%s/leads/%d", lc.baseURL, leadID)
}

func (lc *Lifecycle) followupURL(leadID int64) string {
	return fmt.Sprintf("%s/leads/%d/followup", lc.baseURL, leadID)
}

func (lc *Lifecycle) appointmentsURL() string {
	return fmt.Sprintf("%s/appointments", lc.baseURL)
}

// nameOrID 优先返回真实姓名，姓名为空时降级返回 ID（如 wecom_userid）。
func nameOrID(name, id string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	return id
}

func normalizeBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasPrefix(baseURL, "http://") || strings.HasPrefix(baseURL, "https://") {
		return baseURL
	}
	return ""
}
