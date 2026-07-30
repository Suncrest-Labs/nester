package harvest

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
)

// DefaultJobType is the job-queue type harvest jobs are enqueued under.
const DefaultJobType = "harvest"

// VaultYield is the minimal per-vault view the engine needs to make a harvest
// decision and, if gated in, submit one.
type VaultYield struct {
	VaultID         uuid.UUID
	UserID          uuid.UUID
	ContractAddress string
	Currency        string
	// WalletAddress is the vault owner's address, required for the on-chain
	// harvest call.
	WalletAddress string
	// AccruedYield is the currently harvestable yield in the vault currency.
	AccruedYield decimal.Decimal
	// AccrualRatePerHour is the recent yield accrual rate, used only to estimate
	// the next harvest time for user-facing status. Zero means "unknown".
	AccrualRatePerHour decimal.Decimal
	// HarvestFrequency is the vault owner's configured cadence ("daily" or
	// "weekly", #940). Empty is treated as the default (daily).
	HarvestFrequency string
	// LastHarvestedAt is when this vault's yield was last harvested. Nil means
	// never harvested, so the frequency gate always lets it through.
	LastHarvestedAt *time.Time
}

// VaultSource lists vaults eligible for harvest evaluation.
type VaultSource interface {
	ListHarvestable(ctx context.Context) ([]VaultYield, error)
	GetVaultYield(ctx context.Context, vaultID uuid.UUID) (VaultYield, error)
}

// GasOracle reports harvest gas economics and network congestion so the engine
// can gate and defer.
type GasOracle interface {
	// HarvestFee estimates the gas cost of one harvest, expressed in the vault's
	// settlement currency so it is directly comparable to accrued yield.
	HarvestFee(ctx context.Context, currency string) (decimal.Decimal, error)
	// Congested reports whether the network fee market is currently spiking, in
	// which case non-urgent harvests are deferred to a later tick.
	Congested(ctx context.Context) (bool, error)
}

// Enqueuer submits durable jobs. *jobqueue.Client satisfies it.
type Enqueuer interface {
	Enqueue(ctx context.Context, in jobqueue.EnqueueInput) (jobqueue.Job, error)
}

// Config controls the harvest engine loop.
type Config struct {
	Enabled bool
	// Interval is the default evaluation cadence.
	Interval time.Duration
	// Margin is the safety buffer added on top of the gas fee in the economic
	// gate (harvest iff accrued > gas + margin).
	Margin decimal.Decimal
	// Window buckets idempotency keys so repeated ticks (and event triggers)
	// within the same window collapse to a single enqueued harvest per vault.
	Window time.Duration
	// JobType overrides DefaultJobType when set.
	JobType string
	// MaxAttempts caps retries for enqueued harvest jobs (0 = queue default).
	MaxAttempts int
}

func (c Config) jobType() string {
	if c.JobType != "" {
		return c.JobType
	}
	return DefaultJobType
}

func (c Config) window() time.Duration {
	if c.Window <= 0 {
		return time.Hour
	}
	return c.Window
}

// HarvestJobPayload is the job-queue payload for a single vault harvest.
type HarvestJobPayload struct {
	VaultID       uuid.UUID `json:"vault_id"`
	UserID        uuid.UUID `json:"user_id"`
	WalletAddress string    `json:"wallet_address"`
	Compound      bool      `json:"compound"`
}

// Engine evaluates vaults and enqueues gated harvests.
type Engine struct {
	cfg    Config
	vaults VaultSource
	gas    GasOracle
	queue  Enqueuer
	logger *slog.Logger
	clock  func() time.Time
}

// New constructs an Engine. logger may be nil.
func New(cfg Config, vaults VaultSource, gas GasOracle, queue Enqueuer, logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &Engine{
		cfg:    cfg,
		vaults: vaults,
		gas:    gas,
		queue:  queue,
		logger: logger,
		clock:  time.Now,
	}
}

// Run drives the evaluation loop until ctx is cancelled. When disabled, it
// returns immediately so main can call Run unconditionally.
func (e *Engine) Run(ctx context.Context) {
	if !e.cfg.Enabled {
		e.logger.Info("harvest engine disabled; not starting")
		return
	}
	interval := e.cfg.Interval
	if interval <= 0 {
		interval = time.Hour
	}
	e.logger.Info("harvest engine starting", "interval", interval, "margin", e.cfg.Margin.String())

	e.tick(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			e.logger.Info("harvest engine stopping")
			return
		case <-ticker.C:
			e.tick(ctx)
		}
	}
}

// tick evaluates every harvestable vault once. Congestion defers the whole tick.
func (e *Engine) tick(ctx context.Context) {
	congested, err := e.gas.Congested(ctx)
	if err != nil {
		e.logger.Error("harvest: congestion check failed; skipping tick", "error", err)
		return
	}
	if congested {
		e.logger.Info("harvest: network congested, deferring harvests to next tick")
		return
	}

	vaults, err := e.vaults.ListHarvestable(ctx)
	if err != nil {
		e.logger.Error("harvest: list vaults failed", "error", err)
		return
	}

	enqueued, skipped := 0, 0
	for _, v := range vaults {
		ok, err := e.evaluateAndEnqueue(ctx, v)
		if err != nil {
			e.logger.Error("harvest: enqueue failed", "vault_id", v.VaultID, "error", err)
			continue
		}
		if ok {
			enqueued++
		} else {
			skipped++
		}
	}
	e.logger.Debug("harvest tick complete", "vaults", len(vaults), "enqueued", enqueued, "skipped", skipped)
}

// TriggerVault evaluates and, if gated in, enqueues a harvest for a single vault
// out of cadence (event-triggered harvest). It still applies the economic gate
// and idempotency window, so a burst of triggers cannot double-submit.
func (e *Engine) TriggerVault(ctx context.Context, vaultID uuid.UUID) (bool, error) {
	v, err := e.vaults.GetVaultYield(ctx, vaultID)
	if err != nil {
		return false, err
	}
	return e.evaluateAndEnqueue(ctx, v)
}

// evaluateAndEnqueue applies the gate to one vault and enqueues an idempotent
// harvest job when it clears. Returns whether a harvest was enqueued.
func (e *Engine) evaluateAndEnqueue(ctx context.Context, v VaultYield) (bool, error) {
	if !DueForHarvest(e.clock(), v.HarvestFrequency, v.LastHarvestedAt) {
		return false, nil
	}

	fee, err := e.gas.HarvestFee(ctx, v.Currency)
	if err != nil {
		return false, fmt.Errorf("estimate gas fee: %w", err)
	}
	decision := Evaluate(GatingInput{
		AccruedYield: v.AccruedYield,
		GasFee:       fee,
		Margin:       e.cfg.Margin,
	})
	if !decision.Harvest {
		return false, nil
	}

	payload, err := json.Marshal(HarvestJobPayload{
		VaultID:       v.VaultID,
		UserID:        v.UserID,
		WalletAddress: v.WalletAddress,
	})
	if err != nil {
		return false, fmt.Errorf("marshal payload: %w", err)
	}

	_, err = e.queue.Enqueue(ctx, jobqueue.EnqueueInput{
		Type:           e.cfg.jobType(),
		Payload:        payload,
		IdempotencyKey: e.idempotencyKey(v.VaultID),
		MaxAttempts:    e.cfg.MaxAttempts,
	})
	if err != nil {
		return false, err
	}
	e.logger.Info("harvest enqueued",
		"vault_id", v.VaultID,
		"accrued", v.AccruedYield.String(),
		"threshold", decision.Threshold.String(),
	)
	return true, nil
}

// idempotencyKey collapses all harvest submissions for a vault within the same
// time window to a single job, so repeated ticks and event triggers dedupe.
func (e *Engine) idempotencyKey(vaultID uuid.UUID) string {
	bucket := e.clock().Truncate(e.cfg.window()).Unix()
	return fmt.Sprintf("%s:%d", vaultID, bucket)
}

// discardWriter is the slog fallback when New is called with a nil logger.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// SimulationResult contains the expected outcome of a harvest without execution.
type SimulationResult struct {
	// VaultID is the vault being simulated.
	VaultID uuid.UUID `json:"vault_id"`
	// WouldHarvest indicates whether the harvest would be enqueued given current
	// yield and gas economics.
	WouldHarvest bool `json:"would_harvest"`
	// AccruedYield is the current harvestable yield.
	AccruedYield decimal.Decimal `json:"accrued_yield"`
	// EstimatedGasCost is the gas cost estimate in the vault's currency.
	EstimatedGasCost decimal.Decimal `json:"estimated_gas_cost"`
	// Threshold is the minimum yield required for harvest (gas + margin).
	Threshold decimal.Decimal `json:"threshold"`
	// NetYield is the expected net yield after gas cost (AccruedYield - Threshold).
	NetYield decimal.Decimal `json:"net_yield"`
	// Reason explains the gating decision.
	Reason Reason `json:"reason"`
	// Congested indicates whether the network is currently congested.
	Congested bool `json:"congested"`
}

// SimulateHarvest returns the expected harvest outcome for a vault without
// submitting a transaction. This is useful for gas estimation and user-facing
// "preview harvest" features.
func (e *Engine) SimulateHarvest(ctx context.Context, vaultID uuid.UUID) (SimulationResult, error) {
	v, err := e.vaults.GetVaultYield(ctx, vaultID)
	if err != nil {
		return SimulationResult{}, fmt.Errorf("get vault yield: %w", err)
	}

	fee, err := e.gas.HarvestFee(ctx, v.Currency)
	if err != nil {
		return SimulationResult{}, fmt.Errorf("estimate gas fee: %w", err)
	}

	congested, err := e.gas.Congested(ctx)
	if err != nil {
		return SimulationResult{}, fmt.Errorf("check congestion: %w", err)
	}

	decision := Evaluate(GatingInput{
		AccruedYield: v.AccruedYield,
		GasFee:       fee,
		Margin:       e.cfg.Margin,
	})

	if !DueForHarvest(e.clock(), v.HarvestFrequency, v.LastHarvestedAt) {
		decision.Harvest = false
		decision.Reason = ReasonNotDue
	}

	// If network is congested, the harvest would be deferred (not enqueued).
	wouldHarvest := decision.Harvest && !congested

	return SimulationResult{
		VaultID:          v.VaultID,
		WouldHarvest:     wouldHarvest,
		AccruedYield:     v.AccruedYield,
		EstimatedGasCost: fee,
		Threshold:        decision.Threshold,
		NetYield:         decision.NetGain,
		Reason:           decision.Reason,
		Congested:        congested,
	}, nil
}
