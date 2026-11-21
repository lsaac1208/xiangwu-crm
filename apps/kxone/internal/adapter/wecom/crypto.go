package wecom

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"
)

// VerifyCallbackURL 验证回调 URL（GET 请求）
func (c *client) VerifyCallbackURL(msgSignature, timestamp, nonce, echoStr string) (string, error) {
	cfg := c.getConfig(context.Background())
	// 验证签名
	expectedSignature := signature(cfg.Token, timestamp, nonce, echoStr)
	if expectedSignature != msgSignature {
		return "", fmt.Errorf("签名验证失败")
	}

	// 解密 echostr
	aesKey, err := base64.StdEncoding.DecodeString(cfg.EncodingAESKey + "=")
	if err != nil {
		return "", fmt.Errorf("解码 AES Key 失败: %w", err)
	}

	cipherText, err := base64.StdEncoding.DecodeString(echoStr)
	if err != nil {
		return "", fmt.Errorf("解码 echostr 失败: %w", err)
	}

	plainText, err := aesDecrypt(cipherText, aesKey)
	if err != nil {
		return "", fmt.Errorf("解密失败: %w", err)
	}

	result, err := extractCallbackPlaintext(plainText)
	if err != nil {
		return "", err
	}
	return string(result), nil
}

// DecryptCallback 解密回调消息（POST 请求）
func (c *client) DecryptCallback(msgSignature, timestamp, nonce string, encryptedData []byte) ([]byte, error) {
	cfg := c.getConfig(context.Background())
	// 解析 XML
	var envelope struct {
		ToUserName string `xml:"ToUserName"`
		Encrypt    string `xml:"Encrypt"`
	}

	if err := xml.Unmarshal(encryptedData, &envelope); err != nil {
		return nil, fmt.Errorf("解析 XML 失败: %w", err)
	}

	// 验证签名
	expectedSignature := signature(cfg.Token, timestamp, nonce, envelope.Encrypt)
	if expectedSignature != msgSignature {
		return nil, fmt.Errorf("签名验证失败")
	}

	// 解密消息
	aesKey, err := base64.StdEncoding.DecodeString(cfg.EncodingAESKey + "=")
	if err != nil {
		return nil, fmt.Errorf("解码 AES Key 失败: %w", err)
	}

	cipherText, err := base64.StdEncoding.DecodeString(envelope.Encrypt)
	if err != nil {
		return nil, fmt.Errorf("解码加密消息失败: %w", err)
	}

	plainText, err := aesDecrypt(cipherText, aesKey)
	if err != nil {
		return nil, fmt.Errorf("解密失败: %w", err)
	}

	return extractCallbackPlaintext(plainText)
}

// signature 计算签名
func signature(token, timestamp, nonce, encrypt string) string {
	strs := []string{token, timestamp, nonce, encrypt}
	sort.Strings(strs)

	h := sha1.New()
	h.Write([]byte(strings.Join(strs, "")))

	return fmt.Sprintf("%x", h.Sum(nil))
}

// aesDecrypt AES-256-CBC 解密
func aesDecrypt(cipherText, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	if len(cipherText) < aes.BlockSize {
		return nil, fmt.Errorf("密文太短")
	}
	if len(cipherText)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("密文长度不是块大小的整数倍")
	}
	if len(key) < aes.BlockSize {
		return nil, fmt.Errorf("AES Key 长度不足")
	}

	iv := key[:aes.BlockSize]
	mode := cipher.NewCBCDecrypter(block, iv)

	plainText := make([]byte, len(cipherText))
	mode.CryptBlocks(plainText, cipherText)

	// 去除 PKCS7 填充
	plainText = pkcs7Unpad(plainText)

	return plainText, nil
}

func extractCallbackPlaintext(plainText []byte) ([]byte, error) {
	if len(plainText) < 20 {
		return nil, fmt.Errorf("回调明文长度不足")
	}

	content := plainText[16:]
	length := int(binary.BigEndian.Uint32(content[:4]))
	if length < 0 || len(content) < 4+length {
		return nil, fmt.Errorf("回调明文长度无效")
	}

	return content[4 : 4+length], nil
}

// pkcs7Unpad 去除 PKCS7 填充
func pkcs7Unpad(data []byte) []byte {
	length := len(data)
	if length == 0 {
		return data
	}

	padding := int(data[length-1])
	if padding > length || padding > aes.BlockSize {
		return data
	}

	return data[:length-padding]
}
