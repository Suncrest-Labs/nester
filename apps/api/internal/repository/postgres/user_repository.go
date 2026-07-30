package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
	"github.com/suncrestlabs/nester/apps/api/internal/rotation"
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
		SELECT id, wallet_address, display_name, kyc_status, tier, kyc_submitted_at, kyc_reviewed_at, kyc_rejection_reason, risk_profile, savings_goal, onboarding_completed, last_login_at, timezone, created_at, updated_at
		FROM users
		WHERE id = $1
	`
	return scanUser(r.db.QueryRowContext(ctx, query, id))
}

func (r *UserRepository) GetByWalletAddress(ctx context.Context, addr string) (*user.User, error) {
	query := `
		SELECT id, wallet_address, display_name, kyc_status, tier, kyc_submitted_at, kyc_reviewed_at, kyc_rejection_reason, risk_profile, savings_goal, onboarding_completed, last_login_at, timezone, created_at, updated_at
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
		timezone           string
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
		Timezone:           timezone,
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

func (r *UserRepository) SaveKYCDocument(ctx context.Context, doc *user.KYCDocument, encrypted *user.EncryptedKYCDoc) error {
	query := `
		INSERT INTO kyc_documents (
			id, user_id, id_type, id_number, front_object_key, back_object_key,
			id_number_encrypted, id_number_fingerprint, front_object_key_encrypted,
			back_object_key_encrypted, key_version
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING submitted_at
	`
	err := r.db.QueryRowContext(ctx, query,
		doc.ID.String(), doc.UserID.String(), doc.IDType, doc.IDNumber, doc.FrontObjectKey, doc.BackObjectKey,
		encrypted.IDNumberEncrypted, encrypted.IDNumberFingerprint, encrypted.FrontKeyEncrypted,
		nullOptionalBytes(encrypted.BackKeyEncrypted), encrypted.KeyVersion,
	).Scan(&doc.SubmittedAt)
	return err
}

func (r *UserRepository) GetKYCDocument(ctx context.Context, userID uuid.UUID) (*user.KYCDocument, *user.EncryptedKYCDoc, error) {
	query := `
		SELECT id, user_id, id_type, id_number, front_object_key, back_object_key,
		       id_number_encrypted, id_number_fingerprint, front_object_key_encrypted,
		       back_object_key_encrypted, key_version, submitted_at
		FROM kyc_documents
		WHERE user_id = $1
		ORDER BY submitted_at DESC
		LIMIT 1
	`
	var doc user.KYCDocument
	var encrypted user.EncryptedKYCDoc
	var id, uid string
	var backKey sql.NullString
	var idNumFingerprint, kv sql.NullString
	if err := r.db.QueryRowContext(ctx, query, userID.String()).Scan(
		&id, &uid, &doc.IDType, &doc.IDNumber, &doc.FrontObjectKey, &backKey,
		&encrypted.IDNumberEncrypted, &idNumFingerprint, &encrypted.FrontKeyEncrypted,
		&encrypted.BackKeyEncrypted, &kv, &doc.SubmittedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, errors.New("no kyc document found")
		}
		return nil, nil, err
	}
	doc.ID = uuid.MustParse(id)
	doc.UserID = uuid.MustParse(uid)
	if backKey.Valid {
		doc.BackObjectKey = &backKey.String
	}
	if idNumFingerprint.Valid {
		encrypted.IDNumberFingerprint = idNumFingerprint.String
	}
	if kv.Valid {
		encrypted.KeyVersion = kv.String
	}
	return &doc, &encrypted, nil
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

// KYCBackfillRow holds the plaintext fields of a KYC document row that still
// needs its encrypted columns populated.
type KYCBackfillRow struct {
	ID             uuid.UUID
	IDNumber       string
	FrontObjectKey string
	BackObjectKey  *string
}

// ScanKYCDocumentsForBackfill returns up to limit KYC document rows whose
// id_number_encrypted column is NULL (not yet backfilled), ordered by id for
// stable paging. Each row is committed independently so the backfill is
// resumable.
func (r *UserRepository) ScanKYCDocumentsForBackfill(ctx context.Context, limit int) ([]KYCBackfillRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, id_number, front_object_key, back_object_key
		FROM kyc_documents
		WHERE id_number_encrypted IS NULL
		ORDER BY id
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("kyc repository: scan for backfill: %w", err)
	}
	defer rows.Close()

	var out []KYCBackfillRow
	for rows.Next() {
		var (
			id                       string
			idNum, frontKey          string
			backKey                  sql.NullString
		)
		if err := rows.Scan(&id, &idNum, &frontKey, &backKey); err != nil {
			return nil, err
		}
		parsedID, err := uuid.Parse(id)
		if err != nil {
			return nil, fmt.Errorf("kyc repository: parse id: %w", err)
		}
		row := KYCBackfillRow{
			ID:             parsedID,
			IDNumber:       idNum,
			FrontObjectKey: frontKey,
		}
		if backKey.Valid {
			row.BackObjectKey = &backKey.String
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// CountKYCDocumentsForBackfill reports how many KYC document rows still need
// their encrypted columns populated.
func (r *UserRepository) CountKYCDocumentsForBackfill(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM kyc_documents WHERE id_number_encrypted IS NULL`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("kyc repository: count for backfill: %w", err)
	}
	return n, nil
}

// CountPendingKYCEncryption reports how many KYC document rows are not yet on
// the active key version. Implements the rotation store interface for KYC docs.
func (r *UserRepository) CountPendingKYCEncryption(ctx context.Context, activeVersion string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM kyc_documents WHERE key_version <> $1`, activeVersion).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("kyc repository: count pending rotation: %w", err)
	}
	return n, nil
}

// ScanPendingKYCEncryption returns up to limit KYC document rows whose key
// version is not activeVersion, ordered by id for stable paging. Implements
// the rotation store interface for KYC docs.
func (r *UserRepository) ScanPendingKYCEncryption(ctx context.Context, activeVersion string, limit int) ([]rotation.EncryptedRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, id_number_encrypted, front_object_key_encrypted, back_object_key_encrypted, key_version
		FROM kyc_documents
		WHERE key_version <> $1
		ORDER BY id
		LIMIT $2
	`, activeVersion, limit)
	if err != nil {
		return nil, fmt.Errorf("kyc repository: scan pending rotation: %w", err)
	}
	defer rows.Close()

	var out []rotation.EncryptedRow
	for rows.Next() {
		var (
			id                     string
			idNumEnc, frontKeyEnc  []byte
			backKeyEnc             []byte
			keyVersion             string
		)
		if err := rows.Scan(&id, &idNumEnc, &frontKeyEnc, &backKeyEnc, &keyVersion); err != nil {
			return nil, err
		}
		parsedID, err := uuid.Parse(id)
		if err != nil {
			return nil, fmt.Errorf("kyc repository: parse id: %w", err)
		}
		// Pack the three ciphertexts into a single []byte for the rotation
		// store interface; unpacking happens during re-encryption.
		packed := packKYCCiphertexts(idNumEnc, frontKeyEnc, backKeyEnc)
		out = append(out, rotation.EncryptedRow{ID: parsedID, Ciphertext: packed, KeyVersion: keyVersion})
	}
	return out, rows.Err()
}

// UpdateKYCCipher atomically replaces a KYC document row's ciphertext and key
// version. The ciphertext parameter is expected to be the packed output of
// packKYCCiphertexts.
func (r *UserRepository) UpdateKYCCipher(ctx context.Context, id uuid.UUID, encryptedIDNumber, encryptedFrontKey, encryptedBackKey []byte, keyVersion string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE kyc_documents
		SET id_number_encrypted = $2, front_object_key_encrypted = $3,
		    back_object_key_encrypted = $4, key_version = $5
		WHERE id = $1
	`, id.String(), encryptedIDNumber, encryptedFrontKey, nullOptionalBytes(encryptedBackKey), keyVersion)
	if err != nil {
		return fmt.Errorf("kyc repository: update cipher: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("kyc document not found")
	}
	return nil
}

// UpdateKYCFingerprint sets the id_number_fingerprint column for a KYC
// document row. Called during backfill to populate the blind index.
func (r *UserRepository) UpdateKYCFingerprint(ctx context.Context, id uuid.UUID, fingerprint string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE kyc_documents SET id_number_fingerprint = $2 WHERE id = $1`,
		id.String(), fingerprint)
	return err
}

// packKYCCiphertexts packs three ciphertext byte slices into a single
// length-prefixed blob for transport through the rotation.Store interface.
func packKYCCiphertexts(idNum, frontKey, backKey []byte) []byte {
	writeLen := func(buf []byte, v int) { buf[0] = byte(v >> 8); buf[1] = byte(v) }
	total := 6 + len(idNum) + len(frontKey) + len(backKey)
	buf := make([]byte, total)
	writeLen(buf[0:2], len(idNum))
	writeLen(buf[2:4], len(frontKey))
	writeLen(buf[4:6], len(backKey))
	copy(buf[6:], idNum)
	copy(buf[6+len(idNum):], frontKey)
	copy(buf[6+len(idNum)+len(frontKey):], backKey)
	return buf
}

// UnpackKYCCiphertexts reverses packKYCCiphertexts.
func UnpackKYCCiphertexts(packed []byte) (idNum, frontKey, backKey []byte) {
	if len(packed) < 6 {
		return nil, nil, nil
	}
	readLen := func(buf []byte) int { return int(buf[0])<<8 | int(buf[1]) }
	idNumLen := readLen(packed[0:2])
	frontKeyLen := readLen(packed[2:4])
	backKeyLen := readLen(packed[4:6])
	off := 6
	if idNumLen > 0 {
		idNum = packed[off : off+idNumLen]
		off += idNumLen
	}
	if frontKeyLen > 0 {
		frontKey = packed[off : off+frontKeyLen]
		off += frontKeyLen
	}
	if backKeyLen > 0 {
		backKey = packed[off : off+backKeyLen]
	}
	return
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
