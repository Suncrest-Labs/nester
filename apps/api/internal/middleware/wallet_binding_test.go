package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
)

// stubWalletResolver reports a fixed wallet (or error) for every user, so
// tests can model "the account's linked wallet is X" without a database.
type stubWalletResolver struct {
	wallet string
	err    error
}

func (s stubWalletResolver) WalletForUser(context.Context, string) (string, error) {
	return s.wallet, s.err
}

// bindingHandler builds the production-shaped chain: authenticate, then bind.
func bindingHandler(resolver WalletResolver) http.Handler {
	return Authenticate(testSecret, "", authzMatrixRules, alwaysActiveRevocation)(
		WalletBindingCheck(resolver)(ok200))
}

func walletToken(t *testing.T, wallet string) string {
	t.Helper()
	return makeToken(t, auth.Claims{
		Subject:       "user-1",
		WalletAddress: wallet,
		ExpiresAt:     time.Now().Add(time.Hour).Unix(),
	})
}

// do issues an authenticated GET and returns the status code.
func do(t *testing.T, h http.Handler, path, token string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestWalletBindingCheck_MatchingWallet(t *testing.T) {
	h := bindingHandler(stubWalletResolver{wallet: walletA})
	if code := do(t, h, "/api/v1/users/wallet/"+walletA, walletToken(t, walletA)); code != http.StatusOK {
		t.Fatalf("matching wallet: got %d, want 200", code)
	}
}

func TestWalletBindingCheck_CrossWalletRejection(t *testing.T) {
	h := bindingHandler(stubWalletResolver{wallet: walletA})
	if code := do(t, h, "/api/v1/users/wallet/"+walletB, walletToken(t, walletA)); code != http.StatusForbidden {
		t.Fatalf("cross-wallet replay: got %d, want 403", code)
	}
}

func TestWalletBindingCheck_NoWalletInPath(t *testing.T) {
	h := bindingHandler(stubWalletResolver{wallet: walletA})
	if code := do(t, h, "/api/v1/vaults", walletToken(t, walletA)); code != http.StatusOK {
		t.Fatalf("route without a wallet segment: got %d, want 200", code)
	}
}

func TestWalletBindingCheck_CaseInsensitive(t *testing.T) {
	upper := walletA
	lower := "gcezwkca5vldnrln3rprjmrzox3z6g5chcgzp1wku56v25hxqopjfhm"

	h := bindingHandler(stubWalletResolver{wallet: upper})
	if code := do(t, h, "/api/v1/users/wallet/"+lower, walletToken(t, upper)); code != http.StatusOK {
		t.Fatalf("case-insensitive match: got %d, want 200", code)
	}
}

func TestWalletBindingCheck_PublicRouteSkipped(t *testing.T) {
	// Public routes carry no user context, so there is no binding to check.
	h := bindingHandler(stubWalletResolver{wallet: walletA})
	if code := do(t, h, "/health", ""); code != http.StatusOK {
		t.Fatalf("public route: got %d, want 200", code)
	}
}

func TestWalletBindingCheck_EmptyWalletAddress(t *testing.T) {
	// A token with no wallet claim has no binding to enforce.
	token := makeToken(t, auth.Claims{
		Subject:   "user-1",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	h := bindingHandler(stubWalletResolver{wallet: walletA})
	if code := do(t, h, "/api/v1/users/wallet/"+walletA, token); code != http.StatusOK {
		t.Fatalf("token without a wallet claim: got %d, want 200", code)
	}
}

// ── Session invalidation on wallet change (#1102) ────────────────────────────

// TestWalletBindingCheck_RelinkInvalidatesSession covers the acceptance
// criterion "changing the linked wallet invalidates existing sessions": a
// token minted for walletA must stop working once the account's wallet of
// record is walletB.
func TestWalletBindingCheck_RelinkInvalidatesSession(t *testing.T) {
	h := bindingHandler(stubWalletResolver{wallet: walletB})
	if code := do(t, h, "/api/v1/vaults", walletToken(t, walletA)); code != http.StatusUnauthorized {
		t.Fatalf("token issued before a wallet relink: got %d, want 401", code)
	}
}

// A stale token must be rejected on money-path routes too, not only on the
// routes that happen to name a wallet in their path.
func TestWalletBindingCheck_RelinkInvalidatesOnMoneyPath(t *testing.T) {
	h := bindingHandler(stubWalletResolver{wallet: walletB})
	path := "/api/v1/vaults/00000000-0000-0000-0000-000000000000/withdraw"
	if code := do(t, h, path, walletToken(t, walletA)); code != http.StatusUnauthorized {
		t.Fatalf("stale token on withdraw: got %d, want 401", code)
	}
}

// Still-current bindings must pass, otherwise the check above would be
// trivially satisfied by rejecting everything.
func TestWalletBindingCheck_UnchangedWalletStillPasses(t *testing.T) {
	h := bindingHandler(stubWalletResolver{wallet: walletA})
	if code := do(t, h, "/api/v1/vaults", walletToken(t, walletA)); code != http.StatusOK {
		t.Fatalf("unchanged wallet binding: got %d, want 200", code)
	}
}

// If the account's wallet cannot be resolved, the request fails closed rather
// than assuming the token's claim is still accurate.
func TestWalletBindingCheck_ResolverErrorFailsClosed(t *testing.T) {
	h := bindingHandler(stubWalletResolver{err: errors.New("db unavailable")})
	if code := do(t, h, "/api/v1/vaults", walletToken(t, walletA)); code != http.StatusUnauthorized {
		t.Fatalf("resolver error: got %d, want 401 (fail closed)", code)
	}
}

// A nil resolver disables the relink check only; cross-wallet path binding
// must still be enforced.
func TestWalletBindingCheck_NilResolverStillBindsPath(t *testing.T) {
	h := bindingHandler(nil)
	if code := do(t, h, "/api/v1/users/wallet/"+walletB, walletToken(t, walletA)); code != http.StatusForbidden {
		t.Fatalf("cross-wallet with nil resolver: got %d, want 403", code)
	}
	if code := do(t, h, "/api/v1/vaults", walletToken(t, walletA)); code != http.StatusOK {
		t.Fatalf("nil resolver on unscoped route: got %d, want 200", code)
	}
}

// An account with no wallet on record has nothing to contradict the token.
func TestWalletBindingCheck_NoWalletOnRecordPasses(t *testing.T) {
	h := bindingHandler(stubWalletResolver{wallet: ""})
	if code := do(t, h, "/api/v1/vaults", walletToken(t, walletA)); code != http.StatusOK {
		t.Fatalf("no wallet on record: got %d, want 200", code)
	}
}

// extractWalletFromPath must only treat allowlisted prefixes as wallet-scoped.
func TestExtractWalletFromPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/api/v1/users/wallet/" + walletA, walletA},
		{"/api/v1/users/wallet/" + walletA + "/vaults", walletA},
		{"/api/v1/users/wallet/", ""},
		{"/api/v1/vaults", ""},
		{"/api/v1/vaults/" + walletA, ""},
		{"/health", ""},
	}
	for _, c := range cases {
		if got := extractWalletFromPath(c.path); got != c.want {
			t.Errorf("extractWalletFromPath(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}
