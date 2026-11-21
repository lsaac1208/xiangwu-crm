package repo

// NOTE: 具体 Repository 接口定义在各自的文件中：
// - LeadRepository: lead_repo.go
// - DispatchRecordRepository: dispatch_repo.go
// - FollowupRepository: followup_repo.go
// - CardTaskRepository: card_repo.go
// - CallbackLogRepository: callback_repo.go
// - UserBindingRepository: user_binding_repo.go

import (
	"context"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

// TransactionRepositories 事务中的仓储集合。
type TransactionRepositories interface {
	LeadCreateRequests() LeadCreateRequestRepository
	FollowupRequests() FollowupRequestRepository
	LeadRepository() LeadRepository
	XiangwuFormSubmissionRepository() XiangwuFormSubmissionRepository
	DispatchRecordRepository() DispatchRecordRepository
	FollowupRepository() FollowupRepository
	UserBindingRepository() UserBindingRepository
	UserBindingRequestRepository() UserBindingRequestRepository
	AppointmentRepository() AppointmentRepository
	CloserAvailabilityRepository() CloserAvailabilityRepository
	LeadPoolRepository() LeadPoolRepository
	LeadActivityRepository() LeadActivityRepository
	LeadReminderRepository() LeadReminderRepository
	SystemSettingRepository() SystemSettingRepository
	GroupRobotNotificationRepository() GroupRobotNotificationRepository
}

// TransactionManager 提供跨仓储事务边界。
type TransactionManager interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context, repos TransactionRepositories) error) error
}

// UserBindingRepository 用户绑定仓储接口（在 user_binding_repo.go 中定义完整接口）

// LeadRepository 线索数据仓储接口
type LeadRepository interface {
	// Create 创建线索
	Create(ctx context.Context, lead *types.Lead) error

	// GetByID 根据 ID 获取线索
	GetByID(ctx context.Context, id int64) (*types.Lead, error)

	// GetByLeadNo 根据线索编号获取线索
	GetByLeadNo(ctx context.Context, leadNo string) (*types.Lead, error)

	// Update 更新线索（使用乐观锁）
	Update(ctx context.Context, lead *types.Lead) error

	// AtomicClaim 原子性领取线索（乐观锁 + CAS）
	AtomicClaim(ctx context.Context, leadID int64, salesUserID string, expectedVersion, expectedProfileVersion int) (bool, error)

	// AtomicAssign 原子性分配待分配线索（乐观锁 + CAS）
	AtomicAssign(ctx context.Context, leadID int64, managerUserID, salesUserID string, expectedVersion, expectedProfileVersion int) (bool, error)

	// AtomicReassign 原子性转派线索（乐观锁 + CAS）
	AtomicReassign(ctx context.Context, leadID int64, managerUserID, newSalesUserID string, expectedVersion, expectedProfileVersion int) (bool, error)

	// ListPendingDispatch 查询待分配线索
	ListPendingDispatch(ctx context.Context, limit int) ([]*types.Lead, error)

	// ListPendingClaim 查询待领取线索
	ListPendingClaim(ctx context.Context, limit int) ([]*types.Lead, error)

	// ListAll 查询所有线索（管理员）
	ListAll(ctx context.Context, status string, offset, limit int) ([]*types.Lead, int, error)

	// ListAllFiltered 按条件查询所有线索（管理员）
	ListAllFiltered(ctx context.Context, filter LeadListFilter, offset, limit int) ([]*types.Lead, int, error)

	// ListByManager 查询主管负责的线索
	ListByManager(ctx context.Context, managerUserID, status string, offset, limit int) ([]*types.Lead, int, error)

	// ListByManagerFiltered 按条件查询主管负责的线索
	ListByManagerFiltered(ctx context.Context, managerUserID string, filter LeadListFilter, offset, limit int) ([]*types.Lead, int, error)

	// ListBySales 查询销售负责的线索
	ListBySales(ctx context.Context, salesUserID, status string, offset, limit int) ([]*types.Lead, int, error)

	// ListBySalesFiltered 按条件查询销售负责的线索
	ListBySalesFiltered(ctx context.Context, salesUserID string, filter LeadListFilter, offset, limit int) ([]*types.Lead, int, error)
}

// LeadProfileRepository updates customer-facing profile fields without
// changing the workflow version used by reminder and notification rules.
type LeadProfileRepository interface {
	AtomicUpdateProfile(ctx context.Context, lead *types.Lead, expectedBusinessVersion, expectedProfileVersion int) (bool, error)
	HasOtherActiveMobile(ctx context.Context, mobile string, excludeLeadID int64) (bool, error)
}

type LeadListFilter struct {
	NeedsActionUserID string
	Status            string
	Keyword           string
	SalesUserID       string
	SortBy            string
	SortOrder         string
	StartTime         *time.Time
	EndTime           *time.Time // exclusive
}

type PublicPoolLeadRepository interface {
	ListPublicPoolFiltered(ctx context.Context, filter LeadListFilter, offset, limit int) ([]*types.Lead, int, error)
	CountActiveInvitationByInviter(ctx context.Context, inviterUserID string) (int, error)
	ListAutoPoolCandidates(ctx context.Context, unclaimedBefore, followupBefore time.Time, limit int) ([]*types.Lead, error)
}

// DispatcherLeadRepository applies the restricted dispatcher visibility scope
// in the database so list pagination and direct-detail authorization stay
// consistent and cannot be bypassed with a lead ID.
type DispatcherLeadRepository interface {
	ListByDispatcherFiltered(ctx context.Context, dispatcherUserID string, scope types.DispatcherLeadScope, filter LeadListFilter, offset, limit int) ([]*types.Lead, int, error)
	CanDispatcherViewLead(ctx context.Context, leadID int64, dispatcherUserID string) (bool, error)
}

type WorkbenchLeadSnapshot struct {
	PendingDispatch   int
	PendingClaim      int
	OverdueFollowup   int
	NegotiationActive int
	HandledToday      int
	Leads             []*types.Lead
}

type WorkbenchLeadRepository interface {
	BuildWorkbenchLeadSnapshot(ctx context.Context, userID string, role types.UserRole, now time.Time, taskLimit int) (*WorkbenchLeadSnapshot, error)
}

// LeadStatsFilter contains the bounded filters used by dashboard and SLA
// queries. Implementations should apply every non-empty field in the database.
type LeadStatsFilter struct {
	Status        string
	ManagerUserID string
	SalesUserID   string
	StartTime     *time.Time
	EndTime       *time.Time // exclusive
}

// LeadStatsRepository is an optional optimized capability. Keeping it separate
// from LeadRepository preserves compatibility with lightweight test doubles.
type LeadStatsRepository interface {
	ListForStats(ctx context.Context, filter LeadStatsFilter, limit int) ([]*types.Lead, error)
}

// FollowupStatsRepository avoids scanning the global follow-up table for a
// scoped dashboard request.
type FollowupStatsRepository interface {
	ListByLeadIDs(ctx context.Context, leadIDs []int64) ([]*types.LeadFollowup, error)
}

// UserBindingBatchRepository resolves display names without an N+1 query loop.
type UserBindingBatchRepository interface {
	ListByWeComUserIDs(ctx context.Context, wecomUserIDs []string) (map[string]*types.WeComUserBinding, error)
}

// CardTaskObservabilityRepository exposes ambiguous external operations for
// alerting. Stale updating tasks are intentionally not retried automatically.
type CardTaskObservabilityRepository interface {
	CountStaleUpdating(ctx context.Context, before time.Time) (int64, error)
}

// CardTaskSceneRepository lists the latest card for every recipient in a scene.
// It is used when one business event is broadcast to multiple supervisors.
type CardTaskSceneRepository interface {
	ListLatestBySceneRecipients(ctx context.Context, leadID int64, scene types.CardScene) ([]*types.WeComCardTask, error)
}

// DispatchRecordRepository 分配记录仓储接口
type DispatchRecordRepository interface {
	// Create 创建分配记录
	Create(ctx context.Context, record *types.LeadDispatchRecord) error

	// ListByLeadID 查询线索的分配历史
	ListByLeadID(ctx context.Context, leadID int64) ([]*types.LeadDispatchRecord, error)

	// ListLatestByLeadIDs 批量查询每条线索最新的分配/转派记录
	ListLatestByLeadIDs(ctx context.Context, leadIDs []int64) (map[int64]*types.LeadDispatchRecord, error)
}

// FollowupRepository 跟进记录仓储接口
type FollowupRepository interface {
	// Create 创建跟进记录
	Create(ctx context.Context, followup *types.LeadFollowup) error

	// ListByLeadID 查询线索的跟进历史
	ListByLeadID(ctx context.Context, leadID int64) ([]*types.LeadFollowup, error)

	// ListAll 查询全部跟进记录（统计使用）。
	ListAll(ctx context.Context, offset, limit int) ([]*types.LeadFollowup, int, error)
}

// CardTaskRepository 企业微信卡片任务仓储接口
type CardTaskRepository interface {
	// Create 创建卡片任务
	Create(ctx context.Context, task *types.WeComCardTask) error

	// GetByID 根据 ID 获取卡片任务
	GetByID(ctx context.Context, id int64) (*types.WeComCardTask, error)
	GetByTaskID(ctx context.Context, taskID string) (*types.WeComCardTask, error)

	// GetLatestByScene 获取线索在指定场景下的最新卡片任务
	GetLatestByScene(ctx context.Context, leadID int64, scene types.CardScene) (*types.WeComCardTask, error)

	// Update 更新卡片任务
	Update(ctx context.Context, task *types.WeComCardTask) error

	// TryStartRetry atomically claims a failed task so duplicate schedulers cannot
	// execute the same external card operation concurrently.
	TryStartRetry(ctx context.Context, taskID int64, expectedRetryCount int) (bool, error)

	// TryStartUpdate atomically claims a sent/updated task before calling the
	// external update API, preventing duplicate Asynq deliveries from racing.
	TryStartUpdate(ctx context.Context, taskID int64, expectedStatus types.CardStatus) (bool, error)

	// ListFailedTasks 查询失败待重试的卡片任务
	ListFailedTasks(ctx context.Context, limit int) ([]*types.WeComCardTask, error)
}

// CallbackLogRepository 企业微信回调日志仓储接口
type CallbackLogRepository interface {
	// Create 创建回调日志
	Create(ctx context.Context, log *types.WeComCallbackLog) error

	// UpdateProcessResult 更新回调处理结果
	UpdateProcessResult(ctx context.Context, id int64, status, errorMessage string) error

	// ExistsByTaskAndUser 检查回调是否已存在（幂等检查）
	ExistsByTaskAndUser(ctx context.Context, taskID, fromUserID, eventKey string) (bool, error)
}
