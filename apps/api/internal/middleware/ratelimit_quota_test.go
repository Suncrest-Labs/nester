package middleware

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/suncrestlabs/nester/apps/api/internal/auth"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testQuotaConfig mirrors how main.go wires the middleware. The limit and
// window are the limiter's, so they are passed there rather than here.
func testQuotaConfig() QuotaConfig {
	return QuotaConfig{Enabled: true, Logger: quietLogger()}
}

// okHandler records that the request reached the wrapped handler.
func okHandler(reached *int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if reached != nil {
			*reached++
		}
		w.WriteHeader(http.StatusOK)
	})
}

// authed returns r carrying an authenticated user, as the auth middleware
// would leave it.
func authed(r *http.Request, userID string) *http.Request {
	return r.WithContext(auth.NewContext(r.Context(), auth.User{ID: userID}))
}

// quotaErrorBody is the machine-readable shape of a 429 body. A client has to
// be able to parse this to behave correctly, so the test parses it too rather
// than string-matching.
type quotaErrorBody struct {
	Success bool `json:"success"`
	Error   struct {
		Code              int    `json:"code"`
		Message           string `json:"message"`
		Reason            string `json:"reason"`
		Quota             string `json:"quota"`
		Cost              int    `json:"cost"`
		Limit             int    `json:"limit"`
		Remaining         int    `json:"remaining"`
		RetryAfterSeconds int    `json:"retry_after_seconds"`
		ResetSeconds      int    `json:"reset_seconds"`
	} `json:"error"`
}

// ---------------------------------------------------------------------------
// Headers on every response
// ---------------------------------------------------------------------------

// Well-behaved clients can only self-throttle if they are told the budget
// before they blow it, so the headers must be on successful responses too.
func TestQuotaHeadersOnSuccessfulResponse(t *testing.T) {
	l := newMemoryQuotaLimiter(100, time.Minute)
	h := CostQuota(l, testQuotaConfig())(okHandler(nil))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(httptest.NewRequest(http.MethodGet, "/api/v1/users/profile", nil), "user-1"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("RateLimit-Limit"); got != "100" {
		t.Errorf("RateLimit-Limit = %q, want %q", got, "100")
	}
	// One default-cost request against a 100 bucket.
	if got := rec.Header().Get("RateLimit-Remaining"); got != "99" {
		t.Errorf("RateLimit-Remaining = %q, want %q", got, "99")
	}
	if rec.Header().Get("RateLimit-Reset") == "" {
		t.Error("RateLimit-Reset missing on a successful response")
	}
}

// Remaining must fall by the route's declared cost, not by one.
func TestRemainingReflectsRouteCost(t *testing.T) {
	l := newMemoryQuotaLimiter(100, time.Minute)
	h := CostQuota(l, testQuotaConfig())(okHandler(nil))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", nil), "user-1"))

	want := strconv.Itoa(100 - CostLLMRelay)
	if got := rec.Header().Get("RateLimit-Remaining"); got != want {
		t.Errorf("RateLimit-Remaining = %q, want %q (charged %d for the relay)", got, want, CostLLMRelay)
	}
}

// ---------------------------------------------------------------------------
// Exhaustion
// ---------------------------------------------------------------------------

func TestQuotaExhaustionReturns429WithRetryAfterAndReason(t *testing.T) {
	// Two relay calls fit in the bucket; the third does not.
	l := newMemoryQuotaLimiter(2*CostLLMRelay, time.Minute)
	reached := 0
	h := CostQuota(l, testQuotaConfig())(okHandler(&reached))

	var rec *httptest.ResponseRecorder
	for i := 0; i < 3; i++ {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, authed(httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", nil), "user-1"))
	}

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("third relay call status = %d, want 429", rec.Code)
	}
	if reached != 2 {
		t.Errorf("handler reached %d times, want 2", reached)
	}

	retryAfter := rec.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("Retry-After missing on 429")
	}
	secs, err := strconv.Atoi(retryAfter)
	if err != nil || secs < 1 {
		t.Errorf("Retry-After = %q, want a whole number of seconds >= 1", retryAfter)
	}

	var body quotaErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("429 body is not valid JSON: %v (%s)", err, rec.Body.String())
	}
	if body.Success {
		t.Error("429 body reports success:true")
	}
	if body.Error.Reason != "QUOTA_EXHAUSTED" {
		t.Errorf("reason = %q, want QUOTA_EXHAUSTED", body.Error.Reason)
	}
	if body.Error.Quota == "" {
		t.Error("body does not identify which quota was exhausted")
	}
	if body.Error.Cost != CostLLMRelay {
		t.Errorf("body cost = %d, want %d", body.Error.Cost, CostLLMRelay)
	}
	if body.Error.Limit != 2*CostLLMRelay {
		t.Errorf("body limit = %d, want %d", body.Error.Limit, 2*CostLLMRelay)
	}
	if body.Error.ResetSeconds <= 0 {
		t.Errorf("body reset_seconds = %d, want > 0 so a client knows when it recovers", body.Error.ResetSeconds)
	}
	if body.Error.RetryAfterSeconds != secs {
		t.Errorf("body retry_after_seconds = %d disagrees with the Retry-After header %d",
			body.Error.RetryAfterSeconds, secs)
	}
}

// Rejections carry the headers too, so a client that only reads them still
// learns the budget.
func TestQuotaHeadersOnRejection(t *testing.T) {
	// Capacity is an exact multiple of the cost, so the second call empties
	// the bucket and the third is refused with nothing left.
	l := newMemoryQuotaLimiter(2*CostLLMRelay, time.Minute)
	h := CostQuota(l, testQuotaConfig())(okHandler(nil))

	var rec *httptest.ResponseRecorder
	for i := 0; i < 3; i++ {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, authed(httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", nil), "user-1"))
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("RateLimit-Limit") == "" || rec.Header().Get("RateLimit-Reset") == "" {
		t.Error("rate limit headers missing on a 429")
	}
	if got := rec.Header().Get("RateLimit-Remaining"); got != "0" {
		t.Errorf("RateLimit-Remaining = %q, want %q", got, "0")
	}
}

// An expensive route must exhaust the quota far sooner than a cheap one. This
// is the whole point: counting requests treats these identically.
func TestExpensiveRoutesExhaustSoonerThanCheapOnes(t *testing.T) {
	const limit = 100

	countAllowed := func(method, path string) int {
		l := newMemoryQuotaLimiter(limit, time.Minute)
		h := CostQuota(l, testQuotaConfig())(okHandler(nil))
		allowed := 0
		for i := 0; i < limit+1; i++ {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, authed(httptest.NewRequest(method, path, nil), "user-1"))
			if rec.Code == http.StatusOK {
				allowed++
			}
		}
		return allowed
	}

	cheap := countAllowed(http.MethodGet, "/api/v1/users/profile")
	relay := countAllowed(http.MethodPost, "/api/v1/intelligence/chat")

	if cheap != limit {
		t.Errorf("cheap route allowed %d, want %d", cheap, limit)
	}
	if want := limit / CostLLMRelay; relay != want {
		t.Errorf("relay allowed %d, want %d", relay, want)
	}
	if relay >= cheap {
		t.Errorf("relay allowed %d and cheap allowed %d; the quota is not weighting cost", relay, cheap)
	}
}

// ---------------------------------------------------------------------------
// Token bucket: the boundary a fixed window would let through
// ---------------------------------------------------------------------------

// A fixed window resets its counter wholesale at the boundary, so a caller can
// spend the entire allowance just before it and the entire allowance just
// after: 2x the configured limit inside a couple of milliseconds, at exactly
// the moment the expensive dependencies are already loaded.
//
// The bucket refills continuously, so immediately after the nominal boundary
// only the elapsed sliver has come back.
func TestBucketDeniesTheBurstAFixedWindowWouldAllow(t *testing.T) {
	const limit = 60
	const window = time.Minute

	now := time.Now()
	l := newMemoryQuotaLimiter(limit, window)
	l.now = func() time.Time { return now }

	// Drain the bucket at the very end of the first nominal window.
	for i := 0; i < limit; i++ {
		if d := l.AllowN(context.Background(), "u:1", 1); !d.Allowed {
			t.Fatalf("request %d during initial drain: denied, want allowed", i+1)
		}
	}
	if d := l.AllowN(context.Background(), "u:1", 1); d.Allowed {
		t.Fatal("bucket is drained but still allowing")
	}

	// Cross the boundary. A fixed window would now hand back the full limit.
	now = now.Add(window)

	allowed := 0
	for i := 0; i < limit; i++ {
		if d := l.AllowN(context.Background(), "u:1", 1); d.Allowed {
			allowed++
		}
	}

	// One full window elapsed, so a full refill is correct — what must not
	// happen is 2x the limit inside the boundary instant, which is what the
	// drain plus this burst would total under a fixed window.
	if allowed != limit {
		t.Errorf("after a full window: allowed %d, want %d", allowed, limit)
	}

	// The real boundary case: drain, advance a sliver, and confirm only the
	// sliver's worth of refill is available rather than a whole new window.
	now = now.Add(time.Second) // 1/60th of the window => ~1 token
	allowed = 0
	for i := 0; i < limit; i++ {
		if d := l.AllowN(context.Background(), "u:1", 1); d.Allowed {
			allowed++
		}
	}
	// ~1, give or take a rounding ulp on the refill. The assertion that
	// carries the weight is that it is nowhere near a fresh window's worth.
	if allowed > 2 {
		t.Errorf("one second after draining: allowed %d, want ~1 (a fixed window would have allowed %d)",
			allowed, limit)
	}
	if allowed == 0 {
		t.Error("one second after draining: allowed 0, want ~1 — the bucket is not refilling")
	}
}

// Refill is proportional to elapsed time, not granted in window-sized lumps.
func TestBucketRefillsProportionally(t *testing.T) {
	const limit = 100
	const window = time.Minute

	now := time.Now()
	l := newMemoryQuotaLimiter(limit, window)
	l.now = func() time.Time { return now }

	for i := 0; i < limit; i++ {
		l.AllowN(context.Background(), "u:1", 1)
	}

	now = now.Add(window / 4) // a quarter window => a quarter of the limit
	allowed := 0
	for i := 0; i < limit; i++ {
		if d := l.AllowN(context.Background(), "u:1", 1); d.Allowed {
			allowed++
		}
	}
	if allowed < 24 || allowed > 26 {
		t.Errorf("after a quarter window: allowed %d, want ~25", allowed)
	}
}

// The bucket must never refill past its capacity, or an idle user accumulates
// an unbounded burst.
func TestBucketDoesNotOverfill(t *testing.T) {
	now := time.Now()
	l := newMemoryQuotaLimiter(10, time.Minute)
	l.now = func() time.Time { return now }

	l.AllowN(context.Background(), "u:1", 1)
	now = now.Add(24 * time.Hour)

	allowed := 0
	for i := 0; i < 100; i++ {
		if d := l.AllowN(context.Background(), "u:1", 1); d.Allowed {
			allowed++
		}
	}
	if allowed != 10 {
		t.Errorf("after a long idle period: allowed %d, want the capacity 10", allowed)
	}
}

// A clock that steps backwards must not drain the bucket by a negative refill.
func TestBucketToleratesClockGoingBackwards(t *testing.T) {
	now := time.Now()
	l := newMemoryQuotaLimiter(10, time.Minute)
	l.now = func() time.Time { return now }

	l.AllowN(context.Background(), "u:1", 5)
	before := l.AllowN(context.Background(), "u:1", 0).Remaining

	now = now.Add(-time.Hour)
	after := l.AllowN(context.Background(), "u:1", 0).Remaining

	if after > before {
		t.Errorf("balance grew from %d to %d when the clock went backwards", before, after)
	}
}

// ---------------------------------------------------------------------------
// Keying
// ---------------------------------------------------------------------------

// Quotas are per user: one user exhausting theirs must not affect another.
func TestQuotaIsPerUser(t *testing.T) {
	l := newMemoryQuotaLimiter(2*CostLLMRelay, time.Minute)
	h := CostQuota(l, testQuotaConfig())(okHandler(nil))

	// Three relay calls against a two-call bucket: the third must be refused.
	drain := func(user string) int {
		last := 0
		for i := 0; i < 3; i++ {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, authed(httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", nil), user))
			last = rec.Code
		}
		return last
	}

	if got := drain("user-1"); got != http.StatusTooManyRequests {
		t.Fatalf("user-1 final status = %d, want 429", got)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", nil), "user-2"))
	if rec.Code != http.StatusOK {
		t.Errorf("user-2 status = %d, want 200 — one user's exhaustion leaked into another's quota", rec.Code)
	}
}

// Unauthenticated callers fall back to per-IP accounting.
func TestQuotaFallsBackToIPWhenAnonymous(t *testing.T) {
	l := newMemoryQuotaLimiter(CostLLMRelay, time.Minute)
	h := CostQuota(l, testQuotaConfig())(okHandler(nil))

	req := func(addr string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", nil)
		r.RemoteAddr = addr + ":12345"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}

	if got := req("10.0.0.1"); got != http.StatusOK {
		t.Fatalf("first anonymous call = %d, want 200", got)
	}
	if got := req("10.0.0.1"); got != http.StatusTooManyRequests {
		t.Errorf("second anonymous call from the same IP = %d, want 429", got)
	}
	if got := req("10.0.0.2"); got != http.StatusOK {
		t.Errorf("call from a different IP = %d, want 200", got)
	}
}

// A user ID must never collide with an IP address in the keyspace.
func TestUserAndIPKeysAreDistinct(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/users/profile", nil)
	r.RemoteAddr = "10.0.0.1:1"

	anon := quotaKey(r)
	user := quotaKey(authed(r, "10.0.0.1"))

	if anon == user {
		t.Errorf("a user named %q shares a quota key with the IP of the same name: %q", "10.0.0.1", anon)
	}
}

// ---------------------------------------------------------------------------
// Fail open
// ---------------------------------------------------------------------------

// A limiter outage must not become a service outage.
func TestRedisQuotaFailsOpenWhenRedisIsDown(t *testing.T) {
	rc := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1", // nothing listening
		DialTimeout: 50 * time.Millisecond,
		ReadTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() { _ = rc.Close() })

	l := NewQuotaLimiter(rc, "test", 1, time.Minute, quietLogger())

	// The bucket holds one unit, so a 25-unit relay call would be rejected
	// outright if Redis were answering. It must pass anyway.
	d := l.AllowN(context.Background(), "u:1", CostLLMRelay)
	if !d.Allowed {
		t.Fatal("request rejected while Redis is down; the limiter must fail open")
	}
	if !d.Degraded {
		t.Error("decision not marked degraded, so the caller cannot tell the count is fictional")
	}
}

// The middleware must pass the request through, and must not invent rate limit
// headers it has no basis for.
func TestQuotaMiddlewareFailsOpenWhenRedisIsDown(t *testing.T) {
	rc := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 50 * time.Millisecond,
		ReadTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() { _ = rc.Close() })

	l := NewQuotaLimiter(rc, "test", 1, time.Minute, quietLogger())
	reached := 0
	h := CostQuota(l, testQuotaConfig())(okHandler(&reached))

	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, authed(httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", nil), "user-1"))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200 while Redis is down", i+1, rec.Code)
		}
		if rec.Header().Get("RateLimit-Remaining") != "" {
			t.Error("reported a remaining balance that was never counted")
		}
	}
	if reached != 5 {
		t.Errorf("handler reached %d times, want 5", reached)
	}
}

// ---------------------------------------------------------------------------
// Configuration and opt-out
// ---------------------------------------------------------------------------

// The documented load-test escape hatch: quotas off entirely.
func TestDisabledQuotaChargesNothing(t *testing.T) {
	l := newMemoryQuotaLimiter(CostLLMRelay, time.Minute)
	cfg := testQuotaConfig()
	cfg.Enabled = false
	h := CostQuota(l, cfg)(okHandler(nil))

	for i := 0; i < 20; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, authed(httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", nil), "user-1"))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200 with quotas disabled", i+1, rec.Code)
		}
	}
}

func TestBypassTokenSkipsAccounting(t *testing.T) {
	l := newMemoryQuotaLimiter(CostLLMRelay, time.Minute)
	cfg := testQuotaConfig()
	cfg.BypassToken = "load-test-secret"
	h := CostQuota(l, cfg)(okHandler(nil))

	for i := 0; i < 10; i++ {
		r := authed(httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", nil), "user-1")
		r.Header.Set(quotaBypassHeader, "load-test-secret")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d with a valid bypass token: status = %d, want 200", i+1, rec.Code)
		}
	}
}

func TestBypassRejectsWrongToken(t *testing.T) {
	l := newMemoryQuotaLimiter(CostLLMRelay, time.Minute)
	cfg := testQuotaConfig()
	cfg.BypassToken = "load-test-secret"
	h := CostQuota(l, cfg)(okHandler(nil))

	first := httptest.NewRecorder()
	h.ServeHTTP(first, authed(httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", nil), "user-1"))
	if first.Code != http.StatusOK {
		t.Fatalf("setup call = %d, want 200", first.Code)
	}

	r := authed(httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", nil), "user-1")
	r.Header.Set(quotaBypassHeader, "wrong")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429 — a wrong bypass token must not bypass", rec.Code)
	}
}

// With no token configured the mechanism must be entirely inert, so an unset
// config cannot be bypassed by guessing (including with an empty header).
func TestBypassInertWhenUnconfigured(t *testing.T) {
	l := newMemoryQuotaLimiter(CostLLMRelay, time.Minute)
	h := CostQuota(l, testQuotaConfig())(okHandler(nil))

	for _, attempt := range []string{"", "anything", "true"} {
		user := "user-bypass-" + attempt
		// Spend the whole bucket, then try to escape the consequences.
		first := httptest.NewRecorder()
		h.ServeHTTP(first, authed(httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", nil), user))
		if first.Code != http.StatusOK {
			t.Fatalf("bypass %q: setup call = %d, want 200", attempt, first.Code)
		}

		r := authed(httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", nil), user)
		r.Header.Set(quotaBypassHeader, attempt)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("bypass %q: status = %d, want 429", attempt, rec.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// Metrics hook (#1043)
// ---------------------------------------------------------------------------

func TestObserverSeesAllowedAndRejectedDecisions(t *testing.T) {
	l := newMemoryQuotaLimiter(CostLLMRelay, time.Minute)
	var events []QuotaEvent
	cfg := testQuotaConfig()
	cfg.Observer = func(e QuotaEvent) { events = append(events, e) }
	h := CostQuota(l, cfg)(okHandler(nil))

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, authed(httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", nil), "user-1"))
	}

	if len(events) != 2 {
		t.Fatalf("observed %d events, want 2", len(events))
	}
	if !events[0].Allowed || events[1].Allowed {
		t.Errorf("expected allow then reject, got %v then %v", events[0].Allowed, events[1].Allowed)
	}
	for i, e := range events {
		if e.Cost != CostLLMRelay {
			t.Errorf("event %d cost = %d, want %d", i, e.Cost, CostLLMRelay)
		}
		if e.Route != "POST /api/v1/intelligence/chat" {
			t.Errorf("event %d route = %q, want the declared pattern", i, e.Route)
		}
	}
}

// Metric labels must be bounded. An undeclared route carries IDs in its path,
// so it must not be reported verbatim.
func TestObserverRouteLabelIsBounded(t *testing.T) {
	l := newMemoryQuotaLimiter(100, time.Minute)
	var got string
	cfg := testQuotaConfig()
	cfg.Observer = func(e QuotaEvent) { got = e.Route }
	h := CostQuota(l, cfg)(okHandler(nil))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authed(httptest.NewRequest(http.MethodGet, "/api/v1/users/9f3c-secret-id", nil), "user-1"))

	if got != "GET *" {
		t.Errorf("route label = %q, want %q — raw paths would explode metric cardinality", got, "GET *")
	}
}

// ---------------------------------------------------------------------------
// Redis-backed behaviour (skipped without a live Redis)
// ---------------------------------------------------------------------------

func TestRedisQuotaChargesCost(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set; skipping distributed quota test")
	}
	rc := redis.NewClient(&redis.Options{Addr: addr})
	ctx := context.Background()
	if err := rc.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not reachable at %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = rc.Close() })

	prefix := "test-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	key := "u:quota-1"
	t.Cleanup(func() { _ = rc.Del(ctx, "rlq:"+prefix+":"+key).Err() })

	l := NewQuotaLimiter(rc, prefix, 50, time.Minute, quietLogger())

	first := l.AllowN(ctx, key, CostLLMRelay)
	if !first.Allowed {
		t.Fatal("first relay call denied")
	}
	if want := 50 - CostLLMRelay; first.Remaining != want {
		t.Errorf("remaining = %d, want %d", first.Remaining, want)
	}

	if second := l.AllowN(ctx, key, CostLLMRelay); !second.Allowed {
		t.Fatal("second relay call denied")
	}
	third := l.AllowN(ctx, key, CostLLMRelay)
	if third.Allowed {
		t.Fatal("third relay call allowed; the quota is not being charged")
	}
	if third.RetryAfter <= 0 {
		t.Errorf("RetryAfter = %s, want > 0", third.RetryAfter)
	}
	if third.Reset <= 0 {
		t.Errorf("Reset = %s, want > 0", third.Reset)
	}
}

// The Redis path is the production path, so the boundary property has to hold
// there too — not only in the in-memory fallback the other tests exercise.
func TestRedisQuotaDeniesTheBurstAFixedWindowWouldAllow(t *testing.T) {
	rc, prefix := redisForTest(t)
	ctx := context.Background()
	key := "u:boundary"
	t.Cleanup(func() { _ = rc.Del(ctx, "rlq:"+prefix+":"+key).Err() })

	// A 2s window keeps the test quick while staying well above the
	// millisecond resolution the script computes refill in.
	const limit = 20
	l := NewQuotaLimiter(rc, prefix, limit, 2*time.Second, quietLogger())

	for i := 0; i < limit; i++ {
		if d := l.AllowN(ctx, key, 1); !d.Allowed {
			t.Fatalf("request %d during drain: denied, want allowed", i+1)
		}
	}
	if d := l.AllowN(ctx, key, 1); d.Allowed {
		t.Fatal("bucket drained but still allowing")
	}

	// A tenth of a window later, a fixed window that had just rolled over
	// would hand back the entire limit again. The bucket owes ~2 tokens.
	time.Sleep(200 * time.Millisecond)

	allowed := 0
	for i := 0; i < limit; i++ {
		if d := l.AllowN(ctx, key, 1); d.Allowed {
			allowed++
		}
	}
	if allowed >= limit {
		t.Errorf("allowed %d of %d immediately after draining; a fixed window's boundary burst is present",
			allowed, limit)
	}
	if allowed == 0 {
		t.Error("allowed 0; the bucket is not refilling at all")
	}
	if allowed > limit/2 {
		t.Errorf("allowed %d after a tenth of a window, want roughly %d", allowed, limit/10)
	}
}

// Two limiters sharing one Redis are two API instances. The quota must be
// enforced across them, or scaling out multiplies the effective limit by the
// instance count.
func TestRedisQuotaIsSharedAcrossInstances(t *testing.T) {
	rc, prefix := redisForTest(t)
	ctx := context.Background()
	key := "u:shared"
	t.Cleanup(func() { _ = rc.Del(ctx, "rlq:"+prefix+":"+key).Err() })

	instanceA := NewQuotaLimiter(rc, prefix, 2*CostLLMRelay, time.Minute, quietLogger())
	instanceB := NewQuotaLimiter(rc, prefix, 2*CostLLMRelay, time.Minute, quietLogger())

	if d := instanceA.AllowN(ctx, key, CostLLMRelay); !d.Allowed {
		t.Fatal("instance A first call denied")
	}
	if d := instanceB.AllowN(ctx, key, CostLLMRelay); !d.Allowed {
		t.Fatal("instance B first call denied")
	}
	// The budget is spent. Neither instance may grant another.
	if d := instanceB.AllowN(ctx, key, CostLLMRelay); d.Allowed {
		t.Error("instance B allowed a third call; the quota is not shared across instances")
	}
	if d := instanceA.AllowN(ctx, key, CostLLMRelay); d.Allowed {
		t.Error("instance A allowed a third call; the quota is not shared across instances")
	}
}

// A partial refill must survive the round-trip through Redis. If the balance
// were stored as an integer, every fractional token would be lost and a busy
// key would refill measurably slower than configured.
func TestRedisQuotaKeepsFractionalBalance(t *testing.T) {
	rc, prefix := redisForTest(t)
	ctx := context.Background()
	key := "u:fractional"
	t.Cleanup(func() { _ = rc.Del(ctx, "rlq:"+prefix+":"+key).Err() })

	// 10 tokens/minute => 1 token per 6s. A short sleep accrues a fraction.
	l := NewQuotaLimiter(rc, prefix, 10, time.Minute, quietLogger())
	for i := 0; i < 10; i++ {
		l.AllowN(ctx, key, 1)
	}
	time.Sleep(150 * time.Millisecond)
	l.AllowN(ctx, key, 0) // force a refill + write-back

	stored, err := rc.HGet(ctx, "rlq:"+prefix+":"+key, "t").Result()
	if err != nil {
		t.Fatalf("reading stored balance: %v", err)
	}
	if !strings.Contains(stored, ".") {
		t.Errorf("stored balance %q has no fractional part; partial refill is being truncated", stored)
	}
}

// redisForTest returns a client and a unique key prefix, skipping when no Redis
// is configured.
func redisForTest(t *testing.T) (*redis.Client, string) {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set; skipping distributed quota test")
	}
	rc := redis.NewClient(&redis.Options{Addr: addr})
	if err := rc.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis not reachable at %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = rc.Close() })
	return rc, "test-" + strconv.FormatInt(time.Now().UnixNano(), 10)
}
