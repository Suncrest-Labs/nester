package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
)

// RouteMatch identifies a single method+path endpoint that a route-scoped
// limiter (or the idempotency middleware) applies to.
//
// Path is matched against r.URL.Path. A segment of the form `{name}` is a
// wildcard, so `/api/v1/vaults/{id}/deposit` matches any vault id. Paths
// without braces still match exactly, preserving every existing caller.
type RouteMatch struct {
	Method string
	Path   string
}

// userIDFromContext returns the authenticated user's ID, or "" when the request
// carries no authenticated user (i.e. it has not passed Authenticate or is a
// public route).
func userIDFromContext(r *http.Request) string {
	u, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		return ""
	}
	return u.ID
}

// userOrIP keys authenticated requests by user ID and falls back to the client
// IP for anonymous requests. This gives per-user isolation once a request is
// authenticated while still bounding pre-auth traffic per source.
func userOrIP(r *http.Request) string {
	if id := userIDFromContext(r); id != "" {
		return id
	}
	return clientIP(r)
}

// GlobalRateLimiter enforces a per-client-IP limit across all requests except
// those whose path matches one of excludePrefixes (health, readiness and
// metrics endpoints, which must remain callable by orchestrators under load).
// It is backed by the supplied Limiter, so it is distributed when Redis is
// configured and in-memory otherwise.
func GlobalRateLimiter(l Limiter, excludePrefixes []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, p := range excludePrefixes {
				if strings.HasPrefix(r.URL.Path, p) {
					next.ServeHTTP(w, r)
					return
				}
			}

			allowed, wait := l.Allow(r.Context(), clientIP(r))
			if !allowed {
				writeRateLimited(w, wait, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// apiKeyRateLimitKeyPrefix namespaces API-key rate-limit keys in the shared
// Redis keyspace, so this limiter's counters can never collide with
// clientIP's (both are opaque hex/IP strings that could otherwise coincide).
const apiKeyRateLimitKeyPrefix = "apikey:"

// apiKeyRateLimitKey derives the per-API-key rate-limit key from the bearer
// token presented on r, or "" if the request carries none (an anonymous or
// JWT-authenticated caller passes through this limiter untouched -- JWT
// sessions already get per-user isolation via SensitiveUserRouteLimiter).
//
// The token is hashed rather than used raw: it is a live credential, and this
// key ends up in Redis and potentially in logs/metrics around it, neither of
// which should ever hold a working secret. SHA-256 is sufficient here (this
// is a rate-limit bucket key, not a password hash guarding against offline
// brute force) and keeps the same input always mapping to the same bucket.
func apiKeyRateLimitKey(r *http.Request) string {
	token, ok := bearerToken(r)
	if !ok {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return apiKeyRateLimitKeyPrefix + hex.EncodeToString(sum[:])
}

// APIKeyRateLimiter enforces a rate limit per presented bearer credential, in
// addition to and independent of the per-client-IP limit GlobalRateLimiter
// applies (nester#1343). Today the only bearer credential besides a user JWT
// is the single shared service API key (see middleware.Authenticate), so this
// bounds that key's total request budget regardless of how many distinct
// client IPs it is used from -- a single compromised or misbehaving
// integration holding it cannot exhaust the shared IP-based budget for other
// clients on the same address, and cannot outrun its own limit by rotating
// source IPs either. The same mechanism covers per-integration keys without
// further changes if this codebase grows distinct keys per integration later.
//
// Requests with no bearer token (public routes, or routes rejected by
// Authenticate before this middleware would matter) pass through unbounded by
// this limiter; excludePrefixes additionally skips liveness/readiness/metrics
// endpoints so orchestrators can always reach them even while presenting
// internal credentials.
func APIKeyRateLimiter(l Limiter, excludePrefixes []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, p := range excludePrefixes {
				if strings.HasPrefix(r.URL.Path, p) {
					next.ServeHTTP(w, r)
					return
				}
			}

			key := apiKeyRateLimitKey(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			allowed, wait := l.Allow(r.Context(), key)
			if !allowed {
				writeRateLimited(w, wait, "API key rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SensitiveRouteLimiter applies a strict per-client-IP limit to a fixed set of
// abuse-prone endpoints. Requests that match none of routes pass through
// untouched. Keying by IP works even for pre-authentication routes (e.g. the
// auth challenge/verify handshake) where no user identity exists yet.
func SensitiveRouteLimiter(l Limiter, routes []RouteMatch, message string) func(http.Handler) http.Handler {
	return sensitiveRouteLimiter(l, routes, clientIP, message)
}

// SensitiveUserRouteLimiter applies a strict limit to a fixed set of
// authenticated endpoints, keyed by user ID (falling back to IP for any
// unauthenticated caller that reaches the route). It must be placed after the
// authentication middleware so the user identity is present in the context.
func SensitiveUserRouteLimiter(l Limiter, routes []RouteMatch, message string) func(http.Handler) http.Handler {
	return sensitiveRouteLimiter(l, routes, userOrIP, message)
}

// sensitiveRouteLimiter is the shared implementation behind the exported
// route-scoped limiters: it applies l to any request matching routes, deriving
// the rate-limit key with keyFn, and passes everything else through.
func sensitiveRouteLimiter(l Limiter, routes []RouteMatch, keyFn func(*http.Request) string, message string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !matchesRoute(routes, r) {
				next.ServeHTTP(w, r)
				return
			}

			key := keyFn(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			allowed, wait := l.Allow(r.Context(), key)
			if !allowed {
				writeRateLimited(w, wait, message)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// matchesRoute reports whether r matches any of routes by method and path
// pattern (see RouteMatch).
func matchesRoute(routes []RouteMatch, r *http.Request) bool {
	for _, rt := range routes {
		if r.Method == rt.Method && matchPathPattern(rt.Path, r.URL.Path) {
			return true
		}
	}
	return false
}

// matchPathPattern reports whether path matches pattern. `{param}` segments
// match any non-empty path segment; every other segment must be identical.
func matchPathPattern(pattern, path string) bool {
	if pattern == path {
		return true
	}
	pSegs := splitPath(pattern)
	pathSegs := splitPath(path)
	if len(pSegs) != len(pathSegs) {
		return false
	}
	for i, seg := range pSegs {
		if isPathWildcard(seg) {
			if pathSegs[i] == "" {
				return false
			}
			continue
		}
		if seg != pathSegs[i] {
			return false
		}
	}
	return true
}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func isPathWildcard(seg string) bool {
	return len(seg) >= 2 && seg[0] == '{' && seg[len(seg)-1] == '}'
}
