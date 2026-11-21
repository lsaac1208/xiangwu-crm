package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/cache"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
	"github.com/lsaac128/kxone/internal/domain/card"
	"github.com/lsaac128/kxone/internal/domain/lead"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/pkg/metrics"
	"github.com/lsaac128/kxone/pkg/types"
)

// TaskQueue 任务队列接口（用于依赖注入）
type TaskQueue interface {
	EnqueueUpdateCard(ctx context.Context, leadID int64, cardScene string) error
	EnqueueSendCard(ctx context.Context, leadID int64, receiverUserID, cardScene string) error
	SchedulePendingDispatchReminder(ctx context.Context, leadID int64, delay time.Duration) error
	SchedulePendingClaimReminder(ctx context.Context, leadID int64, delay time.Duration) error
}

type EventTaskQueue interface {
	EnqueueSendCardEvent(ctx context.Context, leadID int64, receiverUserID, cardScene, eventKey string) error
}

// AssignLeadUseCase 主管分配线索用例
type AssignLeadUseCase struct {
	leadRepo         repo.LeadRepository
	dispatchRepo     repo.DispatchRecordRepository
	cardRepo         repo.CardTaskRepository
	wecomClient      wecom.Client
	cache            cache.Cache
	txManager        repo.TransactionManager
	taskQueue        TaskQueue
	metrics          *metrics.Metrics
	stateMachine     *lead.StateMachine
	cardLifecycle    *card.Lifecycle
	runtimeSettings  runtime.RuntimeSettings
	followupSettings runtime.FollowupPolicySettings
	userBindingRepo  repo.UserBindingRepository
	notificationRepo repo.GroupRobotNotificationRepository
}

func (uc *AssignLeadUseCase) WithUserBindingRepository(repository repo.UserBindingRepository) *AssignLeadUseCase {
	uc.userBindingRepo = repository
	return uc
}

func (uc *AssignLeadUseCase) WithGroupRobotNotifications(repository repo.GroupRobotNotificationRepository) *AssignLeadUseCase {
	uc.notificationRepo = repository
	return uc
}

func NewAssignLeadUseCase(
	leadRepo repo.LeadRepository,
	dispatchRepo repo.DispatchRecordRepository,
	cardRepo repo.CardTaskRepository,
	wecomClient wecom.Client,
	cache cache.Cache,
	taskQueue TaskQueue,
) *AssignLeadUseCase {
	defaults := runtime.NewStaticRuntimeSettings(runtime.DefaultWeComConfig())
	return &AssignLeadUseCase{
		leadRepo:         leadRepo,
		dispatchRepo:     dispatchRepo,
		cardRepo:         cardRepo,
		wecomClient:      wecomClient,
		cache:            cache,
		taskQueue:        taskQueue,
		metrics:          metrics.GetGlobalMetrics(),
		stateMachine:     lead.NewStateMachine(),
		cardLifecycle:    card.NewLifecycle(),
		runtimeSettings:  defaults,
		followupSettings: defaults,
	}
}

func (uc *AssignLeadUseCase) WithRuntimeSettings(settings runtime.RuntimeSettings) *AssignLeadUseCase {
	if settings != nil {
		uc.runtimeSettings = settings
		if followups, ok := settings.(runtime.FollowupPolicySettings); ok {
			uc.followupSettings = followups
		}
	}
	return uc
}

func (uc *AssignLeadUseCase) WithTransactionManager(txManager repo.TransactionManager) *AssignLeadUseCase {
	uc.txManager = txManager
	return uc
}

// AssignLeadRequest 分配线索请求
type AssignLeadRequest struct {
	LeadID        int64
	ManagerUserID string
	ActorRole     string
	SalesUserID   string
	Remark        string
}

// Execute 执行分配逻辑（事务边界）
func (uc *AssignLeadUseCase) Execute(ctx context.Context, req *AssignLeadRequest) error {
	if req == nil || req.ManagerUserID == "" || !types.CanInitiallyAssignLead(types.UserRole(req.ActorRole)) {
		return types.ErrUnauthorized
	}
	var assignedLead *types.Lead
	if err := uc.withTransaction(ctx, func(ctx context.Context, leadRepo repo.LeadRepository, dispatchRepo repo.DispatchRecordRepository, userBindingRepo repo.UserBindingRepository, notificationRepo repo.GroupRobotNotificationRepository) error {
		lead, err := uc.assignLead(ctx, req, leadRepo, dispatchRepo, userBindingRepo)
		if err != nil {
			return err
		}
		assignedLead = lead
		if notificationRepo != nil {
			dueAt := time.Now()
			if lead.AssignedAt != nil {
				dueAt = *lead.AssignedAt
			}
			if _, err := notificationRepo.Create(ctx, &types.GroupRobotNotification{
				EventKey: fmt.Sprintf("dispatch-assigned:%d:v%d", lead.ID, lead.Version),
				Category: types.GroupRobotCategoryDispatchClaim, EventType: types.GroupRobotEventDispatchAssigned,
				LeadID: lead.ID, BusinessVersion: lead.Version, DueAt: dueAt,
				Status: types.GroupRobotNotificationPending, CreatedAt: dueAt, UpdatedAt: dueAt,
			}); err != nil {
				return fmt.Errorf("create dispatch group robot notification: %w", err)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	uc.metrics.RecordStateTransition(true)
	uc.metrics.RecordLeadAssign()
	if waker, ok := uc.taskQueue.(wecom.GroupRobotScanWaker); ok {
		if err := waker.WakeGroupRobotScan(ctx); err != nil {
			logNonBlockingError("唤醒群机器人派发通知", assignedLead.ID, err)
		}
	}

	// 6. 提交更新主管卡片任务（异步队列）
	if uc.taskQueue != nil {
		if err := uc.taskQueue.EnqueueUpdateCard(ctx, assignedLead.ID, string(types.CardSceneSupervisorWaitingClaim)); err != nil {
			logNonBlockingError("提交主管等待领取卡片更新任务", assignedLead.ID, err)
			recordFailedCardUpdate(ctx, uc.cardRepo, assignedLead.ID, types.CardSceneSupervisorPendingDispatch, types.CardSceneSupervisorWaitingClaim, err)
		}
	}

	// 7. 提交发送销售领取卡片任务（异步队列）
	if uc.taskQueue != nil {
		if err := uc.taskQueue.EnqueueSendCard(ctx, assignedLead.ID, assignedLead.AssignedSalesUserID, string(types.CardSceneSalesPendingClaim)); err != nil {
			logNonBlockingError("提交销售待领取卡片任务", assignedLead.ID, err)
			recordFailedCardSend(ctx, uc.cardRepo, assignedLead.ID, assignedLead.AssignedSalesUserID, types.CardSceneSalesPendingClaim, err)
		}
	}

	// 8. 提交超时领取提醒任务
	if uc.taskQueue != nil {
		for _, delay := range uc.runtimeSettings.ClaimReminderDelays(ctx) {
			if err := uc.taskQueue.SchedulePendingClaimReminder(ctx, assignedLead.ID, delay); err != nil {
				logNonBlockingError("调度领取超时提醒", assignedLead.ID, err)
			}
		}
	}

	return nil
}

func (uc *AssignLeadUseCase) assignLead(
	ctx context.Context,
	req *AssignLeadRequest,
	leadRepo repo.LeadRepository,
	dispatchRepo repo.DispatchRecordRepository,
	userBindingRepo repo.UserBindingRepository,
) (*types.Lead, error) {
	// 1. 查询线索
	lead, err := leadRepo.GetByID(ctx, req.LeadID)
	if err != nil {
		return nil, err
	}
	if lead == nil {
		return nil, types.ErrLeadNotFound
	}
	if err := validateAssignmentTarget(ctx, userBindingRepo, req.SalesUserID, req.ManagerUserID, req.ActorRole); err != nil {
		return nil, err
	}
	targetIsManager, err := isActiveManagerAssignmentTarget(ctx, userBindingRepo, req.SalesUserID)
	if err != nil {
		return nil, err
	}

	// 2. 校验分配前置条件（纯函数）
	if err := uc.validateAssignment(lead, req); err != nil {
		return nil, err
	}

	// 3. 状态转换（纯函数）
	nextStatus, err := uc.stateMachine.NextState(lead.Status, types.LeadEventAssignedToSales)
	if err != nil {
		return nil, err
	}

	// 4. 更新线索状态（CRM 真实状态，必须成功）。调度员是首次分配的执行人，
	// 不是业务主管：平台线索若已有主管必须保留，没有则继续留空。
	businessManagerUserID := req.ManagerUserID
	if targetIsManager {
		businessManagerUserID = req.SalesUserID
	} else if types.UserRole(strings.TrimSpace(req.ActorRole)) == types.UserRoleDispatcher {
		businessManagerUserID = lead.DispatchManagerUserID
	}
	success, err := leadRepo.AtomicAssign(ctx, req.LeadID, businessManagerUserID, req.SalesUserID, lead.Version, lead.ProfileVersion)
	if err != nil {
		uc.metrics.RecordStateTransition(false)
		return nil, fmt.Errorf("更新线索状态失败: %w", err)
	}
	if !success {
		uc.metrics.RecordStateTransition(false)
		return nil, types.ErrLeadVersionConflict
	}

	// 5. 记录分配日志（与 AtomicAssign 同处一个事务，两者都成功才同步内存对象，
	//    避免 dispatchRepo.Create 失败后事务回滚时内存 version 已被错误递增的副作用）
	now := time.Now()
	dispatchRecord := &types.LeadDispatchRecord{
		LeadID:        req.LeadID,
		ManagerUserID: req.ManagerUserID,
		SalesUserID:   req.SalesUserID,
		Remark:        req.Remark,
		ActionType:    "assign",
		CreatedAt:     now,
	}
	if err := dispatchRepo.Create(ctx, dispatchRecord); err != nil {
		return nil, fmt.Errorf("记录分配日志失败: %w", err)
	}

	// 所有 DB 操作成功后才同步内存对象，确保事务回滚不会产生"内存已改、DB 已回滚"的歧义状态。
	lead.Status = nextStatus
	lead.DispatchManagerUserID = businessManagerUserID
	lead.AssignedSalesUserID = req.SalesUserID
	lead.AssignedInviterUserID = req.SalesUserID
	lead.OwnershipState = types.OwnershipAssignedPendingClaim
	lead.AssignedAt = &now
	lead.InviterAssignedAt = &now
	lead.Version++
	lead.UpdatedAt = now

	return lead, nil
}

func validateAssignmentTarget(ctx context.Context, repository repo.UserBindingRepository, targetUserID, actorUserID, actorRole string) error {
	if repository == nil {
		return nil
	}
	binding, err := repository.GetByWeComUserID(ctx, targetUserID)
	if err != nil {
		return err
	}
	if binding != nil && binding.Status == types.UserStatusActive && binding.Role == types.UserRoleManager {
		role := types.UserRole(strings.TrimSpace(actorRole))
		if role == types.UserRoleAdmin || role == types.UserRoleDispatcher || role == types.UserRoleManager {
			return nil
		}
		return types.ErrInvalidInviter
	}
	return validateAssignableInviter(ctx, repository, targetUserID)
}

func isActiveManagerAssignmentTarget(ctx context.Context, repository repo.UserBindingRepository, targetUserID string) (bool, error) {
	if repository == nil {
		return false, nil
	}
	binding, err := repository.GetByWeComUserID(ctx, targetUserID)
	if err != nil {
		return false, err
	}
	return binding != nil && binding.Status == types.UserStatusActive && binding.Role == types.UserRoleManager, nil
}

func (uc *AssignLeadUseCase) SelfClaim(ctx context.Context, leadID int64, managerUserID, actorRole string) error {
	if actorRole != string(types.UserRoleManager) {
		return types.ErrUnauthorized
	}
	followupSettings := snapshotFollowupPolicy(ctx, uc.followupSettings)
	var claimed *types.Lead
	create := func(ctx context.Context, leadRepo repo.LeadRepository, dispatchRepo repo.DispatchRecordRepository, reminderRepo repo.LeadReminderRepository) error {
		lead, err := leadRepo.GetByID(ctx, leadID)
		if err != nil {
			return err
		}
		if lead == nil {
			return types.ErrLeadNotFound
		}
		if lead.BusinessStage != types.BusinessStageInvitation || lead.ResultStatus == types.LeadResultConverted ||
			lead.ResultStatus == types.LeadResultInvalid || lead.ResultStatus == types.LeadResultClosed ||
			lead.OwnershipState == types.OwnershipPublicPool || lead.CurrentAppointmentID != nil ||
			(lead.Status != types.LeadStatusPendingDispatch && lead.Status != types.LeadStatusAssignedPendingClaim &&
				lead.Status != types.LeadStatusClaimed && lead.Status != types.LeadStatusFollowing) {
			return types.ErrLeadStatusInvalid
		}
		if lead.CurrentOwnerUserID == managerUserID {
			return types.ErrUnauthorized
		}
		now := time.Now()
		wasPendingDispatch := lead.Status == types.LeadStatusPendingDispatch
		lead.DispatchManagerUserID = managerUserID
		lead.AssignedSalesUserID, lead.AssignedInviterUserID = managerUserID, managerUserID
		lead.OwnerUserID, lead.InviterUserID = managerUserID, managerUserID
		lead.CurrentOwnerUserID, lead.CurrentOwnerRole = managerUserID, types.OwnerRoleInviter
		lead.Status = types.LeadStatusFollowing
		lead.OwnershipState = types.OwnershipPrivate
		lead.AssignedAt, lead.InviterAssignedAt = &now, &now
		lead.ClaimedAt, lead.InviterClaimedAt = &now, &now
		types.BeginOwnerCycle(lead, now)
		if lead.NextFollowupDueAt == nil {
			lead.NextFollowupDueAt = timePointer(now.Add(followupSettings.FirstFollowupDue(ctx)))
		}
		lead.Version++
		lead.UpdatedAt = now
		if err := leadRepo.Update(ctx, lead); err != nil {
			return err
		}
		actionType, remark := "reassign", "主管接管线索并亲自跟进"
		if wasPendingDispatch {
			actionType, remark = "self_assign", "主管选择亲自跟进"
		}
		for _, record := range []*types.LeadDispatchRecord{
			{LeadID: lead.ID, ManagerUserID: managerUserID, SalesUserID: managerUserID, ActionType: actionType, Remark: remark, CreatedAt: now},
			{LeadID: lead.ID, ManagerUserID: managerUserID, SalesUserID: managerUserID, ActionType: "claim", Remark: "主管已领取并开始跟进", CreatedAt: now},
		} {
			if err := dispatchRepo.Create(ctx, record); err != nil {
				return err
			}
		}
		if err := scheduleFollowupReminders(ctx, reminderRepo, followupSettings, lead); err != nil {
			return err
		}
		claimed = lead
		return nil
	}
	var err error
	if uc.txManager != nil {
		err = uc.txManager.WithinTransaction(ctx, func(ctx context.Context, repos repo.TransactionRepositories) error {
			return create(ctx, repos.LeadRepository(), repos.DispatchRecordRepository(), repos.LeadReminderRepository())
		})
	} else {
		err = create(ctx, uc.leadRepo, uc.dispatchRepo, nil)
	}
	if err != nil {
		return err
	}
	if uc.taskQueue != nil && claimed != nil {
		if err := uc.taskQueue.EnqueueUpdateCard(ctx, claimed.ID, string(types.CardSceneSupervisorClaimed)); err != nil {
			logNonBlockingError("提交主管自领卡片更新任务", claimed.ID, err)
		}
	}
	return nil
}

func validateAssignableInviter(ctx context.Context, repository repo.UserBindingRepository, userID string) error {
	if repository == nil {
		return nil
	}
	binding, err := repository.GetByWeComUserID(ctx, userID)
	if err != nil {
		return err
	}
	if binding == nil || binding.Status != types.UserStatusActive || !types.IsInvitationRole(binding.Role) {
		return types.ErrInvalidInviter
	}
	return nil
}

func (uc *AssignLeadUseCase) withTransaction(
	ctx context.Context,
	fn func(context.Context, repo.LeadRepository, repo.DispatchRecordRepository, repo.UserBindingRepository, repo.GroupRobotNotificationRepository) error,
) error {
	if uc.txManager == nil {
		return fn(ctx, uc.leadRepo, uc.dispatchRepo, uc.userBindingRepo, uc.notificationRepo)
	}
	return uc.txManager.WithinTransaction(ctx, func(ctx context.Context, repos repo.TransactionRepositories) error {
		var userBindingRepo repo.UserBindingRepository
		if uc.userBindingRepo != nil {
			userBindingRepo = repos.UserBindingRepository()
		}
		var notificationRepo repo.GroupRobotNotificationRepository
		if uc.notificationRepo != nil {
			notificationRepo = repos.GroupRobotNotificationRepository()
		}
		return fn(ctx, repos.LeadRepository(), repos.DispatchRecordRepository(), userBindingRepo, notificationRepo)
	})
}

func (uc *AssignLeadUseCase) validateAssignment(lead *types.Lead, req *AssignLeadRequest) error {
	if req.ActorRole != string(types.UserRoleAdmin) && req.ActorRole != string(types.UserRoleManager) {
		return uc.stateMachine.ValidateAssignment(lead, req.ManagerUserID, req.SalesUserID)
	}
	if types.IsTerminalLeadStatus(lead.Status) {
		return types.ErrLeadStatusInvalid
	}
	if lead.Status != types.LeadStatusPendingDispatch {
		return types.ErrLeadStatusInvalid
	}
	if req.SalesUserID == "" {
		return types.ErrUnauthorized
	}
	return nil
}
