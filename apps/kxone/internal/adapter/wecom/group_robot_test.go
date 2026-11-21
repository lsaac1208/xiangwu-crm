package wecom

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

type groupRobotSettingRepo struct {
	values map[string]string
}

func (r *groupRobotSettingRepo) List(context.Context, string) ([]*types.SystemSetting, error) {
	return nil, nil
}

func (r *groupRobotSettingRepo) GetByKey(_ context.Context, key string) (*types.SystemSetting, error) {
	value, ok := r.values[key]
	if !ok {
		return nil, nil
	}
	return &types.SystemSetting{Key: key, Value: value}, nil
}

func (r *groupRobotSettingRepo) Upsert(context.Context, *types.SystemSetting) error { return nil }

type groupRobotNotificationRecorder struct {
	repo.GroupRobotNotificationRepository
	created *types.GroupRobotNotification
}

type groupRobotScanWakeRecorder struct {
	calls int
	err   error
}

func (r *groupRobotScanWakeRecorder) WakeGroupRobotScan(context.Context) error {
	r.calls++
	return r.err
}

func (r *groupRobotNotificationRecorder) Create(_ context.Context, notification *types.GroupRobotNotification) (bool, error) {
	copyNotification := *notification
	r.created = &copyNotification
	return true, nil
}

func TestGroupRobotNotifierRecordsDurableNotification(t *testing.T) {
	recorder := &groupRobotNotificationRecorder{}
	waker := &groupRobotScanWakeRecorder{}
	notifier := NewDatabaseGroupRobotNotifier(&groupRobotSettingRepo{values: map[string]string{}}).
		WithNotificationRepository(recorder).
		WithScanWaker(waker)
	if err := notifier.NotifyLead(context.Background(), GroupRobotLead{LeadID: 42, BusinessVersion: 3, Source: "tencent"}); err != nil {
		t.Fatalf("record notification: %v", err)
	}
	if recorder.created == nil {
		t.Fatal("notification was not recorded")
	}
	if recorder.created.EventKey != "new-lead:42" || recorder.created.LeadID != 42 || recorder.created.BusinessVersion != 3 {
		t.Fatalf("unexpected notification: %+v", recorder.created)
	}
	if waker.calls != 1 {
		t.Fatalf("worker wake calls = %d, want 1", waker.calls)
	}
}

func TestGroupRobotNotifierKeepsLedgerWhenImmediateWakeFails(t *testing.T) {
	recorder := &groupRobotNotificationRecorder{}
	waker := &groupRobotScanWakeRecorder{err: errors.New("redis unavailable")}
	notifier := NewDatabaseGroupRobotNotifier(&groupRobotSettingRepo{values: map[string]string{}}).
		WithNotificationRepository(recorder).
		WithScanWaker(waker)
	if err := notifier.NotifyLead(context.Background(), GroupRobotLead{LeadID: 43, BusinessVersion: 1}); err == nil {
		t.Fatal("expected wake failure")
	}
	if recorder.created == nil || recorder.created.EventKey != "new-lead:43" {
		t.Fatalf("durable ledger entry was lost: %+v", recorder.created)
	}
}

func TestGroupRobotSenderSendsTextWithMentions(t *testing.T) {
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()

	notifier := NewDatabaseGroupRobotNotifier(&groupRobotSettingRepo{values: map[string]string{
		groupRobotWebhookSettingKey: server.URL,
	}})
	err := notifier.Send(context.Background(), GroupRobotMessage{Content: "新线索\n客户：张同学", MentionUserIDs: []string{"u-1", "u-1", ""}})
	if err != nil {
		t.Fatalf("send markdown: %v", err)
	}
	for _, expected := range []string{`"msgtype":"text"`, `"mentioned_list":["u-1"]`, "新线索", "张同学"} {
		if !strings.Contains(received, expected) {
			t.Fatalf("request body missing %q: %s", expected, received)
		}
	}
}

// Transport failures must be safe for the worker ledger and admin error logs.
func TestGroupRobotSenderErrorsNeverExposeWebhookCredentials(t *testing.T) {
	const secret = "synthetic-repair-secret"
	tests := []struct {
		name      string
		transport http.RoundTripper
	}{
		{"connection", robotErrorTransport{errors.New("connection to https://example.invalid/send?key=" + secret + " failed")}},
		{"timeout", robotErrorTransport{context.DeadlineExceeded}},
		{"rejected", robotResponseTransport{`{"errcode":40013,"errmsg":"https://example.invalid/?key=` + secret + `"}`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender := NewDatabaseGroupRobotNotifier(&groupRobotSettingRepo{values: map[string]string{groupRobotWebhookSettingKey: "https://example.invalid/send?key=" + secret}})
			sender.client = &http.Client{Transport: tt.transport}
			err := sender.Send(context.Background(), GroupRobotMessage{Content: "synthetic"})
			if err == nil {
				t.Fatal("expected delivery failure")
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "key=") {
				t.Fatalf("credential leaked: %v", err)
			}
			if tt.name == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("timeout category lost: %v", err)
			}
		})
	}
}

type robotErrorTransport struct{ err error }

func (r robotErrorTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, r.err }

type robotResponseTransport struct{ body string }

func (r robotResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(r.body)), Header: make(http.Header)}, nil
}
