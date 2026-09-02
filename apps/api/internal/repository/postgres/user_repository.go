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
			id, wallet_address, display_name
		) VALUES ($1, $2, $3)
		RETURNING created_at, updated_at
	`

	if err := r.db.QueryRowContext(
		ctx,
		query,
		model.ID,
		model.WalletAddress,
		model.DisplayName,
	).Scan(&model.CreatedAt, &model.UpdatedAt); err != nil {
		return mapUserError(err)
	}

	return nil
}

func (r *UserRepository) GetByID(ctx context.Context, id uuid.UUID) (*user.User, error) {
	query := `
		SELECT id, wallet_address, display_name, tier, risk_profile, savings_goal, onboarding_completed, last_login_at, timezone, created_at, updated_at
		FROM users
		WHERE id = $1
	`
	return scanUser(r.db.QueryRowContext(ctx, query, id))
}

func (r *UserRepository) GetByWalletAddress(ctx context.Context, addr string) (*user.User, error) {
	query := `
		SELECT id, wallet_address, display_name, tier, risk_profile, savings_goal, onboarding_completed, last_login_at, timezone, created_at, updated_at
		FROM users
		WHERE wallet_address = $1
	`
	return scanUser(r.db.QueryRowContext(ctx, query, addr))
}

func (r *UserRepository) UpdateProfile(ctx context.Context, id uuid.UUID, patch user.ProfilePatch) (*user.User, error) {
	u, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if patch.RiskProfile != nil {
		u.RiskProfile = patch.RiskProfile
	}
	if patch.SavingsGoal != nil {
		u.SavingsGoal = patch.SavingsGoal
	}
	if patch.OnboardingCompleted != nil {
		u.OnboardingCompleted = *patch.OnboardingCompleted
	}
	if patch.Timezone != nil {
		u.Timezone = *patch.Timezone
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE users
		SET risk_profile = $1, savings_goal = $2, onboarding_completed = $3, timezone = $4, updated_at = NOW()
		WHERE id = $5
	`, nullableRiskProfile(u.RiskProfile), nullableStringPtr(u.SavingsGoal), u.OnboardingCompleted, u.Timezone, id)
	if err != nil {
		return nil, err
	}
	return r.GetByID(ctx, id)
}

func nullableRiskProfile(r *user.RiskProfile) interface{} {
	if r == nil {
		return nil
	}
	return string(*r)
}

func nullableStringPtr(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}

type userScanner interface {
	Scan(dest ...any) error
}

func scanUser(row userScanner) (*user.User, error) {
	var (
		id                  string
		walletAddress       string
		displayName         string
		tier                string
		riskProfile         sql.NullString
		savingsGoal         sql.NullString
		onboardingCompleted bool
		lastLoginAt         sql.NullTime
		timezone            string
		createdAt           time.Time
		updatedAt           time.Time
	)

	if err := row.Scan(
		&id,
		&walletAddress,
		&displayName,
		&tier,
		&riskProfile,
		&savingsGoal,
		&onboardingCompleted,
		&lastLoginAt,
		&timezone,
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

	var lastLoginAtPtr *time.Time
	if lastLoginAt.Valid {
		t := lastLoginAt.Time
		lastLoginAtPtr = &t
	}

	var riskPtr *user.RiskProfile
	if riskProfile.Valid && riskProfile.String != "" {
		rp := user.RiskProfile(riskProfile.String)
		riskPtr = &rp
	}
	var savingsPtr *string
	if savingsGoal.Valid {
		savingsPtr = &savingsGoal.String
	}

	return &user.User{
		ID:                  parsedID,
		WalletAddress:       walletAddress,
		DisplayName:         displayName,
		Tier:                tier,
		RiskProfile:         riskPtr,
		SavingsGoal:         savingsPtr,
		OnboardingCompleted: onboardingCompleted,
		LastLoginAt:         lastLoginAtPtr,
		Timezone:            timezone,
		CreatedAt:           createdAt,
		UpdatedAt:           updatedAt,
	}, nil
}

func (r *UserRepository) GetRoles(ctx context.Context, id uuid.UUID) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT role FROM user_roles WHERE user_id = $1 ORDER BY role`,
		id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func nullOptionalBytes(b []byte) any {
	if b == nil {
		return nil
	}
	return b
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
