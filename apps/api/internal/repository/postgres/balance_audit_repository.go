package postgres

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/balanceaudit"
)

// BalanceAuditRepository persists the append-only balance change ledger
// (nester#1124, migration 108). It intentionally exposes no Update or
// Delete method.
type BalanceAuditRepository struct {
	db *sql.DB
}

func NewBalanceAuditRepository(db *sql.DB) *BalanceAuditRepository {
	return &BalanceAuditRepository{db: db}
}

func (r *BalanceAuditRepository) Append(ctx context.Context, entry balanceaudit.Entry) (balanceaudit.Entry, error) {
	return appendBalanceAuditEntry(ctx, r.db, entry)
}

// queryRowContexter is satisfied by both *sql.DB and *sql.Tx, letting
// appendBalanceAuditEntry insert either standalone (via Append) or as part
// of another repository's transaction (see
// VaultRepository.RecordDepositWithAudit), so the audit append and the
// balance mutation it describes commit or roll back together — durability
// nester CodeRabbit's audit-gap finding requires (#1124).
type queryRowContexter interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func appendBalanceAuditEntry(ctx context.Context, q queryRowContexter, entry balanceaudit.Entry) (balanceaudit.Entry, error) {
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}

	var metadataJSON []byte
	if len(entry.Metadata) > 0 {
		b, err := json.Marshal(entry.Metadata)
		if err != nil {
			return balanceaudit.Entry{}, err
		}
		metadataJSON = b
	}

	var chainRef *string
	if entry.ChainReference != "" {
		chainRef = &entry.ChainReference
	}

	const query = `
		INSERT INTO balance_audit_log (
			id, vault_id, user_id, actor, operation, amount, balance_before, balance_after,
			chain_reference, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING created_at
	`
	err := q.QueryRowContext(ctx, query,
		entry.ID.String(), entry.VaultID.String(), entry.UserID.String(),
		entry.Actor, string(entry.Operation), entry.Amount, entry.BalanceBefore, entry.BalanceAfter,
		chainRef, nullableJSON(metadataJSON),
	).Scan(&entry.CreatedAt)
	if err != nil {
		return balanceaudit.Entry{}, err
	}
	return entry, nil
}

func (r *BalanceAuditRepository) ListByVault(ctx context.Context, vaultID uuid.UUID, limit, offset int) ([]balanceaudit.Entry, int, error) {
	limit, offset = normalizeListPage(limit, offset)

	var total int
	if err := r.db.QueryRowContext(ctx,
		`SELECT count(*) FROM balance_audit_log WHERE vault_id = $1`, vaultID.String(),
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	const query = `
		SELECT id, vault_id, user_id, actor, operation, amount, balance_before, balance_after,
		       chain_reference, metadata, created_at
		FROM balance_audit_log
		WHERE vault_id = $1
		ORDER BY created_at ASC, id ASC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.QueryContext(ctx, query, vaultID.String(), limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	entries, err := scanBalanceAuditEntries(rows, total)
	if err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

func (r *BalanceAuditRepository) ListByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]balanceaudit.Entry, int, error) {
	limit, offset = normalizeListPage(limit, offset)

	var total int
	if err := r.db.QueryRowContext(ctx,
		`SELECT count(*) FROM balance_audit_log WHERE user_id = $1`, userID.String(),
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	const query = `
		SELECT id, vault_id, user_id, actor, operation, amount, balance_before, balance_after,
		       chain_reference, metadata, created_at
		FROM balance_audit_log
		WHERE user_id = $1
		ORDER BY created_at ASC, id ASC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.QueryContext(ctx, query, userID.String(), limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	entries, err := scanBalanceAuditEntries(rows, total)
	if err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}

// normalizeListPage applies balanceaudit.DefaultListLimit when a caller
// passes a non-positive limit (the "reasonable default" callers that don't
// yet page get, matching how reconciliation's vaultReconcilePageSize backs
// its own Limit/Offset paging) and clamps a negative offset to 0.
func normalizeListPage(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = balanceaudit.DefaultListLimit
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func scanBalanceAuditEntries(rows *sql.Rows, total int) ([]balanceaudit.Entry, error) {
	var out []balanceaudit.Entry
	for rows.Next() {
		var (
			e            balanceaudit.Entry
			idStr        string
			vaultIDStr   string
			userIDStr    string
			operation    string
			chainRef     sql.NullString
			metadataJSON []byte
		)
		if err := rows.Scan(
			&idStr, &vaultIDStr, &userIDStr, &e.Actor, &operation,
			&e.Amount, &e.BalanceBefore, &e.BalanceAfter,
			&chainRef, &metadataJSON, &e.CreatedAt,
		); err != nil {
			return nil, err
		}

		id, err := uuid.Parse(idStr)
		if err != nil {
			return nil, err
		}
		vaultID, err := uuid.Parse(vaultIDStr)
		if err != nil {
			return nil, err
		}
		userID, err := uuid.Parse(userIDStr)
		if err != nil {
			return nil, err
		}

		e.ID = id
		e.VaultID = vaultID
		e.UserID = userID
		e.Operation = balanceaudit.Operation(operation)
		if chainRef.Valid {
			e.ChainReference = chainRef.String
		}
		if len(metadataJSON) > 0 {
			var meta map[string]any
			if err := json.Unmarshal(metadataJSON, &meta); err != nil {
				return nil, err
			}
			e.Metadata = meta
		}

		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// balanceaudit.Repository's contract documents ErrNotFound for a lookup
	// with no entries at all; returning (nil, nil) here would silently
	// violate it for every caller of ListByVault/ListByUser (nester
	// CodeRabbit finding). But an empty page is not the same as an empty
	// result set: total is the true count for the vault/user regardless of
	// this page's LIMIT/OFFSET, so an offset landing at or past the end of an
	// otherwise non-empty result set (e.g. total=5, offset=10) must return an
	// empty slice with a nil error, not ErrNotFound (nester CodeRabbit
	// post-rebase finding).
	if total == 0 {
		return nil, balanceaudit.ErrNotFound
	}
	return out, nil
}

// nullableJSON returns nil (SQL NULL) for an empty payload rather than
// binding a zero-length []byte, which some drivers coerce to an empty
// (invalid) JSON string instead of NULL.
func nullableJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
