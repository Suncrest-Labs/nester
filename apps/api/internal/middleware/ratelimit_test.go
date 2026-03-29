package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// ok200 is a trivial handler used throughout the middleware tests.
var ok200 = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// --- Within-limit requests ---

func TestIPRateLimiterAllowsRequestsWithinLimit(t *testing.T) {
	const limit = 5
	handler := IPRateLimiter(limit, time.Second)(ok200)

	for i := 0; i < limit; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: got %d, want 200", i+1, rec.Code)
		}
	}
}

// --- Burst: exceeding limit returns 429 ---

func TestIPRateLimiterRejects429WhenLimitExceeded(t *testing.T) {
	const limit = 3
	handler := IPRateLimiter(limit, time.Second)(ok200)

	ip := "10.0.0.2:12345"
	for i := 0; i < limit; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	// (limit+1)th request must be rejected.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ip
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429", rec.Code)
	}
}

// --- 429 includes Retry-After ---

func TestIPRateLimiter429IncludesRetryAfterHeader(t *testing.T) {
	const limit = 1
	handler := IPRateLimiter(limit, time.Second)(ok200)
	ip := "10.0.0.3:12345"

	// Exhaust the limit.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ip
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// This request is rate-limited.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ip
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("got %d, want 429", rec.Code)
	}

	ra := rec.Header().Get("Retry-After")
	if ra == "" {
		t.Fatal("expected Retry-After header on 429, got none")
	}
	secs, err := strconv.Atoi(ra)
	if err != nil || secs < 1 {
		t.Fatalf("Retry-After = %q, want a positive integer in seconds", ra)
	}
}

// --- Window resets after expiry ---

func TestIPRateLimiterWindowResetsAfterExpiry(t *testing.T) {
	const limit = 2
	window := 100 * time.Millisecond
	handler := IPRateLimiter(limit, window)(ok200)
	ip := "10.0.0.4:12345"

	send := func() int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	// Exhaust the bucket.
	for i := 0; i < limit; i++ {
		send()
	}
	if got := send(); got != http.StatusTooManyRequests {
		t.Fatalf("before window reset: got %d, want 429", got)
	}

	// Wait for the window to fully refill.
	time.Sleep(window + 50*time.Millisecond)

	if got := send(); got != http.StatusOK {
		t.Fatalf("after window reset: got %d, want 200", got)
	}
}

// --- Per-IP independence ---

func TestIPRateLimiterPerIPBucketsAreIndependent(t *testing.T) {
	const limit = 1
	handler := IPRateLimiter(limit, time.Second)(ok200)

	sendFrom := func(ip string) int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip + ":1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	// Exhaust the limit for IP A.
	if got := sendFrom("192.168.1.1"); got != http.StatusOK {
		t.Fatalf("IP A first request: got %d, want 200", got)
	}
	if got := sendFrom("192.168.1.1"); got != http.StatusTooManyRequests {
		t.Fatalf("IP A second request: got %d, want 429", got)
	}

	// IP B must still have its full quota.
	if got := sendFrom("192.168.1.2"); got != http.StatusOK {
		t.Fatalf("IP B first request: got %d, want 200 (bucket is independent of IP A)", got)
	}
}

// --- Per-wallet independence ---

func TestWalletRateLimiterPerWalletBucketsAreIndependent(t *testing.T) {
	const limit = 1
	extractWallet := func(r *http.Request) string {
		return r.Header.Get("X-Wallet")
	}
	handler := WalletRateLimiter(limit, time.Second, extractWallet)(ok200)

	sendWallet := func(wallet string) int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		req.Header.Set("X-Wallet", wallet)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	// Exhaust wallet-A.
	if got := sendWallet("wallet-A"); got != http.StatusOK {
		t.Fatalf("wallet-A first: got %d, want 200", got)
	}
	if got := sendWallet("wallet-A"); got != http.StatusTooManyRequests {
		t.Fatalf("wallet-A second: got %d, want 429", got)
	}

	// wallet-B must be unaffected.
	if got := sendWallet("wallet-B"); got != http.StatusOK {
		t.Fatalf("wallet-B first: got %d, want 200 (independent bucket from wallet-A)", got)
	}
}

// --- No wallet key passes through ---

func TestWalletRateLimiterPassesThroughWhenNoKey(t *testing.T) {
	const limit = 1
	extractWallet := func(r *http.Request) string {
		return r.Header.Get("X-Wallet") // absent → ""
	}
	handler := WalletRateLimiter(limit, time.Second, extractWallet)(ok200)

	// Many requests with no wallet header — all must pass (no key, no limit).
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d without wallet header: got %d, want 200", i+1, rec.Code)
		}
	}
}
