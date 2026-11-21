package repo

import (
	"time"

	"github.com/lsaac128/kxone/pkg/types"
	"gorm.io/gorm"
)

// LeadModel 线索数据模型
type LeadModel struct {
	ID                   int64  `gorm:"primaryKey;autoIncrement"`
	LeadNo               string `gorm:"type:varchar(64);uniqueIndex;not null"`
	CustomerName         string `gorm:"type:varchar(64)"`
	CustomerMobile       string `gorm:"type:varchar(512)"` // 仓储层加密存储
	CustomerMobileMasked string `gorm:"type:varchar(32)"`
	CustomerMobileHash   string `gorm:"type:varchar(96);index"`
	CustomerWechat       string `gorm:"type:varchar(512)"` // 仓储层加密存储
	CustomerWechatMasked string `gorm:"type:varchar(255)"`
	City                 string `gorm:"type:varchar(64)"`
	ProductName          string `gorm:"type:varchar(128)"`
	SourceChannel        string `gorm:"type:varchar(128)"`
	SourceCampaign       string `gorm:"type:varchar(512)"`
	ContentEntryID       *int64 `gorm:"index"`
	ContentRevisionID    *int64 `gorm:"index"`
	ContentPath          string `gorm:"type:varchar(512);index"`
	LeadLevel            string `gorm:"type:varchar(32)"`
	BusinessStage        string `gorm:"type:varchar(32);index:idx_leads_workflow_owner_due,priority:1"`
	OwnershipState       string `gorm:"type:varchar(32);index:idx_leads_workflow_owner_due,priority:2"`
	ResultStatus         string `gorm:"type:varchar(32);index"`
	StarRating           int    `gorm:"not null;default:0"`

	Status         string `gorm:"type:varchar(64);index;index:idx_leads_status_created,priority:1;index:idx_leads_status_assigned,priority:1;index:idx_leads_status_updated,priority:1;index:idx_leads_owner_status_updated,priority:2;index:idx_leads_assigned_status_updated,priority:2;not null"`
	Version        int    `gorm:"not null;default:0"`
	ProfileVersion int    `gorm:"not null;default:1"`

	DispatchManagerUserID string `gorm:"type:varchar(128);index"`
	AssignedSalesUserID   string `gorm:"type:varchar(128);index;index:idx_leads_assigned_status_updated,priority:1"`
	OwnerUserID           string `gorm:"type:varchar(128);index;index:idx_leads_owner_status_updated,priority:1"`
	AssignedInviterUserID string `gorm:"type:varchar(128);index"`
	InviterUserID         string `gorm:"type:varchar(128);index"`
	CloserUserID          string `gorm:"type:varchar(128);index"`
	CurrentOwnerUserID    string `gorm:"type:varchar(128);index:idx_leads_workflow_owner_due,priority:3"`
	CurrentOwnerRole      string `gorm:"type:varchar(32)"`
	CurrentAppointmentID  *int64 `gorm:"index"`
	CreatedByUserID       string `gorm:"type:varchar(128);index"`
	CreatedByRole         string `gorm:"type:varchar(64);index"`

	CreatedAt                   time.Time  `gorm:"not null;index;index:idx_leads_status_created,priority:2"`
	AssignedAt                  *time.Time `gorm:"index:idx_leads_status_assigned,priority:2"`
	ClaimedAt                   *time.Time
	FirstFollowupAt             *time.Time
	CurrentOwnerStartedAt       *time.Time
	CurrentOwnerFirstFollowupAt *time.Time
	LastFollowupAt              *time.Time
	NextFollowupDueAt           *time.Time `gorm:"index:idx_leads_workflow_owner_due,priority:4"`
	InviterAssignedAt           *time.Time
	InviterClaimedAt            *time.Time
	ArrivedAt                   *time.Time
	DeliveredAt                 *time.Time
	PoolEnteredAt               *time.Time
	PoolReason                  string         `gorm:"type:varchar(128)"`
	ConvertedAt                 *time.Time     `gorm:"index"`
	ClosedAt                    *time.Time     `gorm:"index"`
	DealAmount                  float64        `gorm:"not null;default:0"`
	PaymentStatus               string         `gorm:"type:varchar(16);not null;default:''"`
	RefundAmount                float64        `gorm:"not null;default:0"`
	CloseReason                 string         `gorm:"type:varchar(255)"`
	UpdatedAt                   time.Time      `gorm:"not null;index:idx_leads_status_updated,priority:2;index:idx_leads_owner_status_updated,priority:3;index:idx_leads_assigned_status_updated,priority:3"`
	DeletedAt                   gorm.DeletedAt `gorm:"index"`
}

func (LeadModel) TableName() string {
	return "leads"
}

// ToEntity 转换为领域实体
func (m *LeadModel) ToEntity() *types.Lead {
	lead := &types.Lead{
		ID:                    m.ID,
		LeadNo:                m.LeadNo,
		CustomerName:          m.CustomerName,
		CustomerMobile:        m.CustomerMobile,
		CustomerMobileMasked:  m.CustomerMobileMasked,
		CustomerMobileHash:    m.CustomerMobileHash,
		CustomerWechat:        m.CustomerWechat,
		CustomerWechatMasked:  m.CustomerWechatMasked,
		City:                  m.City,
		ProductName:           m.ProductName,
		SourceChannel:         m.SourceChannel,
		SourceCampaign:        m.SourceCampaign,
		ContentEntryID:        m.ContentEntryID,
		ContentRevisionID:     m.ContentRevisionID,
		ContentPath:           m.ContentPath,
		LeadLevel:             m.LeadLevel,
		BusinessStage:         types.BusinessStage(m.BusinessStage),
		OwnershipState:        types.OwnershipState(m.OwnershipState),
		ResultStatus:          types.LeadResultStatus(m.ResultStatus),
		StarRating:            m.StarRating,
		Status:                types.LeadStatus(m.Status),
		Version:               m.Version,
		ProfileVersion:        m.ProfileVersion,
		DispatchManagerUserID: m.DispatchManagerUserID,
		AssignedSalesUserID:   m.AssignedSalesUserID,
		OwnerUserID:           m.OwnerUserID,
		AssignedInviterUserID: m.AssignedInviterUserID,
		InviterUserID:         m.InviterUserID,
		CloserUserID:          m.CloserUserID,
		CurrentOwnerUserID:    m.CurrentOwnerUserID,
		CurrentOwnerRole:      types.OwnerRole(m.CurrentOwnerRole),
		CurrentAppointmentID:  m.CurrentAppointmentID,
		CreatedByUserID:       m.CreatedByUserID,
		CreatedByRole:         types.UserRole(m.CreatedByRole),
		CreatedAt:             m.CreatedAt,
		AssignedAt:            m.AssignedAt,
		ClaimedAt:             m.ClaimedAt,
		FirstFollowupAt:       m.FirstFollowupAt,
		CurrentOwnerStartedAt: m.CurrentOwnerStartedAt, CurrentOwnerFirstFollowupAt: m.CurrentOwnerFirstFollowupAt,
		LastFollowupAt:    m.LastFollowupAt,
		NextFollowupDueAt: m.NextFollowupDueAt,
		InviterAssignedAt: m.InviterAssignedAt,
		InviterClaimedAt:  m.InviterClaimedAt,
		ArrivedAt:         m.ArrivedAt,
		DeliveredAt:       m.DeliveredAt,
		PoolEnteredAt:     m.PoolEnteredAt,
		PoolReason:        m.PoolReason,
		ConvertedAt:       m.ConvertedAt,
		ClosedAt:          m.ClosedAt,
		DealAmount:        m.DealAmount,
		PaymentStatus:     m.PaymentStatus,
		RefundAmount:      m.RefundAmount,
		CloseReason:       m.CloseReason,
		UpdatedAt:         m.UpdatedAt,
	}
	types.ProjectLegacyWorkflow(lead)
	return lead
}

// FromEntity 从领域实体转换
func (m *LeadModel) FromEntity(e *types.Lead) {
	types.ProjectLegacyWorkflow(e)
	m.ID = e.ID
	m.LeadNo = e.LeadNo
	m.CustomerName = e.CustomerName
	m.CustomerMobile = e.CustomerMobile
	m.CustomerMobileMasked = e.CustomerMobileMasked
	m.CustomerMobileHash = e.CustomerMobileHash
	m.CustomerWechat = e.CustomerWechat
	m.CustomerWechatMasked = e.CustomerWechatMasked
	m.City = e.City
	m.ProductName = e.ProductName
	m.SourceChannel = e.SourceChannel
	m.SourceCampaign = e.SourceCampaign
	m.ContentEntryID = e.ContentEntryID
	m.ContentRevisionID = e.ContentRevisionID
	m.ContentPath = e.ContentPath
	m.LeadLevel = e.LeadLevel
	m.BusinessStage = string(e.BusinessStage)
	m.OwnershipState = string(e.OwnershipState)
	m.ResultStatus = string(e.ResultStatus)
	m.StarRating = e.StarRating
	m.Status = string(e.Status)
	m.Version = e.Version
	m.ProfileVersion = e.ProfileVersion
	m.DispatchManagerUserID = e.DispatchManagerUserID
	m.AssignedSalesUserID = e.AssignedSalesUserID
	m.OwnerUserID = e.OwnerUserID
	m.AssignedInviterUserID = e.AssignedInviterUserID
	m.InviterUserID = e.InviterUserID
	m.CloserUserID = e.CloserUserID
	m.CurrentOwnerUserID = e.CurrentOwnerUserID
	m.CurrentOwnerRole = string(e.CurrentOwnerRole)
	m.CurrentAppointmentID = e.CurrentAppointmentID
	m.CreatedByUserID = e.CreatedByUserID
	m.CreatedByRole = string(e.CreatedByRole)
	m.CreatedAt = e.CreatedAt
	m.AssignedAt = e.AssignedAt
	m.ClaimedAt = e.ClaimedAt
	m.FirstFollowupAt = e.FirstFollowupAt
	m.CurrentOwnerStartedAt = e.CurrentOwnerStartedAt
	m.CurrentOwnerFirstFollowupAt = e.CurrentOwnerFirstFollowupAt
	m.LastFollowupAt = e.LastFollowupAt
	m.NextFollowupDueAt = e.NextFollowupDueAt
	m.InviterAssignedAt = e.InviterAssignedAt
	m.InviterClaimedAt = e.InviterClaimedAt
	m.ArrivedAt = e.ArrivedAt
	m.DeliveredAt = e.DeliveredAt
	m.PoolEnteredAt = e.PoolEnteredAt
	m.PoolReason = e.PoolReason
	m.ConvertedAt = e.ConvertedAt
	m.ClosedAt = e.ClosedAt
	m.DealAmount = e.DealAmount
	m.PaymentStatus = e.PaymentStatus
	m.RefundAmount = e.RefundAmount
	m.CloseReason = e.CloseReason
	m.UpdatedAt = e.UpdatedAt
}

// LeadDispatchRecordModel 分配记录模型
type LeadDispatchRecordModel struct {
	ID            int64     `gorm:"primaryKey;autoIncrement"`
	LeadID        int64     `gorm:"index;index:idx_dispatch_actor_action_lead,priority:3;not null"`
	ManagerUserID string    `gorm:"type:varchar(128);index:idx_dispatch_actor_action_lead,priority:1;not null"`
	SalesUserID   string    `gorm:"type:varchar(128);not null"`
	Remark        string    `gorm:"type:varchar(512)"`
	ActionType    string    `gorm:"type:varchar(64);index:idx_dispatch_actor_action_lead,priority:2;not null"`
	CreatedAt     time.Time `gorm:"not null;index"`
}

func (LeadDispatchRecordModel) TableName() string {
	return "lead_dispatch_records"
}

func (m *LeadDispatchRecordModel) ToEntity() *types.LeadDispatchRecord {
	return &types.LeadDispatchRecord{
		ID:            m.ID,
		LeadID:        m.LeadID,
		ManagerUserID: m.ManagerUserID,
		SalesUserID:   m.SalesUserID,
		Remark:        m.Remark,
		ActionType:    m.ActionType,
		CreatedAt:     m.CreatedAt,
	}
}

func (m *LeadDispatchRecordModel) FromEntity(e *types.LeadDispatchRecord) {
	m.ID = e.ID
	m.LeadID = e.LeadID
	m.ManagerUserID = e.ManagerUserID
	m.SalesUserID = e.SalesUserID
	m.Remark = e.Remark
	m.ActionType = e.ActionType
	m.CreatedAt = e.CreatedAt
}

// LeadFollowupModel 跟进记录模型
type LeadFollowupModel struct {
	ID             int64  `gorm:"primaryKey;autoIncrement"`
	LeadID         int64  `gorm:"index;not null"`
	SalesUserID    string `gorm:"type:varchar(128);not null"`
	FollowupResult string `gorm:"type:varchar(64);not null"`
	IntentionLevel string `gorm:"type:varchar(64)"`
	BusinessStage  string `gorm:"type:varchar(32);index"`
	ActorRole      string `gorm:"type:varchar(32)"`
	StarRating     int    `gorm:"not null;default:0"`
	DueAtSnapshot  *time.Time
	Remark         string `gorm:"type:text;not null"`
	NextFollowupAt *time.Time
	IsValid        bool      `gorm:"not null;default:true"`
	InvalidReason  string    `gorm:"type:varchar(128)"`
	CreatedAt      time.Time `gorm:"not null;index"`
}

func (LeadFollowupModel) TableName() string {
	return "lead_followups"
}

func (m *LeadFollowupModel) ToEntity() *types.LeadFollowup {
	return &types.LeadFollowup{
		ID:             m.ID,
		LeadID:         m.LeadID,
		SalesUserID:    m.SalesUserID,
		FollowupResult: m.FollowupResult,
		IntentionLevel: m.IntentionLevel,
		BusinessStage:  types.BusinessStage(m.BusinessStage),
		ActorRole:      types.OwnerRole(m.ActorRole),
		StarRating:     m.StarRating,
		DueAtSnapshot:  m.DueAtSnapshot,
		Remark:         m.Remark,
		NextFollowupAt: m.NextFollowupAt,
		IsValid:        m.IsValid,
		InvalidReason:  m.InvalidReason,
		CreatedAt:      m.CreatedAt,
	}
}

func (m *LeadFollowupModel) FromEntity(e *types.LeadFollowup) {
	m.ID = e.ID
	m.LeadID = e.LeadID
	m.SalesUserID = e.SalesUserID
	m.FollowupResult = e.FollowupResult
	m.IntentionLevel = e.IntentionLevel
	m.BusinessStage = string(e.BusinessStage)
	m.ActorRole = string(e.ActorRole)
	m.StarRating = e.StarRating
	m.DueAtSnapshot = e.DueAtSnapshot
	m.Remark = e.Remark
	m.NextFollowupAt = e.NextFollowupAt
	m.IsValid = e.IsValid
	m.InvalidReason = e.InvalidReason
	m.CreatedAt = e.CreatedAt
}

// WeComCardTaskModel 企业微信卡片任务模型
type WeComCardTaskModel struct {
	ID                   int64      `gorm:"primaryKey;autoIncrement"`
	LeadID               int64      `gorm:"index;index:idx_card_lead_scene_sent,priority:1;not null"`
	CardScene            string     `gorm:"type:varchar(64);index:idx_card_lead_scene_sent,priority:2;not null"`
	ReceiverUserID       string     `gorm:"type:varchar(128);not null"`
	TaskID               string     `gorm:"type:varchar(128);uniqueIndex;not null"`
	MsgID                string     `gorm:"type:varchar(128)"`
	ResponseCode         string     `gorm:"type:varchar(256);index"`
	CardStatus           string     `gorm:"type:varchar(64);index;index:idx_card_status_retry,priority:1;not null"`
	RetryCount           int        `gorm:"not null;default:0"`
	LastError            string     `gorm:"type:text"`
	NextRetryAt          *time.Time `gorm:"index;index:idx_card_status_retry,priority:2"`
	SentAt               time.Time  `gorm:"not null;index;index:idx_card_lead_scene_sent,priority:3"`
	UpdatedAt            *time.Time
	ResponseCodeExpireAt *time.Time `gorm:"index"`
}

func (WeComCardTaskModel) TableName() string {
	return "wecom_card_tasks"
}

func (m *WeComCardTaskModel) ToEntity() *types.WeComCardTask {
	return &types.WeComCardTask{
		ID:                   m.ID,
		LeadID:               m.LeadID,
		CardScene:            types.CardScene(m.CardScene),
		ReceiverUserID:       m.ReceiverUserID,
		TaskID:               m.TaskID,
		MsgID:                m.MsgID,
		ResponseCode:         m.ResponseCode,
		CardStatus:           types.CardStatus(m.CardStatus),
		RetryCount:           m.RetryCount,
		LastError:            m.LastError,
		NextRetryAt:          m.NextRetryAt,
		SentAt:               m.SentAt,
		UpdatedAt:            m.UpdatedAt,
		ResponseCodeExpireAt: m.ResponseCodeExpireAt,
	}
}

func (m *WeComCardTaskModel) FromEntity(e *types.WeComCardTask) {
	m.ID = e.ID
	m.LeadID = e.LeadID
	m.CardScene = string(e.CardScene)
	m.ReceiverUserID = e.ReceiverUserID
	m.TaskID = e.TaskID
	m.MsgID = e.MsgID
	m.ResponseCode = e.ResponseCode
	m.CardStatus = string(e.CardStatus)
	m.RetryCount = e.RetryCount
	m.LastError = e.LastError
	m.NextRetryAt = e.NextRetryAt
	m.SentAt = e.SentAt
	m.UpdatedAt = e.UpdatedAt
	m.ResponseCodeExpireAt = e.ResponseCodeExpireAt
}

// WeComCallbackLogModel 企业微信回调日志模型
type WeComCallbackLogModel struct {
	ID            int64     `gorm:"primaryKey;autoIncrement"`
	EventID       string    `gorm:"type:varchar(128);index"`
	TaskID        string    `gorm:"type:varchar(128);index;uniqueIndex:idx_callback_task_user_event,priority:1"`
	LeadID        *int64    `gorm:"index"`
	FromUserID    string    `gorm:"type:varchar(128);index;uniqueIndex:idx_callback_task_user_event,priority:2"`
	EventKey      string    `gorm:"type:varchar(128);index;uniqueIndex:idx_callback_task_user_event,priority:3"`
	RawPayload    string    `gorm:"type:text;not null"`
	ProcessStatus string    `gorm:"type:varchar(64);index;not null"`
	ErrorMessage  string    `gorm:"type:text"`
	CreatedAt     time.Time `gorm:"not null;index"`
}

func (WeComCallbackLogModel) TableName() string {
	return "wecom_callback_logs"
}

func (m *WeComCallbackLogModel) ToEntity() *types.WeComCallbackLog {
	return &types.WeComCallbackLog{
		ID:            m.ID,
		EventID:       m.EventID,
		TaskID:        m.TaskID,
		LeadID:        m.LeadID,
		FromUserID:    m.FromUserID,
		EventKey:      m.EventKey,
		RawPayload:    m.RawPayload,
		ProcessStatus: m.ProcessStatus,
		ErrorMessage:  m.ErrorMessage,
		CreatedAt:     m.CreatedAt,
	}
}

func (m *WeComCallbackLogModel) FromEntity(e *types.WeComCallbackLog) {
	m.ID = e.ID
	m.EventID = e.EventID
	m.TaskID = e.TaskID
	m.LeadID = e.LeadID
	m.FromUserID = e.FromUserID
	m.EventKey = e.EventKey
	m.RawPayload = e.RawPayload
	m.ProcessStatus = e.ProcessStatus
	m.ErrorMessage = e.ErrorMessage
	m.CreatedAt = e.CreatedAt
}

// WeComUserBindingModel 企业微信用户绑定模型
type WeComUserBindingModel struct {
	AuthVersion    int64  `gorm:"not null;default:1"`
	AccountVersion int64  `gorm:"not null;default:1"`
	ID             int64  `gorm:"primaryKey;autoIncrement"`
	CRMUserID      int64  `gorm:"column:crm_user_id;uniqueIndex;not null"`
	Username       string `gorm:"column:username;type:varchar(128);uniqueIndex"`
	PasswordHash   string `gorm:"column:password_hash;type:varchar(255)"`
	WeComUserID    string `gorm:"column:wecom_userid;type:varchar(128);uniqueIndex;not null"`
	// The SQL migration owns this index because SQLite needs a partial unique
	// index while MySQL uses a regular index. Letting AutoMigrate infer another
	// index makes a second SQLite development startup attempt an invalid table
	// rebuild around the partial index.
	GroupRobotMentionUserID string    `gorm:"column:group_robot_mention_userid;type:varchar(128)"`
	EmployeeName            string    `gorm:"type:varchar(128)"`
	Role                    string    `gorm:"type:varchar(64);not null"`
	DepartmentID            int64     `gorm:"column:department_id;index"`
	Status                  string    `gorm:"type:varchar(64);index;not null"`
	CreatedAt               time.Time `gorm:"not null;index"`
	UpdatedAt               time.Time `gorm:"not null"`
}

func (WeComUserBindingModel) TableName() string {
	return "crm_wecom_user_bindings"
}

func (m *WeComUserBindingModel) ToEntity() *types.WeComUserBinding {
	return &types.WeComUserBinding{
		AuthVersion: m.AuthVersion, AccountVersion: m.AccountVersion,
		ID:                      m.ID,
		CRMUserID:               m.CRMUserID,
		Username:                m.Username,
		PasswordHash:            m.PasswordHash,
		WeComUserID:             m.WeComUserID,
		GroupRobotMentionUserID: m.GroupRobotMentionUserID,
		EmployeeName:            m.EmployeeName,
		Role:                    types.UserRole(m.Role),
		DepartmentID:            m.DepartmentID,
		Status:                  types.UserStatus(m.Status),
		CreatedAt:               m.CreatedAt,
		UpdatedAt:               m.UpdatedAt,
	}
}

func (m *WeComUserBindingModel) FromEntity(e *types.WeComUserBinding) {
	m.AuthVersion = e.AuthVersion
	m.AccountVersion = e.AccountVersion
	m.ID = e.ID
	m.CRMUserID = e.CRMUserID
	m.Username = e.Username
	if m.Username == "" {
		m.Username = e.WeComUserID
	}
	m.PasswordHash = e.PasswordHash
	m.WeComUserID = e.WeComUserID
	m.GroupRobotMentionUserID = e.GroupRobotMentionUserID
	m.EmployeeName = e.EmployeeName
	m.Role = string(e.Role)
	m.DepartmentID = e.DepartmentID
	m.Status = string(e.Status)
	m.CreatedAt = e.CreatedAt
	m.UpdatedAt = e.UpdatedAt
}

// WeComUserBindingRequestModel 企业微信用户绑定申请模型
type WeComUserBindingRequestModel struct {
	ID           int64      `gorm:"primaryKey;autoIncrement"`
	WeComUserID  string     `gorm:"column:wecom_userid;type:varchar(128);index;not null"`
	EmployeeName string     `gorm:"type:varchar(128);not null"`
	Mobile       string     `gorm:"type:varchar(64)"`
	Department   string     `gorm:"type:varchar(128)"`
	Remark       string     `gorm:"type:varchar(500)"`
	Status       string     `gorm:"type:varchar(64);index;not null"`
	CRMUserID    int64      `gorm:"column:crm_user_id"`
	Role         string     `gorm:"type:varchar(64)"`
	DepartmentID int64      `gorm:"column:department_id"`
	RejectReason string     `gorm:"type:varchar(500)"`
	ReviewedBy   string     `gorm:"type:varchar(128)"`
	ReviewedAt   *time.Time `gorm:"index"`
	CreatedAt    time.Time  `gorm:"not null;index"`
	UpdatedAt    time.Time  `gorm:"not null"`
}

func (WeComUserBindingRequestModel) TableName() string {
	return "crm_wecom_user_binding_requests"
}

func (m *WeComUserBindingRequestModel) ToEntity() *types.WeComUserBindingRequest {
	return &types.WeComUserBindingRequest{
		ID:           m.ID,
		WeComUserID:  m.WeComUserID,
		EmployeeName: m.EmployeeName,
		Mobile:       m.Mobile,
		Department:   m.Department,
		Remark:       m.Remark,
		Status:       types.BindingRequestStatus(m.Status),
		CRMUserID:    m.CRMUserID,
		Role:         types.UserRole(m.Role),
		DepartmentID: m.DepartmentID,
		RejectReason: m.RejectReason,
		ReviewedBy:   m.ReviewedBy,
		ReviewedAt:   m.ReviewedAt,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}
}

func (m *WeComUserBindingRequestModel) FromEntity(e *types.WeComUserBindingRequest) {
	m.ID = e.ID
	m.WeComUserID = e.WeComUserID
	m.EmployeeName = e.EmployeeName
	m.Mobile = e.Mobile
	m.Department = e.Department
	m.Remark = e.Remark
	m.Status = string(e.Status)
	m.CRMUserID = e.CRMUserID
	m.Role = string(e.Role)
	m.DepartmentID = e.DepartmentID
	m.RejectReason = e.RejectReason
	m.ReviewedBy = e.ReviewedBy
	m.ReviewedAt = e.ReviewedAt
	m.CreatedAt = e.CreatedAt
	m.UpdatedAt = e.UpdatedAt
}

// SystemSettingModel 系统配置模型
type SystemSettingModel struct {
	ID          int64     `gorm:"primaryKey;autoIncrement"`
	Key         string    `gorm:"column:setting_key;type:varchar(128);uniqueIndex;not null"`
	Value       string    `gorm:"column:setting_value;type:text"`
	ValueType   string    `gorm:"type:varchar(32);not null;default:'string'"`
	Group       string    `gorm:"column:setting_group;type:varchar(64);index;not null"`
	Description string    `gorm:"type:varchar(255)"`
	IsSensitive bool      `gorm:"not null;default:false"`
	UpdatedBy   string    `gorm:"type:varchar(128)"`
	CreatedAt   time.Time `gorm:"not null;index"`
	UpdatedAt   time.Time `gorm:"not null"`
}

func (SystemSettingModel) TableName() string {
	return "system_settings"
}

func (m *SystemSettingModel) ToEntity() *types.SystemSetting {
	return &types.SystemSetting{
		ID:          m.ID,
		Key:         m.Key,
		Value:       m.Value,
		ValueType:   m.ValueType,
		Group:       m.Group,
		Description: m.Description,
		IsSensitive: m.IsSensitive,
		UpdatedBy:   m.UpdatedBy,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
}

func (m *SystemSettingModel) FromEntity(e *types.SystemSetting) {
	m.ID = e.ID
	m.Key = e.Key
	m.Value = e.Value
	m.ValueType = e.ValueType
	m.Group = e.Group
	m.Description = e.Description
	m.IsSensitive = e.IsSensitive
	m.UpdatedBy = e.UpdatedBy
	m.CreatedAt = e.CreatedAt
	m.UpdatedAt = e.UpdatedAt
}
