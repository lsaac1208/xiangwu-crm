package usecase

import (
	"context"
	"log/slog"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

// GetLeadUseCase 获取线索详情用例
type GetLeadUseCase struct {
	notificationDiagnostics repo.GroupRobotDiagnosticsReader
	leadRepo                repo.LeadRepository
	followupRepo            repo.FollowupRepository
	dispatchRepo            repo.DispatchRecordRepository
	userBindingRepo         repo.UserBindingRepository
	activityRepo            repo.LeadActivityRepository
}

func (uc *GetLeadUseCase) WithNotificationDiagnostics(reader repo.GroupRobotDiagnosticsReader) *GetLeadUseCase {
	uc.notificationDiagnostics = reader
	return uc
}

func (uc *GetLeadUseCase) WithActivityRepository(activityRepo repo.LeadActivityRepository) *GetLeadUseCase {
	uc.activityRepo = activityRepo
	return uc
}

func NewGetLeadUseCase(
	leadRepo repo.LeadRepository,
	followupRepo repo.FollowupRepository,
	dispatchRepo repo.DispatchRecordRepository,
) *GetLeadUseCase {
	return &GetLeadUseCase{
		leadRepo:     leadRepo,
		followupRepo: followupRepo,
		dispatchRepo: dispatchRepo,
	}
}

func (uc *GetLeadUseCase) WithUserBindingRepository(userBindingRepo repo.UserBindingRepository) *GetLeadUseCase {
	uc.userBindingRepo = userBindingRepo
	return uc
}

// GetLeadResponse 线索详情响应
type GetLeadResponse struct {
	NotificationFailures []repo.GroupRobotDeliveryFailure `json:"notification_failures"`
	NotificationStatus   string                           `json:"notification_status"`
	Lead                 *types.Lead                      `json:"lead"`
	Followups            []*types.LeadFollowup            `json:"followups"`
	DispatchRecords      []*types.LeadDispatchRecord      `json:"dispatch_records"`
	Activities           []*types.LeadActivityRecord      `json:"activities"`
}

// Execute 执行获取线索
func (uc *GetLeadUseCase) Execute(ctx context.Context, leadID int64, currentUserID string, roles ...string) (*GetLeadResponse, error) {
	// 查询线索
	lead, err := uc.leadRepo.GetByID(ctx, leadID)
	if err != nil {
		return nil, err
	}
	if lead == nil {
		return nil, types.ErrLeadNotFound
	}

	// 权限校验（主管或线索负责人才能查看）
	// 简化实现：假设传入的 currentUserID 是企业微信 UserID
	// 生产环境应从数据库查询用户角色
	role := ""
	if len(roles) > 0 {
		role = roles[0]
	}
	dispatcherCanView := false
	if role == string(types.UserRoleDispatcher) {
		dispatcherRepo, ok := uc.leadRepo.(repo.DispatcherLeadRepository)
		if !ok {
			return nil, types.ErrUnauthorized
		}
		dispatcherCanView, err = dispatcherRepo.CanDispatcherViewLead(ctx, leadID, currentUserID)
		if err != nil {
			return nil, err
		}
	}

	canView := role == string(types.UserRoleAdmin) ||
		role == string(types.UserRoleManager) ||
		dispatcherCanView ||
		(types.IsInvitationRole(types.UserRole(role)) &&
			(lead.AssignedSalesUserID == currentUserID || lead.OwnerUserID == currentUserID || lead.InviterUserID == currentUserID || lead.AssignedInviterUserID == currentUserID)) ||
		(role == string(types.UserRoleCloser) &&
			(lead.CreatedByUserID == currentUserID || ((lead.BusinessStage == types.BusinessStageNegotiation || ((lead.Status == types.LeadStatusConverted || lead.Status == types.LeadStatusInvalid) && correctionStage(lead) == types.BusinessStageNegotiation)) &&
				(lead.CloserUserID == currentUserID || lead.CurrentOwnerUserID == currentUserID))))

	if !canView {
		return nil, types.ErrUnauthorized
	}
	if err := enrichLeadUserNames(ctx, uc.userBindingRepo, lead); err != nil {
		return nil, err
	}
	applyLeadMobileVisibility(lead, currentUserID, role)

	// 查询跟进记录
	followups, err := uc.followupRepo.ListByLeadID(ctx, leadID)
	if err != nil {
		return nil, err
	}

	// 查询分配记录
	dispatchRecords, err := uc.dispatchRepo.ListByLeadID(ctx, leadID)
	if err != nil {
		return nil, err
	}
	var activities []*types.LeadActivityRecord
	if uc.activityRepo != nil {
		activities, err = uc.activityRepo.ListByLead(ctx, leadID)
		if err != nil {
			return nil, err
		}
	}
	if uc.userBindingRepo != nil {
		ids := make([]string, 0, len(dispatchRecords)*2+len(followups)+len(activities))
		for _, record := range dispatchRecords {
			ids = append(ids, record.ManagerUserID, record.SalesUserID)
		}
		for _, followup := range followups {
			ids = append(ids, followup.SalesUserID)
		}
		for _, activity := range activities {
			ids = append(ids, activity.ActorUserID)
		}
		names, err := loadUserDisplayNames(ctx, uc.userBindingRepo, ids)
		if err != nil {
			return nil, err
		}
		for _, record := range dispatchRecords {
			record.ManagerName = displayNameOrID(names, record.ManagerUserID)
			record.SalesName = displayNameOrID(names, record.SalesUserID)
		}
		for _, followup := range followups {
			followup.SalesName = displayNameOrID(names, followup.SalesUserID)
		}
		for _, activity := range activities {
			activity.ActorName = displayNameOrID(names, activity.ActorUserID)
		}
	}

	notificationStatus := "unavailable"
	notificationFailures := []repo.GroupRobotDeliveryFailure{}
	if uc.notificationDiagnostics != nil {
		rows, readErr := uc.notificationDiagnostics.LeadDeliveryFailures(ctx, leadID, time.Now().UTC().Add(-24*time.Hour))
		if readErr != nil {
			notificationStatus = "unknown"
			slog.Warn("lead delivery diagnostics unavailable", "lead_id", leadID)
		} else {
			notificationStatus = "available"
			notificationFailures = rows
		}
	}
	return &GetLeadResponse{
		NotificationFailures: notificationFailures,
		NotificationStatus:   notificationStatus,
		Lead:                 lead,
		Followups:            followups,
		DispatchRecords:      dispatchRecords,
		Activities:           activities,
	}, nil
}
