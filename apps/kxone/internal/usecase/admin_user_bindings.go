package usecase

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
	"golang.org/x/crypto/bcrypt"
)

// AdminUserBindingsUseCase 管理企业微信用户绑定用例。
type AdminUserBindingsUseCase struct {
	userBindingRepo repo.UserBindingRepository
}

func (uc *AdminUserBindingsUseCase) ListAssignableInviters(ctx context.Context) (*ListUserBindingsResponse, error) {
	bindings := make([]*types.WeComUserBinding, 0)
	for _, role := range []types.UserRole{types.UserRoleInviter, types.UserRoleSales, types.UserRoleManager} {
		items, _, err := uc.userBindingRepo.List(ctx, string(types.UserStatusActive), string(role), 0, 100)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, items...)
	}
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].Role != bindings[j].Role {
			return bindings[i].Role == types.UserRoleManager
		}
		return bindings[i].EmployeeName < bindings[j].EmployeeName
	})
	return &ListUserBindingsResponse{
		Bindings: bindings, Total: len(bindings), Page: 1, PageSize: 100,
		TotalPages: func() int {
			if len(bindings) == 0 {
				return 0
			}
			return 1
		}(),
	}, nil
}

// ListLeadOwnerFilters returns active frontline users and managers for the
// manager/admin lead-list filter. It deliberately remains separate from the
// assignment candidate endpoint.
func (uc *AdminUserBindingsUseCase) ListLeadOwnerFilters(ctx context.Context) (*ListUserBindingsResponse, error) {
	bindings := make([]*types.WeComUserBinding, 0)
	for _, role := range []types.UserRole{types.UserRoleInviter, types.UserRoleSales, types.UserRoleManager, types.UserRoleCloser} {
		for offset := 0; ; offset += 100 {
			items, total, err := uc.userBindingRepo.List(ctx, string(types.UserStatusActive), string(role), offset, 100)
			if err != nil {
				return nil, err
			}
			bindings = append(bindings, items...)
			if len(items) < 100 || offset+len(items) >= total {
				break
			}
		}
	}

	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].Role != bindings[j].Role {
			return bindings[i].Role < bindings[j].Role
		}
		if bindings[i].EmployeeName != bindings[j].EmployeeName {
			return bindings[i].EmployeeName < bindings[j].EmployeeName
		}
		return bindings[i].WeComUserID < bindings[j].WeComUserID
	})

	totalPages := 0
	if len(bindings) > 0 {
		totalPages = 1
	}
	return &ListUserBindingsResponse{Bindings: bindings, Total: len(bindings), Page: 1, PageSize: 100, TotalPages: totalPages}, nil
}

func (uc *AdminUserBindingsUseCase) ListAssignableClosers(ctx context.Context) (*ListUserBindingsResponse, error) {
	bindings, _, err := uc.userBindingRepo.List(ctx, string(types.UserStatusActive), string(types.UserRoleCloser), 0, 100)
	if err != nil {
		return nil, err
	}
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].EmployeeName < bindings[j].EmployeeName })
	totalPages := 0
	if len(bindings) > 0 {
		totalPages = 1
	}
	return &ListUserBindingsResponse{Bindings: bindings, Total: len(bindings), Page: 1, PageSize: 100, TotalPages: totalPages}, nil
}

func NewAdminUserBindingsUseCase(userBindingRepo repo.UserBindingRepository) *AdminUserBindingsUseCase {
	return &AdminUserBindingsUseCase{userBindingRepo: userBindingRepo}
}

type ListUserBindingsRequest struct {
	Keyword  string
	Status   string
	Role     string
	Page     int
	PageSize int
}

type ListUserBindingsResponse struct {
	Bindings   []*types.WeComUserBinding `json:"bindings"`
	Total      int                       `json:"total"`
	Page       int                       `json:"page"`
	PageSize   int                       `json:"page_size"`
	TotalPages int                       `json:"total_pages"`
}

type SaveUserBindingRequest struct {
	ExpectedVersion         int64
	ID                      int64
	CRMUserID               int64
	WeComUserID             string
	Username                string
	Password                string
	EmployeeName            string
	Role                    types.UserRole
	DepartmentID            int64
	Status                  types.UserStatus
	GroupRobotMentionUserID string
}

func (uc *AdminUserBindingsUseCase) List(ctx context.Context, req *ListUserBindingsRequest) (*ListUserBindingsResponse, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}
	offset := (req.Page - 1) * req.PageSize

	var bindings []*types.WeComUserBinding
	var total int
	var err error
	if searchable, ok := uc.userBindingRepo.(repo.UserBindingSearchRepository); ok {
		bindings, total, err = searchable.Search(ctx, strings.TrimSpace(req.Keyword), req.Status, req.Role, offset, req.PageSize)
	} else {
		bindings, total, err = uc.userBindingRepo.List(ctx, req.Status, req.Role, offset, req.PageSize)
	}
	if err != nil {
		return nil, err
	}

	return &ListUserBindingsResponse{
		Bindings:   bindings,
		Total:      total,
		Page:       req.Page,
		PageSize:   req.PageSize,
		TotalPages: (total + req.PageSize - 1) / req.PageSize,
	}, nil
}

func (uc *AdminUserBindingsUseCase) Create(ctx context.Context, req *SaveUserBindingRequest) (*types.WeComUserBinding, error) {
	if err := validateAccountCredentials(req.Username, req.Password, true); err != nil {
		return nil, err
	}
	mentionUserID := strings.TrimSpace(req.GroupRobotMentionUserID)
	if err := validateGroupRobotMentionUserID(mentionUserID); err != nil {
		return nil, err
	}
	if mentionUserID != "" {
		other, err := uc.userBindingRepo.GetByGroupRobotMentionUserID(ctx, mentionUserID)
		if err != nil {
			return nil, err
		}
		if other != nil {
			return nil, fmt.Errorf("group robot mention UserID is already used by another active user")
		}
	}
	binding := buildUserBinding(req)
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	binding.PasswordHash = string(hash)
	now := time.Now()
	binding.CreatedAt = now
	binding.UpdatedAt = now
	if err := uc.userBindingRepo.Create(ctx, binding); err != nil {
		return nil, err
	}
	return binding, nil
}

func (uc *AdminUserBindingsUseCase) Update(ctx context.Context, req *SaveUserBindingRequest) (*types.WeComUserBinding, error) {
	if err := validateAccountCredentials(req.Username, req.Password, false); err != nil {
		return nil, err
	}
	existing, err := uc.userBindingRepo.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, types.ErrUserBindingNotFound
	}
	if req.ExpectedVersion < 1 || req.ExpectedVersion != existing.AccountVersion {
		return nil, types.ErrAccountVersionConflict
	}
	if req.CRMUserID > 0 && req.CRMUserID != existing.CRMUserID {
		return nil, fmt.Errorf("account identity is immutable")
	}
	req.CRMUserID = existing.CRMUserID
	mentionUserID := strings.TrimSpace(req.GroupRobotMentionUserID)
	if err := validateGroupRobotMentionUserID(mentionUserID); err != nil {
		return nil, err
	}
	if mentionUserID != "" {
		other, lookupErr := uc.userBindingRepo.GetByGroupRobotMentionUserID(ctx, mentionUserID)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if other != nil && other.ID != existing.ID {
			return nil, fmt.Errorf("group robot mention UserID is already used by another active user")
		}
	}

	binding := buildUserBinding(req)
	binding.ID = existing.ID
	// wecom_userid is the immutable internal actor ID referenced by lead and
	// dispatch history. Changing the login username must never sever that audit
	// trail or silently transfer historical access.
	binding.WeComUserID = existing.WeComUserID
	binding.GroupRobotMentionUserID = mentionUserID
	binding.AccountVersion = req.ExpectedVersion
	binding.AuthVersion = existing.AuthVersion
	if strings.TrimSpace(req.Password) != "" {
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if hashErr != nil {
			return nil, fmt.Errorf("hash password: %w", hashErr)
		}
		binding.PasswordHash = string(hash)
	}
	binding.CreatedAt = existing.CreatedAt
	binding.UpdatedAt = time.Now()
	if err := uc.userBindingRepo.Update(ctx, binding); err != nil {
		return nil, err
	}
	return binding, nil
}

func buildUserBinding(req *SaveUserBindingRequest) *types.WeComUserBinding {
	status := req.Status
	if status == "" {
		status = types.UserStatusActive
	}
	return &types.WeComUserBinding{
		CRMUserID:               req.CRMUserID,
		Username:                strings.TrimSpace(req.Username),
		WeComUserID:             strings.TrimSpace(req.Username),
		GroupRobotMentionUserID: strings.TrimSpace(req.GroupRobotMentionUserID),
		EmployeeName:            req.EmployeeName,
		Role:                    req.Role,
		DepartmentID:            req.DepartmentID,
		Status:                  status,
	}
}

func validateGroupRobotMentionUserID(value string) error {
	if len(value) > 128 {
		return fmt.Errorf("group robot mention UserID must contain at most 128 characters")
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return fmt.Errorf("group robot mention UserID must not contain whitespace")
	}
	return nil
}

func validateAccountCredentials(username, password string, passwordRequired bool) error {
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 64 {
		return fmt.Errorf("username must contain 3-64 characters")
	}
	if strings.ContainsAny(username, " \t\r\n") {
		return fmt.Errorf("username must not contain whitespace")
	}
	if passwordRequired || password != "" {
		if len(password) < 8 || len(password) > 72 {
			return fmt.Errorf("password must contain 8-72 characters")
		}
	}
	return nil
}
