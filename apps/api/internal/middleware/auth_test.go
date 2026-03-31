package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
)

const testSecret = "unit-test-hmac-secret"

// defaultRules is the route table used across most auth tests.
//
//   - /health      — public (any method)
//   - GET /api/v1/vaults — public
//   - POST /api/v1/deposit — protected, requires "deposit" scope
//   - everything else — protected (auth required, no specific scope)
var defaultRules = []RouteRule{
	{PathPrefix: "/health", Public: true},
	{Method: http.MethodGet, PathPrefix: "/api/v1/vaults", Public: true},
	{Method: http.MethodPost, PathPrefix: "/api/v1/deposit", Scope: "deposit"},
	{PathPrefix: "/api/v1/admin/", Role: "admin"},
}

// authHandler wraps ok200 with Authenticate using defaultRules and testSecret.
func authHandler() http.Handler {
	return Authenticate(testSecret, defaultRules)(ok200)
}

// makeToken creates a signed JWT for test assertions.
func makeToken(t *testing.T, claims auth.Claims) string {
	t.Helper()
	tok, err := auth.MakeJWT(claims, testSecret)
	if err != nil {
		t.Fatalf("makeToken: %v", err)
	}
	return tok
}

// ---------------------------------------------------------------------------
// Valid JWT
// ---------------------------------------------------------------------------

func TestAuthValidJWTPassesThrough(t *testing.T) {
	token := makeToken(t, auth.Claims{
		Subject:   "user-1",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	authHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 for valid JWT", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Missing Authorization header → 401
// ---------------------------------------------------------------------------

func TestAuthMissingAuthorizationHeaderReturns401(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deposit", nil)
	rec := httptest.NewRecorder()

	authHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 (no Authorization header)", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Malformed token → 401
// ---------------------------------------------------------------------------

func TestAuthMalformedTokenReturns401(t *testing.T) {
	cases := []struct {
		name   string
		header string
	}{
		{"wrong scheme", "Token abc.def.ghi"},
		{"only bearer keyword", "Bearer"},
		{"bearer with blank token", "Bearer "},
		{"plain string not JWT", "Bearer not-a-jwt-at-all"},
		{"wrong number of segments", "Bearer two.parts"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil)
			req.Header.Set("Authorization", tc.header)
			rec := httptest.NewRecorder()

			authHandler().ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s: got %d, want 401", tc.name, rec.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Expired token → 401
// ---------------------------------------------------------------------------

func TestAuthExpiredTokenReturns401(t *testing.T) {
	token := makeToken(t, auth.Claims{
		Subject:   "user-2",
		ExpiresAt: time.Now().Add(-time.Hour).Unix(), // already expired
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	authHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 for expired token", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Insufficient scope → 403
// ---------------------------------------------------------------------------

func TestAuthInsufficientScopeReturns403(t *testing.T) {
	// Valid token but missing the "deposit" scope required by POST /api/v1/deposit.
	token := makeToken(t, auth.Claims{
		Subject:   "user-3",
		Scopes:    []string{"read"}, // "deposit" is absent
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deposit", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	authHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403 (insufficient scope)", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// GetUserFromContext returns the correct user
// ---------------------------------------------------------------------------

func TestAuthGetUserFromContextReturnsCorrectUser(t *testing.T) {
	want := auth.Claims{
		Subject:       "user-42",
		WalletAddress: "GCEZWKCA5VLDNRLN3RPRJMRZOX3Z6G5CHCGZP1WKU56V25HXQOPJFHM",
		Scopes:        []string{"deposit", "read"},
		ExpiresAt:     time.Now().Add(time.Hour).Unix(),
	}
	token := makeToken(t, want)

	var gotUser auth.User
	var gotOK bool

	capture := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotUser, gotOK = auth.GetUserFromContext(r.Context())
	})
	handler := Authenticate(testSecret, defaultRules)(capture)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if !gotOK {
		t.Fatal("GetUserFromContext: ok = false, want true")
	}
	if gotUser.ID != want.Subject {
		t.Errorf("ID = %q, want %q", gotUser.ID, want.Subject)
	}
	if gotUser.WalletAddress != want.WalletAddress {
		t.Errorf("WalletAddress = %q, want %q", gotUser.WalletAddress, want.WalletAddress)
	}
	if len(gotUser.Scopes) != len(want.Scopes) {
		t.Errorf("Scopes = %v, want %v", gotUser.Scopes, want.Scopes)
	}
}

// ---------------------------------------------------------------------------
// Public routes — no auth required
// ---------------------------------------------------------------------------

func TestAuthPublicHealthRoutePassesWithoutToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	authHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 for public /health (no auth)", rec.Code)
	}
}

func TestAuthPublicGetVaultsPassesWithoutToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/vaults", nil)
	rec := httptest.NewRecorder()

	authHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 for public GET /api/v1/vaults", rec.Code)
	}
}

// POST /api/v1/vaults is not in the public list, so it must require auth.
func TestAuthPostVaultsIsProtected(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/vaults", nil)
	rec := httptest.NewRecorder()

	authHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 for unauthenticated POST /api/v1/vaults", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Protected routes — reject unauthenticated requests
// ---------------------------------------------------------------------------

func TestAuthProtectedRouteRejectsUnauthenticated(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deposit", nil)
	rec := httptest.NewRecorder()

	authHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401 for unauthenticated POST /api/v1/deposit", rec.Code)
	}
}

func TestAuthProtectedRouteAllowsCorrectScope(t *testing.T) {
	token := makeToken(t, auth.Claims{
		Subject:   "user-5",
		Scopes:    []string{"deposit"},
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/deposit", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	authHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 for token with correct scope", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Error response format
// ---------------------------------------------------------------------------

func TestAuthErrorResponseIsJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deposit", nil)
	rec := httptest.NewRecorder()

	authHandler().ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"success":false`) {
		t.Errorf("body = %q, want success:false envelope", body)
	}
}

func TestAuthAdminRouteRequiresAdminRole(t *testing.T) {
	token := makeToken(t, auth.Claims{
		Subject:   "user-non-admin",
		Roles:     []string{"operator"},
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	authHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403 for non-admin role on admin route", rec.Code)
	}
}

func TestAuthAdminRouteAllowsAdminRole(t *testing.T) {
	token := makeToken(t, auth.Claims{
		Subject:   "user-admin",
		Roles:     []string{"admin"},
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	authHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 for admin role on admin route", rec.Code)
	}
}
