package breaker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Router maps an outbound request to the breaker guarding its upstream.
//
// Routing is by URL rather than by client because ContractInvoker talks to
// Soroban RPC and Horizon through a single *http.Client — it builds and
// simulates against the RPC, then reads the operator account from Horizon.
// Keying the breaker to the client would force those two upstreams to share
// failure state, so a Horizon outage would shed Soroban traffic. Routing per
// request keeps them independent without splitting any client.
//
// Only configured upstreams are routed; anything else passes through
// unguarded. The set of breakers therefore comes from configuration and is
// fixed at startup, never derived from the request, so neither the breaker
// count nor the metric label set can be moved by traffic.
type Router struct {
	entries []routeEntry
}

type routeEntry struct {
	host    string
	prefix  string
	breaker *Breaker
}

// NewRouter returns an empty router. A router with no entries passes every
// request through, which is what a deployment with the breaker disabled gets.
func NewRouter() *Router { return &Router{} }

// Register routes requests matching rawURL's host and path prefix to b.
//
// The path prefix matters for the single-host case: a local stack can serve
// Horizon and Soroban RPC from one origin on different paths, and matching on
// host alone would silently give them one breaker under whichever name
// registered first.
func (r *Router) Register(rawURL string, b *Breaker) error {
	if b == nil {
		return errors.New("breaker is required")
	}

	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("parse %s upstream url: %w", b.Name(), err)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%s upstream url %q has no host", b.Name(), rawURL)
	}

	entry := routeEntry{
		host:   strings.ToLower(parsed.Host),
		prefix: strings.TrimSuffix(parsed.Path, "/"),
	}

	for _, existing := range r.entries {
		if existing.host == entry.host && existing.prefix == entry.prefix {
			return fmt.Errorf(
				"upstream %s collides with %s: both resolve to %s%s",
				b.Name(), existing.breaker.Name(), entry.host, entry.prefix,
			)
		}
	}

	entry.breaker = b
	r.entries = append(r.entries, entry)
	return nil
}

// Breakers returns every registered breaker, in registration order.
func (r *Router) Breakers() []*Breaker {
	out := make([]*Breaker, 0, len(r.entries))
	for _, entry := range r.entries {
		out = append(out, entry.breaker)
	}
	return out
}

// For returns the breaker guarding u, or nil when the URL is not a guarded
// upstream. The longest matching path prefix wins.
func (r *Router) For(u *url.URL) *Breaker {
	if u == nil {
		return nil
	}

	host := strings.ToLower(u.Host)
	var best *routeEntry

	for i := range r.entries {
		entry := &r.entries[i]
		if entry.host != host {
			continue
		}
		if entry.prefix != "" && !strings.HasPrefix(u.Path, entry.prefix) {
			continue
		}
		if best == nil || len(entry.prefix) > len(best.prefix) {
			best = entry
		}
	}

	if best == nil {
		return nil
	}
	return best.breaker
}

// Transport wraps next so requests to guarded upstreams pass through their
// breaker. A nil next uses http.DefaultTransport.
func (r *Router) Transport(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return &transport{router: r, next: next}
}

type transport struct {
	router *Router
	next   http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
//
// The rejection path returns before calling next, so an open breaker opens no
// connection, resolves no name, and waits on no timeout. That is the whole
// point of the feature: the caller's failure is local and immediate.
func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	b := t.router.For(req.URL)
	if b == nil {
		return t.next.RoundTrip(req)
	}

	permit, err := b.Allow()
	if err != nil {
		return nil, err
	}

	resp, rtErr := t.next.RoundTrip(req)
	b.Record(permit, Classify(req.Context(), resp, rtErr))
	return resp, rtErr
}

// Classify decides whether one completed HTTP attempt implicates the upstream.
//
// The rule is "does this suggest the upstream cannot serve us", not "did the
// caller get what they wanted":
//
//   - Transport failures — connection refused, DNS, TLS, read timeout — are
//     failures. These are precisely the errors that hold a connection until it
//     times out, which is the pile-on this breaker exists to stop.
//
//   - Caller cancellation is ignored. The client disconnected or a parent
//     context was cancelled; the upstream was never given a chance to answer,
//     and counting it would let a burst of abandoned requests open a breaker
//     over a perfectly healthy endpoint. A deadline that expired is NOT
//     ignored — waiting past the deadline is exactly the symptom of a
//     degrading upstream.
//
//   - 5xx is a failure: the upstream is telling us it is unwell.
//
//   - 429 is a failure. It is a 4xx, but it is the upstream asking for less
//     load, and continuing to push is the behaviour this breaker exists to
//     prevent. Shedding until it recovers is the cooperative response.
//
//   - Every other 4xx is a success. A 404 for an unfunded account, or a 400
//     for a malformed contract call, means the upstream is healthy and
//     answering correctly. Counting these would let ordinary user input open
//     the breaker and take the chain offline for everyone — a denial of
//     service any client could trigger.
//
// JSON-RPC errors returned inside a 200 are deliberately NOT inspected. Doing
// so would require reading and replacing the response body in a transport, and
// Soroban's application errors ("startLedger is before the oldest ledger") are
// mostly client mistakes rather than node ill-health. The failure mode in this
// issue — an endpoint degrading under load — surfaces as transport errors and
// 5xx, which are covered.
func Classify(ctx context.Context, resp *http.Response, err error) Outcome {
	if err != nil {
		if errors.Is(err, context.Canceled) || (ctx != nil && errors.Is(ctx.Err(), context.Canceled)) {
			return Ignored
		}
		return Failure
	}

	if resp == nil {
		return Ignored
	}

	switch {
	case resp.StatusCode >= 500:
		return Failure
	case resp.StatusCode == http.StatusTooManyRequests:
		return Failure
	default:
		return Success
	}
}

var _ http.RoundTripper = (*transport)(nil)
