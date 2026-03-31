package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Sentinel errors returned by ParseJWT.
var (
	ErrTokenMalformed = errors.New("auth: token is malformed")
	ErrTokenExpired   = errors.New("auth: token has expired")
	ErrTokenInvalid   = errors.New("auth: token signature is invalid")
)

// Claims is the JWT payload understood by this service.
type Claims struct {
	Subject       string   `json:"sub"`
	WalletAddress string   `json:"wallet,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
	Roles         []string `json:"roles,omitempty"`
	ExpiresAt     int64    `json:"exp,omitempty"`
	IssuedAt      int64    `json:"iat,omitempty"`
}

// ParseJWT validates an HS256 Bearer token signed with secret and returns
// its claims.  It returns ErrTokenMalformed, ErrTokenInvalid, or
// ErrTokenExpired on the respective failure modes.
func ParseJWT(tokenStr, secret string) (Claims, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return Claims{}, ErrTokenMalformed
	}

	// Verify HMAC-SHA256 signature.
	mac := signHS256(parts[0]+"."+parts[1], secret)
	expected := base64.RawURLEncoding.EncodeToString(mac)
	if !hmac.Equal([]byte(parts[2]), []byte(expected)) {
		return Claims{}, ErrTokenInvalid
	}

	// Decode payload.
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrTokenMalformed
	}

	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, ErrTokenMalformed
	}

	if claims.ExpiresAt > 0 && time.Now().Unix() > claims.ExpiresAt {
		return Claims{}, ErrTokenExpired
	}

	return claims, nil
}

// MakeJWT issues a signed HS256 JWT.  It is intended for token issuance and
// test helpers.
func MakeJWT(claims Claims, secret string) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	p := base64.RawURLEncoding.EncodeToString(payload)

	sig := base64.RawURLEncoding.EncodeToString(signHS256(header+"."+p, secret))
	return header + "." + p + "." + sig, nil
}

func signHS256(data, secret string) []byte {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(data))
	return h.Sum(nil)
}
