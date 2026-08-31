package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
)

// WalletResolver reports the wallet address currently linked to a user. It
// backs the "changing the linked wallet invalidates existing sessions" rule:
// a token carries the wallet it was minted for, and if the account's wallet
// has since changed, that token is stale and must not be honoured.
//
// An error fails the request closed. Returning ("", nil) means "no wallet on
// record", which is treated as nothing to contradict the token.
type WalletResolver interface {
	WalletForUser(ctx context.Context, userID string) (string, error)
}

// walletScopedPrefixes lists the route prefixes that address a wallet directly
// in their path. The segment immediately after the prefix is the wallet.
//
// This is an explicit allowlist rather than a scan for anything that looks
// like a Stellar address: a heuristic scan silently changes meaning whenever
// an unrelated route happens to take an address-shaped segment, and it cannot
// tell "the caller's own wallet" from "some other wallet named in the path".
var walletScopedPrefixes = []string{
	"/api/v1/users/wallet/",
}

// WalletBindingCheck returns middleware that verifies the JWT's wallet address
// matches the wallet named in the request path, and that it still matches the
// wallet currently linked to the account. This closes two replay paths:
//
//  1. Cross-wallet replay: a token minted for wallet A used against wallet B's
//     endpoints is rejected.
//  2. Stale-session replay: a token minted before the account's wallet changed
//     is rejected, so relinking a wallet invalidates sessions issued earlier.
//
// resolver may be nil, which disables check (2) only. Requests without a user
// context (public routes) pass through untouched.
func WalletBindingCheck(resolver WalletResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := auth.GetUserFromContext(r.Context())
			if !ok || user.WalletAddress == "" {
				next.ServeHTTP(w, r)
				return
			}

			// (1) The token's wallet must match the wallet named in the path,
			// on the routes that name one.
			if pathWallet := extractWalletFromPath(r.URL.Path); pathWallet != "" {
				if !strings.EqualFold(user.WalletAddress, pathWallet) {
					writeMiddlewareError(w, http.StatusForbidden,
						"token wallet does not match the requested wallet")
					return
				}
			}

			// (2) The token's wallet must still be the account's wallet. This
			// runs on every authenticated request, not just wallet-scoped
			// paths — a stale token is stale everywhere it can move money.
			if resolver != nil && user.ID != "" {
				current, err := resolver.WalletForUser(r.Context(), user.ID)
				if err != nil {
					// Fail closed: if we cannot confirm the binding is still
					// valid, we do not get to assume it is.
					writeMiddlewareError(w, http.StatusUnauthorized,
						"could not verify wallet binding for this session")
					return
				}
				if current != "" && !strings.EqualFold(current, user.WalletAddress) {
					writeMiddlewareError(w, http.StatusUnauthorized,
						"session was issued for a wallet no longer linked to this account")
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractWalletFromPath returns the wallet segment addressed by the path, or
// "" when the path is not wallet-scoped.
func extractWalletFromPath(path string) string {
	for _, prefix := range walletScopedPrefixes {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		rest := strings.TrimPrefix(path, prefix)
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			rest = rest[:i]
		}
		if rest != "" {
			return rest
		}
	}
	return ""
}
