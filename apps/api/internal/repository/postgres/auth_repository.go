package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

type AuthRepository struct {
	db *sql.DB
}

func NewAuthRepository(db *sql.DB) *AuthRepository {
	return &AuthRepository{db: db}
}

func (r *AuthRepository) StoreNonce(ctx context.Context, nonce, walletAddress string, expiresAt time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO auth_nonces (nonce, wallet_address, expires_at) VALUES ($1, $2, $3)
		 ON CONFLICT (nonce) DO UPDATE SET wallet_address = EXCLUDED.wallet_address, expires_at = EXCLUDED.expires_at`,
		nonce,
		walletAddress,
		expiresAt.UTC(),
	)
	return err
}

func (r *AuthRepository) ConsumeNonce(ctx context.Context, nonce, walletAddress string, now time.Time) error {
	var consumed string
	err := r.db.QueryRowContext(
		ctx,
		`DELETE FROM auth_nonces
		 WHERE nonce = $1 AND wallet_address = $2 AND expires_at > $3
		 RETURNING nonce`,
		nonce,
		walletAddress,
		now.UTC(),
	).Scan(&consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrInvalidNonce
	}
	return err
}

func (r *AuthRepository) CreateOrGetUserByWallet(ctx context.Context, walletAddress string) (uuid.UUID, error) {
	var existingID string
	err := r.db.QueryRowContext(ctx, `SELECT id FROM users WHERE wallet_address = $1`, walletAddress).Scan(&existingID)
	if err == nil {
		return uuid.Parse(existingID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, err
	}

	userID := uuid.New()
	email := strings.ToLower(walletAddress) + "@wallet.local"
	name := "wallet-" + strings.ToLower(walletAddress[:8])

	if err := r.db.QueryRowContext(
		ctx,
		`INSERT INTO users (id, email, name, wallet_address)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id`,
		userID.String(),
		email,
		name,
		walletAddress,
	).Scan(&existingID); err != nil {
		return uuid.Nil, fmt.Errorf("create wallet user: %w", err)
	}

	return uuid.Parse(existingID)
}

func (r *AuthRepository) CreateSession(ctx context.Context, session service.AuthSession) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO auth_sessions (id, user_id, refresh_token_hash, wallet_address, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		session.ID.String(),
		session.UserID.String(),
		session.RefreshTokenHash,
		session.WalletAddress,
		session.ExpiresAt.UTC(),
		session.CreatedAt.UTC(),
	)
	return err
}

func (r *AuthRepository) GetSessionByID(ctx context.Context, sessionID uuid.UUID) (service.AuthSession, error) {
	var (
		id               string
		userID           string
		refreshTokenHash string
		walletAddress    string
		expiresAt        time.Time
		createdAt        time.Time
	)

	err := r.db.QueryRowContext(
		ctx,
		`SELECT id, user_id, refresh_token_hash, wallet_address, expires_at, created_at
		 FROM auth_sessions
		 WHERE id = $1`,
		sessionID.String(),
	).Scan(&id, &userID, &refreshTokenHash, &walletAddress, &expiresAt, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return service.AuthSession{}, service.ErrSessionNotFound
	}
	if err != nil {
		return service.AuthSession{}, err
	}

	parsedID, err := uuid.Parse(id)
	if err != nil {
		return service.AuthSession{}, err
	}

	parsedUserID, err := uuid.Parse(userID)
	if err != nil {
		return service.AuthSession{}, err
	}

	return service.AuthSession{
		ID:               parsedID,
		UserID:           parsedUserID,
		RefreshTokenHash: refreshTokenHash,
		WalletAddress:    walletAddress,
		ExpiresAt:        expiresAt,
		CreatedAt:        createdAt,
	}, nil
}

func (r *AuthRepository) DeleteSession(ctx context.Context, sessionID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE id = $1`, sessionID.String())
	return err
}

func (r *AuthRepository) ReplaceSession(ctx context.Context, previousSessionID uuid.UUID, session service.AuthSession) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_sessions WHERE id = $1`, previousSessionID.String()); err != nil {
		return err
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO auth_sessions (id, user_id, refresh_token_hash, wallet_address, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		session.ID.String(),
		session.UserID.String(),
		session.RefreshTokenHash,
		session.WalletAddress,
		session.ExpiresAt.UTC(),
		session.CreatedAt.UTC(),
	); err != nil {
		return err
	}

	return tx.Commit()
}
