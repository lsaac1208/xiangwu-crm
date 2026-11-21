package worker

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
	"github.com/lsaac128/kxone/internal/domain/card"
	"github.com/lsaac128/kxone/internal/notification"
	"github.com/lsaac128/kxone/internal/runtime"
	"github.com/lsaac128/kxone/pkg/metrics"
	"github.com/lsaac128/kxone/pkg/types"
)

// CardRetryScheduler 卡片重试调度接口
type CardRetryScheduler interface {
	ScheduleCardRetry(ctx context.Context, cardTaskID int64, delay time.Duration) error
}

// TaskHandler 任务处理器
type TaskHandler struct {
	leadRepo        repo.LeadRepository
	cardRepo        repo.CardTaskRepository
	wecomClient     wecom.Client
	scheduler       CardRetryScheduler
	cardLifecycle   *card.Lifecycle
	metrics         *metrics.Metrics
	runtimeSettings runtime.RuntimeSettings
	recipientRepo   notification.BindingLister
	stage3Settings  interface {
		runtime.WorkflowFeatureFlags
		runtime.FollowupPolicySettings
	}
	reminderRepo     repo.LeadReminderRepository
	appointmentRepo  repo.AppointmentRepository
	poolAutoReturner LeadPoolAutoReturner
}

func (h *TaskHandler) WithStage6Appointments(appointments repo.AppointmentRepository) *TaskHandler {
	h.appointmentRepo = appointments
	return h
}

func (h *TaskHandler) WithNotificationRecipientRepository(repository notification.BindingLister) *TaskHandler {
	h.recipientRepo = repository
	return h
}

type LeadPoolAutoReturner interface {
	AutoReturnOverdue(ctx context.Context, now time.Time, limit int) error
}

func (h *TaskHandler) WithStage3Workflow(settings interface {
	runtime.WorkflowFeatureFlags
	runtime.FollowupPolicySettings
}, reminderRepo repo.LeadReminderRepository, poolAutoReturner LeadPoolAutoReturner) *TaskHandler {
	h.stage3Settings = settings
	h.reminderRepo = reminderRepo
	h.poolAutoReturner = poolAutoReturner
	return h
}

func NewTaskHandler(
	leadRepo repo.LeadRepository,
	cardRepo repo.CardTaskRepository,
	wecomClient wecom.Client,
	scheduler CardRetryScheduler,
) *TaskHandler {
	return &TaskHandler{
		leadRepo:        leadRepo,
		cardRepo:        cardRepo,
		wecomClient:     wecomClient,
		scheduler:       scheduler,
		cardLifecycle:   card.NewLifecycle(),
		metrics:         metrics.GetGlobalMetrics(),
		runtimeSettings: runtime.NewStaticRuntimeSettings(runtime.DefaultWeComConfig()),
	}
}

func (h *TaskHandler) WithRuntimeSettings(settings runtime.RuntimeSettings) *TaskHandler {
	if settings != nil {
		h.runtimeSettings = settings
	}
	return h
}

func (h *TaskHandler) WithPublicBaseURL(baseURL string) *TaskHandler {
	h.cardLifecycle.WithBaseURL(baseURL)
	return h
}

func (h *TaskHandler) HandleSendCard(ctx context.Context, t *asynq.Task) error {
	start := time.Now()
	var payload SendCardPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("解析任务载荷失败: %w", err)
	}

	slog.Info("processing send-card task",
		"lead_id", payload.LeadID,
		"receiver_userid", payload.ReceiverUserID,
		"card_scene", payload.CardScene)

	// 查询线索
	lead, err := h.leadRepo.GetByID(ctx, payload.LeadID)
	if err != nil {
		return fmt.Errorf("查询线索失败: %w", err)
	}
	if lead == nil {
		return fmt.Errorf("线索不存在: %d", payload.LeadID)
	}

	// 构造卡片内容
	cardScene := types.CardScene(payload.CardScene)
	content := h.cardLifecycle.BuildCardContent(cardScene, lead)

	// 构造 task_id
	eventKey := strings.TrimSpace(payload.EventKey)
	if eventKey == "" {
		eventKey = fmt.Sprintf("v%d", lead.Version)
	}
	taskID := buildRecipientCardTaskID(payload.LeadID, cardScene, payload.ReceiverUserID, eventKey)

	// 先用确定性 task_id 占位。Asynq 至少一次投递时，重复任务会在
	// 外部调用之前被数据库唯一键拦截，避免重复给同一用户发送卡片。
	now := time.Now()
	expireAt := h.cardLifecycle.CalculateResponseCodeExpireTime(now)
	cardTask := &types.WeComCardTask{
		LeadID:               payload.LeadID,
		CardScene:            cardScene,
		ReceiverUserID:       payload.ReceiverUserID,
		TaskID:               taskID,
		CardStatus:           types.CardStatusUpdating,
		SentAt:               now,
		ResponseCodeExpireAt: &expireAt,
	}
	created, err := h.createSendPlaceholder(ctx, cardTask)
	if err != nil {
		return fmt.Errorf("创建卡片发送占位失败: %w", err)
	}
	if !created {
		slog.Info("duplicate send-card task skipped", "task_id", taskID, "lead_id", payload.LeadID)
		return nil
	}

	// 发送卡片
	resp, err := h.wecomClient.SendCard(ctx, &wecom.SendCardRequest{
		ToUser:  payload.ReceiverUserID,
		AgentID: h.runtimeSettings.WeComAgentID(ctx),
		TaskID:  taskID,
		Content: content,
	})
	err = cardSendError(resp, err)

	// 记录监控指标
	elapsed := time.Since(start)
	h.metrics.RecordCardSend(err == nil, elapsed)

	if err != nil {
		cardTask.CardStatus = types.CardStatusUpdateFailed
		cardTask.LastError = err.Error()
		slog.Error("send card failed",
			"lead_id", payload.LeadID,
			"receiver_userid", payload.ReceiverUserID,
			"card_scene", cardScene,
			"err", err)

		// 保存失败状态
		if h.updateCardTask(ctx, cardTask, "保存发送失败卡片任务") {
			h.scheduleRetry(ctx, cardTask, "发送卡片")
		}

		return nil // 不返回 error，避免 Asynq 自动重试
	}

	cardTask.MsgID = resp.MsgID
	cardTask.ResponseCode = resp.ResponseCode
	cardTask.CardStatus = types.CardStatusSent
	slog.Info("send card succeeded",
		"lead_id", payload.LeadID,
		"receiver_userid", payload.ReceiverUserID,
		"card_scene", cardScene,
		"msg_id", resp.MsgID)

	h.updateCardTask(ctx, cardTask, "保存发送成功卡片任务")
	return nil
}

// HandleUpdateCard 处理更新卡片任务
func (h *TaskHandler) HandleUpdateCard(ctx context.Context, t *asynq.Task) error {
	var payload UpdateCardPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("解析任务载荷失败: %w", err)
	}

	slog.Info("processing update-card task",
		"lead_id", payload.LeadID,
		"card_scene", payload.CardScene,
		"attempt", payload.Attempt)

	// 查询线索最新状态
	lead, err := h.leadRepo.GetByID(ctx, payload.LeadID)
	if err != nil {
		return fmt.Errorf("查询线索失败: %w", err)
	}
	if lead == nil {
		return fmt.Errorf("线索不存在: %d", payload.LeadID)
	}

	// 根据场景查询对应卡片任务
	var sourceScene types.CardScene
	switch payload.CardScene {
	case string(types.CardSceneSupervisorClaimed):
		sourceScene = types.CardSceneSupervisorWaitingClaim
	case string(types.CardSceneSupervisorWaitingClaim):
		sourceScene = types.CardSceneSupervisorPendingDispatch
	case string(types.CardSceneSalesClaimed):
		sourceScene = types.CardSceneSalesPendingClaim
	default:
		return fmt.Errorf("未知卡片场景: %s", payload.CardScene)
	}

	var cardTasks []*types.WeComCardTask
	if sceneRepo, ok := h.cardRepo.(repo.CardTaskSceneRepository); ok {
		cardTasks, err = sceneRepo.ListLatestBySceneRecipients(ctx, payload.LeadID, sourceScene)
	} else {
		var cardTask *types.WeComCardTask
		cardTask, err = h.cardRepo.GetLatestByScene(ctx, payload.LeadID, sourceScene)
		if cardTask != nil {
			cardTasks = []*types.WeComCardTask{cardTask}
		}
	}
	if err != nil || len(cardTasks) == 0 {
		slog.Warn("card task not found for update",
			"lead_id", payload.LeadID,
			"source_scene", sourceScene,
			"err", err)
		return nil // 卡片不存在不算错误
	}
	for _, cardTask := range cardTasks {
		if err := h.updateRecipientCard(ctx, lead, cardTask, sourceScene, types.CardScene(payload.CardScene)); err != nil {
			return err
		}
	}
	return nil
}

func (h *TaskHandler) updateRecipientCard(ctx context.Context, lead *types.Lead, cardTask *types.WeComCardTask, sourceScene, targetScene types.CardScene) error {
	start := time.Now()
	previousStatus := cardTask.CardStatus
	if previousStatus != types.CardStatusSent && previousStatus != types.CardStatusUpdated {
		slog.Info("card update skipped because task status is not updateable",
			"lead_id", lead.ID,
			"card_task_id", cardTask.ID,
			"card_status", previousStatus)
		return nil
	}
	claimed, err := h.cardRepo.TryStartUpdate(ctx, cardTask.ID, previousStatus)
	if err != nil {
		return fmt.Errorf("抢占卡片更新任务失败: %w", err)
	}
	if !claimed {
		slog.Info("duplicate card update skipped", "card_task_id", cardTask.ID, "lead_id", lead.ID)
		return nil
	}
	cardTask.CardStatus = types.CardStatusUpdating
	now := time.Now()
	cardTask.UpdatedAt = &now

	// Only an actually expired response_code should trigger a replacement send.
	// The source task is claimed first and the replacement is persisted before
	// the external call, so duplicate deliveries and process crashes are visible.
	if h.cardLifecycle.IsResponseCodeExpired(cardTask, now) {
		slog.Info("response code expired; resending card",
			"lead_id", lead.ID,
			"card_task_id", cardTask.ID,
			"source_scene", sourceScene)

		content := h.cardLifecycle.BuildCardContent(targetScene, lead)
		taskID := buildRecipientCardTaskID(lead.ID, targetScene, cardTask.ReceiverUserID, resendSuffix())
		expireAt := h.cardLifecycle.CalculateResponseCodeExpireTime(now)
		newCardTask := &types.WeComCardTask{
			LeadID:               lead.ID,
			CardScene:            targetScene,
			ReceiverUserID:       cardTask.ReceiverUserID,
			TaskID:               taskID,
			CardStatus:           types.CardStatusUpdating,
			SentAt:               now,
			ResponseCodeExpireAt: &expireAt,
		}
		if !h.createCardTask(ctx, newCardTask, "创建补发卡片占位") {
			cardTask.CardStatus = previousStatus
			cardTask.UpdatedAt = nil
			h.updateCardTask(ctx, cardTask, "恢复补发失败的源卡片状态")
			return fmt.Errorf("创建补发卡片占位失败: lead_id=%d", lead.ID)
		}

		cardTask.CardStatus = types.CardStatusExpired
		h.updateCardTask(ctx, cardTask, "标记旧卡片过期")

		resp, err := h.wecomClient.SendCard(ctx, &wecom.SendCardRequest{
			ToUser:  cardTask.ReceiverUserID,
			AgentID: h.runtimeSettings.WeComAgentID(ctx),
			TaskID:  taskID,
			Content: content,
		})
		err = cardSendError(resp, err)

		if err != nil {
			newCardTask.CardStatus = types.CardStatusUpdateFailed
			newCardTask.LastError = err.Error()
			slog.Error("resend card failed",
				"lead_id", lead.ID,
				"receiver_userid", cardTask.ReceiverUserID,
				"card_scene", targetScene,
				"err", err)
		} else {
			newCardTask.MsgID = resp.MsgID
			newCardTask.ResponseCode = resp.ResponseCode
			newCardTask.CardStatus = types.CardStatusSent
			slog.Info("resend card succeeded",
				"lead_id", lead.ID,
				"receiver_userid", cardTask.ReceiverUserID,
				"card_scene", targetScene,
				"msg_id", resp.MsgID)
		}

		if !h.updateCardTask(ctx, newCardTask, "保存补发卡片结果") {
			return nil
		}
		if err != nil {
			h.scheduleRetry(ctx, newCardTask, "补发卡片")
			return nil
		}
		cardTask.CardStatus = types.CardStatusReplaced
		cardTask.NextRetryAt = nil
		cardTask.LastError = ""
		cardTask.UpdatedAt = &now
		h.updateCardTask(ctx, cardTask, "标记旧卡片已被替代")
		return nil
	}

	// 构造更新内容
	content := h.cardLifecycle.BuildCardContent(targetScene, lead)

	// 调用企业微信更新 API
	resp, err := h.wecomClient.UpdateCard(ctx, &wecom.UpdateCardRequest{
		AgentID:      h.runtimeSettings.WeComAgentID(ctx),
		ResponseCode: cardTask.ResponseCode,
		Content:      content,
	})

	// 记录监控指标
	elapsed := time.Since(start)
	updateSuccess := err == nil && resp != nil && resp.Success
	h.metrics.RecordCardUpdate(updateSuccess, elapsed)

	if !updateSuccess {
		// 更新失败，记录状态
		cardTask.CardScene = targetScene
		cardTask.CardStatus = types.CardStatusUpdateFailed
		if err != nil {
			cardTask.LastError = err.Error()
		} else if resp == nil {
			cardTask.LastError = "企业微信卡片更新返回空响应"
		} else if resp.Error != nil {
			cardTask.LastError = resp.Error.Error()
		} else {
			cardTask.LastError = "企业微信卡片更新失败"
		}

		if isInvalidResponseCode(resp) {
			slog.Warn("invalid response code; resending card",
				"lead_id", lead.ID,
				"card_task_id", cardTask.ID,
				"card_scene", targetScene)
			cardTask.CardStatus = types.CardStatusExpired
			h.updateCardTask(ctx, cardTask, "标记无效 response_code 卡片过期")
			return h.resendCard(ctx, lead, cardTask)
		}

		h.updateCardTask(ctx, cardTask, "保存卡片更新失败状态")
		h.scheduleRetry(ctx, cardTask, "更新卡片")

		return nil // 不返回 error，避免 Asynq 自动重试
	}

	// 更新成功
	cardTask.CardScene = targetScene
	cardTask.CardStatus = types.CardStatusUpdated
	now = time.Now()
	cardTask.UpdatedAt = &now
	h.updateCardTask(ctx, cardTask, "保存卡片更新成功状态")

	slog.Info("card update succeeded",
		"lead_id", lead.ID,
		"card_task_id", cardTask.ID,
		"card_scene", targetScene)
	return nil
}

// HandleRemindDispatch 处理提醒主管分配任务
func (h *TaskHandler) HandleRemindDispatch(ctx context.Context, t *asynq.Task) error {
	var payload RemindDispatchPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("解析任务载荷失败: %w", err)
	}

	slog.Info("processing dispatch reminder",
		"lead_id", payload.LeadID,
		"manager_userid", payload.ManagerUserID,
		"configured_minutes", payload.CreatedMinutes)

	// 查询线索当前状态
	lead, err := h.leadRepo.GetByID(ctx, payload.LeadID)
	if err != nil {
		return fmt.Errorf("查询线索失败: %w", err)
	}
	if lead == nil {
		return nil // 线索不存在，不算错误
	}

	// 检查是否仍处于待分配状态
	if lead.Status != types.LeadStatusPendingDispatch {
		slog.Info("dispatch reminder skipped because lead is no longer pending",
			"lead_id", payload.LeadID,
			"status", lead.Status)
		return nil
	}

	recipients, recipientErr := notification.ActiveAdminManagerUserIDs(ctx, h.recipientRepo)
	if recipientErr != nil {
		slog.Error("list dispatch reminder recipients failed", "lead_id", payload.LeadID, "err", recipientErr)
	}
	if len(recipients) == 0 {
		managerUserID := strings.TrimSpace(payload.ManagerUserID)
		if managerUserID == "" {
			managerUserID = strings.TrimSpace(lead.DispatchManagerUserID)
		}
		if managerUserID != "" {
			recipients = []string{managerUserID}
		}
	}
	if len(recipients) == 0 {
		return fmt.Errorf("线索未设置分配主管: lead_id=%d", payload.LeadID)
	}

	// 计算超时时长
	elapsed := time.Since(lead.CreatedAt)
	elapsedMinutes := int(elapsed.Minutes())
	reminderIndex, shouldSend := reminderOrdinal(payload.CreatedMinutes, elapsedMinutes, h.runtimeSettings.DispatchReminderDelays(ctx))
	if !shouldSend {
		slog.Info("dispatch reminder skipped because rule changed or disabled",
			"lead_id", payload.LeadID,
			"configured_minutes", payload.CreatedMinutes,
			"elapsed_minutes", elapsedMinutes)
		return nil
	}
	configuredMinutes := reminderMessageMinutes(payload.CreatedMinutes, elapsedMinutes)

	// 构造提醒消息
	reminderLevel := reminderLevelByIndex(reminderIndex)

	content := fmt.Sprintf("%s线索分配提醒\n\n"+
		"线索编号：%s\n"+
		"客户：%s\n"+
		"来源：%s\n"+
		"提醒规则：%d 分钟\n"+
		"等待时间：%d 分钟\n\n"+
		"请尽快分配销售跟进！",
		reminderLevel,
		lead.LeadNo,
		lead.CustomerName,
		lead.SourceChannel,
		configuredMinutes,
		elapsedMinutes)

	failed := false
	for _, receiverUserID := range recipients {
		err = h.wecomClient.SendText(ctx, &wecom.SendTextRequest{
			ToUser:  receiverUserID,
			AgentID: h.runtimeSettings.WeComAgentID(ctx),
			Content: content,
		})
		if err != nil {
			failed = true
			slog.Error("send dispatch reminder failed",
				"lead_id", payload.LeadID,
				"manager_userid", receiverUserID,
				"err", err)
			continue
		}
		slog.Info("dispatch reminder sent",
			"lead_id", payload.LeadID,
			"manager_userid", receiverUserID)
	}
	if failed {
		return fmt.Errorf("部分分配提醒发送失败: %w", asynq.SkipRetry)
	}
	return nil
}

// HandleRemindClaim 处理提醒销售领取任务
func (h *TaskHandler) HandleRemindClaim(ctx context.Context, t *asynq.Task) error {
	var payload RemindClaimPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("解析任务载荷失败: %w", err)
	}

	slog.Info("processing claim reminder",
		"lead_id", payload.LeadID,
		"sales_userid", payload.SalesUserID,
		"configured_minutes", payload.AssignedMinutes)

	// 查询线索当前状态
	lead, err := h.leadRepo.GetByID(ctx, payload.LeadID)
	if err != nil {
		return fmt.Errorf("查询线索失败: %w", err)
	}
	if lead == nil {
		return nil
	}

	// 检查是否仍处于待领取状态
	if lead.Status != types.LeadStatusAssignedPendingClaim {
		slog.Info("claim reminder skipped because lead is no longer pending claim",
			"lead_id", payload.LeadID,
			"status", lead.Status)
		return nil
	}

	// 计算超时时长
	if lead.AssignedAt == nil {
		return nil
	}
	elapsed := time.Since(*lead.AssignedAt)
	elapsedMinutes := int(elapsed.Minutes())
	reminderIndex, shouldSend := reminderOrdinal(payload.AssignedMinutes, elapsedMinutes, h.runtimeSettings.ClaimReminderDelays(ctx))
	if !shouldSend {
		slog.Info("claim reminder skipped because rule changed or disabled",
			"lead_id", payload.LeadID,
			"configured_minutes", payload.AssignedMinutes,
			"elapsed_minutes", elapsedMinutes)
		return nil
	}
	configuredMinutes := reminderMessageMinutes(payload.AssignedMinutes, elapsedMinutes)

	// 构造提醒消息
	var receivers string
	salesUserID := payload.SalesUserID
	if salesUserID == "" {
		salesUserID = lead.AssignedSalesUserID
	}
	if salesUserID == "" {
		return fmt.Errorf("线索未设置待领取销售: lead_id=%d", payload.LeadID)
	}

	reminderLevel := reminderLevelByIndex(reminderIndex)
	if reminderIndex >= 1 {
		receivers = joinReceivers(salesUserID, lead.DispatchManagerUserID)
	} else {
		receivers = salesUserID
	}

	content := fmt.Sprintf("%s线索领取提醒\n\n"+
		"线索编号：%s\n"+
		"客户：%s\n"+
		"来源：%s\n"+
		"提醒规则：%d 分钟\n"+
		"等待时间：%d 分钟\n\n"+
		"请尽快领取线索！",
		reminderLevel,
		lead.LeadNo,
		lead.CustomerName,
		lead.SourceChannel,
		configuredMinutes,
		elapsedMinutes)

	// 发送文本提醒
	err = h.wecomClient.SendText(ctx, &wecom.SendTextRequest{
		ToUser:  receivers,
		AgentID: h.runtimeSettings.WeComAgentID(ctx),
		Content: content,
	})

	if err != nil {
		slog.Error("send claim reminder failed",
			"lead_id", payload.LeadID,
			"receivers", receivers,
			"err", err)
		return fmt.Errorf("发送领取提醒失败: %w", asynq.SkipRetry)
	}

	slog.Info("claim reminder sent",
		"lead_id", payload.LeadID,
		"receivers", receivers)
	return nil
}

// HandleCardRetry 处理卡片重试任务
func (h *TaskHandler) HandleCardRetry(ctx context.Context, t *asynq.Task) error {
	var payload CardRetryPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("解析任务载荷失败: %w", err)
	}

	slog.Info("processing card retry",
		"card_task_id", payload.CardTaskID)

	// 查询卡片任务
	cardTask, err := h.cardRepo.GetByID(ctx, payload.CardTaskID)
	if err != nil || cardTask == nil {
		slog.Warn("card retry skipped because card task does not exist",
			"card_task_id", payload.CardTaskID,
			"err", err)
		return nil
	}

	// RetryCount can equal the max for an already scheduled final retry.
	if cardTask.RetryCount > types.MaxCardRetryCount {
		slog.Info("card retry skipped because retry count exceeded max",
			"card_task_id", payload.CardTaskID,
			"retry_count", cardTask.RetryCount)
		return nil
	}
	// 查询线索最新状态
	lead, err := h.leadRepo.GetByID(ctx, cardTask.LeadID)
	if err != nil || lead == nil {
		slog.Warn("card retry skipped because lead does not exist",
			"lead_id", cardTask.LeadID,
			"card_task_id", cardTask.ID,
			"err", err)
		return nil
	}
	claimed, err := h.cardRepo.TryStartRetry(ctx, cardTask.ID, cardTask.RetryCount)
	if err != nil {
		return fmt.Errorf("抢占卡片重试任务失败: %w", err)
	}
	if !claimed {
		slog.Info("duplicate card retry skipped", "card_task_id", cardTask.ID)
		return nil
	}
	cardTask.CardStatus = types.CardStatusUpdating
	cardTask.NextRetryAt = nil

	// 首次发送失败时不会产生 response_code，即使占位记录的过期时间
	// 仍在未来，也不能调用更新接口；这种情况必须重新发送一张卡片。
	if strings.TrimSpace(cardTask.ResponseCode) == "" || h.cardLifecycle.IsResponseCodeExpired(cardTask, time.Now()) {
		slog.Info("response code missing or expired during retry; resending card",
			"lead_id", cardTask.LeadID,
			"card_task_id", payload.CardTaskID)

		// 标记旧卡片为过期
		cardTask.CardStatus = types.CardStatusExpired
		h.updateCardTask(ctx, cardTask, "标记重试卡片过期")

		// 重新发送新卡片
		return h.resendCard(ctx, lead, cardTask)
	}

	// response_code 有效，尝试更新卡片
	slog.Info("retrying card update",
		"lead_id", cardTask.LeadID,
		"card_task_id", payload.CardTaskID,
		"retry_count", cardTask.RetryCount)

	content := h.cardLifecycle.BuildCardContent(cardTask.CardScene, lead)
	resp, err := h.wecomClient.UpdateCard(ctx, &wecom.UpdateCardRequest{
		AgentID:      h.runtimeSettings.WeComAgentID(ctx),
		ResponseCode: cardTask.ResponseCode,
		Content:      content,
	})

	updateSuccess := err == nil && resp != nil && resp.Success

	if !updateSuccess {
		// 更新失败，记录并调度下一次重试
		cardTask.CardStatus = types.CardStatusUpdateFailed
		if err != nil {
			cardTask.LastError = err.Error()
		} else if resp == nil {
			cardTask.LastError = "企业微信卡片更新返回空响应"
		} else if resp.Error != nil {
			cardTask.LastError = resp.Error.Error()
		} else {
			cardTask.LastError = "企业微信卡片更新失败"
		}

		if isInvalidResponseCode(resp) {
			slog.Warn("invalid response code during retry; resending card",
				"lead_id", cardTask.LeadID,
				"card_task_id", payload.CardTaskID)
			cardTask.CardStatus = types.CardStatusExpired
			h.updateCardTask(ctx, cardTask, "标记重试卡片 response_code 无效")
			return h.resendCard(ctx, lead, cardTask)
		}

		h.updateCardTask(ctx, cardTask, "保存卡片重试失败状态")
		h.scheduleRetry(ctx, cardTask, "重试更新卡片")

		return nil
	}

	// 更新成功
	cardTask.CardStatus = types.CardStatusUpdated
	now := time.Now()
	cardTask.UpdatedAt = &now
	h.updateCardTask(ctx, cardTask, "保存卡片重试成功状态")

	slog.Info("card retry succeeded",
		"lead_id", cardTask.LeadID,
		"card_task_id", payload.CardTaskID)
	return nil
}

func (h *TaskHandler) HandleScanFailedCards(ctx context.Context, t *asynq.Task) error {
	if observableRepo, ok := h.cardRepo.(repo.CardTaskObservabilityRepository); ok {
		count, err := observableRepo.CountStaleUpdating(ctx, time.Now().Add(-10*time.Minute))
		if err != nil {
			slog.Error("count stale card operations failed", "err", err)
		} else {
			h.metrics.SetStaleCardUpdating(count)
			if count > 0 {
				slog.Error("stale card operations require reconciliation", "task_count", count)
			}
		}
	}
	tasks, err := h.cardRepo.ListFailedTasks(ctx, 100)
	if err != nil {
		return fmt.Errorf("查询失败卡片任务失败: %w", err)
	}
	if len(tasks) == 0 {
		return nil
	}
	slog.Info("failed card scan found retryable tasks",
		"task_count", len(tasks))

	for _, task := range tasks {
		if task == nil || task.ID == 0 {
			continue
		}
		if h.scheduler == nil {
			continue
		}
		if err := h.scheduler.ScheduleCardRetry(ctx, task.ID, 0); err != nil {
			slog.Error("schedule failed-card compensation failed",
				"card_task_id", task.ID,
				"lead_id", task.LeadID,
				"err", err)
		}
	}
	return nil
}

func (h *TaskHandler) createCardTask(ctx context.Context, task *types.WeComCardTask, action string) bool {
	if err := h.cardRepo.Create(ctx, task); err != nil {
		slog.Error("create card task failed",
			"action", action,
			"lead_id", task.LeadID,
			"task_id", task.TaskID,
			"err", err)
		return false
	}
	return true
}

func (h *TaskHandler) createSendPlaceholder(ctx context.Context, task *types.WeComCardTask) (bool, error) {
	existing, err := h.cardRepo.GetByTaskID(ctx, task.TaskID)
	if err != nil {
		return false, err
	}
	if existing != nil {
		return false, nil
	}
	if err := h.cardRepo.Create(ctx, task); err != nil {
		existing, getErr := h.cardRepo.GetByTaskID(ctx, task.TaskID)
		if getErr == nil && existing != nil {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (h *TaskHandler) updateCardTask(ctx context.Context, task *types.WeComCardTask, action string) bool {
	if err := h.cardRepo.Update(ctx, task); err != nil {
		slog.Error("update card task failed",
			"action", action,
			"card_task_id", task.ID,
			"lead_id", task.LeadID,
			"err", err)
		return false
	}
	return true
}

func isInvalidResponseCode(resp *wecom.UpdateCardResponse) bool {
	if resp == nil || resp.Error == nil {
		return false
	}
	message := strings.ToLower(resp.Error.Error())
	return strings.Contains(message, "60140") || strings.Contains(message, "invalid response code")
}

// resendCard 重新发送卡片
func (h *TaskHandler) resendCard(ctx context.Context, lead *types.Lead, oldCardTask *types.WeComCardTask) error {
	content := h.cardLifecycle.BuildCardContent(oldCardTask.CardScene, lead)
	taskID := buildRecipientCardTaskID(lead.ID, oldCardTask.CardScene, oldCardTask.ReceiverUserID, resendSuffix())
	now := time.Now()
	expireAt := h.cardLifecycle.CalculateResponseCodeExpireTime(now)
	newCardTask := &types.WeComCardTask{
		LeadID:               lead.ID,
		CardScene:            oldCardTask.CardScene,
		ReceiverUserID:       oldCardTask.ReceiverUserID,
		TaskID:               taskID,
		CardStatus:           types.CardStatusUpdating,
		RetryCount:           oldCardTask.RetryCount,
		SentAt:               now,
		ResponseCodeExpireAt: &expireAt,
	}
	if !h.createCardTask(ctx, newCardTask, "创建重新发送卡片占位") {
		oldCardTask.CardStatus = types.CardStatusUpdateFailed
		oldCardTask.LastError = "创建重新发送卡片占位失败"
		if h.updateCardTask(ctx, oldCardTask, "恢复重新发送失败的源卡片状态") {
			h.scheduleRetry(ctx, oldCardTask, "重新发送卡片")
		}
		return nil
	}

	resp, err := h.wecomClient.SendCard(ctx, &wecom.SendCardRequest{
		ToUser:  oldCardTask.ReceiverUserID,
		AgentID: h.runtimeSettings.WeComAgentID(ctx),
		TaskID:  taskID,
		Content: content,
	})
	err = cardSendError(resp, err)

	if err != nil {
		newCardTask.CardStatus = types.CardStatusUpdateFailed
		newCardTask.LastError = err.Error()
		slog.Error("resend card failed",
			"lead_id", lead.ID,
			"card_task_id", oldCardTask.ID,
			"receiver_userid", oldCardTask.ReceiverUserID,
			"card_scene", oldCardTask.CardScene,
			"err", err)
	} else {
		newCardTask.MsgID = resp.MsgID
		newCardTask.ResponseCode = resp.ResponseCode
		newCardTask.CardStatus = types.CardStatusSent
		slog.Info("resend card succeeded",
			"lead_id", lead.ID,
			"card_task_id", oldCardTask.ID,
			"receiver_userid", oldCardTask.ReceiverUserID,
			"card_scene", oldCardTask.CardScene,
			"msg_id", resp.MsgID)
	}

	if !h.updateCardTask(ctx, newCardTask, "保存重新发送卡片结果") {
		return nil
	}
	if err != nil {
		h.scheduleRetry(ctx, newCardTask, "重新发送卡片")
		return nil
	}
	oldCardTask.CardStatus = types.CardStatusReplaced
	oldCardTask.NextRetryAt = nil
	oldCardTask.LastError = ""
	oldCardTask.UpdatedAt = &now
	h.updateCardTask(ctx, oldCardTask, "标记旧卡片已被替代")
	return nil
}

func cardSendError(resp *wecom.SendCardResponse, err error) error {
	if err != nil {
		return err
	}
	if resp == nil {
		return errors.New("企业微信卡片发送返回空响应")
	}
	return nil
}

func buildCardTaskID(leadID int64, scene types.CardScene, suffix string) string {
	sceneCode := map[types.CardScene]string{
		types.CardSceneSupervisorPendingDispatch:    "spd",
		types.CardSceneSupervisorWaitingClaim:       "swc",
		types.CardSceneSupervisorClaimed:            "scd",
		types.CardSceneSalesPendingClaim:            "spc",
		types.CardSceneSalesClaimed:                 "scl",
		types.CardSceneAppointmentPendingAssignment: "apa",
		types.CardSceneAppointmentAssigned:          "aas",
		types.CardSceneAppointmentRescheduled:       "ars",
		types.CardSceneAppointmentCancelled:         "aca",
		types.CardSceneAppointmentNoShow:            "ans",
		types.CardSceneAppointmentArrivedDelivered:  "aad",
	}[scene]
	if sceneCode == "" {
		sceneCode = "card"
	}
	if suffix != "" {
		return fmt.Sprintf("l%d_%s_%s", leadID, sceneCode, suffix)
	}
	return fmt.Sprintf("l%d_%s", leadID, sceneCode)
}

func buildRecipientCardTaskID(leadID int64, scene types.CardScene, receiverUserID, suffix string) string {
	receiverHash := sha256.Sum256([]byte(strings.TrimSpace(receiverUserID)))
	receiver := fmt.Sprintf("%x", receiverHash[:6])
	return buildCardTaskID(leadID, scene, receiver+"_"+suffix)
}

func resendSuffix() string {
	return fmt.Sprintf("r%x", time.Now().UnixNano())
}

func (h *TaskHandler) scheduleRetry(ctx context.Context, cardTask *types.WeComCardTask, operation string) {
	if cardTask.RetryCount >= types.MaxCardRetryCount {
		cardTask.NextRetryAt = nil
		if err := h.cardRepo.Update(ctx, cardTask); err != nil {
			slog.Error("update card retry state failed",
				"card_task_id", cardTask.ID,
				"lead_id", cardTask.LeadID,
				"err", err)
		}
		slog.Warn("card operation reached max retry count",
			"operation", operation,
			"card_task_id", cardTask.ID,
			"lead_id", cardTask.LeadID)
		return
	}

	cardTask.RetryCount++
	nextRetry := card.NewLifecycleWithRetryIntervals(h.runtimeSettings.CardRetryDelays(ctx)).
		CalculateNextRetryTime(time.Now(), cardTask.RetryCount-1)
	cardTask.NextRetryAt = &nextRetry
	if err := h.cardRepo.Update(ctx, cardTask); err != nil {
		slog.Error("update card retry state failed",
			"card_task_id", cardTask.ID,
			"lead_id", cardTask.LeadID,
			"err", err)
		return
	}

	if h.scheduler == nil {
		return
	}

	delay := time.Until(nextRetry)
	if scheduleErr := h.scheduler.ScheduleCardRetry(ctx, cardTask.ID, delay); scheduleErr != nil {
		slog.Error("schedule card retry failed",
			"operation", operation,
			"card_task_id", cardTask.ID,
			"lead_id", cardTask.LeadID,
			"delay", delay,
			"err", scheduleErr)
	} else {
		slog.Info("card retry scheduled",
			"operation", operation,
			"card_task_id", cardTask.ID,
			"lead_id", cardTask.LeadID,
			"retry_count", cardTask.RetryCount,
			"delay", delay)
	}
}

func joinReceivers(receivers ...string) string {
	joined := ""
	for _, receiver := range receivers {
		if receiver == "" {
			continue
		}
		if joined != "" {
			joined += "|"
		}
		joined += receiver
	}
	return joined
}

func reminderOrdinal(payloadMinutes, elapsedMinutes int, delays []time.Duration) (int, bool) {
	minutes := reminderDelayMinutes(delays)
	if len(minutes) == 0 {
		return 0, false
	}

	if payloadMinutes > 0 {
		for index, configured := range minutes {
			if configured == payloadMinutes {
				return index, true
			}
		}
		return 0, false
	}

	for index := len(minutes) - 1; index >= 0; index-- {
		if elapsedMinutes >= minutes[index] {
			return index, true
		}
	}
	return 0, false
}

func reminderDelayMinutes(delays []time.Duration) []int {
	minutes := make([]int, 0, len(delays))
	for _, delay := range delays {
		if delay <= 0 {
			continue
		}
		minute := int(delay / time.Minute)
		if delay%time.Minute != 0 {
			minute++
		}
		if minute < 1 {
			minute = 1
		}
		minutes = append(minutes, minute)
	}
	sort.Ints(minutes)
	return minutes
}

func reminderLevelByIndex(index int) string {
	switch {
	case index >= 2:
		return "【紧急】"
	case index == 1:
		return "【重要】"
	default:
		return ""
	}
}

func reminderMessageMinutes(payloadMinutes, elapsedMinutes int) int {
	if payloadMinutes > 0 {
		return payloadMinutes
	}
	return elapsedMinutes
}
