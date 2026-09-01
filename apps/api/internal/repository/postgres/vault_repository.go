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

	"github.com/suncrestlabs/nester/apps/api/internal/domain/balanceaudit"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

type VaultRepository struct {
	db *sql.DB
}

func NewVaultRepository(db *sql.DB) *VaultRepository {
	return &VaultRepository{db: db}
}

func (r *VaultRepository) CreateVault(ctx context.Context, model vault.Vault) (vault.Vault, error) {
	if model.HarvestFrequency == "" {
		model.HarvestFrequency = vault.DefaultHarvestFrequency
	}

	query := `
		INSERT INTO vaults (
			id, user_id, contract_address, total_deposited, current_balance, currency, status, yield_earned, fees_paid, harvest_frequency
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
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
		model.HarvestFrequency,
	).Scan(&model.CreatedAt, &model.UpdatedAt); err != nil {
		return vault.Vault{}, mapRepositoryError(err)
	}

	return model, nil
}

func (r *VaultRepository) GetVault(ctx context.Context, id uuid.UUID) (vault.Vault, error) {
	query := `
		SELECT id, user_id, contract_address, total_deposited, current_balance, currency, status, yield_earned, fees_paid, harvest_frequency, last_harvested_at, last_synced_at, deleted_at, created_at, updated_at
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

func (r *VaultRepository) ListUserVaults(
	ctx context.Context,
	userID uuid.UUID,
	filter vault.UserListFilter,
) ([]vault.Vault, int, error) {
	where, args := buildUserVaultWhere(userID, filter)

	countQuery := `SELECT COUNT(*) FROM vaults WHERE ` + where
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, mapRepositoryError(err)
	}

	sortColumn := sanitizeUserVaultSort(filter.SortField)
	order := sanitizeOrder(filter.SortOrder)
	offset := (filter.Page - 1) * filter.PerPage

	listQuery := fmt.Sprintf(`
		SELECT id, user_id, contract_address, total_deposited, current_balance, currency, status, yield_earned, fees_paid, harvest_frequency, last_harvested_at, last_synced_at, deleted_at, created_at, updated_at
		FROM vaults
		WHERE %s
		ORDER BY %s %s
		LIMIT $%d OFFSET $%d
	`, where, sortColumn, order, len(args)+1, len(args)+2) // #nosec G201 -- sort/order from whitelist

	args = append(args, filter.PerPage, offset)
	rows, err := r.db.QueryContext(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, mapRepositoryError(err)
	}
	defer rows.Close()

	vaults := make([]vault.Vault, 0)
	for rows.Next() {
		model, err := scanVault(rows)
		if err != nil {
			return nil, 0, err
		}

		allocations, err := loadAllocations(ctx, r.db, model.ID)
		if err != nil {
			return nil, 0, err
		}

		model.Allocations = allocations
		vaults = append(vaults, model)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return vaults, total, nil
}

// ListVaults returns a paginated slice of all non-deleted vaults.
func (r *VaultRepository) ListVaults(ctx context.Context, filter vault.ListFilter) ([]vault.Vault, int, error) {
	args := []any{}
	where := "deleted_at IS NULL"
	if filter.Status != "" {
		args = append(args, filter.Status)
		where += fmt.Sprintf(" AND status = $%d", len(args))
	}

	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM vaults WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, mapRepositoryError(err)
	}

	args = append(args, filter.Limit, filter.Offset)
	listQuery := fmt.Sprintf(`
		SELECT id, user_id, contract_address, total_deposited, current_balance, currency, status, yield_earned, fees_paid, harvest_frequency, last_harvested_at, last_synced_at, deleted_at, created_at, updated_at
		FROM vaults
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, where, len(args)-1, len(args)) // #nosec G201 -- where is built from whitelist only

	rows, err := r.db.QueryContext(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, mapRepositoryError(err)
	}
	defer rows.Close()

	out := make([]vault.Vault, 0, filter.Limit)
	for rows.Next() {
		model, err := scanVault(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, model)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// ListActive returns every non-deleted vault whose status is `active`. Used
// by the performance tracker so it can iterate live vaults each tick.
func (r *VaultRepository) ListActive(ctx context.Context) ([]vault.Vault, error) {
	const query = `
		SELECT id, user_id, contract_address, total_deposited, current_balance, currency, status, yield_earned, fees_paid, harvest_frequency, last_harvested_at, last_synced_at, deleted_at, created_at, updated_at
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

// VaultAPYInfo is a lightweight projection used by the APY deviation scheduler.
type VaultAPYInfo struct {
	ID                 uuid.UUID
	UserID             uuid.UUID
	Currency           string
	LastAPYAlertSentAt *time.Time
}

// ListActiveVaultsForAPYCheck returns id, user_id, currency, and last_apy_alert_sent_at
// for all active, non-deleted vaults. Used by the APY deviation check job (#670).
func (r *VaultRepository) ListActiveVaultsForAPYCheck(ctx context.Context) ([]VaultAPYInfo, error) {
	const query = `
		SELECT id, user_id, currency, last_apy_alert_sent_at
		FROM vaults
		WHERE deleted_at IS NULL AND status = 'active'
		ORDER BY created_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VaultAPYInfo
	for rows.Next() {
		var (
			id, userID, currency string
			alertAt              sql.NullTime
		)
		if err := rows.Scan(&id, &userID, &currency, &alertAt); err != nil {
			return nil, err
		}
		parsedID, _ := uuid.Parse(id)
		parsedUserID, _ := uuid.Parse(userID)
		var alertAtPtr *time.Time
		if alertAt.Valid {
			t := alertAt.Time
			alertAtPtr = &t
		}
		out = append(out, VaultAPYInfo{
			ID:                 parsedID,
			UserID:             parsedUserID,
			Currency:           currency,
			LastAPYAlertSentAt: alertAtPtr,
		})
	}
	return out, rows.Err()
}

// UpdateAPYAlertSentAt records the time an APY drop alert was sent for a vault (#670).
func (r *VaultRepository) UpdateAPYAlertSentAt(ctx context.Context, vaultID uuid.UUID, sentAt time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE vaults SET last_apy_alert_sent_at = $2 WHERE id = $1 AND deleted_at IS NULL`,
		vaultID.String(), sentAt)
	return err
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

// RecordDeposit credits a vault for a deposit recorded through the API and
// writes the matching ledger row.
//
// The ledger insert is ON CONFLICT (transaction_hash) DO NOTHING and, when the
// hash is already claimed, the whole call becomes a no-op: the event indexer
// claims the same key before crediting the same on-chain movement, so a deposit
// observed by both writers is credited exactly once (nester#1147). See
// internal/stellar/balance_ownership.go for the ownership model.
//
// A record with no transaction hash keeps the previous behaviour — it cannot
// collide, because NULLs stay distinct under the unique index, and nothing else
// will credit it.
//
// Lock ordering is unchanged (see RecordWithdrawal): the vaults row is updated
// first, the ledger row inserted second. The claim is therefore detected after
// the UPDATE, and a lost claim rolls the UPDATE back with the transaction.
func (r *VaultRepository) RecordDeposit(ctx context.Context, id uuid.UUID, record vault.TransactionRecord) error {
	if record.Amount.Cmp(decimal.Zero) <= 0 {
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
		record.Amount.String(),
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

	ledgerRow, err := tx.ExecContext(
		ctx,
		`INSERT INTO vault_transactions (
			vault_id, user_id, type, amount, transaction_hash,
			shares_minted_or_burned, share_price_at_time, fee_charged
		) VALUES ($1, $2, 'deposit', $3::numeric, NULLIF($4, ''), $5::numeric, $6::numeric, $7::numeric)
		ON CONFLICT (transaction_hash) DO NOTHING`,
		id.String(),
		record.UserID.String(),
		record.Amount.String(),
		record.TransactionHash,
		record.SharesMintedOrBurned.String(),
		record.SharePriceAtTime.String(),
		record.FeeCharged.String(),
	)
	if err != nil {
		return mapRepositoryError(err)
	}

	inserted, err := ledgerRow.RowsAffected()
	if err != nil {
		return err
	}
	if inserted == 0 {
		// The hash was already claimed, so this deposit has already been
		// credited (by the indexer, or by an earlier retry of this call).
		// Roll the UPDATE back rather than committing a second credit.
		return vault.ErrDuplicateTransaction
	}

	// --- Ledger: post balanced double-entry within same DB transaction ---
	// This makes the ledger and domain tables atomic — the core invariant.
	// Runs only after the duplicate-transaction guard above, so a replayed
	// hash cannot produce a second set of postings.
	if err := r.postDepositLedgerTx(ctx, tx, id, record.UserID, record.Amount, record.TransactionHash); err != nil {
		// If ledger tables don't exist (old tests), skip; otherwise fail.
		if !isLedgerTableMissing(err) {
			return fmt.Errorf("ledger deposit posting failed: %w", err)
		}
	}

	return tx.Commit()
}

// RecordDepositWithAudit performs the balance credit, the (optional)
// transactional launch-cap check, and the balance-audit ledger append as one
// atomic transaction — either all three commit, or none do.
//
// This closes two gaps CodeRabbit found in the earlier non-atomic path:
//  1. TOCTOU on the launch caps (nester#1119): CheckDeposit used to read
//     totals and return well before the balance credit committed, so two
//     concurrent deposits could each pass the check and collectively exceed
//     a cap. Here, capCheck runs after acquiring Postgres advisory
//     transaction locks keyed on the user and (for the global cap) a fixed
//     key, held until commit/rollback, so totals are read and the credit
//     applied as one atomic step with respect to every other deposit.
//  2. The audit trail (nester#1124) used to be appended in a separate,
//     best-effort step after the balance change had already committed, so a
//     failure there left a permanent audit gap. Here the append is inside
//     the same transaction, so it commits or rolls back with the balance
//     change.
//
// auditTemplate must have VaultID/BalanceBefore/BalanceAfter unset — they
// are filled in from the row locked and updated here.
// capCheck's parameter/return signature is intentionally an unnamed func
// type, not a named one, matched structurally by an identical unnamed type
// in the caller-side interface (internal/service.depositAuditRepository) so
// the service layer can type-assert against this method without importing
// this package.
func (r *VaultRepository) RecordDepositWithAudit(
	ctx context.Context,
	vaultID uuid.UUID,
	record vault.TransactionRecord,
	capCheck func(ctx context.Context, currentUserTotal, currentGlobalTotal decimal.Decimal) error,
	auditTemplate balanceaudit.Entry,
) (balanceaudit.Entry, error) {
	if record.Amount.Cmp(decimal.Zero) <= 0 {
		return balanceaudit.Entry{}, vault.ErrInvalidAmount
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return balanceaudit.Entry{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var before decimal.Decimal
	if err := tx.QueryRowContext(ctx,
		`SELECT current_balance FROM vaults WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`,
		vaultID.String(),
	).Scan(&before); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return balanceaudit.Entry{}, vault.ErrVaultNotFound
		}
		return balanceaudit.Entry{}, err
	}

	if capCheck != nil {
		// Advisory transaction locks: released automatically on commit or
		// rollback, and cheap relative to locking every row in `vaults`.
		// Keyed so two deposits for different users (or a deposit and a
		// withdrawal, which doesn't take this path) never contend on the
		// per-user key, only ever on the shared global key.
		if _, err := tx.ExecContext(ctx,
			`SELECT pg_advisory_xact_lock(hashtext('nester:launch_cap:user:' || $1::text))`,
			record.UserID.String(),
		); err != nil {
			return balanceaudit.Entry{}, err
		}
		if _, err := tx.ExecContext(ctx,
			`SELECT pg_advisory_xact_lock(hashtext('nester:launch_cap:global'))`,
		); err != nil {
			return balanceaudit.Entry{}, err
		}

		var userTotal, globalTotal decimal.Decimal
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(current_balance), 0) FROM vaults WHERE user_id = $1 AND deleted_at IS NULL`,
			record.UserID.String(),
		).Scan(&userTotal); err != nil {
			return balanceaudit.Entry{}, err
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(current_balance), 0) FROM vaults WHERE deleted_at IS NULL`,
		).Scan(&globalTotal); err != nil {
			return balanceaudit.Entry{}, err
		}

		if err := capCheck(ctx, userTotal, globalTotal); err != nil {
			return balanceaudit.Entry{}, err
		}
	}

	result, err := tx.ExecContext(
		ctx,
		`UPDATE vaults
		 SET total_deposited = total_deposited + $2::numeric,
		     current_balance = current_balance + $2::numeric,
		     updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`,
		vaultID.String(),
		record.Amount.String(),
	)
	if err != nil {
		return balanceaudit.Entry{}, mapRepositoryError(err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return balanceaudit.Entry{}, err
	}
	if rowsAffected == 0 {
		return balanceaudit.Entry{}, vault.ErrVaultNotFound
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO vault_transactions (
			vault_id, user_id, type, amount, transaction_hash,
			shares_minted_or_burned, share_price_at_time, fee_charged
		) VALUES ($1, $2, 'deposit', $3::numeric, NULLIF($4, ''), $5::numeric, $6::numeric, $7::numeric)`,
		vaultID.String(),
		record.UserID.String(),
		record.Amount.String(),
		record.TransactionHash,
		record.SharesMintedOrBurned.String(),
		record.SharePriceAtTime.String(),
		record.FeeCharged.String(),
	); err != nil {
		return balanceaudit.Entry{}, mapRepositoryError(err)
	}

	auditTemplate.VaultID = vaultID
	auditTemplate.BalanceBefore = before
	auditTemplate.BalanceAfter = before.Add(record.Amount)
	inserted, err := appendBalanceAuditEntry(ctx, tx, auditTemplate)
	if err != nil {
		return balanceaudit.Entry{}, err
	}

	// --- Ledger: post balanced double-entry within same DB transaction ---
	if err := r.postDepositLedgerTx(ctx, tx, vaultID, record.UserID, record.Amount, record.TransactionHash); err != nil {
		if !isLedgerTableMissing(err) {
			return balanceaudit.Entry{}, fmt.Errorf("ledger deposit posting failed: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return balanceaudit.Entry{}, err
	}
	return inserted, nil
}

// RecordWithdrawalWithAudit performs the balance debit and the balance-audit
// ledger append as one atomic transaction (nester#1124 durability finding):
// either both commit, or neither does. auditTemplate must have
// VaultID/BalanceBefore/BalanceAfter unset — they are filled in here.
func (r *VaultRepository) RecordWithdrawalWithAudit(
	ctx context.Context,
	vaultID uuid.UUID,
	record vault.TransactionRecord,
	auditTemplate balanceaudit.Entry,
) (balanceaudit.Entry, error) {
	if record.Amount.Cmp(decimal.Zero) <= 0 {
		return balanceaudit.Entry{}, vault.ErrInvalidAmount
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return balanceaudit.Entry{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var before decimal.Decimal
	if err := tx.QueryRowContext(ctx,
		`SELECT current_balance FROM vaults WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`,
		vaultID.String(),
	).Scan(&before); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return balanceaudit.Entry{}, vault.ErrVaultNotFound
		}
		return balanceaudit.Entry{}, err
	}
	if before.LessThan(record.Amount) {
		return balanceaudit.Entry{}, vault.ErrWithdrawalExceedsPosition
	}

	result, err := tx.ExecContext(
		ctx,
		`UPDATE vaults
		 SET current_balance = current_balance - $2::numeric,
		     updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`,
		vaultID.String(),
		record.Amount.String(),
	)
	if err != nil {
		return balanceaudit.Entry{}, mapRepositoryError(err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return balanceaudit.Entry{}, err
	}
	if rowsAffected == 0 {
		return balanceaudit.Entry{}, vault.ErrVaultNotFound
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO vault_transactions (
			vault_id, user_id, type, amount, transaction_hash,
			shares_minted_or_burned, share_price_at_time, fee_charged
		) VALUES ($1, $2, 'withdrawal', $3::numeric, NULLIF($4, ''), $5::numeric, $6::numeric, $7::numeric)`,
		vaultID.String(),
		record.UserID.String(),
		record.Amount.String(),
		record.TransactionHash,
		record.SharesMintedOrBurned.String(),
		record.SharePriceAtTime.String(),
		record.FeeCharged.String(),
	); err != nil {
		return balanceaudit.Entry{}, mapRepositoryError(err)
	}

	auditTemplate.VaultID = vaultID
	auditTemplate.BalanceBefore = before
	auditTemplate.BalanceAfter = before.Sub(record.Amount)
	inserted, err := appendBalanceAuditEntry(ctx, tx, auditTemplate)
	if err != nil {
		return balanceaudit.Entry{}, err
	}

	// --- Ledger: atomic posting ---
	if err := r.postWithdrawalLedgerTx(ctx, tx, vaultID, record.UserID, record.Amount, record.TransactionHash); err != nil {
		if !isLedgerTableMissing(err) {
			return balanceaudit.Entry{}, fmt.Errorf("ledger withdrawal posting failed: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return balanceaudit.Entry{}, err
	}
	return inserted, nil
}

// RecordHarvestWithAudit applies the same post-harvest balance update as
// RecordHarvest, but appends the balance-audit ledger entry inside the same
// transaction (nester#1124 durability finding) instead of as a separate
// best-effort step afterward. auditTemplate must have
// VaultID/BalanceBefore/BalanceAfter unset — they are filled in here.
func (r *VaultRepository) RecordHarvestWithAudit(
	ctx context.Context,
	input vault.HarvestRecordInput,
	auditTemplate balanceaudit.Entry,
) (balanceaudit.Entry, error) {
	if input.NetYield.Cmp(decimal.Zero) < 0 || input.PerformanceFee.Cmp(decimal.Zero) < 0 {
		return balanceaudit.Entry{}, vault.ErrInvalidAmount
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return balanceaudit.Entry{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var before decimal.Decimal
	if err := tx.QueryRowContext(ctx,
		`SELECT current_balance FROM vaults WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`,
		input.VaultID.String(),
	).Scan(&before); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return balanceaudit.Entry{}, vault.ErrVaultNotFound
		}
		return balanceaudit.Entry{}, err
	}

	var after decimal.Decimal
	if input.Compounded {
		after = before.Add(input.NetYield)
		result, err := tx.ExecContext(
			ctx,
			`UPDATE vaults
			 SET total_deposited = total_deposited + $2::numeric,
			     current_balance = current_balance + $2::numeric,
			     yield_earned = GREATEST(yield_earned - ($2::numeric + $3::numeric), 0),
			     fees_paid = fees_paid + $3::numeric,
			     last_harvested_at = NOW(),
			     updated_at = NOW()
			 WHERE id = $1 AND deleted_at IS NULL`,
			input.VaultID.String(),
			input.NetYield.String(),
			input.PerformanceFee.String(),
		)
		if err != nil {
			return balanceaudit.Entry{}, mapRepositoryError(err)
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return balanceaudit.Entry{}, err
		}
		if rowsAffected == 0 {
			return balanceaudit.Entry{}, vault.ErrVaultNotFound
		}
	} else {
		after = before.Sub(input.NetYield)
		result, err := tx.ExecContext(
			ctx,
			`UPDATE vaults
			 SET current_balance = current_balance - $2::numeric,
			     yield_earned = GREATEST(yield_earned - ($2::numeric + $3::numeric), 0),
			     fees_paid = fees_paid + $3::numeric,
			     last_harvested_at = NOW(),
			     updated_at = NOW()
			 WHERE id = $1 AND deleted_at IS NULL`,
			input.VaultID.String(),
			input.NetYield.String(),
			input.PerformanceFee.String(),
		)
		if err != nil {
			return balanceaudit.Entry{}, mapRepositoryError(err)
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return balanceaudit.Entry{}, err
		}
		if rowsAffected == 0 {
			return balanceaudit.Entry{}, vault.ErrVaultNotFound
		}
	}

	var sharesArg any
	if input.NewSharesMinted != nil {
		sharesArg = input.NewSharesMinted.String()
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO vault_transactions (
			vault_id, user_id, type, amount, transaction_hash, shares_minted_or_burned, fee_charged
		) VALUES ($1, $2, 'harvest', $3::numeric, NULLIF($4, ''), $5::numeric, $6::numeric)`,
		input.VaultID.String(),
		input.UserID.String(),
		input.NetYield.String(),
		input.TransactionHash,
		sharesArg,
		input.PerformanceFee.String(),
	); err != nil {
		return balanceaudit.Entry{}, mapRepositoryError(err)
	}

	auditTemplate.VaultID = input.VaultID
	auditTemplate.BalanceBefore = before
	auditTemplate.BalanceAfter = after
	inserted, err := appendBalanceAuditEntry(ctx, tx, auditTemplate)
	if err != nil {
		return balanceaudit.Entry{}, err
	}

	// --- Ledger: atomic harvest posting (gross = net + fee) ---
	if err := r.postHarvestLedgerTx(ctx, tx, input.VaultID, input.UserID, input.NetYield, input.PerformanceFee, "blend", input.TransactionHash); err != nil {
		if !isLedgerTableMissing(err) {
			return balanceaudit.Entry{}, fmt.Errorf("ledger harvest posting failed: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return balanceaudit.Entry{}, err
	}
	return inserted, nil
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

// UpdateHarvestFrequency sets the vault's harvest cadence, used by the harvest
// engine to gate how often it considers this vault for a harvest (#940).
func (r *VaultRepository) UpdateHarvestFrequency(ctx context.Context, id uuid.UUID, frequency string) error {
	result, err := r.db.ExecContext(
		ctx,
		`UPDATE vaults
		 SET harvest_frequency = $2, updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`,
		id.String(),
		frequency,
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
//
// Serialisation (nester#1084): the position row is locked with
// SELECT ... FOR UPDATE and the sufficient-funds check re-runs under that
// lock. Two concurrent withdrawals therefore cannot both read the same
// pre-withdrawal balance and both pass — the second waits on the row lock and
// re-checks against the post-withdrawal balance. The service-layer check
// stays as a fast-fail before any on-chain submit; this one is authoritative.
//
// Lock ordering (deadlock safety): every money-path write — RecordDeposit,
// RecordWithdrawal, RecordHarvest, applyConfirmedBalanceChange — locks
// exactly one vaults row first (the deposit path's single atomic UPDATE takes
// the same row lock, an equivalent serialisation) and only then inserts into
// vault_transactions. No money path locks a second vaults row or takes the
// two in the other order, so deposit and withdrawal cannot deadlock; they
// queue on the same row lock. The lock spans only the statements below — no
// network or chain I/O happens inside the transaction.
func (r *VaultRepository) RecordWithdrawal(ctx context.Context, id uuid.UUID, record vault.TransactionRecord) error {
	if record.Amount.Cmp(decimal.Zero) <= 0 {
		return vault.ErrInvalidAmount
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var rawBalance string
	if err := tx.QueryRowContext(
		ctx,
		`SELECT current_balance FROM vaults WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`,
		id.String(),
	).Scan(&rawBalance); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return vault.ErrVaultNotFound
		}
		return mapRepositoryError(err)
	}

	balance, err := decimal.NewFromString(rawBalance)
	if err != nil {
		return fmt.Errorf("parse current balance: %w", err)
	}
	if balance.LessThan(record.Amount) {
		return vault.ErrWithdrawalExceedsPosition
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE vaults
		 SET current_balance = current_balance - $2::numeric,
		     updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`,
		id.String(),
		record.Amount.String(),
	); err != nil {
		return mapRepositoryError(err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO vault_transactions (
			vault_id, user_id, type, amount, transaction_hash,
			shares_minted_or_burned, share_price_at_time, fee_charged
		) VALUES ($1, $2, 'withdrawal', $3::numeric, NULLIF($4, ''), $5::numeric, $6::numeric, $7::numeric)`,
		id.String(),
		record.UserID.String(),
		record.Amount.String(),
		record.TransactionHash,
		record.SharesMintedOrBurned.String(),
		record.SharePriceAtTime.String(),
		record.FeeCharged.String(),
	); err != nil {
		return mapRepositoryError(err)
	}

	// --- Ledger: atomic posting ---
	if err := r.postWithdrawalLedgerTx(ctx, tx, id, record.UserID, record.Amount, record.TransactionHash); err != nil {
		if !isLedgerTableMissing(err) {
			return fmt.Errorf("ledger withdrawal posting failed: %w", err)
		}
	}

	return tx.Commit()
}

// RecordHarvest applies post-harvest balance updates and writes a ledger entry.
// It now also posts balanced double-entry ledger entries atomically.
func (r *VaultRepository) RecordHarvest(ctx context.Context, input vault.HarvestRecordInput) error {
	if input.NetYield.Cmp(decimal.Zero) < 0 || input.PerformanceFee.Cmp(decimal.Zero) < 0 {
		return vault.ErrInvalidAmount
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if input.Compounded {
		result, err := tx.ExecContext(
			ctx,
			`UPDATE vaults
			 SET total_deposited = total_deposited + $2::numeric,
			     current_balance = current_balance + $2::numeric,
			     yield_earned = GREATEST(yield_earned - ($2::numeric + $3::numeric), 0),
			     fees_paid = fees_paid + $3::numeric,
			     last_harvested_at = NOW(),
			     updated_at = NOW()
			 WHERE id = $1 AND deleted_at IS NULL`,
			input.VaultID.String(),
			input.NetYield.String(),
			input.PerformanceFee.String(),
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
	} else {
		result, err := tx.ExecContext(
			ctx,
			`UPDATE vaults
			 SET current_balance = current_balance - $2::numeric,
			     yield_earned = GREATEST(yield_earned - ($2::numeric + $3::numeric), 0),
			     fees_paid = fees_paid + $3::numeric,
			     last_harvested_at = NOW(),
			     updated_at = NOW()
			 WHERE id = $1 AND deleted_at IS NULL`,
			input.VaultID.String(),
			input.NetYield.String(),
			input.PerformanceFee.String(),
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
	}

	var sharesArg any
	if input.NewSharesMinted != nil {
		sharesArg = input.NewSharesMinted.String()
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO vault_transactions (
			vault_id, user_id, type, amount, transaction_hash, shares_minted_or_burned, fee_charged
		) VALUES ($1, $2, 'harvest', $3::numeric, NULLIF($4, ''), $5::numeric, $6::numeric)`,
		input.VaultID.String(),
		input.UserID.String(),
		input.NetYield.String(),
		input.TransactionHash,
		sharesArg,
		input.PerformanceFee.String(),
	); err != nil {
		return mapRepositoryError(err)
	}

	// --- Ledger: atomic harvest posting (gross = net + fee) ---
	// Uses default adapter name 'unknown' if not specified; yield_source per adapter.
	if err := r.postHarvestLedgerTx(ctx, tx, input.VaultID, input.UserID, input.NetYield, input.PerformanceFee, "blend", input.TransactionHash); err != nil {
		if !isLedgerTableMissing(err) {
			return fmt.Errorf("ledger harvest posting failed: %w", err)
		}
	}

	return tx.Commit()
}

// ApplyConfirmedDeposit credits a vault's balance for a deposit that has been
// confirmed on-chain. It is keyed by the Stellar transaction hash and is
// idempotent: a second call with the same hash is a no-op (no balance change,
// no duplicate ledger row), so the auto-confirmation worker can safely retry.
// This is the only path that credits balance from a confirmed deposit —
// balance is never moved at submission time.
//
// It shares the atomic balance+audit pattern established by
// RecordDepositWithAudit (see that doc comment): the credit and the
// balance-audit append happen in the same DB transaction, so a confirmed
// deposit always produces exactly one matching audit entry (nester CodeRabbit
// finding: confirmed deposits/withdrawals previously updated balance without
// ever appending an audit entry).
//
// capCheck, when non-nil, is evaluated under the same advisory locks used by
// RecordDepositWithAudit, but its result is never used to refuse the credit:
// unlike an interactive deposit, this money has already moved on-chain by the
// time this is called, so there is nothing left to safely reject — refusing
// to record it would only make our balance diverge from the chain. Instead, a
// cap-exceeded result from capCheck is returned as capWarning so the caller
// can log/alert on it; the credit and its audit entry still commit
// unconditionally (nester CodeRabbit finding: launch caps were not evaluated
// at all on this path).
func (r *VaultRepository) ApplyConfirmedDeposit(
	ctx context.Context,
	id uuid.UUID,
	userID uuid.UUID,
	amount decimal.Decimal,
	txHash string,
	capCheck func(ctx context.Context, currentUserTotal, currentGlobalTotal decimal.Decimal) error,
) (capWarning error, err error) {
	return r.applyConfirmedBalanceChange(ctx, id, userID, amount, txHash, "deposit", capCheck)
}

// ApplyConfirmedWithdrawal debits a vault's balance for a withdrawal confirmed
// on-chain. Idempotent on txHash, mirroring ApplyConfirmedDeposit, and appends
// a matching audit entry in the same transaction as the debit. Withdrawals
// never push a total over a launch cap, so there is no cap check here.
func (r *VaultRepository) ApplyConfirmedWithdrawal(ctx context.Context, id uuid.UUID, userID uuid.UUID, amount decimal.Decimal, txHash string) error {
	_, err := r.applyConfirmedBalanceChange(ctx, id, userID, amount, txHash, "withdrawal", nil)
	return err
}

func (r *VaultRepository) applyConfirmedBalanceChange(
	ctx context.Context,
	id uuid.UUID,
	userID uuid.UUID,
	amount decimal.Decimal,
	txHash, txType string,
	capCheck func(ctx context.Context, currentUserTotal, currentGlobalTotal decimal.Decimal) error,
) (capWarning error, err error) {
	if amount.Cmp(decimal.Zero) <= 0 {
		return nil, vault.ErrInvalidAmount
	}
	if strings.TrimSpace(txHash) == "" {
		// Without a hash we cannot dedupe, and a confirmed on-chain change
		// always has one. Refuse rather than risk a double credit.
		return nil, vault.ErrInvalidVault
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// Claim the hash first. If another worker (or an earlier retry) already
	// applied this transaction, the insert affects zero rows and we leave the
	// balance untouched.
	var userIDArg any
	if userID != uuid.Nil {
		userIDArg = userID.String()
	}
	ledgerRow, err := tx.ExecContext(
		ctx,
		`INSERT INTO vault_transactions (vault_id, user_id, type, amount, transaction_hash)
		 VALUES ($1, $2, $3, $4::numeric, $5)
		 ON CONFLICT (transaction_hash) DO NOTHING`,
		id.String(),
		userIDArg,
		txType,
		amount.String(),
		txHash,
	)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	inserted, err := ledgerRow.RowsAffected()
	if err != nil {
		return nil, err
	}
	if inserted == 0 {
		// Already applied for this hash — idempotent no-op.
		return nil, tx.Commit()
	}

	var before decimal.Decimal
	if err := tx.QueryRowContext(ctx,
		`SELECT current_balance FROM vaults WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`,
		id.String(),
	).Scan(&before); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, vault.ErrVaultNotFound
		}
		return nil, err
	}

	if txType == "deposit" && capCheck != nil && userID != uuid.Nil {
		// Same advisory-lock pattern as RecordDepositWithAudit: cheap,
		// released on commit/rollback, and serializes concurrent confirmed
		// deposits (and interactive ones, which lock the same keys) so the
		// totals capCheck sees reflect every other in-flight deposit.
		if _, err := tx.ExecContext(ctx,
			`SELECT pg_advisory_xact_lock(hashtext('nester:launch_cap:user:' || $1::text))`,
			userID.String(),
		); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx,
			`SELECT pg_advisory_xact_lock(hashtext('nester:launch_cap:global'))`,
		); err != nil {
			return nil, err
		}

		var userTotal, globalTotal decimal.Decimal
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(current_balance), 0) FROM vaults WHERE user_id = $1 AND deleted_at IS NULL`,
			userID.String(),
		).Scan(&userTotal); err != nil {
			return nil, err
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(current_balance), 0) FROM vaults WHERE deleted_at IS NULL`,
		).Scan(&globalTotal); err != nil {
			return nil, err
		}

		// Deliberately not returned as err: see the ApplyConfirmedDeposit doc
		// comment on why an already-confirmed on-chain deposit is credited
		// regardless of the outcome here.
		capWarning = capCheck(ctx, userTotal, globalTotal)
	}

	var balanceSQL string
	if txType == "deposit" {
		balanceSQL = `UPDATE vaults
			 SET total_deposited = total_deposited + $2::numeric,
			     current_balance = current_balance + $2::numeric,
			     updated_at = NOW()
			 WHERE id = $1 AND deleted_at IS NULL`
	} else {
		balanceSQL = `UPDATE vaults
			 SET current_balance = current_balance - $2::numeric,
			     updated_at = NOW()
			 WHERE id = $1 AND deleted_at IS NULL`
	}

	result, err := tx.ExecContext(ctx, balanceSQL, id.String(), amount.String())
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rowsAffected == 0 {
		return nil, vault.ErrVaultNotFound
	}

	var after decimal.Decimal
	if txType == "deposit" {
		after = before.Add(amount)
	} else {
		after = before.Sub(amount)
	}

	auditOp := balanceaudit.OperationDeposit
	if txType != "deposit" {
		auditOp = balanceaudit.OperationWithdrawal
	}
	actor := balanceaudit.SystemActor("chain_confirmation")
	if userID != uuid.Nil {
		actor = userID.String()
	}
	if _, err := appendBalanceAuditEntry(ctx, tx, balanceaudit.Entry{
		VaultID:        id,
		UserID:         userID,
		Actor:          actor,
		Operation:      auditOp,
		Amount:         amount,
		BalanceBefore:  before,
		BalanceAfter:   after,
		ChainReference: txHash,
	}); err != nil {
		return nil, err
	}

	// --- Ledger: post confirmation as deposit/withdrawal ---
	if userID != uuid.Nil {
		if txType == "deposit" {
			if err := r.postDepositLedgerTx(ctx, tx, id, userID, amount, txHash); err != nil {
				if !isLedgerTableMissing(err) {
					return nil, fmt.Errorf("ledger confirmed deposit posting failed: %w", err)
				}
			}
		} else {
			if err := r.postWithdrawalLedgerTx(ctx, tx, id, userID, amount, txHash); err != nil {
				if !isLedgerTableMissing(err) {
					return nil, fmt.Errorf("ledger confirmed withdrawal posting failed: %w", err)
				}
			}
		}
	}

	return capWarning, tx.Commit()
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

// ListUserVaultTransactions returns all deposit and withdrawal rows for a user in a vault.
func (r *VaultRepository) RecordRebalance(ctx context.Context, input vault.RebalanceRecordInput, withdrawRecord, depositRecord vault.TransactionRecord) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE vaults
		 SET current_balance = current_balance - $2::numeric,
		     updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`,
		input.VaultID.String(),
		withdrawRecord.Amount.String(),
	); err != nil {
		return mapRepositoryError(err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO vault_transactions (
			vault_id, user_id, type, amount, transaction_hash,
			shares_minted_or_burned, share_price_at_time, fee_charged
		) VALUES ($1, $2, 'withdrawal', $3::numeric, NULLIF($4, ''), $5::numeric, $6::numeric, $7::numeric)`,
		input.VaultID.String(),
		withdrawRecord.UserID.String(),
		withdrawRecord.Amount.String(),
		withdrawRecord.TransactionHash,
		withdrawRecord.SharesMintedOrBurned.String(),
		withdrawRecord.SharePriceAtTime.String(),
		withdrawRecord.FeeCharged.String(),
	); err != nil {
		return mapRepositoryError(err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE vaults
		 SET current_balance = current_balance + $2::numeric,
		     total_deposited = total_deposited + $2::numeric,
		     updated_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL`,
		input.VaultID.String(),
		depositRecord.Amount.String(),
	); err != nil {
		return mapRepositoryError(err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO vault_transactions (
			vault_id, user_id, type, amount, transaction_hash,
			shares_minted_or_burned, share_price_at_time, fee_charged
		) VALUES ($1, $2, 'deposit', $3::numeric, NULLIF($4, ''), $5::numeric, $6::numeric, $7::numeric)`,
		input.VaultID.String(),
		depositRecord.UserID.String(),
		depositRecord.Amount.String(),
		depositRecord.TransactionHash,
		depositRecord.SharesMintedOrBurned.String(),
		depositRecord.SharePriceAtTime.String(),
		depositRecord.FeeCharged.String(),
	); err != nil {
		return mapRepositoryError(err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO vault_transactions (
			vault_id, user_id, type, amount, transaction_hash
		) VALUES ($1, $2, 'rebalance', $3::numeric, NULLIF($4, ''))`,
		input.VaultID.String(),
		input.UserID.String(),
		input.Amount.String(),
		input.TransactionHash,
	); err != nil {
		return mapRepositoryError(err)
	}

	// --- Ledger: post rebalance movement between yield sources ---
	if err := r.postRebalanceLedgerTx(ctx, tx, input.VaultID, input.FromProtocol, input.ToProtocol, input.Amount, input.TransactionHash); err != nil {
		if !isLedgerTableMissing(err) {
			return fmt.Errorf("ledger rebalance posting failed: %w", err)
		}
	}

	return tx.Commit()
}

func (r *VaultRepository) ListUserVaultTransactions(ctx context.Context, userID uuid.UUID, vaultID uuid.UUID) ([]vault.VaultTransaction, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, vault_id, user_id, type, amount, COALESCE(transaction_hash, ''), shares_minted_or_burned, share_price_at_time, fee_charged, created_at
		 FROM vault_transactions
		 WHERE vault_id = $1 AND user_id = $2
		 ORDER BY created_at ASC`,
		vaultID.String(),
		userID.String(),
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
		id               string
		userID           string
		totalDeposited   string
		currentBalance   string
		contractAddress  string
		currency         string
		status           string
		yieldEarned      string
		feesPaid         string
		harvestFrequency string
		lastHarvestedAt  sql.NullTime
		lastSyncedAt     sql.NullTime
		deletedAt        sql.NullTime
		createdAt        time.Time
		updatedAt        time.Time
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
		&harvestFrequency,
		&lastHarvestedAt,
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

	var lastHarvestedAtPtr *time.Time
	if lastHarvestedAt.Valid {
		t := lastHarvestedAt.Time
		lastHarvestedAtPtr = &t
	}

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
		ID:               parsedID,
		UserID:           parsedUserID,
		ContractAddress:  contractAddress,
		TotalDeposited:   parsedDeposited,
		CurrentBalance:   parsedBalance,
		Currency:         currency,
		Status:           vault.VaultStatus(status),
		YieldEarned:      parsedYield,
		FeesPaid:         parsedFees,
		HarvestFrequency: harvestFrequency,
		LastHarvestedAt:  lastHarvestedAtPtr,
		LastSyncedAt:     lastSyncedAtPtr,
		DeletedAt:        deletedAtPtr,
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
	}, nil
}

func scanVaultTransaction(row scanner) (vault.VaultTransaction, error) {
	var (
		id         string
		vaultID    string
		userID     sql.NullString
		txType     string
		amount     string
		txHash     string
		shares     sql.NullString
		sharePrice sql.NullString
		fee        sql.NullString
		createdAt  time.Time
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

// ── Ledger helpers — double-entry posting within same transaction ──────────
// These helpers make the ledger the source of truth and keep domain writes atomic.

const stroopsPerUSDC = int64(10_000_000)

func decimalToStroops(d decimal.Decimal) int64 {
	mult := d.Mul(decimal.NewFromInt(stroopsPerUSDC))
	return mult.Round(0).IntPart()
}

func isLedgerTableMissing(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "ledger_accounts") && strings.Contains(s, "does not exist") ||
		strings.Contains(s, "ledger_entries") && strings.Contains(s, "does not exist") ||
		strings.Contains(s, "ledger_balances") && strings.Contains(s, "does not exist") ||
		strings.Contains(s, "relation") && strings.Contains(s, "ledger") && strings.Contains(s, "does not exist")
}

// ledgerGetOrCreateAccountTx ensures a ledger account exists inside the provided tx.
func ledgerGetOrCreateAccountTx(ctx context.Context, tx *sql.Tx, accountType string, vaultID *uuid.UUID, userID *uuid.UUID, adapterName *string, assetCode string) (uuid.UUID, error) {
	if assetCode == "" {
		assetCode = "USDC"
	}
	var accountIDStr string
	// Try find existing
	switch accountType {
	case "user_vault_position":
		if vaultID == nil || userID == nil {
			return uuid.Nil, fmt.Errorf("vault_id and user_id required for user_vault_position")
		}
		err := tx.QueryRowContext(ctx, `
			SELECT id FROM ledger_accounts WHERE account_type=$1 AND vault_id=$2 AND user_id=$3 AND asset_code=$4 LIMIT 1
		`, accountType, vaultID.String(), userID.String(), assetCode).Scan(&accountIDStr)
		if err == nil {
			return uuid.Parse(accountIDStr)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, err
		}
	case "vault_asset_pool":
		if vaultID == nil {
			return uuid.Nil, fmt.Errorf("vault_id required")
		}
		err := tx.QueryRowContext(ctx, `
			SELECT id FROM ledger_accounts WHERE account_type=$1 AND vault_id=$2 AND asset_code=$3 LIMIT 1
		`, accountType, vaultID.String(), assetCode).Scan(&accountIDStr)
		if err == nil {
			return uuid.Parse(accountIDStr)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, err
		}
	case "yield_source":
		if adapterName == nil || *adapterName == "" {
			// default
			defaultAdapter := "blend"
			adapterName = &defaultAdapter
		}
		err := tx.QueryRowContext(ctx, `
			SELECT id FROM ledger_accounts WHERE account_type=$1 AND adapter_name=$2 AND asset_code=$3 LIMIT 1
		`, accountType, *adapterName, assetCode).Scan(&accountIDStr)
		if err == nil {
			return uuid.Parse(accountIDStr)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, err
		}
	case "fee_account", "treasury", "system_suspense", "external":
		err := tx.QueryRowContext(ctx, `
			SELECT id FROM ledger_accounts WHERE account_type=$1 AND asset_code=$2 LIMIT 1
		`, accountType, assetCode).Scan(&accountIDStr)
		if err == nil {
			return uuid.Parse(accountIDStr)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, err
		}
	case "penalty_escrow":
		if vaultID == nil {
			return uuid.Nil, fmt.Errorf("vault_id required for penalty_escrow")
		}
		err := tx.QueryRowContext(ctx, `
			SELECT id FROM ledger_accounts WHERE account_type=$1 AND vault_id=$2 AND asset_code=$3 LIMIT 1
		`, accountType, vaultID.String(), assetCode).Scan(&accountIDStr)
		if err == nil {
			return uuid.Parse(accountIDStr)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, err
		}
	default:
		return uuid.Nil, fmt.Errorf("unknown account_type %s", accountType)
	}
	// Create new
	newID := uuid.New()
	var vaultStr *string
	if vaultID != nil {
		s := vaultID.String()
		vaultStr = &s
	}
	var userStr *string
	if userID != nil {
		s := userID.String()
		userStr = &s
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO ledger_accounts (id, account_type, vault_id, user_id, adapter_name, asset_code, asset_unit, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,'stroops',NOW(),NOW())
		ON CONFLICT DO NOTHING
	`, newID.String(), accountType, vaultStr, userStr, adapterName, assetCode)
	if err != nil {
		return uuid.Nil, err
	}
	// Re-select to handle race
	switch accountType {
	case "user_vault_position":
		err = tx.QueryRowContext(ctx, `
			SELECT id FROM ledger_accounts WHERE account_type=$1 AND vault_id=$2 AND user_id=$3 AND asset_code=$4 LIMIT 1
		`, accountType, vaultID.String(), userID.String(), assetCode).Scan(&accountIDStr)
	case "vault_asset_pool":
		err = tx.QueryRowContext(ctx, `
			SELECT id FROM ledger_accounts WHERE account_type=$1 AND vault_id=$2 AND asset_code=$3 LIMIT 1
		`, accountType, vaultID.String(), assetCode).Scan(&accountIDStr)
	case "yield_source":
		err = tx.QueryRowContext(ctx, `
			SELECT id FROM ledger_accounts WHERE account_type=$1 AND adapter_name=$2 AND asset_code=$3 LIMIT 1
		`, accountType, *adapterName, assetCode).Scan(&accountIDStr)
	case "fee_account", "treasury", "system_suspense", "external":
		err = tx.QueryRowContext(ctx, `
			SELECT id FROM ledger_accounts WHERE account_type=$1 AND asset_code=$2 LIMIT 1
		`, accountType, assetCode).Scan(&accountIDStr)
	case "penalty_escrow":
		err = tx.QueryRowContext(ctx, `
			SELECT id FROM ledger_accounts WHERE account_type=$1 AND vault_id=$2 AND asset_code=$3 LIMIT 1
		`, accountType, vaultID.String(), assetCode).Scan(&accountIDStr)
	}
	if err != nil {
		// fallback to newID if selection fails (should not)
		return newID, nil
	}
	return uuid.Parse(accountIDStr)
}

// ledgerPostEntriesTx inserts balanced entries and updates ledger_balances atomically.
// entries must sum to zero, caller ensures >=2.
func ledgerPostEntriesTx(ctx context.Context, tx *sql.Tx, entries []struct {
	AccountID       uuid.UUID
	Amount          int64
	DomainEventType string
	DomainEventID   string
}) error {
	if len(entries) < 2 {
		return fmt.Errorf("at least two ledger entries required")
	}
	var sum int64
	for _, e := range entries {
		sum += e.Amount
		if e.Amount == 0 {
			return fmt.Errorf("ledger amount must be non-zero")
		}
	}
	if sum != 0 {
		return fmt.Errorf("ledger entries unbalanced: sum=%d", sum)
	}
	txID := uuid.New()
	for _, e := range entries {
		dir := "debit"
		if e.Amount < 0 {
			dir = "credit"
		}
		entryID := uuid.New()
		_, err := tx.ExecContext(ctx, `
			INSERT INTO ledger_entries (id, transaction_id, account_id, amount, direction, created_at, domain_event_type, domain_event_id, asset_code, asset_unit)
			VALUES ($1,$2,$3,$4,$5,NOW(),$6,$7,'USDC','stroops')
		`, entryID.String(), txID.String(), e.AccountID.String(), e.Amount, dir, e.DomainEventType, e.DomainEventID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO ledger_balances (account_id, balance, asset_code, asset_unit, updated_at, version)
			VALUES ($1,$2,'USDC','stroops',NOW(),1)
			ON CONFLICT (account_id) DO UPDATE SET
				balance = ledger_balances.balance + EXCLUDED.balance,
				updated_at = NOW(),
				version = ledger_balances.version + 1
		`, e.AccountID.String(), e.Amount)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *VaultRepository) postDepositLedgerTx(ctx context.Context, tx *sql.Tx, vaultID, userID uuid.UUID, amount decimal.Decimal, domainEventID string) error {
	stroops := decimalToStroops(amount)
	if stroops <= 0 {
		return nil
	}
	userAccID, err := ledgerGetOrCreateAccountTx(ctx, tx, "user_vault_position", &vaultID, &userID, nil, "USDC")
	if err != nil {
		return err
	}
	vaultAccID, err := ledgerGetOrCreateAccountTx(ctx, tx, "vault_asset_pool", &vaultID, nil, nil, "USDC")
	if err != nil {
		return err
	}
	suspenseID, err := ledgerGetOrCreateAccountTx(ctx, tx, "system_suspense", nil, nil, nil, "USDC")
	if err != nil {
		return err
	}
	return ledgerPostEntriesTx(ctx, tx, []struct {
		AccountID       uuid.UUID
		Amount          int64
		DomainEventType string
		DomainEventID   string
	}{
		{AccountID: userAccID, Amount: stroops, DomainEventType: "deposit", DomainEventID: domainEventID},
		{AccountID: vaultAccID, Amount: stroops, DomainEventType: "deposit", DomainEventID: domainEventID},
		{AccountID: suspenseID, Amount: -2 * stroops, DomainEventType: "deposit", DomainEventID: domainEventID},
	})
}

func (r *VaultRepository) postWithdrawalLedgerTx(ctx context.Context, tx *sql.Tx, vaultID, userID uuid.UUID, amount decimal.Decimal, domainEventID string) error {
	stroops := decimalToStroops(amount)
	if stroops <= 0 {
		return nil
	}
	userAccID, err := ledgerGetOrCreateAccountTx(ctx, tx, "user_vault_position", &vaultID, &userID, nil, "USDC")
	if err != nil {
		return err
	}
	vaultAccID, err := ledgerGetOrCreateAccountTx(ctx, tx, "vault_asset_pool", &vaultID, nil, nil, "USDC")
	if err != nil {
		return err
	}
	suspenseID, err := ledgerGetOrCreateAccountTx(ctx, tx, "system_suspense", nil, nil, nil, "USDC")
	if err != nil {
		return err
	}
	return ledgerPostEntriesTx(ctx, tx, []struct {
		AccountID       uuid.UUID
		Amount          int64
		DomainEventType string
		DomainEventID   string
	}{
		{AccountID: userAccID, Amount: -stroops, DomainEventType: "withdraw", DomainEventID: domainEventID},
		{AccountID: vaultAccID, Amount: -stroops, DomainEventType: "withdraw", DomainEventID: domainEventID},
		{AccountID: suspenseID, Amount: 2 * stroops, DomainEventType: "withdraw", DomainEventID: domainEventID},
	})
}

func (r *VaultRepository) postHarvestLedgerTx(ctx context.Context, tx *sql.Tx, vaultID, userID uuid.UUID, netYield, perfFee decimal.Decimal, adapterName, domainEventID string) error {
	netStroops := decimalToStroops(netYield)
	feeStroops := decimalToStroops(perfFee)
	if netStroops <= 0 && feeStroops <= 0 {
		return nil
	}
	grossStroops := netStroops + feeStroops
	vaultAccID, err := ledgerGetOrCreateAccountTx(ctx, tx, "vault_asset_pool", &vaultID, nil, nil, "USDC")
	if err != nil {
		return err
	}
	userAccID, err := ledgerGetOrCreateAccountTx(ctx, tx, "user_vault_position", &vaultID, &userID, nil, "USDC")
	if err != nil {
		return err
	}
	feeAccID, err := ledgerGetOrCreateAccountTx(ctx, tx, "fee_account", nil, nil, nil, "USDC")
	if err != nil {
		return err
	}
	yieldAccID, err := ledgerGetOrCreateAccountTx(ctx, tx, "yield_source", nil, nil, &adapterName, "USDC")
	if err != nil {
		return err
	}
	suspenseID, err := ledgerGetOrCreateAccountTx(ctx, tx, "system_suspense", nil, nil, nil, "USDC")
	if err != nil {
		return err
	}
	var entries []struct {
		AccountID       uuid.UUID
		Amount          int64
		DomainEventType string
		DomainEventID   string
	}
	if netStroops != 0 {
		entries = append(entries, struct {
			AccountID       uuid.UUID
			Amount          int64
			DomainEventType string
			DomainEventID   string
		}{AccountID: vaultAccID, Amount: netStroops, DomainEventType: "harvest", DomainEventID: domainEventID})
		entries = append(entries, struct {
			AccountID       uuid.UUID
			Amount          int64
			DomainEventType string
			DomainEventID   string
		}{AccountID: userAccID, Amount: netStroops, DomainEventType: "harvest", DomainEventID: domainEventID})
		entries = append(entries, struct {
			AccountID       uuid.UUID
			Amount          int64
			DomainEventType string
			DomainEventID   string
		}{AccountID: suspenseID, Amount: -netStroops, DomainEventType: "harvest", DomainEventID: domainEventID})
	}
	if feeStroops != 0 {
		entries = append(entries, struct {
			AccountID       uuid.UUID
			Amount          int64
			DomainEventType string
			DomainEventID   string
		}{AccountID: feeAccID, Amount: feeStroops, DomainEventType: "harvest", DomainEventID: domainEventID})
	}
	if grossStroops != 0 {
		entries = append(entries, struct {
			AccountID       uuid.UUID
			Amount          int64
			DomainEventType string
			DomainEventID   string
		}{AccountID: yieldAccID, Amount: -grossStroops, DomainEventType: "harvest", DomainEventID: domainEventID})
	}
	// Validate sum zero: net+net+fee -gross -net = net+fee-gross =0
	// Our entries: vault +net, user +net, suspense -net, fee +fee, yield -gross = net+net-fee? Actually net+net+fee -net -gross = net+fee-gross=0 -> balanced
	return ledgerPostEntriesTx(ctx, tx, entries)
}

func (r *VaultRepository) postRebalanceLedgerTx(ctx context.Context, tx *sql.Tx, vaultID uuid.UUID, fromProtocol, toProtocol string, amount decimal.Decimal, domainEventID string) error {
	stroops := decimalToStroops(amount)
	if stroops <= 0 {
		return nil
	}
	if fromProtocol == toProtocol {
		return nil
	}
	fromID, err := ledgerGetOrCreateAccountTx(ctx, tx, "yield_source", nil, nil, &fromProtocol, "USDC")
	if err != nil {
		return err
	}
	toID, err := ledgerGetOrCreateAccountTx(ctx, tx, "yield_source", nil, nil, &toProtocol, "USDC")
	if err != nil {
		return err
	}
	return ledgerPostEntriesTx(ctx, tx, []struct {
		AccountID       uuid.UUID
		Amount          int64
		DomainEventType string
		DomainEventID   string
	}{
		{AccountID: fromID, Amount: -stroops, DomainEventType: "rebalance", DomainEventID: domainEventID},
		{AccountID: toID, Amount: stroops, DomainEventType: "rebalance", DomainEventID: domainEventID},
	})
}

// UserDepositTotal returns the sum of current_balance across every
// non-deleted vault owned by userID. Implements caps.Totals for the
// per-user deposit cap (nester#1119).
func (r *VaultRepository) UserDepositTotal(ctx context.Context, userID uuid.UUID) (decimal.Decimal, error) {
	const query = `
		SELECT COALESCE(SUM(current_balance), 0)
		FROM vaults
		WHERE user_id = $1 AND deleted_at IS NULL
	`
	var total decimal.Decimal
	if err := r.db.QueryRowContext(ctx, query, userID.String()).Scan(&total); err != nil {
		return decimal.Zero, err
	}
	return total, nil
}

// GlobalDepositTotal returns protocol-wide TVL: the sum of current_balance
// across every non-deleted vault. Implements caps.Totals for the global TVL
// cap (nester#1119).
func (r *VaultRepository) GlobalDepositTotal(ctx context.Context) (decimal.Decimal, error) {
	const query = `SELECT COALESCE(SUM(current_balance), 0) FROM vaults WHERE deleted_at IS NULL`
	var total decimal.Decimal
	if err := r.db.QueryRowContext(ctx, query).Scan(&total); err != nil {
		return decimal.Zero, err
	}
	return total, nil
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
		if pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "transaction_hash") {
			return vault.ErrDuplicateTransaction
		}
		// uq_vaults_contract_address_live (migration 104). Checked before the
		// generic 23505 fallthrough so a duplicate registration is a clear
		// client error rather than an opaque 500 (nester#1148).
		if pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "vaults_contract_address") {
			return vault.ErrContractAddressRegistered
		}
	}

	return err
}
