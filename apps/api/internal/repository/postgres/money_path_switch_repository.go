package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/moneypath"
)

// MoneyPathSwitchRepository persists the global deposit and withdrawal pause
// switches (migration 106, nester#1120). Depends on the domain package rather
// than service, so repository -> service never appears in the import graph.
type MoneyPathSwitchRepository struct {
	db *sql.DB
}

func NewMoneyPathSwitchRepository(db *sql.DB) *MoneyPathSwitchRepository {
	return &MoneyPathSwitchRepository{db: db}
}

const moneyPathSwitchColumns = `operation, paused, reason, changed_by, updated_at`

// GetSwitch reads one switch.
//
// A missing row is an error rather than a default of "not paused": the
// migration seeds both operations, so absence means the schema is not what
// this code expects, and the caller fails closed on the error.
func (r *MoneyPathSwitchRepository) GetSwitch(ctx context.Context, op moneypath.Operation) (moneypath.Switch, error) {
	query := `SELECT ` + moneyPathSwitchColumns + ` FROM money_path_switches WHERE operation = $1`

	var s moneypath.Switch
	var changedBy uuid.NullUUID
	err := r.db.QueryRowContext(ctx, query, string(op)).
		Scan(&s.Operation, &s.Paused, &s.Reason, &changedBy, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return moneypath.Switch{}, fmt.Errorf("%w: %s", moneypath.ErrUnknownOperation, op)
	}
	if err != nil {
		return moneypath.Switch{}, err
	}
	if changedBy.Valid {
		id := changedBy.UUID
		s.ChangedBy = &id
	}
	return s, nil
}

// ListSwitches reads every switch, ordered so the response is stable.
func (r *MoneyPathSwitchRepository) ListSwitches(ctx context.Context) ([]moneypath.Switch, error) {
	query := `SELECT ` + moneyPathSwitchColumns + ` FROM money_path_switches ORDER BY operation`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []moneypath.Switch
	for rows.Next() {
		var s moneypath.Switch
		var changedBy uuid.NullUUID
		if err := rows.Scan(&s.Operation, &s.Paused, &s.Reason, &changedBy, &s.UpdatedAt); err != nil {
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

// SetSwitch engages or releases one switch and returns the stored row.
//
// UPDATE rather than upsert: the migration seeds both operations, so an
// operation with no row is a schema problem that should surface as an error
// during an incident rather than being papered over with an insert.
func (r *MoneyPathSwitchRepository) SetSwitch(
	ctx context.Context,
	op moneypath.Operation,
	paused bool,
	reason string,
	changedBy *uuid.UUID,
) (moneypath.Switch, error) {
	query := `
		UPDATE money_path_switches
		SET paused = $2, reason = $3, changed_by = $4, updated_at = NOW()
		WHERE operation = $1
		RETURNING ` + moneyPathSwitchColumns

	var actor uuid.NullUUID
	if changedBy != nil {
		actor = uuid.NullUUID{UUID: *changedBy, Valid: true}
	}

	var s moneypath.Switch
	var scannedBy uuid.NullUUID
	err := r.db.QueryRowContext(ctx, query, string(op), paused, reason, actor).
		Scan(&s.Operation, &s.Paused, &s.Reason, &scannedBy, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return moneypath.Switch{}, fmt.Errorf("%w: %s", moneypath.ErrUnknownOperation, op)
	}
	if err != nil {
		return moneypath.Switch{}, err
	}
	if scannedBy.Valid {
		id := scannedBy.UUID
		s.ChangedBy = &id
	}
	return s, nil
}
