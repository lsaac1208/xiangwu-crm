package lead

import (
	"github.com/lsaac128/kxone/pkg/types"
)

// StateMachine 线索状态机（纯函数）
type StateMachine struct{}

// NewStateMachine 创建状态机实例
func NewStateMachine() *StateMachine {
	return &StateMachine{}
}

// CanTransition 判断是否允许状态转换
func (sm *StateMachine) CanTransition(current types.LeadStatus, event types.LeadEvent) bool {
	if types.IsTerminalLeadStatus(current) {
		return false
	}

	transitions := sm.allowedTransitions()

	if allowed, exists := transitions[current]; exists {
		for _, allowedEvent := range allowed {
			if allowedEvent == event {
				return true
			}
		}
	}

	return false
}

// NextState 根据当前状态和事件计算下一个状态
func (sm *StateMachine) NextState(current types.LeadStatus, event types.LeadEvent) (types.LeadStatus, error) {
	if !sm.CanTransition(current, event) {
		return current, types.ErrLeadStatusInvalid
	}

	stateMap := map[types.LeadEvent]types.LeadStatus{
		types.LeadEventAssignedToSales:   types.LeadStatusAssignedPendingClaim,
		types.LeadEventClaimedBySales:    types.LeadStatusClaimed,
		types.LeadEventFollowupSubmitted: types.LeadStatusFollowing,
		types.LeadEventMarkedInvalid:     types.LeadStatusInvalid,
		types.LeadEventConverted:         types.LeadStatusConverted,
		types.LeadEventReassigned:        types.LeadStatusAssignedPendingClaim,
		types.LeadEventClosed:            types.LeadStatusClosed,
	}

	if nextState, exists := stateMap[event]; exists {
		return nextState, nil
	}

	return current, types.ErrLeadStatusInvalid
}

// allowedTransitions 定义允许的状态转换规则。
// 线索以 PendingDispatch 创建，不存在 New 状态，此处不包含 New 的转换规则。
func (sm *StateMachine) allowedTransitions() map[types.LeadStatus][]types.LeadEvent {
	return map[types.LeadStatus][]types.LeadEvent{
		types.LeadStatusPendingDispatch: {
			types.LeadEventAssignedToSales,
			types.LeadEventClosed,
		},
		types.LeadStatusAssignedPendingClaim: {
			types.LeadEventClaimedBySales,
			types.LeadEventReassigned,
			types.LeadEventClosed,
		},
		types.LeadStatusClaimed: {
			types.LeadEventFollowupSubmitted,
			types.LeadEventReassigned,
			types.LeadEventMarkedInvalid,
			types.LeadEventConverted, // 销售可在首跟前直接签单转化
			types.LeadEventClosed,
		},
		types.LeadStatusFollowing: {
			types.LeadEventConverted,
			types.LeadEventMarkedInvalid,
			types.LeadEventClosed,
		},
	}
}

// ValidateAssignment 校验分配操作的前置条件
func (sm *StateMachine) ValidateAssignment(lead *types.Lead, managerUserID, salesUserID string) error {
	if types.IsTerminalLeadStatus(lead.Status) {
		return types.ErrLeadStatusInvalid
	}

	// 状态必须允许分配
	if lead.Status != types.LeadStatusPendingDispatch {
		return types.ErrLeadStatusInvalid
	}

	// 销售不能为空
	if salesUserID == "" {
		return types.ErrUnauthorized
	}

	return nil
}

// ValidateClaim 校验领取操作的前置条件
func (sm *StateMachine) ValidateClaim(lead *types.Lead, salesUserID string) error {
	if types.IsTerminalLeadStatus(lead.Status) {
		return types.ErrLeadStatusInvalid
	}

	// 状态必须是待领取
	if lead.Status != types.LeadStatusAssignedPendingClaim {
		if lead.Status == types.LeadStatusClaimed {
			return types.ErrLeadAlreadyClaimed
		}
		return types.ErrLeadStatusInvalid
	}

	// 必须是被分配的销售
	if lead.AssignedSalesUserID != salesUserID {
		return types.ErrSalesNotAssigned
	}

	return nil
}

// ValidateReassignment 校验转派操作的前置条件
func (sm *StateMachine) ValidateReassignment(lead *types.Lead, managerUserID, newSalesUserID string) error {
	if types.IsTerminalLeadStatus(lead.Status) {
		return types.ErrLeadStatusInvalid
	}

	// 待领取、已领取及跟进中的线索均可转派。
	if lead.Status != types.LeadStatusAssignedPendingClaim && lead.Status != types.LeadStatusClaimed && lead.Status != types.LeadStatusFollowing {
		return types.ErrLeadStatusInvalid
	}

	// 主管必须匹配
	if lead.DispatchManagerUserID != managerUserID {
		return types.ErrUnauthorized
	}

	// 新销售不能为空且不能与当前销售相同
	if newSalesUserID == "" || newSalesUserID == lead.AssignedSalesUserID {
		return types.ErrUnauthorized
	}

	return nil
}
