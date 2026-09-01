package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/ledger"
)

// LedgerBalanceVerificationJob recomputes balances from raw ledger_entries and asserts
// they match the cached ledger_balances. This is the safety net that makes the cache optimisation safe.
type LedgerBalanceVerificationJob struct {
	cfg        VerificationConfig
	ledgerRepo ledger.Repository
	logger     *slog.Logger
	leader     LeaderChecker
}

type VerificationConfig struct {
	Enabled  bool
	Interval time.Duration
}

func NewLedgerBalanceVerificationJob(cfg VerificationConfig, repo ledger.Repository, logger *slog.Logger) *LedgerBalanceVerificationJob {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &LedgerBalanceVerificationJob{
		cfg:        cfg,
		ledgerRepo: repo,
		logger:     logger,
	}
}

func (j *LedgerBalanceVerificationJob) SetLeaderChecker(l LeaderChecker) { j.leader = l }
func (j *LedgerBalanceVerificationJob) isLeader() bool                  { return j.leader == nil || j.leader.IsLeader() }

func (j *LedgerBalanceVerificationJob) Run(ctx context.Context) {
	if !j.cfg.Enabled {
		j.logger.Info("ledger balance verification disabled")
		return
	}
	interval := j.cfg.Interval
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	j.logger.Info("ledger balance verification starting", "interval", interval)
	j.Tick(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			j.logger.Info("ledger balance verification stopping")
			return
		case <-ticker.C:
			j.Tick(ctx)
		}
	}
}

// Tick recomputes and checks.
func (j *LedgerBalanceVerificationJob) Tick(ctx context.Context) {
	if !j.isLeader() {
		j.logger.Debug("ledger balance verification: skipping, not leader")
		return
	}
	if j.ledgerRepo == nil {
		j.logger.Error("ledger balance verification: missing repo")
		return
	}
	mismatches, err := j.ledgerRepo.RecomputeBalances(ctx)
	if err != nil {
		j.logger.Error("ledger balance verification failed", "error", err)
		return
	}
	if len(mismatches) == 0 {
		j.logger.Debug("ledger balance verification: all balances ok")
		return
	}
	// Alert on mismatches — do not auto-correct, just log and record
	for _, mm := range mismatches {
		j.logger.Error("ledger balance mismatch detected",
			"account_id", mm.AccountID,
			"cached", mm.Cached,
			"computed", mm.Computed,
			"difference", mm.Difference,
		)
	}
	j.logger.Error("ledger balance verification found mismatches", "count", len(mismatches))
}
