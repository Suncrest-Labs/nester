package postgres

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
)

func seedIntegrationVault(t *testing.T, db *sql.DB, userID uuid.UUID) uuid.UUID {
	t.Helper()
	vaultID := uuid.New()
	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO vaults (id, user_id, contract_address, total_deposited, current_balance, currency, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		vaultID.String(),
		userID.String(),
		// Unique per vault: contract_address is unique among live vaults
		// (migration 104), because the event indexer keys balance mutations
		// on it. A shared literal made every multi-vault test collide.
		"CA-SEED-INT-"+vaultID.String(),
		"0",
		"0",
		"USDC",
		"active",
	)
	if err != nil {
		t.Fatalf("seed vault: %v", err)
	}
	return vaultID
}
