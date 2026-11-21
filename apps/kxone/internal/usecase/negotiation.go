package usecase

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/pkg/types"
)

type NegotiationUseCase struct {
	leads      repo.LeadRepository
	bindings   repo.UserBindingRepository
	activities repo.LeadActivityRepository
	txManager  repo.TransactionManager
}

func NewNegotiationUseCase(leads repo.LeadRepository, bindings repo.UserBindingRepository, activities repo.LeadActivityRepository, txManager repo.TransactionManager) *NegotiationUseCase {
	return &NegotiationUseCase{leads: leads, bindings: bindings, activities: activities, txManager: txManager}
}

type ReassignNegotiationCloserRequest struct {
	LeadID          int64
	ActorUserID     string
	ActorRole       string
	CloserUserID    string
	ExpectedVersion int
	Reason          string
}

func (uc *NegotiationUseCase) ReassignCloser(ctx context.Context, req *ReassignNegotiationCloserRequest) error {
	if req == nil || strings.TrimSpace(req.Reason) == "" {
		return types.ErrNegotiationReassignInvalid
	}
	if req.ActorRole != string(types.UserRoleAdmin) && req.ActorRole != string(types.UserRoleManager) {
		return types.ErrUnauthorized
	}
	return uc.withTransaction(ctx, func(ctx context.Context, leads repo.LeadRepository, bindings repo.UserBindingRepository, activities repo.LeadActivityRepository, reminders repo.LeadReminderRepository, settingsRepo repo.SystemSettingRepository) error {
		if err := validateAssignableCloser(ctx, bindings, req.CloserUserID); err != nil {
			return err
		}
		lead, err := leads.GetByID(ctx, req.LeadID)
		if err != nil || lead == nil {
			if err != nil {
				return err
			}
			return types.ErrLeadNotFound
		}
		types.ProjectLegacyWorkflow(lead)
		if lead.BusinessStage != types.BusinessStageNegotiation || lead.ResultStatus != types.LeadResultActive || lead.CurrentOwnerRole != types.OwnerRoleCloser || lead.CloserUserID == "" || lead.CloserUserID == req.CloserUserID {
			return types.ErrNegotiationReassignInvalid
		}
		if lead.Version != req.ExpectedVersion {
			return types.ErrLeadVersionConflict
		}
		now := time.Now()
		oldCloser := lead.CloserUserID
		before, _ := json.Marshal(map[string]any{"closer_userid": oldCloser, "owner_userid": lead.CurrentOwnerUserID})
		lead.CloserUserID = req.CloserUserID
		lead.CurrentOwnerUserID = req.CloserUserID
		lead.CurrentOwnerRole = types.OwnerRoleCloser
		lead.AssignedSalesUserID = req.CloserUserID
		lead.OwnerUserID = req.CloserUserID
		types.BeginOwnerCycle(lead, now)
		lead.Version++
		lead.UpdatedAt = now
		if err := leads.Update(ctx, lead); err != nil {
			return err
		}
		if activities != nil {
			after, _ := json.Marshal(map[string]any{"closer_userid": lead.CloserUserID, "owner_userid": lead.CurrentOwnerUserID})
			metadata, _ := json.Marshal(map[string]any{"reason": strings.TrimSpace(req.Reason), "previous_closer_userid": oldCloser})
			if err := activities.Create(ctx, &types.LeadActivityRecord{LeadID: lead.ID, ActivityType: types.LeadActivityCloserReassigned, ActorUserID: req.ActorUserID, ActorRole: req.ActorRole, FromValueJSON: string(before), ToValueJSON: string(after), MetadataJSON: string(metadata), CreatedAt: now}); err != nil {
				return err
			}
		}
		if settingsRepo != nil {
			return scheduleFollowupReminders(ctx, reminders, snapshotFollowupPolicy(ctx, runtime.NewDatabaseRuntimeSettings(settingsRepo, 0)), lead)
		}
		return nil
	})
}

func (uc *NegotiationUseCase) withTransaction(ctx context.Context, fn func(context.Context, repo.LeadRepository, repo.UserBindingRepository, repo.LeadActivityRepository, repo.LeadReminderRepository, repo.SystemSettingRepository) error) error {
	if uc.txManager == nil {
		return fn(ctx, uc.leads, uc.bindings, uc.activities, nil, nil)
	}
	return uc.txManager.WithinTransaction(ctx, func(ctx context.Context, repos repo.TransactionRepositories) error {
		return fn(ctx, repos.LeadRepository(), repos.UserBindingRepository(), repos.LeadActivityRepository(), repos.LeadReminderRepository(), repos.SystemSettingRepository())
	})
}
