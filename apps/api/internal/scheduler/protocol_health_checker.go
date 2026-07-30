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
	leader   LeaderChecker
}

// SetLeaderChecker wires leader election (#846). Notification-only (no
// money movement); gated behind the same single leader lock as the other
// four jobs — see APYDeviationJob.SetLeaderChecker for the shared rationale
// (the CanAlert/RecordAlert cooldown check is a read-then-write with no
// locking, so concurrent replicas could each pass it and double-notify).
func (j *ProtocolHealthChecker) SetLeaderChecker(l LeaderChecker) { j.leader = l }

func (j *ProtocolHealthChecker) isLeader() bool {
	return j.leader == nil || j.leader.IsLeader()
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
		cfg:    cfg,
		vaults: vaults,
		tvl:    tvl,
		repo:   repo,
		notify: notify,
		logger: logger,
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
	if !j.isLeader() {
		j.logger.Debug("protocol health checker: skipping tick, not leader")
		return
	}

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

	// Execution-time recheck (#846 split-brain guard).
	if !j.isLeader() {
		j.logger.Debug("protocol health checker: leadership lost mid-tick, skipping alert", "protocol", slug)
		return
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
