package notifications

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisDedupOpTimeout bounds each Redis round trip so a degraded Redis
// never adds unbounded latency to a Send call.
const redisDedupOpTimeout = 150 * time.Millisecond

// RedisDeduplicator is a cross-instance Deduplicator backed by Redis
// SET-NX-EX: the first caller for a key within the window gets seen=false
// ("go ahead and send"); every other caller within the same window gets
// seen=true. Namespaced under "notif:dedup:" so it can't collide with
// unrelated keys on a shared Redis instance.
type RedisDeduplicator struct {
	rc *redis.Client
}

// NewRedisDeduplicator constructs a RedisDeduplicator. rc must not be nil —
// callers with no Redis configured should use InMemoryDeduplicator instead.
func NewRedisDeduplicator(rc *redis.Client) *RedisDeduplicator {
	return &RedisDeduplicator{rc: rc}
}

// SeenRecently implements Deduplicator. A Redis error fails open (returns
// seen=false, i.e. "not deduped, go ahead and send") — a degraded Redis
// must never silently drop a legitimate notification because dedup
// couldn't be checked, mirroring the fail-open philosophy already used by
// internal/middleware's rate limiter and internal/cache.
func (r *RedisDeduplicator) SeenRecently(ctx context.Context, key string, window time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, redisDedupOpTimeout)
	defer cancel()

	wasNewlySet, err := r.rc.SetNX(ctx, "notif:dedup:"+key, "1", window).Result()
	if err != nil {
		return false, err
	}
	return !wasNewlySet, nil
}
