package oracle

import (
	"sync"
	"time"
)

type cacheEntry struct {
	rate ExchangeRate
}

// RateCache is a thread-safe in-memory store for ExchangeRate values.
type RateCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
}

// NewRateCache returns an empty cache.
func NewRateCache() *RateCache {
	return &RateCache{entries: make(map[string]cacheEntry)}
}

func key(base, quote string) string { return base + "/" + quote }

// Get returns the cached rate for base→quote regardless of freshness.
// The second return value is false when no entry exists at all.
func (c *RateCache) Get(base, quote string) (ExchangeRate, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[key(base, quote)]
	return e.rate, ok
}

// Set stores or overwrites the rate for base→quote.
func (c *RateCache) Set(r ExchangeRate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key(r.Base, r.Quote)] = cacheEntry{rate: r}
}

// IsFresh reports whether the cached entry for base→quote exists and has not expired.
func (c *RateCache) IsFresh(base, quote string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[key(base, quote)]
	if !ok {
		return false
	}
	return time.Now().Before(e.rate.ExpiresAt)
}
