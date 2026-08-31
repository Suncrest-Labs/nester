package stellar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/suncrestlabs/nester/apps/api/internal/breaker"
	"github.com/suncrestlabs/nester/apps/api/internal/retry"
	"github.com/suncrestlabs/nester/apps/api/internal/telemetry"
)

// rpcClient is the single entry point for every Soroban JSON-RPC call in this
// package (nester#1086).
//
// It existed in duplicate before — ContractReader and ContractInvoker each had
// their own rpcCall, and the indexer, the fetcher, and the backfill runner each
// hand-rolled a third variation. None of them retried, none of them checked the
// HTTP status, and a transient connection reset during a balance read surfaced
// as an error on the user's screen. Consolidating them is what makes "one
// retry policy" true rather than aspirational: there is now nowhere else to
// make a Soroban call from.
//
// Layering, outermost first:
//
//	rpcClient.call        retry loop
//	  http.Client         circuit breaker transport (nester#1087)
//	    …                 metrics transport
//	      …               net/http
//
// The retry loop sits OUTSIDE the breaker, deliberately. Each attempt is a
// real connection against the upstream, so each is evidence the breaker should
// weigh; and once the breaker opens, the loop's next attempt fails fast
// locally instead of burning its remaining backoff on an endpoint that is
// already known to be down. Putting retry inside the breaker would hide the
// attempts from it and let a retry storm run to completion against a dead
// endpoint.
type rpcClient struct {
	url    string
	client *http.Client

	runner *retry.Runner
	policy retry.Policy

	// observer records attempts, exhaustions, and call latency. Nil is safe
	// and means "no metrics", so tests and tooling need no wiring.
	observer RPCObserver

	// traced controls whether each call opens a span. The invoker's calls
	// carry transaction context worth separating in a waterfall; the
	// indexer's poll every few seconds does not, and tracing it would swamp
	// the trace store with identical background spans.
	traced bool
}

// RPCOptions is the shared retry wiring that startup installs on every Soroban
// caller in this package. The zero value is usable: it means the package
// defaults and no metrics, which is what a test or the backfill CLI gets.
type RPCOptions struct {
	Runner   *retry.Runner
	Policy   retry.Policy
	Observer RPCObserver
}

// RPCObserver receives one report per logical RPC call, after any retries.
// Implemented by *metrics.Metrics; declared here so this package does not
// depend on the metrics package.
type RPCObserver interface {
	// RecordRPCCall reports a completed call: how many attempts it took, how
	// long the whole thing took including backoff, and whether it ended by
	// exhausting the retry policy.
	RecordRPCCall(attempts int, elapsed time.Duration, exhausted bool)
}

// newRPCClient builds a caller for one Soroban RPC endpoint.
//
// A nil http.Client falls back to a plain one; a nil runner to a real-clock
// one. Both defaults exist so a caller constructed outside startup — a test,
// the backfill CLI — behaves sensibly rather than panicking.
func newRPCClient(rpcURL string, client *http.Client, opts RPCOptions, traced bool) *rpcClient {
	if client == nil {
		client = &http.Client{Timeout: defaultRPCTimeout}
	}
	if opts.Runner == nil {
		opts.Runner = retry.New()
	}
	return &rpcClient{
		url:      rpcURL,
		client:   client,
		runner:   opts.Runner,
		policy:   opts.Policy,
		observer: opts.Observer,
		traced:   traced,
	}
}

// defaultRPCTimeout bounds one HTTP attempt when no client is supplied. The
// retry budget bounds the loop as a whole; this bounds a single hung attempt
// within it.
const defaultRPCTimeout = 30 * time.Second

// rpcStatusError is a non-2xx HTTP response from the RPC endpoint.
//
// It exists so retryability can be decided on the status rather than on
// whether a JSON decode happened to fail: before this, a 502 carrying an HTML
// error page surfaced as an opaque JSON syntax error, which is both useless to
// read and impossible to classify.
type rpcStatusError struct {
	Method     string
	StatusCode int
	Body       string
}

func (e *rpcStatusError) Error() string {
	return fmt.Sprintf("rpc %s returned %d: %s", e.Method, e.StatusCode, e.Body)
}

// idempotentRPCMethods is the closed set of Soroban RPC methods that may be
// retried automatically.
//
// The distinction is the whole safety argument of this feature. A read can be
// repeated freely: worst case the caller pays for a round trip it did not
// need. sendTransaction cannot — a resubmitted envelope is a second attempt to
// move real money, and while Stellar's sequence numbers make a true double
// spend unlikely, "unlikely" is not a property to build a savings product on.
// Writes are durably tracked and resubmitted by the submission record instead
// (see submission_pipeline.go), which owns sequence allocation and
// double-submit prevention precisely because that logic cannot live in a
// stateless retry loop.
//
// An unrecognised method is NOT retried. Failing closed means a Soroban method
// added later is safe by default, and someone must think about it to opt in.
var idempotentRPCMethods = map[string]bool{
	"getEvents":           true,
	"getHealth":           true,
	"getLatestLedger":     true,
	"getLedgerEntries":    true,
	"getNetwork":          true,
	"getTransaction":      true,
	"simulateTransaction": true,

	// Explicitly listed as false rather than merely omitted, so that reading
	// this map answers "why isn't the submit retried" without needing to know
	// the default.
	"sendTransaction": false,
}

// isIdempotentRPCMethod reports whether method may be retried automatically.
func isIdempotentRPCMethod(method string) bool {
	return idempotentRPCMethods[method]
}

// retryableRPCError decides whether a failed attempt is worth repeating.
//
// Retried: transport failures (connection refused, reset, DNS, TLS), read
// timeouts, and the status codes that mean "not now" — 5xx and 429. These are
// the failures that clear on their own, and they are the ones this issue
// exists to stop surfacing to users.
//
// Not retried:
//
//   - breaker.ErrOpen. The breaker has already decided the upstream is unwell;
//     retrying against it is pure latency, and the loop must fail out
//     immediately so the caller gets its fast failure.
//   - Caller cancellation. Nobody is waiting for the answer any more.
//   - Any other 4xx. The upstream is healthy and rejecting our request;
//     repeating it produces the same rejection three times slower.
//   - JSON-RPC application errors. Those arrive inside a 200 and are decoded
//     by the call site, not here — see the note in call().
func retryableRPCError(err error) bool {
	if err == nil {
		return false
	}

	// Checked first: an open breaker is the one failure that must never be
	// retried, and it can be wrapped inside a *url.Error by http.Client.
	if errors.Is(err, breaker.ErrOpen) {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}

	var statusErr *rpcStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode >= 500 || statusErr.StatusCode == http.StatusTooManyRequests
	}

	// A response that ended mid-body is a transport fault wearing a decoder's
	// clothes; the next attempt usually gets a whole one.
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}

	// A deadline that expired is the classic symptom of a degrading endpoint.
	// Note this is the *attempt's* deadline; the loop's own budget expiring is
	// handled by retry.Do, which stops rather than asking this.
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// What remains at this point is a transport error, since every decode and
	// status path above is accounted for.
	var jsonErr *json.SyntaxError
	if errors.As(err, &jsonErr) {
		// A syntactically invalid body from a 2xx response is deterministic
		// garbage, not a blip.
		return false
	}

	return true
}

// call performs one logical JSON-RPC call, retrying it when the method is
// idempotent and the failure looks transient.
//
// result is decoded from the response body. JSON-RPC *application* errors —
// the `error` member of a 200 response — are deliberately not inspected here:
// they are decoded into the caller's own result type, and they are
// deterministic (a bad contract argument fails identically every time), so
// retrying them would triple the latency of every genuine rejection.
func (c *rpcClient) call(ctx context.Context, method string, params, result any) error {
	var span trace.Span
	if c.traced {
		ctx, span = startRPCSpan(ctx, method)
		defer span.End()
	}

	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	if err != nil {
		recordRPCError(span, err)
		return err
	}

	// The idempotency gate. A write reaches retry.Do with a nil predicate, so
	// it runs exactly once and still produces the same metrics and the same
	// typed error shape as a read — the policy is uniform, only the retrying
	// is not.
	var retryable func(error) bool
	if isIdempotentRPCMethod(method) {
		retryable = retryableRPCError
	}

	res, err := c.runner.Do(ctx, c.policy, retryable, func(ctx context.Context) error {
		return c.attempt(ctx, method, body, result)
	})

	c.observe(res, err)

	if err != nil {
		wrapped := fmt.Errorf("rpc %s: %w", method, err)
		recordRPCError(span, wrapped)
		return wrapped
	}
	return nil
}

// attempt performs one HTTP round trip. It is called once per retry, and must
// therefore not consume anything it cannot rebuild — hence body is passed in
// as bytes and wrapped in a fresh reader each time.
func (c *rpcClient) attempt(ctx context.Context, method string, body []byte, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if c.traced {
		recordRPCStatus(ctx, resp.StatusCode)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Bounded read: an error page can be arbitrarily large, and this text
		// ends up in an error string.
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return &rpcStatusError{Method: method, StatusCode: resp.StatusCode, Body: string(payload)}
	}

	// UseNumber throughout: event payloads decode into map[string]any, and
	// Soroban amounts are stroops that routinely exceed float64's exact
	// integer range. Decoding them as float64 would silently round a balance.
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	return decoder.Decode(result)
}

func (c *rpcClient) observe(res retry.Result, err error) {
	if c.observer == nil {
		return
	}
	c.observer.RecordRPCCall(res.Attempts, res.Elapsed, errors.Is(err, retry.ErrExhausted))
}

// recordRPCStatus attaches the HTTP status of an attempt to the active span.
// With retries, a span can carry several of these; the last one is the status
// the call ended on.
func recordRPCStatus(ctx context.Context, statusCode int) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.SetAttributes(semconv.HTTPResponseStatusCode(statusCode))
}

// recordRPCError marks the span failed, tolerating the untraced case where
// there is no span at all.
func recordRPCError(span trace.Span, err error) {
	if span == nil {
		return
	}
	telemetry.RecordError(span, err)
}
