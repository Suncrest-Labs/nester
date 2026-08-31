package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/metrics"
)

// recordingAuthMetrics captures what the guard reported.
type recordingAuthMetrics struct {
	mu       sync.Mutex
	failures map[string]int
	lockouts map[string]int
	rejected map[string]int
}

func newRecordingAuthMetrics() *recordingAuthMetrics {
	return &recordingAuthMetrics{
		failures: map[string]int{},
		lockouts: map[string]int{},
		rejected: map[string]int{},
	}
}

func (m *recordingAuthMetrics) RecordAuthFailure(scope, stage string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failures[scope+"/"+stage]++
}

func (m *recordingAuthMetrics) RecordAuthLockout(scope, stage string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lockouts[scope+"/"+stage]++
}

func (m *recordingAuthMetrics) RecordAuthLockedRequest(scope, stage string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rejected[scope+"/"+stage]++
}

func (m *recordingAuthMetrics) count(kind map[string]int, key string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return kind[key]
}

func testAuthStages() []AuthGuardStage {
	return []AuthGuardStage{
		{Stage: metrics.AuthStageChallenge, Route: RouteMatch{Method: http.MethodPost, Path: "/api/v1/auth/challenge"}},
		{Stage: metrics.AuthStageVerify, Route: RouteMatch{Method: http.MethodPost, Path: "/api/v1/auth/verify"}},
	}
}

func testLockoutConfig() AuthLockoutConfig {
	return AuthLockoutConfig{
		Threshold: 3,
		Window:    time.Minute,
		Base:      30 * time.Second,
		Max:       10 * time.Minute,
	}
}

// newTestGuard wires an in-memory guard (nil Redis) around a handler that
// returns the supplied status, and reports how many times the handler ran.
func newTestGuard(t *testing.T, status int, walletLimit int) (http.Handler, *recordingAuthMetrics, *int32) {
	t.Helper()

	var handlerCalls int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&handlerCalls, 1)
		w.WriteHeader(status)
	})

	rec := newRecordingAuthMetrics()
	guard := NewAuthGuard(
		NewLimiter(nil, "authwallet", walletLimit, time.Minute),
		NewAuthLockout(nil, "wallet", testLockoutConfig()),
		NewAuthLockout(nil, "ip", testLockoutConfig()),
		rec,
		testAuthStages(),
	)
	return guard.Middleware()(handler), rec, &handlerCalls
}

func authRequest(t *testing.T, path, wallet, ip string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path,
		strings.NewReader(fmt.Sprintf(`{"wallet_address":%q,"signature":"sig"}`, wallet)))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = ip + ":12345"
	return req
}

// Repeated failures must eventually lock the caller out, and the lockout must
// be reported as a metric (nester#1104).
func TestAuthGuard_RepeatedFailures_LockOutAndAreObservable(t *testing.T) {
	h, rec, calls := newTestGuard(t, http.StatusUnauthorized, 1000)

	const wallet = "GWALLETLOCKOUT"
	// Threshold is 3, so the 4th failure locks.
	var lastCode int
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, authRequest(t, "/api/v1/auth/verify", wallet, "10.0.0.1"))
		lastCode = w.Code
	}

	if lastCode != http.StatusTooManyRequests {
		t.Fatalf("last status = %d, want %d once locked out", lastCode, http.StatusTooManyRequests)
	}
	if got := rec.count(rec.lockouts, metrics.AuthScopeWallet+"/"+metrics.AuthStageVerify); got != 1 {
		t.Errorf("wallet lockouts = %d, want exactly 1 (the transition)", got)
	}
	if got := rec.count(rec.rejected, metrics.AuthScopeWallet+"/"+metrics.AuthStageVerify); got == 0 {
		t.Error("expected rejected-request metric to be recorded while locked")
	}
	// Once locked, the handler must stop being reached at all — that is what
	// protects the challenge store.
	if int(atomic.LoadInt32(calls)) >= 5 {
		t.Errorf("handler ran %d times; the lockout should have short-circuited it", *calls)
	}
}

// A 429 response carries Retry-After so a legitimate client can back off.
func TestAuthGuard_LockedResponse_CarriesRetryAfter(t *testing.T) {
	h, _, _ := newTestGuard(t, http.StatusUnauthorized, 1000)

	const wallet = "GWALLETRETRYAFTER"
	var w *httptest.ResponseRecorder
	for i := 0; i < 5; i++ {
		w = httptest.NewRecorder()
		h.ServeHTTP(w, authRequest(t, "/api/v1/auth/verify", wallet, "10.0.0.2"))
	}

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header missing on a lockout response")
	}
}

// A successful authentication clears the failure history, so a user who
// fumbles a signature and then succeeds is not left carrying a backoff.
func TestAuthGuard_SuccessResetsFailureHistory(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusUnauthorized)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(int(status.Load()))
	})
	rec := newRecordingAuthMetrics()
	h := NewAuthGuard(
		NewLimiter(nil, "authwallet", 1000, time.Minute),
		NewAuthLockout(nil, "wallet", testLockoutConfig()),
		NewAuthLockout(nil, "ip", testLockoutConfig()),
		rec,
		testAuthStages(),
	).Middleware()(handler)

	const wallet = "GWALLETRESET"
	// Three failures: at the threshold, not yet locked.
	for i := 0; i < 3; i++ {
		h.ServeHTTP(httptest.NewRecorder(), authRequest(t, "/api/v1/auth/verify", wallet, "10.0.0.3"))
	}

	// Now succeed, which should clear the history.
	status.Store(http.StatusOK)
	h.ServeHTTP(httptest.NewRecorder(), authRequest(t, "/api/v1/auth/verify", wallet, "10.0.0.3"))

	// Three more failures must again be tolerated without a lockout.
	status.Store(http.StatusUnauthorized)
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, authRequest(t, "/api/v1/auth/verify", wallet, "10.0.0.3"))
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("locked out on attempt %d after a successful auth reset the counter", i+1)
		}
	}
}

// A server error is the server's fault and must not count against the user.
func TestAuthGuard_ServerErrorsDoNotCountAsFailures(t *testing.T) {
	h, rec, _ := newTestGuard(t, http.StatusInternalServerError, 1000)

	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, authRequest(t, "/api/v1/auth/verify", "GWALLET500", "10.0.0.4"))
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("locked out by 5xx responses on attempt %d", i+1)
		}
	}
	if got := rec.count(rec.failures, metrics.AuthScopeWallet+"/"+metrics.AuthStageVerify); got != 0 {
		t.Errorf("recorded %d failures for 5xx responses, want 0", got)
	}
}

// The per-wallet limit must hold even when every request comes from a
// different IP — the case the pre-existing per-IP limiter cannot see.
func TestAuthGuard_PerWalletLimit_HoldsAcrossDistinctIPs(t *testing.T) {
	h, _, calls := newTestGuard(t, http.StatusOK, 5)

	const wallet = "GWALLETDISTRIBUTED"
	limited := 0
	for i := 0; i < 30; i++ {
		w := httptest.NewRecorder()
		// A different source IP every time.
		h.ServeHTTP(w, authRequest(t, "/api/v1/auth/challenge", wallet, fmt.Sprintf("10.1.%d.%d", i/256, i%256)))
		if w.Code == http.StatusTooManyRequests {
			limited++
		}
	}

	if limited == 0 {
		t.Fatal("per-wallet limit never engaged despite 30 requests from distinct IPs")
	}
	if got := int(atomic.LoadInt32(calls)); got > 5 {
		t.Errorf("handler ran %d times, want at most the 5-request wallet limit", got)
	}
}

// The load test the issue asks for: the challenge store cannot be flooded.
//
// It drives concurrent challenge requests for one wallet from many IPs and
// asserts that the number of requests reaching the handler — each of which
// would write one entry into the challenge store — is bounded by the limit,
// not by how many requests the attacker sends.
func TestAuthGuard_ChallengeStoreCannotBeFlooded(t *testing.T) {
	const (
		walletLimit = 10
		attempts    = 500
		workers     = 25
	)

	h, _, calls := newTestGuard(t, http.StatusOK, walletLimit)

	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < attempts/workers; i++ {
				h.ServeHTTP(httptest.NewRecorder(), authRequest(t,
					"/api/v1/auth/challenge",
					"GWALLETFLOOD",
					fmt.Sprintf("10.2.%d.%d", worker, i%256),
				))
			}
		}(worker)
	}
	wg.Wait()

	reached := int(atomic.LoadInt32(calls))
	if reached > walletLimit {
		t.Errorf("%d of %d flood requests reached the challenge store, want at most %d",
			reached, attempts, walletLimit)
	}
	if reached == 0 {
		t.Error("no requests got through at all; the limit is not merely bounding, it is blocking everything")
	}
}

// Routes the guard does not cover must be untouched.
func TestAuthGuard_UnrelatedRoutePassesThrough(t *testing.T) {
	h, rec, calls := newTestGuard(t, http.StatusUnauthorized, 1)

	for i := 0; i < 20; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, authRequest(t, "/api/v1/vaults", "GWALLETOTHER", "10.0.0.5"))
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("unrelated route rate-limited on attempt %d", i+1)
		}
	}
	if got := int(atomic.LoadInt32(calls)); got != 20 {
		t.Errorf("handler ran %d times, want all 20 to pass through", got)
	}
	if got := rec.count(rec.failures, metrics.AuthScopeIP+"/"+metrics.AuthStageVerify); got != 0 {
		t.Errorf("recorded %d failures on an unguarded route, want 0", got)
	}
}

// The handler must still be able to read the body the guard peeked at.
func TestAuthGuard_BodyRemainsReadableByHandler(t *testing.T) {
	var seen string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 512)
		n, _ := r.Body.Read(body)
		seen = string(body[:n])
		w.WriteHeader(http.StatusOK)
	})

	h := NewAuthGuard(
		NewLimiter(nil, "authwallet", 100, time.Minute),
		NewAuthLockout(nil, "wallet", testLockoutConfig()),
		NewAuthLockout(nil, "ip", testLockoutConfig()),
		newRecordingAuthMetrics(),
		testAuthStages(),
	).Middleware()(handler)

	h.ServeHTTP(httptest.NewRecorder(), authRequest(t, "/api/v1/auth/verify", "GWALLETBODY", "10.0.0.6"))

	if !strings.Contains(seen, "GWALLETBODY") {
		t.Errorf("handler read %q, want the original body preserved", seen)
	}
}
