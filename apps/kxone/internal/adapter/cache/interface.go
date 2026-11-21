package cache

import (
	"context"
	"time"
)

// Cache 缓存接口（用于 Redis）
type Cache interface {
	// Set 设置缓存
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error

	// Get 获取缓存
	Get(ctx context.Context, key string) (string, error)

	// SetNX 仅当 key 不存在时设置（用于幂等锁）
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error)

	// Del 删除缓存
	Del(ctx context.Context, keys ...string) error

	// Exists 检查 key 是否存在
	Exists(ctx context.Context, keys ...string) (int64, error)

	// Incr 自增计数（用于共享限流）
	Incr(ctx context.Context, key string) (int64, error)

	// Expire 设置过期时间
	Expire(ctx context.Context, key string, expiration time.Duration) error
}
