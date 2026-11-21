package usecase

import (
	"context"
	"fmt"
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

// ClaimLeadUseCase 销售领取线索用例
type ClaimLeadUseCase struct {
	leadRepo      repo.LeadRepository
	cardRepo      repo.CardTaskRepository
	wecomClient   wecom.Client
	cache         cache.Cache
	taskQueue     TaskQueue
	dispatchRepo  repo.DispatchRecordRepository
	txManager     repo.TransactionManager
	metrics       *metrics.Metrics
	stateMachine  *lead.StateMachine
	cardLifecycle *card.Lifecycle
	flags         runtime.WorkflowFeatureFlags
	settings      runtime.FollowupPolicySettings
	reminderRepo  repo.LeadReminderRepository
}

func (uc *ClaimLeadUseCase) WithStage3Workflow(flags runtime.WorkflowFeatureFlags, settings runtime.FollowupPolicySettings, reminderRepo repo.LeadReminderRepository) *ClaimLeadUseCase {
	uc.flags = flags
	uc.settings = settings
	uc.reminderRepo = reminderRepo
	return uc
}

func (uc *ClaimLeadUseCase) WithDispatchHistory(dispatchRepo repo.DispatchRecordRepository, txManager repo.TransactionManager) *ClaimLeadUseCase {
	uc.dispatchRepo = dispatchRepo
	uc.txManager = txManager
	return uc
}

func NewClaimLeadUseCase(
	leadRepo repo.LeadRepository,
	cardRepo repo.CardTaskRepository,
	wecomClient wecom.Client,
	cache cache.Cache,
	taskQueue TaskQueue,
) *ClaimLeadUseCase {
	return &ClaimLeadUseCase{
		leadRepo:      leadRepo,
		cardRepo:      cardRepo,
		wecomClient:   wecomClient,
		cache:         cache,
		taskQueue:     taskQueue,
		metrics:       metrics.GetGlobalMetrics(),
		stateMachine:  lead.NewStateMachine(),
		cardLifecycle: card.NewLifecycle(),
	}
}

// ClaimLeadRequest 领取线索请求
type ClaimLeadRequest struct {
	LeadID      int64
	SalesUserID string
	TaskID      string // 来自企业微信回调
	Source      string // wecom_card / h5
}

// Execute 执行领取逻辑（包含幂等保障）
func (uc *ClaimLeadUseCase) Execute(ctx context.Context, req *ClaimLeadRequest) error {
	// 1. Redis 幂等键（快速拒绝重复请求）
	snapshot, lookupErr := uc.leadRepo.GetByID(ctx, req.LeadID)
	if lookupErr != nil {
		return lookupErr
	}
	if snapshot == nil {
		return types.ErrLeadNotFound
	}
	idempotentKey := fmt.Sprintf("lead:claim:%d:v%d:%s", req.LeadID, snapshot.Version, req.SalesUserID)
	if uc.cache != nil {
		acquired, err := uc.cache.SetNX(ctx, idempotentKey, "1", 60*time.Second)
		if err != nil {
			logNonBlockingError("Redis 领取幂等检查", req.LeadID, err)
		} else if !acquired {
			// 1 分钟内重复领取，查询当前状态返回
			lead, _ := uc.leadRepo.GetByID(ctx, req.LeadID)
			if lead != nil && lead.Status == types.LeadStatusClaimed {
				return types.ErrLeadAlreadyClaimed
			}
			return types.ErrDuplicateClaim
		}
	}

	followupPolicyEnabled := uc.flags != nil && (uc.flags.WorkflowV2Enabled(ctx) || uc.flags.PoolEnabled(ctx))
	var followupSettings runtime.FollowupPolicySettings
	if followupPolicyEnabled {
		followupSettings = snapshotFollowupPolicy(ctx, uc.settings)
	}

	lead, err := uc.claim(ctx, req, followupPolicyEnabled, followupSettings)
	if err != nil {
		if uc.cache != nil {
			_ = uc.cache.Del(ctx, idempotentKey)
		}
		return err
	}

	// 6. 领取成功，记录监控指标
	uc.metrics.RecordLeadClaim(true)
	uc.metrics.RecordStateTransition(true)

	// 提交更新销售卡片任务
	if uc.taskQueue != nil {
		if err := uc.taskQueue.EnqueueUpdateCard(ctx, lead.ID, string(types.CardSceneSalesClaimed)); err != nil {
			logNonBlockingError("提交销售已领取卡片更新任务", lead.ID, err)
			recordFailedCardUpdate(ctx, uc.cardRepo, lead.ID, types.CardSceneSalesPendingClaim, types.CardSceneSalesClaimed, err)
		}
	}

	// 提交更新主管卡片任务
	if uc.taskQueue != nil {
		if err := uc.taskQueue.EnqueueUpdateCard(ctx, lead.ID, string(types.CardSceneSupervisorClaimed)); err != nil {
			logNonBlockingError("提交主管已领取卡片更新任务", lead.ID, err)
			recordFailedCardUpdate(ctx, uc.cardRepo, lead.ID, types.CardSceneSupervisorWaitingClaim, types.CardSceneSupervisorClaimed, err)
		}
	}

	return nil
}

func (uc *ClaimLeadUseCase) claim(ctx context.Context, req *ClaimLeadRequest, followupPolicyEnabled bool, followupSettings runtime.FollowupPolicySettings) (*types.Lead, error) {
	var claimed *types.Lead
	run := func(ctx context.Context, leadRepo repo.LeadRepository, dispatchRepo repo.DispatchRecordRepository, reminderRepo repo.LeadReminderRepository, appointments repo.AppointmentRepository) error {
		lead, err := leadRepo.GetByID(ctx, req.LeadID)
		if err != nil {
			return err
		}
		if lead == nil {
			return types.ErrLeadNotFound
		}
		if err := uc.stateMachine.ValidateClaim(lead, req.SalesUserID); err != nil {
			return err
		}
		nextStatus, err := uc.stateMachine.NextState(lead.Status, types.LeadEventClaimedBySales)
		if err != nil {
			return err
		}
		success, err := leadRepo.AtomicClaim(ctx, req.LeadID, req.SalesUserID, lead.Version, lead.ProfileVersion)
		if err != nil {
			return fmt.Errorf("领取失败: %w", err)
		}
		if !success {
			latest, _ := leadRepo.GetByID(ctx, req.LeadID)
			if latest == nil {
				return types.ErrLeadNotFound
			}
			if latest.Status == types.LeadStatusClaimed {
				return types.ErrLeadAlreadyClaimed
			}
			if latest.AssignedSalesUserID != req.SalesUserID {
				return types.ErrLeadReassigned
			}
			return types.ErrLeadVersionConflict
		}
		now := time.Now()
		lead.Version++
		if dispatchRepo != nil {
			record := &types.LeadDispatchRecord{
				LeadID: lead.ID, ManagerUserID: lead.DispatchManagerUserID,
				SalesUserID: req.SalesUserID, ActionType: "claim",
				Remark: "销售领取线索（" + req.Source + "）", CreatedAt: now,
			}
			if err := dispatchRepo.Create(ctx, record); err != nil {
				return fmt.Errorf("记录领取日志失败: %w", err)
			}
		}
		lead.Status, lead.OwnerUserID, lead.ClaimedAt = nextStatus, req.SalesUserID, &now
		lead.InviterUserID = req.SalesUserID
		lead.CurrentOwnerUserID = req.SalesUserID
		lead.CurrentOwnerRole = types.OwnerRoleInviter
		lead.OwnershipState = types.OwnershipPrivate
		lead.InviterClaimedAt = &now
		types.BeginOwnerCycle(lead, now)
		if lead.CurrentAppointmentID != nil {
			if err := transferActiveAppointment(ctx, appointments, lead, req.SalesUserID, "inviter", true); err != nil {
				return err
			}
			lead.Version++
			lead.UpdatedAt = now
			if err := leadRepo.Update(ctx, lead); err != nil {
				return err
			}
			appt, err := appointments.GetByID(ctx, *lead.CurrentAppointmentID)
			if err != nil {
				return err
			}
			if err := writeAppointmentReminders(ctx, reminderRepo, appt, lead); err != nil {
				return err
			}
		} else if followupPolicyEnabled && followupSettings != nil {
			lead.NextFollowupDueAt = timePointer(now.Add(followupSettings.FirstFollowupDue(ctx)))
			lead.Version++
			lead.UpdatedAt = now
			if err := leadRepo.Update(ctx, lead); err != nil {
				return fmt.Errorf("设置首次回访截止时间失败: %w", err)
			}
			if err := scheduleFollowupReminders(ctx, reminderRepo, followupSettings, lead); err != nil {
				return fmt.Errorf("创建首次回访提醒失败: %w", err)
			}
		}
		claimed = lead
		return nil
	}
	var err error
	if uc.txManager != nil {
		err = uc.txManager.WithinTransaction(ctx, func(ctx context.Context, repos repo.TransactionRepositories) error {
			return run(ctx, repos.LeadRepository(), repos.DispatchRecordRepository(), repos.LeadReminderRepository(), repos.AppointmentRepository())
		})
	} else {
		err = run(ctx, uc.leadRepo, uc.dispatchRepo, uc.reminderRepo, nil)
	}
	if err != nil {
		uc.metrics.RecordLeadClaim(false)
		uc.metrics.RecordStateTransition(false)
		return nil, err
	}
	return claimed, nil
}
