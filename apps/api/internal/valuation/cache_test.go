package valuation

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/portfolio"
)

func TestCache_SetGetInvalidate(t *testing.T) {
	c := NewCache(time.Minute)
	uid := uuid.New()

	if _, ok := c.Get(uid); ok {
		t.Fatal("empty cache should miss")
	}
	c.Set(uid, portfolio.Valuation{UserID: uid})
	if _, ok := c.Get(uid); !ok {
		t.Fatal("expected hit after set")
	}
	c.Invalidate(uid)
	if _, ok := c.Get(uid); ok {
		t.Fatal("expected miss after invalidate")
	}
}

func TestCache_TTLExpiry(t *testing.T) {
	c := NewCache(time.Minute)
	uid := uuid.New()
	now := time.Now()
	c.clock = func() time.Time { return now }
	c.Set(uid, portfolio.Valuation{UserID: uid})

	if _, ok := c.Get(uid); !ok {
		t.Fatal("expected hit within ttl")
	}
	now = now.Add(2 * time.Minute) // advance past ttl
	if _, ok := c.Get(uid); ok {
		t.Fatal("expected miss after ttl expiry")
	}
}
