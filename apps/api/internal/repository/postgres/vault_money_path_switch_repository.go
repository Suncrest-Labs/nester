package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/moneypath"
)

// VaultMoneyPathSwitchRepository persists the per-vault deposit and
// withdrawal pause switches (migration 122, nester#1322).
//
// The global switches (#1120) live in MoneyPathSwitchRepository. This is the
// same control scoped to a single vault, so one vault can be paused while
// every other vault keeps serving. Depends on the domain package rather than
// service, so repository -> service never appears in the import graph.
type VaultMoneyPathSwitchRepository struct {
	db *sql.DB
}

func NewVaultMoneyPathSwitchRepository(db *sql.DB) *VaultMoneyPathSwitchRepository {
	return &VaultMoneyPathSwitchRepository{db: db}
}

const vaultMoneyPathSwitchColumns = `vault_id, operation, paused, reason, changed_by, updated_at`

// GetVaultSwitch reads one vault's switch for one operation.
//
// A missing row is NOT an error: vaults are created and closed at runtime,
// so there is nothing to seed, and the absence of a row means "not paused".
// This is the deliberate opposite of the global repository, where a missing
// row means the migration did not run and the caller must fail closed — the
// asymmetry is the point, not an oversight.
func (r *VaultMoneyPathSwitchRepository) GetVaultSwitch(
	ctx context.Context,
	vaultID uuid.UUID,
	op moneypath.Operation,
) (moneypath.VaultSwitch, error) {
	query := `SELECT ` + vaultMoneyPathSwitchColumns +
		` FROM vault_money_path_switches WHERE vault_id = $1 AND operation = $2`

	s := moneypath.VaultSwitch{VaultID: vaultID, Operation: op}
	var changedBy uuid.NullUUID
	err := r.db.QueryRowContext(ctx, query, vaultID, string(op)).
		Scan(&s.VaultID, &s.Operation, &s.Paused, &s.Reason, &changedBy, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return moneypath.VaultSwitch{VaultID: vaultID, Operation: op}, nil
	}
	if err != nil {
		return moneypath.VaultSwitch{}, err
	}
	if changedBy.Valid {
		id := changedBy.UUID
		s.ChangedBy = &id
	}
	return s, nil
}

// ListVaultSwitches reads every switch stored for one vault, ordered so the
// response is stable. Operations with no row are absent rather than
// synthesised; the service fills in the released default per operation so
// the admin view always shows both switches.
func (r *VaultMoneyPathSwitchRepository) ListVaultSwitches(
	ctx context.Context,
	vaultID uuid.UUID,
) ([]moneypath.VaultSwitch, error) {
	query := `SELECT ` + vaultMoneyPathSwitchColumns +
		` FROM vault_money_path_switches WHERE vault_id = $1 ORDER BY operation`

	rows, err := r.db.QueryContext(ctx, query, vaultID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []moneypath.VaultSwitch
	for rows.Next() {
		var s moneypath.VaultSwitch
		var changedBy uuid.NullUUID
		if err := rows.Scan(&s.VaultID, &s.Operation, &s.Paused, &s.Reason, &changedBy, &s.UpdatedAt); err != nil {
			return nil, err
		}
		if changedBy.Valid {
			id := changedBy.UUID
			s.ChangedBy = &id
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetVaultSwitch engages or releases one vault's switch and returns the
// stored row.
//
// INSERT ... ON CONFLICT rather than UPDATE: unlike the global switches,
// these rows are created on first use, so an UPDATE would silently match
// nothing the first time an operator pauses a vault — the exact moment the
// control has to work.
func (r *VaultMoneyPathSwitchRepository) SetVaultSwitch(
	ctx context.Context,
	vaultID uuid.UUID,
	op moneypath.Operation,
	paused bool,
	reason string,
	changedBy *uuid.UUID,
) (moneypath.VaultSwitch, error) {
	query := `
		INSERT INTO vault_money_path_switches (vault_id, operation, paused, reason, changed_by, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (vault_id, operation) DO UPDATE
		SET paused = EXCLUDED.paused,
		    reason = EXCLUDED.reason,
		    changed_by = EXCLUDED.changed_by,
		    updated_at = NOW()
		RETURNING ` + vaultMoneyPathSwitchColumns

	var actor uuid.NullUUID
	if changedBy != nil {
		actor = uuid.NullUUID{UUID: *changedBy, Valid: true}
	}

	var s moneypath.VaultSwitch
	var scannedBy uuid.NullUUID
	err := r.db.QueryRowContext(ctx, query, vaultID, string(op), paused, reason, actor).
		Scan(&s.VaultID, &s.Operation, &s.Paused, &s.Reason, &scannedBy, &s.UpdatedAt)
	if err != nil {
		return moneypath.VaultSwitch{}, err
	}
	if scannedBy.Valid {
		id := scannedBy.UUID
		s.ChangedBy = &id
	}
	return s, nil
}
