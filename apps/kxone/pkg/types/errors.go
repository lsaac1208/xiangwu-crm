package types

import "errors"

var (
	ErrInvalidIdempotencyKey = errors.New("请提供有效的录入请求标识")
	ErrIdempotencyConflict   = errors.New("该录入请求已提交不同内容，请确认已有结果后重新录入")
	// 业务错误
	ErrLeadNotFound               = errors.New("线索不存在")
	ErrLeadStatusInvalid          = errors.New("线索状态不允许此操作")
	ErrLeadAlreadyClaimed         = errors.New("线索已被领取")
	ErrLeadReassigned             = errors.New("线索已被转派")
	ErrLeadVersionConflict        = errors.New("线索版本冲突，请刷新后重试")
	ErrUnauthorized               = errors.New("无权限执行此操作")
	ErrSalesNotAssigned           = errors.New("该线索未分配给你")
	ErrUserBindingNotFound        = errors.New("用户绑定不存在")
	ErrInvalidInviter             = errors.New("目标用户不是启用的邀约师")
	ErrInvalidStarRating          = errors.New("有效跟进必须选择 1-5 星")
	ErrInvalidFollowupDue         = errors.New("下次回访时间必须晚于当前时间")
	ErrLeadNotInPublicPool        = errors.New("线索不在邀约公海")
	ErrPublicPoolForbidden        = errors.New("该线索不允许进入邀约公海")
	ErrPublicPoolCapacity         = errors.New("已达到私有线索持有上限")
	ErrPublicPoolCooldown         = errors.New("原负责人仍在公海领取冷却期")
	ErrAppointmentForbidden       = errors.New("预约功能未开启或当前线索不允许预约")
	ErrAppointmentNotFound        = errors.New("预约单不存在")
	ErrAppointmentAlreadyActive   = errors.New("该线索已有有效预约")
	ErrAppointmentTimeInvalid     = errors.New("预约开始和结束时间无效")
	ErrAppointmentDetailsRequired = errors.New("校区和交接说明为必填")
	ErrAppointmentStatusInvalid   = errors.New("预约单状态不允许此操作")
	ErrAppointmentVersionConflict = errors.New("预约单已被其他操作更新，请刷新后重试")
	ErrInvalidCloser              = errors.New("目标用户不是启用的谈单师")
	ErrAppointmentHardConflict    = errors.New("谈单师日程存在硬冲突")
	ErrAppointmentSoftConflict    = errors.New("谈单师日程存在需确认的软冲突")
	ErrConflictOverrideRequired   = errors.New("强制安排软冲突时必须填写原因")
	ErrAvailabilityInvalid        = errors.New("谈单师可用时间设置无效")
	ErrDeliveryForbidden          = errors.New("当前预约不允许确认到访交付")
	ErrDeliveryAlreadyCompleted   = errors.New("预约已完成到访交付")
	ErrNegotiationReassignInvalid = errors.New("谈单阶段转交无效")
	ErrInvalidDispatcherScope     = errors.New("线索调度员查询范围无效")
	ErrInvalidLeadProfile         = errors.New("客户资料填写不完整或格式无效")
	ErrInvalidManualLeadSource    = errors.New("请选择有效的来源渠道")
	ErrInvalidLeadSort            = errors.New("线索排序字段或方向无效")
	ErrInvalidLeadPreference      = errors.New("线索列表偏好或保存视图无效")
	ErrLeadPreferenceConflict     = errors.New("偏好已在其他设备更新，请刷新后重试")
	ErrLeadViewNotFound           = errors.New("保存视图不存在")
	ErrLeadViewLimit              = errors.New("每个账号最多保存 20 个视图")
	ErrLeadViewNameConflict       = errors.New("保存视图名称已存在")
	ErrInvalidBatchLeadRequest    = errors.New("批量线索请求无效")

	// 企业微信错误
	ErrWeComTokenExpired        = errors.New("企业微信 access_token 已过期")
	ErrWeComResponseCodeExpired = errors.New("企业微信 response_code 已过期")
	ErrWeComSendFailed          = errors.New("企业微信消息发送失败")
	ErrWeComUpdateFailed        = errors.New("企业微信卡片更新失败")

	// 参数错误
	ErrInvalidReasonRequired = errors.New("标记无效时 InvalidReason 为必填")

	// 幂等错误
	ErrDuplicateCallback = errors.New("重复回调")
	ErrDuplicateClaim    = errors.New("重复领取")
)
