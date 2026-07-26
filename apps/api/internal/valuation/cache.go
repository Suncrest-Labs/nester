package valuation

import (
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/portfolio"
)

// cacheEntry is a cached valuation with its computation time.
type cacheEntry struct {
	val      portfolio.Valuation
	computed time.Time
}

// Cache is a per-user valuation cache with a freshness TTL and explicit
// event-driven invalidation. Safe for concurrent use.
type Cache struct {
	mu    sync.RWMutex
	ttl   time.Duration
	items map[uuid.UUID]cacheEntry
	clock func() time.Time
}

// NewCache constructs a Cache. A non-positive ttl disables time-based expiry
// (entries live until explicitly invalidated).
func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		ttl:   ttl,
		items: map[uuid.UUID]cacheEntry{},
		clock: time.Now,
	}
}

// Get returns the cached valuation for userID when present and still fresh.
func (c *Cache) Get(userID uuid.UUID) (portfolio.Valuation, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.items[userID]
	if !ok {
		return portfolio.Valuation{}, false
	}
	if c.ttl > 0 && c.clock().Sub(e.computed) > c.ttl {
		return portfolio.Valuation{}, false
	}
	return e.val, true
}

// Set stores a valuation for userID.
func (c *Cache) Set(userID uuid.UUID, val portfolio.Valuation) {
	c.mu.Lock()
	c.items[userID] = cacheEntry{val: val, computed: c.clock()}
	c.mu.Unlock()
}

// Invalidate drops any cached valuation for userID. Called on events that change
// a user's positions (deposit/withdrawal confirmed, harvest, price refresh).
func (c *Cache) Invalidate(userID uuid.UUID) {
	c.mu.Lock()
	delete(c.items, userID)
	c.mu.Unlock()
}
