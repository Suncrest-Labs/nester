package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// trustedProxyCount is the number of trusted reverse proxies / load balancers in
// front of the API. It defaults to 0 (trust none: rate limits key off the direct
// connection address) and is set once at startup via ConfigureClientIP. When
// greater than 0, clientIP derives the originating client address from the
// X-Forwarded-For chain, counting hops from the right so spoofed left-most
// entries cannot forge a client IP past the trusted-proxy boundary.
var trustedProxyCount int

// ConfigureClientIP sets how many trusted proxies sit in front of the API for
// the purpose of client-IP extraction in rate limiting. It must be called once
// during startup, before the server begins serving. A negative count is treated
// as 0. See trustedProxyCount.
func ConfigureClientIP(count int) {
	if count < 0 {
		count = 0
	}
	trustedProxyCount = count
}

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

// clientIP returns the address used to key rate limits for r.
//
// With no trusted proxies configured (the default), it is the direct connection
// address from r.RemoteAddr (port stripped). When trustedProxyCount > 0, the API
// is assumed to sit behind that many trusted proxies, and the originating client
// address is taken from the X-Forwarded-For chain: the direct peer is appended as
// the right-most hop and the entry trustedProxyCount positions further left is
// returned. Counting from the right means a client cannot spoof its way past the
// trusted-proxy boundary by injecting extra left-most X-Forwarded-For entries.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if trustedProxyCount <= 0 {
		return host
	}

	// chain is client...proxies (left to right), with the direct peer last.
	chain := make([]string, 0, trustedProxyCount+1)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for _, p := range strings.Split(xff, ",") {
			if p = strings.TrimSpace(p); p != "" {
				chain = append(chain, p)
			}
		}
	}
	chain = append(chain, host)

	idx := max(len(chain)-1-trustedProxyCount, 0)
	return chain[idx]
}

// writeRateLimited writes the shared 429 response: a Retry-After header (in
// whole seconds, minimum 1) and the API's standard JSON error envelope.
func writeRateLimited(w http.ResponseWriter, wait time.Duration, message string) {
	retryAfter := max(int(wait.Seconds()), 1)
	w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	fmt.Fprintf(w, `{"success":false,"error":{"code":"RATE_LIMITED","message":%q}}`, message)
}

// IPRateLimiter returns middleware that enforces a per-remote-IP rate limit of
// limit requests per window.
func IPRateLimiter(limit int, window time.Duration) func(http.Handler) http.Handler {
	l := newLimiter(limit, window)
	return rateLimitMiddleware(l, clientIP)
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

			allowed, wait := l.allow(clientIP(r))
			if !allowed {
				writeRateLimited(w, wait, "write rate limit exceeded")
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
				writeRateLimited(w, wait, "rate limit exceeded")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
