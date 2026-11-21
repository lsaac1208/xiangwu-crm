package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/agent"
	"gorm.io/gorm"
)

// Optional diagnostic output only. Sampling, application code, transaction
// boundaries and the original acceptance oracle remain in the main test.
type agentHTTPPerfDBPhase struct {
	Operation string    `json:"operation"`
	Started   time.Time `json:"started"`
	Ended     time.Time `json:"ended"`
	ElapsedNS int64     `json:"elapsed_ns"`
	Failed    bool      `json:"failed"`
}

type agentHTTPPerfInterval struct {
	DatabasePhases     []agentHTTPPerfDBPhase `json:"database_phases,omitempty"`
	Sequence           int                    `json:"sequence"`
	ClientStarted      time.Time              `json:"client_started"`
	ServerStarted      time.Time              `json:"server_started"`
	TransactionStarted time.Time              `json:"transaction_started"`
	TransactionEnded   time.Time              `json:"transaction_ended"`
	ServerEnded        time.Time              `json:"server_ended"`
	ClientEnded        time.Time              `json:"client_ended"`
}

// Only fixed operation names and monotonic timings are recorded; query text,
// arguments, results, and error text never enter diagnostics.
func (t *agentHTTPPerfTrace) recordDBPhase(operation string, started, ended time.Time, failed bool) {
	if t == nil || t.interval == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.interval.DatabasePhases = append(t.interval.DatabasePhases, agentHTTPPerfDBPhase{
		Operation: operation, Started: started, Ended: ended,
		ElapsedNS: ended.Sub(started).Nanoseconds(), Failed: failed,
	})
}

type agentHTTPPerfDiagnostics struct {
	path      string
	mu        sync.Mutex
	intervals map[int]agentHTTPPerfInterval
}

func newAgentHTTPPerfDiagnostics(path string) *agentHTTPPerfDiagnostics {
	if path == "" {
		return nil
	}
	return &agentHTTPPerfDiagnostics{path: path, intervals: make(map[int]agentHTTPPerfInterval)}
}

func (d *agentHTTPPerfDiagnostics) recordClient(sequence int, started time.Time, elapsed time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	i := d.intervals[sequence]
	i.Sequence, i.ClientStarted, i.ClientEnded = sequence, started, started.Add(elapsed)
	d.intervals[sequence] = i
}

func (d *agentHTTPPerfDiagnostics) recordServer(interval agentHTTPPerfInterval) {
	d.mu.Lock()
	defer d.mu.Unlock()
	old := d.intervals[interval.Sequence]
	interval.ClientStarted, interval.ClientEnded = old.ClientStarted, old.ClientEnded
	d.intervals[interval.Sequence] = interval
}

func (d *agentHTTPPerfDiagnostics) save(db *gorm.DB, runs []agentHTTPPerfRun) error {
	d.mu.Lock()
	intervals := make([]agentHTTPPerfInterval, 0, len(d.intervals))
	for _, interval := range d.intervals {
		intervals = append(intervals, interval)
	}
	d.mu.Unlock()
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].Sequence < intervals[j].Sequence })
	type snapshotInterval struct {
		RunID      string           `json:"run_id"`
		SnapshotAt *time.Time       `json:"snapshot_at"`
		ReadyAt    time.Time        `json:"ready_at"`
		Metrics    map[string]int64 `json:"metrics"`
	}
	var snapshots []snapshotInterval
	for _, run := range runs {
		var stored agent.Run
		if err := db.Select("id", "snapshot_at").First(&stored, "id = ?", run.ID).Error; err != nil {
			return err
		}
		var event agent.Event
		if err := db.Where("run_id = ? AND type = ?", run.ID, "snapshot_ready").First(&event).Error; err != nil {
			return err
		}
		snapshot := snapshotInterval{RunID: run.ID, SnapshotAt: stored.SnapshotAt, ReadyAt: event.CreatedAt}
		if err := json.Unmarshal([]byte(event.Data), &snapshot.Metrics); err != nil {
			return err
		}
		snapshots = append(snapshots, snapshot)
	}
	// Derive durations before JSON serialization so time.Sub uses Go's monotonic
	// clock. Absolute timestamps let the same samples align with runtime trace.
	requests := make([]map[string]any, 0, len(intervals))
	for _, i := range intervals {
		requests = append(requests, map[string]any{
			"interval": i, "before_handler_ns": i.ServerStarted.Sub(i.ClientStarted).Nanoseconds(),
			"pre_transaction_ns":  i.TransactionStarted.Sub(i.ServerStarted).Nanoseconds(),
			"transaction_ns":      i.TransactionEnded.Sub(i.TransactionStarted).Nanoseconds(),
			"post_transaction_ns": i.ServerEnded.Sub(i.TransactionEnded).Nanoseconds(),
			"after_handler_ns":    i.ClientEnded.Sub(i.ServerEnded).Nanoseconds(),
		})
	}
	data, err := json.MarshalIndent(map[string]any{"requests": requests, "snapshots": snapshots}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(d.path, data, 0600)
}

func TestAgentHTTPPerfDiagnosticsPreservesBothRequestHalves(t *testing.T) {
	if newAgentHTTPPerfDiagnostics("") != nil {
		t.Fatal("diagnostics must be opt-in")
	}
	for _, serverFirst := range []bool{true, false} {
		d := newAgentHTTPPerfDiagnostics("unused")
		start := time.Now()
		interval := agentHTTPPerfInterval{DatabasePhases: []agentHTTPPerfDBPhase{{Operation: "begin", Started: start.Add(2 * time.Millisecond), Ended: start.Add(2500 * time.Microsecond), ElapsedNS: int64(500 * time.Microsecond)}}, Sequence: 1, ServerStarted: start.Add(time.Millisecond), TransactionStarted: start.Add(2 * time.Millisecond), TransactionEnded: start.Add(3 * time.Millisecond), ServerEnded: start.Add(4 * time.Millisecond)}
		if serverFirst {
			d.recordServer(interval)
			d.recordClient(1, start, 5*time.Millisecond)
		} else {
			d.recordClient(1, start, 5*time.Millisecond)
			d.recordServer(interval)
		}
		got := d.intervals[1]
		if len(got.DatabasePhases) != 1 || got.DatabasePhases[0] != interval.DatabasePhases[0] || got.ClientStarted != start || got.ClientEnded.Sub(start) != 5*time.Millisecond || got.TransactionStarted != interval.TransactionStarted || got.ServerEnded != interval.ServerEnded {
			t.Fatalf("request diagnostic merge lost one side: %+v", got)
		}
	}
}

func TestAgentHTTPPerfDatabasePhases(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pool := &agentHTTPPerfPool{DB: db}
	for _, enabled := range []bool{false, true} {
		trace := &agentHTTPPerfTrace{}
		if enabled {
			trace.interval = &agentHTTPPerfInterval{}
		}
		ctx := context.WithValue(context.Background(), agentHTTPPerfTraceKey{}, trace)
		conn, err := pool.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		tx := conn.(*agentHTTPPerfTx)
		if _, err := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS phase_fixture (value TEXT)"); err != nil {
			t.Fatal(err)
		}
		// Error diagnostics expose only a boolean, never this SQL or its argument.
		if _, err := tx.ExecContext(ctx, "INSERT INTO private_missing_table VALUES (?)", "private argument"); err == nil {
			t.Fatal("expected invalid SQL to fail")
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if trace.transactions != 1 || trace.transactionTime <= 0 {
			t.Fatalf("transaction oracle changed: %+v", trace)
		}
		if !enabled {
			if trace.interval != nil {
				t.Fatal("disabled diagnostics recorded an interval")
			}
			continue
		}
		phases := trace.interval.DatabasePhases
		want := []string{"begin", "exec", "exec", "commit"}
		if len(phases) != len(want) {
			t.Fatalf("phases = %+v", phases)
		}
		for i, phase := range phases {
			if phase.Operation != want[i] || phase.Started.IsZero() || phase.Ended.Before(phase.Started) || phase.ElapsedNS != phase.Ended.Sub(phase.Started).Nanoseconds() || phase.Failed != (i == 2) {
				t.Fatalf("invalid phase %d: %+v", i, phase)
			}
			if i > 0 && phase.Started.Before(phases[i-1].Ended) {
				t.Fatalf("phases overlap: %+v", phases)
			}
		}
		if phases[0].Started != trace.interval.TransactionStarted || phases[len(phases)-1].Ended.After(trace.interval.TransactionEnded) {
			t.Fatalf("phases are outside transaction interval: %+v", trace.interval)
		}
		encoded, err := json.Marshal(trace.interval)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "INSERT") {
			t.Fatalf("diagnostics must not contain SQL, args or error text: %s", encoded)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	trace := &agentHTTPPerfTrace{interval: &agentHTTPPerfInterval{}}
	ctx = context.WithValue(ctx, agentHTTPPerfTraceKey{}, trace)
	if _, err := pool.BeginTx(ctx, nil); err == nil {
		t.Fatal("expected canceled BeginTx to fail")
	}
	if len(trace.interval.DatabasePhases) != 1 || !trace.interval.DatabasePhases[0].Failed || trace.transactions != 1 {
		t.Fatalf("failed begin was lost: %+v", trace)
	}
}
