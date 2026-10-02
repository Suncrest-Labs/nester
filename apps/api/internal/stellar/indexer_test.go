package stellar

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

// TestStartEventIndexer_JoinsWaitGroupOnCancellation is the issue #786
// regression test: the caller's shutdown WaitGroup must not return from
// Wait() until the indexer's own goroutine has actually observed context
// cancellation and returned, not merely had its context cancelled.
func TestStartEventIndexer_JoinsWaitGroupOnCancellation(t *testing.T) {
	db, _, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	sysRepo := newStubSysRepo()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	StartEventIndexer(ctx, logger, db, sysRepo, IndexerOptions{
		RPCURL: "https://example.invalid/soroban/rpc",
	}, &wg)

	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// The goroutine observed ctx.Done() and returned before its next
		// tick, exactly as PollEvents' per-event transaction guarantees
		// require: shutdown must not race an in-flight poll.
	case <-time.After(2 * time.Second):
		t.Fatal("wg.Wait() did not return after the indexer's context was cancelled")
	}
}

// TestStartEventIndexer_NilWaitGroupIsSafe confirms the nil-wg path (every
// call site that has no shutdown coordination to do) still starts and stops
// cleanly rather than nil-pointer panicking on wg.Add/wg.Done.
func TestStartEventIndexer_NilWaitGroupIsSafe(t *testing.T) {
	db, _, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	sysRepo := newStubSysRepo()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	assert.NotPanics(t, func() {
		StartEventIndexer(ctx, logger, db, sysRepo, IndexerOptions{
			RPCURL: "https://example.invalid/soroban/rpc",
		}, nil)
	})
}

// TestStartEventIndexer_EmptyRPCURLDisablesIndexer confirms the disabled
// path (no RPC configured) still accepts a wg without leaking an Add(1)
// that is never matched by a Done(), which would hang any caller's
// wg.Wait() forever.
func TestStartEventIndexer_EmptyRPCURLDisablesIndexer(t *testing.T) {
	db, _, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	sysRepo := newStubSysRepo()

	var wg sync.WaitGroup
	StartEventIndexer(context.Background(), logger, db, sysRepo, IndexerOptions{
		RPCURL: "",
	}, &wg)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("wg.Wait() hung: StartEventIndexer must not Add(1) without a matching Done() when disabled")
	}
}

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
			// Stroops, as the contract emits them: 10.25 asset units.
			"amount": "102500000",
		},
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_events").
		WithArgs(event.ID, event.Ledger).
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
	mock.ExpectExec("INSERT INTO processed_events").
		WithArgs(event.ID, event.Ledger).
		WillReturnResult(sqlmock.NewResult(1, 0))
	mock.ExpectCommit()

	processed, err := applyIndexedEvent(context.Background(), db, event)
	assert.NoError(t, err)
	assert.False(t, processed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestExtractEventAmount_JSONNumber(t *testing.T) {
	amount, ok := extractEventAmountStroops(indexedEvent{
		Data: map[string]any{
			"amount": json.Number("123456789012345678901234567890"),
		},
	})
	assert.True(t, ok)
	assert.True(t, amount.Equal(decimal.RequireFromString("123456789012345678901234567890")))
}

func TestExtractEventAmount_LargeAmountPrecision(t *testing.T) {
	// 1e18 stroops is far above float64's exact-integer limit (2^53 ~ 9.007e15).
	const bigStroops = "1000000000000000000"

	t.Run("json.Number preserves precision", func(t *testing.T) {
		ev := indexedEvent{Data: map[string]any{"amount": json.Number(bigStroops)}}
		got, ok := extractEventAmountStroops(ev)
		assert.True(t, ok)
		assert.Equal(t, bigStroops, got.String())
	})

	t.Run("string preserves precision", func(t *testing.T) {
		ev := indexedEvent{Data: map[string]any{"value": bigStroops}}
		got, ok := extractEventAmountStroops(ev)
		assert.True(t, ok)
		assert.Equal(t, bigStroops, got.String())
	})

	t.Run("float64 above 2^53 is rejected instead of silently truncated", func(t *testing.T) {
		ev := indexedEvent{Data: map[string]any{"amount": float64(1e18)}}
		_, ok := extractEventAmountStroops(ev)
		assert.False(t, ok)
	})

	t.Run("safe float64 integer is still accepted", func(t *testing.T) {
		ev := indexedEvent{Data: map[string]any{"amount": float64(1500)}}
		got, ok := extractEventAmountStroops(ev)
		assert.True(t, ok)
		assert.Equal(t, "1500", got.String())
	})
}

func TestApplyIndexedEvent_EmergencyRequested_UpsertsQueueEntry(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	event := indexedEvent{
		ID:         "evt-emrg-reqd",
		ContractID: "CVAULT1",
		EventType:  "emrg_reqd",
		Ledger:     10,
		Data: map[string]any{
			"user":             "GUSER1",
			"seq":              json.Number("3"),
			"shares_requested": json.Number("10000000"),
		},
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_events").
		WithArgs(event.ID, event.Ledger).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO emergency_withdrawal_queue").
		WithArgs("CVAULT1", "GUSER1", "3", "10000000").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	processed, err := applyIndexedEvent(context.Background(), db, event)
	assert.NoError(t, err)
	assert.True(t, processed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyIndexedEvent_PenaltyCharged_PersistsReason(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	event := indexedEvent{
		ID:         "evt-pnlty-chg",
		ContractID: "CVAULT1",
		EventType:  "pnlty_chg",
		Ledger:     11,
		Data: map[string]any{
			"user":          "GUSER2",
			"amount":        json.Number("500000"),
			"shares_burned": json.Number("10000000"),
			"reason":        []any{"EmergencyExit"},
		},
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_events").
		WithArgs(event.ID, event.Ledger).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO penalty_events").
		WithArgs("CVAULT1", "GUSER2", "500000", "10000000", "emergency_exit", event.Ledger).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	processed, err := applyIndexedEvent(context.Background(), db, event)
	assert.NoError(t, err)
	assert.True(t, processed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyIndexedEvent_RebalanceLegExecuted_Persists(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer db.Close()

	event := indexedEvent{
		ID:         "evt-rebal-leg",
		ContractID: "CVAULT1",
		EventType:  "rebal_leg",
		Ledger:     12,
		Data: map[string]any{
			"source_id":  "aave",
			"delta":      json.Number("-4000000"),
			"amount_out": json.Number("4000000"),
			"min_out":    json.Number("3900000"),
		},
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO processed_events").
		WithArgs(event.ID, event.Ledger).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO vault_rebalance_legs").
		WithArgs("CVAULT1", "aave", "-4000000", "4000000", "3900000", event.Ledger).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	processed, err := applyIndexedEvent(context.Background(), db, event)
	assert.NoError(t, err)
	assert.True(t, processed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestExtractEventEnumVariant_HandlesMapShape(t *testing.T) {
	variant, ok := extractEventEnumVariant(indexedEvent{
		Data: map[string]any{"reason": map[string]any{"LockBreak": nil}},
	}, "reason")
	assert.True(t, ok)
	assert.Equal(t, "LockBreak", variant)
}
