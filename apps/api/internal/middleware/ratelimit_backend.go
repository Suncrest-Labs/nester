package middleware

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisCallTimeout bounds each Redis round-trip so a degraded (slow) Redis
// cannot add latency to every request before the limiter fails open.
const redisCallTimeout = 75 * time.Millisecond

// Limiter decides whether a request identified by an opaque key may proceed.
// Implementations may be process-local (in-memory token bucket) or distributed
// (Redis fixed-window counter). A denied call returns an estimated wait before
// the next request for the same key will be allowed.
type Limiter interface {
	Allow(ctx context.Context, key string) (allowed bool, retryAfter time.Duration)
}

// Allow adapts the in-memory token-bucket limiter to the Limiter interface. The
// context is unused because the in-memory bucket never blocks or performs I/O.
func (l *limiter) Allow(_ context.Context, key string) (bool, time.Duration) {
	return l.allow(key)
}

// NewLimiter returns a distributed, Redis-backed limiter when rc is non-nil, and
// a process-local in-memory limiter otherwise. This is the dual-mode entry point:
// deployments with Redis get cross-instance rate limiting, while single-instance
// or Redis-less deployments fall back transparently.
//
// prefix namespaces the Redis keys ("rl:{prefix}:{key}") so several limiters can
// share one Redis instance without colliding.
func NewLimiter(rc *redis.Client, prefix string, limit int, window time.Duration) Limiter {
	if rc != nil {
		return &redisLimiter{rc: rc, prefix: prefix, limit: limit, window: window}
	}
	return newLimiter(limit, window)
}

// redisLimiter implements a fixed-window counter. Each request atomically
// increments a per-key counter; the counter's TTL is set to the window on first
// use and the request is rejected once the count exceeds the limit. Because the
// counter lives in Redis, the limit is enforced across every API instance.
type redisLimiter struct {
	rc     *redis.Client
	prefix string
	limit  int
	window time.Duration
}

// rateLimitScript atomically increments the counter at KEYS[1], sets its TTL to
// ARGV[1] milliseconds when the counter is first created, and returns the
// current count alongside the remaining TTL. Running it as a single script keeps
// the increment and expiry atomic under concurrency.
var rateLimitScript = redis.NewScript(`
local current = redis.call("INCR", KEYS[1])
if current == 1 then
	redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
local ttl = redis.call("PTTL", KEYS[1])
return {current, ttl}
`)

func (l *redisLimiter) Allow(ctx context.Context, key string) (bool, time.Duration) {
	fullKey := "rl:" + l.prefix + ":" + key

	ctx, cancel := context.WithTimeout(ctx, redisCallTimeout)
	defer cancel()

	res, err := rateLimitScript.Run(ctx, l.rc, []string{fullKey}, l.window.Milliseconds()).Result()
	if err != nil {
		// Fail open: a Redis outage (or a call that exceeds redisCallTimeout)
		// must never take down the API by rejecting legitimate traffic. Requests
		// pass through until Redis recovers; the failure is logged so a
		// persistent outage does not go unnoticed.
		slog.Default().WarnContext(ctx, "rate limiter: redis error, failing open",
			"prefix", l.prefix, "error", err)
		return true, 0
	}

	vals, ok := res.([]any)
	if !ok || len(vals) != 2 {
		slog.Default().WarnContext(ctx, "rate limiter: unexpected redis result, failing open",
			"prefix", l.prefix)
		return true, 0
	}
	count, _ := vals[0].(int64)
	ttlMS, _ := vals[1].(int64)

	if count > int64(l.limit) {
		wait := max(time.Duration(ttlMS)*time.Millisecond, time.Second)
		return false, wait
	}
	return true, 0
}
