package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/pkg/types"
)

type LeadPoolUseCase struct {
	leadRepo         repo.LeadRepository
	poolRepo         repo.LeadPoolRepository
	userBindingRepo  repo.UserBindingRepository
	txManager        repo.TransactionManager
	flags            runtime.WorkflowFeatureFlags
	settings         runtime.FollowupPolicySettings
	notificationRepo repo.GroupRobotNotificationRepository
}

func NewLeadPoolUseCase(leadRepo repo.LeadRepository, poolRepo repo.LeadPoolRepository, flags runtime.WorkflowFeatureFlags, settings runtime.FollowupPolicySettings) *LeadPoolUseCase {
	return &LeadPoolUseCase{leadRepo: leadRepo, poolRepo: poolRepo, flags: flags, settings: settings}
}

func (uc *LeadPoolUseCase) WithDependencies(userBindingRepo repo.UserBindingRepository, txManager repo.TransactionManager) *LeadPoolUseCase {
	uc.userBindingRepo = userBindingRepo
	uc.txManager = txManager
	return uc
}

func (uc *LeadPoolUseCase) WithGroupRobotNotifications(repository repo.GroupRobotNotificationRepository) *LeadPoolUseCase {
	uc.notificationRepo = repository
	return uc
}

type ListPublicPoolRequest struct {
	CurrentUser string
	Role        string
	Keyword     string
	Page        int
	PageSize    int
}

type ListPublicPoolResponse struct {
	Leads      []*types.Lead `json:"leads"`
	Total      int           `json:"total"`
	Page       int           `json:"page"`
	PageSize   int           `json:"page_size"`
	TotalPages int           `json:"total_pages"`
}

func (uc *LeadPoolUseCase) List(ctx context.Context, req *ListPublicPoolRequest) (*ListPublicPoolResponse, error) {
	role := types.UserRole(strings.TrimSpace(req.Role))
	if role != types.UserRoleAdmin && role != types.UserRoleManager && !types.IsInvitationRole(role) {
		return nil, types.ErrUnauthorized
	}
	poolLeads, ok := uc.leadRepo.(repo.PublicPoolLeadRepository)
	if !ok {
		return nil, types.ErrPublicPoolForbidden
	}
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}
	leads, total, err := poolLeads.ListPublicPoolFiltered(ctx, repo.LeadListFilter{Keyword: strings.TrimSpace(req.Keyword)}, (req.Page-1)*req.PageSize, req.PageSize)
	if err != nil {
		return nil, err
	}
	for _, lead := range leads {
		lead.CustomerMobile = ""
		lead.CustomerWechat = ""
	}
	return &ListPublicPoolResponse{Leads: leads, Total: total, Page: req.Page, PageSize: req.PageSize, TotalPages: (total + req.PageSize - 1) / req.PageSize}, nil
}

func (uc *LeadPoolUseCase) Enter(ctx context.Context, leadID int64, actorUserID, actorRole, reason string) error {
	if uc.flags == nil || !uc.flags.PoolEnabled(ctx) {
		return types.ErrPublicPoolForbidden
	}
	if actorRole != string(types.UserRoleAdmin) && actorRole != string(types.UserRoleManager) && actorRole != "system" {
		return types.ErrUnauthorized
	}
	if reason == "" {
		reason = types.PoolReasonManual
	}
	if !validPoolReason(reason) {
		return types.ErrPublicPoolForbidden
	}
	followupSettings := snapshotFollowupPolicy(ctx, uc.settings)
	return uc.withTransaction(ctx, func(ctx context.Context, leadRepo repo.LeadRepository, poolRepo repo.LeadPoolRepository, _ repo.LeadReminderRepository) error {
		lead, err := leadRepo.GetByID(ctx, leadID)
		if err != nil || lead == nil {
			if err != nil {
				return err
			}
			return types.ErrLeadNotFound
		}
		now := time.Now()
		if !eligibleForPoolReason(ctx, followupSettings, lead, reason, now, actorRole) {
			return types.ErrPublicPoolForbidden
		}
		previousOwner, err := enterLeadIntoPublicPool(lead, reason, now)
		if err != nil {
			return err
		}
		if err := leadRepo.Update(ctx, lead); err != nil {
			return err
		}
		return poolRepo.Create(ctx, &types.LeadPoolRecord{LeadID: lead.ID, Action: types.PoolActionEnter, Reason: reason, PreviousOwnerUserID: previousOwner, ActorUserID: actorUserID, PoolVersion: lead.Version, CreatedAt: now})
	})
}

func (uc *LeadPoolUseCase) Claim(ctx context.Context, leadID int64, inviterUserID, role string) error {
	actorRole := types.UserRole(role)
	if !types.IsInvitationRole(actorRole) && actorRole != types.UserRoleManager {
		return types.ErrUnauthorized
	}
	if err := validateAssignmentTarget(ctx, uc.userBindingRepo, inviterUserID, inviterUserID, role); err != nil {
		return err
	}
	followupSettings := snapshotFollowupPolicy(ctx, uc.settings)
	return uc.withTransaction(ctx, func(ctx context.Context, leadRepo repo.LeadRepository, poolRepo repo.LeadPoolRepository, reminderRepo repo.LeadReminderRepository) error {
		poolLeads, ok := leadRepo.(repo.PublicPoolLeadRepository)
		if !ok {
			return types.ErrPublicPoolForbidden
		}
		count, err := poolLeads.CountActiveInvitationByInviter(ctx, inviterUserID)
		if err != nil {
			return err
		}
		if count >= followupSettings.PoolMaxActiveLeads(ctx) {
			return types.ErrPublicPoolCapacity
		}
		lead, err := leadRepo.GetByID(ctx, leadID)
		if err != nil || lead == nil {
			if err != nil {
				return err
			}
			return types.ErrLeadNotFound
		}
		latest, err := poolRepo.GetLatestByLead(ctx, leadID)
		if err != nil {
			return err
		}
		now := time.Now()
		if latest != nil && latest.Action == types.PoolActionEnter && latest.PreviousOwnerUserID == inviterUserID && now.Before(latest.CreatedAt.Add(followupSettings.PoolCooldown(ctx))) {
			return types.ErrPublicPoolCooldown
		}
		if err := claimLeadFromPublicPool(lead, inviterUserID, now); err != nil {
			return err
		}
		lead.NextFollowupDueAt = timePointer(now.Add(followupSettings.FirstFollowupDue(ctx)))
		if err := leadRepo.Update(ctx, lead); err != nil {
			return err
		}
		if err := poolRepo.Create(ctx, &types.LeadPoolRecord{LeadID: lead.ID, Action: types.PoolActionClaim, ActorUserID: inviterUserID, PoolVersion: lead.Version, CreatedAt: now}); err != nil {
			return err
		}
		return scheduleFollowupReminders(ctx, reminderRepo, followupSettings, lead)
	})
}

func (uc *LeadPoolUseCase) AutoReturnOverdue(ctx context.Context, now time.Time, limit int) error {
	if uc.flags == nil || !uc.flags.PoolEnabled(ctx) || !uc.flags.PoolAutoReturnEnabled(ctx) {
		return nil
	}
	poolLeads, ok := uc.leadRepo.(repo.PublicPoolLeadRepository)
	if !ok {
		return types.ErrPublicPoolForbidden
	}
	candidates, err := poolLeads.ListAutoPoolCandidates(ctx, now.Add(-uc.settings.PoolUnclaimedTimeout(ctx)), now.Add(-uc.settings.PoolFollowupGrace(ctx)), limit)
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		reason := types.PoolReasonFollowupOverdue
		if candidate.OwnershipState == types.OwnershipAssignedPendingClaim {
			reason = types.PoolReasonUnclaimedTimeout
		}
		if err := uc.Enter(ctx, candidate.ID, "system", "system", reason); err != nil {
			if err != types.ErrLeadVersionConflict && err != types.ErrPublicPoolForbidden {
				return err
			}
			continue
		}
		if uc.notificationRepo != nil {
			returnedLead, lookupErr := uc.leadRepo.GetByID(ctx, candidate.ID)
			if lookupErr != nil || returnedLead == nil {
				slog.Error("read automatic public-pool result for notification failed", "lead_id", candidate.ID, "err", lookupErr)
				continue
			}
			previousOwner := firstNonEmpty(
				candidate.CurrentOwnerUserID,
				candidate.AssignedInviterUserID,
				candidate.InviterUserID,
				candidate.AssignedSalesUserID,
				candidate.OwnerUserID,
			)
			if latest, latestErr := uc.poolRepo.GetLatestByLead(ctx, candidate.ID); latestErr != nil {
				slog.Warn("read automatic public-pool record for notification failed", "lead_id", candidate.ID, "err", latestErr)
			} else if latest != nil && latest.Action == types.PoolActionEnter {
				previousOwner = firstNonEmpty(latest.PreviousOwnerUserID, previousOwner)
			}
			eventAt := now
			if returnedLead.PoolEnteredAt != nil {
				eventAt = *returnedLead.PoolEnteredAt
			}
			payload, _ := json.Marshal(map[string]string{
				"reason": reason, "previous_owner_user_id": previousOwner,
			})
			_, err := uc.notificationRepo.Create(ctx, &types.GroupRobotNotification{
				EventKey: fmt.Sprintf("pool-auto-return:%d:v%d", candidate.ID, returnedLead.Version),
				Category: types.GroupRobotCategoryPool, EventType: types.GroupRobotEventPoolAutoReturn,
				LeadID: candidate.ID, BusinessVersion: returnedLead.Version, DueAt: eventAt,
				Status: types.GroupRobotNotificationPending, PayloadJSON: string(payload), CreatedAt: eventAt, UpdatedAt: eventAt,
			})
			if err != nil {
				slog.Error("record automatic public-pool notification failed", "lead_id", candidate.ID, "err", err)
			}
		}
	}
	return nil
}

func (uc *LeadPoolUseCase) withTransaction(ctx context.Context, fn func(context.Context, repo.LeadRepository, repo.LeadPoolRepository, repo.LeadReminderRepository) error) error {
	if uc.txManager == nil {
		return fn(ctx, uc.leadRepo, uc.poolRepo, nil)
	}
	return uc.txManager.WithinTransaction(ctx, func(ctx context.Context, repos repo.TransactionRepositories) error {
		return fn(ctx, repos.LeadRepository(), repos.LeadPoolRepository(), repos.LeadReminderRepository())
	})
}

func timePointer(value time.Time) *time.Time { return &value }

func validPoolReason(reason string) bool {
	return reason == types.PoolReasonManual || reason == types.PoolReasonUnclaimedTimeout || reason == types.PoolReasonFollowupOverdue || reason == types.PoolReasonRecoverableInvalid
}

func eligibleForPoolReason(ctx context.Context, settings runtime.FollowupPolicySettings, lead *types.Lead, reason string, now time.Time, actorRole string) bool {
	if lead == nil {
		return false
	}
	switch reason {
	case types.PoolReasonManual:
		return true
	case types.PoolReasonUnclaimedTimeout:
		return lead.OwnershipState == types.OwnershipAssignedPendingClaim && lead.InviterAssignedAt != nil && !lead.InviterAssignedAt.Add(settings.PoolUnclaimedTimeout(ctx)).After(now)
	case types.PoolReasonFollowupOverdue:
		return lead.OwnershipState == types.OwnershipPrivate && lead.NextFollowupDueAt != nil && !lead.NextFollowupDueAt.Add(settings.PoolFollowupGrace(ctx)).After(now)
	case types.PoolReasonRecoverableInvalid:
		return actorRole == "system"
	default:
		return false
	}
}
