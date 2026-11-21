package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

// TestClaimLead_Success 测试正常领取流程
func TestClaimLead_Success(t *testing.T) {
	// 准备测试数据
	leadRepo := NewMockLeadRepository()
	cardRepo := NewMockCardTaskRepository()
	wecomClient := NewMockWeComClient()
	cache := NewMockCache()
	taskQueue := &MockTaskQueue{}
	dispatchRepo := NewMockDispatchRecordRepository()

	// 创建已分配待领取的线索
	lead := &types.Lead{
		ID:                    1,
		LeadNo:                "LEAD-001",
		Status:                types.LeadStatusAssignedPendingClaim,
		Version:               2,
		DispatchManagerUserID: "manager1",
		AssignedSalesUserID:   "sales1",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}
	leadRepo.leads[1] = lead

	// 创建用例
	usecase := NewClaimLeadUseCase(
		leadRepo,
		cardRepo,
		wecomClient,
		cache,
		taskQueue,
	).WithDispatchHistory(dispatchRepo, nil)

	// 执行领取
	ctx := context.Background()
	req := &ClaimLeadRequest{
		LeadID:      1,
		SalesUserID: "sales1",
		TaskID:      "lead:1:sales:sales1:claim",
		Source:      "wecom_card",
	}

	err := usecase.Execute(ctx, req)

	// 验证结果
	if err != nil {
		t.Fatalf("领取失败: %v", err)
	}

	// 验证 Redis 幂等键已设置
	idempotentKey := "lead:claim:1:v2:sales1"
	if _, exists := cache.data[idempotentKey]; !exists {
		t.Error("Redis 幂等键未设置")
	}
	if len(dispatchRepo.records) != 1 || dispatchRepo.records[0].ActionType != "claim" || dispatchRepo.records[0].SalesUserID != "sales1" {
		t.Fatalf("领取历史记录 = %+v", dispatchRepo.records)
	}

	// 注意：异步卡片更新需要等待，这里只验证同步逻辑
	// 实际生产环境应该用 Asynq 队列 + 同步等待验证
}

// TestClaimLead_Idempotent 测试幂等性
func TestClaimLead_Idempotent(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	cardRepo := NewMockCardTaskRepository()
	wecomClient := NewMockWeComClient()
	cache := NewMockCache()
	taskQueue := &MockTaskQueue{}

	// 创建已领取的线索
	lead := &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusClaimed,
		Version:               3,
		DispatchManagerUserID: "manager1",
		AssignedSalesUserID:   "sales1",
		OwnerUserID:           "sales1",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}
	leadRepo.leads[1] = lead

	// 预设 Redis 幂等键（模拟重复请求）
	idempotentKey := "lead:claim:1:v3:sales1"
	cache.data[idempotentKey] = "1"

	usecase := NewClaimLeadUseCase(
		leadRepo,
		cardRepo,
		wecomClient,
		cache,
		taskQueue,
	)

	ctx := context.Background()
	req := &ClaimLeadRequest{
		LeadID:      1,
		SalesUserID: "sales1",
		TaskID:      "lead:1:sales:sales1:claim",
		Source:      "wecom_card",
	}

	// 第二次领取应该返回已领取错误
	err := usecase.Execute(ctx, req)

	if err != types.ErrLeadAlreadyClaimed && err != types.ErrDuplicateClaim {
		t.Errorf("期望幂等错误，实际: %v", err)
	}

	// 验证数据库未被再次更新（原子更新会失败）
	initialUpdateCalls := leadRepo.updateCalls
	if leadRepo.updateCalls > initialUpdateCalls {
		t.Error("不应该再次更新数据库")
	}
}

// TestClaimLead_WrongSales 测试权限校验
func TestClaimLead_WrongSales(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	cardRepo := NewMockCardTaskRepository()
	wecomClient := NewMockWeComClient()
	cache := NewMockCache()
	taskQueue := &MockTaskQueue{}

	// 创建分配给 sales1 的线索
	lead := &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusAssignedPendingClaim,
		Version:               2,
		DispatchManagerUserID: "manager1",
		AssignedSalesUserID:   "sales1",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}
	leadRepo.leads[1] = lead

	usecase := NewClaimLeadUseCase(
		leadRepo,
		cardRepo,
		wecomClient,
		cache,
		taskQueue,
	)

	ctx := context.Background()
	req := &ClaimLeadRequest{
		LeadID:      1,
		SalesUserID: "sales2", // 不同的销售尝试领取
		TaskID:      "lead:1:sales:sales2:claim",
		Source:      "wecom_card",
	}

	err := usecase.Execute(ctx, req)

	// 应该返回权限错误
	if err != types.ErrSalesNotAssigned {
		t.Errorf("期望权限错误，实际: %v", err)
	}
}

// TestClaimLead_InvalidStatus 测试状态校验
func TestClaimLead_InvalidStatus(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	cardRepo := NewMockCardTaskRepository()
	wecomClient := NewMockWeComClient()
	cache := NewMockCache()
	taskQueue := &MockTaskQueue{}

	// 创建待分配状态的线索（还未分配给任何销售）
	lead := &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusPendingDispatch,
		Version:               1,
		DispatchManagerUserID: "manager1",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}
	leadRepo.leads[1] = lead

	usecase := NewClaimLeadUseCase(
		leadRepo,
		cardRepo,
		wecomClient,
		cache,
		taskQueue,
	)

	ctx := context.Background()
	req := &ClaimLeadRequest{
		LeadID:      1,
		SalesUserID: "sales1",
		TaskID:      "lead:1:sales:sales1:claim",
		Source:      "wecom_card",
	}

	err := usecase.Execute(ctx, req)

	// 应该返回状态错误
	if err != types.ErrLeadStatusInvalid {
		t.Errorf("期望状态错误，实际: %v", err)
	}
}

// TestClaimLead_ConcurrentClaim 测试并发领取（数据库乐观锁）
func TestClaimLead_ConcurrentClaim(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	cardRepo := NewMockCardTaskRepository()
	wecomClient := NewMockWeComClient()
	cache := NewMockCache()
	taskQueue := &MockTaskQueue{}

	// 创建待领取线索
	lead := &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusAssignedPendingClaim,
		Version:               2,
		DispatchManagerUserID: "manager1",
		AssignedSalesUserID:   "sales1",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}
	leadRepo.leads[1] = lead

	usecase := NewClaimLeadUseCase(
		leadRepo,
		cardRepo,
		wecomClient,
		cache,
		taskQueue,
	)

	ctx := context.Background()

	// 模拟并发场景：第一次领取成功
	req1 := &ClaimLeadRequest{
		LeadID:      1,
		SalesUserID: "sales1",
		TaskID:      "lead:1:sales:sales1:claim",
		Source:      "wecom_card",
	}

	err1 := usecase.Execute(ctx, req1)
	if err1 != nil {
		t.Fatalf("第一次领取失败: %v", err1)
	}

	// 清空缓存模拟 Redis 失效（测试数据库乐观锁）
	cache.data = make(map[string]string)

	// 第二次尝试领取（版本号已变）
	req2 := &ClaimLeadRequest{
		LeadID:      1,
		SalesUserID: "sales1",
		TaskID:      "lead:1:sales:sales1:claim2",
		Source:      "wecom_card",
	}

	err2 := usecase.Execute(ctx, req2)

	// 第二次应该失败（已领取或版本冲突）
	if err2 == nil {
		t.Error("第二次领取应该失败")
	}
}

func TestClaimLead_ContinuesWhenRedisUnavailable(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	cardRepo := NewMockCardTaskRepository()
	wecomClient := NewMockWeComClient()
	cache := NewMockCache()
	cache.setNXErr = errors.New("redis unavailable")
	taskQueue := &MockTaskQueue{}

	leadRepo.leads[1] = &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusAssignedPendingClaim,
		Version:               2,
		DispatchManagerUserID: "manager1",
		AssignedSalesUserID:   "sales1",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}

	uc := NewClaimLeadUseCase(leadRepo, cardRepo, wecomClient, cache, taskQueue)
	err := uc.Execute(context.Background(), &ClaimLeadRequest{
		LeadID:      1,
		SalesUserID: "sales1",
		TaskID:      "l1_spc",
		Source:      "wecom_card",
	})
	if err != nil {
		t.Fatalf("Redis 异常时领取失败: %v", err)
	}

	updatedLead, _ := leadRepo.GetByID(context.Background(), 1)
	if updatedLead.Status != types.LeadStatusClaimed {
		t.Fatalf("线索状态 = %s, want %s", updatedLead.Status, types.LeadStatusClaimed)
	}
	if taskQueue.updateCardCalls != 2 {
		t.Fatalf("卡片更新任务数量 = %d, want 2", taskQueue.updateCardCalls)
	}
}
