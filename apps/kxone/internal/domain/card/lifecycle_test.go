package card

import (
	"testing"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

func TestLifecycle_IsResponseCodeExpired(t *testing.T) {
	lc := NewLifecycle()

	now := time.Now()

	tests := []struct {
		name     string
		cardTask *types.WeComCardTask
		current  time.Time
		expected bool
	}{
		{
			name: "not expired",
			cardTask: &types.WeComCardTask{
				ResponseCodeExpireAt: ptr(now.Add(10 * time.Minute)),
			},
			current:  now,
			expected: false,
		},
		{
			name: "expired",
			cardTask: &types.WeComCardTask{
				ResponseCodeExpireAt: ptr(now.Add(-10 * time.Minute)),
			},
			current:  now,
			expected: true,
		},
		{
			name: "nil expire time means expired",
			cardTask: &types.WeComCardTask{
				ResponseCodeExpireAt: nil,
			},
			current:  now,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := lc.IsResponseCodeExpired(tt.cardTask, tt.current)
			if result != tt.expected {
				t.Errorf("IsResponseCodeExpired() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestLifecycle_ShouldUpdateCard(t *testing.T) {
	lc := NewLifecycle()

	now := time.Now()

	tests := []struct {
		name     string
		cardTask *types.WeComCardTask
		expected bool
	}{
		{
			name: "sent card with valid response_code",
			cardTask: &types.WeComCardTask{
				CardStatus:           types.CardStatusSent,
				ResponseCodeExpireAt: ptr(now.Add(10 * time.Minute)),
			},
			expected: true,
		},
		{
			name: "updated card with valid response_code",
			cardTask: &types.WeComCardTask{
				CardStatus:           types.CardStatusUpdated,
				ResponseCodeExpireAt: ptr(now.Add(10 * time.Minute)),
			},
			expected: true,
		},
		{
			name: "expired response_code",
			cardTask: &types.WeComCardTask{
				CardStatus:           types.CardStatusSent,
				ResponseCodeExpireAt: ptr(now.Add(-10 * time.Minute)),
			},
			expected: false,
		},
		{
			name: "update failed status",
			cardTask: &types.WeComCardTask{
				CardStatus:           types.CardStatusUpdateFailed,
				ResponseCodeExpireAt: ptr(now.Add(10 * time.Minute)),
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := lc.ShouldUpdateCard(tt.cardTask, now)
			if result != tt.expected {
				t.Errorf("ShouldUpdateCard() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestLifecycle_ShouldResendCardAtRetryLimit(t *testing.T) {
	lc := NewLifecycle()
	now := time.Now()

	cardTask := &types.WeComCardTask{
		CardStatus:           types.CardStatusUpdateFailed,
		RetryCount:           types.MaxCardRetryCount,
		ResponseCodeExpireAt: ptr(now.Add(10 * time.Minute)),
	}

	if !lc.ShouldResendCard(cardTask, now) {
		t.Fatal("expected card to be resendable at retry limit")
	}
	if lc.ShouldRetry(cardTask, now) {
		t.Fatal("expected card not to schedule another retry at retry limit")
	}
}

func TestLifecycle_ShouldRetry(t *testing.T) {
	lc := NewLifecycle()

	now := time.Now()

	tests := []struct {
		name     string
		cardTask *types.WeComCardTask
		expected bool
	}{
		{
			name: "update failed, retry count < 3, retry time reached",
			cardTask: &types.WeComCardTask{
				CardStatus:  types.CardStatusUpdateFailed,
				RetryCount:  1,
				NextRetryAt: ptr(now.Add(-1 * time.Minute)),
			},
			expected: true,
		},
		{
			name: "update failed, retry count at max",
			cardTask: &types.WeComCardTask{
				CardStatus:  types.CardStatusUpdateFailed,
				RetryCount:  3,
				NextRetryAt: ptr(now.Add(-1 * time.Minute)),
			},
			expected: false,
		},
		{
			name: "update failed, retry count over max",
			cardTask: &types.WeComCardTask{
				CardStatus:  types.CardStatusUpdateFailed,
				RetryCount:  4,
				NextRetryAt: ptr(now.Add(-1 * time.Minute)),
			},
			expected: false,
		},
		{
			name: "update failed, retry time not reached",
			cardTask: &types.WeComCardTask{
				CardStatus:  types.CardStatusUpdateFailed,
				RetryCount:  1,
				NextRetryAt: ptr(now.Add(10 * time.Minute)),
			},
			expected: false,
		},
		{
			name: "not failed status",
			cardTask: &types.WeComCardTask{
				CardStatus:  types.CardStatusSent,
				RetryCount:  1,
				NextRetryAt: ptr(now.Add(-1 * time.Minute)),
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := lc.ShouldRetry(tt.cardTask, now)
			if result != tt.expected {
				t.Errorf("ShouldRetry() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestLifecycle_CalculateNextRetryTime(t *testing.T) {
	lc := NewLifecycle()

	now := time.Now()

	tests := []struct {
		name          string
		retryCount    int
		expectedDelay time.Duration
	}{
		{
			name:          "first retry: 10s",
			retryCount:    0,
			expectedDelay: 10 * time.Second,
		},
		{
			name:          "second retry: 30s",
			retryCount:    1,
			expectedDelay: 30 * time.Second,
		},
		{
			name:          "third retry: 60s",
			retryCount:    2,
			expectedDelay: 60 * time.Second,
		},
		{
			name:          "beyond limit: still 60s",
			retryCount:    5,
			expectedDelay: 60 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := lc.CalculateNextRetryTime(now, tt.retryCount)
			expected := now.Add(tt.expectedDelay)

			if !result.Equal(expected) {
				t.Errorf("CalculateNextRetryTime(%v) = %v, want %v",
					tt.retryCount, result, expected)
			}
		})
	}
}

func TestLifecycle_CalculateNextRetryTimeWithConfiguredIntervals(t *testing.T) {
	lc := NewLifecycleWithRetryIntervals([]time.Duration{2 * time.Second, 4 * time.Second})
	now := time.Now()

	first := lc.CalculateNextRetryTime(now, 0)
	if !first.Equal(now.Add(2 * time.Second)) {
		t.Fatalf("first retry = %v, want %v", first, now.Add(2*time.Second))
	}

	overflow := lc.CalculateNextRetryTime(now, 5)
	if !overflow.Equal(now.Add(4 * time.Second)) {
		t.Fatalf("overflow retry = %v, want %v", overflow, now.Add(4*time.Second))
	}
}

func TestLifecycle_BuildCardContentIncludesActionURL(t *testing.T) {
	lc := NewLifecycle().WithBaseURL("https://example.com/app/")
	lead := &types.Lead{
		ID:                   42,
		CustomerName:         "客户",
		CustomerMobileMasked: "138****0000",
		SourceChannel:        "lead-gateway:tencent",
		ProductName:          "产品",
	}

	content := lc.BuildCardContent(types.CardSceneSupervisorPendingDispatch, lead)
	if content == nil {
		t.Fatal("content is nil")
	}
	if content.ActionURL == "" {
		t.Fatal("action url is empty")
	}
	if content.ActionURL != "https://example.com/app/leads/42" {
		t.Fatalf("action url = %q", content.ActionURL)
	}
}

func TestLifecycle_BuildCardContentUsesRelativeURLWithoutBaseURL(t *testing.T) {
	lc := NewLifecycle()
	lead := &types.Lead{ID: 42}

	content := lc.BuildCardContent(types.CardSceneSupervisorPendingDispatch, lead)
	if content == nil {
		t.Fatal("content is nil")
	}
	if content.ActionURL != "/leads/42" {
		t.Fatalf("action url = %q", content.ActionURL)
	}
}

// 辅助函数
func ptr(t time.Time) *time.Time {
	return &t
}
