package agent

import "context"

type snapshotQuerySlotKey struct{}

// On a two-core host, two analysis SQL calls must not occupy both CPUs at
// once. This service-owned gate applies only to private analysis databases.
// Queue time counts toward the unchanged SQL deadline. Nested Measure/Query
// calls carry the acquired slot in their context rather than acquiring twice.
func (s *Snapshot) enterQuery(ctx context.Context) (context.Context, func(), error) {
	if s.querySlot == nil || ctx.Value(snapshotQuerySlotKey{}) == s.querySlot {
		return ctx, func() {}, nil
	}
	select {
	case s.querySlot <- struct{}{}:
		return context.WithValue(ctx, snapshotQuerySlotKey{}, s.querySlot), func() { <-s.querySlot }, nil
	case <-ctx.Done():
		return ctx, func() {}, ctx.Err()
	}
}
