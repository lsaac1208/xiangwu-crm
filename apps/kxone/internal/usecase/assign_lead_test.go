package usecase

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
	"github.com/lsaac128/kxone/pkg/types"
)

// MockLeadRepository 模拟线索仓储
type MockLeadRepository struct {
	mu          sync.Mutex
	leads       map[int64]*types.Lead
	updateCalls int
}

func NewMockLeadRepository() *MockLeadRepository {
	return &MockLeadRepository{
		leads: make(map[int64]*types.Lead),
	}
}

func (m *MockLeadRepository) GetByID(ctx context.Context, id int64) (*types.Lead, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lead := m.leads[id]
	if lead == nil {
		return nil, nil
	}
	// 返回副本，避免测试污染
	copy := *lead
	return &copy, nil
}

func (m *MockLeadRepository) Create(ctx context.Context, lead *types.Lead) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if lead.ID == 0 {
		lead.ID = int64(len(m.leads) + 1)
	}
	m.leads[lead.ID] = lead
	return nil
}

func (m *MockLeadRepository) GetByLeadNo(ctx context.Context, leadNo string) (*types.Lead, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, lead := range m.leads {
		if lead.LeadNo == leadNo {
			copy := *lead
			return &copy, nil
		}
	}
	return nil, nil
}

func (m *MockLeadRepository) Update(ctx context.Context, lead *types.Lead) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateCalls++
	m.leads[lead.ID] = lead
	return nil
}

func (m *MockLeadRepository) AtomicClaim(ctx context.Context, leadID int64, salesUserID string, expectedVersion, expectedProfileVersion int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lead := m.leads[leadID]
	if lead == nil ||
		lead.AssignedSalesUserID != salesUserID ||
		lead.Status != types.LeadStatusAssignedPendingClaim ||
		lead.Version != expectedVersion ||
		lead.ProfileVersion != expectedProfileVersion {
		return false, nil
	}

	now := time.Now()
	lead.OwnerUserID = salesUserID
	lead.ClaimedAt = &now
	lead.Status = types.LeadStatusClaimed
	lead.Version++
	lead.UpdatedAt = now
	return true, nil
}

func (m *MockLeadRepository) AtomicAssign(ctx context.Context, leadID int64, managerUserID, salesUserID string, expectedVersion, expectedProfileVersion int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lead := m.leads[leadID]
	if lead == nil ||
		lead.Status != types.LeadStatusPendingDispatch ||
		lead.Version != expectedVersion ||
		lead.ProfileVersion != expectedProfileVersion {
		return false, nil
	}

	now := time.Now()
	lead.DispatchManagerUserID = managerUserID
	lead.AssignedSalesUserID = salesUserID
	lead.AssignedAt = &now
	lead.Status = types.LeadStatusAssignedPendingClaim
	lead.Version++
	lead.UpdatedAt = now
	return true, nil
}

func (m *MockLeadRepository) AtomicReassign(ctx context.Context, leadID int64, managerUserID, newSalesUserID string, expectedVersion, expectedProfileVersion int) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lead := m.leads[leadID]
	if lead == nil ||
		lead.DispatchManagerUserID != managerUserID ||
		lead.Version != expectedVersion ||
		lead.ProfileVersion != expectedProfileVersion ||
		(lead.Status != types.LeadStatusAssignedPendingClaim && lead.Status != types.LeadStatusClaimed) {
		return false, nil
	}

	now := time.Now()
	lead.AssignedSalesUserID = newSalesUserID
	lead.OwnerUserID = ""
	lead.ClaimedAt = nil
	lead.AssignedAt = &now
	lead.Status = types.LeadStatusAssignedPendingClaim
	lead.Version++
	lead.UpdatedAt = now
	return true, nil
}

func (m *MockLeadRepository) ListPendingDispatch(ctx context.Context, limit int) ([]*types.Lead, error) {
	return nil, nil
}

func (m *MockLeadRepository) ListPendingClaim(ctx context.Context, limit int) ([]*types.Lead, error) {
	return nil, nil
}

func (m *MockLeadRepository) ListAll(ctx context.Context, status string, offset, limit int) ([]*types.Lead, int, error) {
	return nil, 0, nil
}

func (m *MockLeadRepository) ListAllFiltered(ctx context.Context, filter repo.LeadListFilter, offset, limit int) ([]*types.Lead, int, error) {
	return nil, 0, nil
}

func (m *MockLeadRepository) ListByManager(ctx context.Context, managerUserID, status string, offset, limit int) ([]*types.Lead, int, error) {
	return nil, 0, nil
}

func (m *MockLeadRepository) ListByManagerFiltered(ctx context.Context, managerUserID string, filter repo.LeadListFilter, offset, limit int) ([]*types.Lead, int, error) {
	return nil, 0, nil
}

func (m *MockLeadRepository) ListBySales(ctx context.Context, salesUserID, status string, offset, limit int) ([]*types.Lead, int, error) {
	return nil, 0, nil
}

func (m *MockLeadRepository) ListBySalesFiltered(ctx context.Context, salesUserID string, filter repo.LeadListFilter, offset, limit int) ([]*types.Lead, int, error) {
	return nil, 0, nil
}

// MockDispatchRecordRepository 模拟分配记录仓储
type MockDispatchRecordRepository struct {
	records []*types.LeadDispatchRecord
}

func NewMockDispatchRecordRepository() *MockDispatchRecordRepository {
	return &MockDispatchRecordRepository{}
}

func (m *MockDispatchRecordRepository) Create(ctx context.Context, record *types.LeadDispatchRecord) error {
	m.records = append(m.records, record)
	return nil
}

func (m *MockDispatchRecordRepository) ListByLeadID(ctx context.Context, leadID int64) ([]*types.LeadDispatchRecord, error) {
	var records []*types.LeadDispatchRecord
	for _, record := range m.records {
		if record.LeadID == leadID {
			records = append(records, record)
		}
	}
	return records, nil
}

func (m *MockDispatchRecordRepository) ListLatestByLeadIDs(ctx context.Context, leadIDs []int64) (map[int64]*types.LeadDispatchRecord, error) {
	latest := make(map[int64]*types.LeadDispatchRecord, len(leadIDs))
	allowed := make(map[int64]struct{}, len(leadIDs))
	for _, leadID := range leadIDs {
		allowed[leadID] = struct{}{}
	}
	for _, record := range m.records {
		if _, ok := allowed[record.LeadID]; !ok {
			continue
		}
		current := latest[record.LeadID]
		if current == nil || record.CreatedAt.After(current.CreatedAt) || (record.CreatedAt.Equal(current.CreatedAt) && record.ID > current.ID) {
			latest[record.LeadID] = record
		}
	}
	return latest, nil
}

// MockCardTaskRepository 模拟卡片任务仓储
type MockCardTaskRepository struct {
	cards       map[int64][]*types.WeComCardTask
	createCalls int
	updateCalls int
}

func NewMockCardTaskRepository() *MockCardTaskRepository {
	return &MockCardTaskRepository{
		cards: make(map[int64][]*types.WeComCardTask),
	}
}

func (m *MockCardTaskRepository) Create(ctx context.Context, task *types.WeComCardTask) error {
	m.createCalls++
	task.ID = int64(len(m.cards) + 1)
	m.cards[task.LeadID] = append(m.cards[task.LeadID], task)
	return nil
}

func (m *MockCardTaskRepository) Update(ctx context.Context, task *types.WeComCardTask) error {
	m.updateCalls++
	return nil
}

func (m *MockCardTaskRepository) GetByID(ctx context.Context, id int64) (*types.WeComCardTask, error) {
	for _, tasks := range m.cards {
		for _, task := range tasks {
			if task.ID == id {
				return task, nil
			}
		}
	}
	return nil, nil
}

func (m *MockCardTaskRepository) GetByTaskID(ctx context.Context, taskID string) (*types.WeComCardTask, error) {
	for _, tasks := range m.cards {
		for _, task := range tasks {
			if task.TaskID == taskID {
				return task, nil
			}
		}
	}
	return nil, nil
}

func (m *MockCardTaskRepository) TryStartRetry(ctx context.Context, taskID int64, expectedRetryCount int) (bool, error) {
	task, _ := m.GetByID(ctx, taskID)
	if task == nil || task.CardStatus != types.CardStatusUpdateFailed || task.RetryCount != expectedRetryCount {
		return false, nil
	}
	task.CardStatus = types.CardStatusUpdating
	return true, nil
}

func (m *MockCardTaskRepository) TryStartUpdate(ctx context.Context, taskID int64, expectedStatus types.CardStatus) (bool, error) {
	task, _ := m.GetByID(ctx, taskID)
	if task == nil || task.CardStatus != expectedStatus {
		return false, nil
	}
	task.CardStatus = types.CardStatusUpdating
	return true, nil
}

func (m *MockCardTaskRepository) GetLatestByScene(ctx context.Context, leadID int64, scene types.CardScene) (*types.WeComCardTask, error) {
	tasks := m.cards[leadID]
	for i := len(tasks) - 1; i >= 0; i-- {
		if tasks[i].CardScene == scene {
			return tasks[i], nil
		}
	}
	return nil, nil
}

func (m *MockCardTaskRepository) ListFailedTasks(ctx context.Context, limit int) ([]*types.WeComCardTask, error) {
	return nil, nil
}

// MockWeComClient 模拟企业微信客户端
type MockWeComClient struct {
	sendCardCalls   int
	updateCardCalls int
	sentCards       []*MockSentCard
	updatedCards    []*MockUpdatedCard
}

type MockSentCard struct {
	ToUser  string
	TaskID  string
	Content *types.WeComCardContent
}

type MockUpdatedCard struct {
	ResponseCode string
	Content      *types.WeComCardContent
}

func NewMockWeComClient() *MockWeComClient {
	return &MockWeComClient{
		sentCards:    make([]*MockSentCard, 0),
		updatedCards: make([]*MockUpdatedCard, 0),
	}
}

func (m *MockWeComClient) SendCard(ctx context.Context, req *wecom.SendCardRequest) (*wecom.SendCardResponse, error) {
	m.sendCardCalls++
	return &wecom.SendCardResponse{
		MsgID:        "mock_msgid",
		ResponseCode: "mock_response_code",
	}, nil
}

func (m *MockWeComClient) UpdateCard(ctx context.Context, req *wecom.UpdateCardRequest) (*wecom.UpdateCardResponse, error) {
	m.updateCardCalls++
	return &wecom.UpdateCardResponse{Success: true}, nil
}

func (m *MockWeComClient) SendText(ctx context.Context, req *wecom.SendTextRequest) error {
	return nil
}

func (m *MockWeComClient) GetAccessToken(ctx context.Context) (string, error) {
	return "mock_token", nil
}

func (m *MockWeComClient) GetUserIDByCode(ctx context.Context, code string) (string, error) {
	return "mock_userid", nil
}

func (m *MockWeComClient) GetJSAPIConfig(ctx context.Context, url string) (*wecom.JSAPIConfig, error) {
	return &wecom.JSAPIConfig{
		AppID:     "mock_corp",
		Timestamp: time.Now().Unix(),
		NonceStr:  "mock_nonce",
		Signature: "mock_signature",
	}, nil
}

func (m *MockWeComClient) VerifyCallbackURL(msgSignature, timestamp, nonce, echoStr string) (string, error) {
	return echoStr, nil
}

func (m *MockWeComClient) DecryptCallback(msgSignature, timestamp, nonce string, encryptedData []byte) ([]byte, error) {
	return encryptedData, nil
}

// MockCache 模拟缓存
type MockCache struct {
	data     map[string]string
	setNXErr error
}

func NewMockCache() *MockCache {
	return &MockCache{
		data: make(map[string]string),
	}
}

func (m *MockCache) Get(ctx context.Context, key string) (string, error) {
	return m.data[key], nil
}

func (m *MockCache) Set(ctx context.Context, key string, value interface{}, expiry time.Duration) error {
	m.data[key] = fmt.Sprint(value)
	return nil
}

func (m *MockCache) SetNX(ctx context.Context, key string, value interface{}, expiry time.Duration) (bool, error) {
	if m.setNXErr != nil {
		return false, m.setNXErr
	}
	if _, exists := m.data[key]; exists {
		return false, nil
	}
	m.data[key] = fmt.Sprint(value)
	return true, nil
}

func (m *MockCache) Del(ctx context.Context, keys ...string) error {
	for _, key := range keys {
		delete(m.data, key)
	}
	return nil
}

func (m *MockCache) Ping(ctx context.Context) error {
	return nil
}

func (m *MockCache) Exists(ctx context.Context, keys ...string) (int64, error) {
	var count int64
	for _, key := range keys {
		if _, exists := m.data[key]; exists {
			count++
		}
	}
	return count, nil
}

func (m *MockCache) Incr(ctx context.Context, key string) (int64, error) {
	current := int64(0)
	if value := m.data[key]; value != "" {
		_, _ = fmt.Sscan(value, &current)
	}
	current++
	m.data[key] = fmt.Sprint(current)
	return current, nil
}

func (m *MockCache) Expire(ctx context.Context, key string, expiry time.Duration) error {
	return nil
}

type MockTaskQueue struct {
	updateCardCalls        int
	sendCardCalls          int
	dispatchReminderCalls  int
	claimReminderCalls     int
	dispatchReminderDelays []time.Duration
	claimReminderDelays    []time.Duration
	updateCardErr          error
	sendCardErr            error
	dispatchReminderErr    error
	claimReminderErr       error
	sendCardReceivers      []string
	sendCardScenes         []string
}

func (m *MockTaskQueue) EnqueueUpdateCard(ctx context.Context, leadID int64, cardScene string) error {
	m.updateCardCalls++
	return m.updateCardErr
}

func (m *MockTaskQueue) EnqueueSendCard(ctx context.Context, leadID int64, receiverUserID, cardScene string) error {
	m.sendCardCalls++
	m.sendCardReceivers = append(m.sendCardReceivers, receiverUserID)
	m.sendCardScenes = append(m.sendCardScenes, cardScene)
	return m.sendCardErr
}

func (m *MockTaskQueue) SchedulePendingDispatchReminder(ctx context.Context, leadID int64, delay time.Duration) error {
	m.dispatchReminderCalls++
	m.dispatchReminderDelays = append(m.dispatchReminderDelays, delay)
	return m.dispatchReminderErr
}

func (m *MockTaskQueue) SchedulePendingClaimReminder(ctx context.Context, leadID int64, delay time.Duration) error {
	m.claimReminderCalls++
	m.claimReminderDelays = append(m.claimReminderDelays, delay)
	return m.claimReminderErr
}

type fakeRuntimeSettings struct {
	dispatchDelays []time.Duration
	claimDelays    []time.Duration
	retryDelays    []time.Duration
	agentID        int
}

func (s *fakeRuntimeSettings) WeComAgentID(ctx context.Context) int {
	return s.agentID
}

func (s *fakeRuntimeSettings) DispatchReminderDelays(ctx context.Context) []time.Duration {
	return s.dispatchDelays
}

func (s *fakeRuntimeSettings) ClaimReminderDelays(ctx context.Context) []time.Duration {
	return s.claimDelays
}

func (s *fakeRuntimeSettings) CardRetryDelays(ctx context.Context) []time.Duration {
	return s.retryDelays
}

// TestAssignLead_Success 测试正常分配流程
func TestAssignLead_Success(t *testing.T) {
	// 准备测试数据
	leadRepo := NewMockLeadRepository()
	dispatchRepo := NewMockDispatchRecordRepository()
	cardRepo := NewMockCardTaskRepository()
	wecomClient := NewMockWeComClient()
	cache := NewMockCache()
	taskQueue := &MockTaskQueue{}

	// 创建待分配线索
	lead := &types.Lead{
		ID:                    1,
		LeadNo:                "LEAD-001",
		Status:                types.LeadStatusPendingDispatch,
		Version:               1,
		DispatchManagerUserID: "manager1",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}
	leadRepo.leads[1] = lead

	// 创建用例
	usecase := NewAssignLeadUseCase(
		leadRepo,
		dispatchRepo,
		cardRepo,
		wecomClient,
		cache,
		taskQueue,
	)

	// 执行分配
	ctx := context.Background()
	req := &AssignLeadRequest{
		LeadID:        1,
		ManagerUserID: "manager1",
		ActorRole:     string(types.UserRoleManager),
		SalesUserID:   "sales1",
		Remark:        "测试分配",
	}

	err := usecase.Execute(ctx, req)

	// 验证结果
	if err != nil {
		t.Fatalf("分配失败: %v", err)
	}

	// 验证线索状态已更新
	updatedLead, _ := leadRepo.GetByID(ctx, 1)
	if updatedLead.Status != types.LeadStatusAssignedPendingClaim {
		t.Errorf("线索状态错误: 期望 %s, 实际 %s",
			types.LeadStatusAssignedPendingClaim, updatedLead.Status)
	}

	if updatedLead.AssignedSalesUserID != "sales1" {
		t.Errorf("分配销售错误: 期望 sales1, 实际 %s", updatedLead.AssignedSalesUserID)
	}

	if updatedLead.Version != 2 {
		t.Errorf("版本号错误: 期望 2, 实际 %d", updatedLead.Version)
	}

	// 验证分配记录已创建
	if len(dispatchRepo.records) != 1 {
		t.Errorf("分配记录数量错误: 期望 1, 实际 %d", len(dispatchRepo.records))
	}

	record := dispatchRepo.records[0]
	if record.SalesUserID != "sales1" || record.ActionType != "assign" {
		t.Errorf("分配记录内容错误")
	}

	if taskQueue.updateCardCalls != 1 {
		t.Errorf("主管卡片更新任务数量错误: 期望 1, 实际 %d", taskQueue.updateCardCalls)
	}
	if taskQueue.sendCardCalls != 1 {
		t.Errorf("销售卡片发送任务数量错误: 期望 1, 实际 %d", taskQueue.sendCardCalls)
	}
	if taskQueue.claimReminderCalls != 3 {
		t.Errorf("领取提醒任务数量错误: 期望 3, 实际 %d", taskQueue.claimReminderCalls)
	}
}

func TestAssignLeadRoleAuthorizationIncludesDispatcherAndRejectsFrontlineRoles(t *testing.T) {
	for _, role := range []types.UserRole{types.UserRoleSales, types.UserRoleInviter, types.UserRoleCloser, ""} {
		t.Run(string(role), func(t *testing.T) {
			uc := NewAssignLeadUseCase(NewMockLeadRepository(), NewMockDispatchRecordRepository(), nil, nil, nil, nil)
			err := uc.Execute(context.Background(), &AssignLeadRequest{
				LeadID: 1, ManagerUserID: "actor-1", ActorRole: string(role), SalesUserID: "inviter-1",
			})
			if err != types.ErrUnauthorized {
				t.Fatalf("role %q error=%v, want ErrUnauthorized", role, err)
			}
		})
	}

	leads := NewMockLeadRepository()
	now := time.Now()
	leads.leads[1] = &types.Lead{
		ID: 1, LeadNo: "DISPATCHER-ASSIGN", CustomerName: "待分配客户",
		Status: types.LeadStatusPendingDispatch, Version: 1, DispatchManagerUserID: "platform-manager",
		CreatedAt: now, UpdatedAt: now,
	}
	dispatches := NewMockDispatchRecordRepository()
	uc := NewAssignLeadUseCase(leads, dispatches, nil, nil, nil, nil)
	if err := uc.Execute(context.Background(), &AssignLeadRequest{
		LeadID: 1, ManagerUserID: "dispatcher-1", ActorRole: string(types.UserRoleDispatcher), SalesUserID: "inviter-1",
	}); err != nil {
		t.Fatalf("dispatcher assignment: %v", err)
	}
	if len(dispatches.records) != 1 || dispatches.records[0].ActionType != "assign" || dispatches.records[0].ManagerUserID != "dispatcher-1" {
		t.Fatalf("dispatcher history=%+v", dispatches.records)
	}
	if leads.leads[1].DispatchManagerUserID != "platform-manager" {
		t.Fatalf("dispatcher overwrote business manager: %+v", leads.leads[1])
	}
}

func TestAssignLead_UsesConfiguredClaimReminderDelays(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	dispatchRepo := NewMockDispatchRecordRepository()
	cardRepo := NewMockCardTaskRepository()
	wecomClient := NewMockWeComClient()
	cache := NewMockCache()
	taskQueue := &MockTaskQueue{}

	leadRepo.leads[1] = &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusPendingDispatch,
		Version:               1,
		DispatchManagerUserID: "manager1",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}

	usecase := NewAssignLeadUseCase(
		leadRepo,
		dispatchRepo,
		cardRepo,
		wecomClient,
		cache,
		taskQueue,
	).WithRuntimeSettings(&fakeRuntimeSettings{
		claimDelays: []time.Duration{2 * time.Minute, 4 * time.Minute},
	})

	err := usecase.Execute(context.Background(), &AssignLeadRequest{
		LeadID:        1,
		ManagerUserID: "manager1",
		ActorRole:     string(types.UserRoleManager),
		SalesUserID:   "sales1",
	})
	if err != nil {
		t.Fatalf("分配失败: %v", err)
	}

	if len(taskQueue.claimReminderDelays) != 2 {
		t.Fatalf("领取提醒任务数量错误: 期望 2, 实际 %d", len(taskQueue.claimReminderDelays))
	}
	if taskQueue.claimReminderDelays[0] != 2*time.Minute || taskQueue.claimReminderDelays[1] != 4*time.Minute {
		t.Fatalf("领取提醒延迟错误: %+v", taskQueue.claimReminderDelays)
	}
}

func TestAssignLeadRecordsFailedSendCardCompensation(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	dispatchRepo := NewMockDispatchRecordRepository()
	cardRepo := NewMockCardTaskRepository()
	wecomClient := NewMockWeComClient()
	cache := NewMockCache()
	taskQueue := &MockTaskQueue{sendCardErr: fmt.Errorf("redis unavailable")}

	leadRepo.leads[1] = &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusPendingDispatch,
		Version:               1,
		DispatchManagerUserID: "manager1",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}

	usecase := NewAssignLeadUseCase(
		leadRepo,
		dispatchRepo,
		cardRepo,
		wecomClient,
		cache,
		taskQueue,
	)

	err := usecase.Execute(context.Background(), &AssignLeadRequest{
		LeadID:        1,
		ManagerUserID: "manager1",
		ActorRole:     string(types.UserRoleManager),
		SalesUserID:   "sales1",
		Remark:        "测试分配",
	})
	if err != nil {
		t.Fatalf("分配失败: %v", err)
	}

	tasks := cardRepo.cards[1]
	if len(tasks) != 1 {
		t.Fatalf("补偿卡片任务数 = %d, want 1", len(tasks))
	}
	task := tasks[0]
	if task.CardScene != types.CardSceneSalesPendingClaim {
		t.Fatalf("card scene = %s, want %s", task.CardScene, types.CardSceneSalesPendingClaim)
	}
	if task.ReceiverUserID != "sales1" {
		t.Fatalf("receiver = %s, want sales1", task.ReceiverUserID)
	}
	if task.CardStatus != types.CardStatusUpdateFailed {
		t.Fatalf("card status = %s, want %s", task.CardStatus, types.CardStatusUpdateFailed)
	}
	if task.LastError == "" {
		t.Fatal("last error is empty")
	}
}

func TestCreateLead_UsesConfiguredDispatchReminderDelays(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	taskQueue := &MockTaskQueue{}
	usecase := NewCreateLeadUseCase(leadRepo, taskQueue).WithRuntimeSettings(&fakeRuntimeSettings{
		dispatchDelays: []time.Duration{1 * time.Minute, 8 * time.Minute},
	})

	_, err := usecase.Execute(context.Background(), &CreateLeadRequest{
		CustomerName:          "测试客户",
		CustomerMobile:        "13912345678",
		DispatchManagerUserID: "manager1",
	})
	if err != nil {
		t.Fatalf("创建线索失败: %v", err)
	}

	if len(taskQueue.dispatchReminderDelays) != 2 {
		t.Fatalf("派发提醒任务数量错误: 期望 2, 实际 %d", len(taskQueue.dispatchReminderDelays))
	}
	if taskQueue.dispatchReminderDelays[0] != time.Minute || taskQueue.dispatchReminderDelays[1] != 8*time.Minute {
		t.Fatalf("派发提醒延迟错误: %+v", taskQueue.dispatchReminderDelays)
	}
}

type notificationBindingLister struct {
	items []*types.WeComUserBinding
}

func (r *notificationBindingLister) List(_ context.Context, status, role string, offset, limit int) ([]*types.WeComUserBinding, int, error) {
	filtered := make([]*types.WeComUserBinding, 0)
	for _, item := range r.items {
		if string(item.Status) == status && string(item.Role) == role {
			filtered = append(filtered, item)
		}
	}
	if offset >= len(filtered) {
		return nil, len(filtered), nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end], len(filtered), nil
}

func TestCreateLead_NotifiesAllActiveAdminsAndManagers(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	taskQueue := &MockTaskQueue{}
	bindings := &notificationBindingLister{items: []*types.WeComUserBinding{
		{WeComUserID: "admin-1", Role: types.UserRoleAdmin, Status: types.UserStatusActive},
		{WeComUserID: "manager-1", Role: types.UserRoleManager, Status: types.UserStatusActive},
		{WeComUserID: "manager-disabled", Role: types.UserRoleManager, Status: types.UserStatusDisabled},
		{WeComUserID: "inviter-1", Role: types.UserRoleInviter, Status: types.UserStatusActive},
	}}
	uc := NewCreateLeadUseCase(leadRepo, taskQueue).WithNotificationRecipientRepository(bindings)

	_, err := uc.Execute(context.Background(), &CreateLeadRequest{
		CustomerName:          "测试客户",
		CustomerMobile:        "13912345678",
		DispatchManagerUserID: "legacy-default",
	})
	if err != nil {
		t.Fatalf("创建线索失败: %v", err)
	}

	want := []string{"admin-1", "manager-1"}
	if fmt.Sprint(taskQueue.sendCardReceivers) != fmt.Sprint(want) {
		t.Fatalf("通知接收人 = %v, want %v", taskQueue.sendCardReceivers, want)
	}
	for _, scene := range taskQueue.sendCardScenes {
		if scene != string(types.CardSceneSupervisorPendingDispatch) {
			t.Fatalf("卡片场景 = %s", scene)
		}
	}
}

// TestAssignLead_InvalidStatus 测试状态校验
func TestAssignLead_InvalidStatus(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	dispatchRepo := NewMockDispatchRecordRepository()
	cardRepo := NewMockCardTaskRepository()
	wecomClient := NewMockWeComClient()
	cache := NewMockCache()
	taskQueue := &MockTaskQueue{}

	// 创建已分配的线索（状态不允许再次分配）
	lead := &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusAssignedPendingClaim,
		Version:               1,
		DispatchManagerUserID: "manager1",
		AssignedSalesUserID:   "sales1",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}
	leadRepo.leads[1] = lead

	usecase := NewAssignLeadUseCase(
		leadRepo,
		dispatchRepo,
		cardRepo,
		wecomClient,
		cache,
		taskQueue,
	)

	ctx := context.Background()
	req := &AssignLeadRequest{
		LeadID:        1,
		ManagerUserID: "manager1",
		ActorRole:     string(types.UserRoleManager),
		SalesUserID:   "sales2",
		Remark:        "尝试重复分配",
	}

	err := usecase.Execute(ctx, req)

	// 应该返回状态错误
	if err != types.ErrLeadStatusInvalid {
		t.Errorf("期望状态错误，实际: %v", err)
	}

	// 验证数据库未更新
	if leadRepo.updateCalls > 0 {
		t.Error("不应该调用数据库更新")
	}
}

// TestAssignLead_SharedPendingDispatchPoolAllowsAnotherManager 测试待分配共享池。
func TestAssignLead_SharedPendingDispatchPoolAllowsAnotherManager(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	dispatchRepo := NewMockDispatchRecordRepository()
	cardRepo := NewMockCardTaskRepository()
	wecomClient := NewMockWeComClient()
	cache := NewMockCache()
	taskQueue := &MockTaskQueue{}

	lead := &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusPendingDispatch,
		Version:               1,
		DispatchManagerUserID: "manager1",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}
	leadRepo.leads[1] = lead

	usecase := NewAssignLeadUseCase(
		leadRepo,
		dispatchRepo,
		cardRepo,
		wecomClient,
		cache,
		taskQueue,
	)

	ctx := context.Background()
	req := &AssignLeadRequest{
		LeadID:        1,
		ManagerUserID: "manager2",
		ActorRole:     string(types.UserRoleManager),
		SalesUserID:   "sales1",
		Remark:        "共享池分配",
	}

	err := usecase.Execute(ctx, req)
	if err != nil {
		t.Fatalf("共享池分配失败: %v", err)
	}

	updatedLead, _ := leadRepo.GetByID(ctx, 1)
	if updatedLead.Status != types.LeadStatusAssignedPendingClaim {
		t.Fatalf("status = %s, want %s", updatedLead.Status, types.LeadStatusAssignedPendingClaim)
	}
	if updatedLead.DispatchManagerUserID != "manager2" {
		t.Fatalf("dispatch manager = %s, want manager2", updatedLead.DispatchManagerUserID)
	}
	if updatedLead.AssignedSalesUserID != "sales1" {
		t.Fatalf("assigned sales = %s, want sales1", updatedLead.AssignedSalesUserID)
	}
	if len(dispatchRepo.records) != 1 || dispatchRepo.records[0].ManagerUserID != "manager2" {
		t.Fatalf("dispatch record = %+v, want manager2", dispatchRepo.records)
	}
}
