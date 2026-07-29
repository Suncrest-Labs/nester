package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/session"
)

type SessionRepository struct {
	db *sql.DB
}

func NewSessionRepository(db *sql.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

func (r *SessionRepository) CreateSession(ctx context.Context, s *session.Session, refreshTokenHash string, refreshExpiresAt time.Time) (*session.RefreshToken, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	err = tx.QueryRowContext(ctx, `
		INSERT INTO sessions (user_id, wallet_address, device_fingerprint, user_agent, ip_address, absolute_expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, last_active_at
	`, s.UserID, s.WalletAddress, s.DeviceFingerprint, nullableStringPtrArg(s.UserAgent), nullableStringPtrArg(s.IPAddress), s.AbsoluteExpiresAt,
	).Scan(&s.ID, &s.CreatedAt, &s.LastActiveAt)
	if err != nil {
		return nil, err
	}

	rt := &session.RefreshToken{
		SessionID: s.ID,
		TokenHash: refreshTokenHash,
		ExpiresAt: refreshExpiresAt,
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO refresh_tokens (session_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id, issued_at
	`, rt.SessionID, rt.TokenHash, rt.ExpiresAt).Scan(&rt.ID, &rt.IssuedAt)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return rt, nil
}

// RotateRefreshToken implements rotation + reuse-detection + device-binding in
// a single transaction. See domain/session.Repository for the contract.
func (r *SessionRepository) RotateRefreshToken(ctx context.Context, rawTokenHash, presentedFingerprint, newTokenHash string, newExpiresAt time.Time) (*session.Session, *session.RefreshToken, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		tokID      uuid.UUID
		sessID     uuid.UUID
		tokUsedAt  sql.NullTime
		tokRevAt   sql.NullTime
		tokExpires time.Time
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id, session_id, used_at, revoked_at, expires_at
		FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE
	`, rawTokenHash).Scan(&tokID, &sessID, &tokUsedAt, &tokRevAt, &tokExpires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, session.ErrRefreshTokenInvalid
	}
	if err != nil {
		return nil, nil, err
	}

	sess, err := scanSessionForUpdate(ctx, tx, sessID)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()

	if sess.RevokedAt != nil {
		return sess, nil, session.ErrSessionRevoked
	}

	if now.After(sess.AbsoluteExpiresAt) {
		if err := revokeSessionTx(ctx, tx, sess.ID, session.ReasonAbsoluteExpiry, now); err != nil {
			return nil, nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, nil, err
		}
		return sess, nil, session.ErrSessionExpired
	}

	if presentedFingerprint != sess.DeviceFingerprint {
		if err := revokeSessionTx(ctx, tx, sess.ID, session.ReasonDeviceMismatch, now); err != nil {
			return nil, nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, nil, err
		}
		return sess, nil, session.ErrDeviceMismatch
	}

	if tokUsedAt.Valid || tokRevAt.Valid {
		if err := revokeSessionTx(ctx, tx, sess.ID, session.ReasonReuseDetected, now); err != nil {
			return nil, nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, nil, err
		}
		return sess, nil, session.ErrRefreshTokenReused
	}

	if now.After(tokExpires) {
		if _, err := tx.ExecContext(ctx, `
			UPDATE refresh_tokens SET revoked_at = $1, revoked_reason = $2 WHERE id = $3
		`, now, session.ReasonRefreshExpired, tokID); err != nil {
			return nil, nil, err
		}
		if err := revokeSessionTx(ctx, tx, sess.ID, session.ReasonRefreshExpired, now); err != nil {
			return nil, nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, nil, err
		}
		return sess, nil, session.ErrRefreshTokenExpired
	}

	if _, err := tx.ExecContext(ctx, `UPDATE refresh_tokens SET used_at = $1 WHERE id = $2`, now, tokID); err != nil {
		return nil, nil, err
	}

	newRT := &session.RefreshToken{
		SessionID: sess.ID,
		TokenHash: newTokenHash,
		ParentID:  &tokID,
		ExpiresAt: newExpiresAt,
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO refresh_tokens (session_id, token_hash, parent_id, expires_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id, issued_at
	`, newRT.SessionID, newRT.TokenHash, newRT.ParentID, newRT.ExpiresAt).Scan(&newRT.ID, &newRT.IssuedAt)
	if err != nil {
		return nil, nil, err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET last_active_at = $1 WHERE id = $2`, now, sess.ID); err != nil {
		return nil, nil, err
	}
	sess.LastActiveAt = now

	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return sess, newRT, nil
}

func (r *SessionRepository) GetSessionByID(ctx context.Context, id uuid.UUID) (*session.Session, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, wallet_address, device_fingerprint, user_agent, ip_address,
		       created_at, last_active_at, absolute_expires_at, revoked_at, revoked_reason
		FROM sessions WHERE id = $1
	`, id)
	s, err := scanSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, session.ErrSessionNotFound
		}
		return nil, err
	}
	return s, nil
}

func (r *SessionRepository) ListActiveByUser(ctx context.Context, userID uuid.UUID) ([]session.Session, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, wallet_address, device_fingerprint, user_agent, ip_address,
		       created_at, last_active_at, absolute_expires_at, revoked_at, revoked_reason
		FROM sessions
		WHERE user_id = $1 AND revoked_at IS NULL AND absolute_expires_at > NOW()
		ORDER BY last_active_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []session.Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func (r *SessionRepository) RevokeSession(ctx context.Context, id uuid.UUID, reason string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE sessions SET revoked_at = NOW(), revoked_reason = $2
		WHERE id = $1 AND revoked_at IS NULL
	`, id, reason)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return session.ErrSessionNotFound
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE refresh_tokens SET revoked_at = NOW(), revoked_reason = $2
		WHERE session_id = $1 AND revoked_at IS NULL
	`, id, reason); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *SessionRepository) RevokeAllByUser(ctx context.Context, userID uuid.UUID, reason string) ([]session.Session, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		UPDATE sessions SET revoked_at = NOW(), revoked_reason = $2
		WHERE user_id = $1 AND revoked_at IS NULL
		RETURNING id, user_id, wallet_address, device_fingerprint, user_agent, ip_address,
		          created_at, last_active_at, absolute_expires_at, revoked_at, revoked_reason
	`, userID, reason)
	if err != nil {
		return nil, err
	}

	var revoked []session.Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		revoked = append(revoked, *s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	if len(revoked) > 0 {
		ids := make([]uuid.UUID, len(revoked))
		for i, s := range revoked {
			ids[i] = s.ID
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE refresh_tokens SET revoked_at = NOW(), revoked_reason = $2
			WHERE session_id = ANY($1) AND revoked_at IS NULL
		`, pq.Array(ids), reason); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return revoked, nil
}

// ── helpers ─────────────────────────────────────────────────────────────────

func nullableStringPtrArg(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

type sessionScanner interface {
	Scan(dest ...any) error
}

func scanSession(row sessionScanner) (*session.Session, error) {
	var (
		s             session.Session
		userAgent     sql.NullString
		ipAddress     sql.NullString
		revokedAt     sql.NullTime
		revokedReason sql.NullString
	)
	if err := row.Scan(
		&s.ID, &s.UserID, &s.WalletAddress, &s.DeviceFingerprint, &userAgent, &ipAddress,
		&s.CreatedAt, &s.LastActiveAt, &s.AbsoluteExpiresAt, &revokedAt, &revokedReason,
	); err != nil {
		return nil, err
	}
	if userAgent.Valid {
		s.UserAgent = &userAgent.String
	}
	if ipAddress.Valid {
		s.IPAddress = &ipAddress.String
	}
	if revokedAt.Valid {
		s.RevokedAt = &revokedAt.Time
	}
	if revokedReason.Valid {
		s.RevokedReason = &revokedReason.String
	}
	return &s, nil
}

// scanSessionForUpdate locks and reads a session row within tx.
func scanSessionForUpdate(ctx context.Context, tx *sql.Tx, id uuid.UUID) (*session.Session, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, user_id, wallet_address, device_fingerprint, user_agent, ip_address,
		       created_at, last_active_at, absolute_expires_at, revoked_at, revoked_reason
		FROM sessions WHERE id = $1 FOR UPDATE
	`, id)
	return scanSession(row)
}

func revokeSessionTx(ctx context.Context, tx *sql.Tx, id uuid.UUID, reason string, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE sessions SET revoked_at = $2, revoked_reason = $3 WHERE id = $1
	`, id, now, reason); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE refresh_tokens SET revoked_at = $2, revoked_reason = $3
		WHERE session_id = $1 AND revoked_at IS NULL
	`, id, now, reason)
	return err
}
