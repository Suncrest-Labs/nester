package middleware

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/api"
)

// bucket is a token-bucket entry for a single rate-limit key.
type bucket struct {
	mu         sync.Mutex
	tokens     float64
	lastRefill time.Time
}

// limiter holds per-key token buckets.
type limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	limit   int           // Max tokens (i.e., max requests allowed within the window)
	window  time.Duration // Time duration over which 'limit' tokens are refilled
}

// newLimiter creates a new token bucket limiter with the given limit and window.
func newLimiter(limit int, window time.Duration) *limiter {
	return &limiter{
		buckets: make(map[string]*bucket),
		limit:   limit,
		window:  window,
	}
}

// allow consumes one token for the given key. It returns true if the request is
// allowed, otherwise it returns false along with an estimated duration until
// a token becomes available.
func (l *limiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	b, ok := l.buckets[key]
	if !ok {
		// First request for this key: create a new bucket.
		// We initialize it with 'limit - 1' tokens because the current request
		// immediately consumes one token, leaving 'limit - 1' for future requests
		// within the current window's "refill equivalent".
		b = &bucket{tokens: float64(l.limit - 1), lastRefill: time.Now()}
		l.buckets[key] = b
		l.mu.Unlock()
		return true, 0
	}
	l.mu.Unlock()

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastRefill)

	// Calculate how many tokens should have refilled based on elapsed time
	// Refill rate is 'limit' tokens over 'window' duration.
	refill := elapsed.Seconds() / l.window.Seconds() * float64(l.limit)

	// Add refilled tokens, ensuring it doesn't exceed the max limit
	b.tokens = min(float64(l.limit), b.tokens+refill)
	b.lastRefill = now

	// If there's at least one token, consume it and allow the request
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}

	// If no tokens are available, estimate the wait time until one token becomes available.
	// We need (1 - b.tokens) more tokens.
	// Rate is (limit / window.Seconds()) tokens per second.
	// So, (1 - b.tokens) / (limit / window.Seconds()) = wait time in seconds.
	waitSeconds := (1 - b.tokens) * l.window.Seconds() / float64(l.limit)
	// Add a small buffer (e.g., 1 second) to the estimated wait time.
	wait := time.Duration(waitSeconds*float64(time.Second)) + time.Second
	return false, wait
}

// min helper function for float64 (Go 1.21+ has built-in min, but for compatibility, we include it)
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// RateLimit implements combined IP and Wallet deposit rate limiting.
func RateLimit(next http.Handler) http.Handler {
	// Initialize IP Rate Limiter: 100 requests per minute
	ipLimiter := newLimiter(100, time.Minute)

	// Initialize Wallet Deposit Rate Limiter: 20 deposits per hour
	walletDepositLimiter := newLimiter(20, time.Hour)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. IP Rate Limiting (100 req/min)
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			// Fallback to full RemoteAddr if splitting fails
			ip = r.RemoteAddr
		}

		if ok, retryAfter := ipLimiter.allow(ip); !ok {
			w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
			api.Error(w, http.StatusTooManyRequests, "global rate limit exceeded: try again later")
			return
		}

		// 2. Wallet Deposit Rate Limiting (20 deposits/hour)
		// This applies only if the X-Wallet-Address header is present,
		// the request method is POST, and the URL path is a deposit endpoint.
		wallet := r.Header.Get("X-Wallet-Address")
		if wallet != "" && r.Method == http.MethodPost {
			// Check if it's a deposit path
			if r.URL.Path == "/api/v1/vaults/deposit" || r.URL.Path == "/api/v1/deposits" {
				if ok, retryAfter := walletDepositLimiter.allow(wallet); !ok {
					w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
					api.Error(w, http.StatusTooManyRequests, "wallet deposit limit exceeded: try again later")
					return
				}
			}
		}

		// If all rate limits pass, proceed to the next handler
		next.ServeHTTP(w, r)
	})
}