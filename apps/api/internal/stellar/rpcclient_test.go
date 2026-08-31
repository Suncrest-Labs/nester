package stellar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/breaker"
	"github.com/suncrestlabs/nester/apps/api/internal/retry"
)

// recordingObserver captures what the retry helper reports, so the metrics
// contract can be asserted without a Prometheus registry.
type recordingObserver struct {
	calls     int
	attempts  int
	exhausted int
	elapsed   time.Duration
}

func (o *recordingObserver) RecordRPCCall(attempts int, elapsed time.Duration, exhausted bool) {
	o.calls++
	o.attempts += attempts
	o.elapsed += elapsed
	if exhausted {
		o.exhausted++
	}
}

// noSleepOptions returns retry options that never actually wait, so tests
// exercise the full attempt schedule in microseconds rather than seconds.
func noSleepOptions(observer RPCObserver) RPCOptions {
	return RPCOptions{
		Runner: retry.NewWithClock(
			time.Now,
			func(context.Context, time.Duration) error { return nil },
			func(time.Duration) time.Duration { return 0 },
		),
		Policy: retry.Policy{
			MaxAttempts: 3,
			BaseDelay:   time.Millisecond,
			MaxDelay:    time.Millisecond,
			Budget:      10 * time.Second,
		},
		Observer: observer,
	}
}

// rpcServer counts requests per JSON-RPC method and replies with whatever the
// handler decides, so a test can assert exactly how many times a method was
// actually sent over the wire.
type rpcServer struct {
	server   *httptest.Server
	requests atomic.Int64
	// byMethod counts calls per method name read from the request body,
	// which is how the write-is-not-retried assertion is made.
	byMethod sync.Map
}

func newRPCServer(t *testing.T, handler func(method string, count int, w http.ResponseWriter)) *rpcServer {
	t.Helper()

	s := &rpcServer{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		s.requests.Add(1)
		count := s.increment(req.Method)
		handler(req.Method, count, w)
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *rpcServer) increment(method string) int {
	actual, _ := s.byMethod.LoadOrStore(method, new(atomic.Int64))
	return int(actual.(*atomic.Int64).Add(1))
}

func (s *rpcServer) count(method string) int {
	value, ok := s.byMethod.Load(method)
	if !ok {
		return 0
	}
	return int(value.(*atomic.Int64).Load())
}

// roundTripFunc stands in for a transport whose failures are scripted, so
// tests can produce errors net/http would otherwise have to be provoked into.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func writeRPCResult(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": result})
}

// ---------------------------------------------------------------------------
// The idempotency gate — the safety property of this feature
// ---------------------------------------------------------------------------

// A write must never be repeated automatically. A resubmitted envelope is a
// second attempt to move real money; durability for the write path comes from
// the submission record, not from trying again here.
func TestWritesAreNeverRetried(t *testing.T) {
	server := newRPCServer(t, func(_ string, _ int, w http.ResponseWriter) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	observer := &recordingObserver{}
	client := newRPCClient(server.server.URL, server.server.Client(), noSleepOptions(observer), false)

	var result map[string]any
	err := client.call(context.Background(), "sendTransaction", map[string]any{"transaction": "AAAA"}, &result)

	if err == nil {
		t.Fatal("call() = nil, want the 500 to surface")
	}
	if got := server.count("sendTransaction"); got != 1 {
		t.Fatalf("sendTransaction was sent %d times, want exactly 1: a write must never be retried", got)
	}
	if observer.attempts != 1 {
		t.Fatalf("observed attempts = %d, want 1", observer.attempts)
	}
}

// The gate is a property of the method, not of the error: even a textbook
// transient failure does not earn a write a second attempt.
func TestWriteIsNotRetriedOnATransportFailure(t *testing.T) {
	calls := 0
	client := newRPCClient("http://127.0.0.1:1/rpc", &http.Client{Timeout: time.Second}, noSleepOptions(nil), false)
	client.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("connection refused")
	})}

	var result map[string]any
	_ = client.call(context.Background(), "sendTransaction", nil, &result)

	if calls != 1 {
		t.Fatalf("sendTransaction attempted %d times on a transport failure, want 1", calls)
	}
}

// A method nobody has classified fails closed. That way a Soroban method added
// to the RPC later is safe by default and someone has to think about it to opt
// in.
func TestUnknownMethodsAreNotRetried(t *testing.T) {
	server := newRPCServer(t, func(_ string, _ int, w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadGateway)
	})

	client := newRPCClient(server.server.URL, server.server.Client(), noSleepOptions(nil), false)

	var result map[string]any
	_ = client.call(context.Background(), "someMethodInventedNextYear", nil, &result)

	if got := server.count("someMethodInventedNextYear"); got != 1 {
		t.Fatalf("unknown method was sent %d times, want 1", got)
	}
}

func TestIdempotencyClassification(t *testing.T) {
	retried := []string{
		"getEvents", "getHealth", "getLatestLedger",
		"getLedgerEntries", "getNetwork", "getTransaction", "simulateTransaction",
	}
	for _, method := range retried {
		if !isIdempotentRPCMethod(method) {
			t.Errorf("%s should be retryable", method)
		}
	}

	for _, method := range []string{"sendTransaction", "", "unknown"} {
		if isIdempotentRPCMethod(method) {
			t.Errorf("%s must not be retryable", method)
		}
	}
}

// ---------------------------------------------------------------------------
// Reads are retried
// ---------------------------------------------------------------------------

// The reason the feature exists: a transient failure during a read is absorbed
// instead of reaching the user.
func TestReadRecoversFromATransientFailure(t *testing.T) {
	server := newRPCServer(t, func(_ string, count int, w http.ResponseWriter) {
		if count < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		writeRPCResult(w, map[string]any{"sequence": 493812})
	})

	observer := &recordingObserver{}
	client := newRPCClient(server.server.URL, server.server.Client(), noSleepOptions(observer), false)

	var resp struct {
		Result struct {
			Sequence uint64 `json:"sequence"`
		} `json:"result"`
	}
	if err := client.call(context.Background(), "getLatestLedger", nil, &resp); err != nil {
		t.Fatalf("call() = %v, want nil after recovery", err)
	}

	if resp.Result.Sequence != 493812 {
		t.Fatalf("sequence = %d, want 493812", resp.Result.Sequence)
	}
	if got := server.count("getLatestLedger"); got != 3 {
		t.Fatalf("sent %d times, want 3", got)
	}
	if observer.attempts != 3 {
		t.Fatalf("observed attempts = %d, want 3", observer.attempts)
	}
	if observer.exhausted != 0 {
		t.Fatalf("observed %d exhaustions on a call that succeeded", observer.exhausted)
	}
}

// The request body must be replayable: a retry that sent an empty body would
// fail against a real endpoint in a way no unit test of the loop alone would
// catch.
func TestRequestBodyIsResentOnEveryAttempt(t *testing.T) {
	var bodies []string
	server := newRPCServer(t, func(_ string, count int, w http.ResponseWriter) {
		if count < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeRPCResult(w, map[string]any{"ok": true})
	})

	// Re-wrap the handler so the raw body of each attempt is captured.
	server.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw map[string]any
		_ = json.NewDecoder(r.Body).Decode(&raw)
		encoded, _ := json.Marshal(raw)
		bodies = append(bodies, string(encoded))

		if len(bodies) < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeRPCResult(w, map[string]any{"ok": true})
	})

	client := newRPCClient(server.server.URL, server.server.Client(), noSleepOptions(nil), false)

	var result map[string]any
	if err := client.call(context.Background(), "getEvents", map[string]any{"startLedger": 42}, &result); err != nil {
		t.Fatalf("call() = %v, want nil", err)
	}

	if len(bodies) != 2 {
		t.Fatalf("captured %d bodies, want 2", len(bodies))
	}
	if bodies[0] != bodies[1] {
		t.Fatalf("retry sent a different body:\n first: %s\nsecond: %s", bodies[0], bodies[1])
	}
	if bodies[1] == "" || bodies[1] == "null" {
		t.Fatalf("retry sent an empty body: %q", bodies[1])
	}
}

// ---------------------------------------------------------------------------
// Failure classification
// ---------------------------------------------------------------------------

func TestRetryableRPCError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},

		// The breaker has already decided the upstream is down; retrying past
		// it is pure latency and defeats the fast-fail it exists to provide.
		{"breaker open", &breaker.OpenError{Name: "soroban_rpc"}, false},
		{"wrapped breaker open", fmt.Errorf("do: %w", &breaker.OpenError{}), false},

		{"caller cancelled", context.Canceled, false},
		{"deadline exceeded", context.DeadlineExceeded, true},

		{"500", &rpcStatusError{StatusCode: 500}, true},
		{"502", &rpcStatusError{StatusCode: 502}, true},
		{"503", &rpcStatusError{StatusCode: 503}, true},
		{"429", &rpcStatusError{StatusCode: 429}, true},

		// The upstream is healthy and rejecting our request; repeating it
		// produces the same rejection three times slower.
		{"400", &rpcStatusError{StatusCode: 400}, false},
		{"404", &rpcStatusError{StatusCode: 404}, false},
		{"wrapped 500", fmt.Errorf("rpc: %w", &rpcStatusError{StatusCode: 500}), true},

		{"transport failure", errors.New("connection reset by peer"), true},
		{"malformed json", &json.SyntaxError{}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryableRPCError(tc.err); got != tc.want {
				t.Fatalf("retryableRPCError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// A 4xx is the upstream working correctly. Retrying it would let ordinary bad
// input cost three round trips and three times the latency.
func TestClientErrorsAreNotRetried(t *testing.T) {
	server := newRPCServer(t, func(_ string, _ int, w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadRequest)
	})

	client := newRPCClient(server.server.URL, server.server.Client(), noSleepOptions(nil), false)

	var result map[string]any
	err := client.call(context.Background(), "getEvents", nil, &result)
	if err == nil {
		t.Fatal("call() = nil, want the 400 to surface")
	}
	if got := server.count("getEvents"); got != 1 {
		t.Fatalf("a 400 was retried %d times, want 1 attempt", got)
	}
	if errors.Is(err, retry.ErrExhausted) {
		t.Fatal("a declined 4xx was reported as retry exhaustion")
	}
}

// An open circuit breaker must stop the retry loop dead, not have it burn its
// whole schedule against an endpoint already known to be down.
func TestOpenBreakerStopsTheRetryLoop(t *testing.T) {
	calls := 0
	client := newRPCClient("https://soroban.example/rpc", &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, &breaker.OpenError{Name: "soroban_rpc", RetryIn: 15 * time.Second}
		}),
	}, noSleepOptions(nil), false)

	var result map[string]any
	err := client.call(context.Background(), "getEvents", nil, &result)

	if calls != 1 {
		t.Fatalf("attempted %d times against an open breaker, want 1", calls)
	}
	if !errors.Is(err, breaker.ErrOpen) {
		t.Fatalf("call() = %v, want it to carry breaker.ErrOpen", err)
	}
	if errors.Is(err, retry.ErrExhausted) {
		t.Fatal("a breaker rejection was reported as retry exhaustion")
	}
}

// A non-2xx must surface as a typed status error rather than as whatever the
// JSON decoder made of an HTML error page — which is both unreadable and
// impossible to classify.
func TestNonSuccessStatusBecomesATypedError(t *testing.T) {
	server := newRPCServer(t, func(_ string, _ int, w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("<html>go away</html>"))
	})

	client := newRPCClient(server.server.URL, server.server.Client(), noSleepOptions(nil), false)

	var result map[string]any
	err := client.call(context.Background(), "getEvents", nil, &result)

	var statusErr *rpcStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("call() = %v, want an *rpcStatusError", err)
	}
	if statusErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", statusErr.StatusCode)
	}
	if statusErr.Method != "getEvents" {
		t.Fatalf("method = %q, want getEvents", statusErr.Method)
	}
}

// ---------------------------------------------------------------------------
// Exhaustion
// ---------------------------------------------------------------------------

// Exhaustion must be recognisable as exhaustion so the API answers 503 rather
// than a generic 500, and must keep its cause so the log still names what
// broke.
func TestExhaustionProducesATypedError(t *testing.T) {
	server := newRPCServer(t, func(_ string, _ int, w http.ResponseWriter) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	observer := &recordingObserver{}
	client := newRPCClient(server.server.URL, server.server.Client(), noSleepOptions(observer), false)

	var result map[string]any
	err := client.call(context.Background(), "getEvents", nil, &result)

	if !errors.Is(err, retry.ErrExhausted) {
		t.Fatalf("call() = %v, want it to match retry.ErrExhausted", err)
	}

	var statusErr *rpcStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("call() = %v, want the underlying status error preserved", err)
	}
	if statusErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("underlying status = %d, want 503", statusErr.StatusCode)
	}

	if got := server.count("getEvents"); got != 3 {
		t.Fatalf("sent %d times, want 3 (the attempt cap)", got)
	}
	if observer.exhausted != 1 {
		t.Fatalf("observed %d exhaustions, want 1", observer.exhausted)
	}
	if observer.attempts != 3 {
		t.Fatalf("observed attempts = %d, want 3", observer.attempts)
	}
}

// Every completed call is reported exactly once, whatever the outcome, or the
// attempts-per-call ratio the metrics are read as would be wrong.
func TestObserverSeesEveryCallExactlyOnce(t *testing.T) {
	server := newRPCServer(t, func(_ string, _ int, w http.ResponseWriter) {
		writeRPCResult(w, map[string]any{"ok": true})
	})

	observer := &recordingObserver{}
	client := newRPCClient(server.server.URL, server.server.Client(), noSleepOptions(observer), false)

	for i := 0; i < 5; i++ {
		var result map[string]any
		if err := client.call(context.Background(), "getEvents", nil, &result); err != nil {
			t.Fatalf("call() = %v", err)
		}
	}

	if observer.calls != 5 {
		t.Fatalf("observer saw %d calls, want 5", observer.calls)
	}
	if observer.attempts != 5 {
		t.Fatalf("observed attempts = %d, want 5 for five first-try successes", observer.attempts)
	}
}

// A nil observer must be safe, so a client built outside startup needs no
// wiring.
func TestNilObserverIsSafe(t *testing.T) {
	server := newRPCServer(t, func(_ string, _ int, w http.ResponseWriter) {
		writeRPCResult(w, map[string]any{"ok": true})
	})

	client := newRPCClient(server.server.URL, server.server.Client(), RPCOptions{}, false)

	var result map[string]any
	if err := client.call(context.Background(), "getEvents", nil, &result); err != nil {
		t.Fatalf("call() = %v, want nil", err)
	}
}

// ---------------------------------------------------------------------------
// Decoding
// ---------------------------------------------------------------------------

// Soroban amounts are stroops and routinely exceed float64's exact integer
// range. Decoding them as float64 would silently round a balance, so every
// call decodes with UseNumber.
func TestLargeIntegersDecodeWithoutLosingPrecision(t *testing.T) {
	const exact = "9007199254740993" // 2^53 + 1: the first integer float64 cannot represent

	server := newRPCServer(t, func(_ string, _ int, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"amount":` + exact + `}}`))
	})

	client := newRPCClient(server.server.URL, server.server.Client(), noSleepOptions(nil), false)

	var resp struct {
		Result map[string]any `json:"result"`
	}
	if err := client.call(context.Background(), "getEvents", nil, &resp); err != nil {
		t.Fatalf("call() = %v", err)
	}

	number, ok := resp.Result["amount"].(json.Number)
	if !ok {
		t.Fatalf("amount decoded as %T, want json.Number", resp.Result["amount"])
	}
	if number.String() != exact {
		t.Fatalf("amount = %s, want %s", number.String(), exact)
	}
}

// A JSON-RPC application error arrives inside a 200 and is decoded by the call
// site, not retried: a bad contract argument fails identically every time, so
// repeating it would triple the latency of every genuine rejection.
func TestApplicationErrorsInsideA200AreNotRetried(t *testing.T) {
	server := newRPCServer(t, func(_ string, _ int, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"error":   map[string]any{"code": -32602, "message": "startLedger must be within the retention window"},
		})
	})

	client := newRPCClient(server.server.URL, server.server.Client(), noSleepOptions(nil), false)

	var resp struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := client.call(context.Background(), "getEvents", nil, &resp); err != nil {
		t.Fatalf("call() = %v, want nil: a 200 is a successful round trip", err)
	}
	if got := server.count("getEvents"); got != 1 {
		t.Fatalf("an application error was retried %d times, want 1", got)
	}
	if resp.Error == nil {
		t.Fatal("the application error was not decoded for the call site")
	}
}
