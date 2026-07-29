// Package cache provides a typed GetOrCompute cache backed by Redis with an
// in-memory (direct-compute) fallback, single-flight recomputation, jittered
// TTLs, serve-stale-while-revalidate, and namespace-scoped invalidation
// (nester#827).
//
// Single-flight is enforced at two levels:
//   - In-process: golang.org/x/sync/singleflight collapses concurrent callers
//     on the same instance into one compute, regardless of whether Redis is
//     configured. This is a hard guarantee.
//   - Cross-process: a short-lived Redis lock (SET NX EX) is best-effort. If
//     an instance can't acquire it, it waits briefly for the winning
//     instance's result to land in cache; if it still hasn't after that,
//     it computes locally rather than blocking indefinitely. Under high
//     cross-instance contention this can mean an occasional duplicate
//     compute — an availability/latency tradeoff, not a correctness bug (the
//     in-process guarantee still holds).
//
// Degradation: any Redis error (including the client being nil, i.e. no
// REDIS_ADDR configured — see cmd/api/main.go's redisClient wiring) falls
// through to calling the compute function directly. A Redis outage makes the
// app slower, never broken.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

// schemaVersion is embedded in every cache key. Bump it when a cached value's
// shape changes so old, incompatible entries are never deserialized as new
// ones — they simply miss under the new key prefix.
const schemaVersion = "v1"

// jitterFraction is how far TTLs are randomly shifted (± this fraction of the
// base TTL) so a batch of keys populated together does not all expire in the
// same instant.
const jitterFraction = 0.10

// crossProcessLockTTL bounds how long one instance's "I'm computing this"
// claim is honored by others before it's considered stale (e.g. the compute
// crashed mid-flight) and up for grabs again.
const crossProcessLockTTL = 5 * time.Second

// crossProcessWait is how long a losing instance waits for the winner's
// result to land in cache before giving up and computing locally itself.
const crossProcessWait = 150 * time.Millisecond

// redisOpTimeout bounds every individual Redis round trip, mirroring
// middleware.redisCallTimeout's rationale: a degraded Redis must never add
// unbounded latency before the cache falls through to direct compute.
const redisOpTimeout = 200 * time.Millisecond

// backgroundRefreshTimeout bounds a stale-while-revalidate refresh kicked off
// after the calling request has already been served its stale value.
const backgroundRefreshTimeout = 10 * time.Second

// Stats is a point-in-time snapshot of cache activity, exposed for a metrics
// endpoint to scrape. Counters are cumulative since process start.
type Stats struct {
	Hits               int64
	Misses             int64
	StampedeSuppressed int64
	RedisErrors        int64
}

// Cache is a typed GetOrCompute layer over an optional shared Redis client.
// Construct one with New and reuse it — do not create a Cache per call site.
type Cache struct {
	redis  *redis.Client
	logger *slog.Logger
	sf     singleflight.Group

	hits               atomic.Int64
	misses             atomic.Int64
	stampedeSuppressed atomic.Int64
	redisErrors        atomic.Int64
}

// New constructs a Cache. Pass the shared *redis.Client used elsewhere in the
// app (see cmd/api/main.go's redisClient) — never open a second connection
// pool. rc may be nil (no Redis configured); the cache still works, providing
// only in-process single-flight and no cross-instance sharing.
func New(rc *redis.Client, logger *slog.Logger) *Cache {
	if logger == nil {
		logger = slog.Default()
	}
	return &Cache{redis: rc, logger: logger}
}

// Stats returns a snapshot of cumulative hit/miss/suppression/error counts.
func (c *Cache) Stats() Stats {
	return Stats{
		Hits:               c.hits.Load(),
		Misses:             c.misses.Load(),
		StampedeSuppressed: c.stampedeSuppressed.Load(),
		RedisErrors:        c.redisErrors.Load(),
	}
}

// Invalidate deletes the cached entry for namespace/id, if any. Scoped to a
// single key — clearing an entire namespace at once is intentionally not
// supported (Redis has no atomic "delete by prefix"; a SCAN-based bulk clear
// would need to be its own explicitly-opt-in operation, not the default
// invalidation path, to avoid an accidental full-namespace wipe under load).
func (c *Cache) Invalidate(ctx context.Context, namespace, id string) error {
	if c.redis == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, redisOpTimeout)
	defer cancel()
	if err := c.redis.Del(ctx, buildKey(namespace, id)).Err(); err != nil {
		c.redisErrors.Add(1)
		return fmt.Errorf("cache: invalidate %s/%s: %w", namespace, id, err)
	}
	return nil
}

func buildKey(namespace, id string) string {
	return fmt.Sprintf("cache:%s:%s:%s", schemaVersion, namespace, id)
}

// envelope is the JSON shape stored in Redis. ComputedAt/SoftExpiresAt let a
// reader decide whether to serve-stale-and-revalidate vs. block-and-recompute
// without a second round trip.
type envelope[T any] struct {
	Value         T         `json:"value"`
	ComputedAt    time.Time `json:"computed_at"`
	SoftExpiresAt time.Time `json:"soft_expires_at"`
	HardExpiresAt time.Time `json:"hard_expires_at"`
}

// Options configures one GetOrCompute call.
type Options struct {
	// Namespace groups related keys (e.g. "vault", "apy", "oracle-rate") so
	// Invalidate can target one without risk of touching another namespace's
	// keys — the namespace is part of the key, not a separate index.
	Namespace string
	// ID identifies the specific entry within Namespace (e.g. a vault ID).
	ID string
	// HardTTL is the absolute point past which a cached value is never
	// served, even stale — GetOrCompute blocks on a fresh compute.
	HardTTL time.Duration
	// SoftTTL, if set and less than HardTTL, enables serve-stale-while-
	// revalidate: once passed, cached reads return immediately with the
	// stale value while a background refresh runs. Zero (or >= HardTTL)
	// disables this — every read past HardTTL blocks on recompute, with no
	// stale window at all.
	SoftTTL time.Duration
}

func (o Options) key() string { return buildKey(o.Namespace, o.ID) }

func jitteredTTL(base time.Duration) time.Duration {
	if base <= 0 {
		return base
	}
	spread := float64(base) * jitterFraction
	offset := time.Duration(rand.Float64()*2*spread - spread) // [-spread, +spread]
	return base + offset
}

func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, redisOpTimeout)
}

// GetOrCompute returns the cached value for opts.Namespace/opts.ID, computing
// it via compute on a miss or hard-expiry. See the package doc for the
// single-flight and degradation guarantees.
//
// Go methods cannot introduce their own type parameters, so this is a free
// function taking *Cache rather than a generic method on Cache — the
// standard shape for a typed cache over a non-generic client in Go.
func GetOrCompute[T any](ctx context.Context, c *Cache, opts Options, compute func(ctx context.Context) (T, error)) (T, error) {
	key := opts.key()

	// singleflight.Group predates generics, so its Do callback is boxed to
	// `(any, error)`; getOrComputeTyped does the real, correctly-typed work
	// and its result is unboxed back to T immediately below.
	result, err, shared := c.sf.Do(key, func() (any, error) {
		return getOrComputeTyped(ctx, c, key, opts, compute)
	})
	if shared {
		c.stampedeSuppressed.Add(1)
	}
	if err != nil {
		var zero T
		return zero, err
	}
	return result.(T), nil
}

func getOrComputeTyped[T any](ctx context.Context, c *Cache, key string, opts Options, compute func(context.Context) (T, error)) (T, error) {
	var zero T

	if c.redis != nil {
		if env, ok := readEnvelope[T](ctx, c, key); ok {
			c.hits.Add(1)
			if time.Now().After(env.SoftExpiresAt) {
				refreshInBackground(c, key, opts, compute)
			}
			return env.Value, nil
		}
	}

	if c.redis == nil {
		v, err := compute(ctx)
		if err != nil {
			return zero, err
		}
		c.misses.Add(1)
		return v, nil
	}

	lockKey := "lock:" + key
	lockCtx, cancel := withTimeout(ctx)
	acquired, lockErr := c.redis.SetNX(lockCtx, lockKey, "1", crossProcessLockTTL).Result()
	cancel()

	if lockErr != nil {
		c.redisErrors.Add(1)
		// Redis is failing right now: degrade to direct compute rather than
		// erroring the caller.
		v, err := compute(ctx)
		if err != nil {
			return zero, err
		}
		c.misses.Add(1)
		return v, nil
	}

	if !acquired {
		// Another instance is already computing this key. Wait a short,
		// bounded window for its result rather than blocking indefinitely.
		time.Sleep(crossProcessWait)
		if env, ok := readEnvelope[T](ctx, c, key); ok {
			c.hits.Add(1)
			return env.Value, nil
		}
	}

	v, err := compute(ctx)
	if err != nil {
		return zero, err
	}
	c.misses.Add(1)
	c.writeBack(ctx, key, opts, v)
	return v, nil
}

// readEnvelope returns the cached envelope if present and not hard-expired.
// Genuinely stale (past soft, before hard) entries are still returned here —
// it's the caller's job to notice env.SoftExpiresAt has passed and trigger a
// background refresh; readEnvelope only filters out hard-expired misses.
func readEnvelope[T any](ctx context.Context, c *Cache, key string) (envelope[T], bool) {
	var zero envelope[T]

	getCtx, cancel := withTimeout(ctx)
	raw, err := c.redis.Get(getCtx, key).Bytes()
	cancel()
	if err != nil {
		if err != redis.Nil {
			c.redisErrors.Add(1)
		}
		return zero, false
	}

	var env envelope[T]
	if err := json.Unmarshal(raw, &env); err != nil {
		// Corrupt, or a schema-version collision that somehow still shares a
		// key — treat as a miss rather than erroring the caller.
		return zero, false
	}
	if time.Now().After(env.HardExpiresAt) {
		return zero, false
	}
	return env, true
}

func (c *Cache) writeBack(ctx context.Context, key string, opts Options, value any) {
	now := time.Now()
	hardTTL := jitteredTTL(opts.HardTTL)
	soft := opts.SoftTTL
	if soft <= 0 || soft > opts.HardTTL {
		soft = opts.HardTTL
	}

	env := map[string]any{
		"value":           value,
		"computed_at":     now,
		"soft_expires_at": now.Add(soft),
		"hard_expires_at": now.Add(hardTTL),
	}
	raw, err := json.Marshal(env)
	if err != nil {
		c.logger.WarnContext(ctx, "cache: failed to marshal value for write-back", "key", key, "error", err)
		return
	}

	setCtx, cancel := withTimeout(ctx)
	setErr := c.redis.Set(setCtx, key, raw, hardTTL).Err()
	cancel()
	if setErr != nil {
		c.redisErrors.Add(1)
		c.logger.WarnContext(ctx, "cache: redis write-back failed", "key", key, "error", setErr)
	}

	// Best-effort lock release so a fast recompute doesn't leave the next
	// caller waiting out the full crossProcessLockTTL for no reason.
	delCtx, cancel2 := withTimeout(ctx)
	_ = c.redis.Del(delCtx, "lock:"+key).Err()
	cancel2()
}

// refreshInBackground recomputes and writes back a soft-expired key without
// making the current caller wait. Uses its own bounded context, independent
// of the request that triggered it, since that request may return (and its
// context may be cancelled) before the refresh finishes.
func refreshInBackground[T any](c *Cache, key string, opts Options, compute func(context.Context) (T, error)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), backgroundRefreshTimeout)
		defer cancel()
		v, err := compute(ctx)
		if err != nil {
			c.logger.WarnContext(ctx, "cache: background refresh failed", "key", key, "error", err)
			return
		}
		c.writeBack(ctx, key, opts, v)
	}()
}
