package usecase

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/notification"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/pkg/types"
)

// CreateLeadUseCase 创建线索并进入主管派发流程。
type CreateLeadUseCase struct {
	idempotencySecret string
	leadRepo          repo.LeadRepository
	cardRepo          repo.CardTaskRepository
	taskQueue         TaskQueue
	recipientRepo     notification.BindingLister
	runtimeSettings   runtime.RuntimeSettings
	followupSettings  runtime.FollowupPolicySettings
	userBindingRepo   repo.UserBindingRepository
	dispatchRepo      repo.DispatchRecordRepository
	reminderRepo      repo.LeadReminderRepository
	txManager         repo.TransactionManager
}

func (uc *CreateLeadUseCase) WithIdempotencySecret(key string) *CreateLeadUseCase {
	uc.idempotencySecret = key
	return uc
}

func (uc *CreateLeadUseCase) WithRoleAwareCreation(bindings repo.UserBindingRepository, dispatches repo.DispatchRecordRepository, reminders repo.LeadReminderRepository, txManager repo.TransactionManager) *CreateLeadUseCase {
	uc.userBindingRepo = bindings
	uc.dispatchRepo = dispatches
	uc.reminderRepo = reminders
	uc.txManager = txManager
	return uc
}

func NewCreateLeadUseCase(
	leadRepo repo.LeadRepository,
	taskQueue TaskQueue,
) *CreateLeadUseCase {
	defaults := runtime.NewStaticRuntimeSettings(runtime.DefaultWeComConfig())
	return &CreateLeadUseCase{
		leadRepo:         leadRepo,
		taskQueue:        taskQueue,
		runtimeSettings:  defaults,
		followupSettings: defaults,
	}
}

func (uc *CreateLeadUseCase) WithRuntimeSettings(settings runtime.RuntimeSettings) *CreateLeadUseCase {
	if settings != nil {
		uc.runtimeSettings = settings
		if followups, ok := settings.(runtime.FollowupPolicySettings); ok {
			uc.followupSettings = followups
		}
	}
	return uc
}

func (uc *CreateLeadUseCase) WithCardTaskRepository(cardRepo repo.CardTaskRepository) *CreateLeadUseCase {
	uc.cardRepo = cardRepo
	return uc
}

func (uc *CreateLeadUseCase) WithNotificationRecipientRepository(repository notification.BindingLister) *CreateLeadUseCase {
	uc.recipientRepo = repository
	return uc
}

// CreateLeadRequest 创建线索请求。
type CreateLeadRequest struct {
	AccountID             int64
	IdempotencyKey        string
	XiangwuFormSubmission *repo.XiangwuFormSubmission
	LeadNo                string
	CustomerName          string
	CustomerMobile        string
	CustomerMobileMasked  string
	CustomerWechat        string
	CustomerWechatMasked  string
	City                  string
	ProductName           string
	SourceChannel         string
	SourceCampaign        string
	ContentEntryID        *int64
	ContentRevisionID     *int64
	ContentPath           string
	LeadLevel             string
	StarRating            int
	DispatchManagerUserID string
	ActorUserID           string
	ActorRole             string
	AssignedInviterUserID string
}

// CreateLeadResponse 创建线索响应。
type CreateLeadResponse struct {
	Warnings []string    `json:"warnings,omitempty"`
	Replayed bool        `json:"replayed,omitempty"`
	Lead     *types.Lead `json:"lead"`
	Outcome  string      `json:"outcome"`
}

// Execute 创建线索并异步发送主管待分配卡片。
func (uc *CreateLeadUseCase) Execute(ctx context.Context, req *CreateLeadRequest) (*CreateLeadResponse, error) {
	return uc.execute(ctx, req, false)
}

// ExecuteManual validates the current manual-source list after checking for an
// existing idempotent request, so an older successful request can be replayed.
func (uc *CreateLeadUseCase) ExecuteManual(ctx context.Context, req *CreateLeadRequest) (*CreateLeadResponse, error) {
	return uc.execute(ctx, req, true)
}

func (uc *CreateLeadUseCase) execute(ctx context.Context, req *CreateLeadRequest, manual bool) (*CreateLeadResponse, error) {
	if req.DispatchManagerUserID == "" && req.ActorUserID == "" {
		return nil, types.ErrUnauthorized
	}
	actorRole := types.UserRole(strings.TrimSpace(req.ActorRole))
	roleAware := strings.TrimSpace(req.ActorUserID) != ""
	if roleAware && actorRole != types.UserRoleAdmin && actorRole != types.UserRoleManager && actorRole != types.UserRoleDispatcher && actorRole != types.UserRoleSales && actorRole != types.UserRoleInviter && actorRole != types.UserRoleCloser {
		return nil, types.ErrUnauthorized
	}
	if roleAware && actorRole == types.UserRoleCloser {
		if strings.TrimSpace(req.AssignedInviterUserID) == "" {
			return nil, types.ErrInvalidInviter
		}
		if err := validateAssignableInviter(ctx, uc.userBindingRepo, req.AssignedInviterUserID); err != nil {
			return nil, err
		}
	}
	if req.StarRating != 0 && !types.IsValidStarRating(req.StarRating) {
		return nil, types.ErrInvalidStarRating
	}

	if err := validateLeadIntake(req, !roleAware); err != nil {
		return nil, err
	}

	var fingerprint string
	if req.AccountID > 0 {
		key, err := uuid.Parse(req.IdempotencyKey)
		if err != nil || key.String() != req.IdempotencyKey {
			return nil, types.ErrInvalidIdempotencyKey
		}
		if uc.idempotencySecret == "" || uc.txManager == nil {
			return nil, fmt.Errorf("lead creation idempotency service unavailable")
		}
		intent := *req
		intent.AccountID = 0
		intent.IdempotencyKey = ""
		intent.ActorUserID = ""
		intent.ActorRole = ""
		intent.DispatchManagerUserID = ""
		data, err := json.Marshal(intent)
		if err != nil {
			return nil, err
		}
		mac := hmac.New(sha256.New, []byte(uc.idempotencySecret))
		mac.Write([]byte("crm-lead-create-v1\x00"))
		mac.Write(data)
		fingerprint = hex.EncodeToString(mac.Sum(nil))
	}
	now := time.Now()
	followupSettings := snapshotFollowupPolicy(ctx, uc.followupSettings)
	leadNo := req.LeadNo
	if leadNo == "" {
		// 使用 UUID 避免高并发下 UnixNano 重复触发 unique 约束冲突。
		leadNo = "L-" + strings.ReplaceAll(uuid.New().String(), "-", "")
	}

	// Masked contact fields are derived server-side. Never trust a caller's
	// pre-masked value: a dispatcher must not be able to smuggle the raw
	// contact into the read-only masked fields.
	maskedMobile := maskMobile(firstNonEmpty(req.CustomerMobile, req.CustomerMobileMasked))
	maskedWechat := maskWechat(firstNonEmpty(req.CustomerWechat, req.CustomerWechatMasked))
	dispatchManagerUserID := firstNonEmpty(req.ActorUserID, req.DispatchManagerUserID)
	if actorRole == types.UserRoleDispatcher {
		// A dispatcher creates and routes the lead but does not become its
		// business supervisor. The immutable actor ID remains in CreatedByUserID.
		dispatchManagerUserID = strings.TrimSpace(req.DispatchManagerUserID)
		if dispatchManagerUserID == strings.TrimSpace(req.ActorUserID) {
			dispatchManagerUserID = ""
		}
	}

	lead := &types.Lead{
		LeadNo:                leadNo,
		CustomerName:          req.CustomerName,
		CustomerMobile:        req.CustomerMobile,
		CustomerMobileMasked:  maskedMobile,
		CustomerWechat:        req.CustomerWechat,
		CustomerWechatMasked:  maskedWechat,
		City:                  req.City,
		ProductName:           req.ProductName,
		SourceChannel:         req.SourceChannel,
		SourceCampaign:        req.SourceCampaign,
		ContentEntryID:        req.ContentEntryID,
		ContentRevisionID:     req.ContentRevisionID,
		ContentPath:           req.ContentPath,
		LeadLevel:             req.LeadLevel,
		StarRating:            req.StarRating,
		Status:                types.LeadStatusPendingDispatch,
		Version:               0,
		DispatchManagerUserID: dispatchManagerUserID,
		CreatedByUserID:       req.ActorUserID,
		CreatedByRole:         actorRole,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	outcome := "pending_dispatch"
	if roleAware && types.IsInvitationRole(actorRole) {
		outcome = "self_following"
		lead.Status = types.LeadStatusFollowing
		lead.AssignedSalesUserID, lead.AssignedInviterUserID = req.ActorUserID, req.ActorUserID
		lead.OwnerUserID, lead.InviterUserID = req.ActorUserID, req.ActorUserID
		lead.CurrentOwnerUserID, lead.CurrentOwnerRole = req.ActorUserID, types.OwnerRoleInviter
		lead.OwnershipState = types.OwnershipPrivate
		lead.AssignedAt, lead.InviterAssignedAt = &now, &now
		lead.ClaimedAt, lead.InviterClaimedAt = &now, &now
		types.BeginOwnerCycle(lead, now)
		lead.NextFollowupDueAt = timePointer(now.Add(followupSettings.FirstFollowupDue(ctx)))
	} else if roleAware && actorRole == types.UserRoleCloser {
		outcome = "assigned_pending_claim"
		lead.Status = types.LeadStatusAssignedPendingClaim
		lead.AssignedSalesUserID = strings.TrimSpace(req.AssignedInviterUserID)
		lead.AssignedInviterUserID = lead.AssignedSalesUserID
		lead.OwnershipState = types.OwnershipAssignedPendingClaim
		lead.AssignedAt, lead.InviterAssignedAt = &now, &now
	}
	types.ProjectLegacyWorkflow(lead)

	create := func(ctx context.Context, leadRepo repo.LeadRepository, dispatchRepo repo.DispatchRecordRepository, reminderRepo repo.LeadReminderRepository, formRepo repo.XiangwuFormSubmissionRepository) error {
		if manual && !types.IsManualLeadSource(req.SourceChannel) {
			return types.ErrInvalidManualLeadSource
		}
		if err := leadRepo.Create(ctx, lead); err != nil {
			return err
		}
		if req.XiangwuFormSubmission != nil {
			if formRepo == nil {
				return fmt.Errorf("xiangwu form repository unavailable")
			}
			req.XiangwuFormSubmission.LeadID = lead.ID
			if err := formRepo.Create(ctx, req.XiangwuFormSubmission); err != nil {
				return err
			}
		}
		if roleAware && dispatchRepo != nil && outcome != "pending_dispatch" {
			action, remark := "assign", "谈单师创建线索并直接分配邀约师"
			if outcome == "self_following" {
				action, remark = "self_create", "创建线索并由本人直接跟进"
			}
			if err := dispatchRepo.Create(ctx, &types.LeadDispatchRecord{LeadID: lead.ID, ManagerUserID: req.ActorUserID, SalesUserID: lead.AssignedInviterUserID, ActionType: action, Remark: remark, CreatedAt: now}); err != nil {
				return err
			}
		}
		if outcome == "self_following" {
			return scheduleFollowupReminders(ctx, reminderRepo, followupSettings, lead)
		}
		return nil
	}
	var replay *CreateLeadResponse
	var err error
	if uc.txManager != nil {
		err = uc.txManager.WithinTransaction(ctx, func(ctx context.Context, repos repo.TransactionRepositories) error {
			var requestRepo repo.LeadCreateRequestRepository
			var requestRecord *repo.LeadCreateRequest
			if req.AccountID > 0 {
				requestRepo = repos.LeadCreateRequests()
				if requestRepo == nil {
					return fmt.Errorf("lead creation request repository unavailable")
				}
				prior, fresh, err := requestRepo.Reserve(ctx, &repo.LeadCreateRequest{AccountID: req.AccountID, Endpoint: "POST /api/leads", RequestKey: req.IdempotencyKey, Fingerprint: fingerprint, CreatedAt: now.UTC()})
				if err != nil {
					return err
				}
				if !fresh {
					if prior.Fingerprint != fingerprint {
						return types.ErrIdempotencyConflict
					}
					// Read through the same ACL and contact masking as normal detail.
					view, err := NewGetLeadUseCase(repos.LeadRepository(), repos.FollowupRepository(), repos.DispatchRecordRepository()).WithUserBindingRepository(repos.UserBindingRepository()).Execute(ctx, prior.LeadID, req.ActorUserID, req.ActorRole)
					if err != nil {
						return err
					}
					replay = &CreateLeadResponse{Lead: view.Lead, Outcome: prior.Outcome, Replayed: true}
					return nil
				}
				requestRecord = prior
			}
			if err := create(ctx, repos.LeadRepository(), repos.DispatchRecordRepository(), repos.LeadReminderRepository(), repos.XiangwuFormSubmissionRepository()); err != nil {
				return err
			}
			if requestRecord != nil {
				return requestRepo.Complete(ctx, requestRecord.ID, lead.ID, outcome)
			}
			return nil
		})
	} else {
		if req.XiangwuFormSubmission != nil {
			return nil, fmt.Errorf("xiangwu form requires transaction manager")
		}
		err = create(ctx, uc.leadRepo, uc.dispatchRepo, uc.reminderRepo, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("创建线索失败: %w", err)
	}

	if replay != nil {
		return replay, nil
	}
	if uc.taskQueue != nil {
		recipients, err := notification.ActiveAdminManagerUserIDs(ctx, uc.recipientRepo)
		if err != nil {
			logNonBlockingError("查询新线索通知接收人", lead.ID, err)
		}
		if len(recipients) == 0 {
			recipients = []string{lead.DispatchManagerUserID}
		}
		supervisorScene := types.CardSceneSupervisorPendingDispatch
		if outcome == "self_following" {
			supervisorScene = types.CardSceneSupervisorClaimed
		} else if outcome == "assigned_pending_claim" {
			supervisorScene = types.CardSceneSupervisorWaitingClaim
		}
		for _, receiverUserID := range recipients {
			if err := uc.taskQueue.EnqueueSendCard(ctx, lead.ID, receiverUserID, string(supervisorScene)); err != nil {
				logNonBlockingError("提交主管待分配卡片任务", lead.ID, err)
				recordFailedCardSend(ctx, uc.cardRepo, lead.ID, receiverUserID, supervisorScene, err)
			}
		}
		if outcome == "pending_dispatch" {
			for _, delay := range uc.runtimeSettings.DispatchReminderDelays(ctx) {
				if err := uc.taskQueue.SchedulePendingDispatchReminder(ctx, lead.ID, delay); err != nil {
					logNonBlockingError("调度派发超时提醒", lead.ID, err)
				}
			}
		} else if outcome == "assigned_pending_claim" {
			if err := uc.taskQueue.EnqueueSendCard(ctx, lead.ID, lead.AssignedInviterUserID, string(types.CardSceneSalesPendingClaim)); err != nil {
				logNonBlockingError("提交邀约师待领取卡片任务", lead.ID, err)
			}
			for _, delay := range uc.runtimeSettings.ClaimReminderDelays(ctx) {
				if err := uc.taskQueue.SchedulePendingClaimReminder(ctx, lead.ID, delay); err != nil {
					logNonBlockingError("调度领取超时提醒", lead.ID, err)
				}
			}
		}
	}

	if actorRole == types.UserRoleDispatcher {
		applyLeadMobileVisibility(lead, req.ActorUserID, req.ActorRole)
	}
	return &CreateLeadResponse{Lead: lead, Outcome: outcome}, nil
}

func maskMobile(mobile string) string {
	mobile = strings.TrimSpace(mobile)
	if mobile == "" {
		return ""
	}
	runes := []rune(mobile)
	if len(runes) < 7 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:3]) + "****" + string(runes[len(runes)-4:])
}

func maskWechat(wechat string) string {
	wechat = strings.TrimSpace(wechat)
	if wechat == "" {
		return ""
	}
	runes := []rune(wechat)
	if len(runes) <= 4 {
		return strings.Repeat("*", len(runes))
	}
	visible := 2
	if len(runes) < 8 {
		visible = 1
	}
	return string(runes[:visible]) + "****" + string(runes[len(runes)-visible:])
}
