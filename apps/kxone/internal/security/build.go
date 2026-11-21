package security

import (
	"fmt"
	"log/slog"
)

// BuildFieldProtector 根据环境和密钥创建字段加密器。
// 生产环境（env == "prod"）要求密钥必须存在；非生产环境密钥为空时降级为明文存储并打印警告。
func BuildFieldProtector(env, key string) (FieldProtector, error) {
	if key == "" {
		if env == "prod" {
			return nil, fmt.Errorf("FIELD_ENCRYPTION_KEY is required in prod")
		}
		slog.Warn("FIELD_ENCRYPTION_KEY is not configured; sensitive fields are not encrypted")
		return nil, nil
	}
	return NewAESGCMFieldProtector(key)
}

// BuildMobileHasher 根据密钥创建手机号 HMAC 哈希器。
// 密钥为空时返回 nil，此时手机号精确搜索索引不可用（全文模糊匹配降级）。
func BuildMobileHasher(key string) (MobileHasher, error) {
	if key == "" {
		return nil, nil
	}
	return NewHMACMobileHasher(key)
}
