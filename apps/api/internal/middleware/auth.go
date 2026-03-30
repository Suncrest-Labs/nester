package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
)

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
func Authenticate(secret string, rules []RouteRule) func(http.Handler) http.Handler {
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

			claims, err := auth.ParseJWT(token, secret)
			if err != nil {
				writeMiddlewareError(w, http.StatusUnauthorized, err.Error())
				return
			}

			user := auth.User{
				ID:            claims.Subject,
				WalletAddress: claims.WalletAddress,
				Scopes:        claims.Scopes,
				Roles:         claims.Roles,
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"success":false,"error":{"code":%d,"message":%q}}`, status, msg)
}
