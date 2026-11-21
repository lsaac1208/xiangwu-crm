package agent

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

func pacedSourceFixture(t *testing.T) *gorm.DB {
	t.Helper()
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	if err := db.Exec("UPDATE leads SET source_channel='revision-0' WHERE id=1").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v+1 FROM n WHERE v<6000)
INSERT INTO lead_followups(id,lead_id,sales_user_id,followup_result,remark,created_at)
SELECT v,1,'worker','connected','revision-0','2026-10-08 00:30:00+00:00' FROM n`).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func TestPacedSourceReadRemainsOneSnapshotWhileBusinessCommits(t *testing.T) {
	db := pacedSourceFixture(t)
	stop := make(chan struct{})
	finished := make(chan error, 1)
	var commits atomic.Int64
	go func() {
		for {
			select {
			case <-stop:
				finished <- nil
				return
			default:
			}
			value := fmt.Sprintf("revision-%d", commits.Load()+1)
			if err := db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Exec("UPDATE leads SET source_channel=? WHERE id=1", value).Error; err != nil {
					return err
				}
				return tx.Exec("UPDATE lead_followups SET remark=? WHERE id=6000", value).Error
			}); err != nil {
				finished <- err
				return
			}
			commits.Add(1)
			time.Sleep(time.Millisecond)
		}
	}()
	started := time.Now()
	parts, _, _, err := readSnapshotSource(t.Context(), db, &snapshotPacer{enabled: true, started: started})
	close(stop)
	writeErr := <-finished
	if err != nil || writeErr != nil {
		t.Fatalf("paced read or concurrent business write failed: read=%v write=%v", err, writeErr)
	}
	var leadRevision, followupRevision string
	for _, part := range parts {
		indexes := fieldIndexes(part.spec)
		for _, row := range part.rows {
			if part.spec.name == "leads" && numberAt(row, indexes, "id") == 1 {
				leadRevision = textAt(row, indexes, "source_channel")
			}
			if part.spec.name == "lead_followups" && numberAt(row, indexes, "id") == 6000 {
				followupRevision = textAt(row, indexes, "remark")
			}
		}
	}
	if leadRevision == "" || leadRevision != followupRevision {
		t.Fatalf("source tables came from different committed revisions: lead=%q followup=%q", leadRevision, followupRevision)
	}
	if commits.Load() == 0 {
		t.Fatal("no business write completed during the source scan")
	}
	t.Logf("source_rows=6001 source_read_ms=%d concurrent_commits=%d", time.Since(started).Milliseconds(), commits.Load())
}

func TestPacedSourceReadCancellationReleasesReadTransaction(t *testing.T) {
	db := pacedSourceFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := time.Now()
	finished := make(chan error, 1)
	go func() {
		parts, _, _, err := readSnapshotSource(ctx, db, &snapshotPacer{enabled: true, started: time.Now()})
		if len(parts) != 0 {
			finished <- errors.New("canceled source scan returned partial data")
			return
		}
		finished <- err
	}()
	time.Sleep(15 * time.Millisecond)
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled source scan, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled source read did not release promptly")
	}
	writeCtx, finishWrite := context.WithTimeout(t.Context(), time.Second)
	defer finishWrite()
	if err := db.WithContext(writeCtx).Exec("UPDATE leads SET profile_version=profile_version+1 WHERE id=1").Error; err != nil {
		t.Fatalf("business write after cancellation failed: %v", err)
	}
	pooled, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	var busy, pages, checkpointed int
	if err := pooled.QueryRowContext(writeCtx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &pages, &checkpointed); err != nil || busy != 0 {
		t.Fatalf("source reader retained a WAL snapshot after cancellation: busy=%d error=%v", busy, err)
	}
	t.Logf("source_cancel_release_ms=%d", time.Since(started).Milliseconds())
}

func TestPacedSourceReadTimeoutDoesNotReturnPartialDataOrMaskRunDeadline(t *testing.T) {
	db := pacedSourceFixture(t)
	for _, parentDeadline := range []bool{false, true} {
		t.Run(fmt.Sprintf("parent_deadline_%v", parentDeadline), func(t *testing.T) {
			// Keep the database connection unavailable until the deadline.
			// A small fixture can finish in <15ms on a fast compiler/host;
			// elapsed scan speed must not determine whether this test expires.
			pooled, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			previousLimit := pooled.Stats().MaxOpenConnections
			pooled.SetMaxOpenConns(1)
			defer pooled.SetMaxOpenConns(previousLimit)
			held, err := pooled.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer held.Close()
			ctx := t.Context()
			limit := 15 * time.Millisecond
			if parentDeadline {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, limit)
				defer cancel()
				limit = 10 * time.Second
			}
			started := time.Now()
			parts, at, missing, err := readSnapshotSourceWithin(ctx, db, &snapshotPacer{enabled: true, started: started}, limit)
			if err := held.Close(); err != nil {
				t.Fatal(err)
			}
			if err == nil || !errors.Is(err, context.DeadlineExceeded) || len(parts) != 0 || !at.IsZero() || len(missing) != 0 {
				t.Fatalf("timeout must return only an explicit error: err=%v rows=%d snapshot_at=%s", err, len(parts), at)
			}
			if errors.Is(err, ErrSnapshotSourceTimeout) == parentDeadline {
				t.Fatalf("source and parent deadline classification are confused: parent=%v error=%v", parentDeadline, err)
			}
			checkCtx, cancelCheck := context.WithTimeout(t.Context(), time.Second)
			defer cancelCheck()
			var busy, pages, checkpointed int
			if err := pooled.QueryRowContext(checkCtx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &pages, &checkpointed); err != nil || busy != 0 {
				t.Fatalf("timed-out source retained its WAL snapshot: busy=%d error=%v", busy, err)
			}
		})
	}
}
