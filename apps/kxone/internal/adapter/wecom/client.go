package wecom

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lsaac128/kxone/pkg/types"
)

// Client 企业微信客户端实现
type client struct {
	configProvider ConfigProvider
	cache          AccessTokenCache
	httpClient     *http.Client
	apiBaseURL     string
	tokenMu        sync.Mutex
	ticketMu       sync.Mutex
	localTokens    map[string]cachedCredential
	localTickets   map[string]cachedCredential
}

// DefaultAPIBaseURL is the official WeCom API endpoint.
const DefaultAPIBaseURL = "https://qyapi.weixin.qq.com"

type cachedCredential struct {
	value     string
	expiresAt time.Time
}

// AccessTokenCache access_token 缓存接口
type AccessTokenCache interface {
	Get(ctx context.Context, corpID string) (string, error)
	Set(ctx context.Context, corpID string, token string, expiry time.Duration) error
	GetJSAPITicket(ctx context.Context, corpID string) (string, error)
	SetJSAPITicket(ctx context.Context, corpID string, ticket string, expiry time.Duration) error
}

// NewClient 创建企业微信客户端
func NewClient(corpID, secret string, agentID int, token, encodingAESKey string, cache AccessTokenCache) Client {
	return NewClientWithBaseURL(corpID, secret, agentID, token, encodingAESKey, cache, DefaultAPIBaseURL)
}

// NewClientWithBaseURL creates a WeCom client with an injectable API endpoint.
func NewClientWithBaseURL(corpID, secret string, agentID int, token, encodingAESKey string, cache AccessTokenCache, apiBaseURL string) Client {
	return NewDynamicClientWithBaseURL(staticConfigProvider{config: Config{
		CorpID:         corpID,
		AgentID:        agentID,
		Secret:         secret,
		Token:          token,
		EncodingAESKey: encodingAESKey,
	}}, cache, apiBaseURL)
}

// NewDynamicClient 创建支持运行期配置的企业微信客户端。
func NewDynamicClient(provider ConfigProvider, cache AccessTokenCache) Client {
	return NewDynamicClientWithBaseURL(provider, cache, DefaultAPIBaseURL)
}

// NewDynamicClientWithBaseURL creates a runtime-configured client with an injectable API endpoint.
func NewDynamicClientWithBaseURL(provider ConfigProvider, cache AccessTokenCache, apiBaseURL string) Client {
	return &client{
		configProvider: provider,
		cache:          cache,
		apiBaseURL:     normalizeAPIBaseURL(apiBaseURL),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 20,
				MaxConnsPerHost:     50,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		localTokens:  make(map[string]cachedCredential),
		localTickets: make(map[string]cachedCredential),
	}
}

// GetAccessToken 获取 access_token（带缓存）
func (c *client) GetAccessToken(ctx context.Context) (string, error) {
	cfg := c.getConfig(ctx)
	if cfg.CorpID == "" || cfg.Secret == "" {
		return "", fmt.Errorf("企业微信 CorpID 或 Secret 未配置")
	}

	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if token := getLocalCredential(c.localTokens, cfg.CorpID); token != "" {
		return token, nil
	}
	if c.cache != nil {
		token, err := c.cache.Get(ctx, cfg.CorpID)
		if err == nil && token != "" {
			setLocalCredential(c.localTokens, cfg.CorpID, token, time.Hour)
			return token, nil
		}
	}

	endpoint := c.apiURL("/cgi-bin/gettoken", url.Values{
		"corpid":     {cfg.CorpID},
		"corpsecret": {cfg.Secret},
	})
	body, err := c.get(ctx, endpoint)
	if err != nil {
		return "", fmt.Errorf("获取 access_token 失败: %w", err)
	}

	var result struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if result.ErrCode != 0 {
		return "", fmt.Errorf("企业微信返回错误: [%d] %s", result.ErrCode, result.ErrMsg)
	}

	// 缓存 token（提前 5 分钟过期，避免临界点问题）
	expiry := time.Duration(result.ExpiresIn-300) * time.Second
	if expiry <= 0 {
		expiry = time.Hour
	}
	setLocalCredential(c.localTokens, cfg.CorpID, result.AccessToken, expiry)
	if c.cache != nil {
		if err := c.cache.Set(ctx, cfg.CorpID, result.AccessToken, expiry); err != nil {
			log.Printf("企业微信 access_token 写入 Redis 缓存失败: corp_id=%s err=%v", cfg.CorpID, err)
		}
	}

	return result.AccessToken, nil
}

func (c *client) GetJSAPIConfig(ctx context.Context, pageURL string) (*JSAPIConfig, error) {
	if pageURL == "" {
		return nil, fmt.Errorf("JSSDK URL 未提供")
	}

	cfg := c.getConfig(ctx)
	if cfg.CorpID == "" {
		return nil, fmt.Errorf("企业微信 CorpID 未配置")
	}

	ticket, err := c.getJSAPITicket(ctx, cfg.CorpID)
	if err != nil {
		return nil, err
	}

	nonce := randomNonce()
	timestamp := time.Now().Unix()
	signature := jsapiSignature(ticket, nonce, timestamp, pageURL)

	return &JSAPIConfig{
		AppID:     cfg.CorpID,
		Timestamp: timestamp,
		NonceStr:  nonce,
		Signature: signature,
	}, nil
}

func (c *client) getJSAPITicket(ctx context.Context, corpID string) (string, error) {
	c.ticketMu.Lock()
	defer c.ticketMu.Unlock()

	if ticket := getLocalCredential(c.localTickets, corpID); ticket != "" {
		return ticket, nil
	}
	if c.cache != nil {
		ticket, err := c.cache.GetJSAPITicket(ctx, corpID)
		if err == nil && ticket != "" {
			setLocalCredential(c.localTickets, corpID, ticket, time.Hour)
			return ticket, nil
		}
	}

	token, err := c.GetAccessToken(ctx)
	if err != nil {
		return "", err
	}

	endpoint := c.apiURL("/cgi-bin/get_jsapi_ticket", url.Values{"access_token": {token}})
	body, err := c.get(ctx, endpoint)
	if err != nil {
		return "", fmt.Errorf("获取 jsapi_ticket 失败: %w", err)
	}

	var result struct {
		ErrCode   int    `json:"errcode"`
		ErrMsg    string `json:"errmsg"`
		Ticket    string `json:"ticket"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析 jsapi_ticket 响应失败: %w", err)
	}
	if result.ErrCode != 0 {
		return "", fmt.Errorf("企业微信返回错误: [%d] %s", result.ErrCode, result.ErrMsg)
	}

	expiry := time.Duration(result.ExpiresIn-300) * time.Second
	if expiry <= 0 {
		expiry = time.Hour
	}
	setLocalCredential(c.localTickets, corpID, result.Ticket, expiry)
	if c.cache != nil {
		if err := c.cache.SetJSAPITicket(ctx, corpID, result.Ticket, expiry); err != nil {
			log.Printf("企业微信 jsapi_ticket 写入 Redis 缓存失败: corp_id=%s err=%v", corpID, err)
		}
	}
	return result.Ticket, nil
}

// SendCard 发送模板卡片
func (c *client) SendCard(ctx context.Context, req *SendCardRequest) (*SendCardResponse, error) {
	token, err := c.GetAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	endpoint := c.apiURL("/cgi-bin/message/send", url.Values{"access_token": {token}})

	agentID := req.AgentID
	if agentID == 0 {
		agentID = c.getConfig(ctx).AgentID
	}

	// 构造请求体
	body := map[string]interface{}{
		"touser":        req.ToUser,
		"msgtype":       "template_card",
		"agentid":       agentID,
		"template_card": buildTemplateCard(req.TaskID, req.Content),
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	httpResp, err := c.postJSON(ctx, endpoint, bodyBytes)
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var result struct {
		ErrCode      int    `json:"errcode"`
		ErrMsg       string `json:"errmsg"`
		MsgID        string `json:"msgid"`
		ResponseCode string `json:"response_code"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if result.ErrCode != 0 {
		return nil, fmt.Errorf("企业微信返回错误: [%d] %s", result.ErrCode, result.ErrMsg)
	}

	return &SendCardResponse{
		MsgID:        result.MsgID,
		ResponseCode: result.ResponseCode,
	}, nil
}

// UpdateCard 更新模板卡片
func (c *client) UpdateCard(ctx context.Context, req *UpdateCardRequest) (*UpdateCardResponse, error) {
	token, err := c.GetAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	endpoint := c.apiURL("/cgi-bin/message/update_template_card", url.Values{"access_token": {token}})

	agentID := req.AgentID
	if agentID == 0 {
		agentID = c.getConfig(ctx).AgentID
	}

	// 构造请求体
	body := map[string]interface{}{
		"agentid":       agentID,
		"response_code": req.ResponseCode,
		"atall":         1,
		"template_card": buildTemplateCardUpdate(req.Content),
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	httpResp, err := c.postJSON(ctx, endpoint, bodyBytes)
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if result.ErrCode != 0 {
		// response_code 过期的特殊处理
		if result.ErrCode == 41001 || result.ErrCode == 40001 {
			return &UpdateCardResponse{Success: false, Error: fmt.Errorf("response_code 已失效")}, nil
		}
		return &UpdateCardResponse{Success: false, Error: fmt.Errorf("[%d] %s", result.ErrCode, result.ErrMsg)}, nil
	}

	return &UpdateCardResponse{Success: true}, nil
}

// GetUserIDByCode 通过 OAuth code 获取 UserID
func (c *client) GetUserIDByCode(ctx context.Context, code string) (string, error) {
	token, err := c.GetAccessToken(ctx)
	if err != nil {
		return "", err
	}

	endpoint := c.apiURL("/cgi-bin/auth/getuserinfo", url.Values{
		"access_token": {token},
		"code":         {code},
	})

	body, err := c.get(ctx, endpoint)
	if err != nil {
		return "", fmt.Errorf("获取用户信息失败: %w", err)
	}

	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		UserID  string `json:"userid"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if result.ErrCode != 0 {
		return "", fmt.Errorf("企业微信返回错误: [%d] %s", result.ErrCode, result.ErrMsg)
	}

	return result.UserID, nil
}

// SendText 发送文本消息
func (c *client) SendText(ctx context.Context, req *SendTextRequest) error {
	token, err := c.GetAccessToken(ctx)
	if err != nil {
		return err
	}

	endpoint := c.apiURL("/cgi-bin/message/send", url.Values{"access_token": {token}})

	agentID := req.AgentID
	if agentID == 0 {
		agentID = c.getConfig(ctx).AgentID
	}

	// 构造请求体
	body := map[string]interface{}{
		"touser":  req.ToUser,
		"msgtype": "text",
		"agentid": agentID,
		"text": map[string]string{
			"content": req.Content,
		},
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}

	httpResp, err := c.postJSON(ctx, endpoint, bodyBytes)
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("解析响应失败: %w", err)
	}

	if result.ErrCode != 0 {
		return fmt.Errorf("企业微信返回错误: [%d] %s", result.ErrCode, result.ErrMsg)
	}

	return nil
}

// buildTemplateCard 构造模板卡片数据
func buildTemplateCard(taskID string, content *types.WeComCardContent) map[string]interface{} {
	card := map[string]interface{}{
		"card_type": "button_interaction",
		"main_title": map[string]string{
			"title": content.MainTitle,
			"desc":  content.MainDesc,
		},
	}
	if taskID != "" {
		card["task_id"] = taskID
	}

	// 内容项
	if len(content.ContentItems) > 0 {
		items := make([]map[string]string, 0, len(content.ContentItems))
		for _, item := range content.ContentItems {
			keyName := strings.TrimSpace(item.KeyName)
			value := strings.TrimSpace(item.Value)
			if keyName == "" || value == "" {
				continue
			}
			items = append(items, map[string]string{
				"keyname": truncateRunes(keyName, 10),
				"value":   truncateRunes(value, 30),
			})
		}
		if len(items) > 0 {
			card["horizontal_content_list"] = items
		}
	}

	// 按钮
	if len(content.Buttons) > 0 {
		buttons := make([]map[string]interface{}, len(content.Buttons))
		for i, btn := range content.Buttons {
			button := map[string]interface{}{
				"text":  btn.Text,
				"style": btn.Style,
			}
			if btn.Type == 1 {
				button["type"] = 1
				button["url"] = btn.URL
			} else {
				button["type"] = 0
				button["key"] = btn.Key
			}
			buttons[i] = button
		}
		card["button_list"] = buttons
	}

	// 跳转链接
	if content.ActionURL != "" {
		card["card_action"] = map[string]interface{}{
			"type": 1,
			"url":  content.ActionURL,
		}
	}

	return card
}

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

// buildTemplateCardUpdate 构造更新卡片数据（与发送略有不同）
func buildTemplateCardUpdate(content *types.WeComCardContent) map[string]interface{} {
	return buildTemplateCard("", content) // 更新时不需要 task_id
}

func (c *client) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("企业微信 API 请求地址无效")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("企业微信 API 请求失败")
	}
	defer resp.Body.Close()
	return readResponseBody(resp)
}

func (c *client) postJSON(ctx context.Context, url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("企业微信 API 请求地址无效")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("企业微信 API 请求失败")
	}
	return resp, nil
}

func readResponseBody(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP 状态码异常: %d", resp.StatusCode)
	}
	return body, nil
}

func normalizeAPIBaseURL(apiBaseURL string) string {
	normalized := strings.TrimRight(strings.TrimSpace(apiBaseURL), "/")
	if normalized == "" {
		return DefaultAPIBaseURL
	}
	return normalized
}

func (c *client) apiURL(endpointPath string, query url.Values) string {
	base, err := url.Parse(c.apiBaseURL)
	if err != nil {
		base, _ = url.Parse(DefaultAPIBaseURL)
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(endpointPath, "/")
	base.RawQuery = query.Encode()
	base.Fragment = ""
	return base.String()
}

func getLocalCredential(cache map[string]cachedCredential, key string) string {
	credential, ok := cache[key]
	if !ok || credential.value == "" || time.Now().After(credential.expiresAt) {
		return ""
	}
	return credential.value
}

func setLocalCredential(cache map[string]cachedCredential, key, value string, expiry time.Duration) {
	if value == "" || expiry <= 0 {
		return
	}
	cache[key] = cachedCredential{
		value:     value,
		expiresAt: time.Now().Add(expiry),
	}
}

func (c *client) getConfig(ctx context.Context) Config {
	if c.configProvider == nil {
		return Config{}
	}
	return c.configProvider.GetWeComConfig(ctx)
}

type staticConfigProvider struct {
	config Config
}

func (p staticConfigProvider) GetWeComConfig(ctx context.Context) Config {
	return p.config
}

func randomNonce() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func jsapiSignature(ticket, nonce string, timestamp int64, pageURL string) string {
	raw := fmt.Sprintf("jsapi_ticket=%s&noncestr=%s&timestamp=%d&url=%s", ticket, nonce, timestamp, pageURL)
	sum := sha1.Sum([]byte(raw))
	return fmt.Sprintf("%x", sum)
}
