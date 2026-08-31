package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
)

// Readable rather than key-shaped, per the convention in redact_test.go: a
// high-entropy literal in a test file still looks like a leaked credential to
// a secret scanner.
const testServiceKey = "SENTINEL-service-api-key-not-a-real-secret"

// A holder of the shared service key must not be able to move a user's money
// by asserting their identity in a header.
//
// The key is shared with the intelligence service, carries no per-caller
// identity, and has no rotation story, so treating it as "any user" on the
// money path makes every deposit and withdrawal reachable by anyone who has
// ever seen it (nester#1149).
func TestServiceKeyCannotImpersonateOnMoneyPathRoutes(t *testing.T) {
	moneyPaths := []string{
		"/api/v1/vaults/11111111-1111-1111-1111-111111111111/deposit",
		"/api/v1/vaults/11111111-1111-1111-1111-111111111111/withdraw",
		"/api/v1/vaults/11111111-1111-1111-1111-111111111111/emergency-withdraw",
		"/api/v1/vaults/11111111-1111-1111-1111-111111111111/harvest",
		"/api/v1/vaults/11111111-1111-1111-1111-111111111111/rebalance",
		"/api/v1/vaults/11111111-1111-1111-1111-111111111111/rebalance/execute",
		"/api/v1/users/savings-goals/deposit",
		// Trailing slash and mixed case must not be an escape hatch.
		"/api/v1/vaults/11111111-1111-1111-1111-111111111111/deposit/",
		"/api/v1/vaults/11111111-1111-1111-1111-111111111111/DEPOSIT",
	}

	handler := Authenticate(testSecret, testServiceKey, defaultRules, alwaysActiveRevocation)(ok200)

	for _, path := range moneyPaths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, nil)
			req.Header.Set("Authorization", "Bearer "+testServiceKey)
			req.Header.Set("X-User-Id", "victim-user-id")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s: got %d, want 403 — service key must not act as a user on the money path", path, rec.Code)
			}
		})
	}
}

// Service auth is still allowed to do its actual job: reading on behalf of a
// user on non-money routes. Denying everything would break the intelligence
// service rather than secure it.
func TestServiceKeyStillWorksOnNonMoneyRoutes(t *testing.T) {
	handler := Authenticate(testSecret, testServiceKey, defaultRules, alwaysActiveRevocation)(ok200)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil)
	req.Header.Set("Authorization", "Bearer "+testServiceKey)
	req.Header.Set("X-User-Id", "user-42")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 for service auth on a non-money route", rec.Code)
	}
}

// The calling service must be identifiable separately from the key it holds,
// so an audit trail can name the caller rather than "someone with the key".
func TestServiceKeyRecordsCallingServiceName(t *testing.T) {
	var captured auth.User
	capture := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		captured, _ = auth.GetUserFromContext(r.Context())
	})

	handler := Authenticate(testSecret, testServiceKey, defaultRules, alwaysActiveRevocation)(capture)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil)
	req.Header.Set("Authorization", "Bearer "+testServiceKey)
	req.Header.Set("X-User-Id", "user-42")
	req.Header.Set("X-Service-Name", "intelligence")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if captured.ServiceName != "intelligence" {
		t.Fatalf("ServiceName = %q, want %q", captured.ServiceName, "intelligence")
	}
	if captured.ID != "user-42" {
		t.Fatalf("ID = %q, want %q", captured.ID, "user-42")
	}
}

// An unnamed caller is still identified as such rather than silently blending
// in with named ones.
func TestServiceKeyDefaultsServiceNameWhenAbsent(t *testing.T) {
	var captured auth.User
	capture := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		captured, _ = auth.GetUserFromContext(r.Context())
	})

	handler := Authenticate(testSecret, testServiceKey, defaultRules, alwaysActiveRevocation)(capture)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil)
	req.Header.Set("Authorization", "Bearer "+testServiceKey)
	req.Header.Set("X-User-Id", "user-42")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if captured.ServiceName != "unknown" {
		t.Fatalf("ServiceName = %q, want %q", captured.ServiceName, "unknown")
	}
}

// A near-miss key must not authenticate. This also pins the comparison to a
// whole-value check rather than a prefix one.
func TestServiceKeyRejectsNearMissAndEmptyKey(t *testing.T) {
	tests := []struct {
		name       string
		configured string
		presented  string
	}{
		{name: "prefix of the real key", configured: testServiceKey, presented: testServiceKey[:len(testServiceKey)-1]},
		{name: "real key plus a suffix", configured: testServiceKey, presented: testServiceKey + "x"},
		// An unset key must never turn an empty bearer token into service auth.
		{name: "service auth disabled", configured: "", presented: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := Authenticate(testSecret, tt.configured, defaultRules, alwaysActiveRevocation)(ok200)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/protected", nil)
			req.Header.Set("Authorization", "Bearer "+tt.presented)
			req.Header.Set("X-User-Id", "user-42")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code == http.StatusOK {
				t.Fatal("a non-matching key authenticated as a service caller")
			}
		})
	}
}

func TestConstantTimeEqual(t *testing.T) {
	if !constantTimeEqual("abc", "abc") {
		t.Fatal("identical values compared unequal")
	}
	if constantTimeEqual("abc", "abd") {
		t.Fatal("different values compared equal")
	}
	if constantTimeEqual("abc", "abcd") {
		t.Fatal("different-length values compared equal")
	}
	if constantTimeEqual("", "abc") {
		t.Fatal("empty compared equal to non-empty")
	}
}

func TestIsMoneyPathRoute(t *testing.T) {
	money := []string{
		"/api/v1/vaults/abc/deposit",
		"/api/v1/vaults/abc/withdraw",
		"/api/v1/vaults/abc/emergency-withdraw",
		"/api/v1/vaults/abc/deposit/",
	}
	safe := []string{
		"/api/v1/vaults",
		"/api/v1/vaults/abc",
		"/api/v1/vaults/abc/performance",
		"/api/v1/analytics/summary",
		"/health",
		"/",
	}

	for _, path := range money {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		if !isMoneyPathRoute(req) {
			t.Errorf("isMoneyPathRoute(%q) = false, want true", path)
		}
	}
	for _, path := range safe {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if isMoneyPathRoute(req) {
			t.Errorf("isMoneyPathRoute(%q) = true, want false", path)
		}
	}
}
