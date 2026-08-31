package cache_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/suncrestlabs/nester/apps/api/internal/cache"
)

// ---------------------------------------------------------------------------
// No-Redis (nil client) tests: in-process behavior only, no external deps.
// ---------------------------------------------------------------------------

func TestGetOrCompute_NoRedis_CallsComputeEveryTimeButSingleFlightsConcurrent(t *testing.T) {
	c := cache.New(nil, nil)
	ctx := context.Background()

	var calls atomic.Int64
	compute := func(context.Context) (int, error) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond) // widen the window so concurrent callers overlap
		return 42, nil
	}

	const n = 20
	var wg sync.WaitGroup
	results := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, err := cache.GetOrCompute(ctx, c, cache.Options{
				Namespace: "test", ID: "same-key", HardTTL: time.Minute,
			}, compute)
			require.NoError(t, err)
			results[i] = v
		}(i)
	}
	wg.Wait()

	for _, r := range results {
		assert.Equal(t, 42, r)
	}
	// Without Redis, single-flight is still enforced in-process: all
	// concurrent callers for the same key collapse into one compute.
	assert.Equal(t, int64(1), calls.Load(), "expected exactly one compute call for concurrent same-key misses")
}

func TestGetOrCompute_NoRedis_PropagatesComputeError(t *testing.T) {
	c := cache.New(nil, nil)
	ctx := context.Background()
	wantErr := errors.New("boom")

	_, err := cache.GetOrCompute(ctx, c, cache.Options{
		Namespace: "test", ID: "err-key", HardTTL: time.Minute,
	}, func(context.Context) (int, error) {
		return 0, wantErr
	})
	assert.ErrorIs(t, err, wantErr)
}

func TestGetOrCompute_NoRedis_DifferentKeysComputeIndependently(t *testing.T) {
	c := cache.New(nil, nil)
	ctx := context.Background()
	var calls atomic.Int64

	compute := func(context.Context) (int, error) {
		return int(calls.Add(1)), nil
	}

	v1, err := cache.GetOrCompute(ctx, c, cache.Options{Namespace: "ns", ID: "a", HardTTL: time.Minute}, compute)
	require.NoError(t, err)
	v2, err := cache.GetOrCompute(ctx, c, cache.Options{Namespace: "ns", ID: "b", HardTTL: time.Minute}, compute)
	require.NoError(t, err)

	assert.NotEqual(t, v1, v2, "different keys must not share a single-flight group")
}

func TestCache_Invalidate_NoRedis_IsNoOp(t *testing.T) {
	c := cache.New(nil, nil)
	err := c.Invalidate(context.Background(), "ns", "id")
	assert.NoError(t, err)
}

func TestCache_Stats_TracksHitsAndMisses(t *testing.T) {
	c := cache.New(nil, nil)
	ctx := context.Background()

	_, err := cache.GetOrCompute(ctx, c, cache.Options{Namespace: "ns", ID: "x", HardTTL: time.Minute},
		func(context.Context) (int, error) { return 1, nil })
	require.NoError(t, err)

	stats := c.Stats()
	// Without Redis every call is a "miss" (there's no cache to hit).
	assert.GreaterOrEqual(t, stats.Misses, int64(1))
}

// ---------------------------------------------------------------------------
// Redis-backed tests: skip when REDIS_ADDR is unset/unreachable, matching
// the existing convention in internal/middleware/ratelimit_backend_test.go
// (no miniredis/redismock in go.mod).
// ---------------------------------------------------------------------------

func newTestRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set; skipping redis-backed cache test")
	}
	rc := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rc.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not reachable at %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = rc.Close() })
	return rc
}

type cachedThing struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestGetOrCompute_Redis_CachesAcrossCalls(t *testing.T) {
	rc := newTestRedisClient(t)
	c := cache.New(rc, nil)
	ctx := context.Background()
	ns, id := "it-cache", uniqueID(t)
	t.Cleanup(func() { _ = c.Invalidate(context.Background(), ns, id) })

	var calls atomic.Int64
	compute := func(context.Context) (cachedThing, error) {
		calls.Add(1)
		return cachedThing{Name: "hello", Count: int(calls.Load())}, nil
	}

	first, err := cache.GetOrCompute(ctx, c, cache.Options{Namespace: ns, ID: id, HardTTL: time.Minute}, compute)
	require.NoError(t, err)
	assert.Equal(t, cachedThing{Name: "hello", Count: 1}, first)

	second, err := cache.GetOrCompute(ctx, c, cache.Options{Namespace: ns, ID: id, HardTTL: time.Minute}, compute)
	require.NoError(t, err)
	assert.Equal(t, first, second, "second call should be served from cache, not recompute")
	assert.Equal(t, int64(1), calls.Load())
}

func TestGetOrCompute_Redis_ConcurrentMissesSuppressStampede(t *testing.T) {
	rc := newTestRedisClient(t)
	c := cache.New(rc, nil)
	ctx := context.Background()
	ns, id := "it-stampede", uniqueID(t)
	t.Cleanup(func() { _ = c.Invalidate(context.Background(), ns, id) })

	var calls atomic.Int64
	compute := func(context.Context) (int, error) {
		calls.Add(1)
		time.Sleep(50 * time.Millisecond)
		return 7, nil
	}

	const n = 15
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := cache.GetOrCompute(ctx, c, cache.Options{Namespace: ns, ID: id, HardTTL: time.Minute}, compute)
			assert.NoError(t, err)
			assert.Equal(t, 7, v)
		}()
	}
	wg.Wait()

	// In-process single-flight collapses same-instance concurrent callers to
	// exactly one compute regardless of Redis.
	assert.Equal(t, int64(1), calls.Load())
}

func TestCache_Invalidate_Redis_ClearsEntry(t *testing.T) {
	rc := newTestRedisClient(t)
	c := cache.New(rc, nil)
	ctx := context.Background()
	ns, id := "it-invalidate", uniqueID(t)

	var calls atomic.Int64
	compute := func(context.Context) (int, error) {
		return int(calls.Add(1)), nil
	}

	first, err := cache.GetOrCompute(ctx, c, cache.Options{Namespace: ns, ID: id, HardTTL: time.Minute}, compute)
	require.NoError(t, err)
	assert.Equal(t, 1, first)

	require.NoError(t, c.Invalidate(ctx, ns, id))

	second, err := cache.GetOrCompute(ctx, c, cache.Options{Namespace: ns, ID: id, HardTTL: time.Minute}, compute)
	require.NoError(t, err)
	assert.Equal(t, 2, second, "invalidated entry must recompute, not serve the old value")
}

func TestCache_Invalidate_Redis_DoesNotTouchOtherNamespace(t *testing.T) {
	rc := newTestRedisClient(t)
	c := cache.New(rc, nil)
	ctx := context.Background()
	id := uniqueID(t)
	t.Cleanup(func() {
		_ = c.Invalidate(context.Background(), "ns-a", id)
		_ = c.Invalidate(context.Background(), "ns-b", id)
	})

	compute := func(v int) func(context.Context) (int, error) {
		return func(context.Context) (int, error) { return v, nil }
	}

	_, err := cache.GetOrCompute(ctx, c, cache.Options{Namespace: "ns-a", ID: id, HardTTL: time.Minute}, compute(1))
	require.NoError(t, err)
	_, err = cache.GetOrCompute(ctx, c, cache.Options{Namespace: "ns-b", ID: id, HardTTL: time.Minute}, compute(2))
	require.NoError(t, err)

	require.NoError(t, c.Invalidate(ctx, "ns-a", id))

	// ns-b's entry (same id, different namespace) must survive.
	var calledB atomic.Bool
	v, err := cache.GetOrCompute(ctx, c, cache.Options{Namespace: "ns-b", ID: id, HardTTL: time.Minute},
		func(context.Context) (int, error) { calledB.Store(true); return 999, nil })
	require.NoError(t, err)
	assert.Equal(t, 2, v)
	assert.False(t, calledB.Load(), "ns-b entry should still be cached after invalidating ns-a")
}

func TestGetOrCompute_Redis_TTLIsJittered(t *testing.T) {
	rc := newTestRedisClient(t)
	c := cache.New(rc, nil)
	ctx := context.Background()
	ns := "it-jitter"

	// The jitter window is ±10% of the base, so TTLs are drawn from
	// [9s, 11s] at sub-second resolution. Sample with PTTL (milliseconds)
	// rather than TTL (whole seconds) — second-resolution rounding collapses
	// that window onto {9s, 10s, 11s} and makes identical samples common
	// enough to flake.
	const (
		base           = 10 * time.Second
		jitterFraction = 0.10
		samples        = 20
	)
	minTTL := time.Duration(float64(base) * (1 - jitterFraction))
	maxTTL := time.Duration(float64(base) * (1 + jitterFraction))

	seen := map[time.Duration]bool{}
	for i := 0; i < samples; i++ {
		id := uniqueID(t)
		_, err := cache.GetOrCompute(ctx, c, cache.Options{Namespace: ns, ID: id, HardTTL: base},
			func(context.Context) (int, error) { return 1, nil })
		require.NoError(t, err)

		ttl := rc.PTTL(ctx, "cache:v1:"+ns+":"+id).Val()
		_ = c.Invalidate(context.Background(), ns, id)

		// Every TTL must land inside the jitter window (allowing a small
		// slack for the round-trip elapsed between SET and PTTL).
		assert.GreaterOrEqual(t, ttl, minTTL-time.Second,
			"TTL %v below the jitter window [%v, %v]", ttl, minTTL, maxTTL)
		assert.LessOrEqual(t, ttl, maxTTL,
			"TTL %v above the jitter window [%v, %v]", ttl, minTTL, maxTTL)

		seen[ttl] = true
	}

	// A constant (unjittered) TTL yields a single distinct value. With 20
	// samples drawn from a continuous 2s-wide window at ms resolution, a
	// working jitter yields many; requiring well over half guards against a
	// hardcoded TTL without being sensitive to occasional ms-level collisions.
	assert.Greater(t, len(seen), samples/2,
		"expected jittered TTLs to vary across calls, got %d distinct values in %d samples: %v",
		len(seen), samples, seen)
}

func TestGetOrCompute_Redis_ServesStaleWhileRevalidating(t *testing.T) {
	rc := newTestRedisClient(t)
	c := cache.New(rc, nil)
	ctx := context.Background()
	ns, id := "it-swr", uniqueID(t)
	t.Cleanup(func() { _ = c.Invalidate(context.Background(), ns, id) })

	var calls atomic.Int64
	compute := func(context.Context) (int, error) {
		n := calls.Add(1)
		return int(n), nil
	}

	// First call: soft TTL already in the past relative to a near-zero value
	// forces every subsequent read into the "soft-expired" branch immediately.
	opts := cache.Options{Namespace: ns, ID: id, HardTTL: 5 * time.Second, SoftTTL: 1 * time.Millisecond}
	first, err := cache.GetOrCompute(ctx, c, opts, compute)
	require.NoError(t, err)
	assert.Equal(t, 1, first)

	time.Sleep(20 * time.Millisecond) // ensure we're past the 1ms soft TTL

	// This read should get the stale value (1) immediately, not block on a
	// synchronous recompute.
	second, err := cache.GetOrCompute(ctx, c, opts, compute)
	require.NoError(t, err)
	assert.Equal(t, 1, second, "expected the stale value to be served immediately")

	// The background refresh should complete shortly after.
	require.Eventually(t, func() bool {
		return calls.Load() >= 2
	}, 2*time.Second, 20*time.Millisecond, "expected a background refresh to have run")
}

func uniqueID(t *testing.T) string {
	t.Helper()
	return t.Name() + "-" + time.Now().Format("150405.000000000")
}
