package stellar

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
)

type recordingDepositObserver struct {
	deposits []ConfirmedDeposit
}

func (r *recordingDepositObserver) OnConfirmedDeposit(_ context.Context, deposit ConfirmedDeposit) {
	r.deposits = append(r.deposits, deposit)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestNotifyDepositObserver_FiresForDeposit(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	closedAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	event := indexedEvent{
		ID:             "evt-1",
		ContractID:     "C1",
		EventType:      "deposit",
		Data:           map[string]any{"amount": "102500000"}, // 10.25 units in stroops
		LedgerClosedAt: closedAt,
	}

	mock.ExpectQuery("SELECT user_id FROM vaults").
		WithArgs(event.ContractID).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow("owner-123"))

	observer := &recordingDepositObserver{}
	notifyDepositObserver(context.Background(), db, discardLogger(), observer, event)

	assert.NoError(t, mock.ExpectationsWereMet())
	if assert.Len(t, observer.deposits, 1) {
		d := observer.deposits[0]
		assert.Equal(t, "owner-123", d.VaultUserID)
		assert.Equal(t, "evt-1", d.EventID)
		assert.Equal(t, "10.25", d.AmountUnits)
		assert.True(t, d.OccurredAt.Equal(closedAt))
	}
}

func TestNotifyDepositObserver_FallsBackToNowWithoutLedgerClosedAt(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	event := indexedEvent{
		ID:         "evt-1",
		ContractID: "C1",
		EventType:  "deposit",
		Data:       map[string]any{"amount": "100000000"},
	}

	mock.ExpectQuery("SELECT user_id FROM vaults").
		WithArgs(event.ContractID).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow("owner-123"))

	before := time.Now().UTC()
	observer := &recordingDepositObserver{}
	notifyDepositObserver(context.Background(), db, discardLogger(), observer, event)
	after := time.Now().UTC()

	if assert.Len(t, observer.deposits, 1) {
		occurred := observer.deposits[0].OccurredAt
		assert.False(t, occurred.Before(before))
		assert.False(t, occurred.After(after))
	}
}

func TestNotifyDepositObserver_IgnoresNonDepositEvents(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	event := indexedEvent{ID: "evt-1", ContractID: "C1", EventType: "withdraw", Data: map[string]any{"amount": "1"}}

	observer := &recordingDepositObserver{}
	notifyDepositObserver(context.Background(), db, discardLogger(), observer, event)

	assert.Empty(t, observer.deposits)
	assert.NoError(t, mock.ExpectationsWereMet()) // no query expected, none should have run
}

func TestNotifyDepositObserver_NilObserverIsNoop(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	event := indexedEvent{ID: "evt-1", ContractID: "C1", EventType: "deposit", Data: map[string]any{"amount": "1"}}

	// Must not panic, and must not touch the DB.
	notifyDepositObserver(context.Background(), db, discardLogger(), nil, event)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestNotifyDepositObserver_NoLiveVaultIsSkippedAndLogged(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	event := indexedEvent{ID: "evt-1", ContractID: "C-deleted", EventType: "deposit", Data: map[string]any{"amount": "1"}}

	mock.ExpectQuery("SELECT user_id FROM vaults").
		WithArgs(event.ContractID).
		WillReturnError(sql.ErrNoRows)

	observer := &recordingDepositObserver{}
	notifyDepositObserver(context.Background(), db, discardLogger(), observer, event)

	assert.Empty(t, observer.deposits)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestNotifyDepositObserver_MissingAmountIsSkipped(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	event := indexedEvent{ID: "evt-1", ContractID: "C1", EventType: "deposit", Data: map[string]any{}}

	observer := &recordingDepositObserver{}
	notifyDepositObserver(context.Background(), db, discardLogger(), observer, event)

	assert.Empty(t, observer.deposits)
	assert.NoError(t, mock.ExpectationsWereMet()) // no query expected: bailed out before the lookup
}

// panickingObserver proves an observer panic is contained and does not
// propagate to the caller (the poller/sync loop).
type panickingObserver struct{}

func (panickingObserver) OnConfirmedDeposit(context.Context, ConfirmedDeposit) {
	panic("boom")
}

func TestNotifyDepositObserver_PanicIsContained(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	event := indexedEvent{ID: "evt-1", ContractID: "C1", EventType: "deposit", Data: map[string]any{"amount": "1"}}

	mock.ExpectQuery("SELECT user_id FROM vaults").
		WithArgs(event.ContractID).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow("owner-123"))

	assert.NotPanics(t, func() {
		notifyDepositObserver(context.Background(), db, discardLogger(), panickingObserver{}, event)
	})
}

func TestNormalizeEventTypeString_MatchesMutationSwitch(t *testing.T) {
	assert.Equal(t, "deposit", normalizeEventTypeString(" Deposit "))
	assert.Equal(t, "deposit", normalizeEventTypeString("DEPOSIT"))
	assert.Equal(t, "withdraw", normalizeEventTypeString("Withdraw"))
}

func TestFetchSorobanEvents_ParsesLedgerClosedAt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"latestLedger": 200,
				"events": []map[string]any{
					{
						"id":             "evt-1",
						"contractId":     "C1",
						"ledger":         100,
						"txHash":         "tx1",
						"topic":          []string{"deposit"},
						"value":          map[string]any{"amount": "100"},
						"ledgerClosedAt": "2026-03-01T12:00:00Z",
					},
				},
			},
		})
	}))
	defer server.Close()

	rpc := newRPCClient(server.URL, nil, RPCOptions{}, false)
	events, latest, err := fetchSorobanEvents(context.Background(), rpc, []string{"C1"}, 1)
	assert.NoError(t, err)
	assert.Equal(t, uint64(200), latest)
	if assert.Len(t, events, 1) {
		want := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
		assert.True(t, events[0].LedgerClosedAt.Equal(want))
	}
}
