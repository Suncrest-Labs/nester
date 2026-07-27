package service

import (
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stellar/go/keypair"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/session"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
)

// ── mocks / fakes ────────────────────────────────────────────────────────────

type mockAuthConfig struct {
	secret                  string
	accessTokenExpiry       time.Duration
	refreshTokenExpiry      time.Duration
	absoluteSessionLifetime time.Duration
	challengeExpiry         time.Duration
}

func (m mockAuthConfig) Secret() string                         { return m.secret }
func (m mockAuthConfig) AccessTokenExpiry() time.Duration       { return m.accessTokenExpiry }
func (m mockAuthConfig) RefreshTokenExpiry() time.Duration      { return m.refreshTokenExpiry }
func (m mockAuthConfig) AbsoluteSessionLifetime() time.Duration { return m.absoluteSessionLifetime }
func (m mockAuthConfig) ChallengeExpiry() time.Duration         { return m.challengeExpiry }

func defaultMockAuthConfig() mockAuthConfig {
	return mockAuthConfig{
		secret:                  "test-super-secret-key-that-is-32-bytes-long",
		accessTokenExpiry:       1 * time.Hour,
		refreshTokenExpiry:      24 * time.Hour,
		absoluteSessionLifetime: 30 * 24 * time.Hour,
		challengeExpiry:         5 * time.Minute,
	}
}

type mockAuthUserRepository struct {
	users map[string]*user.User
	roles map[uuid.UUID][]string
}

func (m *mockAuthUserRepository) Create(ctx context.Context, u *user.User) error {
	m.users[u.WalletAddress] = u
	return nil
}

func (m *mockAuthUserRepository) GetByID(ctx context.Context, id uuid.UUID) (*user.User, error) {
	return nil, errors.New("not found")
}

func (m *mockAuthUserRepository) GetByWalletAddress(ctx context.Context, address string) (*user.User, error) {
	if u, ok := m.users[address]; ok {
		return u, nil
	}
	return nil, errors.New("not found")
}

func (m *mockAuthUserRepository) GetRoles(ctx context.Context, id uuid.UUID) ([]string, error) {
	if roles, ok := m.roles[id]; ok {
		return roles, nil
	}
	return []string{}, nil
}

func (m *mockAuthUserRepository) SaveKYCDocument(_ context.Context, _ *user.KYCDocument, _ *user.EncryptedKYCDoc) error {
	return nil
}

func (m *mockAuthUserRepository) GetKYCDocument(_ context.Context, _ uuid.UUID) (*user.KYCDocument, *user.EncryptedKYCDoc, error) {
	return nil, nil, user.ErrUserNotFound
}

func (m *mockAuthUserRepository) UpdateKYCStatus(_ context.Context, _ uuid.UUID, _ user.KYCStatus, _ *string, _ *time.Time) error {
	return nil
}

func (m *mockAuthUserRepository) UpdateProfile(_ context.Context, _ uuid.UUID, _ user.ProfilePatch) (*user.User, error) {
	return nil, errors.New("not implemented")
}

func newMockRepo() *mockAuthUserRepository {
	return &mockAuthUserRepository{
		users: make(map[string]*user.User),
		roles: make(map[uuid.UUID][]string),
	}
}

// fakeSessionRepository is an in-memory stand-in for the Postgres-backed
// session.Repository, replicating the same rotation/reuse/device-mismatch/
// absolute-expiry semantics so the service-layer logic can be unit-tested
// without a live database. The real repository's transactional behavior
// against Postgres is covered separately by integration tests.
type fakeSessionRepository struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]*session.Session
	tokens   map[string]*session.RefreshToken
}

func newFakeSessionRepository() *fakeSessionRepository {
	return &fakeSessionRepository{
		sessions: make(map[uuid.UUID]*session.Session),
		tokens:   make(map[string]*session.RefreshToken),
	}
}

func (r *fakeSessionRepository) CreateSession(_ context.Context, s *session.Session, refreshTokenHash string, refreshExpiresAt time.Time) (*session.RefreshToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s.ID = uuid.New()
	s.CreatedAt = time.Now()
	s.LastActiveAt = s.CreatedAt
	cp := *s
	r.sessions[s.ID] = &cp

	rt := &session.RefreshToken{
		ID:        uuid.New(),
		SessionID: s.ID,
		TokenHash: refreshTokenHash,
		IssuedAt:  time.Now(),
		ExpiresAt: refreshExpiresAt,
	}
	r.tokens[refreshTokenHash] = rt
	return rt, nil
}

func (r *fakeSessionRepository) RotateRefreshToken(_ context.Context, rawTokenHash, presentedFingerprint, newTokenHash string, newExpiresAt time.Time) (*session.Session, *session.RefreshToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	tok, ok := r.tokens[rawTokenHash]
	if !ok {
		return nil, nil, session.ErrRefreshTokenInvalid
	}
	sess, ok := r.sessions[tok.SessionID]
	if !ok {
		return nil, nil, session.ErrRefreshTokenInvalid
	}

	now := time.Now()

	if sess.RevokedAt != nil {
		cp := *sess
		return &cp, nil, session.ErrSessionRevoked
	}
	if now.After(sess.AbsoluteExpiresAt) {
		r.revokeSessionLocked(sess, session.ReasonAbsoluteExpiry, now)
		cp := *sess
		return &cp, nil, session.ErrSessionExpired
	}
	if presentedFingerprint != sess.DeviceFingerprint {
		r.revokeSessionLocked(sess, session.ReasonDeviceMismatch, now)
		cp := *sess
		return &cp, nil, session.ErrDeviceMismatch
	}
	if tok.UsedAt != nil || tok.RevokedAt != nil {
		r.revokeSessionLocked(sess, session.ReasonReuseDetected, now)
		cp := *sess
		return &cp, nil, session.ErrRefreshTokenReused
	}
	if now.After(tok.ExpiresAt) {
		reason := session.ReasonRefreshExpired
		tok.RevokedAt, tok.RevokedReason = &now, &reason
		r.revokeSessionLocked(sess, session.ReasonRefreshExpired, now)
		cp := *sess
		return &cp, nil, session.ErrRefreshTokenExpired
	}

	tok.UsedAt = &now
	newRT := &session.RefreshToken{
		ID:        uuid.New(),
		SessionID: sess.ID,
		TokenHash: newTokenHash,
		ParentID:  &tok.ID,
		IssuedAt:  now,
		ExpiresAt: newExpiresAt,
	}
	r.tokens[newTokenHash] = newRT
	sess.LastActiveAt = now

	cp := *sess
	return &cp, newRT, nil
}

func (r *fakeSessionRepository) revokeSessionLocked(sess *session.Session, reason string, now time.Time) {
	sess.RevokedAt = &now
	rc := reason
	sess.RevokedReason = &rc
	for _, t := range r.tokens {
		if t.SessionID == sess.ID && t.RevokedAt == nil {
			t.RevokedAt = &now
			trc := reason
			t.RevokedReason = &trc
		}
	}
}

func (r *fakeSessionRepository) GetSessionByID(_ context.Context, id uuid.UUID) (*session.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil, session.ErrSessionNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *fakeSessionRepository) ListActiveByUser(_ context.Context, userID uuid.UUID) ([]session.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	var out []session.Session
	for _, s := range r.sessions {
		if s.UserID == userID && s.RevokedAt == nil && now.Before(s.AbsoluteExpiresAt) {
			out = append(out, *s)
		}
	}
	return out, nil
}

func (r *fakeSessionRepository) RevokeSession(_ context.Context, id uuid.UUID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return session.ErrSessionNotFound
	}
	if s.RevokedAt == nil {
		r.revokeSessionLocked(s, reason, time.Now())
	}
	return nil
}

func (r *fakeSessionRepository) RevokeAllByUser(_ context.Context, userID uuid.UUID, reason string) ([]session.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	var out []session.Session
	for _, s := range r.sessions {
		if s.UserID == userID && s.RevokedAt == nil {
			r.revokeSessionLocked(s, reason, now)
			out = append(out, *s)
		}
	}
	return out, nil
}

type fakeRevocationCache struct {
	mu      sync.Mutex
	revoked map[string]bool
}

func newFakeRevocationCache() *fakeRevocationCache {
	return &fakeRevocationCache{revoked: make(map[string]bool)}
}

func (c *fakeRevocationCache) IsRevoked(_ context.Context, sessionID string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.revoked[sessionID], nil
}

func (c *fakeRevocationCache) MarkRevoked(_ context.Context, sessionID string, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.revoked[sessionID] = true
	return nil
}

type fakeWSHub struct {
	mu             sync.Mutex
	closedSessions []string
	closedUsers    []string
}

func (h *fakeWSHub) CloseConnectionsForSession(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closedSessions = append(h.closedSessions, sessionID)
}

func (h *fakeWSHub) CloseConnectionsForUser(userID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closedUsers = append(h.closedUsers, userID)
}

type recordingAuditLogger struct {
	mu      sync.Mutex
	entries []AuditEntry
}

func (l *recordingAuditLogger) Log(_ context.Context, e AuditEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, e)
	return nil
}

func (l *recordingAuditLogger) actions() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.entries))
	for i, e := range l.entries {
		out[i] = e.Action
	}
	return out
}

type recordingAnomalyDetector struct {
	mu          sync.Mutex
	loginEvents []LoginEvent
	reuseEvents []ReuseEvent
}

func (d *recordingAnomalyDetector) OnLoginSuccess(_ context.Context, evt LoginEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.loginEvents = append(d.loginEvents, evt)
}

func (d *recordingAnomalyDetector) OnRefreshReuseDetected(_ context.Context, evt ReuseEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.reuseEvents = append(d.reuseEvents, evt)
}

type authTestDeps struct {
	svc        AuthService
	sessions   *fakeSessionRepository
	revocation *fakeRevocationCache
	wsHub      *fakeWSHub
	audit      *recordingAuditLogger
	anomaly    *recordingAnomalyDetector
}

func newAuthServiceForTest(cfg mockAuthConfig, repo *mockAuthUserRepository) authTestDeps {
	userService := NewUserService(repo)
	store := NewInMemoryChallengeStore(cfg.ChallengeExpiry())
	deps := authTestDeps{
		sessions:   newFakeSessionRepository(),
		revocation: newFakeRevocationCache(),
		wsHub:      &fakeWSHub{},
		audit:      &recordingAuditLogger{},
		anomaly:    &recordingAnomalyDetector{},
	}
	deps.svc = NewAuthService(store, userService, deps.sessions, deps.revocation, deps.anomaly, deps.audit, deps.wsHub, cfg)
	return deps
}

func setupAuthService() (AuthService, *keypair.Full) {
	deps := newAuthServiceForTest(defaultMockAuthConfig(), newMockRepo())
	kp, _ := keypair.Random()
	return deps.svc, kp
}

const testDeviceFingerprint = "test-device-fingerprint"

func testMeta() SessionMetadata {
	return SessionMetadata{DeviceFingerprint: testDeviceFingerprint, UserAgent: "test-agent/1.0", IPAddress: "127.0.0.1"}
}

func signChallenge(t *testing.T, kp *keypair.Full, challenge string) string {
	t.Helper()
	hash := sep53MessageHash(challenge)
	sigBytes, err := kp.Sign(hash[:])
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(sigBytes)
}

// verifyAndIssue drives the full challenge -> sign -> verify flow and returns
// the resulting token pair.
func verifyAndIssue(t *testing.T, svc AuthService, kp *keypair.Full, meta SessionMetadata) Tokens {
	t.Helper()
	challenge, err := svc.GenerateChallenge(context.Background(), kp.Address())
	require.NoError(t, err)
	sig := signChallenge(t, kp, challenge)
	tokens, err := svc.VerifyAndIssue(context.Background(), kp.Address(), sig, challenge, meta)
	require.NoError(t, err)
	return tokens
}

// ── GenerateChallenge / VerifyAndIssue ──────────────────────────────────────

func TestAuthService_GenerateChallenge(t *testing.T) {
	svc, kp := setupAuthService()

	challenge, err := svc.GenerateChallenge(context.Background(), kp.Address())
	require.NoError(t, err)
	assert.NotEmpty(t, challenge)
	assert.Len(t, challenge, 64) // hex encoding of 32 bytes

	// Invalid wallet
	_, err = svc.GenerateChallenge(context.Background(), "invalid-wallet")
	assert.ErrorIs(t, err, ErrWalletInvalid)
}

func TestAuthService_VerifyAndIssue_Success(t *testing.T) {
	svc, kp := setupAuthService()

	challenge, err := svc.GenerateChallenge(context.Background(), kp.Address())
	require.NoError(t, err)

	sigStr := signChallenge(t, kp, challenge)

	tokens, err := svc.VerifyAndIssue(context.Background(), kp.Address(), sigStr, challenge, testMeta())
	require.NoError(t, err)
	assert.NotEmpty(t, tokens.AccessToken)
	assert.NotEmpty(t, tokens.RefreshToken)
	assert.NotEqual(t, uuid.Nil, tokens.SessionID)
	assert.Greater(t, tokens.ExpiresIn, int64(0))

	// One-time use: using the same challenge again should fail.
	_, err = svc.VerifyAndIssue(context.Background(), kp.Address(), sigStr, challenge, testMeta())
	assert.ErrorIs(t, err, ErrChallengeExpired)
}

func TestAuthService_VerifyAndIssue_RequiresDeviceFingerprint(t *testing.T) {
	svc, kp := setupAuthService()

	challenge, err := svc.GenerateChallenge(context.Background(), kp.Address())
	require.NoError(t, err)
	sigStr := signChallenge(t, kp, challenge)

	meta := testMeta()
	meta.DeviceFingerprint = ""
	_, err = svc.VerifyAndIssue(context.Background(), kp.Address(), sigStr, challenge, meta)
	assert.ErrorIs(t, err, ErrDeviceFingerprintRequired)
}

func TestAuthService_VerifyAndIssue_InvalidSignature(t *testing.T) {
	svc, kp := setupAuthService()

	challenge, err := svc.GenerateChallenge(context.Background(), kp.Address())
	require.NoError(t, err)

	// Use another keypair's signature.
	randomKp, _ := keypair.Random()
	hash := sep53MessageHash(challenge)
	sigBytes, _ := randomKp.Sign(hash[:])
	sigStr := base64.StdEncoding.EncodeToString(sigBytes)

	_, err = svc.VerifyAndIssue(context.Background(), kp.Address(), sigStr, challenge, testMeta())
	assert.ErrorIs(t, err, ErrSignatureInvalid)
}

func TestAuthService_VerifyAndIssue_ExpiredChallenge(t *testing.T) {
	cfg := defaultMockAuthConfig()
	cfg.challengeExpiry = -1 * time.Second
	deps := newAuthServiceForTest(cfg, newMockRepo())

	kp, _ := keypair.Random()
	challenge, _ := deps.svc.GenerateChallenge(context.Background(), kp.Address())
	sigStr := signChallenge(t, kp, challenge)

	_, err := deps.svc.VerifyAndIssue(context.Background(), kp.Address(), sigStr, challenge, testMeta())
	assert.ErrorIs(t, err, ErrChallengeExpired)
}

func TestAuthService_VerifyAndIssue_AdminRolePopulatedInToken(t *testing.T) {
	cfg := defaultMockAuthConfig()
	repo := newMockRepo()
	kp, _ := keypair.Random()

	// Pre-seed the user so we know their ID, then assign admin role.
	adminUser := &user.User{
		ID:            uuid.New(),
		WalletAddress: kp.Address(),
		DisplayName:   kp.Address()[:8],
		KYCStatus:     user.KYCStatusPending,
	}
	repo.users[kp.Address()] = adminUser
	repo.roles[adminUser.ID] = []string{"admin"}

	deps := newAuthServiceForTest(cfg, repo)

	tokens := verifyAndIssue(t, deps.svc, kp, testMeta())

	claims, err := auth.ParseJWT(tokens.AccessToken, cfg.secret)
	require.NoError(t, err)

	assert.Equal(t, []string{"admin"}, claims.Roles, "admin role must be present in issued token")
	assert.NotEmpty(t, claims.SessionID, "issued token must carry a session id")
}

func TestAuthService_VerifyAndIssue_RegularUserHasEmptyRoles(t *testing.T) {
	cfg := defaultMockAuthConfig()
	repo := newMockRepo()
	kp, _ := keypair.Random()
	deps := newAuthServiceForTest(cfg, repo)

	tokens := verifyAndIssue(t, deps.svc, kp, testMeta())

	claims, err := auth.ParseJWT(tokens.AccessToken, cfg.secret)
	require.NoError(t, err)

	assert.Empty(t, claims.Roles, "regular user must have no roles in issued token")
}

// ── Refresh: rotation, reuse detection, device binding, absolute lifetime ──

func TestAuthService_Refresh_RotatesToken(t *testing.T) {
	deps := newAuthServiceForTest(defaultMockAuthConfig(), newMockRepo())
	kp, _ := keypair.Random()

	first := verifyAndIssue(t, deps.svc, kp, testMeta())

	second, err := deps.svc.Refresh(context.Background(), first.RefreshToken, testMeta())
	require.NoError(t, err)
	assert.NotEqual(t, first.RefreshToken, second.RefreshToken, "refresh token must rotate")
	assert.NotEqual(t, first.AccessToken, second.AccessToken)
	assert.Equal(t, first.SessionID, second.SessionID, "rotation stays within the same session")

	// Reusing the original (now-rotated-away) refresh token must fail.
	_, err = deps.svc.Refresh(context.Background(), first.RefreshToken, testMeta())
	assert.ErrorIs(t, err, ErrRefreshFailed)
}

func TestAuthService_Refresh_ReuseDestroysWholeFamily(t *testing.T) {
	deps := newAuthServiceForTest(defaultMockAuthConfig(), newMockRepo())
	kp, _ := keypair.Random()

	first := verifyAndIssue(t, deps.svc, kp, testMeta())
	second, err := deps.svc.Refresh(context.Background(), first.RefreshToken, testMeta())
	require.NoError(t, err)

	// Trigger reuse detection with the stale first-generation token.
	_, err = deps.svc.Refresh(context.Background(), first.RefreshToken, testMeta())
	require.ErrorIs(t, err, ErrRefreshFailed)

	// The legitimately-rotated second-generation token must ALSO now fail —
	// proving the whole family was destroyed, not just the reused token.
	_, err = deps.svc.Refresh(context.Background(), second.RefreshToken, testMeta())
	assert.ErrorIs(t, err, ErrRefreshFailed)

	assert.True(t, deps.revocation.revoked[first.SessionID.String()], "session must be marked revoked in the cache")
	assert.Contains(t, deps.wsHub.closedSessions, first.SessionID.String(), "live connections for the session must be closed")
	require.Len(t, deps.anomaly.reuseEvents, 1)
	assert.Equal(t, session.ReasonReuseDetected, deps.anomaly.reuseEvents[0].Reason)
	assert.Contains(t, deps.audit.actions(), "session.reuse_detected")
}

func TestAuthService_Refresh_DeviceMismatchDestroysSession(t *testing.T) {
	deps := newAuthServiceForTest(defaultMockAuthConfig(), newMockRepo())
	kp, _ := keypair.Random()

	first := verifyAndIssue(t, deps.svc, kp, testMeta())

	otherDevice := testMeta()
	otherDevice.DeviceFingerprint = "a-different-device"
	_, err := deps.svc.Refresh(context.Background(), first.RefreshToken, otherDevice)
	require.ErrorIs(t, err, ErrRefreshFailed)

	// Even the original device is now locked out — the session is dead.
	_, err = deps.svc.Refresh(context.Background(), first.RefreshToken, testMeta())
	assert.ErrorIs(t, err, ErrRefreshFailed)

	require.Len(t, deps.anomaly.reuseEvents, 1)
	assert.Equal(t, session.ReasonDeviceMismatch, deps.anomaly.reuseEvents[0].Reason)
	assert.Contains(t, deps.audit.actions(), "session.device_mismatch")
}

func TestAuthService_Refresh_AbsoluteLifetimeExceededForcesReauth(t *testing.T) {
	cfg := defaultMockAuthConfig()
	cfg.absoluteSessionLifetime = -1 * time.Second // already exceeded at creation
	deps := newAuthServiceForTest(cfg, newMockRepo())
	kp, _ := keypair.Random()

	first := verifyAndIssue(t, deps.svc, kp, testMeta())

	_, err := deps.svc.Refresh(context.Background(), first.RefreshToken, testMeta())
	assert.ErrorIs(t, err, ErrRefreshFailed)
	assert.Contains(t, deps.audit.actions(), "session.absolute_expiry")
	// Natural expiry is not a compromise signal — no anomaly alert.
	assert.Empty(t, deps.anomaly.reuseEvents)
}

func TestAuthService_Refresh_RequiresDeviceFingerprint(t *testing.T) {
	deps := newAuthServiceForTest(defaultMockAuthConfig(), newMockRepo())
	kp, _ := keypair.Random()
	first := verifyAndIssue(t, deps.svc, kp, testMeta())

	meta := testMeta()
	meta.DeviceFingerprint = ""
	_, err := deps.svc.Refresh(context.Background(), first.RefreshToken, meta)
	assert.ErrorIs(t, err, ErrDeviceFingerprintRequired)
}

// ── Logout / LogoutAll / RevokeSession / ListSessions ───────────────────────

func TestAuthService_Logout_RevokesSession(t *testing.T) {
	repo := newMockRepo()
	deps := newAuthServiceForTest(defaultMockAuthConfig(), repo)
	kp, _ := keypair.Random()
	tokens := verifyAndIssue(t, deps.svc, kp, testMeta())
	userID := repo.users[kp.Address()].ID

	require.NoError(t, deps.svc.Logout(context.Background(), userID, tokens.SessionID))

	_, err := deps.svc.Refresh(context.Background(), tokens.RefreshToken, testMeta())
	assert.ErrorIs(t, err, ErrRefreshFailed)
	assert.True(t, deps.revocation.revoked[tokens.SessionID.String()])
	assert.Contains(t, deps.wsHub.closedSessions, tokens.SessionID.String())
}

func TestAuthService_RevokeSession_RejectsNonOwner(t *testing.T) {
	repo := newMockRepo()
	deps := newAuthServiceForTest(defaultMockAuthConfig(), repo)
	kp, _ := keypair.Random()
	tokens := verifyAndIssue(t, deps.svc, kp, testMeta())

	err := deps.svc.RevokeSession(context.Background(), uuid.New(), tokens.SessionID)
	assert.ErrorIs(t, err, session.ErrSessionNotFound, "terminating a session that isn't yours must not succeed or leak existence")
}

func TestAuthService_LogoutAll_RevokesAllSessions(t *testing.T) {
	repo := newMockRepo()
	deps := newAuthServiceForTest(defaultMockAuthConfig(), repo)
	kp, _ := keypair.Random()

	first := verifyAndIssue(t, deps.svc, kp, testMeta())
	second := verifyAndIssue(t, deps.svc, kp, testMeta())
	userID := repo.users[kp.Address()].ID

	count, err := deps.svc.LogoutAll(context.Background(), userID)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	_, err = deps.svc.Refresh(context.Background(), first.RefreshToken, testMeta())
	assert.ErrorIs(t, err, ErrRefreshFailed)
	_, err = deps.svc.Refresh(context.Background(), second.RefreshToken, testMeta())
	assert.ErrorIs(t, err, ErrRefreshFailed)

	assert.Contains(t, deps.wsHub.closedUsers, userID.String())
	assert.Contains(t, deps.audit.actions(), "session.logout_all")
}

func TestAuthService_ListSessions_ReturnsActiveSessions(t *testing.T) {
	repo := newMockRepo()
	deps := newAuthServiceForTest(defaultMockAuthConfig(), repo)
	kp, _ := keypair.Random()

	verifyAndIssue(t, deps.svc, kp, testMeta())
	verifyAndIssue(t, deps.svc, kp, testMeta())
	userID := repo.users[kp.Address()].ID

	sessions, err := deps.svc.ListSessions(context.Background(), userID)
	require.NoError(t, err)
	assert.Len(t, sessions, 2)
}
