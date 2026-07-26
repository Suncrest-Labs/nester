package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/protocoltvl"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

// ActiveVaultLister returns all currently-active vaults with their allocations.
type ActiveVaultLister interface {
	ListActive(ctx context.Context) ([]vault.Vault, error)
}

// ProtocolTVLFetcher retrieves the current aggregate TVL (USD) for a protocol.
type ProtocolTVLFetcher interface {
	ProtocolTVL(ctx context.Context, protocolSlug string) (float64, error)
}

// ProtocolHealthNotifier sends a health alert to a user.
type ProtocolHealthNotifier interface {
	NotifyProtocolHealth(ctx context.Context, userID uuid.UUID, slug string, dropPct, currentTVL float64) error
}

// DegradedSource is a yield source the registry has auto-degraded after its
// adapter exceeded the consecutive-failure threshold on-chain.
type DegradedSource struct {
	// SourceID is the registry symbol (also the protocol slug).
	SourceID string
	// FailureCount is the consecutive failure tally at degradation time.
	FailureCount uint32
	// LastFailureAt is the ledger timestamp of the most recent failure.
	LastFailureAt time.Time
}

// DegradedSourceLister reads auto-degraded sources from yield_registry.
// Backed on-chain by `get_degraded_sources`.
type DegradedSourceLister interface {
	ListDegradedSources(ctx context.Context) ([]DegradedSource, error)
}

// DegradedSourceNotifier alerts a user that a source their vault is allocated
// to has been degraded on-chain.
type DegradedSourceNotifier interface {
	NotifySourceDegraded(ctx context.Context, userID uuid.UUID, slug string, failureCount uint32) error
}

// ProtocolHealthConfig controls the background health-check loop.
type ProtocolHealthConfig struct {
	Enabled  bool
	Interval time.Duration
}

const defaultProtocolHealthInterval = 30 * time.Minute

// ProtocolHealthChecker runs every 30 minutes, fetches TVL for protocols where
// users hold positions, and fires ProtocolHealthAlert notifications when TVL
// drops more than 20% within 24 hours.
type ProtocolHealthChecker struct {
	cfg      ProtocolHealthConfig
	vaults   ActiveVaultLister
	tvl      ProtocolTVLFetcher
	repo     protocoltvl.Repository
	notify   ProtocolHealthNotifier
	logger   *slog.Logger

	// Optional on-chain adapter-failure inputs. Nil-safe: when unset the
	// checker keeps doing TVL-only health checks.
	degraded       DegradedSourceLister
	notifyDegraded DegradedSourceNotifier
	// Sources already alerted on, so a source that stays degraded across
	// ticks does not re-alert every 30 minutes. Cleared when it recovers.
	alertedDegraded map[string]bool
}

// WithDegradedSources wires the on-chain adapter-failure feed into the checker.
// Without it the checker runs TVL-only, exactly as before.
func (j *ProtocolHealthChecker) WithDegradedSources(
	lister DegradedSourceLister,
	notifier DegradedSourceNotifier,
) *ProtocolHealthChecker {
	j.degraded = lister
	j.notifyDegraded = notifier
	return j
}

// NewProtocolHealthChecker constructs the checker.
func NewProtocolHealthChecker(
	cfg ProtocolHealthConfig,
	vaults ActiveVaultLister,
	tvl ProtocolTVLFetcher,
	repo protocoltvl.Repository,
	notify ProtocolHealthNotifier,
	logger *slog.Logger,
) *ProtocolHealthChecker {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultProtocolHealthInterval
	}
	return &ProtocolHealthChecker{
		cfg:             cfg,
		vaults:          vaults,
		tvl:             tvl,
		repo:            repo,
		notify:          notify,
		logger:          logger,
		alertedDegraded: make(map[string]bool),
	}
}

// Run drives the check loop until ctx is cancelled.
func (j *ProtocolHealthChecker) Run(ctx context.Context) {
	if !j.cfg.Enabled {
		j.logger.Info("protocol health checker disabled; not starting")
		return
	}
	j.logger.Info("protocol health checker starting", "interval", j.cfg.Interval)

	j.Tick(ctx)

	ticker := time.NewTicker(j.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			j.logger.Info("protocol health checker stopping")
			return
		case <-ticker.C:
			j.Tick(ctx)
		}
	}
}

// Tick runs one health-check pass. Exported for tests.
func (j *ProtocolHealthChecker) Tick(ctx context.Context) {
	active, err := j.vaults.ListActive(ctx)
	if err != nil {
		j.logger.Error("protocol health checker: list active vaults failed", "error", err)
		return
	}

	// Build protocol → []userID mapping from vault allocations.
	protocolUsers := make(map[string][]uuid.UUID)
	for _, v := range active {
		for _, alloc := range v.Allocations {
			slug := strings.ToLower(strings.TrimSpace(alloc.Protocol))
			if slug == "" {
				continue
			}
			protocolUsers[slug] = appendUnique(protocolUsers[slug], v.UserID)
		}
	}

	for slug, userIDs := range protocolUsers {
		j.checkProtocol(ctx, slug, userIDs)
	}

	j.checkDegradedSources(ctx, protocolUsers)
}

// checkDegradedSources consumes the on-chain adapter-failure signal.
//
// A degraded source means yield_registry saw an adapter exceed its
// consecutive-failure threshold and froze that source. Recovery is an explicit
// admin action on-chain, so this is an operator-actionable alert, not a
// transient blip: alert once per degradation episode and stay quiet until the
// source disappears from the degraded list.
func (j *ProtocolHealthChecker) checkDegradedSources(ctx context.Context, protocolUsers map[string][]uuid.UUID) {
	if j.degraded == nil {
		return
	}

	sources, err := j.degraded.ListDegradedSources(ctx)
	if err != nil {
		j.logger.Warn("protocol health checker: degraded source fetch failed", "error", err)
		return
	}

	stillDegraded := make(map[string]bool, len(sources))
	for _, src := range sources {
		slug := strings.ToLower(strings.TrimSpace(src.SourceID))
		if slug == "" {
			continue
		}
		stillDegraded[slug] = true

		if j.alertedDegraded[slug] {
			continue
		}
		j.alertedDegraded[slug] = true

		j.logger.Error("yield source degraded on-chain",
			"source", slug,
			"failure_count", src.FailureCount,
			"last_failure_at", src.LastFailureAt,
		)

		if j.notifyDegraded == nil {
			continue
		}
		for _, uid := range protocolUsers[slug] {
			u := uid
			count := src.FailureCount
			go func() {
				if err := j.notifyDegraded.NotifySourceDegraded(ctx, u, slug, count); err != nil {
					j.logger.Warn("protocol health checker: degraded notify failed",
						"user_id", u, "source", slug, "error", err)
				}
			}()
		}
	}

	// Drop alert state for sources an admin has recovered, so a future
	// degradation of the same source alerts again.
	for slug := range j.alertedDegraded {
		if !stillDegraded[slug] {
			delete(j.alertedDegraded, slug)
			j.logger.Info("yield source no longer degraded", "source", slug)
		}
	}
}

func (j *ProtocolHealthChecker) checkProtocol(ctx context.Context, slug string, userIDs []uuid.UUID) {
	currentTVL, err := j.tvl.ProtocolTVL(ctx, slug)
	if err != nil {
		j.logger.Warn("protocol health checker: TVL fetch failed", "protocol", slug, "error", err)
		return
	}

	if err := j.repo.InsertSnapshot(ctx, slug, currentTVL); err != nil {
		j.logger.Warn("protocol health checker: snapshot insert failed", "protocol", slug, "error", err)
	}

	// Compare with snapshot ~24h ago.
	ago24h := time.Now().Add(-24 * time.Hour)
	prev, err := j.repo.SnapshotAt(ctx, slug, ago24h)
	if err != nil || prev == nil {
		// No baseline yet — skip alert check, will have data on the next run.
		return
	}

	if prev.TVLUSD <= 0 {
		return
	}

	dropPct := (prev.TVLUSD - currentTVL) / prev.TVLUSD * 100
	if dropPct < protocoltvl.TVLDropThreshold {
		return
	}

	// Check cooldown.
	canAlert, err := j.repo.CanAlert(ctx, slug)
	if err != nil || !canAlert {
		return
	}

	if err := j.repo.RecordAlert(ctx, slug); err != nil {
		j.logger.Warn("protocol health checker: record alert failed", "protocol", slug, "error", err)
	}

	j.logger.Warn("protocol health alert triggered",
		"protocol", slug,
		"drop_pct", fmt.Sprintf("%.1f", dropPct),
		"current_tvl", fmt.Sprintf("%.2f", currentTVL),
		"prev_tvl", fmt.Sprintf("%.2f", prev.TVLUSD),
	)

	for _, uid := range userIDs {
		u := uid
		go func() {
			if err := j.notify.NotifyProtocolHealth(ctx, u, slug, dropPct, currentTVL); err != nil {
				j.logger.Warn("protocol health checker: notify failed", "user_id", u, "error", err)
			}
		}()
	}
}

// appendUnique appends id to slice only if it's not already present.
func appendUnique(ids []uuid.UUID, id uuid.UUID) []uuid.UUID {
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
}

// DispatcherProtocolHealthNotifier sends protocol health alerts via the notifications dispatcher.
type DispatcherProtocolHealthNotifier struct {
	Dispatcher *notifications.Dispatcher
}

func (n DispatcherProtocolHealthNotifier) NotifyProtocolHealth(
	ctx context.Context,
	userID uuid.UUID,
	slug string,
	dropPct, currentTVLUSD float64,
) error {
	if n.Dispatcher == nil {
		return nil
	}
	title := fmt.Sprintf("Protocol health alert: %s", slug)
	body := fmt.Sprintf(
		"%s TVL has dropped %.1f%% in the last 24 hours (now $%.0f). Consider reviewing your position.",
		slug, dropPct, currentTVLUSD,
	)
	return n.Dispatcher.Send(ctx, userID, notifications.EventProtocolHealthAlert, title, body, map[string]any{
		"protocol":       slug,
		"drop_pct":       dropPct,
		"current_tvl":    currentTVLUSD,
		"suggested_action": "Review your position or consider withdrawing.",
	})
}

// NotifySourceDegraded alerts a user that a yield source their vault uses has
// been degraded on-chain after repeated adapter failures. Reuses the protocol
// health alert channel set: same audience, same urgency.
func (n DispatcherProtocolHealthNotifier) NotifySourceDegraded(
	ctx context.Context,
	userID uuid.UUID,
	slug string,
	failureCount uint32,
) error {
	if n.Dispatcher == nil {
		return nil
	}
	title := fmt.Sprintf("Yield source paused: %s", slug)
	body := fmt.Sprintf(
		"%s has been paused automatically after %d consecutive adapter failures. "+
			"Your funds in this source are frozen in place and rebalancing now skips it. "+
			"An administrator must review and re-enable the source.",
		slug, failureCount,
	)
	return n.Dispatcher.Send(ctx, userID, notifications.EventProtocolHealthAlert, title, body, map[string]any{
		"protocol":         slug,
		"failure_count":    failureCount,
		"degraded":         true,
		"suggested_action": "No action needed — an administrator must re-enable this source.",
	})
}
