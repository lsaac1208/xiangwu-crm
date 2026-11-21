package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/wecom"
	"github.com/lsaac128/kxone/pkg/types"
)

type fakeSettingRepo struct {
	values map[string]string
}

func (r *fakeSettingRepo) List(ctx context.Context, group string) ([]*types.SystemSetting, error) {
	return nil, nil
}

func (r *fakeSettingRepo) GetByKey(ctx context.Context, key string) (*types.SystemSetting, error) {
	value, ok := r.values[key]
	if !ok {
		return nil, nil
	}
	return &types.SystemSetting{Key: key, Value: value}, nil
}

func (r *fakeSettingRepo) Upsert(ctx context.Context, setting *types.SystemSetting) error {
	return nil
}

func TestDatabaseRuntimeSettings(t *testing.T) {
	settings := NewDatabaseRuntimeSettingsWithWeComConfig(&fakeSettingRepo{
		values: map[string]string{
			SettingWeComCorpID:            "corp-db",
			SettingWeComAgentID:           "1000002",
			SettingWeComSecret:            "secret-db",
			SettingWeComToken:             "token-db",
			SettingWeComEncodingAESKey:    "aes-db",
			SettingDispatchReminderMins:   "1, 2, bad, 5",
			SettingClaimReminderMins:      "4,6",
			SettingCardRetrySeconds:       "7, 9",
			SettingWorkflowV2Enabled:      "true",
			SettingAppointmentEnabled:     "true",
			SettingPoolEnabled:            "true",
			SettingPoolAutoReturnEnabled:  "true",
			SettingFirstFollowupDueMins:   "90",
			SettingFollowupReminderMins:   "0,30,120",
			SettingStar5FollowupHours:     "12",
			SettingPoolMaxActiveLeads:     "30",
			SettingPoolCooldownMins:       "60",
			SettingPoolUnclaimedMins:      "180",
			SettingPoolFollowupGraceMins:  "45",
			SettingAppointmentDefaultMins: "75",
			SettingAppointmentBufferMins:  "20",
			SettingAppointmentDailyLimit:  "5",
			SettingAppointmentWorkStart:   "8",
			SettingAppointmentWorkEnd:     "20",
			SettingAppointmentNoShowMins:  "30",
		},
	}, wecom.Config{
		CorpID:         "corp-env",
		AgentID:        42,
		Secret:         "secret-env",
		Token:          "token-env",
		EncodingAESKey: "aes-env",
	})

	ctx := context.Background()
	cfg := settings.GetWeComConfig(ctx)
	if cfg.CorpID != "corp-db" || cfg.AgentID != 1000002 || cfg.Secret != "secret-db" || cfg.Token != "token-db" || cfg.EncodingAESKey != "aes-db" {
		t.Fatalf("wecom config mismatch: %+v", cfg)
	}

	assertDurations(t, settings.DispatchReminderDelays(ctx), []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute})
	assertDurations(t, settings.ClaimReminderDelays(ctx), []time.Duration{4 * time.Minute, 6 * time.Minute})
	assertDurations(t, settings.CardRetryDelays(ctx), []time.Duration{7 * time.Second, 9 * time.Second})
	if !settings.WorkflowV2Enabled(ctx) || !settings.AppointmentEnabled(ctx) || !settings.PoolEnabled(ctx) || !settings.PoolAutoReturnEnabled(ctx) {
		t.Fatal("explicitly enabled workflow feature flags must be true")
	}
	if settings.FirstFollowupDue(ctx) != 90*time.Minute || settings.FollowupDueByStar(ctx, 5) != 12*time.Hour {
		t.Fatal("follow-up policy settings were not loaded")
	}
	assertDurations(t, settings.FollowupReminderOffsets(ctx), []time.Duration{0, 30 * time.Minute, 2 * time.Hour})
	if settings.PoolMaxActiveLeads(ctx) != 30 || settings.PoolCooldown(ctx) != time.Hour || settings.PoolUnclaimedTimeout(ctx) != 3*time.Hour || settings.PoolFollowupGrace(ctx) != 45*time.Minute {
		t.Fatal("public pool policy settings were not loaded")
	}
	startHour, endHour := settings.AppointmentWorkdayHours(ctx)
	if settings.AppointmentDefaultDuration(ctx) != 75*time.Minute || settings.AppointmentBuffer(ctx) != 20*time.Minute || settings.AppointmentDailySoftLimit(ctx) != 5 || startHour != 8 || endHour != 20 || settings.AppointmentNoShowFollowup(ctx) != 30*time.Minute {
		t.Fatal("appointment policy settings were not loaded")
	}
}

func TestDatabaseRuntimeSettingsFallbacks(t *testing.T) {
	settings := NewDatabaseRuntimeSettingsWithWeComConfig(&fakeSettingRepo{
		values: map[string]string{
			SettingWeComCorpID:          "",
			SettingWeComAgentID:         "not-number",
			SettingWeComSecret:          "",
			SettingWeComToken:           "",
			SettingWeComEncodingAESKey:  "",
			SettingDispatchReminderMins: "bad,0",
			SettingCardRetrySeconds:     "-1",
		},
	}, wecom.Config{
		CorpID:         "corp-env",
		AgentID:        42,
		Secret:         "secret-env",
		Token:          "token-env",
		EncodingAESKey: "aes-env",
	})

	ctx := context.Background()
	cfg := settings.GetWeComConfig(ctx)
	if cfg.CorpID != "corp-env" || cfg.AgentID != 42 || cfg.Secret != "secret-env" || cfg.Token != "token-env" || cfg.EncodingAESKey != "aes-env" {
		t.Fatalf("fallback wecom config mismatch: %+v", cfg)
	}

	assertDurations(t, settings.DispatchReminderDelays(ctx), []time.Duration{3 * time.Minute, 5 * time.Minute, 10 * time.Minute})
	assertDurations(t, settings.ClaimReminderDelays(ctx), []time.Duration{3 * time.Minute, 5 * time.Minute, 10 * time.Minute})
	assertDurations(t, settings.CardRetryDelays(ctx), []time.Duration{10 * time.Second, 30 * time.Second, 60 * time.Second})
	if settings.WorkflowV2Enabled(ctx) || settings.AppointmentEnabled(ctx) || settings.PoolEnabled(ctx) || settings.PoolAutoReturnEnabled(ctx) {
		t.Fatal("missing workflow feature flags must default to false")
	}
}

func TestDatabaseRuntimeSettingsEmptyReminderSettingDisablesReminders(t *testing.T) {
	settings := NewDatabaseRuntimeSettingsWithWeComConfig(&fakeSettingRepo{
		values: map[string]string{
			SettingDispatchReminderMins: "",
			SettingClaimReminderMins:    "0",
		},
	}, wecom.Config{})

	ctx := context.Background()
	assertDurations(t, settings.DispatchReminderDelays(ctx), nil)
	assertDurations(t, settings.ClaimReminderDelays(ctx), nil)
}

func TestDatabaseRuntimeSettingsRequiresRepository(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for nil setting repository")
		}
	}()

	_ = NewDatabaseRuntimeSettingsWithWeComConfig(nil, wecom.Config{})
}

func TestStaticRuntimeSettingsUsesBuiltInDefaults(t *testing.T) {
	settings := NewStaticRuntimeSettings(wecom.Config{AgentID: 42})
	ctx := context.Background()

	if settings.WeComAgentID(ctx) != 42 {
		t.Fatalf("agent id = %d, want 42", settings.WeComAgentID(ctx))
	}
	assertDurations(t, settings.DispatchReminderDelays(ctx), []time.Duration{3 * time.Minute, 5 * time.Minute, 10 * time.Minute})
	assertDurations(t, settings.ClaimReminderDelays(ctx), []time.Duration{3 * time.Minute, 5 * time.Minute, 10 * time.Minute})
	assertDurations(t, settings.CardRetryDelays(ctx), []time.Duration{10 * time.Second, 30 * time.Second, 60 * time.Second})
	if settings.WorkflowV2Enabled(ctx) || settings.AppointmentEnabled(ctx) || settings.PoolEnabled(ctx) || settings.PoolAutoReturnEnabled(ctx) {
		t.Fatal("static workflow feature flags must default to false")
	}
}

func TestDatabaseRuntimeSettingsAutoReturnRequiresPool(t *testing.T) {
	settings := NewDatabaseRuntimeSettingsWithWeComConfig(&fakeSettingRepo{values: map[string]string{
		SettingPoolEnabled:           "false",
		SettingPoolAutoReturnEnabled: "true",
	}}, wecom.Config{})
	if settings.PoolEnabled(context.Background()) || settings.PoolAutoReturnEnabled(context.Background()) {
		t.Fatal("auto return must be ineffective while the public pool is disabled")
	}
}

func TestDatabaseRuntimeSettingsInvalidFeatureFlagUsesSafeDefault(t *testing.T) {
	settings := NewDatabaseRuntimeSettingsWithWeComConfig(&fakeSettingRepo{
		values: map[string]string{SettingWorkflowV2Enabled: "enabled"},
	}, wecom.Config{})

	if settings.WorkflowV2Enabled(context.Background()) {
		t.Fatal("invalid feature flag must fall back to false")
	}
}

func assertDurations(t *testing.T, got, want []time.Duration) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("duration count = %d, want %d; got=%+v", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("duration[%d] = %v, want %v; got=%+v", i, got[i], want[i], got)
		}
	}
}
