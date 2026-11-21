package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
	"github.com/lsaac128/kxone/pkg/types"
)

func TestResendCardMarksOldTaskReplacedAfterSuccessfulSend(t *testing.T) {
	ctx := context.Background()
	cardRepo := newFakeCardTaskRepo()
	oldTask := &types.WeComCardTask{
		ID:             7,
		LeadID:         42,
		CardScene:      types.CardSceneSupervisorPendingDispatch,
		ReceiverUserID: "manager1",
		TaskID:         "old-task",
		CardStatus:     types.CardStatusExpired,
		SentAt:         time.Now().Add(-time.Hour),
	}
	cardRepo.tasks[oldTask.ID] = oldTask
	handler := NewTaskHandler(nil, cardRepo, &fakeWeComClient{
		sendResp: &wecom.SendCardResponse{MsgID: "msg-new", ResponseCode: "resp-new"},
	}, nil)

	err := handler.resendCard(ctx, &types.Lead{
		ID:             oldTask.LeadID,
		CustomerName:   "测试客户",
		CustomerMobile: "13800000000",
	}, oldTask)
	if err != nil {
		t.Fatalf("resend card: %v", err)
	}

	if oldTask.CardStatus != types.CardStatusReplaced {
		t.Fatalf("old status = %s, want %s", oldTask.CardStatus, types.CardStatusReplaced)
	}
	if oldTask.NextRetryAt != nil {
		t.Fatalf("old next retry = %v, want nil", oldTask.NextRetryAt)
	}
	if len(cardRepo.created) != 1 {
		t.Fatalf("created tasks = %d, want 1", len(cardRepo.created))
	}
	newTask := cardRepo.created[0]
	if newTask.CardStatus != types.CardStatusSent {
		t.Fatalf("new status = %s, want %s", newTask.CardStatus, types.CardStatusSent)
	}
	if newTask.MsgID != "msg-new" || newTask.ResponseCode != "resp-new" {
		t.Fatalf("new task response mismatch: %+v", newTask)
	}
}

func TestResendCardFailureCreatesRetryableTask(t *testing.T) {
	ctx := context.Background()
	cardRepo := newFakeCardTaskRepo()
	scheduler := &fakeCardRetryScheduler{}
	oldTask := &types.WeComCardTask{
		ID:             8,
		LeadID:         43,
		CardScene:      types.CardSceneSupervisorPendingDispatch,
		ReceiverUserID: "manager1",
		TaskID:         "old-failed-task",
		CardStatus:     types.CardStatusExpired,
		SentAt:         time.Now().Add(-time.Hour),
	}
	cardRepo.tasks[oldTask.ID] = oldTask
	handler := NewTaskHandler(nil, cardRepo, &fakeWeComClient{
		sendErr: errors.New("send failed"),
	}, scheduler)

	err := handler.resendCard(ctx, &types.Lead{
		ID:           oldTask.LeadID,
		CustomerName: "失败客户",
	}, oldTask)
	if err != nil {
		t.Fatalf("resend card: %v", err)
	}

	if oldTask.CardStatus != types.CardStatusExpired {
		t.Fatalf("old status = %s, want %s", oldTask.CardStatus, types.CardStatusExpired)
	}
	if len(cardRepo.created) != 1 {
		t.Fatalf("created tasks = %d, want 1", len(cardRepo.created))
	}
	newTask := cardRepo.created[0]
	if newTask.CardStatus != types.CardStatusUpdateFailed {
		t.Fatalf("new status = %s, want %s", newTask.CardStatus, types.CardStatusUpdateFailed)
	}
	if newTask.RetryCount != 1 || newTask.NextRetryAt == nil {
		t.Fatalf("new task retry state mismatch: %+v", newTask)
	}
	if scheduler.calls != 1 {
		t.Fatalf("scheduler calls = %d, want 1", scheduler.calls)
	}
}

func TestResendCardFailurePreservesRetryBudgetAcrossGenerations(t *testing.T) {
	ctx := context.Background()
	cardRepo := newFakeCardTaskRepo()
	scheduler := &fakeCardRetryScheduler{}
	oldTask := &types.WeComCardTask{
		ID: 9, LeadID: 44, CardScene: types.CardSceneSupervisorPendingDispatch,
		ReceiverUserID: "manager1", TaskID: "old-retry-task",
		CardStatus: types.CardStatusExpired, RetryCount: types.MaxCardRetryCount - 1,
		SentAt: time.Now().Add(-time.Hour),
	}
	cardRepo.tasks[oldTask.ID] = oldTask
	handler := NewTaskHandler(nil, cardRepo, &fakeWeComClient{sendErr: errors.New("send failed")}, scheduler)

	if err := handler.resendCard(ctx, &types.Lead{ID: oldTask.LeadID, CustomerName: "失败客户"}, oldTask); err != nil {
		t.Fatalf("resend card: %v", err)
	}

	if len(cardRepo.created) != 1 {
		t.Fatalf("created tasks = %d, want 1", len(cardRepo.created))
	}
	newTask := cardRepo.created[0]
	if newTask.RetryCount != types.MaxCardRetryCount || newTask.NextRetryAt == nil {
		t.Fatalf("new task retry state mismatch: %+v", newTask)
	}
	if scheduler.calls != 1 {
		t.Fatalf("scheduler calls = %d, want 1", scheduler.calls)
	}

	newTask.CardStatus = types.CardStatusExpired
	if err := handler.resendCard(ctx, &types.Lead{ID: oldTask.LeadID, CustomerName: "失败客户"}, newTask); err != nil {
		t.Fatalf("final resend card: %v", err)
	}
	finalTask := cardRepo.created[1]
	if finalTask.RetryCount != types.MaxCardRetryCount || finalTask.NextRetryAt != nil {
		t.Fatalf("final task should be terminal: %+v", finalTask)
	}
	if scheduler.calls != 1 {
		t.Fatalf("terminal failure scheduled another retry: calls=%d", scheduler.calls)
	}
}

func TestScanFailedCardsSchedulesRetry(t *testing.T) {
	ctx := context.Background()
	cardRepo := newFakeCardTaskRepo()
	cardRepo.failed = []*types.WeComCardTask{
		{ID: 31, LeadID: 101, CardStatus: types.CardStatusUpdateFailed},
		{ID: 32, LeadID: 102, CardStatus: types.CardStatusUpdateFailed},
	}
	scheduler := &fakeCardRetryScheduler{}
	handler := NewTaskHandler(nil, cardRepo, &fakeWeComClient{}, scheduler)

	if err := handler.HandleScanFailedCards(ctx, nil); err != nil {
		t.Fatalf("scan failed cards: %v", err)
	}

	if scheduler.calls != 2 {
		t.Fatalf("scheduler calls = %d, want 2", scheduler.calls)
	}
	if scheduler.ids[0] != 31 || scheduler.ids[1] != 32 {
		t.Fatalf("scheduled ids = %v", scheduler.ids)
	}
}

func TestHandleSendCardDuplicateTaskCallsWeComOnce(t *testing.T) {
	ctx := context.Background()
	leadRepo := newFakeWorkerLeadRepo()
	leadRepo.leads[42] = &types.Lead{
		ID: 42, LeadNo: "DUP-42", CustomerName: "Duplicate",
		Status: types.LeadStatusPendingDispatch, Version: 7,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	cardRepo := newFakeCardTaskRepo()
	client := &fakeWeComClient{sendResp: &wecom.SendCardResponse{MsgID: "msg-42", ResponseCode: "resp-42"}}
	handler := NewTaskHandler(leadRepo, cardRepo, client, nil)
	payload, err := json.Marshal(SendCardPayload{
		LeadID: 42, ReceiverUserID: "manager1",
		CardScene: string(types.CardSceneSupervisorPendingDispatch),
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	task := asynq.NewTask(TypeSendCard, payload)
	if err := handler.HandleSendCard(ctx, task); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if err := handler.HandleSendCard(ctx, task); err != nil {
		t.Fatalf("duplicate delivery: %v", err)
	}
	if client.sendCalls != 1 {
		t.Fatalf("wecom send calls = %d, want 1", client.sendCalls)
	}
	if len(cardRepo.created) != 1 {
		t.Fatalf("created card tasks = %d, want 1", len(cardRepo.created))
	}
}

func TestHandleUpdateCardUpdatesEverySupervisorRecipient(t *testing.T) {
	ctx := context.Background()
	leadRepo := newFakeWorkerLeadRepo()
	leadRepo.leads[55] = &types.Lead{ID: 55, Version: 1, Status: types.LeadStatusAssignedPendingClaim}
	cardRepo := newFakeCardTaskRepo()
	expires := time.Now().Add(time.Hour)
	for id, receiver := range map[int64]string{1: "admin-1", 2: "manager-1"} {
		cardRepo.tasks[id] = &types.WeComCardTask{
			ID: id, LeadID: 55, ReceiverUserID: receiver,
			CardScene: types.CardSceneSupervisorPendingDispatch, CardStatus: types.CardStatusSent,
			ResponseCode: receiver + "-response", ResponseCodeExpireAt: &expires, SentAt: time.Now(),
		}
	}
	client := &fakeWeComClient{}
	handler := NewTaskHandler(leadRepo, cardRepo, client, nil)
	payload, _ := json.Marshal(UpdateCardPayload{LeadID: 55, CardScene: string(types.CardSceneSupervisorWaitingClaim)})

	if err := handler.HandleUpdateCard(ctx, asynq.NewTask(TypeUpdateCard, payload)); err != nil {
		t.Fatal(err)
	}
	if client.updateCalls != 2 {
		t.Fatalf("update calls = %d, want 2", client.updateCalls)
	}
	for _, task := range cardRepo.tasks {
		if task.CardScene != types.CardSceneSupervisorWaitingClaim || task.CardStatus != types.CardStatusUpdated {
			t.Fatalf("task not updated: %+v", task)
		}
	}
}

func TestHandleRemindDispatchNotifiesAllActiveAdminsAndManagers(t *testing.T) {
	ctx := context.Background()
	leadRepo := newFakeWorkerLeadRepo()
	leadRepo.leads[56] = &types.Lead{
		ID: 56, LeadNo: "L-56", CustomerName: "测试客户", SourceChannel: "tencent",
		Status: types.LeadStatusPendingDispatch, DispatchManagerUserID: "legacy-default", CreatedAt: time.Now().Add(-4 * time.Minute),
	}
	bindings := &workerNotificationBindingLister{items: []*types.WeComUserBinding{
		{WeComUserID: "admin-1", Role: types.UserRoleAdmin, Status: types.UserStatusActive},
		{WeComUserID: "manager-1", Role: types.UserRoleManager, Status: types.UserStatusActive},
		{WeComUserID: "manager-off", Role: types.UserRoleManager, Status: types.UserStatusDisabled},
	}}
	client := &fakeWeComClient{}
	handler := NewTaskHandler(leadRepo, newFakeCardTaskRepo(), client, nil).WithNotificationRecipientRepository(bindings)
	payload, _ := json.Marshal(RemindDispatchPayload{LeadID: 56, CreatedMinutes: 3})

	if err := handler.HandleRemindDispatch(ctx, asynq.NewTask(TypeTimeoutDispatchReminder, payload)); err != nil {
		t.Fatal(err)
	}
	want := []string{"admin-1", "manager-1"}
	if fmt.Sprint(client.textReceivers) != fmt.Sprint(want) {
		t.Fatalf("text receivers = %v, want %v", client.textReceivers, want)
	}
}

func TestRecipientCardTaskIDSeparatesReceiversAndKeepsRetriesStable(t *testing.T) {
	first := buildRecipientCardTaskID(12, types.CardSceneAppointmentAssigned, "inviter-1", "v3")
	retry := buildRecipientCardTaskID(12, types.CardSceneAppointmentAssigned, "inviter-1", "v3")
	other := buildRecipientCardTaskID(12, types.CardSceneAppointmentAssigned, "closer-1", "v3")
	if first != retry {
		t.Fatalf("same event task id changed: %q != %q", first, retry)
	}
	if first == other {
		t.Fatalf("different receivers share task id: %q", first)
	}
}

func TestHandleSendCardNilResponseBecomesRetryableFailure(t *testing.T) {
	ctx := context.Background()
	leadRepo := newFakeWorkerLeadRepo()
	leadRepo.leads[43] = &types.Lead{
		ID: 43, LeadNo: "NIL-43", CustomerName: "Nil Response",
		Status: types.LeadStatusPendingDispatch, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	cardRepo := newFakeCardTaskRepo()
	scheduler := &fakeCardRetryScheduler{}
	handler := NewTaskHandler(leadRepo, cardRepo, &fakeWeComClient{}, scheduler)
	payload, _ := json.Marshal(SendCardPayload{
		LeadID: 43, ReceiverUserID: "manager1",
		CardScene: string(types.CardSceneSupervisorPendingDispatch),
	})
	if err := handler.HandleSendCard(ctx, asynq.NewTask(TypeSendCard, payload)); err != nil {
		t.Fatalf("handle send: %v", err)
	}
	if len(cardRepo.created) != 1 || cardRepo.created[0].CardStatus != types.CardStatusUpdateFailed {
		t.Fatalf("card task = %+v, want retryable failure", cardRepo.created)
	}
	if cardRepo.created[0].LastError == "" || scheduler.calls != 1 {
		t.Fatalf("last error=%q scheduler calls=%d", cardRepo.created[0].LastError, scheduler.calls)
	}
}

func TestHandleCardRetryResendsInitialSendFailureWithoutResponseCode(t *testing.T) {
	ctx := context.Background()
	leadRepo := newFakeWorkerLeadRepo()
	leadRepo.leads[46] = &types.Lead{
		ID: 46, LeadNo: "RETRY-SEND-46", CustomerName: "Retry Initial Send",
		Status: types.LeadStatusPendingDispatch, Version: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	cardRepo := newFakeCardTaskRepo()
	scheduler := &fakeCardRetryScheduler{}
	client := &fakeWeComClient{}
	handler := NewTaskHandler(leadRepo, cardRepo, client, scheduler)
	payload, _ := json.Marshal(SendCardPayload{
		LeadID: 46, ReceiverUserID: "manager1",
		CardScene: string(types.CardSceneSupervisorPendingDispatch),
	})

	// 空响应模拟首次 SendCard 失败。占位记录会有未来的过期时间，
	// 但不会有 response_code，这正是本回归测试要覆盖的状态。
	if err := handler.HandleSendCard(ctx, asynq.NewTask(TypeSendCard, payload)); err != nil {
		t.Fatalf("handle initial send: %v", err)
	}
	if len(cardRepo.created) != 1 {
		t.Fatalf("created card tasks after initial send = %d, want 1", len(cardRepo.created))
	}
	failedTask := cardRepo.created[0]
	if failedTask.CardStatus != types.CardStatusUpdateFailed || failedTask.RetryCount != 1 {
		t.Fatalf("initial failed task = %+v, want update_failed retry_count=1", failedTask)
	}
	if failedTask.ResponseCode != "" || failedTask.ResponseCodeExpireAt == nil || !failedTask.ResponseCodeExpireAt.After(time.Now()) {
		t.Fatalf("initial failed task does not reproduce missing-code/future-expiry state: %+v", failedTask)
	}

	client.sendResp = &wecom.SendCardResponse{MsgID: "replacement-msg", ResponseCode: "replacement-code"}
	retryPayload, _ := json.Marshal(CardRetryPayload{CardTaskID: failedTask.ID})
	if err := handler.HandleCardRetry(ctx, asynq.NewTask(TypeCardRetry, retryPayload)); err != nil {
		t.Fatalf("handle card retry: %v", err)
	}

	if client.sendCalls != 2 {
		t.Fatalf("SendCard calls = %d, want initial failure plus one resend", client.sendCalls)
	}
	if client.updateCalls != 0 {
		t.Fatalf("UpdateCard calls = %d, want 0 for missing response_code", client.updateCalls)
	}
	if failedTask.CardStatus != types.CardStatusReplaced || failedTask.NextRetryAt != nil {
		t.Fatalf("source task after retry = %+v, want replaced without next retry", failedTask)
	}
	if len(cardRepo.created) != 2 {
		t.Fatalf("created card tasks after retry = %d, want 2", len(cardRepo.created))
	}
	replacement := cardRepo.created[1]
	if replacement.CardStatus != types.CardStatusSent || replacement.MsgID != "replacement-msg" || replacement.ResponseCode != "replacement-code" {
		t.Fatalf("replacement task = %+v, want successful resend", replacement)
	}
}

func TestHandleUpdateCardDuplicateTaskCallsWeComOnce(t *testing.T) {
	ctx := context.Background()
	leadRepo := newFakeWorkerLeadRepo()
	leadRepo.leads[44] = &types.Lead{
		ID: 44, LeadNo: "UPDATE-44", CustomerName: "Update Once",
		Status: types.LeadStatusAssignedPendingClaim, Version: 2,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	cardRepo := newFakeCardTaskRepo()
	expires := time.Now().Add(time.Hour)
	cardRepo.tasks[10] = &types.WeComCardTask{
		ID: 10, LeadID: 44, CardScene: types.CardSceneSupervisorPendingDispatch,
		ReceiverUserID: "manager1", TaskID: "update-once",
		ResponseCode: "response-44", CardStatus: types.CardStatusSent,
		SentAt: time.Now(), ResponseCodeExpireAt: &expires,
	}
	client := &fakeWeComClient{}
	handler := NewTaskHandler(leadRepo, cardRepo, client, nil)
	payload, err := json.Marshal(UpdateCardPayload{
		LeadID: 44, CardScene: string(types.CardSceneSupervisorWaitingClaim), Attempt: 1,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	task := asynq.NewTask(TypeUpdateCard, payload)
	if err := handler.HandleUpdateCard(ctx, task); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if err := handler.HandleUpdateCard(ctx, task); err != nil {
		t.Fatalf("duplicate delivery: %v", err)
	}
	if client.updateCalls != 1 {
		t.Fatalf("wecom update calls = %d, want 1", client.updateCalls)
	}
}

func TestHandleExpiredUpdateDuplicateTaskResendsOnce(t *testing.T) {
	ctx := context.Background()
	leadRepo := newFakeWorkerLeadRepo()
	leadRepo.leads[45] = &types.Lead{
		ID: 45, LeadNo: "EXPIRED-45", CustomerName: "Expired Once",
		Status: types.LeadStatusAssignedPendingClaim, Version: 2,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	cardRepo := newFakeCardTaskRepo()
	expired := time.Now().Add(-time.Minute)
	cardRepo.tasks[11] = &types.WeComCardTask{
		ID: 11, LeadID: 45, CardScene: types.CardSceneSupervisorPendingDispatch,
		ReceiverUserID: "manager1", TaskID: "expired-once",
		ResponseCode: "expired-response", CardStatus: types.CardStatusSent,
		SentAt: time.Now().Add(-time.Hour), ResponseCodeExpireAt: &expired,
	}
	client := &fakeWeComClient{sendResp: &wecom.SendCardResponse{MsgID: "replacement", ResponseCode: "replacement-code"}}
	handler := NewTaskHandler(leadRepo, cardRepo, client, nil)
	payload, err := json.Marshal(UpdateCardPayload{
		LeadID: 45, CardScene: string(types.CardSceneSupervisorWaitingClaim), Attempt: 1,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	task := asynq.NewTask(TypeUpdateCard, payload)
	if err := handler.HandleUpdateCard(ctx, task); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if err := handler.HandleUpdateCard(ctx, task); err != nil {
		t.Fatalf("duplicate delivery: %v", err)
	}
	if client.sendCalls != 1 {
		t.Fatalf("wecom replacement sends = %d, want 1", client.sendCalls)
	}
	if cardRepo.tasks[11].CardStatus != types.CardStatusReplaced {
		t.Fatalf("source status = %s, want replaced", cardRepo.tasks[11].CardStatus)
	}
}

func TestStressHandleSendCardWeComFailureCreatesRetryableTasks(t *testing.T) {
	requireWorkerStress(t)

	ctx := context.Background()
	leadRepo := newFakeWorkerLeadRepo()
	cardRepo := newFakeCardTaskRepo()
	scheduler := &fakeCardRetryScheduler{}
	handler := NewTaskHandler(leadRepo, cardRepo, &fakeWeComClient{
		sendErr: errors.New("wecom unavailable"),
	}, scheduler)

	const total = 200
	for i := 1; i <= total; i++ {
		leadRepo.leads[int64(i)] = &types.Lead{
			ID:             int64(i),
			LeadNo:         fmt.Sprintf("WQ-%04d", i),
			CustomerName:   fmt.Sprintf("客户%04d", i),
			CustomerMobile: fmt.Sprintf("139%08d", i),
			Status:         types.LeadStatusPendingDispatch,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
	}

	start := time.Now()
	for i := 1; i <= total; i++ {
		payload, err := json.Marshal(SendCardPayload{
			LeadID:         int64(i),
			ReceiverUserID: "manager1",
			CardScene:      string(types.CardSceneSupervisorPendingDispatch),
		})
		if err != nil {
			t.Fatalf("marshal payload %d: %v", i, err)
		}
		if err := handler.HandleSendCard(ctx, asynq.NewTask(TypeSendCard, payload)); err != nil {
			t.Fatalf("handle send card %d: %v", i, err)
		}
	}

	if len(cardRepo.created) != total {
		t.Fatalf("created card tasks = %d, want %d", len(cardRepo.created), total)
	}
	if scheduler.calls != total {
		t.Fatalf("scheduled retries = %d, want %d", scheduler.calls, total)
	}
	for _, task := range cardRepo.created {
		if task.CardStatus != types.CardStatusUpdateFailed ||
			task.RetryCount != 1 ||
			task.NextRetryAt == nil ||
			task.LastError == "" {
			t.Fatalf("unexpected failed task state: %+v", task)
		}
	}
	t.Logf("handled %d failed send-card tasks in %s", total, time.Since(start))
}

func requireWorkerStress(t *testing.T) {
	t.Helper()
	if os.Getenv("KXONE_STRESS") != "1" {
		t.Skip("set KXONE_STRESS=1 to run worker stress tests")
	}
}

type fakeCardTaskRepo struct {
	nextID  int64
	tasks   map[int64]*types.WeComCardTask
	created []*types.WeComCardTask
	failed  []*types.WeComCardTask
}

func newFakeCardTaskRepo() *fakeCardTaskRepo {
	return &fakeCardTaskRepo{
		nextID: 100,
		tasks:  map[int64]*types.WeComCardTask{},
	}
}

func (r *fakeCardTaskRepo) Create(ctx context.Context, task *types.WeComCardTask) error {
	r.nextID++
	task.ID = r.nextID
	r.tasks[task.ID] = task
	r.created = append(r.created, task)
	return nil
}

func (r *fakeCardTaskRepo) GetByTaskID(ctx context.Context, taskID string) (*types.WeComCardTask, error) {
	for _, task := range r.tasks {
		if task.TaskID == taskID {
			return task, nil
		}
	}
	return nil, nil
}

func (r *fakeCardTaskRepo) TryStartRetry(ctx context.Context, taskID int64, expectedRetryCount int) (bool, error) {
	task := r.tasks[taskID]
	if task == nil || task.CardStatus != types.CardStatusUpdateFailed || task.RetryCount != expectedRetryCount {
		return false, nil
	}
	task.CardStatus = types.CardStatusUpdating
	return true, nil
}

func (r *fakeCardTaskRepo) TryStartUpdate(ctx context.Context, taskID int64, expectedStatus types.CardStatus) (bool, error) {
	task := r.tasks[taskID]
	if task == nil || task.CardStatus != expectedStatus {
		return false, nil
	}
	task.CardStatus = types.CardStatusUpdating
	return true, nil
}

func (r *fakeCardTaskRepo) GetByID(ctx context.Context, id int64) (*types.WeComCardTask, error) {
	return r.tasks[id], nil
}

func (r *fakeCardTaskRepo) GetLatestByScene(ctx context.Context, leadID int64, scene types.CardScene) (*types.WeComCardTask, error) {
	var latest *types.WeComCardTask
	for _, task := range r.tasks {
		if task.LeadID == leadID && task.CardScene == scene && (latest == nil || task.SentAt.After(latest.SentAt)) {
			latest = task
		}
	}
	return latest, nil
}

func (r *fakeCardTaskRepo) ListLatestBySceneRecipients(ctx context.Context, leadID int64, scene types.CardScene) ([]*types.WeComCardTask, error) {
	latest := make(map[string]*types.WeComCardTask)
	for _, task := range r.tasks {
		if task.LeadID != leadID || task.CardScene != scene {
			continue
		}
		current := latest[task.ReceiverUserID]
		if current == nil || task.SentAt.After(current.SentAt) {
			latest[task.ReceiverUserID] = task
		}
	}
	result := make([]*types.WeComCardTask, 0, len(latest))
	for _, task := range latest {
		result = append(result, task)
	}
	return result, nil
}

func (r *fakeCardTaskRepo) Update(ctx context.Context, task *types.WeComCardTask) error {
	r.tasks[task.ID] = task
	return nil
}

func (r *fakeCardTaskRepo) ListFailedTasks(ctx context.Context, limit int) ([]*types.WeComCardTask, error) {
	return r.failed, nil
}

type fakeCardRetryScheduler struct {
	calls int
	ids   []int64
}

func (s *fakeCardRetryScheduler) ScheduleCardRetry(ctx context.Context, cardTaskID int64, delay time.Duration) error {
	s.calls++
	s.ids = append(s.ids, cardTaskID)
	return nil
}

type fakeWeComClient struct {
	sendResp      *wecom.SendCardResponse
	sendErr       error
	sendCalls     int
	updateCalls   int
	textCalls     int
	textErr       error
	textReceivers []string
}

func (c *fakeWeComClient) SendCard(ctx context.Context, req *wecom.SendCardRequest) (*wecom.SendCardResponse, error) {
	c.sendCalls++
	return c.sendResp, c.sendErr
}

func (c *fakeWeComClient) UpdateCard(ctx context.Context, req *wecom.UpdateCardRequest) (*wecom.UpdateCardResponse, error) {
	c.updateCalls++
	return &wecom.UpdateCardResponse{Success: true}, nil
}

func (c *fakeWeComClient) SendText(ctx context.Context, req *wecom.SendTextRequest) error {
	c.textCalls++
	c.textReceivers = append(c.textReceivers, req.ToUser)
	return c.textErr
}

type workerNotificationBindingLister struct {
	items []*types.WeComUserBinding
}

func (r *workerNotificationBindingLister) List(_ context.Context, status, role string, offset, limit int) ([]*types.WeComUserBinding, int, error) {
	var filtered []*types.WeComUserBinding
	for _, item := range r.items {
		if string(item.Status) == status && string(item.Role) == role {
			filtered = append(filtered, item)
		}
	}
	if offset >= len(filtered) {
		return nil, len(filtered), nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end], len(filtered), nil
}

func (c *fakeWeComClient) GetAccessToken(ctx context.Context) (string, error) {
	return "", nil
}

func (c *fakeWeComClient) VerifyCallbackURL(msgSignature, timestamp, nonce, echoStr string) (string, error) {
	return "", nil
}

func (c *fakeWeComClient) DecryptCallback(msgSignature, timestamp, nonce string, encryptedData []byte) ([]byte, error) {
	return nil, nil
}

func (c *fakeWeComClient) GetUserIDByCode(ctx context.Context, code string) (string, error) {
	return "", nil
}

func (c *fakeWeComClient) GetJSAPIConfig(ctx context.Context, url string) (*wecom.JSAPIConfig, error) {
	return nil, nil
}

type fakeWorkerLeadRepo struct {
	leads map[int64]*types.Lead
}

func newFakeWorkerLeadRepo() *fakeWorkerLeadRepo {
	return &fakeWorkerLeadRepo{leads: map[int64]*types.Lead{}}
}

func (r *fakeWorkerLeadRepo) Create(ctx context.Context, lead *types.Lead) error {
	r.leads[lead.ID] = lead
	return nil
}

func (r *fakeWorkerLeadRepo) GetByID(ctx context.Context, id int64) (*types.Lead, error) {
	lead := r.leads[id]
	if lead == nil {
		return nil, nil
	}
	copy := *lead
	return &copy, nil
}

func (r *fakeWorkerLeadRepo) GetByLeadNo(ctx context.Context, leadNo string) (*types.Lead, error) {
	for _, lead := range r.leads {
		if lead.LeadNo == leadNo {
			copy := *lead
			return &copy, nil
		}
	}
	return nil, nil
}

func (r *fakeWorkerLeadRepo) Update(ctx context.Context, lead *types.Lead) error {
	r.leads[lead.ID] = lead
	return nil
}

func (r *fakeWorkerLeadRepo) AtomicClaim(ctx context.Context, leadID int64, salesUserID string, expectedVersion, expectedProfileVersion int) (bool, error) {
	return false, nil
}

func (r *fakeWorkerLeadRepo) AtomicAssign(ctx context.Context, leadID int64, managerUserID, salesUserID string, expectedVersion, expectedProfileVersion int) (bool, error) {
	return false, nil
}

func (r *fakeWorkerLeadRepo) AtomicReassign(ctx context.Context, leadID int64, managerUserID, newSalesUserID string, expectedVersion, expectedProfileVersion int) (bool, error) {
	return false, nil
}

func (r *fakeWorkerLeadRepo) ListPendingDispatch(ctx context.Context, limit int) ([]*types.Lead, error) {
	return nil, nil
}

func (r *fakeWorkerLeadRepo) ListPendingClaim(ctx context.Context, limit int) ([]*types.Lead, error) {
	return nil, nil
}

func (r *fakeWorkerLeadRepo) ListAll(ctx context.Context, status string, offset, limit int) ([]*types.Lead, int, error) {
	return nil, 0, nil
}

func (r *fakeWorkerLeadRepo) ListAllFiltered(ctx context.Context, filter repo.LeadListFilter, offset, limit int) ([]*types.Lead, int, error) {
	return nil, 0, nil
}

func (r *fakeWorkerLeadRepo) ListByManager(ctx context.Context, managerUserID, status string, offset, limit int) ([]*types.Lead, int, error) {
	return nil, 0, nil
}

func (r *fakeWorkerLeadRepo) ListByManagerFiltered(ctx context.Context, managerUserID string, filter repo.LeadListFilter, offset, limit int) ([]*types.Lead, int, error) {
	return nil, 0, nil
}

func (r *fakeWorkerLeadRepo) ListBySales(ctx context.Context, salesUserID, status string, offset, limit int) ([]*types.Lead, int, error) {
	return nil, 0, nil
}

func (r *fakeWorkerLeadRepo) ListBySalesFiltered(ctx context.Context, salesUserID string, filter repo.LeadListFilter, offset, limit int) ([]*types.Lead, int, error) {
	return nil, 0, nil
}
