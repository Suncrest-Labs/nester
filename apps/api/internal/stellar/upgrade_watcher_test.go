package stellar

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
)

type recordingUpgradeObserver struct {
	events []struct {
		eventType string
		upgrade   PendingUpgrade
	}
}

func (r *recordingUpgradeObserver) OnUpgradeEvent(_ context.Context, eventType string, upgrade PendingUpgrade) {
	r.events = append(r.events, struct {
		eventType string
		upgrade   PendingUpgrade
	}{eventType: eventType, upgrade: upgrade})
}

func TestApplyIndexedEvent_UpgradeLifecycle(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	wasmHash := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	proposeEvt := indexedEvent{
		ID:         "evt-prop-1",
		ContractID: "CCWAMN2476B3R236R3T7G7M3MMK23R2J4S3I6W3M2A4J6S3I6W3M2A4J",
		EventType:  resolveTopicEventType([]interface{}{"UPGRADE", "PROP_UPG", "GADMIN"}),
		Ledger:     500,
		TxHash:     "tx-prop-1",
		Data: map[string]any{
			"wasm_hash": wasmHash,
			"eta":       "1792483200",
			"proposer":  "GADMIN",
		},
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_events").
		WithArgs(proposeEvt.ID, proposeEvt.Ledger).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO protocol_upgrades").
		WithArgs(proposeEvt.ContractID, wasmHash, int64(1792483200), "GADMIN", proposeEvt.Ledger, proposeEvt.TxHash).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	processed, err := applyIndexedEvent(context.Background(), db, proposeEvt)
	assert.NoError(t, err)
	assert.True(t, processed)

	obs := &recordingUpgradeObserver{}
	notifyUpgradeObserver(context.Background(), nil, obs, proposeEvt)
	if assert.Len(t, obs.events, 1) {
		assert.Equal(t, "PROP_UPG", obs.events[0].eventType)
		assert.Equal(t, wasmHash, obs.events[0].upgrade.WasmHash)
		assert.Equal(t, int64(1792483200), obs.events[0].upgrade.ETA)
		assert.Equal(t, "pending", obs.events[0].upgrade.Status)
	}

	cancelEvt := indexedEvent{
		ID:         "evt-can-1",
		ContractID: proposeEvt.ContractID,
		EventType:  resolveTopicEventType([]interface{}{"UPGRADE", "CAN_UPG", "GADMIN"}),
		Ledger:     510,
		Data: map[string]any{
			"wasm_hash":    wasmHash,
			"cancelled_by": "GADMIN",
		},
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_events").
		WithArgs(cancelEvt.ID, cancelEvt.Ledger).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE protocol_upgrades").
		WithArgs("GADMIN", cancelEvt.ContractID, wasmHash).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	processed, err = applyIndexedEvent(context.Background(), db, cancelEvt)
	assert.NoError(t, err)
	assert.True(t, processed)

	execEvt := indexedEvent{
		ID:         "evt-exec-1",
		ContractID: proposeEvt.ContractID,
		EventType:  resolveTopicEventType([]interface{}{"UPGRADE", "EXEC_UPG", "GADMIN"}),
		Ledger:     520,
		Data: map[string]any{
			"wasm_hash":           wasmHash,
			"executed_by":         "GADMIN",
			"execution_timestamp": "1792483300",
		},
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_events").
		WithArgs(execEvt.ID, execEvt.Ledger).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE protocol_upgrades").
		WithArgs("GADMIN", int64(1792483300), execEvt.ContractID, wasmHash).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	processed, err = applyIndexedEvent(context.Background(), db, execEvt)
	assert.NoError(t, err)
	assert.True(t, processed)
	assert.NoError(t, mock.ExpectationsWereMet())
}
