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
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
)

type TransactionRepository struct {
	db *sql.DB
}

func NewTransactionRepository(db *sql.DB) *TransactionRepository {
	return &TransactionRepository{db: db}
}

func (r *TransactionRepository) Upsert(ctx context.Context, model transaction.Transaction) (transaction.Transaction, error) {
	query := `
		INSERT INTO transactions (
			id, vault_id, type, amount, currency, tx_hash, status, error_reason, confirmed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (tx_hash) DO UPDATE SET
			vault_id = EXCLUDED.vault_id,
			type = EXCLUDED.type,
			amount = EXCLUDED.amount,
			currency = EXCLUDED.currency,
			status = EXCLUDED.status,
			error_reason = EXCLUDED.error_reason,
			confirmed_at = EXCLUDED.confirmed_at,
			updated_at = NOW()
		RETURNING created_at, updated_at, confirmed_at
	`

	if err := r.db.QueryRowContext(
		ctx,
		query,
		model.ID.String(),
		model.VaultID.String(),
		string(model.Type),
		model.Amount.String(),
		model.Currency,
		model.TxHash,
		string(model.Status),
		nullString(model.ErrorReason),
		model.ConfirmedAt,
	).Scan(&model.CreatedAt, &model.UpdatedAt, &model.ConfirmedAt); err != nil {
		return transaction.Transaction{}, mapTransactionError(err)
	}

	return model, nil
}

func (r *TransactionRepository) GetByHash(ctx context.Context, hash string) (transaction.Transaction, error) {
	query := `
		SELECT id, vault_id, type, amount, currency, tx_hash, status, error_reason, created_at, updated_at, confirmed_at
		FROM transactions
		WHERE tx_hash = $1
	`

	row := r.db.QueryRowContext(ctx, query, hash)
	model, err := scanTransaction(row)
	if err != nil {
		return transaction.Transaction{}, mapTransactionError(err)
	}

	return model, nil
}

func (r *TransactionRepository) UpdateStatus(ctx context.Context, hash string, status transaction.TransactionStatus, confirmedAt *time.Time, errorReason string) (transaction.Transaction, error) {
	result, err := r.db.ExecContext(
		ctx,
		`UPDATE transactions
		 SET status = $2, confirmed_at = $3, error_reason = $4, updated_at = NOW()
		 WHERE tx_hash = $1`,
		hash,
		string(status),
		confirmedAt,
		nullString(errorReason),
	)
	if err != nil {
		return transaction.Transaction{}, mapTransactionError(err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return transaction.Transaction{}, err
	}
	if rows == 0 {
		return transaction.Transaction{}, transaction.ErrTransactionNotFound
	}

	return r.GetByHash(ctx, hash)
}

type transactionScanner interface {
	Scan(dest ...any) error
}

func scanTransaction(row transactionScanner) (transaction.Transaction, error) {
	var (
		id          string
		vaultID     string
		txType      string
		amount      string
		currency    string
		txHash      string
		status      string
		errorReason sql.NullString
		createdAt   time.Time
		updatedAt   time.Time
		confirmedAt sql.NullTime
	)

	if err := row.Scan(&id, &vaultID, &txType, &amount, &currency, &txHash, &status, &errorReason, &createdAt, &updatedAt, &confirmedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return transaction.Transaction{}, transaction.ErrTransactionNotFound
		}
		return transaction.Transaction{}, err
	}

	parsedID, err := uuid.Parse(id)
	if err != nil {
		return transaction.Transaction{}, fmt.Errorf("parse transaction id: %w", err)
	}

	parsedVaultID, err := uuid.Parse(vaultID)
	if err != nil {
		return transaction.Transaction{}, fmt.Errorf("parse vault id: %w", err)
	}

	parsedAmount, err := decimal.NewFromString(amount)
	if err != nil {
		return transaction.Transaction{}, fmt.Errorf("parse amount: %w", err)
	}

	var confirmedAtPtr *time.Time
	if confirmedAt.Valid {
		t := confirmedAt.Time
		confirmedAtPtr = &t
	}

	model := transaction.Transaction{
		ID:          parsedID,
		VaultID:     parsedVaultID,
		Type:        transaction.TransactionType(txType),
		Amount:      parsedAmount,
		Currency:    currency,
		TxHash:      txHash,
		Status:      transaction.TransactionStatus(status),
		ErrorReason: strings.TrimSpace(errorReason.String),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
		ConfirmedAt: confirmedAtPtr,
	}

	return model, nil
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func mapTransactionError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, sql.ErrNoRows) {
		return transaction.ErrTransactionNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23503" && strings.Contains(pgErr.ConstraintName, "vault") {
			return transaction.ErrInvalidTransaction
		}
		if pgErr.Code == "23505" {
			return transaction.ErrInvalidTransaction
		}
	}

	return err
}
