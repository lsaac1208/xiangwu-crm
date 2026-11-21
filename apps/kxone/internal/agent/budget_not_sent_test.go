package agent

import (
	"errors"
	"testing"
	"time"
)

func TestNotSentReservationIsAuditedIdempotentAndDoesNotReleaseUnknown(t *testing.T) {
	b := testBudget(t)
	now := time.Date(2026, 10, 31, 15, 59, 59, 0, time.UTC)
	id, err := b.Reserve(t.Context(), "not-dispatched", 1, 100, 150, 100, "price", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.releaseUndispatched(t.Context(), id, "another-run"); err == nil {
		t.Fatal("cross-run reservation released")
	}
	if err := b.releaseUndispatched(t.Context(), id, "not-dispatched"); err != nil {
		t.Fatal(err)
	}
	if err := b.releaseUndispatched(t.Context(), id, "not-dispatched"); err != nil {
		t.Fatal(err)
	}
	var record BudgetReservation
	b.db.First(&record, "id=?", id)
	if record.Status != "not_sent" || record.AmountMicro != 100 || record.ActualMicro != 0 || record.Month != "2026-10" || record.SettledAt != nil {
		t.Fatalf("audit destroyed: %+v", record)
	}
	if err := b.Settle(t.Context(), id, 1, Usage{InputTokens: 1}); !errors.Is(err, ErrBudgetSettlementConflict) {
		t.Fatalf("not_sent overwritten as provider usage: %v", err)
	}
	unknown, err := b.Reserve(t.Context(), "not-dispatched", 1, 100, 150, 100, "price", now)
	if err != nil {
		t.Fatal("released call still consumed budget", err)
	}
	if err := b.Settle(t.Context(), unknown, 0, Usage{}); !errors.Is(err, ErrBudgetUsageUnknown) {
		t.Fatal(err)
	}
	summary, err := b.Summary(t.Context(), now)
	if err != nil || summary.TotalMicro != 100 || summary.PendingCalls != 1 || summary.SettledCalls != 0 {
		t.Fatalf("unknown was cleared or not_sent miscounted: %+v %v", summary, err)
	}
	if _, err := b.Reserve(t.Context(), "new", 1, 51, 150, 100, "price", now); !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("unknown ceiling lost: %v", err)
	}
	if err := b.Settle(t.Context(), unknown, 50, Usage{InputTokens: 10}); err != nil {
		t.Fatal(err)
	}
	if err := b.releaseUndispatched(t.Context(), unknown, "not-dispatched"); !errors.Is(err, ErrBudgetSettlementConflict) {
		t.Fatalf("settled charge released: %v", err)
	}
	next, err := b.Summary(t.Context(), now.Add(time.Second))
	if err != nil || next.TotalMicro != 0 {
		t.Fatalf("immutable month moved: %+v %v", next, err)
	}
}
