package agent

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSnapshotCancellationDuringProjectionRemovesPrivateFiles(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	if err := db.Exec(`WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v+1 FROM n WHERE v<6000)
INSERT INTO lead_followups(id,lead_id,sales_user_id,followup_result,remark,created_at)
SELECT v,1,'employee-a','connected','Followup note','2026-10-08 00:30:00+00:00' FROM n`).Error; err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		snapshot, err := BuildSnapshot(ctx, db, dir)
		if snapshot != nil {
			snapshot.Close()
		}
		finished <- err
	}()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case err := <-finished:
			t.Fatalf("build ended before projection cancellation: %v", err)
		case <-deadline.C:
			t.Fatal("projection did not begin")
		case <-poll.C:
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "snapshot-") {
					cancel()
					goto canceled
				}
			}
		}
	}
canceled:
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("canceled projection returned a usable snapshot")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled fixed-row projection did not stop promptly")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("canceled snapshot retained private files: %d entries, %v", len(entries), err)
	}
	var count int64
	if err := db.Table("lead_followups").Count(&count).Error; err != nil || count != 6000 {
		t.Fatalf("canceled snapshot changed source rows: count=%d error=%v", count, err)
	}
}

func TestSnapshotTemporaryWriterDoesNotChangeBusinessDurability(t *testing.T) {
	db := snapshotFixtureDB(t)
	createSnapshotLead(t, db, 1)
	pooled, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pooled.SetMaxOpenConns(1)
	readSettings := func() (string, int) {
		var mode string
		var synchronous int
		if err := pooled.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
			t.Fatal(err)
		}
		if err := pooled.QueryRow("PRAGMA synchronous").Scan(&synchronous); err != nil {
			t.Fatal(err)
		}
		return mode, synchronous
	}
	mode, synchronous := readSettings()
	snapshot, err := BuildSnapshot(t.Context(), db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	afterMode, afterSynchronous := readSettings()
	if mode != afterMode || synchronous != afterSynchronous || mode != "wal" || synchronous == 0 {
		t.Fatalf("business durability changed: %s/%d to %s/%d", mode, synchronous, afterMode, afterSynchronous)
	}
}
