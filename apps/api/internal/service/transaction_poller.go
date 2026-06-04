package service

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
)

// TransactionStatusNotifier is invoked once per transaction whose status
// transitions to a terminal state during a poll. The production wiring in
// main.go broadcasts a WebSocket event; tests pass a recorder. Implementations
// must not block — the poller calls it inline on the tick goroutine.
type TransactionStatusNotifier func(ctx context.Context, tx transaction.Transaction)

// TransactionPollerConfig controls the background reconciliation loop. Fields
// are read once at construction; changes require a restart. Sourced from env in
// main.go (TX_POLLER_ENABLED, TX_POLLER_INTERVAL, TX_POLLER_MIN_AGE).
type TransactionPollerConfig struct {
	Enabled bool
	// Interval between poll ticks. Defaults to 15s when non-positive.
	Interval time.Duration
	// MinAge is the minimum age a transaction must reach before it is polled,
	// giving Horizon time to ingest a freshly submitted transaction. Defaults
	// to 30s when non-positive.
	MinAge time.Duration
}

const (
	defaultPollerInterval = 15 * time.Second
	defaultPollerMinAge   = 30 * time.Second
)

// TransactionPoller periodically reconciles pending transactions against
// Horizon so their status is updated even when the client never polls
// GET /api/v1/transactions/{hash}. Construct with NewTransactionPoller, then
// call Run from a goroutine alongside the API server; Run returns when the
// context is cancelled.
type TransactionPoller struct {
	cfg         TransactionPollerConfig
	service     *TransactionService
	notify      TransactionStatusNotifier
	logger      *slog.Logger
	lastTickEnd atomic.Int64 // unix nanos; observability hook
}

// NewTransactionPoller builds a poller. logger may be nil (a discarding logger
// is used). notify may be nil (status changes are persisted but not broadcast).
func NewTransactionPoller(cfg TransactionPollerConfig, svc *TransactionService, notify TransactionStatusNotifier, logger *slog.Logger) *TransactionPoller {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(pollerDiscardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	if notify == nil {
		notify = func(context.Context, transaction.Transaction) {}
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultPollerInterval
	}
	if cfg.MinAge <= 0 {
		cfg.MinAge = defaultPollerMinAge
	}
	return &TransactionPoller{
		cfg:     cfg,
		service: svc,
		notify:  notify,
		logger:  logger,
	}
}

// Run drives the loop until ctx is cancelled. When Config.Enabled is false, Run
// returns immediately so main.go can call it unconditionally.
func (p *TransactionPoller) Run(ctx context.Context) {
	if !p.cfg.Enabled {
		p.logger.Info("transaction poller disabled; not starting")
		return
	}
	p.logger.Info("transaction poller starting", "interval", p.cfg.Interval, "min_age", p.cfg.MinAge)

	// Run once immediately so a restart picks up any backlog without waiting
	// for the first tick.
	p.Tick(ctx)

	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			p.logger.Info("transaction poller stopping")
			return
		case <-ticker.C:
			p.Tick(ctx)
		}
	}
}

// Tick runs a single reconciliation pass: it lists pending transactions older
// than MinAge, checks each against Horizon, and notifies for any that reached a
// terminal state. A failure on one transaction is logged and skipped so the
// rest of the batch still makes progress. Exported for tests.
func (p *TransactionPoller) Tick(ctx context.Context) {
	defer p.lastTickEnd.Store(time.Now().UnixNano())

	pending, err := p.service.ListPendingOlderThan(ctx, p.cfg.MinAge)
	if err != nil {
		p.logger.Error("transaction poller: list pending failed", "error", err)
		return
	}

	for _, tx := range pending {
		updated, changed, err := p.service.ReconcileTransaction(ctx, tx)
		if err != nil {
			p.logger.Warn("transaction poller: reconcile failed",
				"tx_hash", tx.TxHash,
				"error", err,
			)
			continue
		}
		if !changed {
			continue
		}
		p.logger.Info("transaction poller: status reconciled",
			"tx_hash", updated.TxHash,
			"vault_id", updated.VaultID,
			"type", updated.Type,
			"status", updated.Status,
		)
		p.notify(ctx, updated)
	}
}

// LastTickEnd returns the wall-clock time of the last completed tick, or zero
// if the loop has not ticked yet.
func (p *TransactionPoller) LastTickEnd() time.Time {
	v := p.lastTickEnd.Load()
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(0, v)
}

// pollerDiscardWriter is the io.Writer slog fallback when NewTransactionPoller
// is called with a nil logger.
type pollerDiscardWriter struct{}

func (pollerDiscardWriter) Write(p []byte) (int, error) { return len(p), nil }
