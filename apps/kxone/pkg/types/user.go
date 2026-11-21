package types

import (
	"errors"
	"time"
)

var ErrAccountVersionConflict = errors.New("account version conflict")

// UserStatus 用户状态
type UserStatus string

const (
	UserStatusActive   UserStatus = "active"   // 活跃
	UserStatusDisabled UserStatus = "disabled" // 已禁用
	UserStatusDeleted  UserStatus = "deleted"  // 已删除
)

// UserRole 用户角色
type UserRole string

const (
	UserRoleAdmin      UserRole = "admin"      // 管理员
	UserRoleManager    UserRole = "manager"    // 主管
	UserRoleDispatcher UserRole = "dispatcher" // 线索调度员
	UserRoleSales      UserRole = "sales"      // 销售
	UserRoleInviter    UserRole = "inviter"    // 邀约师
	UserRoleCloser     UserRole = "closer"     // 谈单师
)

func IsValidUserRole(role UserRole) bool {
	return role == UserRoleAdmin || role == UserRoleManager || role == UserRoleDispatcher || role == UserRoleSales || role == UserRoleInviter || role == UserRoleCloser
}

func CanInitiallyAssignLead(role UserRole) bool {
	return role == UserRoleAdmin || role == UserRoleManager || role == UserRoleDispatcher
}

// IsInvitationRole keeps legacy sales accounts usable as inviters during rollout.
func IsInvitationRole(role UserRole) bool {
	return role == UserRoleSales || role == UserRoleInviter
}

// BindingRequestStatus 绑定申请状态
type BindingRequestStatus string

const (
	BindingRequestStatusPending  BindingRequestStatus = "pending"
	BindingRequestStatusApproved BindingRequestStatus = "approved"
	BindingRequestStatusRejected BindingRequestStatus = "rejected"
)

// WeComUserBinding 企业微信用户绑定
type WeComUserBinding struct {
	AuthVersion             int64      `json:"-"`
	AccountVersion          int64      `json:"version"`
	ID                      int64      `json:"id"`
	CRMUserID               int64      `json:"crm_user_id"`
	Username                string     `json:"username"`
	PasswordHash            string     `json:"-"`
	WeComUserID             string     `json:"wecom_userid"`
	GroupRobotMentionUserID string     `json:"group_robot_mention_userid,omitempty"`
	EmployeeName            string     `json:"employee_name"`
	Role                    UserRole   `json:"role"`
	DepartmentID            int64      `json:"department_id,omitempty"`
	Status                  UserStatus `json:"status"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
}

// WeComUserBindingRequest 企业微信用户绑定申请
type WeComUserBindingRequest struct {
	ID           int64                `json:"id"`
	WeComUserID  string               `json:"wecom_userid"`
	EmployeeName string               `json:"employee_name"`
	Mobile       string               `json:"mobile,omitempty"`
	Department   string               `json:"department,omitempty"`
	Remark       string               `json:"remark,omitempty"`
	Status       BindingRequestStatus `json:"status"`
	CRMUserID    int64                `json:"crm_user_id,omitempty"`
	Role         UserRole             `json:"role,omitempty"`
	DepartmentID int64                `json:"department_id,omitempty"`
	RejectReason string               `json:"reject_reason,omitempty"`
	ReviewedBy   string               `json:"reviewed_by,omitempty"`
	ReviewedAt   *time.Time           `json:"reviewed_at,omitempty"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
}
