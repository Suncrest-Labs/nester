package rpcbudget

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Router maps an outbound HTTP request to the per-provider Limiter guarding
// its upstream.
//
// Routing is by URL host and longest matching path prefix, matching
// breaker.Router so Soroban RPC and Horizon maintain independent token buckets
// even when served from the same host on different paths. Unregistered URLs
// pass through unguarded.
type Router struct {
	entries []routeEntry
}

type routeEntry struct {
	host    string
	prefix  string
	limiter *Limiter
}

// NewRouter returns an empty router.
func NewRouter() *Router { return &Router{} }

// Register routes requests matching rawURL's host and path prefix to l.
func (r *Router) Register(rawURL string, l *Limiter) error {
	if l == nil {
		return errors.New("limiter is required")
	}

	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("parse %s upstream url: %w", l.Name(), err)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%s upstream url %q has no host", l.Name(), rawURL)
	}

	entry := routeEntry{
		host:   strings.ToLower(parsed.Host),
		prefix: strings.TrimSuffix(parsed.Path, "/"),
	}

	for _, existing := range r.entries {
		if existing.host == entry.host && existing.prefix == entry.prefix {
			return fmt.Errorf(
				"upstream %s collides with %s: both resolve to %s%s",
				l.Name(), existing.limiter.Name(), entry.host, entry.prefix,
			)
		}
	}

	entry.limiter = l
	r.entries = append(r.entries, entry)
	return nil
}

// Limiters returns every registered limiter in registration order.
func (r *Router) Limiters() []*Limiter {
	out := make([]*Limiter, 0, len(r.entries))
	for _, entry := range r.entries {
		out = append(out, entry.limiter)
	}
	return out
}

// For returns the limiter guarding u, or nil when the URL is not registered.
// The longest matching path prefix wins.
func (r *Router) For(u *url.URL) *Limiter {
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
	return best.limiter
}

// Transport wraps next so requests to registered providers pass through their
// token-bucket limiter before touching the network or circuit breaker.
// A nil next uses http.DefaultTransport.
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
// Shed requests return immediately with *ShedError (wrapping ErrBudgetExhausted)
// before calling next, so they never consume network I/O or trip the circuit
// breaker.
func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	l := t.router.For(req.URL)
	if l == nil {
		return t.next.RoundTrip(req)
	}

	priority := PriorityFromContext(req.Context())
	if err := l.Allow(priority); err != nil {
		return nil, err
	}

	return t.next.RoundTrip(req)
}
