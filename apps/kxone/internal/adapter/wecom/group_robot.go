package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/repo"
	"github.com/lsaac128/kxone/pkg/types"
)

const (
	groupRobotWebhookSettingKey = "notification.group_robot_webhook_url"
)

type GroupRobotLead struct {
	LeadID          int64
	BusinessVersion int
	Source          string
	AccountName     string
	CustomerName    string
	Phone           string
	Wechat          string
	Region          string
	FormName        string
	StatusOrTag     string
	CampaignName    string
	AdgroupName     string
	CreativeName    string
	LeadCreatedAt   string
}

type GroupRobotNotifier interface {
	NotifyLead(context.Context, GroupRobotLead) error
}

type GroupRobotMarkdownSender interface {
	SendMarkdown(context.Context, string) error
}

type GroupRobotMessage struct {
	Content        string
	MentionUserIDs []string
}

type GroupRobotSender interface {
	Send(context.Context, GroupRobotMessage) error
}

// GroupRobotScanWaker asks the Redis-backed worker queue to process newly
// persisted notifications immediately. The minute cron remains the recovery
// path when Redis is temporarily unavailable.
type GroupRobotScanWaker interface {
	WakeGroupRobotScan(context.Context) error
}

type DatabaseGroupRobotNotifier struct {
	settings         repo.SystemSettingRepository
	notificationRepo repo.GroupRobotNotificationRepository
	scanWaker        GroupRobotScanWaker
	client           *http.Client
}

func NewDatabaseGroupRobotNotifier(settings repo.SystemSettingRepository) *DatabaseGroupRobotNotifier {
	return &DatabaseGroupRobotNotifier{settings: settings, client: &http.Client{Timeout: 5 * time.Second}}
}

func (n *DatabaseGroupRobotNotifier) WithNotificationRepository(repository repo.GroupRobotNotificationRepository) *DatabaseGroupRobotNotifier {
	n.notificationRepo = repository
	return n
}

func (n *DatabaseGroupRobotNotifier) WithScanWaker(waker GroupRobotScanWaker) *DatabaseGroupRobotNotifier {
	n.scanWaker = waker
	return n
}

func (n *DatabaseGroupRobotNotifier) NotifyLead(ctx context.Context, lead GroupRobotLead) error {
	if n.notificationRepo == nil {
		return fmt.Errorf("group robot notification repository is not configured")
	}
	now := time.Now()
	_, err := n.notificationRepo.Create(ctx, &types.GroupRobotNotification{
		EventKey: fmt.Sprintf("new-lead:%d", lead.LeadID), Category: types.GroupRobotCategoryNewLead,
		EventType: types.GroupRobotEventNewLead, LeadID: lead.LeadID, BusinessVersion: lead.BusinessVersion,
		DueAt: now, Status: types.GroupRobotNotificationPending, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return err
	}
	if n.scanWaker != nil {
		if err := n.scanWaker.WakeGroupRobotScan(ctx); err != nil {
			return fmt.Errorf("wake group robot notification worker: %w", err)
		}
	}
	return nil
}

func (n *DatabaseGroupRobotNotifier) SendMarkdown(ctx context.Context, content string) error {
	return n.Send(ctx, GroupRobotMessage{Content: content})
}

func (n *DatabaseGroupRobotNotifier) Send(ctx context.Context, message GroupRobotMessage) error {
	webhook, err := n.settings.GetByKey(ctx, groupRobotWebhookSettingKey)
	if err != nil {
		return fmt.Errorf("read group robot webhook: %w", err)
	}
	if webhook == nil || strings.TrimSpace(webhook.Value) == "" {
		return fmt.Errorf("group robot webhook is not configured")
	}

	mentions := make([]string, 0, len(message.MentionUserIDs))
	seen := make(map[string]struct{}, len(message.MentionUserIDs))
	for _, userID := range message.MentionUserIDs {
		userID = strings.TrimSpace(userID)
		if userID == "" {
			continue
		}
		if _, exists := seen[userID]; exists {
			continue
		}
		seen[userID] = struct{}{}
		mentions = append(mentions, userID)
	}
	body, err := json.Marshal(map[string]any{
		"msgtype": "text",
		"text":    map[string]any{"content": message.Content, "mentioned_list": mentions},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(webhook.Value), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("group robot webhook configuration is invalid")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return safeGroupRobotTransportError(err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return safeGroupRobotTransportError(err)
	}
	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(responseBody, &result) != nil || result.ErrCode != 0 {
		return fmt.Errorf("group robot rejected notification: status=%d errcode=%d", resp.StatusCode, result.ErrCode)
	}
	return nil
}

func renderGroupRobotLead(lead GroupRobotLead) string {
	sourceNames := map[string]string{"baidu": "百度营销", "tencent": "腾讯营销", "xiaohongshu": "小红书", "douyin": "抖音", "form": "公众号表单", "manual": "手动录入"}
	title := sourceNames[lead.Source]
	if title == "" {
		title = lead.Source
	}
	if lead.AccountName != "" {
		title += "（" + lead.AccountName + "）"
	}
	lines := []string{"### 新线索 - " + title, "", "> 姓名：" + fallbackText(lead.CustomerName), "> 电话：" + fallbackText(lead.Phone)}
	if lead.Wechat != "" {
		lines = append(lines, "> 微信："+lead.Wechat)
	}
	if lead.Region != "" {
		lines = append(lines, "> 地区："+lead.Region)
	}
	if lead.FormName != "" {
		lines = append(lines, "> 表单："+lead.FormName)
	}
	if lead.StatusOrTag != "" {
		lines = append(lines, "> 标签："+lead.StatusOrTag)
	}
	if lead.LeadCreatedAt != "" {
		lines = append(lines, "> 时间："+lead.LeadCreatedAt)
	}
	for _, detail := range []string{lead.CampaignName, lead.AdgroupName, lead.CreativeName} {
		if detail != "" {
			lines = append(lines, detail)
		}
	}
	return strings.Join(lines, "\n")
}

func fallbackText(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

// No vendor URL, response text or underlying transport message crosses this
// boundary: callers persist and log this error, including admin test sends.
func safeGroupRobotTransportError(err error) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("group robot send canceled: %w", context.Canceled)
	}
	var networkError net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkError) && networkError.Timeout()) {
		return fmt.Errorf("group robot send timeout: %w", context.DeadlineExceeded)
	}
	return fmt.Errorf("group robot send transport failure")
}
