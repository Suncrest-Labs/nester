package middleware

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
)

// UserByIDLookup is the slice of the user repository the wallet resolver
// needs. Declaring it here rather than importing the concrete repository
// keeps the middleware package free of a database dependency.
type UserByIDLookup interface {
	GetByID(ctx context.Context, id uuid.UUID) (*user.User, error)
}

// CachedWalletResolver resolves a user's currently linked wallet, memoising
// the answer for a short TTL.
//
// The binding check runs on every authenticated request, and the underlying
// lookup is a full-row SELECT, so an uncached resolver would add a database
// round trip to the hot path. The TTL bounds how long a revoked binding can
// still be honoured; keep it short.
type CachedWalletResolver struct {
	users UserByIDLookup
	ttl   time.Duration

	mu      sync.RWMutex
	entries map[string]walletCacheEntry
	// now is swappable so tests can advance time without sleeping.
	now func() time.Time
}

type walletCacheEntry struct {
	wallet    string
	expiresAt time.Time
}

// NewCachedWalletResolver returns a resolver over users with the given TTL.
// A non-positive ttl disables caching.
func NewCachedWalletResolver(users UserByIDLookup, ttl time.Duration) *CachedWalletResolver {
	return &CachedWalletResolver{
		users:   users,
		ttl:     ttl,
		entries: make(map[string]walletCacheEntry),
		now:     time.Now,
	}
}

// WalletForUser reports the wallet address currently linked to userID.
//
// A user ID that does not parse as a UUID, or that has no row, yields
// ("", nil): there is no wallet of record to contradict the token, and the
// authenticator has already established the caller's identity.
func (c *CachedWalletResolver) WalletForUser(ctx context.Context, userID string) (string, error) {
	if c == nil || c.users == nil {
		return "", nil
	}

	id, err := uuid.Parse(userID)
	if err != nil {
		return "", nil
	}

	if wallet, ok := c.lookup(userID); ok {
		return wallet, nil
	}

	u, err := c.users.GetByID(ctx, id)
	if err != nil {
		// Surfaced to the caller, which fails the request closed rather than
		// assuming the token's wallet claim is still accurate.
		return "", err
	}
	wallet := ""
	if u != nil {
		wallet = u.WalletAddress
	}
	c.store(userID, wallet)
	return wallet, nil
}

// Invalidate drops any cached binding for userID, so the next request
// re-reads it. Call this wherever a user's wallet is changed.
func (c *CachedWalletResolver) Invalidate(userID string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	delete(c.entries, userID)
	c.mu.Unlock()
}

func (c *CachedWalletResolver) lookup(userID string) (string, bool) {
	if c.ttl <= 0 {
		return "", false
	}
	c.mu.RLock()
	entry, ok := c.entries[userID]
	c.mu.RUnlock()
	if !ok || c.now().After(entry.expiresAt) {
		return "", false
	}
	return entry.wallet, true
}

func (c *CachedWalletResolver) store(userID, wallet string) {
	if c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	c.entries[userID] = walletCacheEntry{wallet: wallet, expiresAt: c.now().Add(c.ttl)}
	c.mu.Unlock()
}
