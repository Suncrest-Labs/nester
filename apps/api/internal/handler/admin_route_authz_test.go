package handler

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
)

// Issue #1105: a single missing role check on an admin route is a total
// compromise, and enumerating those routes by hand in a test is exactly the
// kind of list that silently goes stale the first time someone adds a route.
//
// So this file does not hand-maintain the list. It records what
// AdminHandler.Register actually registers, and asserts a property over every
// recorded route. A new admin route is picked up automatically; if it is not
// gated, these tests fail without anyone having to remember to add an entry.

// recordingMux captures the "METHOD /path" patterns passed to HandleFunc
// instead of building a real router, so a test can enumerate exactly what
// Register mounted.
type recordingMux struct {
	patterns []string
}

func (m *recordingMux) HandleFunc(pattern string, _ func(http.ResponseWriter, *http.Request)) {
	m.patterns = append(m.patterns, pattern)
}

// registeredAdminRoutes returns every pattern AdminHandler.Register mounts.
func registeredAdminRoutes(t *testing.T) []string {
	t.Helper()

	rec := &recordingMux{}
	h := NewAdminHandler(newAdminHandlerStubService(uuid.New()), nil)
	h.Register(rec)

	if len(rec.patterns) == 0 {
		t.Fatal("Register mounted no routes; the recording mux is not wired correctly")
	}
	return rec.patterns
}

// splitPattern splits "GET /api/v1/admin/x" into method and path.
func splitPattern(t *testing.T, pattern string) (method, path string) {
	t.Helper()

	parts := strings.SplitN(pattern, " ", 2)
	if len(parts) != 2 {
		t.Fatalf("route pattern %q is not in %q form", pattern, "METHOD /path")
	}
	return parts[0], parts[1]
}

// concreteize replaces {placeholder} path segments with real values so the
// pattern can be used as a request target. Authorization is decided by the
// middleware on path prefix alone, before any handler or router matching, so
// the substituted values only need to be well-formed.
func concreteize(path string) string {
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			segments[i] = uuid.NewString()
		}
	}
	return strings.Join(segments, "/")
}

// adminAuthRules mirrors the production rule table in cmd/api/main.go for the
// prefixes that decide admin access. If the production table changes shape,
// TestAdminRouteRulesMatchProduction below is the tripwire.
var adminAuthRules = []middleware.RouteRule{
	{PathPrefix: "/api/v1/admin/", Public: false, Role: "admin"},
	{PathPrefix: "/api/v1/", Public: false},
}

func authzChain(t *testing.T) http.Handler {
	t.Helper()

	return middleware.Authenticate(
		testAuthSecret,
		"",
		adminAuthRules,
		noopRevocationChecker{},
	)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Reaching here means authorization allowed the request through.
		w.WriteHeader(http.StatusOK)
	}))
}

const testAuthSecret = "admin-route-authz-test-hmac-secret"

func tokenWithRoles(t *testing.T, roles []string) string {
	t.Helper()

	tok, err := auth.MakeJWT(auth.Claims{
		Subject:   uuid.NewString(),
		Roles:     roles,
		SessionID: uuid.NewString(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}, testAuthSecret)
	if err != nil {
		t.Fatalf("MakeJWT: %v", err)
	}
	return tok
}

// nonAdminAllowlist names the routes AdminHandler registers that are
// deliberately NOT admin-gated, each with the reason. Anything registered
// outside /api/v1/admin/ that is not in this map fails the test below, so
// putting a route here is a conscious, reviewable decision rather than an
// oversight.
var nonAdminAllowlist = map[string]string{
	// Introduced in d1187714 alongside the public audit page. Rebalance
	// history is vault-level transparency data — which protocols a vault
	// moved between and when. It contains no per-user balances or PII, and
	// is intended to be readable by any authenticated user.
	"GET /api/v1/vaults/{id}/rebalance-history": "vault-level transparency data, no PII",
}

// TestEveryAdminRouteRejectsNonAdmin is the core assertion of #1105: for every
// route AdminHandler registers under /api/v1/admin/, a caller holding a valid
// token WITHOUT the admin role must be rejected with 403.
func TestEveryAdminRouteRejectsNonAdmin(t *testing.T) {
	token := tokenWithRoles(t, []string{"operator"})
	chain := authzChain(t)

	var checked int
	for _, pattern := range registeredAdminRoutes(t) {
		method, path := splitPattern(t, pattern)
		if !strings.HasPrefix(path, "/api/v1/admin/") {
			continue
		}
		checked++

		t.Run(pattern, func(t *testing.T) {
			req := httptest.NewRequest(method, concreteize(path), nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()

			chain.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("got %d, want 403 — %s is reachable by a non-admin", rec.Code, pattern)
			}
		})
	}

	if checked == 0 {
		t.Fatal("no /api/v1/admin/ routes were checked; enumeration is broken")
	}
}

// TestEveryAdminRouteRejectsAnonymous asserts the same routes are not
// reachable without a token at all. A 403-only test would still pass if the
// middleware were somehow skipped for unauthenticated callers.
func TestEveryAdminRouteRejectsAnonymous(t *testing.T) {
	chain := authzChain(t)

	for _, pattern := range registeredAdminRoutes(t) {
		method, path := splitPattern(t, pattern)
		if !strings.HasPrefix(path, "/api/v1/admin/") {
			continue
		}

		t.Run(pattern, func(t *testing.T) {
			req := httptest.NewRequest(method, concreteize(path), nil)
			rec := httptest.NewRecorder()

			chain.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("got %d, want 401 — %s is reachable anonymously", rec.Code, pattern)
			}
		})
	}
}

// TestAdminRouteAcceptsAdmin is the positive control. Without it, a middleware
// that rejected everything would pass both tests above.
func TestAdminRouteAcceptsAdmin(t *testing.T) {
	token := tokenWithRoles(t, []string{"admin"})
	chain := authzChain(t)

	for _, pattern := range registeredAdminRoutes(t) {
		method, path := splitPattern(t, pattern)
		if !strings.HasPrefix(path, "/api/v1/admin/") {
			continue
		}

		t.Run(pattern, func(t *testing.T) {
			req := httptest.NewRequest(method, concreteize(path), nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()

			chain.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("got %d, want 200 — admin is blocked from %s", rec.Code, pattern)
			}
		})
	}
}

// TestAdminHandlerRoutesAreAdminScoped is the "adding an admin route without a
// test entry fails CI" criterion from #1105. Every route AdminHandler mounts
// must either sit under /api/v1/admin/ (and so be covered by the tests above)
// or be named in nonAdminAllowlist with a documented reason.
//
// A new route that is neither fails here, naming itself in the message.
func TestAdminHandlerRoutesAreAdminScoped(t *testing.T) {
	var unaccounted []string

	for _, pattern := range registeredAdminRoutes(t) {
		_, path := splitPattern(t, pattern)
		if strings.HasPrefix(path, "/api/v1/admin/") {
			continue
		}
		if _, ok := nonAdminAllowlist[pattern]; ok {
			continue
		}
		unaccounted = append(unaccounted, pattern)
	}

	if len(unaccounted) > 0 {
		sort.Strings(unaccounted)
		t.Fatalf(
			"AdminHandler registers %d route(s) outside /api/v1/admin/ that are not admin-gated:\n  %s\n\n"+
				"Either move the route under /api/v1/admin/, or add it to nonAdminAllowlist in this file "+
				"with the reason it is safe for any authenticated user to call.",
			len(unaccounted), strings.Join(unaccounted, "\n  "),
		)
	}
}

// TestNonAdminAllowlistIsNotStale keeps the allowlist honest in the other
// direction: an entry for a route that no longer exists (renamed, moved under
// /api/v1/admin/, or deleted) is dead config that would silently excuse a
// future route of the same name.
func TestNonAdminAllowlistIsNotStale(t *testing.T) {
	registered := make(map[string]bool)
	for _, pattern := range registeredAdminRoutes(t) {
		registered[pattern] = true
	}

	for pattern := range nonAdminAllowlist {
		if !registered[pattern] {
			t.Errorf("nonAdminAllowlist names %q, which AdminHandler no longer registers; remove the entry", pattern)
		}
	}
}

// TestNonAdminAllowlistRoutesAreReadOnly bounds what the allowlist can excuse.
// A route exempt from the admin gate must at least be non-mutating: allowing a
// POST/PATCH/DELETE through to any authenticated user is never a transparency
// argument.
func TestNonAdminAllowlistRoutesAreReadOnly(t *testing.T) {
	for pattern := range nonAdminAllowlist {
		method, _ := splitPattern(t, pattern)
		if method != http.MethodGet && method != http.MethodHead {
			t.Errorf("nonAdminAllowlist contains %q: only GET/HEAD routes may skip the admin gate", pattern)
		}
	}
}
