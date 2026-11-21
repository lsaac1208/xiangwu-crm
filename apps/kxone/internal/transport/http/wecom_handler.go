package http

import (
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lsaac128/kxone/internal/adapter/wecom"
)

// WeComHandler 企业微信回调处理器
type WeComHandler struct {
	wecomClient     wecom.Client
	callbackHandler *CallbackHandler
}

func NewWeComHandler(wecomClient wecom.Client) *WeComHandler {
	return &WeComHandler{
		wecomClient: wecomClient,
	}
}

func (h *WeComHandler) WithCallbackHandler(callbackHandler *CallbackHandler) *WeComHandler {
	h.callbackHandler = callbackHandler
	return h
}

// GetJSAPIConfig 返回企业微信 JSSDK 初始化配置。
func (h *WeComHandler) GetJSAPIConfig(c *gin.Context) {
	pageURL := c.Query("url")
	if pageURL == "" {
		pageURL = c.GetHeader("Referer")
	}
	pageURL = strings.TrimSpace(pageURL)
	if pageURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing_url"})
		return
	}

	config, err := h.wecomClient.GetJSAPIConfig(c.Request.Context(), pageURL)
	if err != nil {
		respondInternalError(c, "get_jsapi_config_failed", err)
		return
	}

	c.JSON(http.StatusOK, config)
}

// VerifyCallback 处理企业微信回调 URL 验证（GET 请求）
func (h *WeComHandler) VerifyCallback(c *gin.Context) {
	msgSignature := c.Query("msg_signature")
	timestamp := c.Query("timestamp")
	nonce := c.Query("nonce")
	echoStr := c.Query("echostr")

	if msgSignature == "" || timestamp == "" || nonce == "" || echoStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing_parameters"})
		return
	}

	// 解密验证
	plainText, err := h.wecomClient.VerifyCallbackURL(msgSignature, timestamp, nonce, echoStr)
	if err != nil {
		respondError(c, http.StatusBadRequest, "verification_failed", err)
		return
	}

	// 返回明文 echostr
	c.String(http.StatusOK, plainText)
}

// HandleCallback 处理企业微信事件回调（POST 请求）
func (h *WeComHandler) HandleCallback(c *gin.Context) {
	msgSignature := c.Query("msg_signature")
	timestamp := c.Query("timestamp")
	nonce := c.Query("nonce")

	if msgSignature == "" || timestamp == "" || nonce == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing_parameters"})
		return
	}

	// 读取加密消息
	encryptedData, err := io.ReadAll(c.Request.Body)
	if err != nil {
		if IsRequestBodyTooLarge(err) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request_body_too_large"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "read_body_failed"})
		return
	}

	// 解密消息
	plainText, err := h.wecomClient.DecryptCallback(msgSignature, timestamp, nonce, encryptedData)
	if err != nil {
		respondError(c, http.StatusBadRequest, "decrypt_failed", err)
		return
	}

	if h.callbackHandler != nil {
		h.callbackHandler.HandleCallback(c, plainText)
		return
	}

	log.Printf("企业微信回调处理器未注入，已拒绝回调")
	c.JSON(http.StatusInternalServerError, gin.H{"error": "callback_handler_missing"})
}
