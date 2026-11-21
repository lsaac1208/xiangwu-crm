package repo

import (
	"context"
	"strings"
	"time"

	"github.com/lsaac128/kxone/internal/security"
	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

// GormLeadRepository GORM 实现的线索仓储
type GormLeadRepository struct {
	db             *gorm.DB
	fieldProtector security.FieldProtector
	mobileHasher   security.MobileHasher
}

func NewGormLeadRepository(db *gorm.DB) *GormLeadRepository {
	return &GormLeadRepository{db: db}
}

func (r *GormLeadRepository) WithFieldProtector(fieldProtector security.FieldProtector) *GormLeadRepository {
	r.fieldProtector = fieldProtector
	return r
}

func (r *GormLeadRepository) WithMobileHasher(mobileHasher security.MobileHasher) *GormLeadRepository {
	r.mobileHasher = mobileHasher
	return r
}

func (r *GormLeadRepository) Create(ctx context.Context, lead *types.Lead) error {
	model, err := r.modelFromLead(lead)
	if err != nil {
		return err
	}

	if err := r.db.WithContext(ctx).Create(model).Error; err != nil {
		return err
	}

	lead.ID = model.ID
	lead.ProfileVersion = model.ProfileVersion
	return nil
}

func (r *GormLeadRepository) GetByID(ctx context.Context, id int64) (*types.Lead, error) {
	var model LeadModel
	if err := r.db.WithContext(ctx).First(&model, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return r.leadFromModel(&model)
}

func (r *GormLeadRepository) GetByLeadNo(ctx context.Context, leadNo string) (*types.Lead, error) {
	var model LeadModel
	if err := r.db.WithContext(ctx).Where("lead_no = ?", leadNo).First(&model).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return r.leadFromModel(&model)
}

// Update performs a full-column update so zero values can be persisted; callers
// must pass a complete Lead entity, usually loaded via GetByID before mutation.
func (r *GormLeadRepository) Update(ctx context.Context, lead *types.Lead) error {
	model, err := r.modelFromLead(lead)
	if err != nil {
		return err
	}

	// 使用乐观锁更新
	result := r.db.WithContext(ctx).
		Model(&LeadModel{}).
		Where("id = ? AND version = ? AND profile_version = ?", lead.ID, lead.Version-1, lead.ProfileVersion).
		Select("*").
		Omit("id").
		Updates(model)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return types.ErrLeadVersionConflict
	}

	return nil
}

// AtomicUpdateProfile only updates customer profile columns. The workflow
// version remains unchanged so existing reminder tasks keep their validity.
func (r *GormLeadRepository) AtomicUpdateProfile(ctx context.Context, lead *types.Lead, expectedBusinessVersion, expectedProfileVersion int) (bool, error) {
	model, err := r.modelFromLead(lead)
	if err != nil {
		return false, err
	}
	now := time.Now()
	result := r.db.WithContext(ctx).
		Model(&LeadModel{}).
		Where("id = ? AND version = ? AND profile_version = ?", lead.ID, expectedBusinessVersion, expectedProfileVersion).
		Updates(map[string]interface{}{
			"customer_name":          model.CustomerName,
			"customer_mobile":        model.CustomerMobile,
			"customer_mobile_masked": model.CustomerMobileMasked,
			"customer_mobile_hash":   model.CustomerMobileHash,
			"city":                   model.City,
			"product_name":           model.ProductName,
			"source_channel":         model.SourceChannel,
			"profile_version":        gorm.Expr("profile_version + 1"),
			"updated_at":             now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected > 0 {
		lead.ProfileVersion = expectedProfileVersion + 1
		lead.UpdatedAt = now
	}
	return result.RowsAffected > 0, nil
}

func (r *GormLeadRepository) HasOtherActiveMobile(ctx context.Context, mobile string, excludeLeadID int64) (bool, error) {
	if r.mobileHasher == nil {
		return false, nil
	}
	hash := r.mobileHasher.HashMobile(mobile)
	if hash == "" {
		return false, nil
	}
	var count int64
	err := r.db.WithContext(ctx).Model(&LeadModel{}).
		Where("id <> ? AND customer_mobile_hash = ?", excludeLeadID, hash).
		Where("status NOT IN ?", []string{string(types.LeadStatusInvalid), string(types.LeadStatusConverted), string(types.LeadStatusClosed)}).
		Count(&count).Error
	return count > 0, err
}

func (r *GormLeadRepository) AtomicClaim(ctx context.Context, leadID int64, salesUserID string, expectedVersion, expectedProfileVersion int) (bool, error) {
	now := time.Now()

	result := r.db.WithContext(ctx).
		Model(&LeadModel{}).
		Where("id = ? AND assigned_sales_user_id = ? AND status = ? AND version = ? AND profile_version = ?",
			leadID, salesUserID, string(types.LeadStatusAssignedPendingClaim), expectedVersion, expectedProfileVersion).
		Updates(map[string]interface{}{
			"owner_user_id":            salesUserID,
			"inviter_user_id":          salesUserID,
			"current_owner_user_id":    salesUserID,
			"current_owner_role":       string(types.OwnerRoleInviter),
			"ownership_state":          string(types.OwnershipPrivate),
			"claimed_at":               now,
			"current_owner_started_at": now, "current_owner_first_followup_at": nil,
			"inviter_claimed_at": now,
			"status":             string(types.LeadStatusClaimed),
			"version":            gorm.Expr("version + 1"),
			"updated_at":         now,
		})

	if result.Error != nil {
		return false, result.Error
	}

	return result.RowsAffected > 0, nil
}

func (r *GormLeadRepository) AtomicAssign(ctx context.Context, leadID int64, managerUserID, salesUserID string, expectedVersion, expectedProfileVersion int) (bool, error) {
	now := time.Now()

	result := r.db.WithContext(ctx).
		Model(&LeadModel{}).
		Where("id = ? AND status = ? AND version = ? AND profile_version = ?",
			leadID, string(types.LeadStatusPendingDispatch), expectedVersion, expectedProfileVersion).
		Updates(map[string]interface{}{
			"dispatch_manager_user_id": managerUserID,
			"assigned_sales_user_id":   salesUserID,
			"assigned_inviter_user_id": salesUserID,
			"ownership_state":          string(types.OwnershipAssignedPendingClaim),
			"assigned_at":              now,
			"inviter_assigned_at":      now,
			"status":                   string(types.LeadStatusAssignedPendingClaim),
			"version":                  gorm.Expr("version + 1"),
			"updated_at":               now,
		})

	if result.Error != nil {
		return false, result.Error
	}

	return result.RowsAffected > 0, nil
}

func (r *GormLeadRepository) AtomicReassign(ctx context.Context, leadID int64, managerUserID, newSalesUserID string, expectedVersion, expectedProfileVersion int) (bool, error) {
	now := time.Now()

	result := r.db.WithContext(ctx).
		Model(&LeadModel{}).
		Where("id = ? AND dispatch_manager_user_id = ? AND status IN ? AND version = ? AND profile_version = ?",
			leadID,
			managerUserID,
			[]string{
				string(types.LeadStatusAssignedPendingClaim),
				string(types.LeadStatusClaimed),
				string(types.LeadStatusFollowing),
			},
			expectedVersion,
			expectedProfileVersion,
		).
		Updates(map[string]interface{}{
			"assigned_sales_user_id":   newSalesUserID,
			"assigned_inviter_user_id": newSalesUserID,
			"owner_user_id":            "",
			"inviter_user_id":          "",
			"current_owner_user_id":    "",
			"current_owner_role":       "",
			"ownership_state":          string(types.OwnershipAssignedPendingClaim),
			"claimed_at":               nil,
			"current_owner_started_at": nil, "current_owner_first_followup_at": nil,
			"inviter_claimed_at":   nil,
			"next_followup_due_at": nil,
			"assigned_at":          now,
			"inviter_assigned_at":  now,
			"status":               string(types.LeadStatusAssignedPendingClaim),
			"version":              gorm.Expr("version + 1"),
			"updated_at":           now,
		})

	if result.Error != nil {
		return false, result.Error
	}

	return result.RowsAffected > 0, nil
}

func (r *GormLeadRepository) ListPendingDispatch(ctx context.Context, limit int) ([]*types.Lead, error) {
	var models []LeadModel

	err := r.db.WithContext(ctx).
		Where("status = ?", string(types.LeadStatusPendingDispatch)).
		Order(absoluteTimeSort(r.db, "created_at", "ASC", "id")).
		Limit(limit).
		Find(&models).Error

	if err != nil {
		return nil, err
	}

	leads := make([]*types.Lead, len(models))
	for i, model := range models {
		lead, err := r.leadFromModel(&model)
		if err != nil {
			return nil, err
		}
		leads[i] = lead
	}

	return leads, nil
}

func (r *GormLeadRepository) ListPendingClaim(ctx context.Context, limit int) ([]*types.Lead, error) {
	var models []LeadModel

	err := r.db.WithContext(ctx).
		Where("status = ?", string(types.LeadStatusAssignedPendingClaim)).
		Order(absoluteTimeSort(r.db, "assigned_at", "ASC", "id")).
		Limit(limit).
		Find(&models).Error

	if err != nil {
		return nil, err
	}

	leads := make([]*types.Lead, len(models))
	for i, model := range models {
		lead, err := r.leadFromModel(&model)
		if err != nil {
			return nil, err
		}
		leads[i] = lead
	}

	return leads, nil
}

func (r *GormLeadRepository) ListAll(ctx context.Context, status string, offset, limit int) ([]*types.Lead, int, error) {
	return r.ListAllFiltered(ctx, LeadListFilter{Status: status}, offset, limit)
}

func (r *GormLeadRepository) ListAllFiltered(ctx context.Context, filter LeadListFilter, offset, limit int) ([]*types.Lead, int, error) {
	query := r.db.WithContext(ctx).Model(&LeadModel{})
	return r.listWithPagination(query, filter, offset, limit)
}

func (r *GormLeadRepository) ListByManager(ctx context.Context, managerUserID, status string, offset, limit int) ([]*types.Lead, int, error) {
	return r.ListByManagerFiltered(ctx, managerUserID, LeadListFilter{Status: status}, offset, limit)
}

func (r *GormLeadRepository) ListByManagerFiltered(ctx context.Context, managerUserID string, filter LeadListFilter, offset, limit int) ([]*types.Lead, int, error) {
	query := r.db.WithContext(ctx).
		Model(&LeadModel{})
	query = applyManagerLeadScope(query, managerUserID)

	return r.listWithPagination(query, filter, offset, limit)
}

func (r *GormLeadRepository) ListByDispatcherFiltered(ctx context.Context, dispatcherUserID string, scope types.DispatcherLeadScope, filter LeadListFilter, offset, limit int) ([]*types.Lead, int, error) {
	query := r.db.WithContext(ctx).Model(&LeadModel{})
	query = applyDispatcherLeadScope(query, dispatcherUserID, scope)
	return r.listWithPagination(query, filter, offset, limit)
}

func (r *GormLeadRepository) CanDispatcherViewLead(ctx context.Context, leadID int64, dispatcherUserID string) (bool, error) {
	if leadID <= 0 || strings.TrimSpace(dispatcherUserID) == "" {
		return false, nil
	}
	query := r.db.WithContext(ctx).Model(&LeadModel{}).Where("leads.id = ?", leadID)
	query = applyDispatcherLeadScope(query, dispatcherUserID, types.DispatcherLeadScopeAll)
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *GormLeadRepository) ListBySales(ctx context.Context, salesUserID, status string, offset, limit int) ([]*types.Lead, int, error) {
	return r.ListBySalesFiltered(ctx, salesUserID, LeadListFilter{Status: status}, offset, limit)
}

func (r *GormLeadRepository) ListBySalesFiltered(ctx context.Context, salesUserID string, filter LeadListFilter, offset, limit int) ([]*types.Lead, int, error) {
	query := r.db.WithContext(ctx).
		Model(&LeadModel{}).
		Where("(assigned_sales_user_id = ? OR owner_user_id = ? OR inviter_user_id = ? OR assigned_inviter_user_id = ?)", salesUserID, salesUserID, salesUserID, salesUserID)

	return r.listWithPagination(query, filter, offset, limit)
}

func (r *GormLeadRepository) ListByCloserFiltered(ctx context.Context, closerUserID string, filter LeadListFilter, offset, limit int) ([]*types.Lead, int, error) {
	query := r.db.WithContext(ctx).
		Model(&LeadModel{}).
		Where("created_by_user_id = ? OR ((business_stage = ? OR (status IN ? AND (current_owner_role = ? OR delivered_at IS NOT NULL))) AND (closer_user_id = ? OR (current_owner_role = ? AND current_owner_user_id = ?)))",
			closerUserID, string(types.BusinessStageNegotiation), []string{"converted", "invalid"}, string(types.OwnerRoleCloser), closerUserID, string(types.OwnerRoleCloser), closerUserID)
	return r.listWithPagination(query, filter, offset, limit)
}

func (r *GormLeadRepository) ListPublicPoolFiltered(ctx context.Context, filter LeadListFilter, offset, limit int) ([]*types.Lead, int, error) {
	query := r.db.WithContext(ctx).Model(&LeadModel{}).
		Where("business_stage = ? AND ownership_state = ? AND result_status = ?",
			string(types.BusinessStageInvitation), string(types.OwnershipPublicPool), string(types.LeadResultActive))
	filter.Status = ""
	return r.listWithPagination(query, filter, offset, limit)
}

func (r *GormLeadRepository) CountActiveInvitationByInviter(ctx context.Context, inviterUserID string) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&LeadModel{}).
		Where("business_stage = ? AND ownership_state = ? AND result_status = ? AND current_owner_user_id = ?",
			string(types.BusinessStageInvitation), string(types.OwnershipPrivate), string(types.LeadResultActive), inviterUserID).
		Count(&count).Error
	return int(count), err
}

func (r *GormLeadRepository) ListAutoPoolCandidates(ctx context.Context, unclaimedBefore, followupBefore time.Time, limit int) ([]*types.Lead, error) {
	query := r.db.WithContext(ctx).Model(&LeadModel{}).
		Where("business_stage = ? AND result_status = ? AND current_appointment_id IS NULL AND arrived_at IS NULL AND delivered_at IS NULL", string(types.BusinessStageInvitation), string(types.LeadResultActive)).
		Where("(ownership_state = ? AND inviter_assigned_at IS NOT NULL AND "+absoluteTimeComparison(r.db, "inviter_assigned_at", "<=")+") OR (ownership_state = ? AND next_followup_due_at IS NOT NULL AND "+absoluteTimeComparison(r.db, "next_followup_due_at", "<=")+")",
			string(types.OwnershipAssignedPendingClaim), unclaimedBefore, string(types.OwnershipPrivate), followupBefore).
		Order(absoluteTimeSort(r.db, "updated_at", "ASC", "id"))
	if limit > 0 {
		query = query.Limit(limit)
	}
	var models []LeadModel
	if err := query.Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*types.Lead, len(models))
	for i := range models {
		lead, err := r.leadFromModel(&models[i])
		if err != nil {
			return nil, err
		}
		result[i] = lead
	}
	return result, nil
}

func (r *GormLeadRepository) ListForStats(ctx context.Context, filter LeadStatsFilter, limit int) ([]*types.Lead, error) {
	query := r.db.WithContext(ctx).Model(&LeadModel{}).Omit("customer_mobile", "customer_wechat", "customer_mobile_hash")
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.ManagerUserID != "" {
		query = applyManagerLeadScope(query, filter.ManagerUserID)
	}
	if filter.SalesUserID != "" {
		query = applyLeadSalesScope(query, filter.SalesUserID)
	}
	query = applyCreatedAtRange(query, r.db, filter.StartTime, filter.EndTime)
	if limit > 0 {
		query = query.Limit(limit)
	}

	var models []LeadModel
	if err := query.Order(absoluteTimeSort(r.db, "created_at", "DESC", "id")).Find(&models).Error; err != nil {
		return nil, err
	}
	leads := make([]*types.Lead, len(models))
	for i := range models {
		lead, err := r.leadFromModel(&models[i])
		if err != nil {
			return nil, err
		}
		leads[i] = lead
	}
	return leads, nil
}

func (r *GormLeadRepository) listWithPagination(query *gorm.DB, filter LeadListFilter, offset, limit int) ([]*types.Lead, int, error) {
	if filter.Status == "needs_action" {
		// Build one server-side result set so keyword, owner, date, count and
		// pagination apply to every task. Never append unfiltered workbench rows.
		actionOwner := filter.NeedsActionUserID
		if actionOwner == "" {
			actionOwner = filter.SalesUserID
		}
		if actionOwner != "" {
			query = query.Where("(result_status = 'active' AND status = 'assigned_pending_claim' AND (assigned_inviter_user_id = ? OR (COALESCE(assigned_inviter_user_id, '') = '' AND assigned_sales_user_id = ?))) OR ("+currentFollowupPredicate+" AND current_owner_user_id = ? AND "+absoluteTimeComparison(r.db, "next_followup_due_at", "<")+")", actionOwner, actionOwner, actionOwner, databaseTimeBoundary(r.db, time.Now()))
		} else {
			query = query.Where("(result_status = 'active' AND status IN ('pending_dispatch','assigned_pending_claim')) OR ("+currentFollowupPredicate+" AND "+absoluteTimeComparison(r.db, "next_followup_due_at", "<")+")", databaseTimeBoundary(r.db, time.Now()))
		}
	} else if filter.Status == "followup_overdue" {
		query = query.Where(currentFollowupPredicate).Where(absoluteTimeComparison(r.db, "next_followup_due_at", "<"), databaseTimeBoundary(r.db, time.Now()))
		if filter.SalesUserID != "" {
			query = query.Where("current_owner_user_id = ?", filter.SalesUserID)
		}
	} else if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.SalesUserID != "" {
		query = applyLeadSalesScope(query, filter.SalesUserID)
	}
	query = applyCreatedAtRange(query, r.db, filter.StartTime, filter.EndTime)
	mobileHash := ""
	if r.mobileHasher != nil {
		mobileHash = r.mobileHasher.HashSearchKeyword(filter.Keyword)
	}
	query = applyLeadKeywordFilter(query, filter.Keyword, mobileHash)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	sortColumn := map[string]string{
		"updated_at": "updated_at", "created_at": "created_at",
		"next_followup_due_at": "next_followup_due_at", "assigned_at": "assigned_at",
		"converted_at": "converted_at",
	}[filter.SortBy]
	if sortColumn == "" {
		sortColumn = "updated_at"
	}
	sortOrder := strings.ToUpper(filter.SortOrder)
	if sortOrder != "ASC" {
		sortOrder = "DESC"
	}

	var models []LeadModel
	if err := query.
		Order(absoluteTimeSort(r.db, sortColumn, sortOrder, "id")).
		Offset(offset).
		Limit(limit).
		Find(&models).Error; err != nil {
		return nil, 0, err
	}

	leads := make([]*types.Lead, len(models))
	for i, model := range models {
		lead, err := r.leadFromModel(&model)
		if err != nil {
			return nil, 0, err
		}
		leads[i] = lead
	}

	return leads, int(total), nil
}

func applyManagerLeadScope(query *gorm.DB, managerUserID string) *gorm.DB {
	frontlineRoles := []string{
		string(types.UserRoleSales),
		string(types.UserRoleInviter),
		string(types.UserRoleCloser),
	}
	return query.Where("(status = ? OR dispatch_manager_user_id = ? OR created_by_role IN ?)",
		string(types.LeadStatusPendingDispatch), managerUserID, frontlineRoles)
}

func applyDispatcherLeadScope(query *gorm.DB, dispatcherUserID string, scope types.DispatcherLeadScope) *gorm.DB {
	dispatcherUserID = strings.TrimSpace(dispatcherUserID)
	if dispatcherUserID == "" {
		return query.Where("1 = 0")
	}
	handled := `(leads.created_by_user_id = ? OR EXISTS (
		SELECT 1 FROM lead_dispatch_records
		WHERE lead_dispatch_records.lead_id = leads.id
		  AND lead_dispatch_records.manager_user_id = ?
		  AND lead_dispatch_records.action_type = ?
	))`
	switch scope {
	case types.DispatcherLeadScopePending:
		return query.Where("leads.status = ?", string(types.LeadStatusPendingDispatch))
	case types.DispatcherLeadScopeHandled:
		return query.Where(handled, dispatcherUserID, dispatcherUserID, "assign")
	case types.DispatcherLeadScopeAll:
		return query.Where("(leads.status = ? OR "+handled+")", string(types.LeadStatusPendingDispatch), dispatcherUserID, dispatcherUserID, "assign")
	default:
		return query.Where("1 = 0")
	}
}

func applyLeadSalesScope(query *gorm.DB, salesUserID string) *gorm.DB {
	return query.Where("(assigned_sales_user_id = ? OR owner_user_id = ? OR assigned_inviter_user_id = ? OR inviter_user_id = ? OR closer_user_id = ? OR current_owner_user_id = ?)",
		salesUserID, salesUserID, salesUserID, salesUserID, salesUserID, salesUserID)
}

func (r *GormLeadRepository) BuildWorkbenchLeadSnapshot(ctx context.Context, userID string, role types.UserRole, now time.Time, taskLimit int) (*WorkbenchLeadSnapshot, error) {
	base := r.db.WithContext(ctx).Model(&LeadModel{})
	switch {
	case role == types.UserRoleAdmin || role == types.UserRoleManager:
	case role == types.UserRoleDispatcher:
		base = applyDispatcherLeadScope(base, userID, types.DispatcherLeadScopeAll)
	case types.IsInvitationRole(role):
		base = applyLeadSalesScope(base, userID)
	case role == types.UserRoleCloser:
		base = base.Where("created_by_user_id = ? OR closer_user_id = ? OR (current_owner_role = ? AND current_owner_user_id = ?)", userID, userID, string(types.OwnerRoleCloser), userID)
	default:
		return nil, types.ErrUnauthorized
	}
	count := func(query *gorm.DB) (int, error) {
		var value int64
		err := query.Count(&value).Error
		return int(value), err
	}
	result := &WorkbenchLeadSnapshot{}
	var err error
	if role == types.UserRoleAdmin || role == types.UserRoleManager || role == types.UserRoleDispatcher {
		result.PendingDispatch, err = count(base.Session(&gorm.Session{}).Where("business_stage = ? AND ownership_state = ?", string(types.BusinessStageInvitation), string(types.OwnershipPendingAssignment)))
		if err != nil {
			return nil, err
		}
	}
	pendingClaim := base.Session(&gorm.Session{}).Where("ownership_state = ?", string(types.OwnershipAssignedPendingClaim))
	if role != types.UserRoleAdmin && role != types.UserRoleManager {
		pendingClaim = pendingClaim.Where("assigned_inviter_user_id = ? OR created_by_user_id = ?", userID, userID)
	}
	result.PendingClaim, err = count(pendingClaim)
	if err != nil {
		return nil, err
	}
	result.OverdueFollowup, err = count(currentFollowupScope(base.Session(&gorm.Session{}), userID, role).Where(absoluteTimeComparison(r.db, "next_followup_due_at", "<"), databaseTimeBoundary(r.db, now)))
	if err != nil {
		return nil, err
	}
	result.NegotiationActive, err = count(base.Session(&gorm.Session{}).Where("business_stage = ? AND result_status = ?", string(types.BusinessStageNegotiation), string(types.LeadResultActive)))
	if err != nil {
		return nil, err
	}
	if role == types.UserRoleDispatcher {
		dayStart := types.BusinessDayStart(now)
		result.HandledToday, err = count(base.Session(&gorm.Session{}).Where("created_by_user_id = ? AND "+absoluteTimeComparison(r.db, "updated_at", ">="), userID, databaseTimeBoundary(r.db, dayStart)))
		if err != nil {
			return nil, err
		}
	}
	// Rank actual lead tasks across the complete visible scope before limiting.
	// Appointment tasks have their own bounded query and independent totals.
	overdue := currentFollowupPredicate + " AND " + absoluteTimeComparison(r.db, "next_followup_due_at", "<")
	overdueArgs := []any{databaseTimeBoundary(r.db, now)}
	manager := role == types.UserRoleAdmin || role == types.UserRoleManager
	if !manager {
		overdue += " AND current_owner_user_id = ?"
		overdueArgs = append(overdueArgs, userID)
	}
	pending, pendingArgs := "1 = 0", []any{}
	if manager || role == types.UserRoleDispatcher {
		pending = "(business_stage = ? AND ownership_state = ?)"
		pendingArgs = []any{string(types.BusinessStageInvitation), string(types.OwnershipPendingAssignment)}
	}
	if role == types.UserRoleManager || types.IsInvitationRole(role) {
		pending += " OR (ownership_state = ? AND assigned_inviter_user_id = ?)"
		pendingArgs = append(pendingArgs, string(types.OwnershipAssignedPendingClaim), userID)
	} else if role == types.UserRoleCloser {
		pending += " OR (ownership_state = ? AND created_by_user_id = ?)"
		pendingArgs = append(pendingArgs, string(types.OwnershipAssignedPendingClaim), userID)
	}
	negotiation, negotiationArgs := "1 = 0", []any{}
	if role == types.UserRoleCloser {
		negotiation = "business_stage = ? AND result_status = ? AND delivered_at IS NOT NULL AND (last_followup_at IS NULL OR " + absoluteTimeValue(r.db, "last_followup_at") + " <= " + absoluteTimeValue(r.db, "delivered_at") + ")"
		negotiationArgs = []any{string(types.BusinessStageNegotiation), string(types.LeadResultActive)}
	}
	selectArgs := append(append([]any{}, overdueArgs...), negotiationArgs...)
	filterArgs := append(append(append([]any{}, overdueArgs...), pendingArgs...), negotiationArgs...)
	query := base.Session(&gorm.Session{}).
		Select("leads.*, CASE WHEN ("+overdue+") OR ("+negotiation+") THEN "+absoluteTimeValue(r.db, "next_followup_due_at")+" END AS task_due", selectArgs...).
		Where("("+overdue+") OR ("+pending+") OR ("+negotiation+")", filterArgs...).
		Order("task_due IS NULL ASC, task_due ASC, " + absoluteTimeSort(r.db, "updated_at", "DESC", "id"))
	if taskLimit > 0 {
		query = query.Limit(taskLimit)
	}
	var models []LeadModel
	if err := query.Find(&models).Error; err != nil {
		return nil, err
	}
	result.Leads = make([]*types.Lead, 0, len(models))
	for i := range models {
		lead, err := r.leadFromModel(&models[i])
		if err != nil {
			return nil, err
		}
		result.Leads = append(result.Leads, lead)
	}
	return result, nil
}

func applyCreatedAtRange(query, rootDB *gorm.DB, startAt, endAtExclusive *time.Time) *gorm.DB {
	if startAt != nil {
		query = query.Where(absoluteTimeComparison(rootDB, "created_at", ">="), databaseTimeBoundary(rootDB, *startAt))
	}
	if endAtExclusive != nil {
		query = query.Where(absoluteTimeComparison(rootDB, "created_at", "<"), databaseTimeBoundary(rootDB, *endAtExclusive))
	}
	return query
}

func databaseTimeBoundary(db *gorm.DB, value time.Time) time.Time {
	if db != nil && db.Dialector != nil && db.Dialector.Name() == "sqlite" {
		return value.In(time.FixedZone("Asia/Shanghai", 8*60*60))
	}
	return value.UTC()
}

func applyLeadKeywordFilter(query *gorm.DB, keyword, mobileHash string) *gorm.DB {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return query
	}
	pattern := "%" + escapeLike(keyword) + "%"
	conditions := []string{
		"lead_no LIKE ? ESCAPE '~'",
		"customer_name LIKE ? ESCAPE '~'",
		"customer_mobile_masked LIKE ? ESCAPE '~'",
		"city LIKE ? ESCAPE '~'",
		"product_name LIKE ? ESCAPE '~'",
		"source_channel LIKE ? ESCAPE '~'",
		"source_campaign LIKE ? ESCAPE '~'",
		"lead_level LIKE ? ESCAPE '~'",
	}
	args := []any{pattern, pattern, pattern, pattern, pattern, pattern, pattern, pattern}
	if mobileHash != "" {
		conditions = append(conditions, "customer_mobile_hash = ?")
		args = append(args, mobileHash)
	}
	condition := "(" + strings.Join(conditions, " OR ") + ")"
	return query.Where(condition, args...)
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `~`, `~~`)
	value = strings.ReplaceAll(value, `%`, `~%`)
	value = strings.ReplaceAll(value, `_`, `~_`)
	return value
}

func (r *GormLeadRepository) modelFromLead(lead *types.Lead) (*LeadModel, error) {
	model := &LeadModel{}
	model.FromEntity(lead)
	if r.mobileHasher != nil {
		model.CustomerMobileHash = r.mobileHasher.HashMobile(model.CustomerMobile)
	}
	if r.fieldProtector != nil {
		encrypted, err := r.fieldProtector.Protect(model.CustomerMobile)
		if err != nil {
			return nil, err
		}
		model.CustomerMobile = encrypted
		encryptedWechat, err := r.fieldProtector.Protect(model.CustomerWechat)
		if err != nil {
			return nil, err
		}
		model.CustomerWechat = encryptedWechat
	}
	return model, nil
}

func (r *GormLeadRepository) leadFromModel(model *LeadModel) (*types.Lead, error) {
	lead := model.ToEntity()
	if r.fieldProtector != nil {
		plaintext, err := r.fieldProtector.Reveal(lead.CustomerMobile)
		if err != nil {
			return nil, err
		}
		lead.CustomerMobile = plaintext
		plaintextWechat, err := r.fieldProtector.Reveal(lead.CustomerWechat)
		if err != nil {
			return nil, err
		}
		lead.CustomerWechat = plaintextWechat
	}
	return lead, nil
}

// Kept in sync with types.IsCurrentFollowupEligible; repository contract tests exercise both.
const currentFollowupPredicate = "result_status = 'active' AND ownership_state = 'private' AND current_owner_user_id <> '' AND next_followup_due_at IS NOT NULL AND current_appointment_id IS NULL AND status NOT IN ('converted','closed','invalid','assigned_pending_claim','public_pool') AND business_stage IN ('invitation','negotiation')"

func currentFollowupScope(query *gorm.DB, userID string, role types.UserRole) *gorm.DB {
	query = query.Where(currentFollowupPredicate)
	if role != types.UserRoleAdmin && role != types.UserRoleManager {
		query = query.Where("current_owner_user_id = ?", userID)
	}
	return query
}
