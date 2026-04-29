package indexer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/suncrestlabs/nester/internal/stellar"
	"log/slog"
)

type Indexer struct {
	db      *sql.DB
	client  *stellar.Client
	logger  *slog.Logger
	poller  *stellar.EventPoller
	cursor  string
	backfill chan uint64
}

func New(db *sql.DB, client *stellar.Client, logger *slog.Logger) *Indexer {
	return &Indexer{
		db:       db,
		client:   client,
		logger:   logger.With("component", "indexer"),
		poller:   stellar.NewEventPoller(client),
		cursor:   "default",
		backfill: make(chan uint64, 10),
	}
}

func (idx *Indexer) Start(ctx context.Context) error {
	idx.logger.Info("starting stellar event indexer")

	// 1. Load cursor
	lastLedger, err := idx.loadCursor(ctx)
	if err != nil {
		return fmt.Errorf("load cursor: %w", err)
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case fromLedger := <-idx.backfill:
			if err := idx.processRange(ctx, fromLedger, lastLedger); err != nil {
				idx.logger.Error("backfill failed", "error", err, "from", fromLedger)
			}
		case <-ticker.C:
			// In a real scenario, we would get the latest ledger from RPC
			// For now, poll a slice of ledgers
			currentLedger := lastLedger + 100 // Simplified
			if err := idx.processRange(ctx, lastLedger+1, currentLedger); err != nil {
				idx.logger.Error("process range failed", "error", err, "from", lastLedger+1)
				continue
			}
			lastLedger = currentLedger
			if err := idx.saveCursor(ctx, lastLedger); err != nil {
				idx.logger.Error("save cursor failed", "error", err)
			}
		}
	}
}

func (idx *Indexer) Backfill(ledger uint64) {
	idx.backfill <- ledger
}

func (idx *Indexer) BackfillManual(ctx context.Context, from uint64) error {
	idx.logger.Info("starting manual backfill", "from", from)
	// Get current ledger for the end of the range
	// In a real scenario, we'd query the latest ledger from RPC
	to := from + 1000 // Simplified for demonstration
	return idx.processRange(ctx, from, to)
}

func (idx *Indexer) processRange(ctx context.Context, from, to uint64) error {
	idx.logger.Debug("processing range", "from", from, "to", to)

	// In nester, we track multiple contracts (Vaults, yield sources)
	// For simplicity, we poll all relevant contract events
	events, err := idx.poller.PollEvents(ctx, "*", from, to)
	if err != nil {
		return err
	}

	for _, ev := range events {
		if err := idx.persistEvent(ctx, ev); err != nil {
			return fmt.Errorf("persist event: %w", err)
		}
	}

	return nil
}

func (idx *Indexer) persistEvent(ctx context.Context, ev stellar.Event) error {
	data, err := json.Marshal(ev.Data)
	if err != nil {
		return err
	}

	_, err = idx.db.ExecContext(ctx, `
		INSERT INTO contract_events (contract_id, event_type, ledger, tx_hash, data)
		VALUES ($1, $2, $3, $4, $5)
	`, ev.ContractID, ev.EventType, ev.BlockNumber, ev.TransactionID, data)
	return err
}

func (idx *Indexer) loadCursor(ctx context.Context) (uint64, error) {
	var lastLedger uint64
	err := idx.db.QueryRowContext(ctx, "SELECT last_ledger FROM indexer_cursors WHERE id = $1", idx.cursor).Scan(&lastLedger)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return lastLedger, err
}

func (idx *Indexer) saveCursor(ctx context.Context, ledger uint64) error {
	_, err := idx.db.ExecContext(ctx, `
		INSERT INTO indexer_cursors (id, last_ledger, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (id) DO UPDATE SET last_ledger = EXCLUDED.last_ledger, updated_at = EXCLUDED.updated_at
	`, idx.cursor, ledger)
	return err
}
