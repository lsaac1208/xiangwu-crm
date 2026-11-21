package usecase

import (
	"context"
	"encoding/json"
	"maps"
	"strings"
	"time"
	"unicode"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/security"
	"github.com/lsaac128/kxone/pkg/types"
)

const LeadProfileWarningDuplicateMobile = "duplicate_mobile"

type UpdateLeadProfileRequest struct {
	LeadID                  int64
	ActorUserID             string
	ActorRole               string
	ExpectedBusinessVersion int
	ExpectedProfileVersion  int
	CustomerName            *string
	CustomerMobile          *string
	City                    *string
	ProductName             *string
	SourceChannel           *string
}

type UpdateLeadProfileResult struct {
	Lead     *types.Lead `json:"lead"`
	Warnings []string    `json:"warnings"`
}

type UpdateLeadProfileUseCase struct {
	leads      repo.LeadRepository
	activities repo.LeadActivityRepository
	txManager  repo.TransactionManager
	now        func() time.Time
}

func NewUpdateLeadProfileUseCase(leads repo.LeadRepository, activities repo.LeadActivityRepository) *UpdateLeadProfileUseCase {
	return &UpdateLeadProfileUseCase{leads: leads, activities: activities, now: time.Now}
}

func (uc *UpdateLeadProfileUseCase) WithTransactionManager(txManager repo.TransactionManager) *UpdateLeadProfileUseCase {
	uc.txManager = txManager
	return uc
}

func (uc *UpdateLeadProfileUseCase) Execute(ctx context.Context, req *UpdateLeadProfileRequest) (*UpdateLeadProfileResult, error) {
	if req == nil || req.LeadID <= 0 || strings.TrimSpace(req.ActorUserID) == "" || req.ExpectedBusinessVersion < 0 || req.ExpectedProfileVersion < 1 {
		return nil, types.ErrInvalidLeadProfile
	}
	if req.CustomerName == nil && req.CustomerMobile == nil && req.City == nil && req.ProductName == nil && req.SourceChannel == nil {
		return nil, types.ErrInvalidLeadProfile
	}

	var result *UpdateLeadProfileResult
	err := uc.withTransaction(ctx, func(ctx context.Context, leads repo.LeadRepository, activities repo.LeadActivityRepository) error {
		lead, err := leads.GetByID(ctx, req.LeadID)
		if err != nil {
			return err
		}
		if lead == nil {
			return types.ErrLeadNotFound
		}
		if !canEditLeadProfile(lead, req.ActorUserID, types.UserRole(strings.TrimSpace(req.ActorRole))) {
			return types.ErrUnauthorized
		}
		if lead.Version != req.ExpectedBusinessVersion || lead.ProfileVersion != req.ExpectedProfileVersion {
			return types.ErrLeadVersionConflict
		}

		before := profileAuditValues(lead)
		beforeMobile := security.NormalizeMobile(lead.CustomerMobile)
		if err := applyLeadProfilePatch(lead, req); err != nil {
			return err
		}
		after := profileAuditValues(lead)
		// Audit masks are intentionally lossy; compare the actual normalized
		// contact when deciding whether to persist a profile change.
		compareBefore, compareAfter := maps.Clone(before), maps.Clone(after)
		compareBefore["customer_mobile_masked"] = beforeMobile
		compareAfter["customer_mobile_masked"] = security.NormalizeMobile(lead.CustomerMobile)
		changed := changedProfileFields(compareBefore, compareAfter)
		if len(changed) == 0 {
			result = &UpdateLeadProfileResult{Lead: lead, Warnings: []string{}}
			return nil
		}

		profileRepo, ok := leads.(repo.LeadProfileRepository)
		if !ok {
			return types.ErrUnauthorized
		}
		updated, err := profileRepo.AtomicUpdateProfile(ctx, lead, req.ExpectedBusinessVersion, req.ExpectedProfileVersion)
		if err != nil {
			return err
		}
		if !updated {
			return types.ErrLeadVersionConflict
		}

		fromJSON, _ := json.Marshal(selectProfileAuditValues(before, changed))
		toJSON, _ := json.Marshal(selectProfileAuditValues(after, changed))
		metadataJSON, _ := json.Marshal(map[string]interface{}{
			"changed_fields":  changed,
			"profile_version": lead.ProfileVersion,
		})
		if activities != nil {
			if err := activities.Create(ctx, &types.LeadActivityRecord{
				LeadID:        lead.ID,
				ActivityType:  types.LeadActivityProfileUpdated,
				ActorUserID:   strings.TrimSpace(req.ActorUserID),
				ActorRole:     strings.TrimSpace(req.ActorRole),
				FromValueJSON: string(fromJSON),
				ToValueJSON:   string(toJSON),
				MetadataJSON:  string(metadataJSON),
				CreatedAt:     uc.now(),
			}); err != nil {
				return err
			}
		}

		warnings := make([]string, 0, 2)
		if !validLeadContact(lead.CustomerMobile, lead.CustomerWechat) {
			warnings = append(warnings, "legacy_contact_needs_review")
		}
		if lead.CustomerMobile != "" {
			duplicate, err := profileRepo.HasOtherActiveMobile(ctx, lead.CustomerMobile, lead.ID)
			if err != nil {
				return err
			}
			if duplicate {
				warnings = append(warnings, LeadProfileWarningDuplicateMobile)
			}
		}
		result = &UpdateLeadProfileResult{Lead: lead, Warnings: warnings}
		return nil
	})
	return result, err
}

func (uc *UpdateLeadProfileUseCase) withTransaction(ctx context.Context, fn func(context.Context, repo.LeadRepository, repo.LeadActivityRepository) error) error {
	if uc.txManager == nil {
		return fn(ctx, uc.leads, uc.activities)
	}
	return uc.txManager.WithinTransaction(ctx, func(ctx context.Context, repos repo.TransactionRepositories) error {
		return fn(ctx, repos.LeadRepository(), repos.LeadActivityRepository())
	})
}

func canEditLeadProfile(lead *types.Lead, actorUserID string, role types.UserRole) bool {
	if lead == nil {
		return false
	}
	if role == types.UserRoleAdmin || role == types.UserRoleManager {
		return true
	}
	if lead.Status == types.LeadStatusInvalid || lead.Status == types.LeadStatusConverted {
		return canCorrectLead(lead, actorUserID, role)
	}
	if lead.ResultStatus != types.LeadResultActive || (lead.Status != types.LeadStatusClaimed && lead.Status != types.LeadStatusFollowing) {
		return false
	}
	actorUserID = strings.TrimSpace(actorUserID)
	if actorUserID == "" {
		return false
	}
	if types.IsInvitationRole(role) && lead.BusinessStage == types.BusinessStageInvitation {
		return lead.CurrentOwnerUserID == actorUserID || lead.InviterUserID == actorUserID || lead.OwnerUserID == actorUserID
	}
	if role == types.UserRoleCloser && lead.BusinessStage == types.BusinessStageNegotiation {
		return lead.CurrentOwnerUserID == actorUserID || lead.CloserUserID == actorUserID
	}
	return false
}

func applyLeadProfilePatch(lead *types.Lead, req *UpdateLeadProfileRequest) error {
	if req.SourceChannel != nil {
		source := strings.TrimSpace(*req.SourceChannel)
		if source != strings.TrimSpace(lead.SourceChannel) && !types.IsManualLeadSource(source) {
			return types.ErrInvalidLeadProfile
		}
		lead.SourceChannel = source
	}
	if req.CustomerName != nil {
		lead.CustomerName = strings.TrimSpace(*req.CustomerName)
	}
	if req.City != nil {
		lead.City = strings.TrimSpace(*req.City)
	}
	if req.ProductName != nil {
		lead.ProductName = strings.TrimSpace(*req.ProductName)
	}
	if req.CustomerMobile != nil && strings.TrimSpace(*req.CustomerMobile) != strings.TrimSpace(lead.CustomerMobile) {
		raw := strings.TrimSpace(*req.CustomerMobile)
		if !hasOnlySupportedPhoneCharacters(raw) {
			return types.ErrInvalidLeadProfile
		}
		normalized := security.NormalizeMobile(raw)
		if raw != "" && (len(normalized) < 6 || len(normalized) > 20) {
			return types.ErrInvalidLeadProfile
		}
		if normalized == "" && strings.TrimSpace(lead.CustomerWechat) == "" {
			return types.ErrInvalidLeadProfile
		}
		lead.CustomerMobile = normalized
		lead.CustomerMobileMasked = maskMobile(lead.CustomerMobile)
	}
	if lead.CustomerName == "" || len([]rune(lead.CustomerName)) > 64 || len([]rune(lead.City)) > 64 || len([]rune(lead.ProductName)) > 128 {
		return types.ErrInvalidLeadProfile
	}

	return nil
}

func hasOnlySupportedPhoneCharacters(value string) bool {
	for _, r := range value {
		if (r >= '0' && r <= '9') || unicode.IsSpace(r) || r == '+' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func profileAuditValues(lead *types.Lead) map[string]string {
	return map[string]string{
		"customer_name":          lead.CustomerName,
		"customer_mobile_masked": maskMobile(lead.CustomerMobile),
		"city":                   lead.City,
		"product_name":           lead.ProductName,
		"source_channel":         lead.SourceChannel,
	}
}

func changedProfileFields(before, after map[string]string) []string {
	ordered := []string{"customer_name", "customer_mobile_masked", "city", "product_name", "source_channel"}
	changed := make([]string, 0, len(ordered))
	for _, key := range ordered {
		if before[key] != after[key] {
			changed = append(changed, key)
		}
	}
	return changed
}

func selectProfileAuditValues(values map[string]string, fields []string) map[string]string {
	selected := make(map[string]string, len(fields))
	for _, field := range fields {
		selected[field] = values[field]
	}
	return selected
}
