package usecase

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/domain/lead"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/pkg/types"
)

// CreateFollowupUseCase 创建跟进记录用例
type CreateFollowupUseCase struct {
	leadRepo          repo.LeadRepository
	followupRepo      repo.FollowupRepository
	txManager         repo.TransactionManager
	stateMachine      *lead.StateMachine
	flags             runtime.WorkflowFeatureFlags
	settings          runtime.FollowupPolicySettings
	poolRepo          repo.LeadPoolRepository
	reminderRepo      repo.LeadReminderRepository
	idempotencySecret string
}

func NewCreateFollowupUseCase(
	leadRepo repo.LeadRepository,
	followupRepo repo.FollowupRepository,
) *CreateFollowupUseCase {
	defaults := runtime.NewStaticRuntimeSettings(runtime.DefaultWeComConfig())
	return &CreateFollowupUseCase{
		leadRepo:     leadRepo,
		followupRepo: followupRepo,
		stateMachine: lead.NewStateMachine(),
		flags:        defaults,
		settings:     defaults,
	}
}

func (uc *CreateFollowupUseCase) WithStage3Workflow(flags runtime.WorkflowFeatureFlags, settings runtime.FollowupPolicySettings, poolRepo repo.LeadPoolRepository, reminderRepo repo.LeadReminderRepository) *CreateFollowupUseCase {
	if flags != nil {
		uc.flags = flags
	}
	if settings != nil {
		uc.settings = settings
	}
	uc.poolRepo = poolRepo
	uc.reminderRepo = reminderRepo
	return uc
}

func (uc *CreateFollowupUseCase) WithTransactionManager(txManager repo.TransactionManager) *CreateFollowupUseCase {
	uc.txManager = txManager
	return uc
}

func (uc *CreateFollowupUseCase) WithIdempotencySecret(secret string) *CreateFollowupUseCase {
	uc.idempotencySecret = secret
	return uc
}

// CreateFollowupRequest 创建跟进记录请求
type CreateFollowupRequest struct {
	AccountID          int64
	IdempotencyKey     string
	LeadID             int64
	SalesUserID        string
	FollowupResult     string
	IntentionLevel     string
	Remark             string
	NextFollowupAt     *time.Time
	IsValid            bool
	InvalidReason      string
	StarRating         int
	StarRatingProvided bool
	ReturnToPool       bool
}

// Execute 执行创建跟进逻辑
func (uc *CreateFollowupUseCase) Execute(ctx context.Context, req *CreateFollowupRequest) error {
	workflowV2 := uc.flags != nil && uc.flags.WorkflowV2Enabled(ctx)
	poolEnabled := uc.flags != nil && uc.flags.PoolEnabled(ctx)
	followupSettings := snapshotFollowupPolicy(ctx, uc.settings)
	fingerprint := ""
	if req.AccountID > 0 {
		key, err := uuid.Parse(req.IdempotencyKey)
		if err != nil || key.String() != req.IdempotencyKey {
			return types.ErrInvalidIdempotencyKey
		}
		if uc.idempotencySecret == "" || uc.txManager == nil {
			return fmt.Errorf("followup idempotency service unavailable")
		}
		intent := *req
		intent.AccountID = 0
		intent.IdempotencyKey = ""
		intent.InvalidReason = strings.TrimSpace(intent.InvalidReason)
		if intent.NextFollowupAt != nil {
			utc := intent.NextFollowupAt.UTC()
			intent.NextFollowupAt = &utc
		}
		payload, err := json.Marshal(intent)
		if err != nil {
			return err
		}
		mac := hmac.New(sha256.New, []byte(uc.idempotencySecret))
		_, _ = mac.Write(payload)
		fingerprint = hex.EncodeToString(mac.Sum(nil))
	}
	return uc.withTransaction(ctx, func(ctx context.Context, leadRepo repo.LeadRepository, followupRepo repo.FollowupRepository, poolRepo repo.LeadPoolRepository, reminderRepo repo.LeadReminderRepository, requests repo.FollowupRequestRepository) error {
		var receipt *repo.FollowupRequest
		if req.AccountID > 0 {
			if requests == nil {
				return fmt.Errorf("followup request repository unavailable")
			}
			prior, fresh, err := requests.Reserve(ctx, &repo.FollowupRequest{AccountID: req.AccountID, LeadID: req.LeadID, RequestKey: req.IdempotencyKey, Fingerprint: fingerprint, CreatedAt: time.Now().UTC()})
			if err != nil {
				return err
			}
			if !fresh {
				if prior.Fingerprint != fingerprint {
					return types.ErrIdempotencyConflict
				}
				// This receipt acknowledges this authenticated account's own completed
				// operation only; no current lead details are disclosed after a pool return.
				if prior.FollowupID <= 0 {
					return fmt.Errorf("followup request is incomplete")
				}
				return nil
			}
			receipt = prior
		}
		id, err := uc.execute(ctx, req, leadRepo, followupRepo, poolRepo, reminderRepo, workflowV2, poolEnabled, followupSettings)
		if err != nil {
			return err
		}
		if receipt != nil {
			return requests.Complete(ctx, receipt.ID, id)
		}
		return nil
	})
}

func (uc *CreateFollowupUseCase) execute(
	ctx context.Context,
	req *CreateFollowupRequest,
	leadRepo repo.LeadRepository,
	followupRepo repo.FollowupRepository,
	poolRepo repo.LeadPoolRepository,
	reminderRepo repo.LeadReminderRepository,
	workflowV2 bool,
	poolEnabled bool,
	followupSettings runtime.FollowupPolicySettings,
) (int64, error) {
	// 0. 参数校验：标记无效时必须填写原因
	invalidReason := strings.TrimSpace(req.InvalidReason)
	if !req.IsValid && invalidReason == "" {
		return 0, types.ErrInvalidReasonRequired
	}
	followupPolicyEnabled := workflowV2 || poolEnabled

	if req.ReturnToPool && (req.IsValid || !poolEnabled || poolRepo == nil) {
		return 0, types.ErrPublicPoolForbidden
	}

	// 1. 查询线索
	lead, err := leadRepo.GetByID(ctx, req.LeadID)
	if err != nil {
		return 0, err
	}
	if lead == nil {
		return 0, types.ErrLeadNotFound
	}
	starRating := lead.StarRating
	if req.StarRatingProvided || req.StarRating != 0 {
		if !types.IsValidStarRating(req.StarRating) {
			return 0, types.ErrInvalidStarRating
		}
		starRating = req.StarRating
	} else if !types.IsValidStarRating(starRating) {
		starRating = types.LegacyIntentionStarRating(req.IntentionLevel)
		if !types.IsValidStarRating(starRating) {
			starRating = 3
		}
	}
	types.ProjectLegacyWorkflow(lead)
	if types.IsTerminalLeadStatus(lead.Status) {
		return 0, types.ErrLeadStatusInvalid
	}

	// 2. 校验权限（只有负责人可以填写跟进）
	if lead.OwnerUserID != req.SalesUserID {
		return 0, types.ErrUnauthorized
	}
	if lead.BusinessStage != types.BusinessStageInvitation && req.ReturnToPool {
		return 0, types.ErrPublicPoolForbidden
	}

	// 3. 创建跟进记录
	now := time.Now()
	nextFollowupAt := req.NextFollowupAt
	if req.IsValid && followupPolicyEnabled {
		if nextFollowupAt == nil {
			nextFollowupAt = timePointer(now.Add(followupSettings.FollowupDueByStar(ctx, starRating)))
		} else if !nextFollowupAt.After(now) {
			return 0, types.ErrInvalidFollowupDue
		}
	}
	followup := &types.LeadFollowup{
		LeadID:         req.LeadID,
		SalesUserID:    req.SalesUserID,
		FollowupResult: req.FollowupResult,
		IntentionLevel: req.IntentionLevel,
		BusinessStage:  lead.BusinessStage,
		ActorRole:      lead.CurrentOwnerRole,
		StarRating:     starRating,
		DueAtSnapshot:  lead.NextFollowupDueAt,
		Remark:         req.Remark,
		NextFollowupAt: nextFollowupAt,
		IsValid:        req.IsValid,
		InvalidReason:  invalidReason,
		CreatedAt:      now,
	}

	if err := followupRepo.Create(ctx, followup); err != nil {
		return 0, fmt.Errorf("创建跟进记录失败: %w", err)
	}

	// 4. 计算线索状态变化并单次更新，避免同一请求内二次乐观锁冲突。
	needsLeadUpdate := true
	lead.LastFollowupAt = &now
	if lead.CurrentOwnerStartedAt != nil && lead.CurrentOwnerFirstFollowupAt == nil && !now.Before(*lead.CurrentOwnerStartedAt) {
		lead.CurrentOwnerFirstFollowupAt = &now
	}
	lead.NextFollowupDueAt = nextFollowupAt
	if req.IsValid && types.IsValidStarRating(starRating) {
		lead.StarRating = starRating
	}
	isFirstFollowup := lead.FirstFollowupAt == nil
	if isFirstFollowup {
		lead.FirstFollowupAt = &now
		needsLeadUpdate = true
	}

	var poolRecord *types.LeadPoolRecord
	if req.ReturnToPool {
		previousOwner, err := enterLeadIntoPublicPool(lead, types.PoolReasonRecoverableInvalid, now)
		if err != nil {
			return 0, err
		}
		poolRecord = &types.LeadPoolRecord{LeadID: lead.ID, Action: types.PoolActionEnter, Reason: types.PoolReasonRecoverableInvalid, PreviousOwnerUserID: previousOwner, ActorUserID: req.SalesUserID, PoolVersion: lead.Version, CreatedAt: now}
	} else if !req.IsValid {
		nextStatus, err := uc.stateMachine.NextState(lead.Status, types.LeadEventMarkedInvalid)
		if err != nil {
			return 0, err
		}
		lead.Status = nextStatus
		lead.BusinessStage = types.BusinessStageFinished
		lead.ResultStatus = types.LeadResultInvalid
		needsLeadUpdate = true
	} else if lead.Status == types.LeadStatusClaimed {
		nextStatus, err := uc.stateMachine.NextState(lead.Status, types.LeadEventFollowupSubmitted)
		if err != nil {
			return 0, err
		}
		lead.Status = nextStatus
		lead.OwnershipState = types.OwnershipPrivate
		lead.ResultStatus = types.LeadResultActive
		needsLeadUpdate = true
	}

	if needsLeadUpdate {
		if poolRecord == nil {
			lead.Version++
		}
		lead.UpdatedAt = now
		if err := leadRepo.Update(ctx, lead); err != nil {
			return 0, fmt.Errorf("更新线索状态失败: %w", err)
		}
		if poolRecord != nil {
			poolRecord.PoolVersion = lead.Version
			if err := poolRepo.Create(ctx, poolRecord); err != nil {
				return 0, err
			}
		}
		if req.IsValid && followupPolicyEnabled {
			if err := scheduleFollowupReminders(ctx, reminderRepo, followupSettings, lead); err != nil {
				return 0, err
			}
		}
	}

	return followup.ID, nil
}

func (uc *CreateFollowupUseCase) withTransaction(
	ctx context.Context,
	fn func(context.Context, repo.LeadRepository, repo.FollowupRepository, repo.LeadPoolRepository, repo.LeadReminderRepository, repo.FollowupRequestRepository) error,
) error {
	if uc.txManager == nil {
		return fn(ctx, uc.leadRepo, uc.followupRepo, uc.poolRepo, uc.reminderRepo, nil)
	}
	return uc.txManager.WithinTransaction(ctx, func(ctx context.Context, repos repo.TransactionRepositories) error {
		return fn(ctx, repos.LeadRepository(), repos.FollowupRepository(), repos.LeadPoolRepository(), repos.LeadReminderRepository(), repos.FollowupRequests())
	})
}
