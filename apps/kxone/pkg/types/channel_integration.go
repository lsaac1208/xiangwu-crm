package types

import "time"

const (
	LeadPlatformBaidu       = "baidu"
	LeadPlatformTencent     = "tencent"
	LeadPlatformXiaohongshu = "xiaohongshu"
	LeadPlatformDouyin      = "douyin"
)

type ChannelIntegration struct {
	ID               int64      `json:"id"`
	Platform         string     `json:"platform"`
	AccountName      string     `json:"account_name"`
	CallbackPath     string     `json:"callback_path"`
	AuthMode         string     `json:"auth_mode"`
	SecretConfigured bool       `json:"secret_configured"`
	WebhookToken     string     `json:"-"`
	Enabled          bool       `json:"enabled"`
	LastReceivedAt   *time.Time `json:"last_received_at,omitempty"`
	LastStatus       string     `json:"last_status,omitempty"`
	LastLeadID       *int64     `json:"last_lead_id,omitempty"`
	LastError        string     `json:"last_error,omitempty"`
	CreatedBy        string     `json:"created_by"`
	UpdatedBy        string     `json:"updated_by"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func IsLeadPlatform(value string) bool {
	return value == LeadPlatformBaidu || value == LeadPlatformTencent || value == LeadPlatformXiaohongshu || value == LeadPlatformDouyin
}
