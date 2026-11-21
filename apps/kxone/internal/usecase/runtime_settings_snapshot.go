package usecase

import (
	"context"
	"time"

	"github.com/lsaac128/kxone/internal/runtime"
)

// followupPolicySnapshot freezes database-backed runtime settings before a
// transaction starts. SQLite deployments commonly run with one connection;
// querying settings through the non-transaction repository from inside an
// open transaction would otherwise wait forever for that same connection.
type followupPolicySnapshot struct {
	firstDue       time.Duration
	dueByStar      [6]time.Duration
	reminderOffset []time.Duration
	poolMax        int
	poolCooldown   time.Duration
	poolUnclaimed  time.Duration
	poolGrace      time.Duration
}

func snapshotFollowupPolicy(ctx context.Context, settings runtime.FollowupPolicySettings) runtime.FollowupPolicySettings {
	if settings == nil {
		return nil
	}
	snapshot := &followupPolicySnapshot{
		firstDue:       settings.FirstFollowupDue(ctx),
		reminderOffset: append([]time.Duration(nil), settings.FollowupReminderOffsets(ctx)...),
		poolMax:        settings.PoolMaxActiveLeads(ctx),
		poolCooldown:   settings.PoolCooldown(ctx),
		poolUnclaimed:  settings.PoolUnclaimedTimeout(ctx),
		poolGrace:      settings.PoolFollowupGrace(ctx),
	}
	for star := 1; star <= 5; star++ {
		snapshot.dueByStar[star] = settings.FollowupDueByStar(ctx, star)
	}
	return snapshot
}

func (s *followupPolicySnapshot) FirstFollowupDue(context.Context) time.Duration { return s.firstDue }
func (s *followupPolicySnapshot) FollowupDueByStar(_ context.Context, star int) time.Duration {
	if star < 1 || star > 5 {
		return 0
	}
	return s.dueByStar[star]
}
func (s *followupPolicySnapshot) FollowupReminderOffsets(context.Context) []time.Duration {
	return append([]time.Duration(nil), s.reminderOffset...)
}
func (s *followupPolicySnapshot) PoolMaxActiveLeads(context.Context) int     { return s.poolMax }
func (s *followupPolicySnapshot) PoolCooldown(context.Context) time.Duration { return s.poolCooldown }
func (s *followupPolicySnapshot) PoolUnclaimedTimeout(context.Context) time.Duration {
	return s.poolUnclaimed
}
func (s *followupPolicySnapshot) PoolFollowupGrace(context.Context) time.Duration { return s.poolGrace }

type appointmentPolicySnapshot struct {
	defaultDuration time.Duration
	buffer          time.Duration
	dailyLimit      int
	workStart       int
	workEnd         int
	noShowFollowup  time.Duration
}

func snapshotAppointmentPolicy(ctx context.Context, settings runtime.AppointmentPolicySettings) runtime.AppointmentPolicySettings {
	if settings == nil {
		return nil
	}
	start, end := settings.AppointmentWorkdayHours(ctx)
	return &appointmentPolicySnapshot{
		defaultDuration: settings.AppointmentDefaultDuration(ctx),
		buffer:          settings.AppointmentBuffer(ctx),
		dailyLimit:      settings.AppointmentDailySoftLimit(ctx),
		workStart:       start,
		workEnd:         end,
		noShowFollowup:  settings.AppointmentNoShowFollowup(ctx),
	}
}

func (s *appointmentPolicySnapshot) AppointmentDefaultDuration(context.Context) time.Duration {
	return s.defaultDuration
}
func (s *appointmentPolicySnapshot) AppointmentBuffer(context.Context) time.Duration { return s.buffer }
func (s *appointmentPolicySnapshot) AppointmentDailySoftLimit(context.Context) int {
	return s.dailyLimit
}
func (s *appointmentPolicySnapshot) AppointmentWorkdayHours(context.Context) (int, int) {
	return s.workStart, s.workEnd
}
func (s *appointmentPolicySnapshot) AppointmentNoShowFollowup(context.Context) time.Duration {
	return s.noShowFollowup
}
