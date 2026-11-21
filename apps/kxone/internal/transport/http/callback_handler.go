package http

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
	"github.com/lsaac128/kxone/internal/usecase"
	"github.com/lsaac128/kxone/pkg/types"
)

// CallbackHandler 企业微信回调事件处理器
type CallbackHandler struct {
	wecomClient  wecom.Client
	claimUseCase *usecase.ClaimLeadUseCase
	callbackRepo repo.CallbackLogRepository
}

func NewCallbackHandler(
	wecomClient wecom.Client,
	claimUseCase *usecase.ClaimLeadUseCase,
	callbackRepo repo.CallbackLogRepository,
) *CallbackHandler {
	return &CallbackHandler{
		wecomClient:  wecomClient,
		claimUseCase: claimUseCase,
		callbackRepo: callbackRepo,
	}
}

// CallbackEvent 企业微信回调事件结构
type CallbackEvent struct {
	ToUserName   string `xml:"ToUserName"`
	FromUserName string `xml:"FromUserName"`
	CreateTime   int64  `xml:"CreateTime"`
	MsgType      string `xml:"MsgType"`
	Event        string `xml:"Event"`
	EventKey     string `xml:"EventKey"`
	TaskID       string `xml:"TaskId"`
}

// HandleCallback 处理企业微信事件回调
func (h *CallbackHandler) HandleCallback(c *gin.Context, plainText []byte) {
	// 解析事件 XML
	var event CallbackEvent
	if err := xml.Unmarshal(plainText, &event); err != nil {
		h.logCallback(c.Request.Context(), "", "", "", string(plainText), "failed", err.Error())
		c.Writer.WriteString("success") // 企业微信要求必须返回 success
		return
	}

	// 记录回调日志（幂等检查）
	exists, err := h.callbackRepo.ExistsByTaskAndUser(
		c.Request.Context(),
		event.TaskID,
		event.FromUserName,
		event.EventKey,
	)
	if err != nil {
		log.Printf("企业微信回调查重失败: task_id=%s from_user=%s event_key=%s err=%v",
			event.TaskID, event.FromUserName, event.EventKey, err)
	}

	if exists {
		// 重复回调，直接返回成功
		c.Writer.WriteString("success")
		return
	}

	// 保存回调日志
	callbackLog, duplicated := h.logCallback(
		c.Request.Context(),
		event.TaskID,
		event.FromUserName,
		event.EventKey,
		string(plainText),
		"processing",
		"",
	)
	if duplicated {
		c.Writer.WriteString("success")
		return
	}

	// 分发事件处理
	var processErr error
	if event.EventKey == "" && event.TaskID == "" {
		if callbackLog != nil {
			if err := h.callbackRepo.UpdateProcessResult(c.Request.Context(), callbackLog.ID, "success", ""); err != nil {
				log.Printf("企业微信回调状态更新失败: callback_id=%d err=%v", callbackLog.ID, err)
			}
		}
		c.Writer.WriteString("success")
		return
	}
	switch event.EventKey {
	case "CLAIM_LEAD":
		processErr = h.handleClaimEvent(c.Request.Context(), &event)
	case "ASSIGN_LEAD":
		// 主管点击分配，跳转到 H5，无需后端处理
		processErr = nil
	case "VIEW_LEAD":
		// 查看详情，跳转到 H5，无需后端处理
		processErr = nil
	case "FOLLOWUP_LEAD":
		// 填写跟进，跳转到 H5，无需后端处理
		processErr = nil
	default:
		processErr = fmt.Errorf("未知事件类型: %s", event.EventKey)
	}

	// 更新处理状态
	status := "success"
	errorMsg := ""
	if processErr != nil {
		status = "failed"
		errorMsg = processErr.Error()
	}

	if callbackLog != nil {
		if err := h.callbackRepo.UpdateProcessResult(c.Request.Context(), callbackLog.ID, status, errorMsg); err != nil {
			log.Printf("企业微信回调状态更新失败: callback_id=%d err=%v", callbackLog.ID, err)
		}
	}

	// 企业微信要求必须返回 success
	c.Writer.WriteString("success")
}

// handleClaimEvent 处理销售领取事件
func (h *CallbackHandler) handleClaimEvent(ctx context.Context, event *CallbackEvent) error {
	// 从 task_id 中提取 lead_id。
	leadID, err := extractLeadIDFromTaskID(event.TaskID)
	if err != nil {
		return fmt.Errorf("解析 task_id 失败: %w", err)
	}

	// 执行领取逻辑
	return h.claimUseCase.Execute(ctx, &usecase.ClaimLeadRequest{
		LeadID:      leadID,
		SalesUserID: event.FromUserName,
		TaskID:      event.TaskID,
		Source:      "wecom_card",
	})
}

// logCallback 记录回调日志
func (h *CallbackHandler) logCallback(
	ctx context.Context,
	taskID, fromUserID, eventKey, payload, status, errorMsg string,
) (*types.WeComCallbackLog, bool) {
	callbackLog := &types.WeComCallbackLog{
		TaskID:        taskID,
		FromUserID:    fromUserID,
		EventKey:      eventKey,
		RawPayload:    payload,
		ProcessStatus: status,
		ErrorMessage:  errorMsg,
	}

	// 如果能提取 lead_id，也记录下来
	if taskID != "" {
		if leadID, err := extractLeadIDFromTaskID(taskID); err == nil {
			callbackLog.LeadID = &leadID
		}
	}

	if err := h.callbackRepo.Create(ctx, callbackLog); err != nil {
		if errors.Is(err, types.ErrDuplicateCallback) {
			return nil, true
		}
		log.Printf("企业微信回调日志写入失败: task_id=%s from_user=%s event_key=%s err=%v",
			taskID, fromUserID, eventKey, err)
		return nil, false
	}
	return callbackLog, false
}

// extractLeadIDFromTaskID 从 task_id 中提取 lead_id
// 支持旧格式 lead:{lead_id}:... 和新格式 l{lead_id}_{scene}[_resend]。
func extractLeadIDFromTaskID(taskID string) (int64, error) {
	var leadID int64
	if _, err := fmt.Sscanf(taskID, "lead:%d:", &leadID); err == nil {
		return leadID, nil
	}
	if _, err := fmt.Sscanf(taskID, "l%d_", &leadID); err == nil {
		return leadID, nil
	}
	return 0, fmt.Errorf("invalid task_id format: %s", taskID)
}
