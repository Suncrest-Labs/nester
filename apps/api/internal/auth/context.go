// Package auth provides types and helpers for request authentication.
package auth

import "context"

type contextKey struct{}

// User represents an authenticated API caller.
type User struct {
	ID            string
	WalletAddress string
	Scopes        []string
	Roles         []string
}

// HasScope reports whether the user holds the given scope.
func (u User) HasScope(scope string) bool {
	for _, s := range u.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// HasRole reports whether the user holds the given role.
func (u User) HasRole(role string) bool {
	for _, r := range u.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// NewContext returns a copy of ctx carrying u.
func NewContext(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, contextKey{}, u)
}

// GetUserFromContext retrieves the User stored in ctx by the authentication
// middleware.  The second return value is false if no user is present.
func GetUserFromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(contextKey{}).(User)
	return u, ok
}
