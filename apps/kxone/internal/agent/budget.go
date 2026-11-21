package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrBudgetExceeded                 = errors.New("agent monthly budget exhausted")
	ErrRunBudgetExceeded              = errors.New("agent run budget exhausted")
	ErrBudgetUsageUnknown             = errors.New("model usage unknown; reservation retained")
	ErrBudgetSettlementConflict       = errors.New("reservation already settled with different usage")
	ErrBudgetActualExceedsReservation = errors.New("actual charge exceeds reserved maximum; actual charge recorded for reconciliation")
)

// BudgetReservation is a per-model-call ledger, not a per-question estimate.
// AmountMicro is the original ceiling; ActualMicro replaces it only after known
// usage has been settled. An interrupted/unknown call continues to consume its
// ceiling. Month is immutable even if settlement happens in the following month.
type BudgetReservation struct {
	ID           string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	RunID        string     `gorm:"type:varchar(36);not null;index:idx_ai_budget_run" json:"run_id"`
	AccountID    int64      `gorm:"not null;index" json:"account_id"`
	Month        string     `gorm:"type:varchar(7);not null;index:idx_ai_budget_month_status,priority:1" json:"month"`
	Status       string     `gorm:"type:varchar(16);not null;index:idx_ai_budget_month_status,priority:2" json:"status"`
	AmountMicro  int64      `gorm:"not null" json:"amount_micro"`
	ActualMicro  int64      `gorm:"not null;default:0" json:"actual_micro"`
	InputTokens  int64      `gorm:"not null;default:0" json:"input_tokens"`
	OutputTokens int64      `gorm:"not null;default:0" json:"output_tokens"`
	PriceVersion string     `gorm:"type:varchar(64);not null" json:"price_version"`
	CreatedAt    time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt    time.Time  `gorm:"not null" json:"updated_at"`
	SettledAt    *time.Time `json:"settled_at,omitempty"`
}

func (BudgetReservation) TableName() string { return "ai_budget_reservations" }

type BudgetSummary struct {
	Month         string `json:"month"`
	SettledMicro  int64  `json:"settled_micro"`
	ReservedMicro int64  `json:"reserved_micro"`
	TotalMicro    int64  `json:"total_micro"`
	InputTokens   int64  `json:"input_tokens"`
	OutputTokens  int64  `json:"output_tokens"`
	SettledCalls  int64  `json:"settled_calls"`
	PendingCalls  int64  `json:"pending_calls"`
}

type Budget struct{ db *gorm.DB }

func NewBudget(db *gorm.DB) *Budget { return &Budget{db: db} }

var budgetShanghai = time.FixedZone("Asia/Shanghai", 8*60*60)

func budgetMonth(now time.Time) string { return now.In(budgetShanghai).Format("2006-01") }

const budgetCharge = "CASE WHEN status = 'settled' THEN actual_micro WHEN status = 'not_sent' THEN 0 ELSE amount_micro END"

// Reserve performs the sum checks and insert in one write statement. In SQLite
// this obtains the writer lock before evaluating the sums, including when two
// service instances share the database; there is no read-then-write race.
func (b *Budget) Reserve(ctx context.Context, runID string, accountID int64, amountMicro, monthlyLimitMicro, runLimitMicro int64, priceVersion string, now time.Time) (string, error) {
	if b == nil || b.db == nil {
		return "", errors.New("budget database unavailable")
	}
	if runID == "" || accountID <= 0 || priceVersion == "" || amountMicro <= 0 || monthlyLimitMicro < 0 || runLimitMicro < 0 {
		return "", errors.New("invalid budget reservation")
	}
	if amountMicro > monthlyLimitMicro {
		return "", ErrBudgetExceeded
	}
	if amountMicro > runLimitMicro {
		return "", ErrRunBudgetExceeded
	}
	id, month := uuid.NewString(), budgetMonth(now)
	query := `INSERT INTO ai_budget_reservations
		(id, run_id, account_id, month, status, amount_micro, actual_micro, input_tokens, output_tokens, price_version, created_at, updated_at)
		SELECT ?, ?, ?, ?, 'reserved', ?, 0, 0, 0, ?, ?, ?
		WHERE COALESCE((SELECT SUM(` + budgetCharge + `) FROM ai_budget_reservations WHERE month = ?), 0) <= ?
		AND COALESCE((SELECT SUM(` + budgetCharge + `) FROM ai_budget_reservations WHERE run_id = ?), 0) <= ?`
	r := b.db.WithContext(ctx).Exec(query, id, runID, accountID, month, amountMicro, priceVersion, now.UTC(), now.UTC(), month, monthlyLimitMicro-amountMicro, runID, runLimitMicro-amountMicro)
	if r.Error != nil {
		return "", fmt.Errorf("reserve model budget: %w", r.Error)
	}
	if r.RowsAffected == 1 {
		return id, nil
	}
	// The rejection is definitive. This read is only for the user-facing reason;
	// it does not decide whether the call may proceed.
	var runTotal int64
	if err := b.db.WithContext(ctx).Model(&BudgetReservation{}).Select("COALESCE(SUM("+budgetCharge+"), 0)").Where("run_id = ?", runID).Scan(&runTotal).Error; err != nil {
		return "", fmt.Errorf("read rejected reservation: %w", err)
	}
	if runTotal > runLimitMicro-amountMicro {
		return "", ErrRunBudgetExceeded
	}
	return "", ErrBudgetExceeded
}

func (b *Budget) Settle(ctx context.Context, id string, actualMicro int64, usage Usage) error {
	if b == nil || b.db == nil {
		return errors.New("budget database unavailable")
	}
	if id == "" || actualMicro < 0 || usage.InputTokens < 0 || usage.OutputTokens < 0 {
		return errors.New("invalid budget settlement")
	}
	if usage.InputTokens == 0 && usage.OutputTokens == 0 {
		return ErrBudgetUsageUnknown
	}
	now := time.Now().UTC()
	r := b.db.WithContext(ctx).Model(&BudgetReservation{}).
		Where("id = ? AND status = 'reserved'", id).
		Updates(map[string]any{"status": "settled", "actual_micro": actualMicro, "input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens, "settled_at": now, "updated_at": now})
	if r.Error != nil {
		return fmt.Errorf("settle model budget: %w", r.Error)
	}
	var reservation BudgetReservation
	if err := b.db.WithContext(ctx).First(&reservation, "id = ?", id).Error; err != nil {
		return fmt.Errorf("read model reservation: %w", err)
	}
	if reservation.Status == "settled" {
		if reservation.ActualMicro == actualMicro && reservation.InputTokens == usage.InputTokens && reservation.OutputTokens == usage.OutputTokens {
			// A known charge must never be hidden behind a smaller estimate.
			// Record it, then fail closed so the caller stops and reconciles the
			// price/output ceiling rather than making another paid call.
			if actualMicro > reservation.AmountMicro {
				return ErrBudgetActualExceedsReservation
			}
			return nil
		}
		return ErrBudgetSettlementConflict
	}
	return ErrBudgetSettlementConflict
}

func (b *Budget) Summary(ctx context.Context, now time.Time) (BudgetSummary, error) {
	s := BudgetSummary{Month: budgetMonth(now)}
	if b == nil || b.db == nil {
		return s, errors.New("budget database unavailable")
	}
	query := `SELECT
		COALESCE(SUM(CASE WHEN status = 'settled' THEN actual_micro ELSE 0 END),0) AS settled_micro,
		COALESCE(SUM(CASE WHEN status NOT IN ('settled', 'not_sent') THEN amount_micro ELSE 0 END),0) AS reserved_micro,
		COALESCE(SUM(input_tokens),0) AS input_tokens,
		COALESCE(SUM(output_tokens),0) AS output_tokens,
		COALESCE(SUM(CASE WHEN status = 'settled' THEN 1 ELSE 0 END),0) AS settled_calls,
		COALESCE(SUM(CASE WHEN status NOT IN ('settled', 'not_sent') THEN 1 ELSE 0 END),0) AS pending_calls
		FROM ai_budget_reservations WHERE month = ?`
	if err := b.db.WithContext(ctx).Raw(query, s.Month).Scan(&s).Error; err != nil {
		return s, fmt.Errorf("summarize model budget: %w", err)
	}
	s.TotalMicro = s.SettledMicro + s.ReservedMicro
	return s, nil
}

// releaseUndispatched is only used synchronously before callModel is entered.
// It is never a restart cleanup or a provider error/unknown-usage settlement.
// Preserve the reservation and original ceiling as evidence; no tokens/charge
// are fabricated. A crash before this write leaves the ceiling reserved.
func (b *Budget) releaseUndispatched(ctx context.Context, id, runID string) error {
	if b == nil || b.db == nil || id == "" || runID == "" {
		return errors.New("invalid undispatched reservation")
	}
	result := b.db.WithContext(ctx).Model(&BudgetReservation{}).
		Where("id=? AND run_id=? AND status='reserved' AND actual_micro=0 AND input_tokens=0 AND output_tokens=0", id, runID).
		Updates(map[string]any{"status": "not_sent", "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return fmt.Errorf("release undispatched reservation: %w", result.Error)
	}
	var record BudgetReservation
	if err := b.db.WithContext(ctx).First(&record, "id=? AND run_id=?", id, runID).Error; err != nil {
		return err
	}
	if record.Status != "not_sent" {
		return ErrBudgetSettlementConflict
	}
	return nil
}
