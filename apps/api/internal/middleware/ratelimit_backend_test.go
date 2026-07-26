package middleware

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// TestRedisLimiterDistributed exercises the Redis-backed limiter against a real
// Redis. It is skipped unless REDIS_ADDR is set and reachable, so it runs in CI
// (where Redis is available) but never blocks local runs without one.
func TestRedisLimiterDistributed(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set; skipping distributed rate-limit test")
	}

	rc := redis.NewClient(&redis.Options{Addr: addr})
	ctx := context.Background()
	if err := rc.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not reachable at %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = rc.Close() })

	prefix := "test-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	key := "1.2.3.4"
	t.Cleanup(func() { _ = rc.Del(ctx, "rl:"+prefix+":"+key).Err() })

	l := NewLimiter(rc, prefix, 2, time.Second)

	// Two requests allowed, third denied with a positive retry-after.
	for i := 0; i < 2; i++ {
		if allowed, _ := l.Allow(ctx, key); !allowed {
			t.Fatalf("request %d: got denied, want allowed", i+1)
		}
	}
	allowed, wait := l.Allow(ctx, key)
	if allowed {
		t.Fatal("third request: got allowed, want denied")
	}
	if wait <= 0 {
		t.Fatalf("retryAfter = %s, want > 0", wait)
	}

	// After the window the counter expires and requests are allowed again.
	time.Sleep(1100 * time.Millisecond)
	if allowed, _ := l.Allow(ctx, key); !allowed {
		t.Fatal("after window reset: got denied, want allowed")
	}
}

// TestRedisLimiterFailsOpenOnError verifies that a Redis outage never blocks
// traffic: when the client cannot reach Redis, Allow returns allowed=true.
func TestRedisLimiterFailsOpenOnError(t *testing.T) {
	rc := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1", // nothing listening here
		DialTimeout: 200 * time.Millisecond,
		MaxRetries:  -1,
	})
	t.Cleanup(func() { _ = rc.Close() })

	l := &redisLimiter{rc: rc, prefix: "x", limit: 1, window: time.Second}
	allowed, wait := l.Allow(context.Background(), "k")
	if !allowed || wait != 0 {
		t.Fatalf("Allow on unreachable redis = (%v, %s), want (true, 0) — must fail open", allowed, wait)
	}
}
