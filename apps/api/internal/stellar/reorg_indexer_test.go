package stellar

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func setupReorgTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("pgx", "postgres://postgres:postgres@localhost:5432/nester_test?sslmode=disable")
	if err != nil {
		t.Skip("postgres not available:", err)
	}

	if err := db.Ping(); err != nil {
		t.Skip("postgres not reachable:", err)
	}

	return db
}

func TestReorgSafeIndexer_ProcessBatch_NoReorg(t *testing.T) {
	db := setupReorgTestDB(t)
	defer db.Close()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	indexer := NewReorgSafeIndexer(db, log)

	indexer.Dispatcher().Register("deposit", func(ctx context.Context, tx *sql.Tx, event indexedEvent) error {
		return nil
	})

	ctx := context.Background()

	events := []indexedEvent{
		{
			ID:         "tx1",
			ContractID: "contract1",
			EventType:  "deposit",
			Ledger:     100,
			Data:       map[string]any{"amount": "1000"},
		},
	}

	err := indexer.ProcessBatch(ctx, events, 100, "hash100", "hash99")
	if err != nil {
		t.Fatalf("process batch: %v", err)
	}

	checkpoint, err := indexer.GetLastCheckpoint(ctx)
	if err != nil {
		t.Fatalf("get checkpoint: %v", err)
	}

	if checkpoint == nil {
		t.Fatal("expected checkpoint")
	}

	if checkpoint.Sequence != 100 {
		t.Errorf("expected sequence 100, got %d", checkpoint.Sequence)
	}

	if checkpoint.Hash != "hash100" {
		t.Errorf("expected hash hash100, got %s", checkpoint.Hash)
	}
}

func TestReorgSafeIndexer_ProcessBatch_DuplicateEvent(t *testing.T) {
	db := setupReorgTestDB(t)
	defer db.Close()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	indexer := NewReorgSafeIndexer(db, log)

	handlerCalls := 0
	indexer.Dispatcher().Register("deposit", func(ctx context.Context, tx *sql.Tx, event indexedEvent) error {
		handlerCalls++
		return nil
	})

	ctx := context.Background()

	events := []indexedEvent{
		{
			ID:         "tx1",
			ContractID: "contract1",
			EventType:  "deposit",
			Ledger:     100,
			Data:       map[string]any{"amount": "1000"},
		},
	}

	err := indexer.ProcessBatch(ctx, events, 100, "hash100", "hash99")
	if err != nil {
		t.Fatalf("first batch: %v", err)
	}

	if handlerCalls != 1 {
		t.Errorf("expected 1 handler call, got %d", handlerCalls)
	}

	err = indexer.ProcessBatch(ctx, events, 100, "hash100", "hash99")
	if err != nil {
		t.Fatalf("second batch: %v", err)
	}

	if handlerCalls != 1 {
		t.Errorf("expected handler not called on duplicate, got %d calls", handlerCalls)
	}
}

func TestReorgSafeIndexer_GetIndexerLag(t *testing.T) {
	db := setupReorgTestDB(t)
	defer db.Close()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	indexer := NewReorgSafeIndexer(db, log)

	ctx := context.Background()

	lag, err := indexer.GetIndexerLag(ctx, 100)
	if err != nil {
		t.Fatalf("get lag: %v", err)
	}

	if lag != 100 {
		t.Errorf("expected lag 100 (no checkpoint), got %d", lag)
	}

	events := []indexedEvent{
		{
			ID:         "tx1",
			ContractID: "contract1",
			EventType:  "deposit",
			Ledger:     90,
			Data:       map[string]any{},
		},
	}

	err = indexer.ProcessBatch(ctx, events, 90, "hash90", "hash89")
	if err != nil {
		t.Fatalf("process batch: %v", err)
	}

	lag, err = indexer.GetIndexerLag(ctx, 100)
	if err != nil {
		t.Fatalf("get lag: %v", err)
	}

	if lag != 10 {
		t.Errorf("expected lag 10, got %d", lag)
	}
}

func TestEventDispatcher_Register(t *testing.T) {
	dispatcher := NewEventDispatcher()

	called := false
	handler := func(ctx context.Context, tx *sql.Tx, event indexedEvent) error {
		called = true
		return nil
	}

	dispatcher.Register("test_event", handler)

	ctx := context.Background()
	event := indexedEvent{
		ID:         "tx1",
		ContractID: "contract1",
		EventType:  "test_event",
		Ledger:     100,
		Data:       map[string]any{},
	}

	err := dispatcher.Dispatch(ctx, nil, event)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if !called {
		t.Error("expected handler to be called")
	}
}

func TestEventDispatcher_UnregisteredEvent(t *testing.T) {
	dispatcher := NewEventDispatcher()

	ctx := context.Background()
	event := indexedEvent{
		ID:         "tx1",
		ContractID: "contract1",
		EventType:  "unknown_event",
		Ledger:     100,
		Data:       map[string]any{},
	}

	err := dispatcher.Dispatch(ctx, nil, event)
	if err != nil {
		t.Fatalf("dispatch unregistered event should not error: %v", err)
	}
}
