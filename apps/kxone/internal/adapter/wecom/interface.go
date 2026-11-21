package wecom

import (
	"context"

	"github.com/lsaac128/kxone/pkg/types"
)

// Config 企业微信运行期配置。
type Config struct {
	CorpID         string
	AgentID        int
	Secret         string
	Token          string
	EncodingAESKey string
}

// ConfigProvider 提供企业微信运行期配置。
type ConfigProvider interface {
	GetWeComConfig(ctx context.Context) Config
}

// Client 企业微信客户端接口
type Client interface {
	// SendCard 发送模板卡片
	SendCard(ctx context.Context, req *SendCardRequest) (*SendCardResponse, error)

	// UpdateCard 更新模板卡片
	UpdateCard(ctx context.Context, req *UpdateCardRequest) (*UpdateCardResponse, error)

	// SendText 发送文本消息
	SendText(ctx context.Context, req *SendTextRequest) error

	// GetAccessToken 获取 access_token（带缓存）
	GetAccessToken(ctx context.Context) (string, error)

	// VerifyCallbackURL 验证回调 URL（GET 请求）
	VerifyCallbackURL(msgSignature, timestamp, nonce, echoStr string) (string, error)

	// DecryptCallback 解密回调消息（POST 请求）
	DecryptCallback(msgSignature, timestamp, nonce string, encryptedData []byte) ([]byte, error)

	// GetUserIDByCode 通过 OAuth code 获取 UserID
	GetUserIDByCode(ctx context.Context, code string) (string, error)

	// GetJSAPIConfig 获取 JSSDK 初始化配置
	GetJSAPIConfig(ctx context.Context, url string) (*JSAPIConfig, error)
}

// SendCardRequest 发送卡片请求
type SendCardRequest struct {
	ToUser  string                  // 接收人 UserID
	AgentID int                     // 应用 ID
	TaskID  string                  // 任务 ID（用于后续更新）
	Content *types.WeComCardContent // 卡片内容
}

// SendCardResponse 发送卡片响应
type SendCardResponse struct {
	MsgID        string // 消息 ID
	ResponseCode string // response_code（用于更新卡片）
}

// UpdateCardRequest 更新卡片请求
type UpdateCardRequest struct {
	AgentID      int                     // 应用 ID
	ResponseCode string                  // 之前发送时返回的 response_code
	Content      *types.WeComCardContent // 新的卡片内容
}

// UpdateCardResponse 更新卡片响应
type UpdateCardResponse struct {
	Success bool
	Error   error
}

// SendTextRequest 发送文本消息请求
type SendTextRequest struct {
	ToUser  string // 接收人 UserID，多个用 | 分隔
	AgentID int    // 应用 ID
	Content string // 文本内容
}

// JSAPIConfig JSSDK 初始化配置。
type JSAPIConfig struct {
	AppID     string `json:"appId"`
	Timestamp int64  `json:"timestamp"`
	NonceStr  string `json:"nonceStr"`
	Signature string `json:"signature"`
}
