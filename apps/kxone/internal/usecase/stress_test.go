package usecase

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

func TestStressConcurrentClaimSameLead(t *testing.T) {
	requireStress(t)

	leadRepo := NewMockLeadRepository()
	cardRepo := NewMockCardTaskRepository()
	wecomClient := NewMockWeComClient()
	cache := NewMockCache()
	cache.setNXErr = fmt.Errorf("redis unavailable")
	taskQueue := &MockTaskQueue{}

	leadRepo.leads[1] = &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusAssignedPendingClaim,
		Version:               1,
		DispatchManagerUserID: "manager1",
		AssignedSalesUserID:   "sales1",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}

	uc := NewClaimLeadUseCase(leadRepo, cardRepo, wecomClient, cache, taskQueue)
	const workers = 200
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	failures := 0

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := uc.Execute(context.Background(), &ClaimLeadRequest{
				LeadID:      1,
				SalesUserID: "sales1",
				TaskID:      fmt.Sprintf("stress-%d", i),
				Source:      "stress",
			})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successes++
			} else {
				failures++
			}
		}(i)
	}
	wg.Wait()

	if successes != 1 || failures != workers-1 {
		t.Fatalf("concurrent claim successes=%d failures=%d", successes, failures)
	}
}

func TestStressConcurrentAssignSamePendingDispatchLead(t *testing.T) {
	requireStress(t)

	leadRepo := NewMockLeadRepository()
	dispatchRepo := NewMockDispatchRecordRepository()
	cardRepo := NewMockCardTaskRepository()
	taskQueue := &MockTaskQueue{}

	leadRepo.leads[1] = &types.Lead{
		ID:                    1,
		Status:                types.LeadStatusPendingDispatch,
		Version:               1,
		DispatchManagerUserID: "manager1",
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}

	uc := NewAssignLeadUseCase(
		leadRepo,
		dispatchRepo,
		cardRepo,
		NewMockWeComClient(),
		NewMockCache(),
		taskQueue,
	)
	const workers = 200
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	failures := 0

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := uc.Execute(context.Background(), &AssignLeadRequest{
				LeadID:        1,
				ManagerUserID: fmt.Sprintf("manager%d", i%8),
				ActorRole:     string(types.UserRoleManager),
				SalesUserID:   fmt.Sprintf("sales%d", i),
				Remark:        "stress concurrent assign",
			})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successes++
			} else {
				failures++
			}
		}(i)
	}
	wg.Wait()

	if successes != 1 || failures != workers-1 {
		t.Fatalf("concurrent assign successes=%d failures=%d", successes, failures)
	}
	if len(dispatchRepo.records) != 1 {
		t.Fatalf("dispatch records = %d, want 1", len(dispatchRepo.records))
	}
}

func TestStressBatchCreateLeads(t *testing.T) {
	requireStress(t)

	leadRepo := NewMockLeadRepository()
	uc := NewCreateLeadUseCase(leadRepo, nil)
	const total = 1000

	start := time.Now()
	for i := 0; i < total; i++ {
		_, err := uc.Execute(context.Background(), &CreateLeadRequest{
			LeadNo:                fmt.Sprintf("stress-lead-%04d", i),
			CustomerName:          fmt.Sprintf("客户%04d", i),
			CustomerMobile:        fmt.Sprintf("139%08d", i),
			DispatchManagerUserID: "manager1",
			SourceChannel:         "stress",
		})
		if err != nil {
			t.Fatalf("create lead %d: %v", i, err)
		}
	}
	t.Logf("created %d leads in %s", total, time.Since(start))
}

func TestStressWorkerQueueFailureCreatesCompensation(t *testing.T) {
	requireStress(t)

	leadRepo := NewMockLeadRepository()
	dispatchRepo := NewMockDispatchRecordRepository()
	cardRepo := NewMockCardTaskRepository()
	taskQueue := &MockTaskQueue{sendCardErr: fmt.Errorf("redis unavailable")}
	uc := NewAssignLeadUseCase(
		leadRepo,
		dispatchRepo,
		cardRepo,
		NewMockWeComClient(),
		NewMockCache(),
		taskQueue,
	)

	const total = 300
	now := time.Now()
	for i := 1; i <= total; i++ {
		leadRepo.leads[int64(i)] = &types.Lead{
			ID:                    int64(i),
			Status:                types.LeadStatusPendingDispatch,
			Version:               1,
			DispatchManagerUserID: "manager1",
			CreatedAt:             now,
			UpdatedAt:             now,
		}
	}

	start := time.Now()
	for i := 1; i <= total; i++ {
		err := uc.Execute(context.Background(), &AssignLeadRequest{
			LeadID:        int64(i),
			ManagerUserID: "manager1",
			ActorRole:     string(types.UserRoleManager),
			SalesUserID:   fmt.Sprintf("sales%03d", i%10),
			Remark:        "stress queue fallback",
		})
		if err != nil {
			t.Fatalf("assign lead %d: %v", i, err)
		}
	}

	compensations := 0
	for _, tasks := range cardRepo.cards {
		for _, task := range tasks {
			if task.CardScene == types.CardSceneSalesPendingClaim &&
				task.CardStatus == types.CardStatusUpdateFailed &&
				task.LastError != "" {
				compensations++
			}
		}
	}
	if compensations != total {
		t.Fatalf("compensation tasks = %d, want %d", compensations, total)
	}
	if taskQueue.sendCardCalls != total {
		t.Fatalf("send card enqueue calls = %d, want %d", taskQueue.sendCardCalls, total)
	}
	t.Logf("assigned %d leads with queue failure compensation in %s", total, time.Since(start))
}

func requireStress(t *testing.T) {
	t.Helper()
	if os.Getenv("KXONE_STRESS") != "1" {
		t.Skip("set KXONE_STRESS=1 to run stress tests")
	}
}
