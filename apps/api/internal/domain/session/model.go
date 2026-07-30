package session

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrSessionNotFound     = errors.New("session: not found")
	ErrSessionRevoked      = errors.New("session: revoked")
	ErrSessionExpired      = errors.New("session: absolute lifetime exceeded")
	ErrRefreshTokenInvalid = errors.New("session: refresh token invalid")
	ErrRefreshTokenExpired = errors.New("session: refresh token expired")
	ErrRefreshTokenReused  = errors.New("session: refresh token reuse detected")
	ErrDeviceMismatch      = errors.New("session: device fingerprint mismatch")
)

// Revoke reasons — persisted as sessions.revoked_reason / refresh_tokens.revoked_reason
// and used as the audit-log action suffix.
const (
	ReasonLogout         = "logout"
	ReasonLogoutAll      = "logout_all"
	ReasonReuseDetected  = "reuse_detected"
	ReasonDeviceMismatch = "device_mismatch"
	ReasonAbsoluteExpiry = "absolute_expiry"
	ReasonRefreshExpired = "refresh_expired"
	ReasonAdmin          = "admin"
)

type Session struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	WalletAddress     string
	DeviceFingerprint string
	UserAgent         *string
	IPAddress         *string
	CreatedAt         time.Time
	LastActiveAt      time.Time
	AbsoluteExpiresAt time.Time
	RevokedAt         *time.Time
	RevokedReason     *string
}

func (s Session) IsActive(now time.Time) bool {
	return s.RevokedAt == nil && now.Before(s.AbsoluteExpiresAt)
}

type RefreshToken struct {
	ID            uuid.UUID
	SessionID     uuid.UUID
	TokenHash     string
	ParentID      *uuid.UUID
	IssuedAt      time.Time
	ExpiresAt     time.Time
	UsedAt        *time.Time
	RevokedAt     *time.Time
	RevokedReason *string
}

// Repository persists sessions and their refresh-token rotation chains.
// Implementations must be safe for concurrent use.
type Repository interface {
	// CreateSession inserts a session row and its first refresh_tokens row
	// in one transaction.
	CreateSession(ctx context.Context, s *Session, refreshTokenHash string, refreshExpiresAt time.Time) (*RefreshToken, error)

	// RotateRefreshToken implements rotation + reuse-detection + device-binding
	// in one DB transaction. rawTokenHash is the hash of the refresh token the
	// caller presented; presentedFingerprint is the caller's current device
	// fingerprint, checked against the session's bound fingerprint.
	//
	// On success returns the (updated) session and the newly-minted refresh
	// token. On ErrRefreshTokenReused or ErrDeviceMismatch, the session
	// pointer is still populated (its ID) so the caller can revoke the Redis
	// cache entry, close WS connections, and audit-log against it even
	// though the whole family was just destroyed.
	RotateRefreshToken(ctx context.Context, rawTokenHash, presentedFingerprint, newTokenHash string, newExpiresAt time.Time) (*Session, *RefreshToken, error)

	GetSessionByID(ctx context.Context, id uuid.UUID) (*Session, error)
	ListActiveByUser(ctx context.Context, userID uuid.UUID) ([]Session, error)
	RevokeSession(ctx context.Context, id uuid.UUID, reason string) error
	RevokeAllByUser(ctx context.Context, userID uuid.UUID, reason string) ([]Session, error)
}
