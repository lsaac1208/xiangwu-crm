package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestAccountLoginCooldownEscalatesWithoutSlidingAndSuccessResets(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	server.SetTime(now)
	c := &RedisCache{client: redis.NewClient(&redis.Options{Addr: server.Addr()})}
	t.Cleanup(func() { c.Close() })
	ctx := context.Background()
	attempt := func(op string, want int64) {
		t.Helper()
		got, err := c.LoginAttempt(ctx, "login:account:test-hmac", op)
		if err != nil || got != want {
			t.Fatalf("%s got %d want %d err=%v", op, got, want, err)
		}
	}
	for i := 0; i < 9; i++ {
		attempt("failure", 0)
	}
	attempt("failure", 30)
	server.SetTime(now.Add(29 * time.Second))
	attempt("failure", 1)
	attempt("check", 1)
	server.SetTime(now.Add(30 * time.Second))
	attempt("check", 0)
	attempt("failure", 60)
	server.SetTime(now.Add(90 * time.Second))
	attempt("failure", 120)
	server.SetTime(now.Add(210 * time.Second))
	attempt("failure", 300)
	server.SetTime(now.Add(510 * time.Second))
	attempt("success", 0)
	attempt("check", 0)
	attempt("failure", 0)
	if _, err := c.LoginAttempt(ctx, "login:account:other-hmac", "check"); err != nil {
		t.Fatal(err)
	}
}
