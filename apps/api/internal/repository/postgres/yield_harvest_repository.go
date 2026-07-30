package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/yieldharvest"
)

// YieldHarvestRepository implements yieldharvest.Repository using Postgres.
type YieldHarvestRepository struct {
	db *sql.DB
}

// NewYieldHarvestRepository constructs a YieldHarvestRepository.
func NewYieldHarvestRepository(db *sql.DB) *YieldHarvestRepository {
	return &YieldHarvestRepository{db: db}
}

// Create inserts a new yield_harvests row and returns the persisted record.
// It also posts balanced double-entry ledger entries atomically within the same transaction.
func (r *YieldHarvestRepository) Create(ctx context.Context, input yieldharvest.CreateInput) (yieldharvest.YieldHarvest, error) {
	if input.HarvestedAt.IsZero() {
		input.HarvestedAt = time.Now().UTC()
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return yieldharvest.YieldHarvest{}, err
	}
	defer func() { _ = tx.Rollback() }()

	const q = `
		INSERT INTO yield_harvests (user_id, vault_id, amount, currency, harvested_at, tx_hash)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, user_id, vault_id, amount, currency, harvested_at, tx_hash`

	row := tx.QueryRowContext(ctx, q,
		input.UserID.String(),
		input.VaultID.String(),
		input.Amount.String(),
		input.Currency,
		input.HarvestedAt,
		nullableString(input.TxHash),
	)

	record, err := scanYieldHarvest(row)
	if err != nil {
		return yieldharvest.YieldHarvest{}, err
	}

	// --- Ledger: post harvest within same transaction (gross = net + fee, fee 10%) ---
	if !input.Amount.IsZero() {
		if err := r.postHarvestLedgerTx(ctx, tx, input.VaultID, input.UserID, input.Amount, input.TxHash); err != nil {
			if !isLedgerTableMissingErr(err) {
				return yieldharvest.YieldHarvest{}, fmt.Errorf("ledger harvest posting: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return yieldharvest.YieldHarvest{}, err
	}
	return record, nil
}

func isLedgerTableMissingErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "ledger") && strings.Contains(s, "does not exist")
}

// postHarvestLedgerTx posts harvest ledger entries within the same tx.
// Net yield = amount, fee = 10% of gross, gross = net / 0.9 . For simplicity fee = net/9.
func (r *YieldHarvestRepository) postHarvestLedgerTx(ctx context.Context, tx *sql.Tx, vaultID uuid.UUID, userID uuid.UUID, netAmount decimal.Decimal, txHash string) error {
	netStroops := netAmount.Mul(decimal.NewFromInt(10_000_000)).Round(0).IntPart()
	if netStroops <= 0 {
		return nil
	}
	// fee = net / 9 (approx 10% of gross)
	feeStroops := netStroops / 9
	if feeStroops < 0 {
		feeStroops = 0
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
	adapter := "blend"
	yieldAccID, err := ledgerGetOrCreateAccountTx(ctx, tx, "yield_source", nil, nil, &adapter, "USDC")
	if err != nil {
		return err
	}
	suspenseID, err := ledgerGetOrCreateAccountTx(ctx, tx, "system_suspense", nil, nil, nil, "USDC")
	if err != nil {
		return err
	}

	// Build balanced entries: vault +net, user +net, fee +fee, yield -gross, suspense -net
	type e struct {
		AccountID       uuid.UUID
		Amount          int64
		DomainEventType string
		DomainEventID   string
	}
	entries := []e{
		{AccountID: vaultAccID, Amount: netStroops, DomainEventType: "harvest", DomainEventID: txHash},
		{AccountID: userAccID, Amount: netStroops, DomainEventType: "harvest", DomainEventID: txHash},
		{AccountID: suspenseID, Amount: -netStroops, DomainEventType: "harvest", DomainEventID: txHash},
	}
	if feeStroops != 0 {
		entries = append(entries, e{AccountID: feeAccID, Amount: feeStroops, DomainEventType: "harvest", DomainEventID: txHash})
	}
	if grossStroops != 0 {
		entries = append(entries, e{AccountID: yieldAccID, Amount: -grossStroops, DomainEventType: "harvest", DomainEventID: txHash})
	}

	// Validate sum zero
	var sum int64
	for _, en := range entries {
		sum += en.Amount
	}
	if sum != 0 {
		// Should be zero: net+net+fee -net -gross = net+fee-gross =0
		return fmt.Errorf("harvest ledger unbalanced: sum=%d", sum)
	}

	txID := uuid.New()
	for _, en := range entries {
		if en.Amount == 0 {
			continue
		}
		dir := "debit"
		if en.Amount < 0 {
			dir = "credit"
		}
		entryID := uuid.New()
		_, err := tx.ExecContext(ctx, `
			INSERT INTO ledger_entries (id, transaction_id, account_id, amount, direction, created_at, domain_event_type, domain_event_id, asset_code, asset_unit)
			VALUES ($1,$2,$3,$4,$5,NOW(),$6,$7,'USDC','stroops')
		`, entryID.String(), txID.String(), en.AccountID.String(), en.Amount, dir, en.DomainEventType, en.DomainEventID)
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
		`, en.AccountID.String(), en.Amount)
		if err != nil {
			return err
		}
	}
	return nil
}

// ListForUser returns harvest records for a user with keyset cursor pagination.
func (r *YieldHarvestRepository) ListForUser(ctx context.Context, filter yieldharvest.ListFilter) ([]yieldharvest.YieldHarvest, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	args := []any{filter.UserID, limit}
	where := "yh.user_id = $1"

	if filter.CursorTime != nil && filter.CursorID != nil {
		args = append(args, *filter.CursorTime, *filter.CursorID)
		idx := len(args) - 1
		where += fmt.Sprintf(" AND (yh.harvested_at, yh.id) < ($%d, $%d)", idx, idx+1)
	}
	if filter.Since != nil {
		args = append(args, *filter.Since)
		where += fmt.Sprintf(" AND yh.harvested_at >= $%d", len(args))
	}
	if filter.Until != nil {
		args = append(args, *filter.Until)
		where += fmt.Sprintf(" AND yh.harvested_at < $%d", len(args))
	}

	q := fmt.Sprintf(`
		SELECT
			yh.id,
			yh.user_id,
			yh.vault_id,
			yh.amount,
			yh.currency,
			COALESCE(va.protocol, '') AS protocol,
			yh.harvested_at,
			COALESCE(yh.tx_hash, '') AS tx_hash
		FROM yield_harvests yh
		LEFT JOIN LATERAL (
			SELECT protocol
			FROM vault_allocations
			WHERE vault_id = yh.vault_id
			ORDER BY allocated_at DESC
			LIMIT 1
		) va ON true
		WHERE %s
		ORDER BY yh.harvested_at DESC, yh.id DESC
		LIMIT $2`, where)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("yield_harvests list: %w", err)
	}
	defer rows.Close()

	var results []yieldharvest.YieldHarvest
	for rows.Next() {
		var h yieldharvest.YieldHarvest
		var amountStr string
		if err := rows.Scan(
			&h.ID,
			&h.UserID,
			&h.VaultID,
			&amountStr,
			&h.Currency,
			&h.Protocol,
			&h.HarvestedAt,
			&h.TxHash,
		); err != nil {
			return nil, fmt.Errorf("yield_harvests scan: %w", err)
		}
		h.Amount, err = decimal.NewFromString(amountStr)
		if err != nil {
			return nil, fmt.Errorf("yield_harvests parse amount: %w", err)
		}
		results = append(results, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("yield_harvests rows: %w", err)
	}
	return results, nil
}

func scanYieldHarvest(row *sql.Row) (yieldharvest.YieldHarvest, error) {
	var h yieldharvest.YieldHarvest
	var amountStr string
	var txHash sql.NullString
	if err := row.Scan(
		&h.ID,
		&h.UserID,
		&h.VaultID,
		&amountStr,
		&h.Currency,
		&h.HarvestedAt,
		&txHash,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return yieldharvest.YieldHarvest{}, err
		}
		return yieldharvest.YieldHarvest{}, fmt.Errorf("yield_harvest scan: %w", err)
	}
	amount, err := decimal.NewFromString(amountStr)
	if err != nil {
		return yieldharvest.YieldHarvest{}, fmt.Errorf("yield_harvest parse amount: %w", err)
	}
	h.Amount = amount
	if txHash.Valid {
		h.TxHash = txHash.String
	}
	return h, nil
}

// nullableString returns nil when s is empty so Postgres stores NULL instead of "".
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
