package stellar

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
)

// A deposit event whose transaction hash has already been claimed (by the API
// write path, or by an earlier delivery) must not move the balance a second
// time: the claim insert affects zero rows and no UPDATE follows
// (nester#1147).
func TestApplyIndexedEvent_Deposit_AlreadyClaimedHash_DoesNotCredit(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	event := indexedEvent{
		ID:         "evt-dup",
		ContractID: "C1",
		EventType:  "deposit",
		Ledger:     900,
		TxHash:     "hash-already-credited",
		// Stroops, as the contract emits them: 10 asset units (nester#1146).
		Data: map[string]any{"amount": "100000000"},
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_events").
		WithArgs(event.ID, event.Ledger).
		WillReturnResult(sqlmock.NewResult(1, 1))
	// The API path already claimed this hash: zero rows inserted.
	mock.ExpectExec("INSERT INTO vault_transactions").
		WithArgs(event.ContractID, "deposit", "10", event.TxHash).
		WillReturnResult(sqlmock.NewResult(0, 0))
	// Crucially: no "UPDATE vaults" is expected.
	mock.ExpectCommit()

	processed, err := applyIndexedEvent(context.Background(), db, event)
	assert.NoError(t, err)
	assert.True(t, processed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// When the indexer wins the claim it credits normally.
func TestApplyIndexedEvent_Deposit_UnclaimedHash_Credits(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	event := indexedEvent{
		ID:         "evt-fresh",
		ContractID: "C1",
		EventType:  "deposit",
		Ledger:     901,
		TxHash:     "hash-not-yet-seen",
		// Stroops, as the contract emits them: 10 asset units (nester#1146).
		Data: map[string]any{"amount": "100000000"},
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_events").
		WithArgs(event.ID, event.Ledger).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO vault_transactions").
		WithArgs(event.ContractID, "deposit", "10", event.TxHash).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE vaults").
		WithArgs("10", event.ContractID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	processed, err := applyIndexedEvent(context.Background(), db, event)
	assert.NoError(t, err)
	assert.True(t, processed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// An event with no transaction hash cannot participate in the shared key. It
// must still be applied — it can only be a direct on-chain movement nothing
// else will credit — and must not attempt a claim.
func TestApplyIndexedEvent_Deposit_NoTxHash_CreditsWithoutClaiming(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	event := indexedEvent{
		ID:         "evt-nohash",
		ContractID: "C1",
		EventType:  "deposit",
		Ledger:     902,
		// 7 asset units in stroops.
		Data: map[string]any{"amount": "70000000"},
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_events").
		WithArgs(event.ID, event.Ledger).
		WillReturnResult(sqlmock.NewResult(1, 1))
	// No claim insert is expected; straight to the balance update.
	mock.ExpectExec("UPDATE vaults").
		WithArgs("7", event.ContractID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	processed, err := applyIndexedEvent(context.Background(), db, event)
	assert.NoError(t, err)
	assert.True(t, processed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// Withdrawals share the same key, so an already-claimed withdrawal hash must
// not debit twice.
func TestApplyIndexedEvent_Withdraw_AlreadyClaimedHash_DoesNotDebit(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	event := indexedEvent{
		ID:         "evt-wd-dup",
		ContractID: "C1",
		EventType:  "withdraw",
		Ledger:     903,
		TxHash:     "wd-hash-already-applied",
		// 3 asset units in stroops.
		Data: map[string]any{"amount": "30000000"},
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_events").
		WithArgs(event.ID, event.Ledger).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO vault_transactions").
		WithArgs(event.ContractID, "withdrawal", "3", event.TxHash).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	processed, err := applyIndexedEvent(context.Background(), db, event)
	assert.NoError(t, err)
	assert.True(t, processed)
	assert.NoError(t, mock.ExpectationsWereMet())
}
