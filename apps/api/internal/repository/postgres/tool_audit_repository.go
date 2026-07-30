package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/toolaudit"
)

const genesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

type ToolAuditRepository struct {
	db *sql.DB
}

func NewToolAuditRepository(db *sql.DB) *ToolAuditRepository {
	return &ToolAuditRepository{db: db}
}

// InsertChained reads the caller's latest hash, computes the new entry's
// hash, and inserts it within a single transaction guarded by a per-user
// Postgres advisory lock. Without the lock, two concurrent invocations for
// the same user could both read the same prevHash and insert two entries
// claiming the same parent — silently forking the tamper-evident chain.
func (r *ToolAuditRepository) InsertChained(ctx context.Context, inv toolaudit.ToolInvocation) (toolaudit.ToolInvocation, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return toolaudit.ToolInvocation{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, userLockKey(inv.UserID)); err != nil {
		return toolaudit.ToolInvocation{}, err
	}

	var prevHash string
	err = tx.QueryRowContext(ctx, `
		SELECT entry_hash
		FROM tool_invocations
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, inv.UserID).Scan(&prevHash)
	if err != nil {
		if err == sql.ErrNoRows {
			prevHash = genesisHash
		} else {
			return toolaudit.ToolInvocation{}, err
		}
	}

	inv.PrevHash = prevHash
	inv.EntryHash = inv.ComputeHash(prevHash)

	argsBytes, err := json.Marshal(inv.Arguments)
	if err != nil {
		return toolaudit.ToolInvocation{}, err
	}
	resultBytes, err := json.Marshal(inv.Result)
	if err != nil {
		return toolaudit.ToolInvocation{}, err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO tool_invocations (
			id, user_id, request_id, conversation_id, tool_name, arguments, consequential, status, result, error_message, prev_hash, entry_hash, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		)
	`,
		inv.ID, inv.UserID, inv.RequestID, inv.ConversationID, inv.ToolName, argsBytes, inv.Consequential, inv.Status, resultBytes, inv.ErrorMessage, inv.PrevHash, inv.EntryHash, inv.CreatedAt,
	)
	if err != nil {
		return toolaudit.ToolInvocation{}, err
	}

	if err := tx.Commit(); err != nil {
		return toolaudit.ToolInvocation{}, err
	}
	return inv, nil
}

// userLockKey derives a stable int64 advisory-lock key from a user ID string.
// pg_advisory_xact_lock takes a bigint; FNV-1a keeps the mapping deterministic
// without needing user_id to be numeric.
func userLockKey(userID string) int64 {
	h := fnv.New64a()
	_, _ = fmt.Fprint(h, userID)
	return int64(h.Sum64()) //nolint:gosec // deterministic bucketing, not security-sensitive
}
