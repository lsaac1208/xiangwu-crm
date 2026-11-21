package worker

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/hibiken/asynq"
)

// Client 任务客户端（用于提交任务）
type Client struct {
	client *asynq.Client
}

// WakeGroupRobotScan enqueues one immediate reconciliation pass. A stable task
// ID coalesces bursts of new leads while the durable database ledger and cron
// guarantee that no notification depends solely on this wake-up.
func (c *Client) WakeGroupRobotScan(ctx context.Context) error {
	_, err := c.client.EnqueueContext(
		ctx,
		asynq.NewTask(TypeScanGroupRobotRules, nil),
		asynq.Queue("default"),
		asynq.TaskID("group-robot-scan-wakeup"),
		asynq.MaxRetry(0),
	)
	if errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return err
}

// NewClient 创建任务客户端
func NewClient(redisAddr, password string, db int) *Client {
	redisOpt := asynq.RedisClientOpt{
		Addr:     redisAddr,
		Password: password,
		DB:       db,
	}
	client := asynq.NewClient(redisOpt)

	return &Client{client: client}
}

// EnqueueUpdateCard 提交更新卡片任务（立即执行）
func (c *Client) EnqueueUpdateCard(ctx context.Context, leadID int64, cardScene string) error {
	payload, err := json.Marshal(UpdateCardPayload{
		LeadID:    leadID,
		CardScene: cardScene,
		Attempt:   1,
	})
	if err != nil {
		return err
	}

	task := asynq.NewTask(TypeUpdateCard, payload)
	_, err = c.client.EnqueueContext(ctx, task, asynq.Queue("cards"))

	return err
}

// EnqueueSendCard 提交发送卡片任务（立即执行）
func (c *Client) EnqueueSendCard(ctx context.Context, leadID int64, receiverUserID, cardScene string) error {
	return c.EnqueueSendCardEvent(ctx, leadID, receiverUserID, cardScene, "")
}

func (c *Client) EnqueueSendCardEvent(ctx context.Context, leadID int64, receiverUserID, cardScene, eventKey string) error {
	payload, err := json.Marshal(SendCardPayload{
		LeadID:         leadID,
		ReceiverUserID: receiverUserID,
		CardScene:      cardScene,
		EventKey:       eventKey,
	})
	if err != nil {
		return err
	}

	task := asynq.NewTask(TypeSendCard, payload)
	_, err = c.client.EnqueueContext(ctx, task, asynq.Queue("cards"))

	return err
}

// SchedulePendingDispatchReminder 调度待分配超时提醒
func (c *Client) SchedulePendingDispatchReminder(ctx context.Context, leadID int64, delay time.Duration) error {
	payload, err := json.Marshal(TimeoutDispatchReminderPayload{
		LeadID:         leadID,
		CreatedMinutes: durationMinutes(delay),
	})
	if err != nil {
		return err
	}

	task := asynq.NewTask(TypeTimeoutDispatchReminder, payload)

	_, err = c.client.EnqueueContext(ctx, task,
		asynq.ProcessIn(delay),
		asynq.Queue("default"),
	)

	return err
}

// SchedulePendingClaimReminder 调度待领取超时提醒
func (c *Client) SchedulePendingClaimReminder(ctx context.Context, leadID int64, delay time.Duration) error {
	payload, err := json.Marshal(TimeoutClaimReminderPayload{
		LeadID:          leadID,
		AssignedMinutes: durationMinutes(delay),
	})
	if err != nil {
		return err
	}

	task := asynq.NewTask(TypeTimeoutClaimReminder, payload)

	_, err = c.client.EnqueueContext(ctx, task,
		asynq.ProcessIn(delay),
		asynq.Queue("default"),
	)

	return err
}

// ScheduleCardRetry 调度卡片重试任务（实现 TaskScheduler 接口）
func (c *Client) ScheduleCardRetry(ctx context.Context, cardTaskID int64, delay time.Duration) error {
	payload, err := json.Marshal(CardRetryPayload{
		CardTaskID: cardTaskID,
	})
	if err != nil {
		return err
	}

	task := asynq.NewTask(TypeCardRetry, payload)

	_, err = c.client.EnqueueContext(ctx, task,
		asynq.ProcessIn(delay),
		asynq.Queue("default"),
	)

	return err
}

func durationMinutes(delay time.Duration) int {
	if delay <= 0 {
		return 0
	}
	minutes := int(delay / time.Minute)
	if delay%time.Minute != 0 {
		minutes++
	}
	if minutes < 1 {
		return 1
	}
	return minutes
}

// Close 关闭客户端
func (c *Client) Close() error {
	return c.client.Close()
}
