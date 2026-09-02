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
		// Yield discovery is readable without a wallet: a visitor browses
		// protocols, APYs and TVL before deciding to connect. The prefix has
		// no trailing slash so it also covers the exact "/api/v1/yields"
		// list path, which is what the dApp calls — with a trailing slash
		// that path fell through to the catch-all below and answered 401,
		// leaving the Yields page empty for signed-out visitors.
		//
		// GET only: the bookmark writes under this prefix stay protected.
		// The per-user bookmark reads it does cover resolve the caller from
		// the auth context themselves and 401 without one (see
		// YieldBookmarkHandler.userID), so nothing per-user is exposed here.
		{Method: http.MethodGet, PathPrefix: "/api/v1/yields", Public: true},
		{Method: http.MethodGet, PathPrefix: "/api/v1/yield-opportunities", Public: true},
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
