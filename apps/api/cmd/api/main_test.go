package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestApplyIndexedEvent_Deposit_ProcessesOnce(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	event := indexedEvent{
		ID:         "evt-1",
		ContractID: "C1",
		EventType:  "deposit",
		Ledger:     123,
		Data: map[string]any{
			"amount": "10.25",
		},
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_chain_events").
		WithArgs(event.ID, event.ContractID, event.EventType, event.Ledger).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE vaults").
		WithArgs("10.25", event.ContractID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	processed, err := applyIndexedEvent(context.Background(), db, event)
	assert.NoError(t, err)
	assert.True(t, processed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyIndexedEvent_DuplicateEvent_IsSkipped(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	event := indexedEvent{
		ID:         "evt-duplicate",
		ContractID: "C1",
		EventType:  "deposit",
		Ledger:     124,
		Data: map[string]any{
			"amount": "5",
		},
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_chain_events").
		WithArgs(event.ID, event.ContractID, event.EventType, event.Ledger).
		WillReturnResult(sqlmock.NewResult(1, 0))
	mock.ExpectCommit()

	processed, err := applyIndexedEvent(context.Background(), db, event)
	assert.NoError(t, err)
	assert.False(t, processed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestExtractEventAmount_JSONNumber(t *testing.T) {
	amount, ok := extractEventAmount(indexedEvent{
		Data: map[string]any{
			"amount": json.Number("123456789012345678901234567890"),
		},
	})
	assert.True(t, ok)
	assert.True(t, amount.Equal(decimal.RequireFromString("123456789012345678901234567890")))
}

