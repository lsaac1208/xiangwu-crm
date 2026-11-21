package usecase

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/domain/lead"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/pkg/types"
)

type CompleteLeadUseCase struct {
	leadRepo     repo.LeadRepository
	stateMachine *lead.StateMachine
	flags        runtime.WorkflowFeatureFlags
	activities   repo.LeadActivityRepository
	txManager    repo.TransactionManager
}

func (uc *CompleteLeadUseCase) WithAudit(activities repo.LeadActivityRepository, txManager repo.TransactionManager) *CompleteLeadUseCase {
	uc.activities = activities
	uc.txManager = txManager
	return uc
}

func (uc *CompleteLeadUseCase) WithWorkflow(flags runtime.WorkflowFeatureFlags) *CompleteLeadUseCase {
	uc.flags = flags
	return uc
}

const maxCloseReasonLength = 255

func NewCompleteLeadUseCase(leadRepo repo.LeadRepository) *CompleteLeadUseCase {
	return &CompleteLeadUseCase{
		leadRepo:     leadRepo,
		stateMachine: lead.NewStateMachine(),
	}
}

type CompleteLeadRequest struct {
	LeadID          int64
	ActorUserID     string
	ActorRole       string
	Action          string
	DealAmount      float64
	CloseReason     string
	PaymentStatus   string
	RefundAmount    float64
	ExpectedVersion *int
	Reason          string
}

func (uc *CompleteLeadUseCase) Execute(ctx context.Context, req *CompleteLeadRequest) (*types.Lead, error) {
	if req == nil {
		return nil, types.ErrLeadStatusInvalid
	}
	action := strings.TrimSpace(req.Action)
	var completed *types.Lead
	err := uc.withTransaction(ctx, func(ctx context.Context, leads repo.LeadRepository, activities repo.LeadActivityRepository) error {
		current, err := leads.GetByID(ctx, req.LeadID)
		if err != nil {
			return err
		}
		if current == nil {
			return types.ErrLeadNotFound
		}
		types.ProjectLegacyWorkflow(current)
		workflowV2 := uc.flags != nil && uc.flags.WorkflowV2Enabled(ctx)
		if !canCompleteLead(current, req.ActorUserID, req.ActorRole, workflowV2, action) {
			return types.ErrUnauthorized
		}

		if req.ExpectedVersion != nil && *req.ExpectedVersion != current.Version {
			return types.ErrLeadVersionConflict
		}
		if action == "update_deal" || action == "restore_invalid" {
			if req.ExpectedVersion == nil || strings.TrimSpace(req.Reason) == "" || len([]rune(req.Reason)) > maxCloseReasonLength {
				return types.ErrLeadStatusInvalid
			}
		}
		before := completionAuditValues(current)
		now := time.Now()
		previousStage := correctionStage(current)

		switch action {
		case "convert":
			if err := applyPayment(current, req); err != nil {
				return err
			}
			nextStatus, err := uc.stateMachine.NextState(current.Status, types.LeadEventConverted)
			if err != nil {
				return err
			}
			current.Status = nextStatus
			current.BusinessStage = types.BusinessStageFinished
			current.ResultStatus = types.LeadResultConverted
			current.ConvertedAt = &now
			current.ClosedAt = nil
			current.DealAmount = req.DealAmount
			current.CloseReason = ""
		case "update_deal":
			if current.Status != types.LeadStatusConverted {
				return types.ErrLeadStatusInvalid
			}
			if err := applyPayment(current, req); err != nil {
				return err
			}
		case "restore_invalid":
			if current.Status != types.LeadStatusInvalid {
				return types.ErrLeadStatusInvalid
			}
			current.BusinessStage = correctionStage(current)
			current.Status = types.LeadStatusFollowing
			current.ResultStatus = types.LeadResultActive
			current.ClosedAt = nil
			current.CloseReason = ""
			if current.CurrentOwnerUserID == "" {
				current.BusinessStage = types.BusinessStageInvitation
				current.Status = types.LeadStatusPendingDispatch
				current.OwnershipState = types.OwnershipPendingAssignment
			}
		case "close":
			closeReason := strings.TrimSpace(req.CloseReason)
			if closeReason == "" || len([]rune(closeReason)) > maxCloseReasonLength {
				return types.ErrLeadStatusInvalid
			}
			nextStatus, err := uc.stateMachine.NextState(current.Status, types.LeadEventClosed)
			if err != nil {
				return err
			}
			current.Status = nextStatus
			current.BusinessStage = types.BusinessStageFinished
			current.ResultStatus = types.LeadResultClosed
			current.ClosedAt = &now
			current.CloseReason = closeReason
		default:
			return types.ErrLeadStatusInvalid
		}

		current.NextFollowupDueAt = nil
		current.Version++
		current.UpdatedAt = now
		if err := leads.Update(ctx, current); err != nil {
			return err
		}
		if activities != nil {
			activityType := types.LeadActivityNegotiationConverted
			conversionPath := "negotiation"
			if previousStage == types.BusinessStageInvitation {
				activityType = types.LeadActivityInvitationDirectConverted
				conversionPath = "invitation_direct"
			}
			switch action {
			case "update_deal":
				activityType = "deal_updated"
			case "restore_invalid":
				activityType = "invalid_restored"
			case "close":
				activityType = types.LeadActivityNegotiationClosed
			}
			beforeJSON, _ := json.Marshal(before)
			afterJSON, _ := json.Marshal(completionAuditValues(current))
			metadataJSON, _ := json.Marshal(map[string]any{"conversion_path": conversionPath, "deal_amount": current.DealAmount, "payment_status": current.PaymentStatus, "refund_amount": current.RefundAmount, "product_name": current.ProductName, "reason": strings.TrimSpace(req.Reason)})
			if err := activities.Create(ctx, &types.LeadActivityRecord{LeadID: current.ID, ActivityType: activityType, ActorUserID: strings.TrimSpace(req.ActorUserID), ActorRole: strings.TrimSpace(req.ActorRole), FromValueJSON: string(beforeJSON), ToValueJSON: string(afterJSON), MetadataJSON: string(metadataJSON), CreatedAt: now}); err != nil {
				return err
			}
		}
		completed = current
		return nil
	})
	if err != nil {
		return nil, err
	}
	return completed, nil
}

func (uc *CompleteLeadUseCase) withTransaction(ctx context.Context, fn func(context.Context, repo.LeadRepository, repo.LeadActivityRepository) error) error {
	if uc.txManager == nil {
		return fn(ctx, uc.leadRepo, uc.activities)
	}
	return uc.txManager.WithinTransaction(ctx, func(ctx context.Context, repos repo.TransactionRepositories) error {
		return fn(ctx, repos.LeadRepository(), repos.LeadActivityRepository())
	})
}

func canCompleteLead(lead *types.Lead, actorUserID, actorRole string, workflowV2 bool, action string) bool {
	if strings.TrimSpace(actorUserID) == "" {
		return false
	}
	if action == "update_deal" || action == "restore_invalid" {
		return canCorrectLead(lead, actorUserID, types.UserRole(actorRole))
	}
	if actorRole == string(types.UserRoleAdmin) {
		return true
	}
	// Conversion permissions follow the lead's projected business stage even
	// when the dual-stage feature flag is off. This keeps the single-stage CRM
	// path explicit and gives managers the same conversion ability as the UI.
	if action == "convert" {
		if lead.ResultStatus != types.LeadResultActive || (lead.Status != types.LeadStatusClaimed && lead.Status != types.LeadStatusFollowing) {
			return false
		}
		if actorRole == string(types.UserRoleManager) {
			return true
		}
		if lead.BusinessStage == types.BusinessStageInvitation && types.IsInvitationRole(types.UserRole(actorRole)) {
			return lead.CurrentOwnerUserID == actorUserID || lead.InviterUserID == actorUserID || lead.OwnerUserID == actorUserID
		}
		if lead.BusinessStage == types.BusinessStageNegotiation && actorRole == string(types.UserRoleCloser) {
			return lead.CurrentOwnerUserID == actorUserID || lead.CloserUserID == actorUserID
		}
		return false
	}
	if workflowV2 && lead.BusinessStage != types.BusinessStageNegotiation {
		return false
	}
	if actorRole == string(types.UserRoleManager) && lead.DispatchManagerUserID == actorUserID {
		return true
	}
	if actorRole == string(types.UserRoleCloser) && lead.BusinessStage == types.BusinessStageNegotiation && lead.CurrentOwnerRole == types.OwnerRoleCloser && lead.CurrentOwnerUserID == actorUserID {
		return true
	}
	if workflowV2 {
		return false
	}
	if types.IsInvitationRole(types.UserRole(actorRole)) && (lead.OwnerUserID == actorUserID || lead.AssignedSalesUserID == actorUserID) {
		return true
	}
	return false
}

// Terminal outcomes retain ownership; corrections follow the owner of the last business stage.
func correctionStage(current *types.Lead) types.BusinessStage {
	if current.CurrentOwnerRole == types.OwnerRoleCloser || current.DeliveredAt != nil || current.BusinessStage == types.BusinessStageNegotiation {
		return types.BusinessStageNegotiation
	}
	return types.BusinessStageInvitation
}

func canCorrectLead(current *types.Lead, actor string, role types.UserRole) bool {
	if strings.TrimSpace(actor) == "" {
		return false
	}
	if role == types.UserRoleAdmin || role == types.UserRoleManager {
		return true
	}
	if correctionStage(current) == types.BusinessStageNegotiation {
		return role == types.UserRoleCloser && (current.CurrentOwnerUserID == actor || current.CloserUserID == actor)
	}
	return types.IsInvitationRole(role) && (current.CurrentOwnerUserID == actor || current.InviterUserID == actor || current.OwnerUserID == actor)
}

func validPaymentAmount(amount float64) bool {
	return !math.IsNaN(amount) && !math.IsInf(amount, 0) && amount >= 0 && amount <= 999999999.99 && math.Abs(amount*100-math.Round(amount*100)) < 0.0001
}

func applyPayment(current *types.Lead, req *CompleteLeadRequest) error {
	status := strings.TrimSpace(req.PaymentStatus)
	// Older clients may omit payment status. Keep it unknown rather than inventing full payment.
	if status != "" && status != "partial" && status != "paid" && status != "refunded" {
		return types.ErrLeadStatusInvalid
	}
	if req.Action == "update_deal" && status == "" {
		return types.ErrLeadStatusInvalid
	}
	if !validPaymentAmount(req.DealAmount) || req.DealAmount <= 0 || !validPaymentAmount(req.RefundAmount) {
		return types.ErrLeadStatusInvalid
	}
	if status == "refunded" {
		if req.RefundAmount <= 0 || req.RefundAmount > req.DealAmount {
			return types.ErrLeadStatusInvalid
		}
	} else if req.RefundAmount != 0 {
		return types.ErrLeadStatusInvalid
	}
	current.DealAmount = req.DealAmount
	current.PaymentStatus = status
	current.RefundAmount = req.RefundAmount
	return nil
}

func completionAuditValues(current *types.Lead) map[string]any {
	return map[string]any{"business_stage": current.BusinessStage, "status": current.Status, "result_status": current.ResultStatus, "version": current.Version, "deal_amount": current.DealAmount, "payment_status": current.PaymentStatus, "refund_amount": current.RefundAmount, "next_followup_due_at": current.NextFollowupDueAt, "close_reason": current.CloseReason}
}
