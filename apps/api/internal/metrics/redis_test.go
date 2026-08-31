package metrics

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestNormalizeRedisCommandIsBounded(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"GET", "get"},
		{"get", "get"},
		{"SetEx", "setex"},
		{"EVALSHA", "evalsha"},
		{"NOTACOMMAND", redisCommandOther},
		{"", redisCommandOther},
		{"user:550e8400-e29b-41d4-a716-446655440000", redisCommandOther},
	}

	for _, tc := range cases {
		if got := normalizeRedisCommand(tc.in); got != tc.want {
			t.Errorf("normalizeRedisCommand(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestInstrumentRedisNilClientIsSafe(t *testing.T) {
	m := New()
	m.InstrumentRedis(nil) // must not panic or register anything

	if got := counterValue(t, m.Registry(), "nester_redis_commands_total", nil); got != 0 {
		t.Fatalf("expected no redis series for a nil client, got %v", got)
	}
}

// TestRedisNilIsNotAnError proves a cache miss does not inflate the error
// rate. Counting redis.Nil as a failure would make any alert on the error
// rate track cache misses instead of faults.
func TestRedisNilIsNotAnError(t *testing.T) {
	m := New()
	hook := &redisHook{m: m}

	hook.observe("get", time.Millisecond, redis.Nil)

	if got := counterValue(t, m.Registry(), "nester_redis_errors_total", map[string]string{
		"command": "get",
	}); got != 0 {
		t.Fatalf("redis.Nil was counted as an error: %v", got)
	}
	if got := counterValue(t, m.Registry(), "nester_redis_commands_total", map[string]string{
		"command": "get",
	}); got != 1 {
		t.Fatalf("expected the command itself to still be counted, got %v", got)
	}
}

func TestRedisErrorIsCounted(t *testing.T) {
	m := New()
	hook := &redisHook{m: m}

	hook.observe("set", 2*time.Millisecond, context.DeadlineExceeded)

	if got := counterValue(t, m.Registry(), "nester_redis_errors_total", map[string]string{
		"command": "set",
	}); got != 1 {
		t.Fatalf("expected one redis error counted, got %v", got)
	}
	if got := histogramCount(t, m.Registry(), "nester_redis_command_duration_seconds", map[string]string{
		"command": "set",
	}); got != 1 {
		t.Fatalf("expected a duration observation for the failed command, got %d", got)
	}
}

// TestRedisIntegration exercises the hook against a real Redis, covering
// command count, duration, errors, and pool statistics.
//
// Skips without REDIS_ADDR, following the convention in
// internal/middleware/ratelimit_backend_test.go. CI sets it.
func TestRedisIntegration(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set; skipping redis metrics integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()

	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("redis unreachable: %v", err)
	}

	m := New()
	m.InstrumentRedis(client)

	// A key carrying a user identifier, to prove keys never reach labels.
	const key = "metrics-test:user:550e8400-e29b-41d4-a716-446655440000"

	if err := client.Set(ctx, key, "value", time.Minute).Err(); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := client.Get(ctx, key).Err(); err != nil {
		t.Fatalf("get: %v", err)
	}
	// A miss: exercises the redis.Nil path against the real client.
	if err := client.Get(ctx, key+":absent").Err(); err != redis.Nil {
		t.Fatalf("expected redis.Nil for a missing key, got %v", err)
	}
	if err := client.Del(ctx, key).Err(); err != nil {
		t.Fatalf("del: %v", err)
	}

	if got := counterValue(t, m.Registry(), "nester_redis_commands_total", map[string]string{
		"command": "set",
	}); got < 1 {
		t.Errorf("expected SET to be counted, got %v", got)
	}
	if got := counterValue(t, m.Registry(), "nester_redis_commands_total", map[string]string{
		"command": "get",
	}); got < 2 {
		t.Errorf("expected both GETs to be counted, got %v", got)
	}
	if got := histogramCount(t, m.Registry(), "nester_redis_command_duration_seconds", map[string]string{
		"command": "set",
	}); got < 1 {
		t.Errorf("expected a duration observation for SET, got %d", got)
	}
	if got := counterValue(t, m.Registry(), "nester_redis_errors_total", map[string]string{
		"command": "get",
	}); got != 0 {
		t.Errorf("the cache miss was counted as an error: %v", got)
	}

	// Pool statistics must appear in the same scrape.
	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var sawPoolMetric bool
	for _, family := range families {
		if strings.HasPrefix(family.GetName(), "nester_redis_pool_") {
			sawPoolMetric = true
			break
		}
	}
	if !sawPoolMetric {
		t.Error("expected redis pool metrics in the exposition")
	}

	// The key embeds a user identifier; it must not be anywhere in the
	// metrics.
	for _, value := range allLabelValues(t, m.Registry()) {
		if strings.Contains(value, "550e8400") || strings.Contains(value, "metrics-test") {
			t.Fatalf("redis key material leaked into label %q", value)
		}
	}
}
