package service

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
	"github.com/suncrestlabs/nester/apps/api/internal/metrics"
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
	metrics     PollerMetricsRecorder
	lastTickEnd atomic.Int64 // unix nanos; observability hook
}

// PollerMetricsRecorder is the slice of metrics recording this poller needs
// (nester#1108). Declared here as an interface, matching the event indexer's
// approach, so the service package does not depend on the metrics package and
// tests can record calls without a Prometheus registry.
type PollerMetricsRecorder interface {
	RecordReconcileRun(outcome metrics.ReconcileOutcome)
	RecordReconcileDivergence(kind metrics.DivergenceKind)
	SetPendingSubmissions(count int, oldest time.Duration)
}

// noopPollerMetrics is used when no recorder is supplied, so Tick never has to
// nil-check.
type noopPollerMetrics struct{}

func (noopPollerMetrics) RecordReconcileRun(metrics.ReconcileOutcome)      {}
func (noopPollerMetrics) RecordReconcileDivergence(metrics.DivergenceKind) {}
func (noopPollerMetrics) SetPendingSubmissions(int, time.Duration)         {}

// SetMetrics wires metrics recording. Call before Run. A nil recorder leaves
// the poller unmetered rather than panicking, so a caller that has not built a
// registry still works.
func (p *TransactionPoller) SetMetrics(rec PollerMetricsRecorder) {
	if rec == nil {
		return
	}
	p.metrics = rec
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
		metrics: noopPollerMetrics{},
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
		// Recorded as a failed run rather than left silent (nester#1108): a
		// pass that could not list its work found no divergences because it
		// inspected nothing, which must not read as a clean result.
		p.metrics.RecordReconcileRun(metrics.ReconcileFailed)
		return
	}

	// Backlog depth and the age of its oldest entry, published once per pass.
	// The list is already filtered to transactions older than MinAge, so this
	// measures exactly the in-flight money the user cannot yet see.
	now := time.Now()
	var oldest time.Duration
	for _, tx := range pending {
		if age := now.Sub(tx.CreatedAt); age > oldest {
			oldest = age
		}
	}
	p.metrics.SetPendingSubmissions(len(pending), oldest)

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
			// Still not terminal after being old enough to poll: the chain
			// has not resolved it and neither have we. That is the "stuck"
			// discrepancy kind, and it is the shape a lost submission takes.
			p.metrics.RecordReconcileDivergence(metrics.DivergenceStuck)
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

	p.metrics.RecordReconcileRun(metrics.ReconcileCompleted)
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
