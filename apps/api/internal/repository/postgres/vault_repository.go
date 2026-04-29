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

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

type VaultRepository struct {
	db *sql.DB
}

func NewVaultRepository(db *sql.DB) *VaultRepository {
	return &VaultRepository{db: db}
}

func (r *VaultRepository) CreateVault(ctx context.Context, model vault.Vault) (vault.Vault, error) {
	query := `
		INSERT INTO vaults (
			id, user_id, contract_address, total_deposited, current_balance, currency, status, yield_earned, fees_paid
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING created_at, updated_at
	`

	if err := r.db.QueryRowContext(
		ctx,
		query,
		model.ID.String(),
		model.UserID.String(),
		model.ContractAddress,
		model.TotalDeposited.String(),
		model.CurrentBalance.String(),
		model.Currency,
		string(model.Status),
		model.YieldEarned.String(),
		model.FeesPaid.String(),
	).Scan(&model.CreatedAt, &model.UpdatedAt); err != nil {
		return vault.Vault{}, mapRepositoryError(err)
	}

	return model, nil
}

func (r *VaultRepository) GetVault(ctx context.Context, id uuid.UUID) (vault.Vault, error) {
	query := `
		SELECT id, user_id, contract_address, total_deposited, current_balance, currency, status, created_at, updated_at
		FROM vaults
		WHERE id = $1 AND deleted_at IS NULL
	`

	model, err := scanVault(r.db.QueryRowContext(ctx, query, id.String()))
	if err != nil {
		return vault.Vault{}, mapRepositoryError(err)
	}

	allocations, err := loadAllocations(ctx, r.db, id)
	if err != nil {
		return vault.Vault{}, err
	}

	model.Allocations = allocations
	return model, nil
}

func (r *VaultRepository) GetUserVaults(ctx context.Context, userID uuid.UUID) ([]vault.Vault, error) {
	query := `
		SELECT id, user_id, contract_address, total_deposited, current_balance, currency, status, created_at, updated_at
		FROM vaults
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID.String())
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	defer rows.Close()

	vaults := make([]vault.Vault, 0)
	for rows.Next() {
		model, err := scanVault(rows)
		if err != nil {
			return nil, err
		}

		allocations, err := loadAllocations(ctx, r.db, model.ID)
		if err != nil {
			return nil, err
		}

		model.Allocations = allocations
		vaults = append(vaults, model)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return vaults, nil
}

// ListActive returns every non-deleted vault whose status is `active`. Used
// by the performance tracker so it can iterate live vaults each tick.
func (r *VaultRepository) ListActive(ctx context.Context) ([]vault.Vault, error) {
	const query = `
		SELECT id, user_id, contract_address, total_deposited, current_balance, currency, status, created_at, updated_at
		FROM vaults
		WHERE deleted_at IS NULL AND status = 'active'
		ORDER BY created_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	defer rows.Close()

	out := make([]vault.Vault, 0)
	for rows.Next() {
		model, err := scanVault(rows)
		if err != nil {
			return nil, err
		}
		allocations, err := loadAllocations(ctx, r.db, model.ID)
		if err != nil {
			return nil, err
		}
		model.Allocations = allocations
		out = append(out, model)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *VaultRepository) UpdateVaultBalances(ctx context.Context, id uuid.UUID, totalDeposited decimal.Decimal, currentBalance decimal.Decimal) error {
	result, err := r.db.ExecContext(
		ctx,
		`UPDATE vaults SET total_deposited = $2, current_balance = $3, updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL`,
		id.String(),
		totalDeposited.String(),
		currentBalance.String(),
	)
	if err != nil {
		return mapRepositoryError(err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return vault.ErrVaultNotFound
	}

	return nil
}

func (r *VaultRepository) RecordDeposit(ctx context.Context, id uuid.UUID, amount decimal.Decimal) error {
	if amount.Cmp(decimal.Zero) <= 0 {
		return vault.ErrInvalidAmount
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(
		ctx,
		`UPDATE vaults
		 SET total_deposited = total_deposited + $2::numeric,
		     current_balance = current_balance + $2::numeric,
		     updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`,
		id.String(),
		amount.String(),
	)
	if err != nil {
		return mapRepositoryError(err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return vault.ErrVaultNotFound
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO vault_transactions (vault_id, type, amount) VALUES ($1, 'deposit', $2::numeric)`,
		id.String(),
		amount.String(),
	); err != nil {
		return mapRepositoryError(err)
	}

	return tx.Commit()
}

func (r *VaultRepository) ReplaceAllocations(ctx context.Context, vaultID uuid.UUID, allocations []vault.Allocation) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if err := ensureVaultExists(ctx, tx, vaultID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM allocations WHERE vault_id = $1`, vaultID.String()); err != nil {
		return mapRepositoryError(err)
	}

	for _, allocation := range allocations {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO allocations (id, vault_id, protocol, amount, apy, status, allocated_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			allocation.ID.String(),
			vaultID.String(),
			allocation.Protocol,
			allocation.Amount.String(),
			allocation.APY.String(),
			allocation.Status,
			allocation.AllocatedAt.UTC(),
		); err != nil {
			return mapRepositoryError(err)
		}
	}

	if _, err := tx.ExecContext(ctx, `UPDATE vaults SET updated_at = NOW() WHERE id = $1`, vaultID.String()); err != nil {
		return mapRepositoryError(err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

// UpdateVault performs a partial update on a vault row (contract address and/or
// status). The caller is responsible for pre-validating the state transition.
func (r *VaultRepository) UpdateVault(ctx context.Context, id uuid.UUID, contractAddress string, status vault.VaultStatus) error {
	result, err := r.db.ExecContext(
		ctx,
		`UPDATE vaults
		 SET contract_address = $2, status = $3, updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`,
		id.String(),
		contractAddress,
		string(status),
	)
	if err != nil {
		return mapRepositoryError(err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return vault.ErrVaultNotFound
	}

	return nil
}

// RecordWithdrawal decrements current_balance atomically and writes a ledger
// entry. It does NOT touch total_deposited (deposits are never reversed).
func (r *VaultRepository) RecordWithdrawal(ctx context.Context, id uuid.UUID, amount decimal.Decimal) error {
	if amount.Cmp(decimal.Zero) <= 0 {
		return vault.ErrInvalidAmount
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(
		ctx,
		`UPDATE vaults
		 SET current_balance = current_balance - $2::numeric,
		     updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`,
		id.String(),
		amount.String(),
	)
	if err != nil {
		return mapRepositoryError(err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return vault.ErrVaultNotFound
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO vault_transactions (vault_id, type, amount) VALUES ($1, 'withdrawal', $2::numeric)`,
		id.String(),
		amount.String(),
	); err != nil {
		return mapRepositoryError(err)
	}

	return tx.Commit()
}

// SoftDeleteVault stamps deleted_at so reads exclude this vault going forward.
func (r *VaultRepository) SoftDeleteVault(ctx context.Context, id uuid.UUID) error {
	result, err := r.db.ExecContext(
		ctx,
		`UPDATE vaults SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`,
		id.String(),
	)
	if err != nil {
		return mapRepositoryError(err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return vault.ErrVaultNotFound
	}

	return nil
}

// ListDeposits returns all deposit transactions for a vault ordered newest
// first.
func (r *VaultRepository) ListDeposits(ctx context.Context, vaultID uuid.UUID) ([]vault.VaultTransaction, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, vault_id, user_id, type, amount, COALESCE(transaction_hash, ''), shares_minted_or_burned, share_price_at_time, fee_charged, created_at
		 FROM vault_transactions
		 WHERE vault_id = $1 AND type = 'deposit'
		 ORDER BY created_at DESC`,
		vaultID.String(),
	)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	defer rows.Close()

	txns := make([]vault.VaultTransaction, 0)
	for rows.Next() {
		txn, err := scanVaultTransaction(rows)
		if err != nil {
			return nil, err
		}
		txns = append(txns, txn)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return txns, nil
}

// ── scanners ─────────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func scanVault(row scanner) (vault.Vault, error) {
	var (
		id              string
		userID          string
		totalDeposited  string
		currentBalance  string
		contractAddress string
		currency        string
		status          string
		yieldEarned     string
		feesPaid        string
		lastSyncedAt    sql.NullTime
		deletedAt       sql.NullTime
		createdAt       time.Time
		updatedAt       time.Time
	)

	if err := row.Scan(
		&id,
		&userID,
		&contractAddress,
		&totalDeposited,
		&currentBalance,
		&currency,
		&status,
		&yieldEarned,
		&feesPaid,
		&lastSyncedAt,
		&deletedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return vault.Vault{}, vault.ErrVaultNotFound
		}
		return vault.Vault{}, err
	}

	parsedID, err := uuid.Parse(id)
	if err != nil {
		return vault.Vault{}, fmt.Errorf("parse vault id: %w", err)
	}

	parsedUserID, err := uuid.Parse(userID)
	if err != nil {
		return vault.Vault{}, fmt.Errorf("parse user id: %w", err)
	}

	parsedDeposited, err := decimal.NewFromString(totalDeposited)
	if err != nil {
		return vault.Vault{}, fmt.Errorf("parse total deposited: %w", err)
	}

	parsedBalance, err := decimal.NewFromString(currentBalance)
	if err != nil {
		return vault.Vault{}, fmt.Errorf("parse current balance: %w", err)
	}

	parsedYield, _ := decimal.NewFromString(yieldEarned)
	parsedFees, _ := decimal.NewFromString(feesPaid)

	var lastSyncedAtPtr *time.Time
	if lastSyncedAt.Valid {
		t := lastSyncedAt.Time
		lastSyncedAtPtr = &t
	}

	var deletedAtPtr *time.Time
	if deletedAt.Valid {
		t := deletedAt.Time
		deletedAtPtr = &t
	}

	return vault.Vault{
		ID:              parsedID,
		UserID:          parsedUserID,
		ContractAddress: contractAddress,
		TotalDeposited:  parsedDeposited,
		CurrentBalance:  parsedBalance,
		Currency:        currency,
		Status:          vault.VaultStatus(status),
		YieldEarned:     parsedYield,
		FeesPaid:        parsedFees,
		LastSyncedAt:    lastSyncedAtPtr,
		DeletedAt:       deletedAtPtr,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}, nil
}

func scanVaultTransaction(row scanner) (vault.VaultTransaction, error) {
	var (
		id        string
		vaultID   string
		userID    sql.NullString
		txType    string
		amount    string
		txHash    string
		shares    sql.NullString
		sharePrice sql.NullString
		fee       sql.NullString
		createdAt time.Time
	)

	if err := row.Scan(&id, &vaultID, &userID, &txType, &amount, &txHash, &shares, &sharePrice, &fee, &createdAt); err != nil {
		return vault.VaultTransaction{}, err
	}

	parsedID, err := uuid.Parse(id)
	if err != nil {
		return vault.VaultTransaction{}, fmt.Errorf("parse transaction id: %w", err)
	}

	parsedVaultID, err := uuid.Parse(vaultID)
	if err != nil {
		return vault.VaultTransaction{}, fmt.Errorf("parse transaction vault_id: %w", err)
	}

	parsedAmount, err := decimal.NewFromString(amount)
	if err != nil {
		return vault.VaultTransaction{}, fmt.Errorf("parse transaction amount: %w", err)
	}

	var userIDPtr *uuid.UUID
	if userID.Valid {
		uid, _ := uuid.Parse(userID.String)
		userIDPtr = &uid
	}

	var sharesPtr *decimal.Decimal
	if shares.Valid {
		d, _ := decimal.NewFromString(shares.String)
		sharesPtr = &d
	}

	var sharePricePtr *decimal.Decimal
	if sharePrice.Valid {
		d, _ := decimal.NewFromString(sharePrice.String)
		sharePricePtr = &d
	}

	var feePtr *decimal.Decimal
	if fee.Valid {
		d, _ := decimal.NewFromString(fee.String)
		feePtr = &d
	}

	return vault.VaultTransaction{
		ID:                   parsedID,
		VaultID:              parsedVaultID,
		UserID:               userIDPtr,
		Type:                 txType,
		Amount:               parsedAmount,
		TransactionHash:      txHash,
		SharesMintedOrBurned: sharesPtr,
		SharePriceAtTime:     sharePricePtr,
		FeeCharged:           feePtr,
		CreatedAt:            createdAt,
	}, nil
}

func loadAllocations(ctx context.Context, db queryer, vaultID uuid.UUID) ([]vault.Allocation, error) {
	rows, err := db.QueryContext(
		ctx,
		`SELECT id, vault_id, protocol, amount, apy, status, allocated_at, updated_at FROM allocations WHERE vault_id = $1 ORDER BY allocated_at DESC`,
		vaultID.String(),
	)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	defer rows.Close()

	allocations := make([]vault.Allocation, 0)
	for rows.Next() {
		var (
			id          string
			parsedVault string
			protocol    string
			amount      string
			apy         string
			status      string
			allocatedAt time.Time
			updatedAt   sql.NullTime
		)

		if err := rows.Scan(&id, &parsedVault, &protocol, &amount, &apy, &status, &allocatedAt, &updatedAt); err != nil {
			return nil, err
		}

		allocationID, err := uuid.Parse(id)
		if err != nil {
			return nil, fmt.Errorf("parse allocation id: %w", err)
		}

		vaultUUID, err := uuid.Parse(parsedVault)
		if err != nil {
			return nil, fmt.Errorf("parse allocation vault id: %w", err)
		}

		parsedAmount, err := decimal.NewFromString(amount)
		if err != nil {
			return nil, fmt.Errorf("parse allocation amount: %w", err)
		}

		parsedAPY, err := decimal.NewFromString(apy)
		if err != nil {
			return nil, fmt.Errorf("parse allocation apy: %w", err)
		}

		var updatedPtr *time.Time
		if updatedAt.Valid {
			t := updatedAt.Time
			updatedPtr = &t
		}

		allocations = append(allocations, vault.Allocation{
			ID:          allocationID,
			VaultID:     vaultUUID,
			Protocol:    protocol,
			Amount:      parsedAmount,
			APY:         parsedAPY,
			Status:      status,
			AllocatedAt: allocatedAt,
			UpdatedAt:   updatedPtr,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return allocations, nil
}

func ensureVaultExists(ctx context.Context, tx *sql.Tx, vaultID uuid.UUID) error {
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT TRUE FROM vaults WHERE id = $1 AND deleted_at IS NULL`, vaultID.String()).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return vault.ErrVaultNotFound
		}
		return err
	}
	return nil
}

func mapRepositoryError(err error) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23503" && strings.Contains(pgErr.ConstraintName, "user") {
			return vault.ErrUserNotFound
		}
		if pgErr.Code == "23503" && strings.Contains(pgErr.ConstraintName, "vault") {
			return vault.ErrVaultNotFound
		}
	}

	return err
}
