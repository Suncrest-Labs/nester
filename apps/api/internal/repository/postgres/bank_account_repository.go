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

	"github.com/suncrestlabs/nester/apps/api/internal/domain/bankaccount"
)

type BankAccountRepository struct {
	db *sql.DB
}

func NewBankAccountRepository(db *sql.DB) *BankAccountRepository {
	return &BankAccountRepository{db: db}
}

func (r *BankAccountRepository) Create(
	ctx context.Context,
	account bankaccount.BankAccount,
	encryptedNumber []byte,
	fingerprint string,
) (bankaccount.BankAccount, error) {
	last4 := account.AccountNumber
	if len(last4) > 4 {
		last4 = last4[len(last4)-4:]
	}

	query := `
		INSERT INTO bank_accounts (
			id, user_id, bank_name, bank_code,
			account_number_encrypted, account_number_fingerprint, account_last4,
			account_name, currency, country, is_default, verified_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING created_at
	`

	err := r.db.QueryRowContext(
		ctx,
		query,
		account.ID.String(),
		account.UserID.String(),
		account.BankName,
		nullOptionalString(account.BankCode),
		encryptedNumber,
		fingerprint,
		last4,
		account.AccountName,
		strings.ToUpper(account.Currency),
		strings.ToUpper(account.Country),
		account.IsDefault,
		account.VerifiedAt,
	).Scan(&account.CreatedAt)
	if err != nil {
		return bankaccount.BankAccount{}, mapBankAccountError(err)
	}
	return account, nil
}

func (r *BankAccountRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]bankaccount.BankAccount, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, bank_name, COALESCE(bank_code, ''), account_last4,
		       account_name, currency, country, is_default, verified_at, created_at
		FROM bank_accounts
		WHERE user_id = $1
		ORDER BY is_default DESC, created_at DESC
	`, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []bankaccount.BankAccount
	for rows.Next() {
		var (
			id, uid, bankName, bankCode, last4 string
			accountName, currency, country     string
			isDefault                          bool
			verifiedAt                         sql.NullTime
			createdAt                          time.Time
		)
		if err := rows.Scan(
			&id, &uid, &bankName, &bankCode, &last4,
			&accountName, &currency, &country, &isDefault, &verifiedAt, &createdAt,
		); err != nil {
			return nil, err
		}
		parsedID, _ := uuid.Parse(id)
		parsedUser, _ := uuid.Parse(uid)
		var verified *time.Time
		if verifiedAt.Valid {
			verified = &verifiedAt.Time
		}
		out = append(out, bankaccount.BankAccount{
			ID:            parsedID,
			UserID:        parsedUser,
			BankName:      bankName,
			BankCode:      bankCode,
			AccountNumber: last4,
			AccountName:   accountName,
			Currency:      currency,
			Country:       country,
			IsDefault:     isDefault,
			VerifiedAt:    verified,
			CreatedAt:     createdAt,
		})
	}
	return out, rows.Err()
}

func (r *BankAccountRepository) GetByID(ctx context.Context, id uuid.UUID) (bankaccount.BankAccount, []byte, error) {
	var (
		uid, bankName, bankCode string
		encrypted               []byte
		accountName, currency, country string
		isDefault               bool
		verifiedAt              sql.NullTime
		createdAt               time.Time
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT user_id, bank_name, COALESCE(bank_code, ''), account_number_encrypted,
		       account_name, currency, country, is_default, verified_at, created_at
		FROM bank_accounts WHERE id = $1
	`, id.String()).Scan(
		&uid, &bankName, &bankCode, &encrypted,
		&accountName, &currency, &country, &isDefault, &verifiedAt, &createdAt,
	)
	if err != nil {
		return bankaccount.BankAccount{}, nil, mapBankAccountError(err)
	}
	parsedUser, _ := uuid.Parse(uid)
	var verified *time.Time
	if verifiedAt.Valid {
		verified = &verifiedAt.Time
	}
	return bankaccount.BankAccount{
		ID:            id,
		UserID:        parsedUser,
		BankName:      bankName,
		BankCode:      bankCode,
		AccountName:   accountName,
		Currency:      currency,
		Country:       country,
		IsDefault:     isDefault,
		VerifiedAt:    verified,
		CreatedAt:     createdAt,
	}, encrypted, nil
}

func (r *BankAccountRepository) Delete(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM bank_accounts WHERE id = $1`, id.String())
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return bankaccount.ErrNotFound
	}
	return nil
}

func (r *BankAccountRepository) ClearDefaultForCurrency(ctx context.Context, userID uuid.UUID, currency string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE bank_accounts SET is_default = false
		WHERE user_id = $1 AND currency = $2 AND is_default = true
	`, userID.String(), strings.ToUpper(currency))
	return err
}

func (r *BankAccountRepository) SetDefault(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE bank_accounts SET is_default = true
		WHERE id = $1 AND user_id = $2
	`, id.String(), userID.String())
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return bankaccount.ErrNotFound
	}
	return nil
}

func mapBankAccountError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return bankaccount.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return bankaccount.ErrDuplicateAccount
	}
	return fmt.Errorf("bank account repository: %w", err)
}

func nullOptionalString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
