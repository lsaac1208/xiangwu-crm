package usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

var (
	ErrBindingRequestNotFound = errors.New("binding request not found")
	ErrBindingRequestReviewed = errors.New("binding request already reviewed")
	ErrUserAlreadyBound       = errors.New("wecom user already bound")
)

// UserBindingRequestsUseCase 管理企业微信用户绑定申请。
type UserBindingRequestsUseCase struct {
	requestRepo     repo.UserBindingRequestRepository
	userBindingRepo repo.UserBindingRepository
	txManager       repo.TransactionManager
}

func (uc *UserBindingRequestsUseCase) WithTransactionManager(txManager repo.TransactionManager) *UserBindingRequestsUseCase {
	uc.txManager = txManager
	return uc
}

func NewUserBindingRequestsUseCase(
	requestRepo repo.UserBindingRequestRepository,
	userBindingRepo repo.UserBindingRepository,
) *UserBindingRequestsUseCase {
	return &UserBindingRequestsUseCase{
		requestRepo:     requestRepo,
		userBindingRepo: userBindingRepo,
	}
}

type SubmitBindingRequest struct {
	WeComUserID  string
	EmployeeName string
	Mobile       string
	Department   string
	Remark       string
}

type ListBindingRequestsRequest struct {
	Status   string
	Page     int
	PageSize int
}

type ListBindingRequestsResponse struct {
	Requests   []*types.WeComUserBindingRequest `json:"requests"`
	Total      int                              `json:"total"`
	Page       int                              `json:"page"`
	PageSize   int                              `json:"page_size"`
	TotalPages int                              `json:"total_pages"`
}

type ReviewBindingRequest struct {
	ID           int64
	Action       string
	CRMUserID    int64
	EmployeeName string
	Role         types.UserRole
	DepartmentID int64
	RejectReason string
	ReviewedBy   string
}

func (uc *UserBindingRequestsUseCase) Submit(ctx context.Context, req *SubmitBindingRequest) (*types.WeComUserBindingRequest, error) {
	wecomUserID := strings.TrimSpace(req.WeComUserID)
	employeeName := strings.TrimSpace(req.EmployeeName)
	if wecomUserID == "" || employeeName == "" {
		return nil, errors.New("wecom_userid and employee_name are required")
	}

	binding, err := uc.userBindingRepo.GetByWeComUserID(ctx, wecomUserID)
	if err != nil {
		return nil, err
	}
	if binding != nil && binding.Status == types.UserStatusActive {
		return nil, ErrUserAlreadyBound
	}

	now := time.Now()
	existing, err := uc.requestRepo.GetLatestByWeComUserID(ctx, wecomUserID)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.Status == types.BindingRequestStatusPending {
		existing.EmployeeName = employeeName
		existing.Mobile = strings.TrimSpace(req.Mobile)
		existing.Department = strings.TrimSpace(req.Department)
		existing.Remark = strings.TrimSpace(req.Remark)
		existing.UpdatedAt = now
		if err := uc.requestRepo.Update(ctx, existing); err != nil {
			return nil, err
		}
		return existing, nil
	}

	request := &types.WeComUserBindingRequest{
		WeComUserID:  wecomUserID,
		EmployeeName: employeeName,
		Mobile:       strings.TrimSpace(req.Mobile),
		Department:   strings.TrimSpace(req.Department),
		Remark:       strings.TrimSpace(req.Remark),
		Status:       types.BindingRequestStatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := uc.requestRepo.Create(ctx, request); err != nil {
		return nil, err
	}
	return request, nil
}

func (uc *UserBindingRequestsUseCase) LatestByWeComUserID(ctx context.Context, wecomUserID string) (*types.WeComUserBindingRequest, error) {
	return uc.requestRepo.GetLatestByWeComUserID(ctx, strings.TrimSpace(wecomUserID))
}

func (uc *UserBindingRequestsUseCase) List(ctx context.Context, req *ListBindingRequestsRequest) (*ListBindingRequestsResponse, error) {
	if req.Page < 1 {
		req.Page = 1
	}
	if req.PageSize < 1 || req.PageSize > 100 {
		req.PageSize = 20
	}
	offset := (req.Page - 1) * req.PageSize

	requests, total, err := uc.requestRepo.List(ctx, req.Status, offset, req.PageSize)
	if err != nil {
		return nil, err
	}
	return &ListBindingRequestsResponse{
		Requests:   requests,
		Total:      total,
		Page:       req.Page,
		PageSize:   req.PageSize,
		TotalPages: (total + req.PageSize - 1) / req.PageSize,
	}, nil
}

func (uc *UserBindingRequestsUseCase) Review(ctx context.Context, req *ReviewBindingRequest) (*types.WeComUserBindingRequest, error) {
	var reviewed *types.WeComUserBindingRequest
	err := uc.withReviewTransaction(ctx, func(ctx context.Context, requestRepo repo.UserBindingRequestRepository, bindingRepo repo.UserBindingRepository) error {
		request, err := uc.review(ctx, req, requestRepo, bindingRepo)
		if err != nil {
			return err
		}
		reviewed = request
		return nil
	})
	return reviewed, err
}

func (uc *UserBindingRequestsUseCase) review(ctx context.Context, req *ReviewBindingRequest, requestRepo repo.UserBindingRequestRepository, bindingRepo repo.UserBindingRepository) (*types.WeComUserBindingRequest, error) {
	request, err := requestRepo.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, ErrBindingRequestNotFound
	}
	if request.Status != types.BindingRequestStatusPending {
		return nil, ErrBindingRequestReviewed
	}

	now := time.Now()
	request.ReviewedBy = strings.TrimSpace(req.ReviewedBy)
	request.ReviewedAt = &now
	request.UpdatedAt = now

	switch req.Action {
	case "approve":
		if !validRole(req.Role) {
			return nil, errors.New("role is required")
		}
		name := strings.TrimSpace(req.EmployeeName)
		if name == "" {
			name = request.EmployeeName
		}
		binding := &types.WeComUserBinding{
			CRMUserID:    req.CRMUserID,
			WeComUserID:  request.WeComUserID,
			EmployeeName: name,
			Role:         req.Role,
			DepartmentID: req.DepartmentID,
			Status:       types.UserStatusActive,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := bindingRepo.Create(ctx, binding); err != nil {
			return nil, err
		}
		req.CRMUserID = binding.CRMUserID
		request.Status = types.BindingRequestStatusApproved
		request.CRMUserID = req.CRMUserID
		request.EmployeeName = name
		request.Role = req.Role
		request.DepartmentID = req.DepartmentID
	case "reject":
		request.Status = types.BindingRequestStatusRejected
		request.RejectReason = strings.TrimSpace(req.RejectReason)
	default:
		return nil, errors.New("invalid review action")
	}

	if err := requestRepo.Update(ctx, request); err != nil {
		return nil, err
	}
	return request, nil
}

func (uc *UserBindingRequestsUseCase) withReviewTransaction(ctx context.Context, fn func(context.Context, repo.UserBindingRequestRepository, repo.UserBindingRepository) error) error {
	if uc.txManager == nil {
		return fn(ctx, uc.requestRepo, uc.userBindingRepo)
	}
	return uc.txManager.WithinTransaction(ctx, func(ctx context.Context, repos repo.TransactionRepositories) error {
		return fn(ctx, repos.UserBindingRequestRepository(), repos.UserBindingRepository())
	})
}

func validRole(role types.UserRole) bool {
	return types.IsValidUserRole(role)
}
