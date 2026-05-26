package postgres

import (
	"context"
	"database/sql"
	"encoding/base64"
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

// decodeCursor decodes a base64 cursor into (createdAt, id).
func decodeCursor(cursor string) (time.Time, string, error) {
	if cursor == "" {
		return time.Time{}, "", nil
	}
	b, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid cursor")
	}
	parts := strings.SplitN(string(b), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("invalid cursor format")
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid cursor timestamp")
	}
	return t, parts[1], nil
}

// encodeCursor encodes (createdAt, id) into a base64 cursor string.
func encodeCursor(createdAt time.Time, id string) string {
	raw := createdAt.UTC().Format(time.RFC3339Nano) + "|" + id
	return base64.URLEncoding.EncodeToString([]byte(raw))
}

// ListByUserID returns a cursor-paginated list of transactions for a user,
// joining through the vaults table to resolve ownership.
func (r *TransactionRepository) ListByUserID(ctx context.Context, filter transaction.ListFilter) (transaction.Page[transaction.Transaction], error) {
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 25
	}

	// Decode cursor (gives us createdAt + id boundary for keyset pagination).
	var cursorTime time.Time
	var cursorID string
	if filter.Cursor != "" {
		var err error
		cursorTime, cursorID, err = decodeCursor(filter.Cursor)
		if err != nil {
			return transaction.Page[transaction.Transaction]{}, err
		}
	}

	// --- Count query (total matching rows, ignoring cursor) ---
	countArgs := []any{filter.UserID}
	countClauses := []string{"v.user_id = $1"}
	argIdx := 2

	if len(filter.Types) > 0 {
		placeholders := make([]string, len(filter.Types))
		for i, t := range filter.Types {
			placeholders[i] = fmt.Sprintf("$%d", argIdx)
			countArgs = append(countArgs, string(t))
			argIdx++
		}
		countClauses = append(countClauses, "t.type IN ("+strings.Join(placeholders, ",")+")")
	}
	if filter.Status != "" {
		countClauses = append(countClauses, fmt.Sprintf("t.status = $%d", argIdx))
		countArgs = append(countArgs, string(filter.Status))
		argIdx++
	}
	if filter.From != nil {
		countClauses = append(countClauses, fmt.Sprintf("t.created_at >= $%d", argIdx))
		countArgs = append(countArgs, *filter.From)
		argIdx++
	}
	if filter.To != nil {
		countClauses = append(countClauses, fmt.Sprintf("t.created_at <= $%d", argIdx))
		countArgs = append(countArgs, *filter.To)
		argIdx++
	}
	if filter.VaultID != "" {
		if vaultUUID, err := uuid.Parse(filter.VaultID); err == nil {
			countClauses = append(countClauses, fmt.Sprintf("t.vault_id = $%d", argIdx))
			countArgs = append(countArgs, vaultUUID)
			argIdx++
		}
	}
	if filter.Search != "" {
		countClauses = append(countClauses, fmt.Sprintf("(t.tx_hash ILIKE $%d OR t.amount::text ILIKE $%d)", argIdx, argIdx))
		countArgs = append(countArgs, "%"+filter.Search+"%")
		argIdx++
	}

	countQuery := `SELECT COUNT(*) FROM transactions t
		JOIN vaults v ON t.vault_id = v.id
		WHERE ` + strings.Join(countClauses, " AND ")

	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return transaction.Page[transaction.Transaction]{}, err
	}

	// --- Sum query for summary stats (Deposit, Withdrawal, Yield Earned completed totals) ---
	sumArgs := []any{filter.UserID}
	sumClauses := []string{"v.user_id = $1", "t.status = 'completed'"}
	sumArgIdx := 2

	if filter.From != nil {
		sumClauses = append(sumClauses, fmt.Sprintf("t.created_at >= $%d", sumArgIdx))
		sumArgs = append(sumArgs, *filter.From)
		sumArgIdx++
	}
	if filter.To != nil {
		sumClauses = append(sumClauses, fmt.Sprintf("t.created_at <= $%d", sumArgIdx))
		sumArgs = append(sumArgs, *filter.To)
		sumArgIdx++
	}
	if filter.VaultID != "" {
		if vaultUUID, err := uuid.Parse(filter.VaultID); err == nil {
			sumClauses = append(sumClauses, fmt.Sprintf("t.vault_id = $%d", sumArgIdx))
			sumArgs = append(sumArgs, vaultUUID)
			sumArgIdx++
		}
	}
	if filter.Search != "" {
		sumClauses = append(sumClauses, fmt.Sprintf("(t.tx_hash ILIKE $%d OR t.amount::text ILIKE $%d)", sumArgIdx, sumArgIdx))
		sumArgs = append(sumArgs, "%"+filter.Search+"%")
		sumArgIdx++
	}

	sumQuery := `SELECT t.type, COALESCE(SUM(t.amount), 0) FROM transactions t
		JOIN vaults v ON t.vault_id = v.id
		WHERE ` + strings.Join(sumClauses, " AND ") + `
		GROUP BY t.type`

	var totalDeposited decimal.Decimal
	var totalWithdrawn decimal.Decimal
	var totalYield decimal.Decimal

	sumRows, err := r.db.QueryContext(ctx, sumQuery, sumArgs...)
	if err == nil {
		defer sumRows.Close()
		for sumRows.Next() {
			var tType string
			var sum decimal.Decimal
			if err := sumRows.Scan(&tType, &sum); err == nil {
				switch transaction.TransactionType(tType) {
				case transaction.TypeDeposit:
					totalDeposited = sum
				case transaction.TypeWithdrawal:
					totalWithdrawn = sum
				case transaction.TypeYieldEarned:
					totalYield = sum
				}
			}
		}
	}

	// --- Data query (keyset pagination) ---
	dataArgs := []any{filter.UserID}
	dataClauses := []string{"v.user_id = $1"}
	argIdx = 2

	if len(filter.Types) > 0 {
		placeholders := make([]string, len(filter.Types))
		for i, tp := range filter.Types {
			placeholders[i] = fmt.Sprintf("$%d", argIdx)
			dataArgs = append(dataArgs, string(tp))
			argIdx++
		}
		dataClauses = append(dataClauses, "t.type IN ("+strings.Join(placeholders, ",")+")")
	}
	if filter.Status != "" {
		dataClauses = append(dataClauses, fmt.Sprintf("t.status = $%d", argIdx))
		dataArgs = append(dataArgs, string(filter.Status))
		argIdx++
	}
	if filter.From != nil {
		dataClauses = append(dataClauses, fmt.Sprintf("t.created_at >= $%d", argIdx))
		dataArgs = append(dataArgs, *filter.From)
		argIdx++
	}
	if filter.To != nil {
		dataClauses = append(dataClauses, fmt.Sprintf("t.created_at <= $%d", argIdx))
		dataArgs = append(dataArgs, *filter.To)
		argIdx++
	}
	if filter.VaultID != "" {
		if vaultUUID, err := uuid.Parse(filter.VaultID); err == nil {
			dataClauses = append(dataClauses, fmt.Sprintf("t.vault_id = $%d", argIdx))
			dataArgs = append(dataArgs, vaultUUID)
			argIdx++
		}
	}
	if filter.Search != "" {
		dataClauses = append(dataClauses, fmt.Sprintf("(t.tx_hash ILIKE $%d OR t.amount::text ILIKE $%d)", argIdx, argIdx))
		dataArgs = append(dataArgs, "%"+filter.Search+"%")
		argIdx++
	}
	if !cursorTime.IsZero() && cursorID != "" {
		dataClauses = append(dataClauses, fmt.Sprintf("(t.created_at, t.id) < ($%d, $%d)", argIdx, argIdx+1))
		dataArgs = append(dataArgs, cursorTime, cursorID)
		argIdx += 2
	}

	// fetch limit+1 to detect next page
	dataArgs = append(dataArgs, limit+1)
	limitPlaceholder := fmt.Sprintf("$%d", argIdx)

	dataQuery := `
		SELECT t.id, t.vault_id, t.type, t.amount, t.currency, t.tx_hash,
		       t.status, t.error_reason, t.created_at, t.updated_at, t.confirmed_at
		FROM transactions t
		JOIN vaults v ON t.vault_id = v.id
		WHERE ` + strings.Join(dataClauses, " AND ") + `
		ORDER BY t.created_at DESC, t.id DESC
		LIMIT ` + limitPlaceholder

	rows, err := r.db.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return transaction.Page[transaction.Transaction]{}, err
	}
	defer rows.Close()

	var items []transaction.Transaction
	for rows.Next() {
		m, err := scanTransaction(rows)
		if err != nil {
			return transaction.Page[transaction.Transaction]{}, err
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return transaction.Page[transaction.Transaction]{}, err
	}

	var nextCursor string
	if len(items) > limit {
		items = items[:limit] // trim the extra row
		last := items[len(items)-1]
		nextCursor = encodeCursor(last.CreatedAt, last.ID.String())
	}

	return transaction.Page[transaction.Transaction]{
		Items:          items,
		NextCursor:     nextCursor,
		Total:          total,
		TotalDeposited: totalDeposited,
		TotalWithdrawn: totalWithdrawn,
		TotalYield:     totalYield,
	}, nil
}

