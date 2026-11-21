package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

// TestReassignLead_Success 测试正常转派流程
func TestReassignLead_Success(t *testing.T) {
	// 准备测试数据
	leadRepo := NewMockLeadRepository()
	dispatchRepo := NewMockDispatchRecordRepository()
	cardRepo := NewMockCardTaskRepository()
	taskQueue := &MockTaskQueue{}

	// 创建已领取的线索
	lead := &types.Lead{
		ID:                    1,
		LeadNo:                "LEAD-001",
		Status:                types.LeadStatusClaimed,
		Version:               3,
		DispatchManagerUserID: "manager1",
		AssignedSalesUserID:   "sales1",
		OwnerUserID:           "sales1",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}
	leadRepo.leads[1] = lead

	// 创建用例
	usecase := NewReassignLeadUseCase(
		leadRepo,
		dispatchRepo,
		cardRepo,
		taskQueue,
	)

	// 执行转派
	ctx := context.Background()
	req := &ReassignLeadRequest{
		LeadID:         1,
		ManagerUserID:  "manager1",
		NewSalesUserID: "sales2",
		Remark:         "销售1无法跟进，转派给销售2",
	}

	err := usecase.Execute(ctx, req)

	// 验证结果
	if err != nil {
		t.Fatalf("转派失败: %v", err)
	}

	// 验证线索状态已更新
	updatedLead, _ := leadRepo.GetByID(ctx, 1)
	if updatedLead.Status != types.LeadStatusAssignedPendingClaim {
		t.Errorf("线索状态错误: 期望 %s, 实际 %s",
			types.LeadStatusAssignedPendingClaim, updatedLead.Status)
	}

	if updatedLead.AssignedSalesUserID != "sales2" {
		t.Errorf("转派销售错误: 期望 sales2, 实际 %s", updatedLead.AssignedSalesUserID)
	}

	if updatedLead.OwnerUserID != "" {
		t.Errorf("应该清空 OwnerUserID，实际: %s", updatedLead.OwnerUserID)
	}

	if updatedLead.Version != 4 {
		t.Errorf("版本号错误: 期望 4, 实际 %d", updatedLead.Version)
	}

	// 验证转派记录已创建
	if len(dispatchRepo.records) != 1 {
		t.Errorf("转派记录数量错误: 期望 1, 实际 %d", len(dispatchRepo.records))
	}

	record := dispatchRepo.records[0]
	if record.SalesUserID != "sales2" || record.ActionType != "reassign" {
		t.Errorf("转派记录内容错误")
	}

	if taskQueue.sendCardCalls != 1 {
		t.Errorf("销售卡片发送任务数量错误: 期望 1, 实际 %d", taskQueue.sendCardCalls)
	}
	if taskQueue.claimReminderCalls != 3 {
		t.Errorf("领取提醒任务数量错误: 期望 3, 实际 %d", taskQueue.claimReminderCalls)
	}
}

// TestReassignLead_FromPendingClaim 测试从待领取状态转派
func TestReassignLead_FromPendingClaim(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	dispatchRepo := NewMockDispatchRecordRepository()
	cardRepo := NewMockCardTaskRepository()
	taskQueue := &MockTaskQueue{}

	// 创建待领取的线索
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

	usecase := NewReassignLeadUseCase(
		leadRepo,
		dispatchRepo,
		cardRepo,
		taskQueue,
	)

	ctx := context.Background()
	req := &ReassignLeadRequest{
		LeadID:         1,
		ManagerUserID:  "manager1",
		NewSalesUserID: "sales2",
		Remark:         "销售1超时未领取，转派给销售2",
	}

	err := usecase.Execute(ctx, req)

	// 应该转派成功
	if err != nil {
		t.Fatalf("转派失败: %v", err)
	}

	updatedLead, _ := leadRepo.GetByID(ctx, 1)
	if updatedLead.AssignedSalesUserID != "sales2" {
		t.Errorf("转派销售错误: 期望 sales2, 实际 %s", updatedLead.AssignedSalesUserID)
	}
}

// TestReassignLead_InvalidStatus 测试状态校验
func TestReassignLead_InvalidStatus(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	dispatchRepo := NewMockDispatchRecordRepository()
	cardRepo := NewMockCardTaskRepository()
	taskQueue := &MockTaskQueue{}

	// 创建已成交的线索（不允许转派）
	lead := &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusConverted,
		Version:               4,
		DispatchManagerUserID: "manager1",
		AssignedSalesUserID:   "sales1",
		OwnerUserID:           "sales1",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}
	leadRepo.leads[1] = lead

	usecase := NewReassignLeadUseCase(
		leadRepo,
		dispatchRepo,
		cardRepo,
		taskQueue,
	)

	ctx := context.Background()
	req := &ReassignLeadRequest{
		LeadID:         1,
		ManagerUserID:  "manager1",
		NewSalesUserID: "sales2",
		Remark:         "尝试转派已成交线索",
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

// TestReassignLead_UnauthorizedManager 测试权限校验
func TestReassignLead_ManagersJointlyManageLeads(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	dispatchRepo := NewMockDispatchRecordRepository()
	cardRepo := NewMockCardTaskRepository()
	taskQueue := &MockTaskQueue{}

	// 创建线索，指定主管为 manager1
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

	usecase := NewReassignLeadUseCase(
		leadRepo,
		dispatchRepo,
		cardRepo,
		taskQueue,
	)

	ctx := context.Background()
	req := &ReassignLeadRequest{
		LeadID:        1,
		ManagerUserID: "manager2", ActorRole: "manager", // 所有主管共同管理
		NewSalesUserID: "sales2",
		Remark:         "共同管理交接",
	}

	err := usecase.Execute(ctx, req)

	// 应该返回权限错误
	if err != nil {
		t.Errorf("主管共同管理转派失败: %v", err)
	}

	// 验证数据库未更新
	if leadRepo.updateCalls != 1 {
		t.Error("应该原子更新线索一次")
	}
}

// TestReassignLead_AssignedAtRefreshed 转派后 AssignedAt 必须刷新，否则 SLA 计算基于旧分配时间导致虚高。
func TestReassignLead_AssignedAtRefreshed(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	dispatchRepo := NewMockDispatchRecordRepository()
	cardRepo := NewMockCardTaskRepository()
	taskQueue := &MockTaskQueue{}

	// 模拟一条五分钟前已分配、当前已领取的线索
	staleAssignedAt := time.Now().Add(-5 * time.Minute)
	lead := &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusClaimed,
		Version:               2,
		DispatchManagerUserID: "manager1",
		AssignedSalesUserID:   "sales1",
		OwnerUserID:           "sales1",
		AssignedAt:            &staleAssignedAt,
		CreatedAt:             time.Now().Add(-6 * time.Minute),
		UpdatedAt:             time.Now(),
	}
	leadRepo.leads[1] = lead

	before := time.Now()

	usecase := NewReassignLeadUseCase(leadRepo, dispatchRepo, cardRepo, taskQueue)
	err := usecase.Execute(context.Background(), &ReassignLeadRequest{
		LeadID:         1,
		ManagerUserID:  "manager1",
		NewSalesUserID: "sales2",
	})
	if err != nil {
		t.Fatalf("转派失败: %v", err)
	}

	after := time.Now()
	updated, _ := leadRepo.GetByID(context.Background(), 1)
	if updated.AssignedAt == nil {
		t.Fatal("AssignedAt 不应为 nil")
	}
	// 转派后 AssignedAt 应指向本次转派时刻，而不是 staleAssignedAt（5分钟前）
	if updated.AssignedAt.Before(before) || updated.AssignedAt.After(after) {
		t.Fatalf("AssignedAt = %v，期望在 [%v, %v] 之间（本次转派时刻）", updated.AssignedAt, before, after)
	}
}

// TestReassignLead_SameOrEmptySales 测试销售参数校验
func TestReassignLead_SameOrEmptySales(t *testing.T) {
	leadRepo := NewMockLeadRepository()
	dispatchRepo := NewMockDispatchRecordRepository()
	cardRepo := NewMockCardTaskRepository()
	taskQueue := &MockTaskQueue{}

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

	usecase := NewReassignLeadUseCase(
		leadRepo,
		dispatchRepo,
		cardRepo,
		taskQueue,
	)

	ctx := context.Background()

	// 测试转派给同一个销售
	req1 := &ReassignLeadRequest{
		LeadID:         1,
		ManagerUserID:  "manager1",
		NewSalesUserID: "sales1", // 与当前销售相同
		Remark:         "尝试转派给同一个销售",
	}

	err1 := usecase.Execute(ctx, req1)
	if err1 != types.ErrUnauthorized {
		t.Errorf("期望参数错误，实际: %v", err1)
	}

	// 测试空销售ID
	req2 := &ReassignLeadRequest{
		LeadID:         1,
		ManagerUserID:  "manager1",
		NewSalesUserID: "", // 空销售ID
		Remark:         "尝试转派给空销售",
	}

	err2 := usecase.Execute(ctx, req2)
	if err2 != types.ErrUnauthorized {
		t.Errorf("期望参数错误，实际: %v", err2)
	}
}
