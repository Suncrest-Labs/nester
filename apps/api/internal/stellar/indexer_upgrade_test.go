package stellar

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/systemstate"
)

func readPrometheusCounter(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, c.Write(&m))
	return m.GetCounter().GetValue()
}

func TestApplyIndexedEventWithCursor_UnknownEventHaltsCursorAndIncrementsMetric(t *testing.T) {
	resetIndexerDecodeFailuresForTest()
	beforeMetric := readPrometheusCounter(t, IndexerDecodeFailuresTotal)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	event := indexedEvent{
		ID:         "evt-unknown-1",
		ContractID: "CVAULT1",
		EventType:  "future_v2_event",
		Ledger:     500,
		Data: map[string]any{
			"unexpected_field": "123",
		},
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_events").
		WithArgs(event.ID, event.Ledger).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectRollback()

	applied, err := applyIndexedEventWithCursor(context.Background(), db, event)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownEventSchema)
	assert.False(t, applied)

	afterMetric := readPrometheusCounter(t, IndexerDecodeFailuresTotal)
	assert.Equal(t, beforeMetric+1, afterMetric)
	assert.Equal(t, uint64(1), IndexerDecodeFailuresCount())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestPollEvents_UnknownEventShapeHaltsCursor(t *testing.T) {
	resetIndexerDecodeFailuresForTest()
	beforeMetric := readPrometheusCounter(t, IndexerDecodeFailuresTotal)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	sysRepo := newStubSysRepo()
	sysRepo.values[systemstate.KeyLastLedger] = "100"

	// Query vault contracts
	mock.ExpectQuery("SELECT DISTINCT contract_address FROM vaults").
		WillReturnRows(sqlmock.NewRows([]string{"contract_address"}).AddRow("CVAULT1"))

	// First event (deposit at ledger 101) succeeds and advances cursor to 101
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_events").
		WithArgs("evt-101", uint64(101)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE vaults").
		WithArgs("10", "CVAULT1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO system_state").
		WithArgs(systemstate.KeyLastLedger, "101").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	// Second event (unknown schema at ledger 102) fails and rolls back without advancing cursor to 102
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_events").
		WithArgs("evt-102-unknown", uint64(102)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectRollback()

	fetcher := &scriptedFetcher{
		tip: 110,
		events: []indexedEvent{
			{
				ID:         "evt-101",
				ContractID: "CVAULT1",
				EventType:  "deposit",
				Ledger:     101,
				Data:       map[string]any{"amount": json.Number("100000000")},
			},
			{
				ID:         "evt-102-unknown",
				ContractID: "CVAULT1",
				EventType:  "v2_weird_topic",
				Ledger:     102,
				Data:       map[string]any{"foo": "bar"},
			},
			{
				ID:         "evt-103",
				ContractID: "CVAULT1",
				EventType:  "deposit",
				Ledger:     103,
				Data:       map[string]any{"amount": json.Number("200000000")},
			},
		},
	}

	poller := &EventPoller{
		DB:      db,
		SysRepo: sysRepo,
		Fetcher: fetcher,
	}

	applied, err := poller.PollEvents(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownEventSchema)
	assert.Equal(t, 1, applied)

	afterMetric := readPrometheusCounter(t, IndexerDecodeFailuresTotal)
	assert.Equal(t, beforeMetric+1, afterMetric)
	assert.Equal(t, uint64(1), IndexerDecodeFailuresCount())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyIndexedEvent_MalformedDepositHaltsAndIncrementsMetric(t *testing.T) {
	resetIndexerDecodeFailuresForTest()
	beforeMetric := readPrometheusCounter(t, IndexerDecodeFailuresTotal)

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	event := indexedEvent{
		ID:         "evt-bad-deposit",
		ContractID: "CVAULT1",
		EventType:  "deposit",
		Ledger:     200,
		Data: map[string]any{
			"new_schema_amount": "100000000",
		},
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_events").
		WithArgs(event.ID, event.Ledger).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectRollback()

	processed, err := applyIndexedEvent(context.Background(), db, event)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEventSchemaDecode)
	assert.False(t, processed)

	afterMetric := readPrometheusCounter(t, IndexerDecodeFailuresTotal)
	assert.Equal(t, beforeMetric+1, afterMetric)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyIndexedEvent_UpgradeEventUpdatesStoredWasmHash(t *testing.T) {
	cases := []struct {
		name      string
		eventType string
		data      map[string]any
		wantHash  string
	}{
		{
			name:      "EXEC_UPG updates stored wasm hash",
			eventType: "EXEC_UPG",
			data: map[string]any{
				"wasm_hash":           "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899",
				"executed_by":         "GADMIN123",
				"execution_timestamp": json.Number("1710000000"),
			},
			wantHash: "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899",
		},
		{
			name:      "UPGRADED updates stored wasm hash",
			eventType: "UPGRADED",
			data: map[string]any{
				"wasm_hash": "11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff",
			},
			wantHash: "11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()

			event := indexedEvent{
				ID:         "evt-upgrade-" + tc.eventType,
				ContractID: "CVAULT_UPGRADE_1",
				EventType:  tc.eventType,
				Ledger:     305,
				Data:       tc.data,
			}

			mock.ExpectBegin()
			mock.ExpectExec("INSERT INTO processed_events").
				WithArgs(event.ID, event.Ledger).
				WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec("INSERT INTO system_state").
				WithArgs(ContractWasmHashStateKey("CVAULT_UPGRADE_1"), tc.wantHash).
				WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec("INSERT INTO system_state").
				WithArgs(systemstate.KeyLastLedger, "305").
				WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()

			processed, err := applyIndexedEventWithCursor(context.Background(), db, event)
			require.NoError(t, err)
			assert.True(t, processed)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestExtractEventTypeFromTopics_HandlesThreeLevelUpgradeTopics(t *testing.T) {
	assert.Equal(t, "EXEC_UPG", extractEventTypeFromTopics([]any{"UPGRADE", "EXEC_UPG", "GADMIN"}))
	assert.Equal(t, "UPGRADED", extractEventTypeFromTopics([]any{"NESTER", "UPGRADED", "GADMIN"}))
	assert.Equal(t, "DEPOSIT", extractEventTypeFromTopics([]any{"VAULT", "DEPOSIT", "GUSER"}))
	assert.Equal(t, "deposit", extractEventTypeFromTopics([]any{"deposit", "GUSER"}))
}
