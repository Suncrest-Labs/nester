package middleware

import "net/http"

// ProductionAuthRules is the authentication policy the API actually serves.
//
// It lives here, rather than inline in main.go, so the authorization matrix
// test can assert against the same table the server runs. A test that copies
// the rules instead proves only that the copy is self-consistent: production
// can drift — a route made public, a role requirement dropped — and the test
// stays green while describing a policy nobody deploys.
//
// Order matters. The first rule whose method and path prefix match wins, so
// specific prefixes must precede the "/api/v1/" catch-all.
func ProductionAuthRules() []RouteRule {
	return []RouteRule{
		{PathPrefix: "/health", Public: true},
		{PathPrefix: "/healthz", Public: true},
		{PathPrefix: "/readyz", Public: true},
		{PathPrefix: "/ws", Public: true},
		{Method: http.MethodPost, PathPrefix: "/api/v1/auth/challenge", Public: true},
		{Method: http.MethodPost, PathPrefix: "/api/v1/auth/verify", Public: true},
		{Method: http.MethodPost, PathPrefix: "/api/v1/auth/refresh", Public: true},
		// No blanket "/api/v1/auth/" rule: logout, logout-all, and sessions
		// must stay protected and fall through to the "/api/v1/" catch-all.
		{PathPrefix: "/api/v1/banks/", Public: true},
		{PathPrefix: "/api/v1/yields/", Public: true},
		{PathPrefix: "/api/v1/savings-goals/shared/", Public: true},
		// Whether deposits or withdrawals are halted, and the operator's
		// reason. Public because a signed-out visitor sees the same outage
		// and the UI is required to explain it rather than fail generically
		// (#1120). It exposes nothing the paused response does not already.
		{Method: http.MethodGet, PathPrefix: "/api/v1/money-path/status", Public: true},
		{PathPrefix: "/api/v1/admin/", Public: false, Role: "admin"},
		{PathPrefix: "/api/v1/internal/", Role: "service"},
		{PathPrefix: "/api/v1/", Public: false},
	}
}
