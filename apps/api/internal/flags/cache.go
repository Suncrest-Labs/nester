package flags

import (
	"context"
	"sync"
	"time"
)

// defaultTTL bounds staleness even if an invalidation message is lost.
const defaultTTL = 30 * time.Second

// Cache is an in-process, TTL-bounded read-through cache in front of a
// Reader (normally the Postgres Store). Cross-instance freshness comes from
// pub/sub invalidation: when any instance changes a flag, every instance's
// Invalidate is called and the next read goes back to the source of truth.
// The short TTL is a backstop so a lost invalidation can only delay — never
// prevent — convergence.
type Cache struct {
	source Reader
	ttl    time.Duration

	mu      sync.RWMutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	flag    Flag
	err     error
	expires time.Time
}

// NewCache wraps source with an in-process cache. ttl <= 0 uses the default.
func NewCache(source Reader, ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	return &Cache{source: source, ttl: ttl, entries: make(map[string]cacheEntry)}
}

// Get returns the cached flag when fresh, otherwise reads through to the
// source. Not-found is cached too (negative caching) so a missing flag does
// not hammer the database on a hot path.
func (c *Cache) Get(ctx context.Context, name string) (Flag, error) {
	c.mu.RLock()
	e, ok := c.entries[name]
	c.mu.RUnlock()
	if ok && time.Now().Before(e.expires) {
		return e.flag, e.err
	}

	f, err := c.source.Get(ctx, name)
	// Only cache definitive answers. A transient store error must not be
	// cached: the next call should retry the source rather than serve the
	// error for a full TTL (and kill-switch fail-safe handling depends on
	// seeing the error, not a stale success).
	if err == nil || err == ErrNotFound {
		c.mu.Lock()
		c.entries[name] = cacheEntry{flag: f, err: err, expires: time.Now().Add(c.ttl)}
		c.mu.Unlock()
	}
	return f, err
}

// Invalidate drops one flag from the cache. Wire this to the Redis pub/sub
// subscription so a change on any instance propagates here within seconds.
func (c *Cache) Invalidate(name string) {
	c.mu.Lock()
	delete(c.entries, name)
	c.mu.Unlock()
}

// InvalidateAll drops every entry (e.g. on pub/sub reconnect, when messages
// may have been missed).
func (c *Cache) InvalidateAll() {
	c.mu.Lock()
	c.entries = make(map[string]cacheEntry)
	c.mu.Unlock()
}

// PubSub is the minimal subscription surface the cache needs for
// cross-instance invalidation. The Redis pub/sub used by the WebSocket
// fan-out satisfies this with a thin adapter.
type PubSub interface {
	// Subscribe delivers each message payload on channel to fn until ctx is
	// cancelled.
	Subscribe(ctx context.Context, channel string, fn func(payload string)) error
}

// InvalidationChannel is the pub/sub channel flag changes broadcast on.
const InvalidationChannel = "flags:invalidate"

// SubscribeInvalidations connects the cache to the cluster-wide invalidation
// channel. It blocks until ctx is cancelled; run it in a goroutine. On
// subscribe (and resubscribe) the whole cache is dropped so any missed
// messages cannot leave a stale entry behind.
func (c *Cache) SubscribeInvalidations(ctx context.Context, ps PubSub) error {
	c.InvalidateAll()
	return ps.Subscribe(ctx, InvalidationChannel, func(payload string) {
		if payload == "*" {
			c.InvalidateAll()
			return
		}
		c.Invalidate(payload)
	})
}
