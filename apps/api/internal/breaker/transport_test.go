package breaker

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// countingTransport records how many requests actually reached it, which is
// how "the breaker made no network call" is asserted throughout this file.
type countingTransport struct {
	calls  atomic.Int64
	status int
	err    error
}

func (t *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls.Add(1)
	if t.err != nil {
		return nil, t.err
	}
	return &http.Response{
		StatusCode: t.status,
		Body:       io.NopCloser(strings.NewReader("{}")),
		Request:    req,
	}, nil
}

const (
	sorobanURL = "https://soroban-testnet.example/rpc"
	horizonURL = "https://horizon-testnet.example"
)

func newTestRouter(t *testing.T, clock *fakeClock, cfg Config) (*Router, *Breaker, *Breaker) {
	t.Helper()

	soroban := NewWithClock("soroban_rpc", cfg, nil, clock.Now)
	horizon := NewWithClock("horizon", cfg, nil, clock.Now)

	router := NewRouter()
	if err := router.Register(sorobanURL, soroban); err != nil {
		t.Fatalf("register soroban: %v", err)
	}
	if err := router.Register(horizonURL, horizon); err != nil {
		t.Fatalf("register horizon: %v", err)
	}
	return router, soroban, horizon
}

func doGet(client *http.Client, rawURL string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	return resp, err
}

// The behaviour the whole feature exists for: once the breaker is open, calls
// stop reaching the network entirely.
func TestOpenBreakerMakesNoNetworkCall(t *testing.T) {
	clock := newFakeClock()
	cfg := testConfig()
	router, soroban, _ := newTestRouter(t, clock, cfg)

	upstream := &countingTransport{status: http.StatusInternalServerError}
	client := &http.Client{Transport: router.Transport(upstream)}

	// Drive it open on real 5xx responses.
	for soroban.State() == StateClosed {
		if _, err := doGet(client, sorobanURL); err != nil {
			t.Fatalf("request while closed: %v", err)
		}
	}

	callsWhenOpen := upstream.calls.Load()

	for i := 0; i < 100; i++ {
		_, err := doGet(client, sorobanURL)
		if err == nil {
			t.Fatal("request succeeded while the breaker was open")
		}
		if !errors.Is(err, ErrOpen) {
			t.Fatalf("error = %v, want one matching ErrOpen", err)
		}
	}

	if got := upstream.calls.Load(); got != callsWhenOpen {
		t.Fatalf("%d requests reached the upstream while open, want 0", got-callsWhenOpen)
	}
}

// The rejection must be immediate. A fast failure that still waited on a
// timeout would leave the pile-on exactly as it was.
func TestOpenBreakerFailsFast(t *testing.T) {
	clock := newFakeClock()
	router, soroban, _ := newTestRouter(t, clock, testConfig())

	// An upstream that would block far longer than this test may run.
	slow := &countingTransport{err: errors.New("dial tcp: i/o timeout")}
	client := &http.Client{Transport: router.Transport(slow)}

	for soroban.State() == StateClosed {
		_, _ = doGet(client, sorobanURL)
	}

	started := time.Now()
	for i := 0; i < 1000; i++ {
		if _, err := doGet(client, sorobanURL); err == nil {
			t.Fatal("request succeeded while open")
		}
	}
	elapsed := time.Since(started)

	// 1000 rejections are pure local bookkeeping; anything near a network
	// timeout means the rejection path is not short-circuiting.
	if elapsed > time.Second {
		t.Fatalf("1000 rejections took %v, want a fraction of a second", elapsed)
	}
}

// errors.Is must survive the *url.Error that http.Client.Do wraps a transport
// error in, or no handler could recognise the condition.
func TestOpenErrorSurvivesClientWrapping(t *testing.T) {
	clock := newFakeClock()
	router, soroban, _ := newTestRouter(t, clock, testConfig())

	upstream := &countingTransport{status: http.StatusBadGateway}
	client := &http.Client{Transport: router.Transport(upstream)}

	for soroban.State() == StateClosed {
		_, _ = doGet(client, sorobanURL)
	}

	_, err := doGet(client, sorobanURL)
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("errors.Is(%v, ErrOpen) = false through *url.Error", err)
	}

	var openErr *OpenError
	if !errors.As(err, &openErr) {
		t.Fatalf("errors.As(%v, *OpenError) = false", err)
	}
	if openErr.Name != "soroban_rpc" {
		t.Fatalf("name = %q, want soroban_rpc", openErr.Name)
	}
}

// One upstream failing must not shed the other's traffic.
func TestUpstreamsAreIsolated(t *testing.T) {
	clock := newFakeClock()
	router, soroban, horizon := newTestRouter(t, clock, testConfig())

	// A transport that fails Soroban and serves Horizon.
	upstream := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Host, "soroban") {
			return nil, errors.New("connection refused")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
	})
	client := &http.Client{Transport: router.Transport(upstream)}

	for i := 0; i < 40 && soroban.State() == StateClosed; i++ {
		_, _ = doGet(client, sorobanURL)
	}

	if soroban.State() != StateOpen {
		t.Fatalf("soroban state = %s, want open", soroban.State())
	}
	if horizon.State() != StateClosed {
		t.Fatalf("horizon state = %s, want closed: a Soroban outage shed Horizon traffic", horizon.State())
	}

	// Horizon still serves.
	resp, err := doGet(client, horizonURL+"/accounts/GABC")
	if err != nil {
		t.Fatalf("horizon request failed while soroban was open: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("horizon status = %d, want 200", resp.StatusCode)
	}
}

// A recovering upstream is restored without operator action.
func TestRecoveryThroughTheTransport(t *testing.T) {
	clock := newFakeClock()
	router, soroban, _ := newTestRouter(t, clock, testConfig())

	var healthy atomic.Bool
	upstream := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !healthy.Load() {
			return nil, errors.New("connection refused")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
	})
	client := &http.Client{Transport: router.Transport(upstream)}

	for i := 0; i < 40 && soroban.State() == StateClosed; i++ {
		_, _ = doGet(client, sorobanURL)
	}
	if soroban.State() != StateOpen {
		t.Fatal("precondition: breaker should be open")
	}

	// The upstream comes back, but the breaker is still shedding.
	healthy.Store(true)
	if _, err := doGet(client, sorobanURL); !errors.Is(err, ErrOpen) {
		t.Fatalf("request was admitted before the open period elapsed: %v", err)
	}

	clock.Advance(15 * time.Second)

	// The probe goes through and closes the breaker.
	resp, err := doGet(client, sorobanURL)
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("probe status = %d, want 200", resp.StatusCode)
	}
	if soroban.State() != StateClosed {
		t.Fatalf("state after a successful probe = %s, want closed", soroban.State())
	}

	// Traffic flows again.
	for i := 0; i < 20; i++ {
		if _, err := doGet(client, sorobanURL); err != nil {
			t.Fatalf("request after recovery: %v", err)
		}
	}
}

// Requests to hosts with no breaker registered pass straight through. The
// breaker set comes from configuration, so an unguarded upstream must not be
// silently blocked or silently given a breaker.
func TestUnroutedHostsPassThrough(t *testing.T) {
	clock := newFakeClock()
	router, _, _ := newTestRouter(t, clock, testConfig())

	upstream := &countingTransport{status: http.StatusInternalServerError}
	client := &http.Client{Transport: router.Transport(upstream)}

	for i := 0; i < 50; i++ {
		if _, err := doGet(client, "https://api.coingecko.example/price"); err != nil {
			t.Fatalf("unrouted request failed: %v", err)
		}
	}

	if got := upstream.calls.Load(); got != 50 {
		t.Fatalf("%d of 50 unrouted requests reached the upstream, want all", got)
	}
}

// ---------------------------------------------------------------------------
// Failure classification
// ---------------------------------------------------------------------------

func TestClassify(t *testing.T) {
	cases := []struct {
		name   string
		status int
		err    error
		ctx    context.Context
		want   Outcome
	}{
		{name: "200 is a success", status: http.StatusOK, want: Success},
		{name: "301 is a success", status: http.StatusMovedPermanently, want: Success},

		// The upstream answered correctly; our request was wrong. Counting
		// these would let ordinary user input take the chain offline.
		{name: "400 is a success", status: http.StatusBadRequest, want: Success},
		{name: "404 is a success", status: http.StatusNotFound, want: Success},

		// The upstream asking for less load.
		{name: "429 is a failure", status: http.StatusTooManyRequests, want: Failure},

		{name: "500 is a failure", status: http.StatusInternalServerError, want: Failure},
		{name: "502 is a failure", status: http.StatusBadGateway, want: Failure},
		{name: "503 is a failure", status: http.StatusServiceUnavailable, want: Failure},

		{name: "transport error is a failure", err: errors.New("connection refused"), want: Failure},
		{name: "dns failure is a failure", err: &net.DNSError{Err: "no such host"}, want: Failure},

		// A deadline that expired IS the symptom of a degrading upstream.
		{name: "deadline exceeded is a failure", err: context.DeadlineExceeded, want: Failure},

		// The caller went away; the upstream never got its chance.
		{name: "caller cancellation is ignored", err: context.Canceled, want: Ignored},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := tc.ctx
			if ctx == nil {
				ctx = context.Background()
			}

			var resp *http.Response
			if tc.err == nil {
				resp = &http.Response{StatusCode: tc.status}
			}

			if got := Classify(ctx, resp, tc.err); got != tc.want {
				t.Fatalf("Classify() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A cancelled parent context is recognised even when the transport reports a
// different error, which is what net/http actually does on cancellation.
func TestClassifyDetectsCancellationViaContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := Classify(ctx, nil, errors.New("request canceled"))
	if got != Ignored {
		t.Fatalf("Classify() = %v, want Ignored", got)
	}
}

// A cancelled request must not count against a healthy upstream — otherwise a
// burst of client disconnects could open the breaker over a working endpoint.
func TestCancelledRequestsDoNotOpenTheBreaker(t *testing.T) {
	clock := newFakeClock()
	router, soroban, _ := newTestRouter(t, clock, testConfig())

	upstream := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})
	client := &http.Client{Transport: router.Transport(upstream)}

	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, sorobanURL, nil)
		_, _ = client.Do(req)
	}

	if soroban.State() != StateClosed {
		t.Fatalf("state = %s after 100 cancelled requests, want closed", soroban.State())
	}
	if got := soroban.Snapshot().Total; got != 0 {
		t.Fatalf("%d cancelled requests were counted, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// Routing
// ---------------------------------------------------------------------------

func TestRouterMatchesOnHost(t *testing.T) {
	clock := newFakeClock()
	router, soroban, horizon := newTestRouter(t, clock, testConfig())

	cases := map[string]*Breaker{
		sorobanURL:                         soroban,
		sorobanURL + "?foo=bar":            soroban,
		horizonURL + "/accounts/GABC":      horizon,
		"https://horizon-testnet.example/": horizon,
		"https://unrelated.example/thing":  nil,

		// The soroban upstream is configured with a /rpc path, so the bare
		// root of that host is a different endpoint and is left unguarded.
		// Failing open on an unrecognised path is the safe direction: the
		// alternative is blocking a call the breaker knows nothing about.
		"https://soroban-testnet.example/": nil,
	}

	for rawURL, want := range cases {
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			t.Fatalf("build request for %s: %v", rawURL, err)
		}
		if got := router.For(req.URL); got != want {
			t.Fatalf("For(%s) = %v, want %v", rawURL, got, want)
		}
	}
}

// A local stack can serve both upstreams from one origin on different paths.
// Matching on host alone would silently give them a single shared breaker
// under whichever name registered first.
func TestRouterDistinguishesUpstreamsOnTheSameHost(t *testing.T) {
	clock := newFakeClock()
	soroban := NewWithClock("soroban_rpc", testConfig(), nil, clock.Now)
	horizon := NewWithClock("horizon", testConfig(), nil, clock.Now)

	router := NewRouter()
	if err := router.Register("http://localhost:8000/soroban/rpc", soroban); err != nil {
		t.Fatalf("register soroban: %v", err)
	}
	if err := router.Register("http://localhost:8000", horizon); err != nil {
		t.Fatalf("register horizon: %v", err)
	}

	rpcReq, _ := http.NewRequest(http.MethodGet, "http://localhost:8000/soroban/rpc", nil)
	if got := router.For(rpcReq.URL); got != soroban {
		t.Fatalf("rpc path routed to %v, want the soroban breaker", got)
	}

	horizonReq, _ := http.NewRequest(http.MethodGet, "http://localhost:8000/accounts/GABC", nil)
	if got := router.For(horizonReq.URL); got != horizon {
		t.Fatalf("horizon path routed to %v, want the horizon breaker", got)
	}
}

// A genuine collision is a configuration mistake and must be loud at startup,
// not a silently shared breaker reporting under the wrong name.
func TestRouterRejectsCollidingUpstreams(t *testing.T) {
	router := NewRouter()
	if err := router.Register(sorobanURL, New("soroban_rpc", testConfig(), nil)); err != nil {
		t.Fatalf("first register: %v", err)
	}

	err := router.Register(sorobanURL, New("horizon", testConfig(), nil))
	if err == nil {
		t.Fatal("Register() accepted a colliding upstream, want an error")
	}
	if !strings.Contains(err.Error(), "collides") {
		t.Fatalf("error = %v, want it to name the collision", err)
	}
}

func TestRouterRejectsUnusableURLs(t *testing.T) {
	router := NewRouter()
	b := New("soroban_rpc", testConfig(), nil)

	if err := router.Register("", b); err == nil {
		t.Error("Register(\"\") = nil, want an error")
	}
	if err := router.Register("not-a-url", b); err == nil {
		t.Error("Register(\"not-a-url\") = nil, want an error")
	}
	if err := router.Register(sorobanURL, nil); err == nil {
		t.Error("Register(nil breaker) = nil, want an error")
	}
}

// An empty router guards nothing, which is what a deployment with the breaker
// disabled gets. It must not reject or panic.
func TestEmptyRouterPassesEverythingThrough(t *testing.T) {
	upstream := &countingTransport{status: http.StatusInternalServerError}
	client := &http.Client{Transport: NewRouter().Transport(upstream)}

	for i := 0; i < 20; i++ {
		if _, err := doGet(client, sorobanURL); err != nil {
			t.Fatalf("request through an empty router failed: %v", err)
		}
	}
	if got := upstream.calls.Load(); got != 20 {
		t.Fatalf("%d of 20 requests reached the upstream, want all", got)
	}
}

// Against a real server, to prove the transport composes with net/http rather
// than only with a stub round tripper.
func TestTransportAgainstRealServer(t *testing.T) {
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	clock := newFakeClock()
	b := NewWithClock("soroban_rpc", testConfig(), nil, clock.Now)
	router := NewRouter()
	if err := router.Register(server.URL, b); err != nil {
		t.Fatalf("register: %v", err)
	}

	client := &http.Client{Transport: router.Transport(nil)}

	for i := 0; i < 40 && b.State() == StateClosed; i++ {
		_, _ = doGet(client, server.URL)
	}
	if b.State() != StateOpen {
		t.Fatalf("state = %s, want open", b.State())
	}

	hitsWhenOpen := hits.Load()
	for i := 0; i < 25; i++ {
		if _, err := doGet(client, server.URL); !errors.Is(err, ErrOpen) {
			t.Fatalf("request %d = %v, want ErrOpen", i, err)
		}
	}
	if got := hits.Load(); got != hitsWhenOpen {
		t.Fatalf("%d requests reached the real server while open, want 0", got-hitsWhenOpen)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
