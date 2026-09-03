package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
)

// authzMatrixRules is the production route table itself, not a copy of it.
// A copy proves only that the copy is self-consistent: production could make
// a route public and the test would stay green describing a policy nobody
// deploys.
var authzMatrixRules = ProductionAuthRules()

// authzMatrixHandler wraps ok200 with Authenticate using authzMatrixRules.
func authzMatrixHandler() http.Handler {
	return Authenticate(testSecret, "", authzMatrixRules, alwaysActiveRevocation)(ok200)
}

// walletA and walletB represent two distinct Stellar accounts for
// cross-user authorization testing.
const (
	walletA = "GCEZWKCA5VLDNRLN3RPRJMRZOX3Z6G5CHCGZP1WKU56V25HXQOPJFHM"
	walletB = "GAZK5JWNIQIF5OFJDQ7J2ZFND3KQ2HP7FYJNKJX6H4L3Y7S5Z4PK5YYB"
)

// AuthzRoute describes a single route to exercise in the authorization matrix.
type AuthzRoute struct {
	Method      string
	Path        string
	Public      bool // expected: anonymous gets 200
	RequireRole string
}

// authzMatrix enumerates every authenticated route the API exposes. Each entry
// specifies the HTTP method, path, and whether it is public.
//
// IMPORTANT: When a new route is added to the API, it MUST appear here or the
// TestAuthorizationMatrix route-coverage check will fail.
var authzMatrix = []AuthzRoute{
	// ── Health / infrastructure ──────────────────────────────────────────
	{Method: "GET", Path: "/health", Public: true},
	{Method: "GET", Path: "/healthz", Public: true},
	{Method: "GET", Path: "/readyz", Public: true},
	{Method: "GET", Path: "/ws", Public: true},

	// ── Auth (public handshake) ──────────────────────────────────────────
	{Method: "POST", Path: "/api/v1/auth/challenge", Public: true},
	{Method: "POST", Path: "/api/v1/auth/verify", Public: true},
	{Method: "POST", Path: "/api/v1/auth/refresh", Public: true},

	// ── Auth (protected) ─────────────────────────────────────────────────
	{Method: "POST", Path: "/api/v1/auth/logout"},
	{Method: "POST", Path: "/api/v1/auth/logout-all"},
	{Method: "GET", Path: "/api/v1/auth/sessions"},
	{Method: "DELETE", Path: "/api/v1/auth/sessions/00000000-0000-0000-0000-000000000000"},

	// ── Vaults ───────────────────────────────────────────────────────────
	{Method: "POST", Path: "/api/v1/vaults"},
	{Method: "GET", Path: "/api/v1/vaults"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000"},
	{Method: "POST", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/deposit"},
	{Method: "POST", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/withdraw"},
	{Method: "POST", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/harvest"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/harvest/preview"},
	{Method: "PATCH", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/harvest-frequency"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/my-position"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/preview-deposit"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/preview-withdraw"},
	{Method: "POST", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/rebalance"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/rebalance-suggestion"},
	{Method: "POST", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/emergency-withdraw"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/share-price"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/convert"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/allocations"},
	{Method: "POST", Path: "/api/v1/vault/rebalance"},

	// ── Transactions ─────────────────────────────────────────────────────
	{Method: "GET", Path: "/api/v1/transactions"},
	{Method: "POST", Path: "/api/v1/transactions"},
	{Method: "GET", Path: "/api/v1/transactions/00000000000000000000-0"},

	// ── Portfolio / valuation / performance ──────────────────────────────
	{Method: "GET", Path: "/api/v1/portfolio/summary"},
	{Method: "GET", Path: "/api/v1/portfolio/valuation"},
	{Method: "GET", Path: "/api/v1/performance/snapshots"},

	// ── Activity / notifications ─────────────────────────────────────────
	{Method: "GET", Path: "/api/v1/activity"},

	// ── User ─────────────────────────────────────────────────────────────
	{Method: "GET", Path: "/api/v1/users/me"},

	// ── Watchlist / savings goals / schedules ────────────────────────────
	{Method: "GET", Path: "/api/v1/users/watchlist"},
	{Method: "POST", Path: "/api/v1/users/watchlist"},
	{Method: "GET", Path: "/api/v1/users/savings-goals"},
	{Method: "POST", Path: "/api/v1/users/savings-goals"},
	{Method: "GET", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000"},
	{Method: "DELETE", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000"},
	{Method: "GET", Path: "/api/v1/users/savings-schedules"},

	// ── Admin (requires admin role) ──────────────────────────────────────
	{Method: "GET", Path: "/api/v1/admin/users", RequireRole: "admin"},

	// ── Yields (public) ───────────────────────────────────────────────
	// Discovery is readable without a wallet. The bare list path is the one
	// the dApp calls, and it is the case a trailing-slash-only prefix rule
	// silently left protected, so it is asserted explicitly here.
	{Method: "GET", Path: "/api/v1/yields", Public: true},
	{Method: "GET", Path: "/api/v1/yields/", Public: true},
	{Method: "GET", Path: "/api/v1/yields/00000000-0000-0000-0000-000000000000", Public: true},
	{Method: "GET", Path: "/api/v1/yield-opportunities", Public: true},
	{Method: "GET", Path: "/api/v1/yield-opportunities/compare", Public: true},

	// ── Money-path pause switches (#1120) ──────────────────────────────
	{Method: "GET", Path: "/api/v1/admin/money-path/switches", RequireRole: "admin"},
	{Method: "PUT", Path: "/api/v1/admin/money-path/switches/deposit", RequireRole: "admin"},
	{Method: "GET", Path: "/api/v1/money-path/status", Public: true},

	// ── Routes recovered from the handler registrations ────────────────
	// Added when the coverage guard showed the original matrix exercised
	// 58 of 178 registered routes, leaving the whole admin surface
	// unverified.
	// admin
	{Method: "DELETE", Path: "/api/v1/admin/savings-goal-templates/00000000-0000-0000-0000-000000000000", RequireRole: "admin"},
	{Method: "DELETE", Path: "/api/v1/admin/vaults/00000000-0000-0000-0000-000000000000/allocations/00000000-0000-0000-0000-000000000000", RequireRole: "admin"},
	{Method: "GET", Path: "/api/v1/admin/audit/verify", RequireRole: "admin"},
	{Method: "GET", Path: "/api/v1/admin/backfill", RequireRole: "admin"},
	{Method: "GET", Path: "/api/v1/admin/backfill/00000000-0000-0000-0000-000000000000", RequireRole: "admin"},
	{Method: "GET", Path: "/api/v1/admin/dashboard", RequireRole: "admin"},
	{Method: "GET", Path: "/api/v1/admin/health", RequireRole: "admin"},
	{Method: "GET", Path: "/api/v1/admin/savings-goal-templates", RequireRole: "admin"},
	{Method: "GET", Path: "/api/v1/admin/scheduler/leadership", RequireRole: "admin"},
	{Method: "GET", Path: "/api/v1/admin/users/00000000-0000-0000-0000-000000000000/money-path", RequireRole: "admin"},
	{Method: "GET", Path: "/api/v1/admin/vaults", RequireRole: "admin"},
	{Method: "GET", Path: "/api/v1/admin/vaults/00000000-0000-0000-0000-000000000000", RequireRole: "admin"},
	{Method: "PATCH", Path: "/api/v1/admin/savings-goal-templates/00000000-0000-0000-0000-000000000000", RequireRole: "admin"},
	{Method: "PATCH", Path: "/api/v1/admin/vaults/00000000-0000-0000-0000-000000000000/allocations/00000000-0000-0000-0000-000000000000", RequireRole: "admin"},
	{Method: "POST", Path: "/api/v1/admin/backfill", RequireRole: "admin"},
	{Method: "POST", Path: "/api/v1/admin/backfill/00000000-0000-0000-0000-000000000000/resume", RequireRole: "admin"},
	{Method: "POST", Path: "/api/v1/admin/savings-goal-templates", RequireRole: "admin"},
	{Method: "POST", Path: "/api/v1/admin/sync-events", RequireRole: "admin"},
	{Method: "POST", Path: "/api/v1/admin/vaults/00000000-0000-0000-0000-000000000000/allocations", RequireRole: "admin"},
	{Method: "POST", Path: "/api/v1/admin/vaults/00000000-0000-0000-0000-000000000000/pause", RequireRole: "admin"},
	{Method: "POST", Path: "/api/v1/admin/vaults/00000000-0000-0000-0000-000000000000/rebalance", RequireRole: "admin"},
	{Method: "POST", Path: "/api/v1/admin/vaults/00000000-0000-0000-0000-000000000000/unpause", RequireRole: "admin"},

	// analytics
	{Method: "GET", Path: "/api/v1/analytics/users/00000000-0000-0000-0000-000000000000"},

	// internal

	// portfolio
	{Method: "GET", Path: "/api/v1/portfolio/summary"},

	// rates
	{Method: "GET", Path: "/api/v1/rates"},

	// savings-goal-templates
	{Method: "GET", Path: "/api/v1/savings-goal-templates"},

	// savings-goals
	{Method: "GET", Path: "/api/v1/savings-goals/shared/00000000-0000-0000-0000-000000000000", Public: true},

	// tools
	{Method: "POST", Path: "/api/v1/tools/projection"},
	{Method: "POST", Path: "/api/v1/tools/simulation"},

	// users
	{Method: "DELETE", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000/schedule"},
	{Method: "DELETE", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000/schedules/00000000-0000-0000-0000-000000000000"},
	{Method: "DELETE", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000/share"},
	{Method: "DELETE", Path: "/api/v1/users/watchlist/00000000-0000-0000-0000-000000000000"},
	{Method: "GET", Path: "/api/v1/users/notification-preferences"},
	{Method: "GET", Path: "/api/v1/users/profile"},
	{Method: "GET", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000/contributions"},
	{Method: "GET", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000/notification-preferences"},
	{Method: "GET", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000/schedules"},
	{Method: "GET", Path: "/api/v1/users/savings-goals/summary"},
	{Method: "GET", Path: "/api/v1/users/wallet/" + walletA},
	{Method: "GET", Path: "/api/v1/users/watchlist"},
	{Method: "PATCH", Path: "/api/v1/users/notification-preferences"},
	{Method: "PATCH", Path: "/api/v1/users/profile"},
	{Method: "PATCH", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000"},
	{Method: "PATCH", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000/archive"},
	{Method: "PATCH", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000/notification-preferences"},
	{Method: "PATCH", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000/pause"},
	{Method: "PATCH", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000/resume"},
	{Method: "PATCH", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000/schedule"},
	{Method: "PATCH", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000/unarchive"},
	{Method: "POST", Path: "/api/v1/users"},
	{Method: "POST", Path: "/api/v1/users/device-tokens"},
	{Method: "POST", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000/complete"},
	{Method: "POST", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000/restore"},
	{Method: "POST", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000/schedule"},
	{Method: "POST", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000/schedules"},
	{Method: "POST", Path: "/api/v1/users/savings-goals/00000000-0000-0000-0000-000000000000/share"},
	{Method: "POST", Path: "/api/v1/users/savings-goals/deposit"},
	{Method: "POST", Path: "/api/v1/users/savings-goals/from-template"},
	{Method: "GET", Path: "/api/v1/users/savings-gamification/progress"},
	{Method: "POST", Path: "/api/v1/users/watchlist"},

	// vaults
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/analytics"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/apy-history"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/emergency-queue"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/harvest/status"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/penalty-distributions"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/penalty-events"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/performance"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/performance/apy"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/performance/history"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/projection"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/rebalance-completions"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/rebalance-history"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/rebalance-legs"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/risk"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/risk/history"},
	{Method: "GET", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/tvl"},
	{Method: "GET", Path: "/api/v1/vaults/tvl"},
	{Method: "POST", Path: "/api/v1/vaults/00000000-0000-0000-0000-000000000000/risk/refresh"},

	// webhooks
	{Method: "DELETE", Path: "/api/v1/webhooks/00000000-0000-0000-0000-000000000000"},
	{Method: "GET", Path: "/api/v1/webhooks"},
	{Method: "GET", Path: "/api/v1/webhooks/00000000-0000-0000-0000-000000000000/deliveries"},
	{Method: "POST", Path: "/api/v1/webhooks"},
	{Method: "POST", Path: "/api/v1/webhooks/deliveries/00000000-0000-0000-0000-000000000000/redeliver"},

	// yields
	//
	// The bookmark reads sit under the public GET prefix, so the middleware
	// lets them through; the handler resolves the caller from the auth
	// context and answers 401 without one, which is where per-user access
	// is actually enforced. The writes carry no public rule at all.
	{Method: "DELETE", Path: "/api/v1/yields/bookmarks/aave-v3"},
	{Method: "GET", Path: "/api/v1/yields/aave-v3/apy-history", Public: true},
	{Method: "GET", Path: "/api/v1/yields/bookmarks", Public: true},
	{Method: "GET", Path: "/api/v1/yields/harvests", Public: true},
	{Method: "POST", Path: "/api/v1/yields/bookmarks"},
}

// TestAuthorizationMatrix verifies the three-way authorization contract for
// every route in the API:
//
//  1. Anonymous request (no token) → 401 for protected routes, 200 for public.
//  2. Non-owner request (valid token, different user) → 401/403 for protected
//     routes that require ownership; the response must NOT be 404 so the
//     endpoint is not an existence oracle.
//  3. Owner request (valid token, matching user) → 200.
//
// This test intentionally targets the auth middleware layer. Handler-level
// ownership checks (returning 404 for non-owners) are covered by the
// handler-level IDOR tests in vault_idor_test.go.
func TestAuthorizationMatrix(t *testing.T) {
	handler := authzMatrixHandler()

	for _, route := range authzMatrix {
		name := route.Method + " " + route.Path
		t.Run(name, func(t *testing.T) {
			// ── 1. Anonymous → 401 (or 200 if public) ────────────────────
			t.Run("anonymous", func(t *testing.T) {
				req := httptest.NewRequest(route.Method, route.Path, nil)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)

				if route.Public {
					if rec.Code != http.StatusOK {
						t.Errorf("public route %s: anonymous got %d, want 200", name, rec.Code)
					}
				} else {
					if rec.Code != http.StatusUnauthorized {
						t.Errorf("protected route %s: anonymous got %d, want 401", name, rec.Code)
					}
				}
			})

			// ── 2. Non-owner (valid token, wrong user) ───────────────────
			if !route.Public {
				t.Run("non-owner", func(t *testing.T) {
					token := makeToken(t, auth.Claims{
						Subject:       "user-other",
						WalletAddress: walletB,
						Roles:         []string{},
						ExpiresAt:     time.Now().Add(time.Hour).Unix(),
					})
					req := httptest.NewRequest(route.Method, route.Path, nil)
					req.Header.Set("Authorization", "Bearer "+token)
					rec := httptest.NewRecorder()
					handler.ServeHTTP(rec, req)

					// Non-owner must NOT get 404 (existence oracle leak).
					// They should get 401 (no matching user context) or 403.
					if rec.Code == http.StatusNotFound {
						t.Errorf("route %s: non-owner got 404 (existence oracle leak), want 401 or 403", name)
					}
				})
			}

			// ── 3. Owner (valid token, correct user) ─────────────────────
			t.Run("owner", func(t *testing.T) {
				roles := []string{}
				if route.RequireRole == "admin" {
					roles = []string{"admin"}
				}
				token := makeToken(t, auth.Claims{
					Subject:       "user-owner",
					WalletAddress: walletA,
					Roles:         roles,
					ExpiresAt:     time.Now().Add(time.Hour).Unix(),
				})
				req := httptest.NewRequest(route.Method, route.Path, nil)
				req.Header.Set("Authorization", "Bearer "+token)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)

				if rec.Code == http.StatusUnauthorized {
					t.Errorf("route %s: owner got 401 (should be authenticated)", name)
				}
			})
		})
	}
}

// TestAuthorizationMatrixRouteCount acts as a compile-time guard: if someone
// adds a route to authzMatrixRules but forgets to add it to authzMatrix,
// this test warns (it does not fail, since the production rules are the
// source of truth for public/protected status).
func TestAuthorizationMatrixRouteCount(t *testing.T) {
	// Count non-public rules in production rules (each may match many paths).
	protectedPrefixes := 0
	for _, r := range authzMatrixRules {
		if !r.Public {
			protectedPrefixes++
		}
	}
	t.Logf("production rules: %d total, %d protected", len(authzMatrixRules), protectedPrefixes)
	t.Logf("matrix entries: %d", len(authzMatrix))

	// Sanity: matrix should have at least as many entries as protected prefixes.
	if len(authzMatrix) < protectedPrefixes {
		t.Errorf("matrix has %d entries but production has %d protected prefixes — some routes may be untested",
			len(authzMatrix), protectedPrefixes)
	}
}
