package agent

import (
	"context"
	"time"
)

// One anchor per run. Tool work and successive successful calls do not renew
// this allowance. Short test/deployment timeouts scale the same proportions;
// the production 180-second run gives primary 90s and protects backup 60s.
type primaryTimeWindow struct{ deadline time.Time }

func newPrimaryTimeWindow(ctx context.Context, now time.Time, runTimeout time.Duration) primaryTimeWindow {
	window := min(90*time.Second, runTimeout/2)
	reserve := min(60*time.Second, runTimeout/3)
	deadline := now.Add(window)
	if whole, ok := ctx.Deadline(); ok && whole.Add(-reserve).Before(deadline) {
		deadline = whole.Add(-reserve)
	}
	return primaryTimeWindow{deadline: deadline}
}

func (w primaryTimeWindow) expired(now time.Time) bool {
	return !w.deadline.IsZero() && !now.Before(w.deadline)
}

func (w primaryTimeWindow) callContext(ctx context.Context, now time.Time) (context.Context, context.CancelFunc) {
	deadline := now.Add(60 * time.Second)
	if w.deadline.Before(deadline) {
		deadline = w.deadline
	}
	return context.WithDeadline(ctx, deadline)
}
