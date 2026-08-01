package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strconv"
)

// SignWebhookPayload computes the HMAC-SHA256 signature Nester sends on every
// webhook delivery (#836). The MAC covers "{timestamp}.{payload}" — not the
// payload alone — so a captured signature cannot be replayed against a
// different timestamp, and recipients are expected to reject a timestamp
// too far from "now" (documented in docs/webhooks.md alongside this).
//
// Returned as a hex string with no "sha256=" prefix; NewWebhookSignatureHeader
// builds the header value integrators actually see.
func SignWebhookPayload(secret []byte, timestamp int64, payload []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte("."))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// NewWebhookSignatureHeader builds the X-Nester-Signature header value:
// "t={timestamp},v1={hex hmac}" — versioned (v1=) so a future signing scheme
// change can add a new prefix without breaking existing integrators.
func NewWebhookSignatureHeader(secret []byte, timestamp int64, payload []byte) string {
	sig := SignWebhookPayload(secret, timestamp, payload)
	return "t=" + strconv.FormatInt(timestamp, 10) + ",v1=" + sig
}

// VerifyWebhookSignature recomputes the signature and compares it to want in
// constant time. Exported so integrators' verification code (and this
// package's own tests) can be checked against the exact same logic Nester
// itself uses to sign.
func VerifyWebhookSignature(secret []byte, timestamp int64, payload []byte, want string) bool {
	got := SignWebhookPayload(secret, timestamp, payload)
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
