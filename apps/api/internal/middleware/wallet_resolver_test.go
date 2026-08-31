package middleware

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
)

// countingUserLookup records how many times the database was consulted, so
// the cache can be observed rather than assumed.
type countingUserLookup struct {
	wallet string
	err    error
	calls  int
}

func (c *countingUserLookup) GetByID(context.Context, uuid.UUID) (*user.User, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	return &user.User{WalletAddress: c.wallet}, nil
}

func TestCachedWalletResolver_ReturnsWallet(t *testing.T) {
	lookup := &countingUserLookup{wallet: walletA}
	r := NewCachedWalletResolver(lookup, time.Minute)

	got, err := r.WalletForUser(context.Background(), uuid.NewString())
	if err != nil {
		t.Fatalf("WalletForUser: unexpected error %v", err)
	}
	if got != walletA {
		t.Fatalf("wallet = %q, want %q", got, walletA)
	}
}

func TestCachedWalletResolver_CachesWithinTTL(t *testing.T) {
	lookup := &countingUserLookup{wallet: walletA}
	r := NewCachedWalletResolver(lookup, time.Minute)
	id := uuid.NewString()

	for i := range 3 {
		if _, err := r.WalletForUser(context.Background(), id); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if lookup.calls != 1 {
		t.Fatalf("lookups = %d, want 1 (subsequent reads should be cached)", lookup.calls)
	}
}

func TestCachedWalletResolver_RefetchesAfterTTL(t *testing.T) {
	lookup := &countingUserLookup{wallet: walletA}
	r := NewCachedWalletResolver(lookup, time.Minute)

	now := time.Now()
	r.now = func() time.Time { return now }
	id := uuid.NewString()

	if _, err := r.WalletForUser(context.Background(), id); err != nil {
		t.Fatalf("first call: %v", err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := r.WalletForUser(context.Background(), id); err != nil {
		t.Fatalf("post-expiry call: %v", err)
	}
	if lookup.calls != 2 {
		t.Fatalf("lookups = %d, want 2 (entry should expire)", lookup.calls)
	}
}

// A relink must become visible once the cached entry expires, otherwise the
// invalidation guarantee is only as good as a stale cache.
func TestCachedWalletResolver_SeesRelinkAfterTTL(t *testing.T) {
	lookup := &countingUserLookup{wallet: walletA}
	r := NewCachedWalletResolver(lookup, time.Minute)

	now := time.Now()
	r.now = func() time.Time { return now }
	id := uuid.NewString()

	if _, err := r.WalletForUser(context.Background(), id); err != nil {
		t.Fatalf("first call: %v", err)
	}
	lookup.wallet = walletB
	now = now.Add(2 * time.Minute)

	got, err := r.WalletForUser(context.Background(), id)
	if err != nil {
		t.Fatalf("post-relink call: %v", err)
	}
	if got != walletB {
		t.Fatalf("wallet = %q, want %q after relink", got, walletB)
	}
}

func TestCachedWalletResolver_InvalidateForcesRefetch(t *testing.T) {
	lookup := &countingUserLookup{wallet: walletA}
	r := NewCachedWalletResolver(lookup, time.Minute)
	id := uuid.NewString()

	if _, err := r.WalletForUser(context.Background(), id); err != nil {
		t.Fatalf("first call: %v", err)
	}
	lookup.wallet = walletB
	r.Invalidate(id)

	got, err := r.WalletForUser(context.Background(), id)
	if err != nil {
		t.Fatalf("post-invalidate call: %v", err)
	}
	if got != walletB {
		t.Fatalf("wallet = %q, want %q after invalidate", got, walletB)
	}
	if lookup.calls != 2 {
		t.Fatalf("lookups = %d, want 2", lookup.calls)
	}
}

func TestCachedWalletResolver_PropagatesError(t *testing.T) {
	lookup := &countingUserLookup{err: errors.New("db down")}
	r := NewCachedWalletResolver(lookup, time.Minute)

	if _, err := r.WalletForUser(context.Background(), uuid.NewString()); err == nil {
		t.Fatal("expected the lookup error to propagate so the request fails closed")
	}
}

// Errors must not be cached as a successful empty result, or one blip would
// disable the check for a whole TTL.
func TestCachedWalletResolver_DoesNotCacheErrors(t *testing.T) {
	lookup := &countingUserLookup{err: errors.New("db down")}
	r := NewCachedWalletResolver(lookup, time.Minute)
	id := uuid.NewString()

	_, _ = r.WalletForUser(context.Background(), id)
	_, _ = r.WalletForUser(context.Background(), id)

	if lookup.calls != 2 {
		t.Fatalf("lookups = %d, want 2 (errors must not be cached)", lookup.calls)
	}
}

// A non-UUID subject cannot address a row; it must not reach the database.
func TestCachedWalletResolver_NonUUIDUserID(t *testing.T) {
	lookup := &countingUserLookup{wallet: walletA}
	r := NewCachedWalletResolver(lookup, time.Minute)

	got, err := r.WalletForUser(context.Background(), "user-owner")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if got != "" {
		t.Fatalf("wallet = %q, want \"\"", got)
	}
	if lookup.calls != 0 {
		t.Fatalf("lookups = %d, want 0", lookup.calls)
	}
}

func TestCachedWalletResolver_ZeroTTLDisablesCache(t *testing.T) {
	lookup := &countingUserLookup{wallet: walletA}
	r := NewCachedWalletResolver(lookup, 0)
	id := uuid.NewString()

	_, _ = r.WalletForUser(context.Background(), id)
	_, _ = r.WalletForUser(context.Background(), id)

	if lookup.calls != 2 {
		t.Fatalf("lookups = %d, want 2 (ttl<=0 disables caching)", lookup.calls)
	}
}

// A nil resolver is the "check disabled" configuration and must be safe.
func TestCachedWalletResolver_NilSafe(t *testing.T) {
	var r *CachedWalletResolver
	got, err := r.WalletForUser(context.Background(), uuid.NewString())
	if err != nil || got != "" {
		t.Fatalf("nil resolver: got (%q, %v), want (\"\", nil)", got, err)
	}
	r.Invalidate("anything")
}
