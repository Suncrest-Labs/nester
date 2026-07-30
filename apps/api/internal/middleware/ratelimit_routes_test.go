package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
)

// --- Global limiter: excluded paths always pass ---

func TestGlobalRateLimiterExcludesHealthAndMetrics(t *testing.T) {
	const limit = 2
	l := NewLimiter(nil, "global", limit, time.Second)
	handler := GlobalRateLimiter(l, []string{"/health", "/healthz", "/readyz", "/metrics"})(ok200)

	for _, path := range []string{"/healthz", "/readyz", "/metrics", "/health/detailed"} {
		for i := 0; i < limit+5; i++ {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.RemoteAddr = "10.0.0.1:1111"
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s request %d: got %d, want 200 (excluded path must never be limited)", path, i+1, rec.Code)
			}
		}
	}
}

// --- Global limiter: non-excluded paths are limited with Retry-After ---

func TestGlobalRateLimiterLimitsNonExcludedPaths(t *testing.T) {
	const limit = 3
	l := NewLimiter(nil, "global", limit, time.Second)
	handler := GlobalRateLimiter(l, []string{"/health"})(ok200)

	send := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/vaults", nil)
		req.RemoteAddr = "10.0.0.2:2222"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	for i := 0; i < limit; i++ {
		if rec := send(); rec.Code != http.StatusOK {
			t.Fatalf("request %d: got %d, want 200", i+1, rec.Code)
		}
	}

	rec := send()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over-limit request: got %d, want 429", rec.Code)
	}
	ra := rec.Header().Get("Retry-After")
	if secs, err := strconv.Atoi(ra); err != nil || secs < 1 {
		t.Fatalf("Retry-After = %q, want positive integer seconds", ra)
	}
}

// --- Global limiter: per-IP isolation ---

func TestGlobalRateLimiterPerIPIsolation(t *testing.T) {
	const limit = 1
	l := NewLimiter(nil, "global", limit, time.Second)
	handler := GlobalRateLimiter(l, nil)(ok200)

	send := func(ip string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
		req.RemoteAddr = ip + ":9999"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := send("192.168.0.1"); got != http.StatusOK {
		t.Fatalf("IP A first: got %d, want 200", got)
	}
	if got := send("192.168.0.1"); got != http.StatusTooManyRequests {
		t.Fatalf("IP A second: got %d, want 429", got)
	}
	if got := send("192.168.0.2"); got != http.StatusOK {
		t.Fatalf("IP B first: got %d, want 200 (independent bucket)", got)
	}
}

// --- Sensitive IP limiter: only matched routes are limited ---

func TestSensitiveRouteLimiterOnlyLimitsMatchedRoutes(t *testing.T) {
	const limit = 1
	l := NewLimiter(nil, "auth", limit, time.Second)
	routes := []RouteMatch{
		{Method: http.MethodPost, Path: "/api/v1/auth/challenge"},
		{Method: http.MethodPost, Path: "/api/v1/auth/verify"},
	}
	handler := SensitiveRouteLimiter(l, routes, "authentication rate limit exceeded")(ok200)

	send := func(method, path string) int {
		req := httptest.NewRequest(method, path, nil)
		req.RemoteAddr = "10.0.0.5:5555"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	// Unmatched by method: GET on a sensitive path must always pass.
	for i := 0; i < 5; i++ {
		if got := send(http.MethodGet, "/api/v1/auth/challenge"); got != http.StatusOK {
			t.Fatalf("GET challenge %d: got %d, want 200 (only POST is limited)", i+1, got)
		}
	}
	// Unmatched by path: a different endpoint must always pass.
	for i := 0; i < 5; i++ {
		if got := send(http.MethodPost, "/api/v1/vaults"); got != http.StatusOK {
			t.Fatalf("POST vaults %d: got %d, want 200 (unmatched path)", i+1, got)
		}
	}
	// Matched route: first allowed, next rejected.
	if got := send(http.MethodPost, "/api/v1/auth/challenge"); got != http.StatusOK {
		t.Fatalf("POST challenge first: got %d, want 200", got)
	}
	if got := send(http.MethodPost, "/api/v1/auth/challenge"); got != http.StatusTooManyRequests {
		t.Fatalf("POST challenge second: got %d, want 429", got)
	}
}

// --- Sensitive IP limiter: window resets after expiry ---

func TestSensitiveRouteLimiterWindowResets(t *testing.T) {
	const limit = 1
	window := 100 * time.Millisecond
	l := NewLimiter(nil, "auth", limit, window)
	routes := []RouteMatch{{Method: http.MethodPost, Path: "/api/v1/auth/verify"}}
	handler := SensitiveRouteLimiter(l, routes, "authentication rate limit exceeded")(ok200)

	send := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify", nil)
		req.RemoteAddr = "10.0.0.6:6666"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := send(); got != http.StatusOK {
		t.Fatalf("first: got %d, want 200", got)
	}
	if got := send(); got != http.StatusTooManyRequests {
		t.Fatalf("second (before reset): got %d, want 429", got)
	}
	time.Sleep(window + 50*time.Millisecond)
	if got := send(); got != http.StatusOK {
		t.Fatalf("after window reset: got %d, want 200", got)
	}
}

// --- Sensitive user limiter: per-user isolation via Authenticate ---

func TestSensitiveUserRouteLimiterPerUser(t *testing.T) {
	const limit = 1
	l := NewLimiter(nil, "settlement", limit, time.Second)
	routes := []RouteMatch{{Method: http.MethodPost, Path: "/api/v1/settlements"}}

	rules := []RouteRule{{PathPrefix: "/api/v1/"}}
	chain := Authenticate(testSecret, "", rules, alwaysActiveRevocation)(
		SensitiveUserRouteLimiter(l, routes, "settlement rate limit exceeded")(ok200),
	)

	mint := func(id string) string {
		return makeToken(t, auth.Claims{
			Subject:       id,
			WalletAddress: "wallet-" + id,
			ExpiresAt:     time.Now().Add(time.Hour).Unix(),
		})
	}

	send := func(token string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/settlements", nil)
		req.RemoteAddr = "10.0.0.7:7777" // same IP for every request
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		chain.ServeHTTP(rec, req)
		return rec.Code
	}

	tokA := mint("user-A")
	tokB := mint("user-B")

	if got := send(tokA); got != http.StatusOK {
		t.Fatalf("user-A first: got %d, want 200", got)
	}
	if got := send(tokA); got != http.StatusTooManyRequests {
		t.Fatalf("user-A second: got %d, want 429", got)
	}
	// user-B shares the IP but must have an independent per-user bucket.
	if got := send(tokB); got != http.StatusOK {
		t.Fatalf("user-B first: got %d, want 200 (bucket is per-user, not per-IP)", got)
	}
}

// --- Proxy-aware client IP keying ---

func TestClientIPUsesForwardedForBehindTrustedProxies(t *testing.T) {
	ConfigureClientIP(1)
	t.Cleanup(func() { ConfigureClientIP(0) })

	const limit = 1
	l := NewLimiter(nil, "global", limit, time.Second)
	handler := GlobalRateLimiter(l, nil)(ok200)

	// One trusted proxy: the real client is the right-most X-Forwarded-For entry.
	// The proxy connects from a single RemoteAddr, but two different clients
	// behind it must get independent buckets.
	send := func(client string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
		req.RemoteAddr = "10.0.0.254:5555" // the proxy's address, shared
		req.Header.Set("X-Forwarded-For", client)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := send("203.0.113.1"); got != http.StatusOK {
		t.Fatalf("client A first: got %d, want 200", got)
	}
	if got := send("203.0.113.1"); got != http.StatusTooManyRequests {
		t.Fatalf("client A second: got %d, want 429", got)
	}
	// A different client behind the same proxy must not be throttled.
	if got := send("203.0.113.2"); got != http.StatusOK {
		t.Fatalf("client B first: got %d, want 200 (keyed by forwarded client IP, not proxy)", got)
	}
}

func TestClientIPForwardedForCannotBeSpoofedPastTrustedHops(t *testing.T) {
	ConfigureClientIP(1)
	t.Cleanup(func() { ConfigureClientIP(0) })

	const limit = 1
	l := NewLimiter(nil, "global", limit, time.Second)
	handler := GlobalRateLimiter(l, nil)(ok200)

	// With one trusted proxy, only the right-most XFF entry (appended by that
	// proxy) is trusted. A client injecting extra left-most entries cannot rotate
	// its effective key, so it stays in a single bucket.
	send := func(xff string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/x", nil)
		req.RemoteAddr = "10.0.0.254:5555"
		req.Header.Set("X-Forwarded-For", xff)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := send("spoof-1, 203.0.113.9"); got != http.StatusOK {
		t.Fatalf("first: got %d, want 200", got)
	}
	if got := send("spoof-2, 203.0.113.9"); got != http.StatusTooManyRequests {
		t.Fatalf("second (spoofed left-most entry changed): got %d, want 429 (same trusted client IP)", got)
	}
}

// --- NewLimiter without Redis returns a working in-memory fallback ---

func TestNewLimiterFallsBackToMemoryWithoutRedis(t *testing.T) {
	l := NewLimiter(nil, "test", 1, time.Second)

	allowed, _ := l.Allow(t.Context(), "k1")
	if !allowed {
		t.Fatal("first request for key: got denied, want allowed")
	}
	allowed, wait := l.Allow(t.Context(), "k1")
	if allowed {
		t.Fatal("second request for key: got allowed, want denied")
	}
	if wait <= 0 {
		t.Fatalf("denied request retryAfter = %s, want > 0", wait)
	}
	// A different key is unaffected.
	if allowed, _ := l.Allow(t.Context(), "k2"); !allowed {
		t.Fatal("first request for second key: got denied, want allowed")
	}
}
