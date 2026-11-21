package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

// ListLeadsUseCase 查询线索列表用例
type ListLeadsUseCase struct {
	leadRepo        repo.LeadRepository
	dispatchRepo    repo.DispatchRecordRepository
	userBindingRepo repo.UserBindingRepository
}

func NewListLeadsUseCase(leadRepo repo.LeadRepository) *ListLeadsUseCase {
	return &ListLeadsUseCase{
		leadRepo: leadRepo,
	}
}

func (uc *ListLeadsUseCase) WithUserBindingRepository(userBindingRepo repo.UserBindingRepository) *ListLeadsUseCase {
	uc.userBindingRepo = userBindingRepo
	return uc
}

func (uc *ListLeadsUseCase) WithDispatchRecordRepository(dispatchRepo repo.DispatchRecordRepository) *ListLeadsUseCase {
	uc.dispatchRepo = dispatchRepo
	return uc
}

// ListLeadsRequest 查询线索列表请求
type ListLeadsRequest struct {
	CurrentUser     string // 当前用户 WeComUserID
	Role            string // 用户角色
	Status          string // 筛选状态（可选）
	Keyword         string // 搜索关键词（可选）
	SalesUserID     string // 管理员/主管按负责人筛选（可选）
	SortBy          string // 排序字段（白名单）
	SortOrder       string // asc / desc
	DispatcherScope types.DispatcherLeadScope
	StartTime       *time.Time
	EndTime         *time.Time // exclusive
	Page            int        // 页码（从 1 开始）
	PageSize        int        // 每页大小
}

// ListLeadsResponse 查询线索列表响应
type ListLeadsResponse struct {
	Leads      []*types.Lead
	Total      int
	Page       int
	PageSize   int
	TotalPages int
}

// Execute 执行查询线索列表逻辑
func (uc *ListLeadsUseCase) Execute(ctx context.Context, req *ListLeadsRequest) (*ListLeadsResponse, error) {
	if req == nil {
		return nil, types.ErrInvalidLeadSort
	}
	req.SortBy = strings.TrimSpace(req.SortBy)
	req.SortOrder = strings.ToLower(strings.TrimSpace(req.SortOrder))
	if req.SortBy == "" {
		req.SortBy = "updated_at"
	}
	if req.SortOrder == "" {
		req.SortOrder = "desc"
	}
	if !IsAllowedLeadSort(req.SortBy, req.SortOrder) {
		return nil, types.ErrInvalidLeadSort
	}
	role := types.UserRole(strings.TrimSpace(req.Role))
	if role != types.UserRoleDispatcher && req.DispatcherScope != "" {
		return nil, types.ErrInvalidDispatcherScope
	}
	if role == types.UserRoleDispatcher {
		if req.DispatcherScope == "" {
			req.DispatcherScope = types.DispatcherLeadScopeAll
		}
		if !types.IsValidDispatcherLeadScope(req.DispatcherScope) {
			return nil, types.ErrInvalidDispatcherScope
		}
	}
	salesUserID := strings.TrimSpace(req.SalesUserID)
	if salesUserID != "" {
		if role != types.UserRoleAdmin && role != types.UserRoleManager {
			return nil, types.ErrUnauthorized
		}
		binding, err := uc.userBindingRepo.GetByWeComUserID(ctx, salesUserID)
		if err != nil {
			return nil, err
		}
		if binding == nil || binding.Status != types.UserStatusActive || (!types.IsInvitationRole(binding.Role) && binding.Role != types.UserRoleManager && binding.Role != types.UserRoleCloser) {
			return nil, types.ErrInvalidInviter
		}
	}
	// 默认分页参数
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}

	offset := (req.Page - 1) * req.PageSize
	filter := repo.LeadListFilter{
		Status:    strings.TrimSpace(req.Status),
		Keyword:   strings.TrimSpace(req.Keyword),
		SortBy:    req.SortBy,
		SortOrder: req.SortOrder,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	}
	if req.Role == string(types.UserRoleAdmin) || req.Role == string(types.UserRoleManager) {
		filter.SalesUserID = salesUserID
	}
	if filter.Status == "needs_action" && (types.IsInvitationRole(role) || role == types.UserRoleCloser) {
		filter.NeedsActionUserID = strings.TrimSpace(req.CurrentUser)
		if filter.NeedsActionUserID == "" {
			return nil, types.ErrUnauthorized
		}
	}

	// 根据角色查询不同范围的线索
	var leads []*types.Lead
	var total int
	var err error

	switch role {
	case types.UserRoleAdmin, types.UserRoleManager:
		// 管理员和主管都是全局读取角色。调度员执行首次分配后，
		// 线索的派发人不一定是主管，因此不能再用派发人限制主管视野。
		leads, total, err = uc.leadRepo.ListAllFiltered(ctx, filter, offset, req.PageSize)
	case types.UserRoleSales, types.UserRoleInviter:
		// 销售查看分配给自己的线索
		leads, total, err = uc.leadRepo.ListBySalesFiltered(ctx, req.CurrentUser, filter, offset, req.PageSize)
	case types.UserRoleCloser:
		closerRepo, ok := uc.leadRepo.(interface {
			ListByCloserFiltered(context.Context, string, repo.LeadListFilter, int, int) ([]*types.Lead, int, error)
		})
		if !ok {
			return nil, types.ErrUnauthorized
		}
		leads, total, err = closerRepo.ListByCloserFiltered(ctx, req.CurrentUser, filter, offset, req.PageSize)
	case types.UserRoleDispatcher:
		dispatcherRepo, ok := uc.leadRepo.(repo.DispatcherLeadRepository)
		if !ok {
			return nil, types.ErrUnauthorized
		}
		leads, total, err = dispatcherRepo.ListByDispatcherFiltered(ctx, req.CurrentUser, req.DispatcherScope, filter, offset, req.PageSize)
	default:
		return nil, types.ErrUnauthorized
	}

	if err != nil {
		return nil, err
	}
	if err := uc.enrichLatestDispatch(ctx, leads); err != nil {
		return nil, err
	}
	if err := enrichLeadUserNames(ctx, uc.userBindingRepo, leads...); err != nil {
		return nil, err
	}
	applyLeadsMobileVisibility(leads, req.CurrentUser, req.Role)

	totalPages := (total + req.PageSize - 1) / req.PageSize

	return &ListLeadsResponse{
		Leads:      leads,
		Total:      total,
		Page:       req.Page,
		PageSize:   req.PageSize,
		TotalPages: totalPages,
	}, nil
}

func IsAllowedLeadSort(sortBy, sortOrder string) bool {
	if sortOrder != "asc" && sortOrder != "desc" {
		return false
	}
	switch sortBy {
	case "updated_at", "created_at", "next_followup_due_at", "assigned_at", "converted_at":
		return true
	default:
		return false
	}
}

func (uc *ListLeadsUseCase) enrichLatestDispatch(ctx context.Context, leads []*types.Lead) error {
	if uc.dispatchRepo == nil || len(leads) == 0 {
		return nil
	}

	leadIDs := make([]int64, 0, len(leads))
	for _, lead := range leads {
		if lead != nil && lead.ID > 0 {
			leadIDs = append(leadIDs, lead.ID)
		}
	}

	records, err := uc.dispatchRepo.ListLatestByLeadIDs(ctx, leadIDs)
	if err != nil {
		return err
	}

	for _, lead := range leads {
		if lead == nil {
			continue
		}
		record := records[lead.ID]
		if record == nil {
			continue
		}
		lead.LastDispatchManagerUserID = record.ManagerUserID
		lead.LastDispatchActionType = record.ActionType
		dispatchedAt := record.CreatedAt
		lead.LastDispatchedAt = &dispatchedAt
	}

	return nil
}
