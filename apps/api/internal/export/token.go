package export

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Download tokens make export delivery secure: a generated file may contain
// a user's complete financial history, so it is fetched only through a
// time-limited, single-user token — never a guessable public URL. The
// download endpoint verifies both the signature (integrity + expiry) and
// that the authenticated requester is the token's owner.

// ErrTokenInvalid is returned for malformed or tampered tokens.
var ErrTokenInvalid = errors.New("export: invalid download token")

// ErrTokenExpired is returned when the token's window has passed.
var ErrTokenExpired = errors.New("export: download token expired")

// ErrNotOwner is returned when an authenticated user presents a valid token
// belonging to a different user.
var ErrNotOwner = errors.New("export: token does not belong to requester")

// TokenIssuer signs and verifies download tokens with an HMAC key held
// server-side. Tokens are self-describing: exportID | userID | expiry | sig.
type TokenIssuer struct {
	key []byte
	ttl time.Duration
	now func() time.Time
}

// NewTokenIssuer builds an issuer. ttl bounds the download window (files
// also expire and are purged after the retention window).
func NewTokenIssuer(key []byte, ttl time.Duration) (*TokenIssuer, error) {
	if len(key) < 32 {
		return nil, errors.New("export: token key must be at least 32 bytes")
	}
	if ttl <= 0 {
		return nil, errors.New("export: token ttl must be positive")
	}
	return &TokenIssuer{key: key, ttl: ttl, now: time.Now}, nil
}

// Issue creates a download token for exportID owned by userID.
func (t *TokenIssuer) Issue(exportID, userID string) string {
	exp := t.now().Add(t.ttl).Unix()
	payload := fmt.Sprintf("%s|%s|%d", exportID, userID, exp)
	return payload + "|" + t.sign(payload)
}

// Verify checks the token's integrity, expiry, and ownership: requesterID
// must be the authenticated user attempting the download. It returns the
// exportID the token grants access to.
func (t *TokenIssuer) Verify(token, requesterID string) (exportID string, err error) {
	parts := strings.Split(token, "|")
	if len(parts) != 4 {
		return "", ErrTokenInvalid
	}
	payload := strings.Join(parts[:3], "|")
	if !hmac.Equal([]byte(t.sign(payload)), []byte(parts[3])) {
		return "", ErrTokenInvalid
	}

	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return "", ErrTokenInvalid
	}
	if t.now().Unix() > exp {
		return "", ErrTokenExpired
	}

	if parts[1] != requesterID {
		return "", ErrNotOwner
	}
	return parts[0], nil
}

func (t *TokenIssuer) sign(payload string) string {
	mac := hmac.New(sha256.New, t.key)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
