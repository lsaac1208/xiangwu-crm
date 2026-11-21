package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/domain/lead"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/pkg/types"
)

// ReassignLeadUseCase 主管转派线索用例。
type ReassignLeadUseCase struct {
	leadRepo        repo.LeadRepository
	dispatchRepo    repo.DispatchRecordRepository
	cardRepo        repo.CardTaskRepository
	txManager       repo.TransactionManager
	taskQueue       TaskQueue
	stateMachine    *lead.StateMachine
	runtimeSettings runtime.RuntimeSettings
	userBindingRepo repo.UserBindingRepository
}

func (uc *ReassignLeadUseCase) WithUserBindingRepository(repository repo.UserBindingRepository) *ReassignLeadUseCase {
	uc.userBindingRepo = repository
	return uc
}

func NewReassignLeadUseCase(
	leadRepo repo.LeadRepository,
	dispatchRepo repo.DispatchRecordRepository,
	cardRepo repo.CardTaskRepository,
	taskQueue TaskQueue,
) *ReassignLeadUseCase {
	return &ReassignLeadUseCase{
		leadRepo:        leadRepo,
		dispatchRepo:    dispatchRepo,
		cardRepo:        cardRepo,
		taskQueue:       taskQueue,
		stateMachine:    lead.NewStateMachine(),
		runtimeSettings: runtime.NewStaticRuntimeSettings(runtime.DefaultWeComConfig()),
	}
}

func (uc *ReassignLeadUseCase) WithRuntimeSettings(settings runtime.RuntimeSettings) *ReassignLeadUseCase {
	if settings != nil {
		uc.runtimeSettings = settings
	}
	return uc
}

func (uc *ReassignLeadUseCase) WithTransactionManager(txManager repo.TransactionManager) *ReassignLeadUseCase {
	uc.txManager = txManager
	return uc
}

// ReassignLeadRequest 转派线索请求。
type ReassignLeadRequest struct {
	LeadID         int64
	ManagerUserID  string
	ActorRole      string
	NewSalesUserID string
	Remark         string
}

// Execute 执行转派逻辑。
func (uc *ReassignLeadUseCase) Execute(ctx context.Context, req *ReassignLeadRequest) error {
	var reassignedLead *types.Lead
	var oldSalesUserID string
	if err := uc.withTransaction(ctx, func(ctx context.Context, leadRepo repo.LeadRepository, dispatchRepo repo.DispatchRecordRepository, bindings repo.UserBindingRepository, appointments repo.AppointmentRepository) error {
		lead, oldSales, err := uc.reassignLead(ctx, req, leadRepo, dispatchRepo, bindings, appointments)
		if err != nil {
			return err
		}
		reassignedLead = lead
		oldSalesUserID = oldSales
		return nil
	}); err != nil {
		return err
	}

	uc.markOldSalesCardReassigned(ctx, reassignedLead.ID, oldSalesUserID)
	if uc.taskQueue != nil {
		if err := uc.taskQueue.EnqueueSendCard(ctx, reassignedLead.ID, reassignedLead.AssignedSalesUserID, string(types.CardSceneSalesPendingClaim)); err != nil {
			logNonBlockingError("提交转派后销售待领取卡片任务", reassignedLead.ID, err)
			recordFailedCardSend(ctx, uc.cardRepo, reassignedLead.ID, reassignedLead.AssignedSalesUserID, types.CardSceneSalesPendingClaim, err)
		}
		for _, delay := range uc.runtimeSettings.ClaimReminderDelays(ctx) {
			if err := uc.taskQueue.SchedulePendingClaimReminder(ctx, reassignedLead.ID, delay); err != nil {
				logNonBlockingError("调度转派后领取超时提醒", reassignedLead.ID, err)
			}
		}
	}

	return nil
}

func (uc *ReassignLeadUseCase) reassignLead(
	ctx context.Context,
	req *ReassignLeadRequest,
	leadRepo repo.LeadRepository,
	dispatchRepo repo.DispatchRecordRepository,
	bindings repo.UserBindingRepository,
	appointments repo.AppointmentRepository,
) (*types.Lead, string, error) {
	lead, err := leadRepo.GetByID(ctx, req.LeadID)
	if err != nil {
		return nil, "", err
	}
	if lead == nil {
		return nil, "", types.ErrLeadNotFound
	}
	if err := validateAssignmentTarget(ctx, bindings, req.NewSalesUserID, req.ManagerUserID, req.ActorRole); err != nil {
		return nil, "", err
	}
	targetIsManager, err := isActiveManagerAssignmentTarget(ctx, bindings, req.NewSalesUserID)
	if err != nil {
		return nil, "", err
	}

	oldSalesUserID := lead.AssignedSalesUserID
	if err := uc.validateReassignment(lead, req); err != nil {
		return nil, "", err
	}

	now := time.Now()
	if targetIsManager {
		lead.DispatchManagerUserID = req.NewSalesUserID
	}
	lead.AssignedSalesUserID = req.NewSalesUserID
	lead.AssignedInviterUserID = req.NewSalesUserID
	lead.OwnerUserID = ""
	lead.InviterUserID = ""
	lead.CurrentOwnerUserID = ""
	lead.CurrentOwnerRole = ""
	lead.OwnershipState = types.OwnershipAssignedPendingClaim
	lead.ClaimedAt = nil
	types.ClearOwnerCycle(lead)
	lead.InviterClaimedAt = nil
	lead.NextFollowupDueAt = nil
	lead.AssignedAt = &now
	lead.InviterAssignedAt = &now
	lead.Status = types.LeadStatusAssignedPendingClaim
	lead.Version++
	lead.UpdatedAt = now
	if err := transferActiveAppointment(ctx, appointments, lead, req.ManagerUserID, req.ActorRole, false); err != nil {
		return nil, "", err
	}
	if err := leadRepo.Update(ctx, lead); err != nil {
		return nil, "", fmt.Errorf("转派失败: %w", err)
	}
	record := &types.LeadDispatchRecord{
		LeadID:        req.LeadID,
		ManagerUserID: req.ManagerUserID,
		SalesUserID:   req.NewSalesUserID,
		Remark:        req.Remark,
		ActionType:    "reassign",
		CreatedAt:     now,
	}
	if err := dispatchRepo.Create(ctx, record); err != nil {
		return nil, "", fmt.Errorf("记录转派日志失败: %w", err)
	}

	return lead, oldSalesUserID, nil
}

func (uc *ReassignLeadUseCase) withTransaction(
	ctx context.Context,
	fn func(context.Context, repo.LeadRepository, repo.DispatchRecordRepository, repo.UserBindingRepository, repo.AppointmentRepository) error,
) error {
	if uc.txManager == nil {
		return fn(ctx, uc.leadRepo, uc.dispatchRepo, uc.userBindingRepo, nil)
	}
	return uc.txManager.WithinTransaction(ctx, func(ctx context.Context, repos repo.TransactionRepositories) error {
		var bindings repo.UserBindingRepository
		if uc.userBindingRepo != nil {
			bindings = repos.UserBindingRepository()
		}
		return fn(ctx, repos.LeadRepository(), repos.DispatchRecordRepository(), bindings, repos.AppointmentRepository())
	})
}

func (uc *ReassignLeadUseCase) validateReassignment(lead *types.Lead, req *ReassignLeadRequest) error {
	return validateReassignmentState(lead, req.NewSalesUserID, req.ActorRole, types.BusinessStageInvitation)
}

func (uc *ReassignLeadUseCase) markOldSalesCardReassigned(ctx context.Context, leadID int64, oldSalesUserID string) {
	if oldSalesUserID == "" || uc.cardRepo == nil {
		return
	}

	cardTask, err := uc.cardRepo.GetLatestByScene(ctx, leadID, types.CardSceneSalesPendingClaim)
	if err != nil || cardTask == nil || cardTask.ReceiverUserID != oldSalesUserID {
		return
	}

	cardTask.CardStatus = types.CardStatusReplaced
	now := time.Now()
	cardTask.UpdatedAt = &now
	if err := uc.cardRepo.Update(ctx, cardTask); err != nil {
		logNonBlockingError("标记旧销售卡片已替代", leadID, err)
	}
}
