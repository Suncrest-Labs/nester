package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/metrics"
)

// CanaryConfig controls the synthetic deposit/withdraw probe (nester#1390).
//
// The canary performs an actual on-chain deposit followed by an immediate
// withdrawal using a dedicated canary vault and a pre-funded canary account.
// It runs on a fixed schedule and records the round-trip latency and outcome
// to Prometheus so an alert can fire before users report problems.
//
// Sourced from env in main.go:
//
//	CANARY_ENABLED=true
//	CANARY_INTERVAL=5m
//	CANARY_VAULT_ID=<uuid of the dedicated canary vault>
//	CANARY_AMOUNT=0.01         # USDC amount to cycle per probe
//	CANARY_LATENCY_THRESHOLD=60s  # probe alerts above this duration
type CanaryConfig struct {
	Enabled           bool
	Interval          time.Duration
	VaultID           uuid.UUID
	Amount            string
	LatencyThreshold  time.Duration
}

// CanaryInvoker is the minimal chain interface the canary needs.
// Production wiring uses service.VaultService; tests pass a fake.
type CanaryInvoker interface {
	// CanaryDeposit performs a minimal deposit and returns the transaction hash.
	CanaryDeposit(ctx context.Context, vaultID uuid.UUID, amount string) (txHash string, err error)
	// CanaryWithdraw performs a full withdrawal of the just-deposited amount and
	// returns the transaction hash.
	CanaryWithdraw(ctx context.Context, vaultID uuid.UUID, txHash string) (string, error)
}

// CanaryJob is the long-lived synthetic probe. Construct with NewCanaryJob
// and call Run from a goroutine.
type CanaryJob struct {
	cfg     CanaryConfig
	invoker CanaryInvoker
	metrics *metrics.Metrics
	logger  *slog.Logger
	leader  LeaderChecker
}

// NewCanaryJob constructs the canary job. logger and m may be nil —
// a discarding logger and a nil metrics recorder are used in that case so
// the job never panics in test configurations.
func NewCanaryJob(cfg CanaryConfig, invoker CanaryInvoker, m *metrics.Metrics, logger *slog.Logger) *CanaryJob {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &CanaryJob{
		cfg:     cfg,
		invoker: invoker,
		metrics: m,
		logger:  logger,
	}
}

// SetLeaderChecker wires leader election into the canary so only one
// replica executes the real on-chain probe. The canary moves real money
// (small amount, but real), so it must be a SINGLETON like the rebalancer.
func (j *CanaryJob) SetLeaderChecker(l LeaderChecker) { j.leader = l }

func (j *CanaryJob) isLeader() bool {
	return j.leader == nil || j.leader.IsLeader()
}

// Run drives the probe loop until ctx is cancelled. When Enabled is false,
// Run returns immediately.
func (j *CanaryJob) Run(ctx context.Context) {
	if !j.cfg.Enabled {
		j.logger.Info("canary probe disabled")
		return
	}
	interval := j.cfg.Interval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	j.logger.Info("canary probe starting", "interval", interval, "vault_id", j.cfg.VaultID)
	j.probe(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			j.logger.Info("canary probe stopping")
			return
		case <-ticker.C:
			j.probe(ctx)
		}
	}
}

// probe executes a single deposit→withdraw round trip and records the result.
func (j *CanaryJob) probe(ctx context.Context) {
	if !j.isLeader() {
		return
	}

	start := time.Now()

	depositHash, withdrawHash, err := j.roundTrip(ctx)
	elapsed := time.Since(start)

	if err != nil {
		j.logger.Error("canary probe failed",
			"error", err,
			"elapsed_ms", elapsed.Milliseconds(),
			"vault_id", j.cfg.VaultID,
		)
		j.metrics.RecordCanaryProbe(metrics.CanaryOutcomeFailed, elapsed)
		return
	}

	threshold := j.cfg.LatencyThreshold
	if threshold <= 0 {
		threshold = 60 * time.Second
	}

	if elapsed > threshold {
		j.logger.Warn("canary probe succeeded but exceeded latency threshold",
			"elapsed_ms", elapsed.Milliseconds(),
			"threshold_ms", threshold.Milliseconds(),
			"deposit_tx", depositHash,
			"withdraw_tx", withdrawHash,
			"vault_id", j.cfg.VaultID,
		)
		j.metrics.RecordCanaryProbe(metrics.CanaryOutcomeSlow, elapsed)
		return
	}

	j.logger.Info("canary probe succeeded",
		"elapsed_ms", elapsed.Milliseconds(),
		"deposit_tx", depositHash,
		"withdraw_tx", withdrawHash,
		"vault_id", j.cfg.VaultID,
	)
	j.metrics.RecordCanaryProbe(metrics.CanaryOutcomeSucceeded, elapsed)
}

// roundTrip executes the deposit and then the withdrawal, returning both
// transaction hashes. Any error aborts the round trip and leaves the canary
// balance in the vault (which is expected: the vault is a dedicated canary
// vault and the operator can recover it manually).
func (j *CanaryJob) roundTrip(ctx context.Context) (depositHash, withdrawHash string, err error) {
	depositHash, err = j.invoker.CanaryDeposit(ctx, j.cfg.VaultID, j.cfg.Amount)
	if err != nil {
		return "", "", err
	}
	withdrawHash, err = j.invoker.CanaryWithdraw(ctx, j.cfg.VaultID, depositHash)
	if err != nil {
		return depositHash, "", err
	}
	return depositHash, withdrawHash, nil
}
