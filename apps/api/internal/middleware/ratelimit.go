package middleware

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
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
	limit   int
	window  time.Duration
}

func newLimiter(limit int, window time.Duration) *limiter {
	return &limiter{
		buckets: make(map[string]*bucket),
		limit:   limit,
		window:  window,
	}
}

// allow consumes one token for key.  It returns true when the request is
// allowed; otherwise it returns false along with an estimated wait duration
// until the next token becomes available.
func (l *limiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	b, ok := l.buckets[key]
	if !ok {
		// First request for this key — charge immediately, start with limit-1.
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
	refill := elapsed.Seconds() / l.window.Seconds() * float64(l.limit)
	b.tokens = min(float64(l.limit), b.tokens+refill)
	b.lastRefill = now

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}

	// Estimate how long until one token is available.
	wait := time.Duration((1-b.tokens)/float64(l.limit)*float64(l.window)) + time.Second
	return false, wait
}

// IPRateLimiter returns middleware that enforces a per-remote-IP rate limit of
// limit requests per window.
func IPRateLimiter(limit int, window time.Duration) func(http.Handler) http.Handler {
	l := newLimiter(limit, window)
	return rateLimitMiddleware(l, func(r *http.Request) string {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return r.RemoteAddr
		}
		return ip
	})
}

// WalletRateLimiter returns middleware that enforces a per-wallet rate limit.
// extractWallet derives the wallet key from the request; an empty string means
// no key is present and the request passes through unchecked.
func WalletRateLimiter(limit int, window time.Duration, extractWallet func(*http.Request) string) func(http.Handler) http.Handler {
	l := newLimiter(limit, window)
	return rateLimitMiddleware(l, extractWallet)
}

// WriteMethodRateLimiter returns middleware that applies a stricter per-IP rate
// limit only to mutating HTTP methods (POST, PUT, PATCH, DELETE). Read-only
// requests (GET, HEAD, OPTIONS) pass through untouched.
//
// This satisfies the per-route-group tier requirement from Issue #10: public
// read endpoints get the global limit while write/state-changing endpoints get
// a tighter limit to prevent abuse (e.g., rapid vault creation).
func WriteMethodRateLimiter(limit int, window time.Duration) func(http.Handler) http.Handler {
	l := newLimiter(limit, window)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				// fall through to rate limiting
			default:
				next.ServeHTTP(w, r)
				return
			}

			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}

			allowed, wait := l.allow(ip)
			if !allowed {
				retryAfter := int(wait.Seconds())
				if retryAfter < 1 {
					retryAfter = 1
				}
				w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprintf(w, `{"success":false,"error":{"code":"RATE_LIMITED","message":"write rate limit exceeded"}}`)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func rateLimitMiddleware(l *limiter, keyFn func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFn(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			allowed, wait := l.allow(key)
			if !allowed {
				retryAfter := int(wait.Seconds())
				if retryAfter < 1 {
					retryAfter = 1
				}
				w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprintf(w, `{"success":false,"error":{"code":429,"message":"rate limit exceeded"}}`)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
