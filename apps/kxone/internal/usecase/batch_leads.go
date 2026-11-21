package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/pkg/types"
)

type BatchLeadItem struct {
	LeadID         int64 `json:"lead_id"`
	Version        int   `json:"version"`
	ProfileVersion int   `json:"profile_version"`
}

type BatchLeadRequest struct {
	Items         []BatchLeadItem `json:"items"`
	TargetUserID  string          `json:"target_user_id"`
	BusinessStage string          `json:"business_stage"`
	Remark        string          `json:"remark"`
	ActorUserID   string          `json:"-"`
	ActorRole     string          `json:"-"`
}

type BatchLeadResult struct {
	Updated int `json:"updated"`
}

type BatchLeadConflict struct {
	LeadNos []string `json:"lead_nos"`
}

func (e *BatchLeadConflict) Error() string { return "batch lead conflict" }

type BatchLeadsUseCase struct {
	txManager repo.TransactionManager
	settings  interface{}
}

func NewBatchLeadsUseCase(txManager repo.TransactionManager) *BatchLeadsUseCase {
	return &BatchLeadsUseCase{txManager: txManager}
}

func validateBatchLeadRequest(req *BatchLeadRequest) error {
	if req == nil || len(req.Items) == 0 || len(req.Items) > 50 || strings.TrimSpace(req.TargetUserID) == "" {
		return types.ErrInvalidBatchLeadRequest
	}
	seen := map[int64]bool{}
	for _, item := range req.Items {
		if item.LeadID <= 0 || item.Version < 0 || item.ProfileVersion < 1 || seen[item.LeadID] {
			return types.ErrInvalidBatchLeadRequest
		}
		seen[item.LeadID] = true
	}
	return nil
}

func (uc *BatchLeadsUseCase) Assign(ctx context.Context, req *BatchLeadRequest) (*BatchLeadResult, error) {
	if err := validateBatchLeadRequest(req); err != nil {
		return nil, err
	}
	if !types.CanInitiallyAssignLead(types.UserRole(req.ActorRole)) {
		return nil, types.ErrUnauthorized
	}
	err := uc.txManager.WithinTransaction(ctx, func(ctx context.Context, repos repo.TransactionRepositories) error {
		if err := validateAssignmentTarget(ctx, repos.UserBindingRepository(), req.TargetUserID, req.ActorUserID, req.ActorRole); err != nil {
			return err
		}
		leads, conflict, err := loadBatchLeads(ctx, repos.LeadRepository(), req.Items)
		if err != nil {
			return err
		}
		if len(conflict) > 0 {
			return &BatchLeadConflict{LeadNos: conflict}
		}
		for _, lead := range leads {
			if lead.Status != types.LeadStatusPendingDispatch || lead.BusinessStage != types.BusinessStageInvitation || lead.ResultStatus != types.LeadResultActive {
				return &BatchLeadConflict{LeadNos: []string{lead.LeadNo}}
			}
		}
		for _, lead := range leads {
			targetIsManager, err := isActiveManagerAssignmentTarget(ctx, repos.UserBindingRepository(), req.TargetUserID)
			if err != nil {
				return err
			}
			managerID := req.ActorUserID
			if targetIsManager {
				managerID = req.TargetUserID
			} else if types.UserRole(req.ActorRole) == types.UserRoleDispatcher {
				managerID = lead.DispatchManagerUserID
			}
			now := time.Now()
			ok, err := repos.LeadRepository().AtomicAssign(ctx, lead.ID, managerID, req.TargetUserID, lead.Version, lead.ProfileVersion)
			if err != nil {
				return err
			}
			if !ok {
				return &BatchLeadConflict{LeadNos: []string{lead.LeadNo}}
			}
			if err := repos.DispatchRecordRepository().Create(ctx, &types.LeadDispatchRecord{LeadID: lead.ID, ManagerUserID: req.ActorUserID, SalesUserID: req.TargetUserID, Remark: strings.TrimSpace(req.Remark), ActionType: "assign", CreatedAt: now}); err != nil {
				return err
			}
			if notifications := repos.GroupRobotNotificationRepository(); notifications != nil {
				if _, err := notifications.Create(ctx, &types.GroupRobotNotification{EventKey: fmt.Sprintf("dispatch-assigned:%d:v%d", lead.ID, lead.Version+1), Category: types.GroupRobotCategoryDispatchClaim, EventType: types.GroupRobotEventDispatchAssigned, LeadID: lead.ID, BusinessVersion: lead.Version + 1, DueAt: now, Status: types.GroupRobotNotificationPending, CreatedAt: now, UpdatedAt: now}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &BatchLeadResult{Updated: len(req.Items)}, nil
}

func (uc *BatchLeadsUseCase) Reassign(ctx context.Context, req *BatchLeadRequest) (*BatchLeadResult, error) {
	if err := validateBatchLeadRequest(req); err != nil {
		return nil, err
	}
	if req.ActorRole != string(types.UserRoleAdmin) && req.ActorRole != string(types.UserRoleManager) {
		return nil, types.ErrUnauthorized
	}
	stage := types.BusinessStage(strings.TrimSpace(req.BusinessStage))
	if stage != types.BusinessStageInvitation && stage != types.BusinessStageNegotiation {
		return nil, types.ErrInvalidBatchLeadRequest
	}
	err := uc.txManager.WithinTransaction(ctx, func(ctx context.Context, repos repo.TransactionRepositories) error {
		targetIsManager := false
		if stage == types.BusinessStageInvitation {
			if err := validateBatchInvitationTarget(ctx, repos.UserBindingRepository(), req.TargetUserID); err != nil {
				return err
			}
			var err error
			targetIsManager, err = isActiveManagerAssignmentTarget(ctx, repos.UserBindingRepository(), req.TargetUserID)
			if err != nil {
				return err
			}
		} else if err := validateAssignableCloser(ctx, repos.UserBindingRepository(), req.TargetUserID); err != nil {
			return err
		}
		leads, conflict, err := loadBatchLeads(ctx, repos.LeadRepository(), req.Items)
		if err != nil {
			return err
		}
		if len(conflict) > 0 {
			return &BatchLeadConflict{LeadNos: conflict}
		}
		conflicts := []string{}
		for _, lead := range leads {
			if validateReassignmentState(lead, req.TargetUserID, req.ActorRole, stage) != nil {
				conflicts = append(conflicts, lead.LeadNo)
			}
		}
		if len(conflicts) > 0 {
			return &BatchLeadConflict{LeadNos: conflicts}
		}

		for _, lead := range leads {
			now := time.Now()
			oldOwner := lead.CurrentOwnerUserID
			if stage == types.BusinessStageInvitation {
				if targetIsManager {
					lead.DispatchManagerUserID = req.TargetUserID
				}
				lead.AssignedSalesUserID, lead.AssignedInviterUserID = req.TargetUserID, req.TargetUserID
				lead.OwnerUserID, lead.InviterUserID, lead.CurrentOwnerUserID, lead.CurrentOwnerRole = "", "", "", ""
				lead.OwnershipState, lead.Status = types.OwnershipAssignedPendingClaim, types.LeadStatusAssignedPendingClaim
				lead.ClaimedAt, lead.InviterClaimedAt, lead.NextFollowupDueAt = nil, nil, nil
				types.ClearOwnerCycle(lead)
				lead.AssignedAt, lead.InviterAssignedAt = &now, &now
				lead.Version++
				lead.UpdatedAt = now
				if err := transferActiveAppointment(ctx, repos.AppointmentRepository(), lead, req.ActorUserID, req.ActorRole, false); err != nil {
					return err
				}
				if err := repos.LeadRepository().Update(ctx, lead); err != nil {
					return err
				}
				if err := repos.DispatchRecordRepository().Create(ctx, &types.LeadDispatchRecord{LeadID: lead.ID, ManagerUserID: req.ActorUserID, SalesUserID: req.TargetUserID, Remark: strings.TrimSpace(req.Remark), ActionType: "reassign", CreatedAt: now}); err != nil {
					return err
				}
				if notifications := repos.GroupRobotNotificationRepository(); notifications != nil {
					if _, err := notifications.Create(ctx, &types.GroupRobotNotification{EventKey: fmt.Sprintf("dispatch-assigned:%d:v%d", lead.ID, lead.Version), Category: types.GroupRobotCategoryDispatchClaim, EventType: types.GroupRobotEventDispatchAssigned, LeadID: lead.ID, BusinessVersion: lead.Version, DueAt: now, Status: types.GroupRobotNotificationPending, CreatedAt: now, UpdatedAt: now}); err != nil {
						return err
					}
				}
			} else {
				before, _ := json.Marshal(map[string]any{"closer_userid": lead.CloserUserID, "owner_userid": oldOwner})
				lead.CloserUserID, lead.CurrentOwnerUserID, lead.AssignedSalesUserID, lead.OwnerUserID = req.TargetUserID, req.TargetUserID, req.TargetUserID, req.TargetUserID
				types.BeginOwnerCycle(lead, now)
				lead.Version++
				lead.UpdatedAt = now
				if err := repos.LeadRepository().Update(ctx, lead); err != nil {
					return err
				}
				if err := scheduleFollowupReminders(ctx, repos.LeadReminderRepository(), snapshotFollowupPolicy(ctx, runtime.NewDatabaseRuntimeSettings(repos.SystemSettingRepository(), 0)), lead); err != nil {
					return err
				}
				after, _ := json.Marshal(map[string]any{"closer_userid": lead.CloserUserID, "owner_userid": lead.CurrentOwnerUserID})
				metadata, _ := json.Marshal(map[string]any{"reason": strings.TrimSpace(req.Remark), "previous_closer_userid": oldOwner, "batch": true})
				if err := repos.LeadActivityRepository().Create(ctx, &types.LeadActivityRecord{LeadID: lead.ID, ActivityType: types.LeadActivityCloserReassigned, ActorUserID: req.ActorUserID, ActorRole: req.ActorRole, FromValueJSON: string(before), ToValueJSON: string(after), MetadataJSON: string(metadata), CreatedAt: now}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &BatchLeadResult{Updated: len(req.Items)}, nil
}

func validateBatchInvitationTarget(ctx context.Context, repository repo.UserBindingRepository, targetUserID string) error {
	binding, err := repository.GetByWeComUserID(ctx, strings.TrimSpace(targetUserID))
	if err != nil {
		return err
	}
	if binding == nil || binding.Status != types.UserStatusActive || (!types.IsInvitationRole(binding.Role) && binding.Role != types.UserRoleManager) {
		return types.ErrInvalidInviter
	}
	return nil
}

func loadBatchLeads(ctx context.Context, repository repo.LeadRepository, items []BatchLeadItem) ([]*types.Lead, []string, error) {
	leads := make([]*types.Lead, 0, len(items))
	conflicts := []string{}
	for _, item := range items {
		lead, err := repository.GetByID(ctx, item.LeadID)
		if err != nil {
			return nil, nil, err
		}
		if lead == nil {
			conflicts = append(conflicts, fmt.Sprintf("ID:%d", item.LeadID))
			continue
		}
		if lead.Version != item.Version || lead.ProfileVersion != item.ProfileVersion {
			conflicts = append(conflicts, lead.LeadNo)
			continue
		}
		leads = append(leads, lead)
	}
	return leads, conflicts, nil
}
