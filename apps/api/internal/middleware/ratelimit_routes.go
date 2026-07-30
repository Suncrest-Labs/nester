package middleware

import (
	"net/http"
	"strings"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
)

// RouteMatch identifies a single method+path endpoint that a route-scoped
// limiter applies to. Path is matched exactly against r.URL.Path.
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

// matchesRoute reports whether r matches any of routes by exact method and path.
func matchesRoute(routes []RouteMatch, r *http.Request) bool {
	for _, rt := range routes {
		if r.Method == rt.Method && r.URL.Path == rt.Path {
			return true
		}
	}
	return false
}
