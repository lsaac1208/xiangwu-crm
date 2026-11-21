package worker

import (
	"testing"
	"time"
)

func TestDurationMinutesRoundsUp(t *testing.T) {
	tests := []struct {
		delay time.Duration
		want  int
	}{
		{delay: 0, want: 0},
		{delay: 30 * time.Second, want: 1},
		{delay: time.Minute, want: 1},
		{delay: 90 * time.Second, want: 2},
	}

	for _, tt := range tests {
		if got := durationMinutes(tt.delay); got != tt.want {
			t.Fatalf("durationMinutes(%v) = %d, want %d", tt.delay, got, tt.want)
		}
	}
}

func TestReminderOrdinalUsesCurrentConfiguredRules(t *testing.T) {
	delays := []time.Duration{10 * time.Minute, 3 * time.Minute, 5 * time.Minute}

	index, ok := reminderOrdinal(5, 5, delays)
	if !ok {
		t.Fatal("expected configured reminder to be allowed")
	}
	if index != 1 {
		t.Fatalf("index = %d, want 1", index)
	}

	if _, ok := reminderOrdinal(4, 4, delays); ok {
		t.Fatal("expected stale configured reminder to be skipped")
	}

	if _, ok := reminderOrdinal(3, 3, nil); ok {
		t.Fatal("expected disabled reminder rules to skip task")
	}
}

func TestReminderOrdinalSupportsLegacyPayload(t *testing.T) {
	index, ok := reminderOrdinal(0, 7, []time.Duration{3 * time.Minute, 5 * time.Minute, 10 * time.Minute})
	if !ok {
		t.Fatal("expected legacy payload to be allowed by elapsed minutes")
	}
	if index != 1 {
		t.Fatalf("index = %d, want 1", index)
	}
}

func TestReminderLevelByIndex(t *testing.T) {
	if got := reminderLevelByIndex(0); got != "" {
		t.Fatalf("level 0 = %q, want empty", got)
	}
	if got := reminderLevelByIndex(1); got != "【重要】" {
		t.Fatalf("level 1 = %q, want important", got)
	}
	if got := reminderLevelByIndex(2); got != "【紧急】" {
		t.Fatalf("level 2 = %q, want urgent", got)
	}
}
