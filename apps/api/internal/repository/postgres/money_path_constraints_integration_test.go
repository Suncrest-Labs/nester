package postgres

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/testutil"
)

// The database must refuse money-path violations on its own (nester#1083).
// Asserting these in Go only proves the current call path is careful; asserting
// them here proves a future one cannot get it wrong.
func TestIntegrationMoneyPathConstraintsRejectViolations(t *testing.T) {
	db := openIntegrationDB(t)
	testutil.ApplyAllMigrations(t, db, filepath.Join("..", "..", "..", "migrations"))

	userID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO users (id, wallet_address, display_name) VALUES ($1, $2, $3)`,
		userID, "GTESTCONSTRAINTS"+uuid.NewString()[:8], "constraints",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	contract := "CTESTCONSTRAINT" + uuid.NewString()[:8]
	insertVault := func(id uuid.UUID, addr string) error {
		_, err := db.Exec(
			`INSERT INTO vaults (id, user_id, contract_address, currency, status)
			 VALUES ($1, $2, $3, 'USDC', 'active')`,
			id, userID, addr,
		)
		return err
	}

	vaultID := uuid.New()
	if err := insertVault(vaultID, contract); err != nil {
		t.Fatalf("seed vault: %v", err)
	}

	t.Run("duplicate contract address is rejected", func(t *testing.T) {
		// Two vaults on one contract makes the indexer's
		// `WHERE contract_address = $1` update both, so one user's on-chain
		// deposit credits another user's vault.
		if err := insertVault(uuid.New(), contract); err == nil {
			t.Fatal("a second vault on the same contract address was accepted")
		}
	})

	t.Run("soft-deleted vault frees its contract address", func(t *testing.T) {
		addr := "CSOFTDELETE" + uuid.NewString()[:8]
		first := uuid.New()
		if err := insertVault(first, addr); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, err := db.Exec(`UPDATE vaults SET deleted_at = NOW() WHERE id = $1`, first); err != nil {
			t.Fatalf("soft delete: %v", err)
		}
		// The uniqueness is scoped to live rows on purpose: a deleted vault
		// must not permanently burn its contract address.
		if err := insertVault(uuid.New(), addr); err != nil {
			t.Fatalf("re-registering a soft-deleted contract address was rejected: %v", err)
		}
	})

	t.Run("negative balance is rejected", func(t *testing.T) {
		_, err := db.Exec(`UPDATE vaults SET current_balance = -1 WHERE id = $1`, vaultID)
		if err == nil {
			t.Fatal("a negative current_balance was accepted")
		}
	})

	t.Run("negative yield and fees are rejected", func(t *testing.T) {
		if _, err := db.Exec(`UPDATE vaults SET yield_earned = -1 WHERE id = $1`, vaultID); err == nil {
			t.Fatal("a negative yield_earned was accepted")
		}
		if _, err := db.Exec(`UPDATE vaults SET fees_paid = -1 WHERE id = $1`, vaultID); err == nil {
			t.Fatal("a negative fees_paid was accepted")
		}
	})

	t.Run("duplicate transaction hash is rejected", func(t *testing.T) {
		hash := "TXHASH" + uuid.NewString()
		insertTx := func() error {
			_, err := db.Exec(
				`INSERT INTO vault_transactions (vault_id, type, amount, tx_hash)
				 VALUES ($1, 'deposit', 10, $2)`,
				vaultID, hash,
			)
			return err
		}
		if err := insertTx(); err != nil {
			t.Fatalf("first insert: %v", err)
		}
		// This is what makes replay rejection an invariant rather than a
		// race between two concurrent requests.
		if err := insertTx(); err == nil {
			t.Fatal("the same transaction hash was recorded twice")
		}
	})

	t.Run("null transaction hashes do not collide", func(t *testing.T) {
		// Server-submitted rows may legitimately have no hash yet; the
		// partial index must not treat them as duplicates of each other.
		for i := 0; i < 2; i++ {
			if _, err := db.Exec(
				`INSERT INTO vault_transactions (vault_id, type, amount, tx_hash)
				 VALUES ($1, 'deposit', 5, NULL)`,
				vaultID,
			); err != nil {
				t.Fatalf("insert %d with null hash: %v", i, err)
			}
		}
	})

	t.Run("negative shares and non-positive share price are rejected", func(t *testing.T) {
		if _, err := db.Exec(
			`INSERT INTO vault_transactions (vault_id, type, amount, shares_minted_or_burned)
			 VALUES ($1, 'deposit', 10, -1)`,
			vaultID,
		); err == nil {
			t.Fatal("a negative share count was accepted")
		}
		if _, err := db.Exec(
			`INSERT INTO vault_transactions (vault_id, type, amount, share_price_at_time)
			 VALUES ($1, 'deposit', 10, 0)`,
			vaultID,
		); err == nil {
			t.Fatal("a zero share price was accepted")
		}
	})

	t.Run("non-positive amount is rejected", func(t *testing.T) {
		if _, err := db.Exec(
			`INSERT INTO vault_transactions (vault_id, type, amount) VALUES ($1, 'deposit', 0)`,
			vaultID,
		); err == nil {
			t.Fatal("a zero-amount transaction was accepted")
		}
	})
}
