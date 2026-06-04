package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// ErrCircuitBreakerTriggered is the sentinel a `RebalanceSubmitter`
// returns when the on-chain contract refuses a rebalance because the
// vault's built-in circuit breaker is open (e.g. >20% withdrawal in the
// last 2 hours). The loop catches it, logs at INFO (not ERROR), and
// continues with the next vault on the next tick.
var ErrCircuitBreakerTriggered = errors.New("vault circuit breaker is open")

// VaultSnapshot is the minimal view the scheduler needs of a single
// vault. The repository adapter is responsible for translating from
// the domain.Vault model. Kept here so the package has no dependency
// on internal/domain.
type VaultSnapshot struct {
	ID                 uuid.UUID
	ContractAddress    string
	CurrentAllocations []CurrentAllocation
}

// VaultFetcher returns the set of vaults the scheduler should consider
// on each tick. Production wiring lists active vaults from Postgres;
// tests pass a fake.
type VaultFetcher interface {
	ListActiveVaults(ctx context.Context) ([]VaultSnapshot, error)
}

// YieldFetcher returns the latest APY snapshot per protocol. Production
// implementations call an on-chain query or DefiLlama; tests pass a fake
// — issue #372 explicitly calls this out as the boundary that lets the
// decision logic be unit-tested without contract calls.
type YieldFetcher interface {
	FetchYields(ctx context.Context) ([]ProtocolYield, error)
}

// RebalanceSubmitter submits the on-chain `rebalance` call to the vault
// contract and records the resulting tx hash. Tests pass a fake that
// just records the call; the production implementation lives in
// internal/service/soroban_vault_chain_invoker.go (or a thin wrapper
// over it). Wiring this in main.go is a follow-up to #372 — this MVP
// keeps the chain integration behind an interface so the scheduler
// loop and decision core can ship and be tested first.
type RebalanceSubmitter interface {
	SubmitRebalance(ctx context.Context, vaultID uuid.UUID, optimalProtocol string, gainBPS int64) error
}

// Config controls the scheduler loop. All fields are read once at
// startup; changes require a restart. Sourced from env in main.go:
//
//	REBALANCER_ENABLED=true
//	REBALANCER_INTERVAL_MINUTES=15
//	REBALANCER_MIN_APY_GAIN_BPS=50
type Config struct {
	Enabled       bool
	Interval      time.Duration
	MinAPYGainBPS int64
}

// Scheduler is the long-lived loop. Construct with `New`, then call
// `Run` from a goroutine alongside the API server. `Run` returns when
// the context is cancelled.
type Scheduler struct {
	cfg         Config
	vaults      VaultFetcher
	yields      YieldFetcher
	submitter   RebalanceSubmitter
	logger      *slog.Logger
	lastTickEnd atomic.Int64 // unix nanos; observability hook
}

// New constructs a Scheduler. `logger` may be nil — a discarding logger
// is used in that case so the rebalancer never blocks on logging.
func New(cfg Config, vaults VaultFetcher, yields YieldFetcher, submitter RebalanceSubmitter, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &Scheduler{
		cfg:       cfg,
		vaults:    vaults,
		yields:    yields,
		submitter: submitter,
		logger:    logger,
	}
}

// Run drives the loop until ctx is cancelled. When Config.Enabled is
// false, Run returns immediately — main.go can call Run unconditionally.
func (s *Scheduler) Run(ctx context.Context) {
	if !s.cfg.Enabled {
		s.logger.Info("rebalancer disabled; not starting")
		return
	}
	s.logger.Info("rebalancer starting", "interval", s.cfg.Interval, "min_apy_gain_bps", s.cfg.MinAPYGainBPS)

	// Run once immediately so deployments don't have to wait for the
	// first tick to see anything happen.
	s.tick(ctx)

	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("rebalancer stopping")
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// TriggerOnce runs a single rebalance evaluation pass. Exposed for the
// admin endpoint POST /api/v1/admin/vaults/{id}/rebalance and for tests.
// When `vaultID` is `uuid.Nil`, every active vault is evaluated.
func (s *Scheduler) TriggerOnce(ctx context.Context, vaultID uuid.UUID) error {
	vaults, err := s.vaults.ListActiveVaults(ctx)
	if err != nil {
		return err
	}
	yields, err := s.yields.FetchYields(ctx)
	if err != nil {
		return err
	}
	for _, v := range vaults {
		if vaultID != uuid.Nil && v.ID != vaultID {
			continue
		}
		s.evaluateVault(ctx, v, yields)
	}
	return nil
}

func (s *Scheduler) tick(ctx context.Context) {
	defer s.lastTickEnd.Store(time.Now().UnixNano())

	vaults, err := s.vaults.ListActiveVaults(ctx)
	if err != nil {
		s.logger.Error("rebalancer: list active vaults failed", "error", err)
		return
	}
	yields, err := s.yields.FetchYields(ctx)
	if err != nil {
		s.logger.Error("rebalancer: fetch yields failed", "error", err)
		return
	}
	for _, v := range vaults {
		s.evaluateVault(ctx, v, yields)
	}
}

func (s *Scheduler) evaluateVault(ctx context.Context, v VaultSnapshot, yields []ProtocolYield) {
	d := Decide(DecisionInput{
		CurrentAllocations: v.CurrentAllocations,
		Yields:             yields,
		MinAPYGainBPS:      s.cfg.MinAPYGainBPS,
	})
	if !d.Rebalance {
		s.logger.Debug("rebalancer: skip",
			"vault_id", v.ID,
			"reason", d.Reason,
			"gain_bps", d.ExpectedGainBPS,
		)
		return
	}
	if err := s.submitter.SubmitRebalance(ctx, v.ID, d.OptimalProtocol, d.ExpectedGainBPS); err != nil {
		if errors.Is(err, ErrCircuitBreakerTriggered) {
			s.logger.Info("rebalancer: circuit breaker open, skipping vault until next tick",
				"vault_id", v.ID,
				"protocol", d.OptimalProtocol,
			)
			return
		}
		s.logger.Error("rebalancer: submit failed",
			"vault_id", v.ID,
			"protocol", d.OptimalProtocol,
			"error", err,
		)
		return
	}
	s.logger.Info("rebalancer: rebalance submitted",
		"vault_id", v.ID,
		"from", d.CurrentTopProtocol,
		"to", d.OptimalProtocol,
		"gain_bps", d.ExpectedGainBPS,
	)
}

// LastTickEnd returns the wall-clock time of the last completed tick, or
// zero if the loop hasn't ticked yet. Exposed for /admin/health.
func (s *Scheduler) LastTickEnd() time.Time {
	v := s.lastTickEnd.Load()
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(0, v)
}

// discardWriter is the io.Writer slog fallback when New(..., nil) is used.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
