package middleware

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
)

// RevocationChecker answers whether a session (identified by the JWT's sid
// claim) has been revoked. Backed by Redis in production; consulted on
// every request that carries a session ID.
type RevocationChecker interface {
	IsRevoked(ctx context.Context, sessionID string) (bool, error)
}

// RouteRule describes the authentication policy for a URL prefix + method pair.
type RouteRule struct {
	// Method is the HTTP method this rule applies to; "" matches any method.
	Method string
	// PathPrefix is matched as a prefix against r.URL.Path.
	PathPrefix string
	// Public marks the route as accessible without authentication.
	Public bool
	// Scope is the JWT scope required to access a non-public route.
	// An empty string means any authenticated caller may access the route.
	Scope string
	// Role is the JWT role required to access a non-public route.
	// An empty string means any authenticated caller may access the route.
	Role string
}

// Authenticate returns middleware that validates Bearer JWT tokens signed with
// secret.  rules are evaluated in order; the first matching rule determines
// access policy.  If no rule matches, the request is treated as protected
// (auth required, no specific scope).
func Authenticate(secret, serviceAPIKey string, rules []RouteRule, revocation RevocationChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rule := matchRule(rules, r)

			// Public routes bypass authentication entirely.
			if rule != nil && rule.Public {
				next.ServeHTTP(w, r)
				return
			}

			token, ok := bearerToken(r)
			if !ok {
				writeMiddlewareError(w, http.StatusUnauthorized, "missing or malformed authorization header")
				return
			}

			// Service-to-service auth for internal callers.
			//
			// subtle.ConstantTimeCompare rather than ==, matching the JWT path
			// below: a byte-wise short-circuit on a shared secret leaks its
			// prefix to anyone who can time responses (nester#1149).
			if serviceAPIKey != "" && constantTimeEqual(token, serviceAPIKey) {
				userID := strings.TrimSpace(r.Header.Get("X-User-Id"))
				if userID == "" {
					writeMiddlewareError(w, http.StatusUnauthorized, "X-User-Id header required for service auth")
					return
				}

				// The key is shared between internal services and has no
				// per-caller identity of its own, so anyone holding it could
				// otherwise act as any user on any route. Money-path routes
				// refuse it outright: a service asserting a user identity has
				// no business moving that user's funds (nester#1149).
				if isMoneyPathRoute(r) {
					writeMiddlewareError(w, http.StatusForbidden,
						"service credentials cannot act on behalf of a user on money-path routes")
					return
				}

				// Identify the calling service separately from the key, so
				// audit logs distinguish callers that share one secret.
				serviceName := strings.TrimSpace(r.Header.Get("X-Service-Name"))
				if serviceName == "" {
					serviceName = "unknown"
				}

				user := auth.User{
					ID:            userID,
					WalletAddress: "",
					Scopes:        nil,
					Roles:         []string{"service"},
					ServiceName:   serviceName,
				}
				next.ServeHTTP(w, r.WithContext(auth.NewContext(r.Context(), user)))
				return
			}

			claims, err := auth.ParseJWT(token, secret)
			if err != nil {
				writeMiddlewareError(w, http.StatusUnauthorized, err.Error())
				return
			}

			// Fail-closed: a session-bearing token whose revocation status
			// can't be determined is rejected rather than let through, since
			// serving a possibly-revoked session is the worse failure mode
			// for a savings platform.
			if claims.SessionID != "" {
				revoked, err := revocation.IsRevoked(r.Context(), claims.SessionID)
				if err != nil {
					writeMiddlewareError(w, http.StatusServiceUnavailable, "session verification unavailable")
					return
				}
				if revoked {
					writeMiddlewareError(w, http.StatusUnauthorized, "session has been revoked, please sign in again")
					return
				}
			}

			user := auth.User{
				ID:            claims.Subject,
				WalletAddress: claims.WalletAddress,
				Scopes:        claims.Scopes,
				Roles:         claims.Roles,
				SessionID:     claims.SessionID,
			}

			// Scope check for routes that require a specific permission.
			if rule != nil && rule.Scope != "" && !user.HasScope(rule.Scope) {
				writeMiddlewareError(w, http.StatusForbidden, "insufficient scope")
				return
			}
			// Role check for routes that require a specific role.
			if rule != nil && rule.Role != "" && !user.HasRole(rule.Role) {
				writeMiddlewareError(w, http.StatusForbidden, "insufficient role")
				return
			}

			next.ServeHTTP(w, r.WithContext(auth.NewContext(r.Context(), user)))
		})
	}
}

// constantTimeEqual compares two secrets without leaking their contents
// through timing. subtle.ConstantTimeCompare is itself only constant-time for
// equal-length inputs, so length is folded into the result rather than being
// allowed to short-circuit the comparison.
func constantTimeEqual(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// moneyPathSuffixes are the request-path suffixes that move value.
//
// Matched as suffixes because vault routes are registered with a path
// parameter ("/api/v1/vaults/{id}/deposit"), so the concrete request path
// carries an id this table cannot enumerate.
var moneyPathSuffixes = []string{
	"/deposit",
	"/withdraw",
	"/emergency-withdraw",
	"/harvest",
	"/rebalance",
	"/rebalance/execute",
	"/transfer",
	"/payout",
}

// isMoneyPathRoute reports whether the request targets a route that moves
// user funds.
//
// Deliberately a denylist of value-moving suffixes rather than an allowlist of
// safe routes: the read surface is large and grows constantly, and a new
// analytics endpoint appearing without a matching entry here should not be
// what stops internal service callers from working. The set that moves money
// is small, stable, and worth naming explicitly.
//
// Trailing slashes are trimmed so "/deposit/" cannot slip past, and the match
// is case-insensitive because Go's ServeMux routes are case-sensitive while
// some proxies are not.
func isMoneyPathRoute(r *http.Request) bool {
	path := strings.ToLower(strings.TrimRight(r.URL.Path, "/"))
	if path == "" {
		return false
	}
	for _, suffix := range moneyPathSuffixes {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

// bearerToken extracts the raw token string from an
// "Authorization: Bearer <token>" header.
func bearerToken(r *http.Request) (string, bool) {
	v := r.Header.Get("Authorization")
	if v == "" {
		return "", false
	}
	parts := strings.SplitN(v, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", false
	}
	return token, true
}

// matchRule returns the first rule that matches r's method and path.
func matchRule(rules []RouteRule, r *http.Request) *RouteRule {
	for i := range rules {
		rule := &rules[i]
		if rule.Method != "" && !strings.EqualFold(rule.Method, r.Method) {
			continue
		}
		if strings.HasPrefix(r.URL.Path, rule.PathPrefix) {
			return rule
		}
	}
	return nil
}

// writeMiddlewareError writes a JSON error envelope consistent with the rest
// of the API error format.
func writeMiddlewareError(w http.ResponseWriter, status int, msg string) {
	type errBody struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	type envelope struct {
		Success bool    `json:"success"`
		Error   errBody `json:"error"`
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Success: false, Error: errBody{Code: status, Message: msg}})
}
