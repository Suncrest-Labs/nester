// Package chaos provides a protocol-agnostic http.RoundTripper wrapper that
// deterministically induces the failure modes real external dependencies
// produce in production, so tests can assert the system degrades correctly
// under them rather than only exercising the success path (issue #1055).
//
// This complements internal/chaintest, which is a full fake Soroban/Horizon
// server purpose-built for the chain boundary's own JSON-RPC and REST
// shapes. Transport wraps any *http.Client via SetHTTPClient (a hook every
// outbound client in this codebase already exposes for metrics
// instrumentation), so the same injection layer drives scenarios against
// Anthropic, CoinGecko, DeFiLlama, Paystack, and Flutterwave without a
// bespoke fake server per dependency.
//
// Every fault is deterministic given a Script: no fault here depends on
// wall-clock timing to decide *whether* to fail, only optionally on how
// long a caller waits before its own context deadline ends the attempt. A
// chaos test that cannot be replayed from its Script is a flake generator,
// which is worse than no test — see the package doc on chaintest for the
// same principle applied to the chain boundary.
package chaos

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Fault is one injectable failure mode. The zero value, FaultNone, passes
// the request through to the wrapped transport unmodified.
type Fault int

const (
	FaultNone Fault = iota

	// FaultLatency delays the request by Script.Latency before forwarding it
	// to the wrapped transport. Combine with a caller-side timeout shorter
	// than Latency to exercise "upstream responds, but too late" — the
	// distinct case from FaultTimeout below, which never responds at all.
	FaultLatency

	// FaultTimeout blocks until the request's context is done, then returns
	// ctx.Err(). It never reaches the wrapped transport. This is what a
	// hung upstream looks like from the caller's side: no response, no
	// error, until the caller's own deadline ends it.
	FaultTimeout

	// FaultConnectionReset returns a network-level error immediately,
	// without forwarding to the wrapped transport — indistinguishable to
	// the caller from a TCP RST or a dropped connection mid-request.
	FaultConnectionReset

	// FaultMalformedBody forwards the request, then replaces a successful
	// response's body with Script.MalformedBody (default: truncated JSON).
	// The status code and headers from the real response are preserved, so
	// this specifically exercises "the body lied about being well-formed,"
	// not "the upstream reported an error."
	FaultMalformedBody

	// FaultWrongContentType behaves like FaultMalformedBody but only
	// rewrites the Content-Type header (default: text/html), leaving the
	// body untouched — the "got an error page instead of JSON" case.
	FaultWrongContentType

	// FaultPartialStream forwards the request, then truncates a successful
	// response's body after Script.PartialStreamBytes bytes and closes the
	// connection without an EOF the reader can distinguish from a clean
	// end — the untested Anthropic streaming-path case #1055 names
	// explicitly.
	FaultPartialStream

	// FaultRateLimited returns HTTP 429 immediately without forwarding to
	// the wrapped transport. Script.RetryAfter, if non-zero, is sent as a
	// Retry-After header; a zero value omits the header entirely, which is
	// the "429 with no guidance on when to retry" case named in the issue.
	FaultRateLimited

	// FaultServerError returns Script.ServerErrorStatus (default 500)
	// immediately without forwarding to the wrapped transport.
	FaultServerError

	// FaultSlowDrip forwards the request, then writes a successful
	// response's body one byte at a time with Script.SlowDripInterval
	// between bytes — the "holds a connection open" case that starves a
	// shared pool, which the bulkhead scenario tests against.
	FaultSlowDrip
)

func (f Fault) String() string {
	switch f {
	case FaultNone:
		return "none"
	case FaultLatency:
		return "latency"
	case FaultTimeout:
		return "timeout"
	case FaultConnectionReset:
		return "connection_reset"
	case FaultMalformedBody:
		return "malformed_body"
	case FaultWrongContentType:
		return "wrong_content_type"
	case FaultPartialStream:
		return "partial_stream"
	case FaultRateLimited:
		return "rate_limited"
	case FaultServerError:
		return "server_error"
	case FaultSlowDrip:
		return "slow_drip"
	default:
		return "unknown"
	}
}

// ErrConnectionReset is returned by RoundTrip for FaultConnectionReset. It
// deliberately does not wrap a net.OpError — callers should treat any
// RoundTrip error as a transport failure, exactly as they must for a real
// connection reset, rather than special-casing this package's error type.
var ErrConnectionReset = errors.New("chaos: connection reset by peer")

// Script is a deterministic, replayable fault configuration. The same
// Script always produces the same sequence of injected faults for the same
// sequence of requests — there is no seeded randomness in this package,
// unlike a scenario-generation layer built on top of it might have; Script
// itself is the unit of reproducibility.
type Script struct {
	// Fault is applied to every request RoundTrip handles, in order, unless
	// Sequence is set (see below). A Script with neither Fault nor Sequence
	// set behaves as FaultNone — every request passes through.
	Fault Fault

	// Sequence, if non-empty, overrides Fault: request N (0-indexed) uses
	// Sequence[N], and any request beyond len(Sequence) reuses the last
	// entry. This drives scenarios like "the first two calls fail, then
	// the circuit breaker's half-open probe succeeds."
	Sequence []Fault

	Latency            time.Duration
	MalformedBody      []byte
	PartialStreamBytes int
	RetryAfter         time.Duration
	ServerErrorStatus  int
	SlowDripInterval   time.Duration
}

func (s Script) faultFor(callIndex int) Fault {
	if len(s.Sequence) == 0 {
		return s.Fault
	}
	if callIndex < len(s.Sequence) {
		return s.Sequence[callIndex]
	}
	return s.Sequence[len(s.Sequence)-1]
}

func (s Script) malformedBody() []byte {
	if s.MalformedBody != nil {
		return s.MalformedBody
	}
	return []byte(`{"status":`)
}

func (s Script) serverErrorStatus() int {
	if s.ServerErrorStatus != 0 {
		return s.ServerErrorStatus
	}
	return http.StatusInternalServerError
}

func (s Script) slowDripInterval() time.Duration {
	if s.SlowDripInterval > 0 {
		return s.SlowDripInterval
	}
	return 10 * time.Millisecond
}

// Transport wraps an http.RoundTripper and applies a Script's faults before
// (or instead of) delegating to it. The zero value is not usable; construct
// with New.
type Transport struct {
	next   http.RoundTripper
	script Script
	calls  atomic.Int64

	mu      sync.Mutex
	history []Fault
}

// New wraps next (http.DefaultTransport if nil) with the given Script.
func New(next http.RoundTripper, script Script) *Transport {
	if next == nil {
		next = http.DefaultTransport
	}
	return &Transport{next: next, script: script}
}

// CallCount returns how many requests RoundTrip has handled so far.
func (t *Transport) CallCount() int {
	return int(t.calls.Load())
}

// History returns the fault applied to each call, in order — useful for
// asserting a Sequence-based scenario actually exercised the faults it
// claimed to (e.g. "the breaker opened after exactly 3 failures").
func (t *Transport) History() []Fault {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Fault, len(t.history))
	copy(out, t.history)
	return out
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	callIndex := int(t.calls.Add(1)) - 1
	fault := t.script.faultFor(callIndex)

	t.mu.Lock()
	t.history = append(t.history, fault)
	t.mu.Unlock()

	switch fault {
	case FaultNone:
		return t.next.RoundTrip(req)

	case FaultLatency:
		if err := sleepCtx(req.Context(), t.script.Latency); err != nil {
			return nil, err
		}
		return t.next.RoundTrip(req)

	case FaultTimeout:
		<-req.Context().Done()
		return nil, req.Context().Err()

	case FaultConnectionReset:
		return nil, ErrConnectionReset

	case FaultRateLimited:
		return t.buildFault429(req), nil

	case FaultServerError:
		return t.buildFaultStatus(req, t.script.serverErrorStatus()), nil

	case FaultMalformedBody:
		resp, err := t.next.RoundTrip(req)
		if err != nil || resp == nil {
			return resp, err
		}
		return t.replaceBody(resp, t.script.malformedBody()), nil

	case FaultWrongContentType:
		resp, err := t.next.RoundTrip(req)
		if err != nil || resp == nil {
			return resp, err
		}
		resp.Header.Set("Content-Type", "text/html")
		return resp, nil

	case FaultPartialStream:
		resp, err := t.next.RoundTrip(req)
		if err != nil || resp == nil {
			return resp, err
		}
		return t.truncateStream(resp), nil

	case FaultSlowDrip:
		resp, err := t.next.RoundTrip(req)
		if err != nil || resp == nil {
			return resp, err
		}
		return t.slowDrip(req.Context(), resp), nil

	default:
		return t.next.RoundTrip(req)
	}
}

func (t *Transport) buildFault429(req *http.Request) *http.Response {
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	if t.script.RetryAfter > 0 {
		header.Set("Retry-After", formatRetryAfterSeconds(t.script.RetryAfter))
	}
	body := io.NopCloser(bytes.NewReader([]byte(`{"error":"rate limited"}`)))
	return &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Status:     http.StatusText(http.StatusTooManyRequests),
		Header:     header,
		Body:       body,
		Request:    req,
	}
}

func (t *Transport) buildFaultStatus(req *http.Request, status int) *http.Response {
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	body := io.NopCloser(bytes.NewReader([]byte(`{"error":"upstream failure"}`)))
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     header,
		Body:       body,
		Request:    req,
	}
}

func (t *Transport) replaceBody(resp *http.Response, body []byte) *http.Response {
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	return resp
}

func (t *Transport) truncateStream(resp *http.Response) *http.Response {
	n := t.script.PartialStreamBytes
	if n <= 0 {
		n = 16
	}
	full, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		full = nil
	}
	if n > len(full) {
		n = len(full)
	}
	resp.Body = io.NopCloser(bytes.NewReader(full[:n]))
	resp.ContentLength = -1
	resp.Header.Del("Content-Length")
	return resp
}

// slowDrip replaces resp.Body with a reader that delivers the original body
// one byte at a time, so a reader on the other end sees the connection held
// open for len(body) * SlowDripInterval — the "hold a connection open"
// bulkhead-isolation case.
func (t *Transport) slowDrip(ctx context.Context, resp *http.Response) *http.Response {
	full, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		full = nil
	}
	resp.Body = &slowDripReader{ctx: ctx, remaining: full, interval: t.script.slowDripInterval()}
	resp.ContentLength = -1
	resp.Header.Del("Content-Length")
	return resp
}

type slowDripReader struct {
	ctx       context.Context
	remaining []byte
	interval  time.Duration
}

func (r *slowDripReader) Read(p []byte) (int, error) {
	if len(r.remaining) == 0 {
		return 0, io.EOF
	}
	if err := sleepCtx(r.ctx, r.interval); err != nil {
		return 0, err
	}
	p[0] = r.remaining[0]
	r.remaining = r.remaining[1:]
	return 1, nil
}

func (r *slowDripReader) Close() error {
	r.remaining = nil
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func formatRetryAfterSeconds(d time.Duration) string {
	seconds := int(d.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}

var _ http.RoundTripper = (*Transport)(nil)
