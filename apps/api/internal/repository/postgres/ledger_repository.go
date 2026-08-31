package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/ledger"
)

type LedgerRepository struct {
	db *sql.DB
}

func NewLedgerRepository(db *sql.DB) *LedgerRepository {
	return &LedgerRepository{db: db}
}

// CreateAccount creates a ledger account.
func (r *LedgerRepository) CreateAccount(ctx context.Context, acc ledger.Account) (ledger.Account, error) {
	if err := ledger.ValidateAccountType(acc.AccountType); err != nil {
		return ledger.Account{}, err
	}
	if acc.ID == uuid.Nil {
		acc.ID = uuid.New()
	}
	if acc.AssetCode == "" {
		acc.AssetCode = "USDC"
	}
	if acc.AssetUnit == "" {
		acc.AssetUnit = "stroops"
	}
	now := time.Now().UTC()
	query := `
		INSERT INTO ledger_accounts (id, account_type, vault_id, user_id, adapter_name, asset_code, asset_unit, description, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT DO NOTHING
		RETURNING created_at, updated_at
	`
	var vaultID *string
	if acc.VaultID != nil {
		s := acc.VaultID.String()
		vaultID = &s
	}
	var userID *string
	if acc.UserID != nil {
		s := acc.UserID.String()
		userID = &s
	}
	err := r.db.QueryRowContext(ctx, query,
		acc.ID.String(),
		acc.AccountType,
		vaultID,
		userID,
		acc.AdapterName,
		acc.AssetCode,
		acc.AssetUnit,
		acc.Description,
		now,
		now,
	).Scan(&acc.CreatedAt, &acc.UpdatedAt)
	if err != nil {
		// ON CONFLICT DO NOTHING may return no row, fallback to get
		if errors.Is(err, sql.ErrNoRows) {
			return r.GetOrCreateAccount(ctx, acc.AccountType, acc.VaultID, acc.UserID, acc.AdapterName, acc.AssetCode)
		}
		return ledger.Account{}, err
	}
	return acc, nil
}

func (r *LedgerRepository) GetAccount(ctx context.Context, id uuid.UUID) (ledger.Account, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, account_type, vault_id, user_id, adapter_name, asset_code, asset_unit, COALESCE(description,''), created_at, updated_at
		FROM ledger_accounts WHERE id = $1
	`, id.String())
	return scanLedgerAccount(row)
}

// GetOrCreateAccount finds or creates an account using the repo's DB (own transaction).
func (r *LedgerRepository) GetOrCreateAccount(ctx context.Context, accountType string, vaultID *uuid.UUID, userID *uuid.UUID, adapterName *string, assetCode string) (ledger.Account, error) {
	if assetCode == "" {
		assetCode = "USDC"
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ledger.Account{}, err
	}
	defer func() { _ = tx.Rollback() }()
	acc, err := r.getOrCreateAccountTx(ctx, tx, accountType, vaultID, userID, adapterName, assetCode)
	if err != nil {
		return ledger.Account{}, err
	}
	if err := tx.Commit(); err != nil {
		return ledger.Account{}, err
	}
	return acc, nil
}

// GetOrCreateAccountTx is the same but uses an existing transaction handle — required for atomic posting.
func (r *LedgerRepository) GetOrCreateAccountTx(ctx context.Context, tx *sql.Tx, accountType string, vaultID *uuid.UUID, userID *uuid.UUID, adapterName *string, assetCode string) (ledger.Account, error) {
	if assetCode == "" {
		assetCode = "USDC"
	}
	return r.getOrCreateAccountTx(ctx, tx, accountType, vaultID, userID, adapterName, assetCode)
}

func (r *LedgerRepository) getOrCreateAccountTx(ctx context.Context, tx *sql.Tx, accountType string, vaultID *uuid.UUID, userID *uuid.UUID, adapterName *string, assetCode string) (ledger.Account, error) {
	if err := ledger.ValidateAccountType(accountType); err != nil {
		return ledger.Account{}, err
	}
	// Build SELECT based on type
	var row *sql.Row
	switch accountType {
	case ledger.AccountTypeUserVaultPosition:
		if vaultID == nil || userID == nil {
			return ledger.Account{}, errors.New("vault_id and user_id required for user_vault_position")
		}
		row = tx.QueryRowContext(ctx, `
			SELECT id, account_type, vault_id, user_id, adapter_name, asset_code, asset_unit, COALESCE(description,''), created_at, updated_at
			FROM ledger_accounts
			WHERE account_type = $1 AND vault_id = $2 AND user_id = $3 AND asset_code = $4
			LIMIT 1
		`, accountType, vaultID.String(), userID.String(), assetCode)
	case ledger.AccountTypeVaultAssetPool:
		if vaultID == nil {
			return ledger.Account{}, errors.New("vault_id required for vault_asset_pool")
		}
		row = tx.QueryRowContext(ctx, `
			SELECT id, account_type, vault_id, user_id, adapter_name, asset_code, asset_unit, COALESCE(description,''), created_at, updated_at
			FROM ledger_accounts
			WHERE account_type = $1 AND vault_id = $2 AND asset_code = $3
			LIMIT 1
		`, accountType, vaultID.String(), assetCode)
	case ledger.AccountTypeYieldSource:
		if adapterName == nil || *adapterName == "" {
			return ledger.Account{}, errors.New("adapter_name required for yield_source")
		}
		row = tx.QueryRowContext(ctx, `
			SELECT id, account_type, vault_id, user_id, adapter_name, asset_code, asset_unit, COALESCE(description,''), created_at, updated_at
			FROM ledger_accounts
			WHERE account_type = $1 AND adapter_name = $2 AND asset_code = $3
			LIMIT 1
		`, accountType, *adapterName, assetCode)
	case ledger.AccountTypeFee, ledger.AccountTypeTreasury, ledger.AccountTypeSystemSuspense, ledger.AccountTypeExternal:
		row = tx.QueryRowContext(ctx, `
			SELECT id, account_type, vault_id, user_id, adapter_name, asset_code, asset_unit, COALESCE(description,''), created_at, updated_at
			FROM ledger_accounts
			WHERE account_type = $1 AND asset_code = $2
			LIMIT 1
		`, accountType, assetCode)
	case ledger.AccountTypePenaltyEscrow:
		if vaultID == nil {
			return ledger.Account{}, errors.New("vault_id required for penalty_escrow")
		}
		row = tx.QueryRowContext(ctx, `
			SELECT id, account_type, vault_id, user_id, adapter_name, asset_code, asset_unit, COALESCE(description,''), created_at, updated_at
			FROM ledger_accounts
			WHERE account_type = $1 AND vault_id = $2 AND asset_code = $3
			LIMIT 1
		`, accountType, vaultID.String(), assetCode)
	default:
		return ledger.Account{}, ledger.ErrInvalidAccountType
	}

	acc, err := scanLedgerAccount(row)
	if err == nil {
		return acc, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ledger.Account{}, err
	}

	// Not found — create
	newID := uuid.New()
	now := time.Now().UTC()
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
	_, err = tx.ExecContext(ctx, `
		INSERT INTO ledger_accounts (id, account_type, vault_id, user_id, adapter_name, asset_code, asset_unit, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,'stroops',$7,$8)
		ON CONFLICT DO NOTHING
	`, newID.String(), accountType, vaultStr, userStr, adapterName, assetCode, now, now)
	if err != nil {
		return ledger.Account{}, err
	}
	// Re-select (handle race where another tx inserted)
	switch accountType {
	case ledger.AccountTypeUserVaultPosition:
		row = tx.QueryRowContext(ctx, `
			SELECT id, account_type, vault_id, user_id, adapter_name, asset_code, asset_unit, COALESCE(description,''), created_at, updated_at
			FROM ledger_accounts
			WHERE account_type = $1 AND vault_id = $2 AND user_id = $3 AND asset_code = $4
			LIMIT 1
		`, accountType, vaultID.String(), userID.String(), assetCode)
	case ledger.AccountTypeVaultAssetPool:
		row = tx.QueryRowContext(ctx, `
			SELECT id, account_type, vault_id, user_id, adapter_name, asset_code, asset_unit, COALESCE(description,''), created_at, updated_at
			FROM ledger_accounts
			WHERE account_type = $1 AND vault_id = $2 AND asset_code = $3
			LIMIT 1
		`, accountType, vaultID.String(), assetCode)
	case ledger.AccountTypeYieldSource:
		row = tx.QueryRowContext(ctx, `
			SELECT id, account_type, vault_id, user_id, adapter_name, asset_code, asset_unit, COALESCE(description,''), created_at, updated_at
			FROM ledger_accounts
			WHERE account_type = $1 AND adapter_name = $2 AND asset_code = $3
			LIMIT 1
		`, accountType, *adapterName, assetCode)
	case ledger.AccountTypeFee, ledger.AccountTypeTreasury, ledger.AccountTypeSystemSuspense, ledger.AccountTypeExternal:
		row = tx.QueryRowContext(ctx, `
			SELECT id, account_type, vault_id, user_id, adapter_name, asset_code, asset_unit, COALESCE(description,''), created_at, updated_at
			FROM ledger_accounts
			WHERE account_type = $1 AND asset_code = $2
			LIMIT 1
		`, accountType, assetCode)
	case ledger.AccountTypePenaltyEscrow:
		row = tx.QueryRowContext(ctx, `
			SELECT id, account_type, vault_id, user_id, adapter_name, asset_code, asset_unit, COALESCE(description,''), created_at, updated_at
			FROM ledger_accounts
			WHERE account_type = $1 AND vault_id = $2 AND asset_code = $3
			LIMIT 1
		`, accountType, vaultID.String(), assetCode)
	}
	return scanLedgerAccount(row)
}

// PostEntries posts balanced entries in its own transaction.
func (r *LedgerRepository) PostEntries(ctx context.Context, entries []ledger.Entry) error {
	if err := ledger.ValidateBalanced(entries); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := r.postEntriesTx(ctx, tx, entries); err != nil {
		return err
	}
	return tx.Commit()
}

// PostEntriesTx posts using an existing transaction handle — atomic with domain write.
func (r *LedgerRepository) PostEntriesTx(ctx context.Context, tx *sql.Tx, entries []ledger.Entry) error {
	if err := ledger.ValidateBalanced(entries); err != nil {
		return err
	}
	if tx == nil {
		return errors.New("tx is required")
	}
	return r.postEntriesTx(ctx, tx, entries)
}

func (r *LedgerRepository) postEntriesTx(ctx context.Context, tx *sql.Tx, entries []ledger.Entry) error {
	// Insert entries and update balances in same tx
	for _, e := range entries {
		if e.ID == uuid.Nil {
			e.ID = uuid.New()
		}
		dir := e.Direction
		if dir == "" {
			dir = ledger.DirectionFromAmount(e.Amount)
		}
		if e.AssetCode == "" {
			e.AssetCode = "USDC"
		}
		if e.AssetUnit == "" {
			e.AssetUnit = "stroops"
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO ledger_entries (id, transaction_id, account_id, amount, direction, created_at, domain_event_type, domain_event_id, asset_code, asset_unit)
			VALUES ($1,$2,$3,$4,$5,NOW(),$6,$7,$8,$9)
		`, e.ID.String(), e.TransactionID.String(), e.AccountID.String(), e.Amount, dir, e.DomainEventType, e.DomainEventID, e.AssetCode, e.AssetUnit)
		if err != nil {
			return fmt.Errorf("insert ledger_entry: %w", err)
		}
		// Upsert balance: balance = balance + amount
		_, err = tx.ExecContext(ctx, `
			INSERT INTO ledger_balances (account_id, balance, asset_code, asset_unit, updated_at, version)
			VALUES ($1,$2,$3,$4,NOW(),1)
			ON CONFLICT (account_id) DO UPDATE SET
				balance = ledger_balances.balance + EXCLUDED.balance,
				updated_at = NOW(),
				version = ledger_balances.version + 1
		`, e.AccountID.String(), e.Amount, e.AssetCode, e.AssetUnit)
		if err != nil {
			return fmt.Errorf("upsert ledger_balance: %w", err)
		}
	}
	return nil
}

func (r *LedgerRepository) GetBalance(ctx context.Context, accountID uuid.UUID) (ledger.Balance, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT account_id, balance, asset_code, asset_unit, updated_at, version
		FROM ledger_balances WHERE account_id = $1
	`, accountID.String())
	return scanLedgerBalance(row)
}

func (r *LedgerRepository) GetBalancesByVault(ctx context.Context, vaultID uuid.UUID) (map[string]int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT lb.account_id, lb.balance, la.account_type
		FROM ledger_balances lb
		JOIN ledger_accounts la ON la.id = lb.account_id
		WHERE la.vault_id = $1
	`, vaultID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := make(map[string]int64)
	for rows.Next() {
		var accountID string
		var balance int64
		var accType string
		if err := rows.Scan(&accountID, &balance, &accType); err != nil {
			return nil, err
		}
		m[accountID] = balance
	}
	return m, rows.Err()
}

func (r *LedgerRepository) GetUserVaultBalance(ctx context.Context, userID, vaultID uuid.UUID) (int64, error) {
	var bal sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT lb.balance
		FROM ledger_accounts la
		JOIN ledger_balances lb ON lb.account_id = la.id
		WHERE la.account_type = 'user_vault_position' AND la.vault_id = $1 AND la.user_id = $2 AND la.asset_code = 'USDC'
	`, vaultID.String(), userID.String()).Scan(&bal)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !bal.Valid {
		return 0, nil
	}
	return bal.Int64, nil
}

func (r *LedgerRepository) GetVaultPoolBalance(ctx context.Context, vaultID uuid.UUID) (int64, error) {
	var bal sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT lb.balance
		FROM ledger_accounts la
		JOIN ledger_balances lb ON lb.account_id = la.id
		WHERE la.account_type = 'vault_asset_pool' AND la.vault_id = $1 AND la.asset_code = 'USDC'
	`, vaultID.String()).Scan(&bal)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !bal.Valid {
		return 0, nil
	}
	return bal.Int64, nil
}

func (r *LedgerRepository) SumUserPositionBalances(ctx context.Context, vaultID uuid.UUID) (int64, error) {
	var sum sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(lb.balance),0)
		FROM ledger_accounts la
		JOIN ledger_balances lb ON lb.account_id = la.id
		WHERE la.account_type = 'user_vault_position' AND la.vault_id = $1
	`, vaultID.String()).Scan(&sum)
	if err != nil {
		return 0, err
	}
	if !sum.Valid {
		return 0, nil
	}
	return sum.Int64, nil
}

func (r *LedgerRepository) SumAllEntries(ctx context.Context) (int64, error) {
	var sum sql.NullInt64
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM ledger_entries`).Scan(&sum)
	if err != nil {
		return 0, err
	}
	if !sum.Valid {
		return 0, nil
	}
	return sum.Int64, nil
}

func (r *LedgerRepository) RecomputeBalances(ctx context.Context) ([]ledger.BalanceMismatch, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT la.id, COALESCE(lb.balance,0) as cached, COALESCE(SUM(le.amount),0) as computed
		FROM ledger_accounts la
		LEFT JOIN ledger_balances lb ON lb.account_id = la.id
		LEFT JOIN ledger_entries le ON le.account_id = la.id
		GROUP BY la.id, lb.balance
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var mismatches []ledger.BalanceMismatch
	for rows.Next() {
		var accountID string
		var cached, computed int64
		if err := rows.Scan(&accountID, &cached, &computed); err != nil {
			return nil, err
		}
		if cached != computed {
			id, _ := uuid.Parse(accountID)
			mismatches = append(mismatches, ledger.BalanceMismatch{
				AccountID:  id,
				Cached:     cached,
				Computed:   computed,
				Difference: computed - cached,
			})
		}
	}
	return mismatches, rows.Err()
}

func (r *LedgerRepository) CreateReconciliationRecord(ctx context.Context, rec ledger.ReconciliationRecord) error {
	if rec.ID == uuid.Nil {
		rec.ID = uuid.New()
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO ledger_reconciliation_records (id, vault_id, ledger_vault_pool_balance, on_chain_balance, difference, tolerance, status, details, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW())
	`, rec.ID.String(), rec.VaultID.String(), rec.LedgerVaultPoolBalance, rec.OnChainBalance, rec.Difference, rec.Tolerance, rec.Status, rec.Details)
	return err
}

func (r *LedgerRepository) ListReconciliationRecords(ctx context.Context, vaultID uuid.UUID, limit int) ([]ledger.ReconciliationRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, vault_id, ledger_vault_pool_balance, on_chain_balance, difference, tolerance, status, COALESCE(details::text,'{}'), created_at
		FROM ledger_reconciliation_records
		WHERE vault_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, vaultID.String(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ledger.ReconciliationRecord
	for rows.Next() {
		var rec ledger.ReconciliationRecord
		var idStr, vaultStr, details string
		if err := rows.Scan(&idStr, &vaultStr, &rec.LedgerVaultPoolBalance, &rec.OnChainBalance, &rec.Difference, &rec.Tolerance, &rec.Status, &details, &rec.CreatedAt); err != nil {
			return nil, err
		}
		rec.ID, _ = uuid.Parse(idStr)
		rec.VaultID, _ = uuid.Parse(vaultStr)
		rec.Details = details
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (r *LedgerRepository) ListAllBalances(ctx context.Context) ([]ledger.Balance, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT account_id, balance, asset_code, asset_unit, updated_at, version FROM ledger_balances
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ledger.Balance
	for rows.Next() {
		var b ledger.Balance
		var accID string
		if err := rows.Scan(&accID, &b.Balance, &b.AssetCode, &b.AssetUnit, &b.UpdatedAt, &b.Version); err != nil {
			return nil, err
		}
		b.AccountID, _ = uuid.Parse(accID)
		out = append(out, b)
	}
	return out, rows.Err()
}

type ledgerAccountScanner interface {
	Scan(dest ...any) error
}

func scanLedgerAccount(row ledgerAccountScanner) (ledger.Account, error) {
	var (
		id          string
		accType     string
		vaultID     sql.NullString
		userID      sql.NullString
		adapterName sql.NullString
		assetCode   string
		assetUnit   string
		desc        string
		createdAt   time.Time
		updatedAt   time.Time
	)
	if err := row.Scan(&id, &accType, &vaultID, &userID, &adapterName, &assetCode, &assetUnit, &desc, &createdAt, &updatedAt); err != nil {
		return ledger.Account{}, err
	}
	parsedID, err := uuid.Parse(id)
	if err != nil {
		return ledger.Account{}, err
	}
	var vID *uuid.UUID
	if vaultID.Valid {
		vid, err := uuid.Parse(vaultID.String)
		if err == nil {
			vID = &vid
		}
	}
	var uID *uuid.UUID
	if userID.Valid {
		uid, err := uuid.Parse(userID.String)
		if err == nil {
			uID = &uid
		}
	}
	var adapter *string
	if adapterName.Valid {
		adapter = &adapterName.String
	}
	return ledger.Account{
		ID:          parsedID,
		AccountType: accType,
		VaultID:     vID,
		UserID:      uID,
		AdapterName: adapter,
		AssetCode:   assetCode,
		AssetUnit:   assetUnit,
		Description: desc,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}

func scanLedgerBalance(row ledgerAccountScanner) (ledger.Balance, error) {
	var (
		accountID string
		balance   int64
		assetCode string
		assetUnit string
		updatedAt time.Time
		version   int64
	)
	if err := row.Scan(&accountID, &balance, &assetCode, &assetUnit, &updatedAt, &version); err != nil {
		return ledger.Balance{}, err
	}
	aid, err := uuid.Parse(accountID)
	if err != nil {
		return ledger.Balance{}, err
	}
	return ledger.Balance{
		AccountID: aid,
		Balance:   balance,
		AssetCode: assetCode,
		AssetUnit: assetUnit,
		UpdatedAt: updatedAt,
		Version:   version,
	}, nil
}
