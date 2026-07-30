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

	"github.com/suncrestlabs/nester/apps/api/internal/domain/offramp"
	"github.com/suncrestlabs/nester/apps/api/pkg/listquery"
)

type SettlementRepository struct {
	db *sql.DB
}

func NewSettlementRepository(db *sql.DB) *SettlementRepository {
	return &SettlementRepository{db: db}
}

func (r *SettlementRepository) Create(ctx context.Context, model offramp.Settlement) (offramp.Settlement, error) {
	query := `
		INSERT INTO settlements (
			id, user_id, vault_id,
			amount, currency, fiat_currency, fiat_amount, exchange_rate,
			destination_type, destination_provider,
			destination_account_number, destination_account_name, destination_bank_code,
			status, retry_count, error_message, notes
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING created_at
	`

	if err := r.db.QueryRowContext(
		ctx,
		query,
		model.ID.String(),
		model.UserID.String(),
		model.VaultID.String(),
		model.Amount.String(),
		model.Currency,
		model.FiatCurrency,
		model.FiatAmount.String(),
		model.ExchangeRate.String(),
		model.Destination.Type,
		model.Destination.Provider,
		model.Destination.AccountNumber,
		model.Destination.AccountName,
		model.Destination.BankCode,
		string(model.Status),
		model.RetryCount,
		model.ErrorMessage,
		model.Notes,
	).Scan(&model.CreatedAt); err != nil {
		return offramp.Settlement{}, mapSettlementError(err)
	}

	return model, nil
}

func (r *SettlementRepository) GetByID(ctx context.Context, id uuid.UUID) (offramp.Settlement, error) {
	query := `
		SELECT id, user_id, vault_id,
		       amount, currency, fiat_currency, fiat_amount, exchange_rate,
		       destination_type, destination_provider,
		       destination_account_number, destination_account_name, destination_bank_code,
		       status, retry_count, error_message, notes, estimated_fee, created_at, completed_at
		FROM settlements
		WHERE id = $1
	`

	row := r.db.QueryRowContext(ctx, query, id.String())
	model, err := scanSettlement(row)
	if err != nil {
		return offramp.Settlement{}, mapSettlementError(err)
	}

	return model, nil
}

func (r *SettlementRepository) ListByUserID(
	ctx context.Context,
	userID uuid.UUID,
	filter offramp.UserListFilter,
) ([]offramp.Settlement, int, string, error) {
	where, args := buildUserSettlementWhere(userID, filter)

	countQuery := `SELECT COUNT(*) FROM settlements WHERE ` + where
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, "", mapSettlementError(err)
	}

	sortColumn := sanitizeUserSettlementSort(filter.SortField)
	order := sanitizeOrder(filter.SortOrder)
	useKeyset := filter.Cursor != "" && sortColumn == "created_at" && order == "DESC"

	limit := filter.PerPage + 1
	listArgs := append([]any(nil), args...)
	selectWhere := where

	if useKeyset {
		cursor, err := listquery.DecodeSettlementCursor(filter.Cursor)
		if err != nil {
			return nil, 0, "", err
		}
		keyset, updatedArgs := settlementKeysetClause(cursor, listArgs)
		listArgs = updatedArgs
		selectWhere = where + " AND " + keyset
	}

	var listQuery string
	if useKeyset {
		listQuery = fmt.Sprintf(`
			SELECT id, user_id, vault_id,
			       amount, currency, fiat_currency, fiat_amount, exchange_rate,
			       destination_type, destination_provider,
			       destination_account_number, destination_account_name, destination_bank_code,
			       status, retry_count, error_message, notes, estimated_fee, created_at, completed_at
			FROM settlements
			WHERE %s
			ORDER BY %s %s, id DESC
			LIMIT $%d
		`, selectWhere, sortColumn, order, len(listArgs)+1) // #nosec G201
		listArgs = append(listArgs, limit)
	} else {
		offset := (filter.Page - 1) * filter.PerPage
		listQuery = fmt.Sprintf(`
			SELECT id, user_id, vault_id,
			       amount, currency, fiat_currency, fiat_amount, exchange_rate,
			       destination_type, destination_provider,
			       destination_account_number, destination_account_name, destination_bank_code,
			       status, retry_count, error_message, notes, estimated_fee, created_at, completed_at
			FROM settlements
			WHERE %s
			ORDER BY %s %s
			LIMIT $%d OFFSET $%d
		`, selectWhere, sortColumn, order, len(listArgs)+1, len(listArgs)+2) // #nosec G201
		listArgs = append(listArgs, filter.PerPage, offset)
	}

	rows, err := r.db.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return nil, 0, "", mapSettlementError(err)
	}
	defer rows.Close()

	settlements := make([]offramp.Settlement, 0)
	for rows.Next() {
		model, err := scanSettlement(rows)
		if err != nil {
			return nil, 0, "", err
		}
		settlements = append(settlements, model)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, "", err
	}

	nextCursor := ""
	if useKeyset {
		if len(settlements) > filter.PerPage {
			last := settlements[filter.PerPage-1]
			nextCursor = listquery.EncodeSettlementCursor(last.CreatedAt, last.ID)
			settlements = settlements[:filter.PerPage]
		}
	}

	return settlements, total, nextCursor, nil
}

func (r *SettlementRepository) UpdateStatus(
	ctx context.Context,
	id uuid.UUID,
	status offramp.SettlementStatus,
	completedAt *time.Time,
) error {
	result, err := r.db.ExecContext(
		ctx,
		`UPDATE settlements SET status = $2, completed_at = $3 WHERE id = $1`,
		id.String(),
		string(status),
		completedAt,
	)
	if err != nil {
		return mapSettlementError(err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return offramp.ErrSettlementNotFound
	}

	return nil
}

func scanSettlement(row scanner) (offramp.Settlement, error) {
	var (
		id            string
		userID        string
		vaultID       string
		amount        string
		currency      string
		fiatCurrency  string
		fiatAmount    string
		exchangeRate  string
		destType      string
		destProvider  string
		destAcctNum   string
		destAcctName  string
		destBankCode  string
		status        string
		retryCount    int
		errorMessage  sql.NullString
		notes         sql.NullString
		estimatedFee  sql.NullString
		createdAt     time.Time
		completedAt   sql.NullTime
	)

	if err := row.Scan(
		&id, &userID, &vaultID,
		&amount, &currency, &fiatCurrency, &fiatAmount, &exchangeRate,
		&destType, &destProvider, &destAcctNum, &destAcctName, &destBankCode,
		&status, &retryCount, &errorMessage, &notes, &estimatedFee, &createdAt, &completedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return offramp.Settlement{}, offramp.ErrSettlementNotFound
		}
		return offramp.Settlement{}, err
	}

	parsedID, err := uuid.Parse(id)
	if err != nil {
		return offramp.Settlement{}, fmt.Errorf("parse settlement id: %w", err)
	}

	parsedUserID, err := uuid.Parse(userID)
	if err != nil {
		return offramp.Settlement{}, fmt.Errorf("parse user id: %w", err)
	}

	parsedVaultID, err := uuid.Parse(vaultID)
	if err != nil {
		return offramp.Settlement{}, fmt.Errorf("parse vault id: %w", err)
	}

	parsedAmount, err := decimal.NewFromString(amount)
	if err != nil {
		return offramp.Settlement{}, fmt.Errorf("parse amount: %w", err)
	}

	parsedFiatAmount, err := decimal.NewFromString(fiatAmount)
	if err != nil {
		return offramp.Settlement{}, fmt.Errorf("parse fiat_amount: %w", err)
	}

	parsedRate, err := decimal.NewFromString(exchangeRate)
	if err != nil {
		return offramp.Settlement{}, fmt.Errorf("parse exchange_rate: %w", err)
	}

	var completedAtPtr *time.Time
	if completedAt.Valid {
		t := completedAt.Time
		completedAtPtr = &t
	}

	var estFeePtr *decimal.Decimal
	if estimatedFee.Valid {
		d, err := decimal.NewFromString(estimatedFee.String)
		if err == nil {
			estFeePtr = &d
		}
	}

	return offramp.Settlement{
		ID:           parsedID,
		UserID:       parsedUserID,
		VaultID:      parsedVaultID,
		Amount:       parsedAmount,
		Currency:     currency,
		FiatCurrency: fiatCurrency,
		FiatAmount:   parsedFiatAmount,
		ExchangeRate: parsedRate,
		Destination: offramp.Destination{
			Type:          destType,
			Provider:      destProvider,
			AccountNumber: destAcctNum,
			AccountName:   destAcctName,
			BankCode:      destBankCode,
		},
		Status:       offramp.SettlementStatus(status),
		RetryCount:   retryCount,
		ErrorMessage: errorMessage.String,
		Notes:        notes.String,
		EstimatedFee: estFeePtr,
		CreatedAt:    createdAt,
		CompletedAt:  completedAtPtr,
	}, nil
}

func mapSettlementError(err error) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23503" && strings.Contains(pgErr.ConstraintName, "user") {
			return offramp.ErrUserNotFound
		}
		if pgErr.Code == "23503" && strings.Contains(pgErr.ConstraintName, "vault") {
			return offramp.ErrVaultNotFound
		}
	}

	return err
}
