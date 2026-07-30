package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/performance"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

const (
	defaultAPYDropThresholdPct = 20.0
	apyAlertCooldown           = 24 * time.Hour
	apyLookbackDays            = 30
)

// APYVaultLister returns active vaults with their last alert timestamps.
type APYVaultLister interface {
	ListActiveVaultsForAPYCheck(ctx context.Context) ([]APYVaultInfo, error)
}

// APYVaultInfo is the minimal vault data needed by the deviation job.
type APYVaultInfo struct {
	ID                 uuid.UUID
	UserID             uuid.UUID
	Currency           string
	LastAPYAlertSentAt *time.Time
}

// APYSnapshotReader loads performance history for a vault.
type APYSnapshotReader interface {
	HistoryForVault(ctx context.Context, vaultID uuid.UUID, since time.Time) ([]performance.Snapshot, error)
}

// APYAlertUpdater persists the alert timestamp so we don't spam.
type APYAlertUpdater interface {
	UpdateAPYAlertSentAt(ctx context.Context, vaultID uuid.UUID, sentAt time.Time) error
}

// APYNotifier sends the drop notification to the vault owner.
type APYNotifier interface {
	Send(ctx context.Context, userID uuid.UUID, evt notifications.EventType, title, body string, payload map[string]any) error
}

// APYDeviationConfig controls the deviation checker.
type APYDeviationConfig struct {
	Enabled      bool
	Interval     time.Duration
	ThresholdPct float64
}

// APYDeviationJobFromEnv reads VAULT_APY_DROP_THRESHOLD_PCT (default 20) and
// builds a config with a 1-hour interval.
func APYDeviationJobFromEnv() APYDeviationConfig {
	threshold := defaultAPYDropThresholdPct
	if v := os.Getenv("VAULT_APY_DROP_THRESHOLD_PCT"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil && parsed > 0 {
			threshold = parsed
		}
	}
	enabled := os.Getenv("VAULT_APY_CHECK_ENABLED") != "false"
	return APYDeviationConfig{
		Enabled:      enabled,
		Interval:     time.Hour,
		ThresholdPct: threshold,
	}
}

// APYDeviationJob runs on a ticker and fires notifications when vault APY
// drops more than ThresholdPct from its 30-day mean.
type APYDeviationJob struct {
	cfg       APYDeviationConfig
	vaults    APYVaultLister
	snapshots APYSnapshotReader
	updater   APYAlertUpdater
	notifier  APYNotifier
	logger    *slog.Logger
	leader    LeaderChecker
}

// SetLeaderChecker wires leader election (#846). This job only sends
// notifications (no money movement), so it is not required to be singleton
// for correctness — but the dedup check (LastAPYAlertSentAt + a 24h
// cooldown) is a read-then-write against Postgres with no locking, so N
// replicas ticking at once can each read "not yet alerted" and all send a
// duplicate notification in the same race window. Gating behind the same
// single leader lock as the money-moving jobs avoids that and is simplest
// given #846's "one leader instance" design (see Leadership's doc comment).
func (j *APYDeviationJob) SetLeaderChecker(l LeaderChecker) { j.leader = l }

func (j *APYDeviationJob) isLeader() bool {
	return j.leader == nil || j.leader.IsLeader()
}

// NewAPYDeviationJob constructs the job.
func NewAPYDeviationJob(
	cfg APYDeviationConfig,
	vaults APYVaultLister,
	snapshots APYSnapshotReader,
	updater APYAlertUpdater,
	notifier APYNotifier,
	logger *slog.Logger,
) *APYDeviationJob {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &APYDeviationJob{
		cfg:       cfg,
		vaults:    vaults,
		snapshots: snapshots,
		updater:   updater,
		notifier:  notifier,
		logger:    logger,
	}
}

// Run drives the check loop until ctx is cancelled.
func (j *APYDeviationJob) Run(ctx context.Context) {
	if !j.cfg.Enabled {
		j.logger.Info("apy-deviation: disabled; not starting")
		return
	}
	j.logger.Info("apy-deviation: starting",
		"interval", j.cfg.Interval,
		"threshold_pct", j.cfg.ThresholdPct,
	)

	j.tick(ctx)

	ticker := time.NewTicker(j.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			j.logger.Info("apy-deviation: stopping")
			return
		case <-ticker.C:
			j.tick(ctx)
		}
	}
}

func (j *APYDeviationJob) tick(ctx context.Context) {
	if !j.isLeader() {
		j.logger.Debug("apy-deviation: skipping tick, not leader")
		return
	}

	vaults, err := j.vaults.ListActiveVaultsForAPYCheck(ctx)
	if err != nil {
		j.logger.Error("apy-deviation: list vaults failed", "error", err)
		return
	}
	now := time.Now().UTC()
	for _, v := range vaults {
		j.checkVault(ctx, v, now)
	}
}

func (j *APYDeviationJob) checkVault(ctx context.Context, v APYVaultInfo, now time.Time) {
	// Deduplication: skip if we already alerted within the cooldown window.
	if v.LastAPYAlertSentAt != nil && now.Sub(*v.LastAPYAlertSentAt) < apyAlertCooldown {
		return
	}

	since := now.Add(-apyLookbackDays * 24 * time.Hour)
	snapshots, err := j.snapshots.HistoryForVault(ctx, v.ID, since)
	if err != nil {
		j.logger.Error("apy-deviation: load snapshots failed", "vault_id", v.ID, "error", err)
		return
	}

	meanAPY, latestAPY, ok := computeAPYStats(snapshots)
	if !ok {
		return
	}

	// Drop percentage relative to mean: (mean - latest) / mean * 100.
	if meanAPY <= 0 {
		return
	}
	dropPct := (meanAPY - latestAPY) / meanAPY * 100
	if dropPct <= j.cfg.ThresholdPct {
		return
	}

	// Execution-time recheck (#846 split-brain guard): re-verify immediately
	// before the notification send, since checkVault runs once per vault and
	// a large fleet can take long enough for leadership to change mid-tick.
	if !j.isLeader() {
		j.logger.Debug("apy-deviation: leadership lost mid-tick, skipping notify", "vault_id", v.ID)
		return
	}

	title := "Your vault yield dropped"
	body := fmt.Sprintf(
		"Your %s vault APY has dropped to %.1f%% — down from a 30-day average of %.1f%%. Consider reviewing your allocation.",
		v.Currency, latestAPY, meanAPY,
	)
	if err := j.notifier.Send(ctx, v.UserID, notifications.EventVaultAPYDrop, title, body, map[string]any{
		"vault_id":   v.ID.String(),
		"mean_apy":   meanAPY,
		"latest_apy": latestAPY,
		"drop_pct":   dropPct,
	}); err != nil {
		j.logger.Error("apy-deviation: send notification failed", "vault_id", v.ID, "error", err)
		return
	}

	if err := j.updater.UpdateAPYAlertSentAt(ctx, v.ID, now); err != nil {
		j.logger.Error("apy-deviation: update alert timestamp failed", "vault_id", v.ID, "error", err)
	}
}

// computeAPYStats derives the 30-day mean APY and the most recent APY from snapshots.
// Returns (meanAPY, latestAPY, ok). ok is false when there are fewer than 2 snapshots.
func computeAPYStats(snapshots []performance.Snapshot) (meanAPY, latestAPY float64, ok bool) {
	if len(snapshots) < 2 {
		return 0, 0, false
	}

	dailyAPYs := make([]float64, 0, len(snapshots)-1)
	for i := 1; i < len(snapshots); i++ {
		prev := snapshots[i-1]
		curr := snapshots[i]
		if prev.SharePrice.IsZero() || prev.SharePrice.Sign() <= 0 {
			continue
		}
		ratio, _ := curr.SharePrice.Div(prev.SharePrice).Float64()
		if ratio <= 0 {
			continue
		}
		hours := curr.SnapshotAt.Sub(prev.SnapshotAt).Hours()
		if hours <= 0 {
			continue
		}
		apy := (math.Pow(ratio, 365.0/(hours/24)) - 1) * 100
		if math.IsNaN(apy) || math.IsInf(apy, 0) {
			continue
		}
		dailyAPYs = append(dailyAPYs, apy)
	}

	if len(dailyAPYs) == 0 {
		return 0, 0, false
	}

	var sum float64
	for _, a := range dailyAPYs {
		sum += a
	}
	meanAPY = sum / float64(len(dailyAPYs))
	latestAPY = dailyAPYs[len(dailyAPYs)-1]
	return meanAPY, latestAPY, true
}
