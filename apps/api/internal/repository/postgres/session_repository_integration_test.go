package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/session"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// hashOpaqueToken mirrors service.hashOpaqueToken (unexported, package
// service) so test fixtures here hash refresh tokens exactly the way the
// real session repository does in production. Duplicated rather than
// exported from service: this is a test-fixture concern specific to
// constructing valid-looking rows, not something production code elsewhere
// in this package needs.
func hashOpaqueToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// seedSession inserts a session row and its first refresh token, returning
// both. absLifetime controls how far in the future AbsoluteExpiresAt is set;
// pass a negative duration to create an already-expired session.
func seedSession(t *testing.T, ctx context.Context, repo *SessionRepository, userID uuid.UUID, absLifetime, refreshLifetime time.Duration) (*session.Session, string) {
	t.Helper()

	now := time.Now()
	rawToken := "raw-token-" + uuid.NewString()
	tokenHash := hashOpaqueToken(rawToken)

	sess := &session.Session{
		UserID:            userID,
		WalletAddress:     "G" + userID.String()[:30],
		DeviceFingerprint: "device-" + uuid.NewString(),
		AbsoluteExpiresAt: now.Add(absLifetime),
	}

	_, err := repo.CreateSession(ctx, sess, tokenHash, now.Add(refreshLifetime))
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, sess.ID)

	return sess, rawToken
}

// ── CreateSession ─────────────────────────────────────────────────────────────

func TestSessionRepository_CreateSession_PersistsSessionAndToken(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	userID := seedIntegrationUser(t, db)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	sess, rawToken := seedSession(t, ctx, repo, userID, 30*24*time.Hour, 7*24*time.Hour)

	// Confirm the session is retrievable.
	fetched, err := repo.GetSessionByID(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, sess.ID, fetched.ID)
	assert.Equal(t, userID, fetched.UserID)
	assert.Nil(t, fetched.RevokedAt)

	// Confirm the refresh token is usable (rotate succeeds).
	newRaw := "new-raw-" + uuid.NewString()
	newHash := hashOpaqueToken(newRaw)
	_, newRT, err := repo.RotateRefreshToken(ctx,
		hashOpaqueToken(rawToken),
		sess.DeviceFingerprint,
		newHash,
		time.Now().Add(7*24*time.Hour),
	)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, newRT.ID)
}

// ── RotateRefreshToken: happy path ───────────────────────────────────────────

func TestSessionRepository_RotateRefreshToken_UpdatesLastActiveAt(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	userID := seedIntegrationUser(t, db)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	sess, rawToken := seedSession(t, ctx, repo, userID, 30*24*time.Hour, 7*24*time.Hour)
	before := sess.LastActiveAt

	// Brief sleep so the clock advances at least one millisecond.
	time.Sleep(2 * time.Millisecond)

	newRaw := "new-raw-" + uuid.NewString()
	updatedSess, _, err := repo.RotateRefreshToken(ctx,
		hashOpaqueToken(rawToken),
		sess.DeviceFingerprint,
		hashOpaqueToken(newRaw),
		time.Now().Add(7*24*time.Hour),
	)
	require.NoError(t, err)
	assert.True(t, updatedSess.LastActiveAt.After(before),
		"last_active_at must be updated after a successful rotation")
}

// ── RotateRefreshToken: absolute expiry ──────────────────────────────────────

func TestSessionRepository_RotateRefreshToken_AbsoluteExpiry_RevokesSessionAndErrors(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	userID := seedIntegrationUser(t, db)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	// Create a session whose absolute lifetime is already in the past.
	sess, rawToken := seedSession(t, ctx, repo, userID, -1*time.Second, 7*24*time.Hour)

	newRaw := "new-raw-" + uuid.NewString()
	returnedSess, _, err := repo.RotateRefreshToken(ctx,
		hashOpaqueToken(rawToken),
		sess.DeviceFingerprint,
		hashOpaqueToken(newRaw),
		time.Now().Add(7*24*time.Hour),
	)

	// Must return the domain sentinel.
	assert.ErrorIs(t, err, session.ErrSessionExpired,
		"rotation of an absolutely-expired session must return ErrSessionExpired")

	// The partial session must be returned so the caller can do side-effects
	// (revocation cache, WebSocket close, audit log) even though the DB row
	// was already mutated.
	require.NotNil(t, returnedSess, "session pointer must be non-nil on absolute expiry")
	assert.Equal(t, sess.ID, returnedSess.ID)

	// Confirm the session is now revoked in the database.
	persisted, err := repo.GetSessionByID(ctx, sess.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.RevokedAt, "session must be revoked in DB after absolute expiry")
	require.NotNil(t, persisted.RevokedReason)
	assert.Equal(t, session.ReasonAbsoluteExpiry, *persisted.RevokedReason)
}

func TestSessionRepository_RotateRefreshToken_AbsoluteExpiry_DoesNotSlideLonger(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	userID := seedIntegrationUser(t, db)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	// Session that will expire 50 ms from now — long enough to create, short
	// enough that a single rotation cannot extend it past its deadline.
	sess, rawToken := seedSession(t, ctx, repo, userID, 50*time.Millisecond, 7*24*time.Hour)

	// First rotation: succeeds because the session is still live.
	newRaw := "new-raw-" + uuid.NewString()
	_, newRT, err := repo.RotateRefreshToken(ctx,
		hashOpaqueToken(rawToken),
		sess.DeviceFingerprint,
		hashOpaqueToken(newRaw),
		time.Now().Add(7*24*time.Hour), // refresh window extends far beyond absolute limit
	)
	require.NoError(t, err, "first rotation must succeed while session is still live")

	// Wait for the absolute deadline to pass.
	time.Sleep(100 * time.Millisecond)

	// Second rotation: must fail with ErrSessionExpired, not succeed.
	// This is the core invariant: a long refresh-token window cannot push the
	// session past its absolute limit.
	newerRaw := "newer-raw-" + uuid.NewString()
	_, _, err = repo.RotateRefreshToken(ctx,
		newRT.TokenHash,
		sess.DeviceFingerprint,
		hashOpaqueToken(newerRaw),
		time.Now().Add(7*24*time.Hour),
	)
	assert.ErrorIs(t, err, session.ErrSessionExpired,
		"a refresh whose new window would extend past the absolute deadline must be rejected")
}

// ── RotateRefreshToken: reuse detection ─────────────────────────────────────

func TestSessionRepository_RotateRefreshToken_ReuseDetection_RevokesFamily(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	userID := seedIntegrationUser(t, db)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	sess, rawToken := seedSession(t, ctx, repo, userID, 30*24*time.Hour, 7*24*time.Hour)

	// First legitimate rotation.
	newRaw := "new-raw-" + uuid.NewString()
	_, _, err := repo.RotateRefreshToken(ctx,
		hashOpaqueToken(rawToken),
		sess.DeviceFingerprint,
		hashOpaqueToken(newRaw),
		time.Now().Add(7*24*time.Hour),
	)
	require.NoError(t, err)

	// Replay the original (now-used) token — reuse detected.
	reusedRaw := "reused-raw-" + uuid.NewString()
	returnedSess, _, err := repo.RotateRefreshToken(ctx,
		hashOpaqueToken(rawToken),
		sess.DeviceFingerprint,
		hashOpaqueToken(reusedRaw),
		time.Now().Add(7*24*time.Hour),
	)
	assert.ErrorIs(t, err, session.ErrRefreshTokenReused)
	require.NotNil(t, returnedSess)

	// The whole session must be dead.
	persisted, err := repo.GetSessionByID(ctx, sess.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.RevokedAt)
	assert.Equal(t, session.ReasonReuseDetected, *persisted.RevokedReason)
}

// ── RotateRefreshToken: device mismatch ─────────────────────────────────────

func TestSessionRepository_RotateRefreshToken_DeviceMismatch_RevokesSession(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	userID := seedIntegrationUser(t, db)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	sess, rawToken := seedSession(t, ctx, repo, userID, 30*24*time.Hour, 7*24*time.Hour)

	newRaw := "new-raw-" + uuid.NewString()
	returnedSess, _, err := repo.RotateRefreshToken(ctx,
		hashOpaqueToken(rawToken),
		"a-completely-different-device",
		hashOpaqueToken(newRaw),
		time.Now().Add(7*24*time.Hour),
	)
	assert.ErrorIs(t, err, session.ErrDeviceMismatch)
	require.NotNil(t, returnedSess)

	persisted, err := repo.GetSessionByID(ctx, sess.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.RevokedAt)
	assert.Equal(t, session.ReasonDeviceMismatch, *persisted.RevokedReason)
}

// ── RotateRefreshToken: expired refresh token ────────────────────────────────

func TestSessionRepository_RotateRefreshToken_ExpiredRefreshToken_RevokesSession(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	userID := seedIntegrationUser(t, db)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	// Refresh token expires immediately; absolute session lifetime is fine.
	sess, rawToken := seedSession(t, ctx, repo, userID, 30*24*time.Hour, -1*time.Second)

	newRaw := "new-raw-" + uuid.NewString()
	_, _, err := repo.RotateRefreshToken(ctx,
		hashOpaqueToken(rawToken),
		sess.DeviceFingerprint,
		hashOpaqueToken(newRaw),
		time.Now().Add(7*24*time.Hour),
	)
	assert.ErrorIs(t, err, session.ErrRefreshTokenExpired)

	persisted, err := repo.GetSessionByID(ctx, sess.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.RevokedAt)
	assert.Equal(t, session.ReasonRefreshExpired, *persisted.RevokedReason)
}

// ── RotateRefreshToken: unknown token ────────────────────────────────────────

func TestSessionRepository_RotateRefreshToken_UnknownToken_ReturnsInvalid(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	_ = seedIntegrationUser(t, db)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	_, _, err := repo.RotateRefreshToken(ctx,
		hashOpaqueToken("completely-unknown-token"),
		"any-device",
		hashOpaqueToken("new-raw"),
		time.Now().Add(7*24*time.Hour),
	)
	assert.ErrorIs(t, err, session.ErrRefreshTokenInvalid)
}

// ── RevokeSession ─────────────────────────────────────────────────────────────

func TestSessionRepository_RevokeSession_MarksRevokedAndBlocksRotation(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	userID := seedIntegrationUser(t, db)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	sess, rawToken := seedSession(t, ctx, repo, userID, 30*24*time.Hour, 7*24*time.Hour)

	require.NoError(t, repo.RevokeSession(ctx, sess.ID, session.ReasonLogout))

	// GetSessionByID still works but shows revoked_at set.
	persisted, err := repo.GetSessionByID(ctx, sess.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted.RevokedAt)
	assert.Equal(t, session.ReasonLogout, *persisted.RevokedReason)

	// Revoking again must not panic and must return ErrSessionNotFound (idempotency
	// is enforced via the WHERE revoked_at IS NULL guard).
	err = repo.RevokeSession(ctx, sess.ID, session.ReasonLogout)
	assert.ErrorIs(t, err, session.ErrSessionNotFound)

	// Attempting to rotate on a revoked session must fail.
	newRaw := "new-raw-" + uuid.NewString()
	_, _, err = repo.RotateRefreshToken(ctx,
		hashOpaqueToken(rawToken),
		sess.DeviceFingerprint,
		hashOpaqueToken(newRaw),
		time.Now().Add(7*24*time.Hour),
	)
	assert.ErrorIs(t, err, session.ErrSessionRevoked)
}

// ── RevokeAllByUser ───────────────────────────────────────────────────────────

func TestSessionRepository_RevokeAllByUser_RevokesEveryActiveSession(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	userID := seedIntegrationUser(t, db)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	// Create three sessions for the same user.
	s1, _ := seedSession(t, ctx, repo, userID, 30*24*time.Hour, 7*24*time.Hour)
	s2, _ := seedSession(t, ctx, repo, userID, 30*24*time.Hour, 7*24*time.Hour)
	s3, _ := seedSession(t, ctx, repo, userID, 30*24*time.Hour, 7*24*time.Hour)

	revoked, err := repo.RevokeAllByUser(ctx, userID, session.ReasonLogoutAll)
	require.NoError(t, err)
	assert.Len(t, revoked, 3)

	for _, id := range []uuid.UUID{s1.ID, s2.ID, s3.ID} {
		persisted, err := repo.GetSessionByID(ctx, id)
		require.NoError(t, err)
		require.NotNil(t, persisted.RevokedAt, "session %s must be revoked", id)
		assert.Equal(t, session.ReasonLogoutAll, *persisted.RevokedReason)
	}
}

func TestSessionRepository_RevokeAllByUser_DoesNotTouchOtherUsers(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	userA := seedIntegrationUser(t, db)
	userB := seedIntegrationUser(t, db)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	sessA, _ := seedSession(t, ctx, repo, userA, 30*24*time.Hour, 7*24*time.Hour)
	sessB, _ := seedSession(t, ctx, repo, userB, 30*24*time.Hour, 7*24*time.Hour)

	_, err := repo.RevokeAllByUser(ctx, userA, session.ReasonLogoutAll)
	require.NoError(t, err)

	// userB's session must be untouched.
	persisted, err := repo.GetSessionByID(ctx, sessB.ID)
	require.NoError(t, err)
	assert.Nil(t, persisted.RevokedAt, "userB's session must not be revoked by userA's logout-all")

	// userA's session must be revoked.
	persistedA, err := repo.GetSessionByID(ctx, sessA.ID)
	require.NoError(t, err)
	assert.NotNil(t, persistedA.RevokedAt)
}

// ── ListActiveByUser ──────────────────────────────────────────────────────────

func TestSessionRepository_ListActiveByUser_ExcludesRevokedAndExpired(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	userID := seedIntegrationUser(t, db)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	// active session
	_, _ = seedSession(t, ctx, repo, userID, 30*24*time.Hour, 7*24*time.Hour)

	// absolutely-expired session
	_, _ = seedSession(t, ctx, repo, userID, -1*time.Second, 7*24*time.Hour)

	// revoked session
	sessRevoked, _ := seedSession(t, ctx, repo, userID, 30*24*time.Hour, 7*24*time.Hour)
	require.NoError(t, repo.RevokeSession(ctx, sessRevoked.ID, session.ReasonLogout))

	active, err := repo.ListActiveByUser(ctx, userID)
	require.NoError(t, err)
	assert.Len(t, active, 1, "only the single active, non-expired session must be returned")
	assert.Nil(t, active[0].RevokedAt)
	assert.True(t, time.Now().Before(active[0].AbsoluteExpiresAt))
}

// ── GetSessionByID ────────────────────────────────────────────────────────────

func TestSessionRepository_GetSessionByID_NotFound(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	repo := NewSessionRepository(db)
	ctx := context.Background()

	_, err := repo.GetSessionByID(ctx, uuid.New())
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
}
