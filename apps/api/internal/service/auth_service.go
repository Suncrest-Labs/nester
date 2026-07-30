package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/stellar/go/keypair"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/session"
)

// sep53MessagePrefix is the fixed prefix defined by SEP-53 ("Sign and Verify
// Messages") to prevent a signed challenge from being confused with a raw
// transaction. Wallets (Freighter, etc.) hash `prefix + message` with SHA-256
// before signing, so verification must reconstruct the same payload.
const sep53MessagePrefix = "Stellar Signed Message:\n"

func sep53MessageHash(message string) [32]byte {
	return sha256.Sum256([]byte(sep53MessagePrefix + message))
}

var (
	ErrChallengeExpired          = errors.New("challenge expired or invalid")
	ErrSignatureInvalid          = errors.New("signature is invalid")
	ErrWalletInvalid             = errors.New("wallet address is invalid")
	ErrDeviceFingerprintRequired = errors.New("device fingerprint is required")
	// ErrRefreshFailed is the single generic error surfaced to callers of
	// Refresh, regardless of the internal cause (invalid, expired, reused,
	// device mismatch, session revoked/expired) — the distinction must not
	// leak over the wire, only into the audit log / anomaly hook.
	ErrRefreshFailed = errors.New("refresh token is invalid or expired")
)

// SessionMetadata carries the request context captured when a session is
// created or refreshed.
type SessionMetadata struct {
	// DeviceFingerprint is a client-generated, client-persisted opaque ID
	// bound to the session at creation and enforced on every refresh.
	DeviceFingerprint string
	UserAgent         string
	IPAddress         string
}

// Tokens is the access/refresh token pair returned by VerifyAndIssue and Refresh.
type Tokens struct {
	AccessToken      string
	RefreshToken     string
	ExpiresIn        int64 // access-token seconds
	RefreshExpiresIn int64 // refresh-token seconds, for the handler's cookie MaxAge
	SessionID        uuid.UUID
	UserID           uuid.UUID
}

// WSSessionCloser lets the auth service force-close live WebSocket
// connections when a session is revoked, without importing the ws package
// directly. *ws.Hub satisfies this interface structurally.
type WSSessionCloser interface {
	CloseConnectionsForSession(sessionID string)
	CloseConnectionsForUser(userID string)
}

type AuthService interface {
	GenerateChallenge(ctx context.Context, walletAddress string) (string, error)
	VerifyAndIssue(ctx context.Context, walletAddress, signature, challenge string, meta SessionMetadata) (Tokens, error)
	Refresh(ctx context.Context, rawRefreshToken string, meta SessionMetadata) (Tokens, error)
	Logout(ctx context.Context, userID, sessionID uuid.UUID) error
	LogoutAll(ctx context.Context, userID uuid.UUID) (int, error)
	ListSessions(ctx context.Context, userID uuid.UUID) ([]session.Session, error)
	RevokeSession(ctx context.Context, userID, sessionID uuid.UUID) error
}

type AuthConfig interface {
	Secret() string
	AccessTokenExpiry() time.Duration
	RefreshTokenExpiry() time.Duration
	AbsoluteSessionLifetime() time.Duration
	ChallengeExpiry() time.Duration
}

type authService struct {
	store       ChallengeStore
	userService *UserService
	sessionRepo session.Repository
	revocation  RevocationCache
	anomaly     AnomalyDetector
	audit       AuditLogger
	wsHub       WSSessionCloser
	config      AuthConfig
}

func NewAuthService(
	store ChallengeStore,
	userService *UserService,
	sessionRepo session.Repository,
	revocation RevocationCache,
	anomaly AnomalyDetector,
	audit AuditLogger,
	wsHub WSSessionCloser,
	cfg AuthConfig,
) AuthService {
	return &authService{
		store:       store,
		userService: userService,
		sessionRepo: sessionRepo,
		revocation:  revocation,
		anomaly:     anomaly,
		audit:       audit,
		wsHub:       wsHub,
		config:      cfg,
	}
}

func (s *authService) GenerateChallenge(ctx context.Context, walletAddress string) (string, error) {
	if _, err := keypair.ParseAddress(walletAddress); err != nil {
		return "", ErrWalletInvalid
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	challenge := hex.EncodeToString(b)

	if err := s.store.Set(ctx, walletAddress, challenge); err != nil {
		return "", err
	}
	return challenge, nil
}

func (s *authService) VerifyAndIssue(ctx context.Context, walletAddress, signature, challenge string, meta SessionMetadata) (Tokens, error) {
	if meta.DeviceFingerprint == "" {
		return Tokens{}, ErrDeviceFingerprintRequired
	}

	stored, err := s.store.GetAndDelete(ctx, walletAddress)
	if err != nil {
		if errors.Is(err, ErrChallengeNotFound) {
			return Tokens{}, ErrChallengeExpired
		}
		return Tokens{}, err
	}

	if stored != challenge {
		return Tokens{}, ErrChallengeExpired
	}

	kp, err := keypair.ParseAddress(walletAddress)
	if err != nil {
		return Tokens{}, ErrWalletInvalid
	}

	sigBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return Tokens{}, ErrSignatureInvalid
	}

	hash := sep53MessageHash(challenge)
	if err := kp.Verify(hash[:], sigBytes); err != nil {
		return Tokens{}, ErrSignatureInvalid
	}

	user, err := s.userService.GetUserByWallet(ctx, walletAddress)
	if err != nil {
		user, err = s.userService.RegisterUser(ctx, walletAddress, walletAddress[:8])
		if err != nil {
			return Tokens{}, err
		}
	}

	roles, err := s.userService.GetUserRoles(ctx, user.ID)
	if err != nil {
		return Tokens{}, err
	}

	now := time.Now()
	rawRefresh, err := generateOpaqueToken()
	if err != nil {
		return Tokens{}, err
	}

	sess := &session.Session{
		UserID:            user.ID,
		WalletAddress:     walletAddress,
		DeviceFingerprint: meta.DeviceFingerprint,
		UserAgent:         nonEmptyPtr(meta.UserAgent),
		IPAddress:         nonEmptyPtr(meta.IPAddress),
		AbsoluteExpiresAt: now.Add(s.config.AbsoluteSessionLifetime()),
	}
	if _, err := s.sessionRepo.CreateSession(ctx, sess, hashOpaqueToken(rawRefresh), now.Add(s.config.RefreshTokenExpiry())); err != nil {
		return Tokens{}, err
	}

	accessToken, err := auth.MakeJWT(auth.Claims{
		Subject:       user.ID.String(),
		WalletAddress: walletAddress,
		IssuedAt:      now.Unix(),
		ExpiresAt:     now.Add(s.config.AccessTokenExpiry()).Unix(),
		Roles:         roles,
		SessionID:     sess.ID.String(),
		TokenID:       uuid.NewString(),
	}, s.config.Secret())
	if err != nil {
		return Tokens{}, err
	}

	s.anomaly.OnLoginSuccess(ctx, LoginEvent{
		UserID:            user.ID,
		SessionID:         sess.ID,
		WalletAddress:     walletAddress,
		IPAddress:         meta.IPAddress,
		UserAgent:         meta.UserAgent,
		DeviceFingerprint: meta.DeviceFingerprint,
		At:                now,
	})
	_ = s.audit.Log(ctx, AuditEntry{
		UserID:     &user.ID,
		Action:     "session.created",
		EntityType: "session",
		EntityID:   sess.ID,
		IPAddress:  meta.IPAddress,
	})

	return Tokens{
		AccessToken:      accessToken,
		RefreshToken:     rawRefresh,
		ExpiresIn:        int64(s.config.AccessTokenExpiry().Seconds()),
		RefreshExpiresIn: int64(s.config.RefreshTokenExpiry().Seconds()),
		SessionID:        sess.ID,
		UserID:           user.ID,
	}, nil
}

func (s *authService) Refresh(ctx context.Context, rawRefreshToken string, meta SessionMetadata) (Tokens, error) {
	if rawRefreshToken == "" {
		return Tokens{}, ErrRefreshFailed
	}
	if meta.DeviceFingerprint == "" {
		return Tokens{}, ErrDeviceFingerprintRequired
	}

	now := time.Now()
	newRaw, err := generateOpaqueToken()
	if err != nil {
		return Tokens{}, err
	}

	sess, _, err := s.sessionRepo.RotateRefreshToken(
		ctx,
		hashOpaqueToken(rawRefreshToken),
		meta.DeviceFingerprint,
		hashOpaqueToken(newRaw),
		now.Add(s.config.RefreshTokenExpiry()),
	)
	if err != nil {
		switch {
		case errors.Is(err, session.ErrRefreshTokenReused):
			s.finalizeRevocation(ctx, sess, session.ReasonReuseDetected, meta, true)
		case errors.Is(err, session.ErrDeviceMismatch):
			s.finalizeRevocation(ctx, sess, session.ReasonDeviceMismatch, meta, true)
		case errors.Is(err, session.ErrSessionExpired):
			s.finalizeRevocation(ctx, sess, session.ReasonAbsoluteExpiry, meta, false)
		case errors.Is(err, session.ErrRefreshTokenExpired):
			s.finalizeRevocation(ctx, sess, session.ReasonRefreshExpired, meta, false)
		}
		return Tokens{}, ErrRefreshFailed
	}

	roles, err := s.userService.GetUserRoles(ctx, sess.UserID)
	if err != nil {
		return Tokens{}, err
	}

	accessToken, err := auth.MakeJWT(auth.Claims{
		Subject:       sess.UserID.String(),
		WalletAddress: sess.WalletAddress,
		IssuedAt:      now.Unix(),
		ExpiresAt:     now.Add(s.config.AccessTokenExpiry()).Unix(),
		Roles:         roles,
		SessionID:     sess.ID.String(),
		TokenID:       uuid.NewString(),
	}, s.config.Secret())
	if err != nil {
		return Tokens{}, err
	}

	return Tokens{
		AccessToken:      accessToken,
		RefreshToken:     newRaw,
		ExpiresIn:        int64(s.config.AccessTokenExpiry().Seconds()),
		RefreshExpiresIn: int64(s.config.RefreshTokenExpiry().Seconds()),
		SessionID:        sess.ID,
		UserID:           sess.UserID,
	}, nil
}

// finalizeRevocation propagates a session's death (already persisted to
// Postgres by the repository call that returned the triggering error) to the
// Redis revocation cache, any live WebSocket connections, and the audit log.
// isCompromise gates whether the anomaly hook fires — genuine theft signals
// (reuse, device mismatch) alert; natural expiry does not.
func (s *authService) finalizeRevocation(ctx context.Context, sess *session.Session, reason string, meta SessionMetadata, isCompromise bool) {
	if sess == nil {
		return
	}
	ttl := s.config.AccessTokenExpiry() + time.Minute
	_ = s.revocation.MarkRevoked(ctx, sess.ID.String(), ttl)
	s.wsHub.CloseConnectionsForSession(sess.ID.String())
	_ = s.audit.Log(ctx, AuditEntry{
		UserID:     &sess.UserID,
		Action:     "session." + reason,
		EntityType: "session",
		EntityID:   sess.ID,
		IPAddress:  meta.IPAddress,
	})
	if isCompromise {
		s.anomaly.OnRefreshReuseDetected(ctx, ReuseEvent{
			SessionID: sess.ID,
			UserID:    sess.UserID,
			Reason:    reason,
			IPAddress: meta.IPAddress,
			UserAgent: meta.UserAgent,
			At:        time.Now(),
		})
	}
}

// revokeOwnedSession revokes sessionID after confirming it belongs to
// userID, propagating the same side effects as finalizeRevocation.
func (s *authService) revokeOwnedSession(ctx context.Context, userID, sessionID uuid.UUID, reason string) error {
	sess, err := s.sessionRepo.GetSessionByID(ctx, sessionID)
	if err != nil {
		return err
	}
	if sess.UserID != userID {
		return session.ErrSessionNotFound
	}

	if err := s.sessionRepo.RevokeSession(ctx, sessionID, reason); err != nil {
		return err
	}

	ttl := s.config.AccessTokenExpiry() + time.Minute
	_ = s.revocation.MarkRevoked(ctx, sessionID.String(), ttl)
	s.wsHub.CloseConnectionsForSession(sessionID.String())
	_ = s.audit.Log(ctx, AuditEntry{
		UserID:     &userID,
		Action:     "session." + reason,
		EntityType: "session",
		EntityID:   sessionID,
	})
	return nil
}

func (s *authService) Logout(ctx context.Context, userID, sessionID uuid.UUID) error {
	return s.revokeOwnedSession(ctx, userID, sessionID, session.ReasonLogout)
}

func (s *authService) RevokeSession(ctx context.Context, userID, sessionID uuid.UUID) error {
	return s.revokeOwnedSession(ctx, userID, sessionID, session.ReasonLogout)
}

func (s *authService) LogoutAll(ctx context.Context, userID uuid.UUID) (int, error) {
	revoked, err := s.sessionRepo.RevokeAllByUser(ctx, userID, session.ReasonLogoutAll)
	if err != nil {
		return 0, err
	}

	ttl := s.config.AccessTokenExpiry() + time.Minute
	for _, sess := range revoked {
		_ = s.revocation.MarkRevoked(ctx, sess.ID.String(), ttl)
	}
	s.wsHub.CloseConnectionsForUser(userID.String())
	_ = s.audit.Log(ctx, AuditEntry{
		UserID:     &userID,
		Action:     "session.logout_all",
		EntityType: "user",
		EntityID:   userID,
		NewValue:   map[string]int{"revoked_count": len(revoked)},
	})

	return len(revoked), nil
}

func (s *authService) ListSessions(ctx context.Context, userID uuid.UUID) ([]session.Session, error) {
	return s.sessionRepo.ListActiveByUser(ctx, userID)
}

// ── helpers ─────────────────────────────────────────────────────────────────

func generateOpaqueToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashOpaqueToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func nonEmptyPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
