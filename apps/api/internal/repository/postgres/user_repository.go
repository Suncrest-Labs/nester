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
		model.ID,
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
		SELECT id, wallet_address, display_name, kyc_status, tier, kyc_submitted_at, kyc_reviewed_at, kyc_rejection_reason, risk_profile, savings_goal, onboarding_completed, last_login_at, created_at, updated_at
		FROM users
		WHERE id = $1
	`
	return scanUser(r.db.QueryRowContext(ctx, query, id))
}

func (r *UserRepository) GetByWalletAddress(ctx context.Context, addr string) (*user.User, error) {
	query := `
		SELECT id, wallet_address, display_name, kyc_status, tier, kyc_submitted_at, kyc_reviewed_at, kyc_rejection_reason, risk_profile, savings_goal, onboarding_completed, last_login_at, created_at, updated_at
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
	_, err = r.db.ExecContext(ctx, `
		UPDATE users
		SET risk_profile = $1, savings_goal = $2, onboarding_completed = $3, updated_at = NOW()
		WHERE id = $4
	`, nullableRiskProfile(u.RiskProfile), nullableStringPtr(u.SavingsGoal), u.OnboardingCompleted, id)
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
		id                 string
		walletAddress      string
		displayName        string
		kycStatus          string
		tier               string
		kycSubmittedAt     sql.NullTime
		kycReviewedAt      sql.NullTime
		kycRejectionReason sql.NullString
    riskProfile         sql.NullString
		savingsGoal         sql.NullString
		onboardingCompleted bool
		lastLoginAt        sql.NullTime
		createdAt          time.Time
		updatedAt          time.Time
	)

	if err := row.Scan(
		&id,
		&walletAddress,
		&displayName,
		&kycStatus,
		&tier,
		&kycSubmittedAt,
		&kycReviewedAt,
		&kycRejectionReason,
		&riskProfile,
		&savingsGoal,
		&onboardingCompleted,
		&lastLoginAt,
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

	var lastLoginAtPtr, kycSubAtPtr, kycRevAtPtr *time.Time
	if lastLoginAt.Valid {
		t := lastLoginAt.Time
		lastLoginAtPtr = &t
	}
	if kycSubmittedAt.Valid {
		t := kycSubmittedAt.Time
		kycSubAtPtr = &t
	}
	if kycReviewedAt.Valid {
		t := kycReviewedAt.Time
		kycRevAtPtr = &t
	}
	var kycRejReasonPtr *string
	if kycRejectionReason.Valid {
		kycRejReasonPtr = &kycRejectionReason.String
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
		ID:                 parsedID,
		WalletAddress:      walletAddress,
		DisplayName:        displayName,
		KYCStatus:          user.KYCStatus(kycStatus),
		Tier:               tier,
		KYCSubmittedAt:     kycSubAtPtr,
		KYCReviewedAt:      kycRevAtPtr,
		KYCRejectionReason: kycRejReasonPtr,
    RiskProfile:         riskPtr,
		SavingsGoal:         savingsPtr,
		OnboardingCompleted: onboardingCompleted,
		LastLoginAt:        lastLoginAtPtr,
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
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

func (r *UserRepository) SaveKYCDocument(ctx context.Context, doc *user.KYCDocument) error {
	query := `
		INSERT INTO kyc_documents (
			id, user_id, id_type, id_number, front_object_key, back_object_key
		) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING submitted_at
	`
	err := r.db.QueryRowContext(ctx, query,
		doc.ID.String(), doc.UserID.String(), doc.IDType, doc.IDNumber, doc.FrontObjectKey, doc.BackObjectKey,
	).Scan(&doc.SubmittedAt)
	return err
}

func (r *UserRepository) GetKYCDocument(ctx context.Context, userID uuid.UUID) (*user.KYCDocument, error) {
	query := `
		SELECT id, user_id, id_type, id_number, front_object_key, back_object_key, submitted_at
		FROM kyc_documents
		WHERE user_id = $1
		ORDER BY submitted_at DESC
		LIMIT 1
	`
	var doc user.KYCDocument
	var id, uid string
	var backKey sql.NullString
	if err := r.db.QueryRowContext(ctx, query, userID.String()).Scan(
		&id, &uid, &doc.IDType, &doc.IDNumber, &doc.FrontObjectKey, &backKey, &doc.SubmittedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("no kyc document found")
		}
		return nil, err
	}
	doc.ID = uuid.MustParse(id)
	doc.UserID = uuid.MustParse(uid)
	if backKey.Valid {
		doc.BackObjectKey = &backKey.String
	}
	return &doc, nil
}

func (r *UserRepository) UpdateKYCStatus(ctx context.Context, userID uuid.UUID, status user.KYCStatus, reason *string, ts *time.Time) error {
	var query string
	var err error
	if status == user.KYCStatusPending {
		query = `UPDATE users SET kyc_status = $1, kyc_submitted_at = $2, kyc_rejection_reason = NULL, kyc_reviewed_at = NULL, updated_at = NOW() WHERE id = $3`
		_, err = r.db.ExecContext(ctx, query, string(status), ts, userID.String())
	} else {
		query = `UPDATE users SET kyc_status = $1, kyc_reviewed_at = $2, kyc_rejection_reason = $3, updated_at = NOW() WHERE id = $4`
		_, err = r.db.ExecContext(ctx, query, string(status), ts, reason, userID.String())
	}
	return err
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
