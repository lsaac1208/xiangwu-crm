package agent

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// This benchmark measures construction work only. It does not replace the
// unchanged HTTP acceptance test or add delay to foreground business requests.
func BenchmarkSnapshotProjection(b *testing.B) {
	for _, spec := range snapshotTables[:2] {
		b.Run(spec.name, func(b *testing.B) {
			indexes := fieldIndexes(spec)
			part := snapshotData{spec: spec, rows: make([][]any, 1000)}
			for i := range part.rows {
				row := make([]any, len(spec.fields))
				set := func(name string, value any) {
					if index, exists := indexes[name]; exists {
						row[index] = value
					}
				}
				for name, value := range map[string]any{
					"id": int64(i + 1), "lead_id": int64(i + 1), "star_rating": int64(5),
					"status": "following", "business_stage": "invitation", "ownership_state": "private", "result_status": "active",
					"source_channel": "好学校", "current_owner_user_id": "sales_001", "current_owner_role": "inviter",
					"sales_user_id": "sales_001", "followup_result": "connected", "intention_level": "high", "is_valid": int64(1),
					"remark": "历史跟进希望参加校园参观", "created_at": "2026-10-08 00:30:00+00:00", "updated_at": "2026-10-08 00:30:00+00:00",
					"deal_amount": float64(0), "refund_amount": float64(0), "payment_status": "unpaid",
				} {
					set(name, value)
				}
				// Exercise alternate nullable shapes as real historical records do.
				if i%2 == 0 {
					set("next_followup_at", "2026-10-10 00:30:00+00:00")
					set("next_followup_due_at", "2026-10-10 00:30:00+00:00")
				}
				part.rows[i] = row
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := benchmarkSnapshotProjection(part); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchmarkSnapshotProjection(part snapshotData) error {
	ctx := context.Background()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		return err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s := &Snapshot{At: time.Now().UTC(), redactor: newRedactor(), counts: map[string]int64{}, columns: map[string][]string{}, projectionCache: map[string]projectedText{}}
	s.redactor.prepare()
	if err := s.projectTable(ctx, tx, conn, part, &snapshotPacer{}); err != nil {
		return err
	}
	return tx.Commit()
}
