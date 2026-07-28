package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RevocationCache answers "is this session currently revoked?" fast enough to
// consult on every authenticated request. Only revoked sessions ever get a
// key — absence of a key means "not revoked", so there is no need to
// positive-cache or warm the cache on session creation.
type RevocationCache interface {
	IsRevoked(ctx context.Context, sessionID string) (bool, error)
	MarkRevoked(ctx context.Context, sessionID string, ttl time.Duration) error
}

func revocationKey(sessionID string) string {
	return "auth:revoked:" + sessionID
}

// ── Redis implementation ──────────────────────────────────────────────────────

type RedisRevocationCache struct {
	client *redis.Client
}

func NewRedisRevocationCache(client *redis.Client) *RedisRevocationCache {
	return &RedisRevocationCache{client: client}
}

func (c *RedisRevocationCache) MarkRevoked(ctx context.Context, sessionID string, ttl time.Duration) error {
	if err := c.client.Set(ctx, revocationKey(sessionID), "1", ttl).Err(); err != nil {
		return fmt.Errorf("redis Set: %w", err)
	}
	return nil
}

func (c *RedisRevocationCache) IsRevoked(ctx context.Context, sessionID string) (bool, error) {
	n, err := c.client.Exists(ctx, revocationKey(sessionID)).Result()
	if err != nil {
		return false, fmt.Errorf("redis Exists: %w", err)
	}
	return n > 0, nil
}

// ── In-memory implementation (dev / single-instance fallback) ─────────────────

type InMemoryRevocationCache struct {
	mu sync.Mutex
	m  map[string]time.Time // sessionID -> expiry
}

func NewInMemoryRevocationCache() *InMemoryRevocationCache {
	return &InMemoryRevocationCache{m: make(map[string]time.Time)}
}

func (c *InMemoryRevocationCache) MarkRevoked(_ context.Context, sessionID string, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[sessionID] = time.Now().Add(ttl)
	return nil
}

func (c *InMemoryRevocationCache) IsRevoked(_ context.Context, sessionID string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	expiresAt, ok := c.m[sessionID]
	if !ok {
		return false, nil
	}
	if time.Now().After(expiresAt) {
		delete(c.m, sessionID)
		return false, nil
	}
	return true, nil
}
