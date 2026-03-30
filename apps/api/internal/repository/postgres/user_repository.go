package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, model *user.User) error {
	query := `
		INSERT INTO users (
			id, wallet_address, display_name, kyc_status
		) VALUES ($1, $2, $3, $4)
		RETURNING created_at, updated_at
	`

	if err := r.db.QueryRowContext(
		ctx,
		query,
		model.ID.String(),
		model.WalletAddress,
		model.DisplayName,
		string(model.KYCStatus),
	).Scan(&model.CreatedAt, &model.UpdatedAt); err != nil {
		return mapUserError(err)
	}

	return nil
}

func (r *UserRepository) GetByID(ctx context.Context, id uuid.UUID) (*user.User, error) {
	query := `
		SELECT id, wallet_address, display_name, kyc_status, created_at, updated_at
		FROM users
		WHERE id = $1
	`
	return scanUser(r.db.QueryRowContext(ctx, query, id.String()))
}

func (r *UserRepository) GetByWalletAddress(ctx context.Context, addr string) (*user.User, error) {
	query := `
		SELECT id, wallet_address, display_name, kyc_status, created_at, updated_at
		FROM users
		WHERE wallet_address = $1
	`
	return scanUser(r.db.QueryRowContext(ctx, query, addr))
}

type userScanner interface {
	Scan(dest ...any) error
}

func scanUser(row userScanner) (*user.User, error) {
	var (
		id            string
		walletAddress string
		displayName   string
		kycStatus     string
		createdAt     time.Time
		updatedAt     time.Time
	)

	if err := row.Scan(
		&id,
		&walletAddress,
		&displayName,
		&kycStatus,
		&createdAt,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, user.ErrUserNotFound
		}
		return nil, err
	}

	parsedID, err := uuid.Parse(id)
	if err != nil {
		return nil, err // should not happen if UUID is well-formed in DB
	}

	return &user.User{
		ID:            parsedID,
		WalletAddress: walletAddress,
		DisplayName:   displayName,
		KYCStatus:     user.KYCStatus(kycStatus),
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}, nil
}

func mapUserError(err error) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// Unique violation for wallet_address
		if pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "users_wallet_address_key") {
			return user.ErrDuplicateWallet
		}
	}

	return err
}
