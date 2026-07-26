package stellar

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

var (
	ErrReorgDetected     = errors.New("ledger reorganization detected")
	ErrReorgTooDeep      = errors.New("reorg depth exceeds tolerance")
	ErrDuplicateEvent    = errors.New("duplicate event")
	ErrCheckpointMismatch = errors.New("checkpoint hash mismatch")
)

const (
	MaxReorgDepth         = 10
	FinalizationThreshold = 5
)

type LedgerCheckpoint struct {
	Sequence     uint64
	Hash         string
	ParentHash   string
	ProcessedAt  time.Time
	IsFinalized  bool
}

type EventKey struct {
	LedgerSequence uint64
	TxHash         string
	EventIndex     int
}

type EventHandler func(ctx context.Context, tx *sql.Tx, event indexedEvent) error

type EventDispatcher struct {
	handlers map[string]EventHandler
	mu       sync.RWMutex
}

func NewEventDispatcher() *EventDispatcher {
	return &EventDispatcher{
		handlers: make(map[string]EventHandler),
	}
}

func (d *EventDispatcher) Register(eventType string, handler EventHandler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[eventType] = handler
}

func (d *EventDispatcher) Dispatch(ctx context.Context, tx *sql.Tx, event indexedEvent) error {
	d.mu.RLock()
	handler, exists := d.handlers[event.EventType]
	d.mu.RUnlock()

	if !exists {
		return nil
	}

	return handler(ctx, tx, event)
}

type ReorgSafeIndexer struct {
	db         *sql.DB
	logger     *slog.Logger
	dispatcher *EventDispatcher
	mu         sync.Mutex
}

func NewReorgSafeIndexer(db *sql.DB, logger *slog.Logger) *ReorgSafeIndexer {
	return &ReorgSafeIndexer{
		db:         db,
		logger:     logger,
		dispatcher: NewEventDispatcher(),
	}
}

func (i *ReorgSafeIndexer) Dispatcher() *EventDispatcher {
	return i.dispatcher
}

func (i *ReorgSafeIndexer) ProcessBatch(ctx context.Context, events []indexedEvent, ledgerSeq uint64, ledgerHash string, parentHash string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if err := i.verifyParentHash(ctx, ledgerSeq, parentHash); err != nil {
		if errors.Is(err, ErrReorgDetected) {
			return i.handleReorg(ctx, ledgerSeq, parentHash)
		}
		return err
	}

	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for idx, event := range events {
		eventKey := EventKey{
			LedgerSequence: ledgerSeq,
			TxHash:         event.ID,
			EventIndex:     idx,
		}

		inserted, err := i.insertDedupKey(ctx, tx, eventKey)
		if err != nil {
			return fmt.Errorf("insert dedup: %w", err)
		}

		if !inserted {
			continue
		}

		if err := i.dispatcher.Dispatch(ctx, tx, event); err != nil {
			return fmt.Errorf("dispatch event %s: %w", event.ID, err)
		}
	}

	if err := i.saveCheckpoint(ctx, tx, ledgerSeq, ledgerHash, parentHash); err != nil {
		return fmt.Errorf("save checkpoint: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

func (i *ReorgSafeIndexer) verifyParentHash(ctx context.Context, ledgerSeq uint64, parentHash string) error {
	if ledgerSeq == 0 {
		return nil
	}

	var storedHash string
	err := i.db.QueryRowContext(
		ctx,
		`SELECT ledger_hash FROM ledger_checkpoints WHERE ledger_sequence = $1`,
		ledgerSeq-1,
	).Scan(&storedHash)

	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	if storedHash != parentHash {
		return ErrReorgDetected
	}

	return nil
}

func (i *ReorgSafeIndexer) handleReorg(ctx context.Context, currentLedger uint64, parentHash string) error {
	i.logger.Warn("reorg detected", "current_ledger", currentLedger, "expected_parent", parentHash)

	var forkPoint uint64
	for depth := uint64(1); depth <= MaxReorgDepth; depth++ {
		checkLedger := currentLedger - depth
		if checkLedger == 0 {
			break
		}

		var storedHash string
		err := i.db.QueryRowContext(
			ctx,
			`SELECT ledger_hash FROM ledger_checkpoints WHERE ledger_sequence = $1`,
			checkLedger,
		).Scan(&storedHash)

		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}

		forkPoint = checkLedger
		break
	}

	if forkPoint == 0 {
		return ErrReorgTooDeep
	}

	i.logger.Info("reverting to fork point", "fork_point", forkPoint, "depth", currentLedger-forkPoint)

	if err := i.revertToCheckpoint(ctx, forkPoint); err != nil {
		return fmt.Errorf("revert to checkpoint: %w", err)
	}

	return nil
}

func (i *ReorgSafeIndexer) revertToCheckpoint(ctx context.Context, forkPoint uint64) error {
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(
		ctx,
		`DELETE FROM processed_events WHERE ledger_sequence > $1`,
		forkPoint,
	)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(
		ctx,
		`DELETE FROM ledger_checkpoints WHERE ledger_sequence > $1`,
		forkPoint,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (i *ReorgSafeIndexer) insertDedupKey(ctx context.Context, tx *sql.Tx, key EventKey) (bool, error) {
	query := `
		INSERT INTO processed_events (event_id, ledger_sequence, tx_hash, event_index, processed_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (ledger_sequence, tx_hash, event_index) WHERE ledger_sequence IS NOT NULL AND tx_hash IS NOT NULL
		DO NOTHING
		RETURNING event_id
	`

	var eventID string
	err := tx.QueryRowContext(ctx, query, key.TxHash, key.LedgerSequence, key.TxHash, key.EventIndex).Scan(&eventID)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

func (i *ReorgSafeIndexer) saveCheckpoint(ctx context.Context, tx *sql.Tx, ledgerSeq uint64, ledgerHash string, parentHash string) error {
	query := `
		INSERT INTO ledger_checkpoints (ledger_sequence, ledger_hash, parent_hash, processed_at, is_finalized)
		VALUES ($1, $2, $3, NOW(), $4)
		ON CONFLICT (ledger_sequence) DO UPDATE
		SET ledger_hash = EXCLUDED.ledger_hash,
		    parent_hash = EXCLUDED.parent_hash,
		    processed_at = EXCLUDED.processed_at,
		    is_finalized = EXCLUDED.is_finalized
	`

	isFinalized := false
	if ledgerSeq > FinalizationThreshold {
		var oldestUnfinalized uint64
		err := tx.QueryRowContext(
			ctx,
			`SELECT COALESCE(MIN(ledger_sequence), $1) FROM ledger_checkpoints WHERE NOT is_finalized`,
			ledgerSeq,
		).Scan(&oldestUnfinalized)

		if err != nil {
			return err
		}

		if ledgerSeq-oldestUnfinalized >= FinalizationThreshold {
			isFinalized = true

			_, err = tx.ExecContext(
				ctx,
				`UPDATE ledger_checkpoints SET is_finalized = TRUE WHERE ledger_sequence <= $1 AND NOT is_finalized`,
				ledgerSeq-FinalizationThreshold,
			)
			if err != nil {
				return err
			}
		}
	}

	_, err := tx.ExecContext(ctx, query, ledgerSeq, ledgerHash, parentHash, isFinalized)
	return err
}

func (i *ReorgSafeIndexer) GetLastCheckpoint(ctx context.Context) (*LedgerCheckpoint, error) {
	query := `
		SELECT ledger_sequence, ledger_hash, parent_hash, processed_at, is_finalized
		FROM ledger_checkpoints
		ORDER BY ledger_sequence DESC
		LIMIT 1
	`

	var checkpoint LedgerCheckpoint
	err := i.db.QueryRowContext(ctx, query).Scan(
		&checkpoint.Sequence,
		&checkpoint.Hash,
		&checkpoint.ParentHash,
		&checkpoint.ProcessedAt,
		&checkpoint.IsFinalized,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &checkpoint, nil
}

func (i *ReorgSafeIndexer) GetIndexerLag(ctx context.Context, currentNetworkLedger uint64) (uint64, error) {
	checkpoint, err := i.GetLastCheckpoint(ctx)
	if err != nil {
		return 0, err
	}

	if checkpoint == nil {
		return currentNetworkLedger, nil
	}

	if currentNetworkLedger > checkpoint.Sequence {
		return currentNetworkLedger - checkpoint.Sequence, nil
	}

	return 0, nil
}
