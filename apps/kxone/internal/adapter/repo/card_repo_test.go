package repo

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/config"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestGormCardTaskRepositoryListFailedTasksOnlyReturnsRetryableFailures(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{
		Driver:   "sqlite",
		Database: filepath.Join(t.TempDir(), "cards.db"),
		MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := NewGormCardTaskRepository(db)
	ctx := context.Background()
	now := time.Now()
	due := now.Add(-time.Minute)
	future := now.Add(time.Minute)

	tasks := []*types.WeComCardTask{
		cardTaskForTest("retryable", types.CardStatusUpdateFailed, 1, &due),
		cardTaskForTest("expired", types.CardStatusExpired, 0, nil),
		cardTaskForTest("replaced", types.CardStatusReplaced, 0, nil),
		cardTaskForTest("maxed", types.CardStatusUpdateFailed, types.MaxCardRetryCount, &due),
		cardTaskForTest("future", types.CardStatusUpdateFailed, 1, &future),
	}
	for _, task := range tasks {
		if err := repo.Create(ctx, task); err != nil {
			t.Fatalf("create %s: %v", task.TaskID, err)
		}
	}

	got, err := repo.ListFailedTasks(ctx, 20)
	if err != nil {
		t.Fatalf("list failed tasks: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("failed task count = %d, want 1: %+v", len(got), got)
	}
	if got[0].TaskID != "retryable" {
		t.Fatalf("task id = %q, want retryable", got[0].TaskID)
	}
}

func TestGormCardTaskRepositoryTryStartRetryAllowsSingleWinner(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{
		Driver: "sqlite", Database: filepath.Join(t.TempDir(), "card-cas.db"), MaxConns: 20,
	})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repository := NewGormCardTaskRepository(db)
	task := cardTaskForTest("cas-retry", types.CardStatusUpdateFailed, 1, nil)
	if err := repository.Create(context.Background(), task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	const contenders = 16
	results := make([]bool, contenders)
	errs := make([]error, contenders)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = repository.TryStartRetry(context.Background(), task.ID, task.RetryCount)
		}(i)
	}
	close(start)
	wg.Wait()

	winners := 0
	for i, err := range errs {
		if err != nil {
			t.Fatalf("contender %d: %v", i, err)
		}
		if results[i] {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("CAS winners = %d, want 1", winners)
	}
}

func TestGormCardTaskRepositoryCountsStaleUpdatingWithoutRetrying(t *testing.T) {
	db, err := NewDB(&config.DatabaseConfig{
		Driver: "sqlite", Database: filepath.Join(t.TempDir(), "card-stale.db"), MaxConns: 4,
	})
	if err != nil {
		t.Fatalf("new db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repository := NewGormCardTaskRepository(db)
	old := cardTaskForTest("stale-updating", types.CardStatusUpdating, 0, nil)
	oldTime := time.Now().Add(-20 * time.Minute)
	old.SentAt = oldTime
	old.UpdatedAt = &oldTime
	recent := cardTaskForTest("recent-updating", types.CardStatusUpdating, 0, nil)
	for _, task := range []*types.WeComCardTask{old, recent} {
		if err := repository.Create(context.Background(), task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}
	count, err := repository.CountStaleUpdating(context.Background(), time.Now().Add(-10*time.Minute))
	if err != nil {
		t.Fatalf("count stale: %v", err)
	}
	if count != 1 {
		t.Fatalf("stale count = %d, want 1", count)
	}
	failed, err := repository.ListFailedTasks(context.Background(), 20)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(failed) != 0 {
		t.Fatalf("updating tasks must not be automatically retried: %+v", failed)
	}
}

func cardTaskForTest(taskID string, status types.CardStatus, retryCount int, nextRetryAt *time.Time) *types.WeComCardTask {
	now := time.Now()
	expireAt := now.Add(50 * time.Minute)
	return &types.WeComCardTask{
		LeadID:               1,
		CardScene:            types.CardSceneSupervisorPendingDispatch,
		ReceiverUserID:       "manager1",
		TaskID:               taskID,
		CardStatus:           status,
		RetryCount:           retryCount,
		NextRetryAt:          nextRetryAt,
		SentAt:               now,
		ResponseCodeExpireAt: &expireAt,
	}
}
