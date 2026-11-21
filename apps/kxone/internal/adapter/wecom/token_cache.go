package wecom

import (
	"context"
	"fmt"
	"time"

	"github.com/lsaac128/kxone/internal/adapter/cache"
)

// TokenCache access_token 缓存实现（基于 Redis）
type TokenCache struct {
	cache cache.Cache
}

// NewTokenCache 创建 token 缓存
func NewTokenCache(c cache.Cache) *TokenCache {
	return &TokenCache{
		cache: c,
	}
}

// Get 获取缓存的 token
func (tc *TokenCache) Get(ctx context.Context, corpID string) (string, error) {
	key := fmt.Sprintf("wecom:access_token:%s", corpID)
	return tc.cache.Get(ctx, key)
}

// Set 设置 token 缓存
func (tc *TokenCache) Set(ctx context.Context, corpID string, token string, expiry time.Duration) error {
	key := fmt.Sprintf("wecom:access_token:%s", corpID)
	return tc.cache.Set(ctx, key, token, expiry)
}

func (tc *TokenCache) GetJSAPITicket(ctx context.Context, corpID string) (string, error) {
	key := fmt.Sprintf("wecom:jsapi_ticket:%s", corpID)
	return tc.cache.Get(ctx, key)
}

func (tc *TokenCache) SetJSAPITicket(ctx context.Context, corpID string, ticket string, expiry time.Duration) error {
	key := fmt.Sprintf("wecom:jsapi_ticket:%s", corpID)
	return tc.cache.Set(ctx, key, ticket, expiry)
}
