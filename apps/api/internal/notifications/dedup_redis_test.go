package notifications

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func newTestRedisClientForDedup(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set; skipping redis-backed dedup test")
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

func TestRedisDeduplicator_FirstCallNotSeenSecondCallSeen(t *testing.T) {
	rc := newTestRedisClientForDedup(t)
	dedup := NewRedisDeduplicator(rc)
	key := "it-dedup-" + t.Name()
	t.Cleanup(func() { rc.Del(context.Background(), "notif:dedup:"+key) })

	seen, err := dedup.SeenRecently(context.Background(), key, time.Minute)
	if err != nil {
		t.Fatalf("first SeenRecently: %v", err)
	}
	if seen {
		t.Fatal("expected first call to report not-seen")
	}

	seen, err = dedup.SeenRecently(context.Background(), key, time.Minute)
	if err != nil {
		t.Fatalf("second SeenRecently: %v", err)
	}
	if !seen {
		t.Fatal("expected second call within the window to report seen")
	}
}

func TestRedisDeduplicator_ExpiresAfterWindow(t *testing.T) {
	rc := newTestRedisClientForDedup(t)
	dedup := NewRedisDeduplicator(rc)
	key := "it-dedup-expiry-" + t.Name()
	t.Cleanup(func() { rc.Del(context.Background(), "notif:dedup:"+key) })

	if _, err := dedup.SeenRecently(context.Background(), key, 500*time.Millisecond); err != nil {
		t.Fatalf("first SeenRecently: %v", err)
	}
	time.Sleep(700 * time.Millisecond)

	seen, err := dedup.SeenRecently(context.Background(), key, time.Minute)
	if err != nil {
		t.Fatalf("SeenRecently after expiry: %v", err)
	}
	if seen {
		t.Fatal("expected dedup entry to have expired, but it was still reported as seen")
	}
}

func TestRedisDeduplicator_DifferentKeysIndependent(t *testing.T) {
	rc := newTestRedisClientForDedup(t)
	dedup := NewRedisDeduplicator(rc)
	keyA := "it-dedup-a-" + t.Name()
	keyB := "it-dedup-b-" + t.Name()
	t.Cleanup(func() {
		rc.Del(context.Background(), "notif:dedup:"+keyA, "notif:dedup:"+keyB)
	})

	if _, err := dedup.SeenRecently(context.Background(), keyA, time.Minute); err != nil {
		t.Fatalf("keyA SeenRecently: %v", err)
	}
	seen, err := dedup.SeenRecently(context.Background(), keyB, time.Minute)
	if err != nil {
		t.Fatalf("keyB SeenRecently: %v", err)
	}
	if seen {
		t.Fatal("expected an unrelated key to be independent, not affected by keyA's dedup entry")
	}
}
