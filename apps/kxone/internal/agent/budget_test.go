package agent

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testBudget(t *testing.T) *Budget {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "budget.db")+"?_busy_timeout=10000&_journal_mode=WAL"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(12)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&BudgetReservation{}); err != nil {
		t.Fatal(err)
	}
	return NewBudget(db)
}

func TestBudgetConcurrentReservationsDoNotOverspend(t *testing.T) {
	b := testBudget(t)
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	errs := []error{}
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Independent Budget wrappers prove safety is in the SQL statement,
			// not a process-local mutex.
			_, err := NewBudget(b.db).Reserve(context.Background(), "same-run", 1, 100, 750, 1000, "test-price", now)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				success++
			} else if !errors.Is(err, ErrBudgetExceeded) {
				errs = append(errs, err)
			}
		}()
	}
	wg.Wait()
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if success != 7 {
		t.Fatalf("successful reservations=%d, want 7", success)
	}
	s, err := b.Summary(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalMicro != 700 || s.ReservedMicro != 700 || s.PendingCalls != 7 {
		t.Fatalf("ledger=%+v", s)
	}
}

func TestBudgetRunLimitIncludesAllCallsAndPreviousMonths(t *testing.T) {
	b := testBudget(t)
	ctx := context.Background()
	sep := time.Date(2026, 9, 30, 15, 59, 59, 0, time.UTC)
	id, err := b.Reserve(ctx, "run", 1, 80, 1000, 100, "price", sep)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Settle(ctx, id, 60, Usage{InputTokens: 50, OutputTokens: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Reserve(ctx, "run", 1, 41, 1000, 100, "price", sep.Add(time.Second)); !errors.Is(err, ErrRunBudgetExceeded) {
		t.Fatalf("run limit: %v", err)
	}
	if _, err := b.Reserve(ctx, "run", 1, 40, 1000, 100, "price", sep.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
}

func TestBudgetUnknownUsageSettlementIdempotencyAndMonthBoundary(t *testing.T) {
	b := testBudget(t)
	ctx := context.Background()
	before := time.Date(2026, 9, 30, 15, 59, 59, 0, time.UTC)
	after := before.Add(time.Second)
	id, err := b.Reserve(ctx, "run-a", 1, 100, 100, 100, "price-a", before)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Settle(ctx, id, 0, Usage{}); !errors.Is(err, ErrBudgetUsageUnknown) {
		t.Fatalf("unknown usage=%v", err)
	}
	s, _ := b.Summary(ctx, before)
	if s.Month != "2026-09" || s.ReservedMicro != 100 {
		t.Fatalf("unknown call released reservation: %+v", s)
	}
	if _, err := b.Reserve(ctx, "run-b", 1, 1, 100, 100, "price-a", before); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("unknown reservation ignored: %v", err)
	}
	usage := Usage{InputTokens: 7, OutputTokens: 2}
	if err := b.Settle(ctx, id, 40, usage); err != nil {
		t.Fatal(err)
	}
	if err := b.Settle(ctx, id, 40, usage); err != nil {
		t.Fatalf("idempotent settle=%v", err)
	}
	if err := b.Settle(ctx, id, 41, usage); !errors.Is(err, ErrBudgetSettlementConflict) {
		t.Fatalf("conflicting settle=%v", err)
	}
	s, _ = b.Summary(ctx, before)
	if s.SettledMicro != 40 || s.ReservedMicro != 0 || s.InputTokens != 7 || s.OutputTokens != 2 || s.SettledCalls != 1 {
		t.Fatalf("settled ledger=%+v", s)
	}
	next, _ := b.Summary(ctx, after)
	if next.Month != "2026-10" || next.TotalMicro != 0 {
		t.Fatalf("call moved into settlement month: %+v", next)
	}
	if _, err := b.Reserve(ctx, "new-run", 1, 100, 100, 100, "price-b", after); err != nil {
		t.Fatal(err)
	}
}

func TestBudgetOverCeilingAndConcurrentDuplicateSettlement(t *testing.T) {
	b := testBudget(t)
	ctx := context.Background()
	now := time.Now()
	id, err := b.Reserve(ctx, "run", 1, 100, 1000, 1000, "price", now)
	if err != nil {
		t.Fatal(err)
	}
	overrunID, err := b.Reserve(ctx, "overrun", 1, 100, 1000, 1000, "price", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Settle(ctx, overrunID, 101, Usage{InputTokens: 10}); !errors.Is(err, ErrBudgetActualExceedsReservation) {
		t.Fatalf("ceiling error=%v", err)
	}
	s, _ := b.Summary(ctx, now)
	if s.ReservedMicro != 100 || s.SettledMicro != 101 {
		t.Fatalf("known over-ceiling charge hidden: %+v", s)
	}
	if err := b.Settle(ctx, overrunID, 101, Usage{InputTokens: 10}); !errors.Is(err, ErrBudgetActualExceedsReservation) {
		t.Fatalf("overrun repeated settlement=%v", err)
	}
	var wg sync.WaitGroup
	ch := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); ch <- b.Settle(ctx, id, 90, Usage{InputTokens: 5, OutputTokens: 3}) }()
	}
	wg.Wait()
	close(ch)
	for err := range ch {
		if err != nil {
			t.Fatalf("duplicate settlement: %v", err)
		}
	}
	s, _ = b.Summary(ctx, now)
	if s.TotalMicro != 191 || s.SettledCalls != 2 {
		t.Fatalf("double charged: %+v", s)
	}
}

func TestBudgetInvalidReservationAndCanceledContext(t *testing.T) {
	b := testBudget(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.Reserve(ctx, "run", 1, 1, 100, 100, "price", time.Now()); err == nil {
		t.Fatal("canceled request reserved budget")
	}
	if _, err := b.Reserve(context.Background(), "run", 1, -1, 100, 100, "price", time.Now()); err == nil {
		t.Fatal("negative reservation accepted")
	}
	if _, err := b.Reserve(context.Background(), "run", 1, 1, 0, 100, "price", time.Now()); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("disabled budget=%v", err)
	}
}
