package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	admindomain "github.com/suncrestlabs/nester/apps/api/internal/domain/admin"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/audit"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/scheduler"
)

// RebalanceDriftJobType is the jobqueue.Job.Type enqueued by APYDriftDetector
// when a vault's APY drift clears the configured threshold (#613). Handled by
// NewAPYDriftRebalanceJobHandler, which reuses the existing manual admin
// rebalance path (AdminService.TriggerRebalance) — the same mechanism behind
// POST /api/v1/admin/vaults/{id}/rebalance — rather than submitting on-chain
// directly, so drift-triggered and operator-triggered rebalances go through
// one audited, in-flight-guarded code path.
const RebalanceDriftJobType = "rebalance_apy_drift"

// DefaultAPYDriftThresholdBPS mirrors config.RebalancerConfig's default
// (REBALANCE_APY_THRESHOLD=200, i.e. 2%) for callers that construct an
// APYDriftDetector without going through the env-driven config loader (e.g.
// tests).
const DefaultAPYDriftThresholdBPS = 200

// RebalanceDriftJobPayload is the durable payload for RebalanceDriftJobType.
// It carries the detector's snapshot at enqueue time purely for audit/
// observability (the "why did this fire" trail); the job handler always
// re-derives the actual rebalance target through AdminService, never from
// this payload, since chain state may have moved between enqueue and
// execution.
type RebalanceDriftJobPayload struct {
	VaultID            uuid.UUID `json:"vault_id"`
	CurrentTopProtocol string    `json:"current_top_protocol"`
	OptimalProtocol    string    `json:"optimal_protocol"`
	DriftBPS           int64     `json:"drift_bps"`
	ThresholdBPS       int64     `json:"threshold_bps"`
	DetectedAt         time.Time `json:"detected_at"`
}

// YieldRegistryFetcher returns the latest APY per protocol from the yield
// registry (DeFiLlama-backed YieldService, per #613's "compares ... against
// the yield registry"). Declared narrowly here so APYDriftDetector doesn't
// depend on YieldService's full surface — see YieldServiceRegistryAdapter.
type YieldRegistryFetcher interface {
	FetchAPYRegistry(ctx context.Context) ([]scheduler.ProtocolYield, error)
}

// DriftJobEnqueuer is the jobqueue.Client surface APYDriftDetector needs.
// Narrowed to EnqueueJSON (rather than depending on *jobqueue.Client
// directly) so tests can substitute a fake without a real queue repository.
type DriftJobEnqueuer interface {
	EnqueueJSON(ctx context.Context, jobType string, payload any, opts ...jobqueue.EnqueueOption) (jobqueue.Job, error)
}

// APYDriftDetector implements #613's automated rebalance trigger: it
// compares each active vault's current allocation APY against the best
// available protocol in the yield registry, and — when the spread exceeds
// the configured threshold — enqueues a rebalance job rather than executing
// synchronously, so a slow or failing rebalance never blocks the detection
// pass evaluating the next vault.
//
// Drift *detection* is new; drift *decisioning* (the weighted-APY-vs-optimal
// math, already unit-tested against boundary cases in decision_test.go) is
// not reimplemented here — it delegates to scheduler.Decide, the same pure
// core the pre-existing (but never wired up) #372 Scheduler uses.
type APYDriftDetector struct {
	vaultRepo    vault.Repository
	yields       YieldRegistryFetcher
	jobs         DriftJobEnqueuer
	thresholdBPS int64
	interval     time.Duration
	logger       *slog.Logger
	leader       scheduler.LeaderChecker
}

// DefaultAPYDriftCheckInterval is how often Run sweeps all active vaults
// when no interval is configured.
const DefaultAPYDriftCheckInterval = 15 * time.Minute

// NewAPYDriftDetector constructs a detector. jobs may be nil at construction
// time and wired in later via SetJobEnqueuer — main.go builds this detector
// early (alongside the yield service, for the read-only analytics endpoint)
// but the durable job queue client it needs to actually enqueue rebalances
// isn't constructed until later; CheckAll is a safe no-op until SetJobEnqueuer
// is called. thresholdBPS <= 0 falls back to DefaultAPYDriftThresholdBPS;
// interval <= 0 falls back to DefaultAPYDriftCheckInterval. logger may be nil.
func NewAPYDriftDetector(
	vaultRepo vault.Repository,
	yields YieldRegistryFetcher,
	jobs DriftJobEnqueuer,
	thresholdBPS int64,
	interval time.Duration,
	logger *slog.Logger,
) *APYDriftDetector {
	if thresholdBPS <= 0 {
		thresholdBPS = DefaultAPYDriftThresholdBPS
	}
	if interval <= 0 {
		interval = DefaultAPYDriftCheckInterval
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &APYDriftDetector{
		vaultRepo:    vaultRepo,
		yields:       yields,
		jobs:         jobs,
		thresholdBPS: thresholdBPS,
		interval:     interval,
		logger:       logger,
	}
}

// SetLeaderChecker wires leader election (#846-style): CheckAll enqueues
// rebalance jobs, so N replicas sweeping concurrently would each detect the
// same drift and race to enqueue duplicate jobs (the idempotency key in
// evaluateAndEnqueue collapses same-day duplicates, but there is no reason
// to pay N redundant yield-registry fetches and vault scans per tick when
// only one needs to run). Matches scheduler.APYDeviationJob's convention.
func (d *APYDriftDetector) SetLeaderChecker(l scheduler.LeaderChecker) { d.leader = l }

func (d *APYDriftDetector) isLeader() bool {
	return d.leader == nil || d.leader.IsLeader()
}

// SetJobEnqueuer wires the durable job-queue client once it's available (see
// NewAPYDriftDetector's doc comment). Must be called before Run/CheckAll can
// actually enqueue anything; calling it more than once is fine (last write
// wins), though main.go only ever calls it once.
func (d *APYDriftDetector) SetJobEnqueuer(jobs DriftJobEnqueuer) { d.jobs = jobs }

// Run drives the periodic detection loop until ctx is cancelled, matching
// scheduler.APYDeviationJob.Run's convention: an immediate first pass, then
// one pass per interval.
func (d *APYDriftDetector) Run(ctx context.Context) {
	d.logger.Info("apy-drift: starting", "interval", d.interval, "threshold_bps", d.thresholdBPS)
	d.tick(ctx)

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			d.logger.Info("apy-drift: stopping")
			return
		case <-ticker.C:
			d.tick(ctx)
		}
	}
}

func (d *APYDriftDetector) tick(ctx context.Context) {
	if !d.isLeader() {
		return
	}
	if err := d.CheckAll(ctx); err != nil {
		d.logger.Error("apy-drift: check pass failed", "error", err)
	}
}

// DriftState is a vault's current APY-drift snapshot, exposed on
// GET /api/v1/vaults/{id}/analytics per #613's requirement.
type DriftState struct {
	CurrentTopProtocol string `json:"current_top_protocol,omitempty"`
	OptimalProtocol    string `json:"optimal_protocol,omitempty"`
	DriftBPS           int64  `json:"drift_bps"`
	ThresholdBPS       int64  `json:"threshold_bps"`
	ExceedsThreshold   bool   `json:"exceeds_threshold"`
	Reason             string `json:"reason"`
}

// GetDriftState computes (without enqueueing) the current drift snapshot for
// a single vault. Used by the vault analytics endpoint so operators/users can
// see drift state without waiting for the next periodic sweep.
func (d *APYDriftDetector) GetDriftState(ctx context.Context, vaultID uuid.UUID) (DriftState, error) {
	v, err := d.vaultRepo.GetVault(ctx, vaultID)
	if err != nil {
		return DriftState{}, fmt.Errorf("apy drift: get vault: %w", err)
	}
	yields, err := d.yields.FetchAPYRegistry(ctx)
	if err != nil {
		return DriftState{}, fmt.Errorf("apy drift: fetch yield registry: %w", err)
	}
	decision := d.decideForVault(v, yields)
	return DriftState{
		CurrentTopProtocol: decision.CurrentTopProtocol,
		OptimalProtocol:    decision.OptimalProtocol,
		DriftBPS:           decision.ExpectedGainBPS,
		ThresholdBPS:       d.thresholdBPS,
		ExceedsThreshold:   decision.Rebalance,
		Reason:             decision.Reason,
	}, nil
}

// CheckAll runs one detection pass over every active vault: for each, it
// computes drift and — when it clears the threshold — enqueues a
// RebalanceDriftJobType job. Intended to be called on a periodic ticker
// (wired in main.go alongside the other scheduler jobs); errors for one
// vault are logged and do not abort the pass for the rest.
func (d *APYDriftDetector) CheckAll(ctx context.Context) error {
	const pageSize = 200
	yields, err := d.yields.FetchAPYRegistry(ctx)
	if err != nil {
		return fmt.Errorf("apy drift: fetch yield registry: %w", err)
	}
	if len(yields) == 0 {
		d.logger.Debug("apy drift: yield registry returned no protocols, skipping pass")
		return nil
	}

	for offset := 0; ; offset += pageSize {
		vaults, total, err := d.vaultRepo.ListVaults(ctx, vault.ListFilter{
			Limit:  pageSize,
			Offset: offset,
			Status: string(vault.StatusActive),
		})
		if err != nil {
			return fmt.Errorf("apy drift: list active vaults: %w", err)
		}
		for _, summary := range vaults {
			full, err := d.vaultRepo.GetVault(ctx, summary.ID)
			if err != nil {
				d.logger.Error("apy drift: get vault failed", "vault_id", summary.ID, "error", err)
				continue
			}
			d.evaluateAndEnqueue(ctx, full, yields)
		}
		if offset+pageSize >= total || len(vaults) == 0 {
			break
		}
	}
	return nil
}

// decideForVault maps a vault's allocations into scheduler.Decide's input
// shape and runs the shared pure-decision core. Used by both GetDriftState
// (single-vault, on-demand) and evaluateAndEnqueue (batch sweep) so they can
// never disagree about what counts as drift.
func (d *APYDriftDetector) decideForVault(v vault.Vault, yields []scheduler.ProtocolYield) scheduler.Decision {
	current := make([]scheduler.CurrentAllocation, 0, len(v.Allocations))
	for _, a := range v.Allocations {
		current = append(current, scheduler.CurrentAllocation{Protocol: a.Protocol, Amount: a.Amount})
	}
	return scheduler.Decide(scheduler.DecisionInput{
		CurrentAllocations: current,
		Yields:             yields,
		MinAPYGainBPS:      d.thresholdBPS,
	})
}

// evaluateAndEnqueue computes the drift decision for one vault against the
// shared yield-registry snapshot and enqueues a rebalance job when it clears
// the threshold. A vault with no allocations, or already at the optimal
// protocol, or below threshold, is a deliberate no-op (the same "no-op"
// cases scheduler.Decide's tests already cover at the pure-function level).
func (d *APYDriftDetector) evaluateAndEnqueue(ctx context.Context, v vault.Vault, yields []scheduler.ProtocolYield) {
	if d.jobs == nil {
		// SetJobEnqueuer hasn't been called yet (see NewAPYDriftDetector's
		// doc comment) — nothing to enqueue onto, so skip rather than panic.
		return
	}

	decision := d.decideForVault(v, yields)

	if !decision.Rebalance {
		d.logger.Debug("apy drift: no action",
			"vault_id", v.ID, "reason", decision.Reason, "drift_bps", decision.ExpectedGainBPS)
		return
	}

	payload := RebalanceDriftJobPayload{
		VaultID:            v.ID,
		CurrentTopProtocol: decision.CurrentTopProtocol,
		OptimalProtocol:    decision.OptimalProtocol,
		DriftBPS:           decision.ExpectedGainBPS,
		ThresholdBPS:       d.thresholdBPS,
		DetectedAt:         time.Now().UTC(),
	}

	// Idempotency key scoped to the vault and day: repeated detection passes
	// within the same day for a vault already sitting above threshold
	// collapse into the single in-flight/queued job rather than piling up
	// duplicate rebalance requests the admin_rebalance_repository's
	// in-flight guard would just reject anyway.
	idempotencyKey := fmt.Sprintf("apy-drift:%s:%s", v.ID, payload.DetectedAt.Format("2006-01-02"))

	// A fresh correlation id per enqueue (nester#1339): the job handler's own
	// audit entry (written on successful submission, in
	// NewAPYDriftRebalanceJobHandler) carries it through job.CorrelationID so
	// it can be traced back to this specific detection pass.
	job, err := d.jobs.EnqueueJSON(ctx, RebalanceDriftJobType, payload,
		jobqueue.WithIdempotencyKey(idempotencyKey),
		jobqueue.WithPriority(jobqueue.PriorityBalance),
		jobqueue.WithCorrelationID(uuid.NewString()),
	)
	if err != nil {
		d.logger.Error("apy drift: enqueue rebalance job failed",
			"vault_id", v.ID, "optimal_protocol", decision.OptimalProtocol, "error", err)
		return
	}
	d.logger.Info("apy drift: rebalance enqueued",
		"vault_id", v.ID,
		"job_id", job.ID,
		"from", decision.CurrentTopProtocol,
		"to", decision.OptimalProtocol,
		"drift_bps", decision.ExpectedGainBPS,
		"threshold_bps", d.thresholdBPS,
	)
}

// RebalanceTrigger is the AdminService surface the drift job handler needs —
// exactly TriggerRebalance, the same method POST
// /api/v1/admin/vaults/{id}/rebalance calls, so a drift-triggered rebalance
// gets identical validation (vault must be active, no in-flight rebalance)
// and produces an identical admin_rebalance_history row.
type RebalanceTrigger interface {
	TriggerRebalance(ctx context.Context, vaultID uuid.UUID, req admindomain.RebalanceRequest) (admindomain.RebalanceResponse, error)
}

// NewAPYDriftRebalanceJobHandler builds the jobqueue.Handler for
// RebalanceDriftJobType (#613). It reuses AdminService.TriggerRebalance
// (strategy "auto", not a dry run) as the actual rebalance mechanism, then
// writes an audit log entry recording that the trigger was automated drift
// detection rather than an operator action.
//
// ErrRebalanceInFlight and ErrRebalanceNotEligible are treated as expected,
// non-retryable outcomes (dead-lettered without alarm-worthy logging): by
// the time the job runs, an operator may have already triggered a manual
// rebalance, or the vault may have been paused — both mean "no action
// needed" rather than "something is broken that a retry would fix".
func NewAPYDriftRebalanceJobHandler(admin RebalanceTrigger, auditLogger AuditLogger, logger *slog.Logger) jobqueue.Handler {
	if auditLogger == nil {
		auditLogger = NoopAuditLogger{}
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return jobqueue.HandlerFunc(func(ctx context.Context, job jobqueue.Job) error {
		var p RebalanceDriftJobPayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return jobqueue.Permanent(fmt.Errorf("rebalance drift job: unmarshal payload: %w", err))
		}
		if p.VaultID == uuid.Nil {
			return jobqueue.Permanent(fmt.Errorf("rebalance drift job: payload carries no vault id"))
		}

		resp, err := admin.TriggerRebalance(ctx, p.VaultID, admindomain.RebalanceRequest{
			Strategy: admindomain.RebalanceStrategyAuto,
			DryRun:   false,
		})
		if err != nil {
			switch {
			case errIsRebalanceInFlight(err), errIsRebalanceNotEligible(err), errIsVaultNotFound(err):
				logger.Info("rebalance drift job: no-op",
					"vault_id", p.VaultID, "reason", err.Error())
				return nil
			default:
				return fmt.Errorf("rebalance drift job: trigger rebalance: %w", err)
			}
		}

		// Audit entry per #613's "Emit an audit log entry whenever a
		// rebalance is enqueued due to drift" — logged on successful
		// submission (not at enqueue time) so the audit trail reflects a
		// rebalance that actually happened, not merely a detection pass.
		newValue, _ := json.Marshal(map[string]any{
			"trigger":       "apy_drift",
			"from_protocol": p.CurrentTopProtocol,
			"to_protocol":   p.OptimalProtocol,
			"drift_bps":     p.DriftBPS,
			"threshold_bps": p.ThresholdBPS,
			"rebalance_id":  resp.RebalanceID,
			"tx_hash":       resp.TxHash,
			"detected_at":   p.DetectedAt,
		})
		if auditErr := auditLogger.Log(ctx, AuditEntry{
			Action:        "vault.rebalance.apy_drift_triggered",
			EntityType:    "vault",
			EntityID:      p.VaultID,
			NewValue:      json.RawMessage(newValue),
			CorrelationID: job.CorrelationID,
		}); auditErr != nil {
			// Audit failure must not fail (and therefore retry-loop) a
			// rebalance that already succeeded on-chain; log and continue.
			logger.Error("rebalance drift job: audit log failed", "vault_id", p.VaultID, "error", auditErr)
		}

		logger.Info("rebalance drift job: rebalance submitted",
			"vault_id", p.VaultID, "rebalance_id", resp.RebalanceID, "status", resp.Status)
		return nil
	})
}

// errIsRebalanceInFlight/errIsRebalanceNotEligible/errIsVaultNotFound are
// small local wrappers around errors.Is so the handler above reads as a
// flat switch instead of three inline errors.Is calls per branch.
func errIsRebalanceInFlight(err error) bool    { return isErr(err, ErrRebalanceInFlight) }
func errIsRebalanceNotEligible(err error) bool { return isErr(err, ErrRebalanceNotEligible) }
func errIsVaultNotFound(err error) bool        { return isErr(err, vault.ErrVaultNotFound) }

func isErr(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// YieldServiceRegistryAdapter adapts *YieldService (DeFiLlama-backed) to the
// YieldRegistryFetcher interface APYDriftDetector depends on: it reduces the
// full pool list to one best-APY entry per protocol, matching the shape
// scheduler.Decide already expects (scheduler.ProtocolYield).
type YieldServiceRegistryAdapter struct {
	svc *YieldService
	// Chain is the DeFiLlama chain filter passed to GetYieldOpportunities.
	// Defaults to "Stellar" (this codebase's only chain today) when empty.
	Chain string
}

// NewYieldServiceRegistryAdapter builds an adapter over an existing
// *YieldService (main.go already constructs one for the yield-opportunities
// endpoints; this reuses that instance rather than a second DeFiLlama
// client/cache).
func NewYieldServiceRegistryAdapter(svc *YieldService) *YieldServiceRegistryAdapter {
	return &YieldServiceRegistryAdapter{svc: svc, Chain: "Stellar"}
}

// FetchAPYRegistry implements YieldRegistryFetcher.
func (a *YieldServiceRegistryAdapter) FetchAPYRegistry(ctx context.Context) ([]scheduler.ProtocolYield, error) {
	chain := a.Chain
	if chain == "" {
		chain = "Stellar"
	}
	resp, err := a.svc.GetYieldOpportunities(ctx, chain, 0)
	if err != nil {
		return nil, err
	}

	bestByProtocol := make(map[string]float64)
	for _, pool := range resp.Pools {
		key := pool.Project
		if key == "" {
			continue
		}
		if existing, ok := bestByProtocol[key]; !ok || pool.APY > existing {
			bestByProtocol[key] = pool.APY
		}
	}

	out := make([]scheduler.ProtocolYield, 0, len(bestByProtocol))
	for protocol, apy := range bestByProtocol {
		// YieldPool.APY is a percentage (e.g. 7.0 for 7%); scheduler.Decide
		// expects a fraction (0.07), matching vault.Allocation.APY's
		// existing convention (see vault_rebalance_service.go's
		// AllocationPct.APY and allocationsToPct).
		out = append(out, scheduler.ProtocolYield{
			Protocol: protocol,
			APY:      decimalFromPercent(apy),
		})
	}
	return out, nil
}

// decimalFromPercent converts a percentage value (e.g. 7.0 for 7%) into the
// fractional decimal.Decimal (0.07) that scheduler.ProtocolYield.APY expects.
func decimalFromPercent(pct float64) decimal.Decimal {
	return decimal.NewFromFloat(pct).Div(decimal.NewFromInt(100))
}

var _ audit.Entry // keep the audit domain import honest if AuditEntry's alias ever moves.
