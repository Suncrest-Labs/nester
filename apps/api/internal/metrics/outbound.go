package metrics

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

// Upstream identifies an external dependency in metrics.
//
// It is a named string type rather than a bare string so that every call site
// must reference one of the constants below. That is the mechanism enforcing
// bounded cardinality on the upstream label: hostnames and URLs are never
// used, because a redirect, a misconfiguration, or a per-tenant endpoint
// would each mint new series, and a URL can carry credentials in its
// userinfo or query.
type Upstream string

const (
	UpstreamSorobanRPC   Upstream = "soroban_rpc"
	UpstreamHorizon      Upstream = "horizon"
	UpstreamCoinGecko    Upstream = "coingecko"
	UpstreamDeFiLlama    Upstream = "defillama"
	UpstreamAnthropic    Upstream = "anthropic_relay"
	UpstreamIntelligence Upstream = "intelligence"
	UpstreamOther        Upstream = "other"
)

// Transport error kinds. A closed set: the raw error text from a failed
// request can contain the target URL, and URLs can carry query parameters and
// credentials, so it must never reach a label.
const (
	errKindTimeout  = "timeout"
	errKindCanceled = "canceled"
	errKindDNS      = "dns"
	errKindConnect  = "connect"
	errKindOther    = "other"
)

// roundTripper instruments an http.RoundTripper with request count, latency,
// and transport error count for one upstream.
type roundTripper struct {
	m        *Metrics
	next     http.RoundTripper
	upstream Upstream
}

// InstrumentClient wraps an http.Client's transport so its requests are
// recorded against the given upstream.
//
// It mutates the client in place and returns it, which suits the way clients
// are constructed inline throughout this codebase. Passing a nil client
// returns nil so callers need not branch.
//
// Only the request method and the response status class are read. Headers are
// never inspected, so Authorization headers, API keys, and bearer tokens
// cannot reach a metric. The request body is never read, which also means
// instrumentation cannot disturb it.
func (m *Metrics) InstrumentClient(client *http.Client, upstream Upstream) *http.Client {
	if client == nil {
		return nil
	}

	next := client.Transport
	if next == nil {
		next = http.DefaultTransport
	}

	client.Transport = &roundTripper{m: m, next: next, upstream: upstream}
	return client
}

// RoundTrip implements http.RoundTripper.
func (t *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	upstream := string(t.upstream)
	method := normalizeMethod(req.Method)

	startedAt := time.Now()
	resp, err := t.next.RoundTrip(req)
	elapsed := time.Since(startedAt).Seconds()

	t.m.outboundDuration.WithLabelValues(upstream, method).Observe(elapsed)

	if err != nil {
		// No response means no status class, so these failures are counted
		// separately rather than folded into requests_total under a fake
		// class. Alert on errors_total and 5xx together for total failure.
		t.m.outboundErrorsTotal.WithLabelValues(upstream, classifyTransportError(err)).Inc()
		return resp, err
	}

	t.m.outboundRequestsTotal.WithLabelValues(upstream, method, statusClass(resp.StatusCode)).Inc()
	return resp, nil
}

// classifyTransportError maps a transport failure to one of the bounded error
// kinds. The error's text is deliberately never used.
func classifyTransportError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return errKindTimeout
	case errors.Is(err, context.Canceled):
		return errKindCanceled
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return errKindDNS
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return errKindTimeout
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return errKindConnect
	}

	return errKindOther
}

var _ http.RoundTripper = (*roundTripper)(nil)
