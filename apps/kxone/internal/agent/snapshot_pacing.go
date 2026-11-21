package agent

import (
	"context"
	"runtime"
	"time"
)

// Snapshot construction is a background operation sharing the server with CRM
// writes. On small hosts, bound its work bursts instead of assuming all rows
// have the same cost: a lead projects over a hundred columns, while a followup
// has far fewer. The source scan has its own short deadline so fair scheduling
// cannot retain an old WAL snapshot indefinitely. This limiter never runs on
// business requests or model SQL; those retain their deadlines and limits.
type snapshotPacer struct {
	enabled     bool
	started     time.Time
	pauseCredit time.Duration
}

func newSnapshotPacer() *snapshotPacer {
	return &snapshotPacer{enabled: runtime.GOMAXPROCS(0) <= 2, started: time.Now()}
}

func (p *snapshotPacer) checkpoint(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !p.enabled {
		return nil
	}
	work := time.Since(p.started)
	if work < 250*time.Microsecond {
		return nil
	}
	// Target approximately one quarter of a CPU for the serialized builder.
	// A scheduling/IO stall is not active CPU: cap its compensating pause so
	// other host activity cannot create unbounded debt for this run.
	pause := min(work, 2*time.Millisecond) * 3
	// Timers can wake substantially later than requested on a busy small host.
	// Carry that already-yielded time forward instead of sleeping again after
	// every short work slice. Bound the credit so an unrelated long stall never
	// permits an unbounded burst of subsequent background work.
	if p.pauseCredit >= pause {
		p.pauseCredit -= pause
		p.started = time.Now()
		return nil
	}
	pause -= p.pauseCredit
	p.pauseCredit = 0
	waitStarted := time.Now()
	timer := time.NewTimer(pause)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		p.started = time.Now()
		p.pauseCredit = min(max(p.started.Sub(waitStarted)-pause, 0), 6*time.Millisecond)
		return nil
	}
}
