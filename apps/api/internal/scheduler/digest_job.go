package scheduler

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/digest"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/intelligence"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

// DigestCadenceRepository lists users due for a digest at a given cadence.
type DigestCadenceRepository interface {
	ListUserIDsForDigestCadence(ctx context.Context, cadence string) ([]uuid.UUID, error)
}

// DigestClient requests the generated narrative digest for one user/period.
type DigestClient interface {
	GenerateDigest(ctx context.Context, request intelligence.DigestGenerateRequest) (*intelligence.DigestGenerateResponse, error)
}

// DigestNotifier sends the digest-ready notification.
type DigestNotifier interface {
	Send(ctx context.Context, userID uuid.UUID, evt notifications.EventType, title, body string, payload map[string]any) error
}

// DigestJobConfig controls the digest scheduler.
type DigestJobConfig struct {
	Enabled  bool
	Interval time.Duration
}

// DigestJob is the leader-elected periodic job that generates and delivers
// the financial insight digest (#859). It ticks frequently (daily is
// sufficient) but only actually generates a digest for a user once their
// most recently completed period has no cached row yet — the digest.
// Repository cache is what makes "once per user per period" durable across
// ticks and replicas, not the tick interval itself.
type DigestJob struct {
	cfg      DigestJobConfig
	cadences DigestCadenceRepository
	cache    digest.Repository
	client   DigestClient
	notifier DigestNotifier
	logger   *slog.Logger
	clock    func() time.Time
	leader   LeaderChecker
}

// NewDigestJob constructs the job.
func NewDigestJob(
	cfg DigestJobConfig,
	cadences DigestCadenceRepository,
	cache digest.Repository,
	client DigestClient,
	notifier DigestNotifier,
	logger *slog.Logger,
) *DigestJob {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &DigestJob{
		cfg:      cfg,
		cadences: cadences,
		cache:    cache,
		client:   client,
		notifier: notifier,
		logger:   logger,
		clock:    func() time.Time { return time.Now().UTC() },
	}
}

// SetLeaderChecker wires leader election (#846) so only one replica
// generates and dispatches digests per tick.
func (j *DigestJob) SetLeaderChecker(l LeaderChecker) { j.leader = l }

func (j *DigestJob) isLeader() bool { return j.leader == nil || j.leader.IsLeader() }

// SetClock overrides the time source (tests only).
func (j *DigestJob) SetClock(clock func() time.Time) { j.clock = clock }

// Run drives the check loop until ctx is cancelled. A tick also fires once
// immediately on start.
func (j *DigestJob) Run(ctx context.Context) {
	if !j.cfg.Enabled {
		j.logger.Info("digest: disabled; not starting")
		return
	}
	interval := j.cfg.Interval
	if interval <= 0 {
		interval = 24 * time.Hour
	}

	j.Tick(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			j.Tick(ctx)
		}
	}
}

// Tick runs a single pass over both cadences.
func (j *DigestJob) Tick(ctx context.Context) {
	if !j.isLeader() {
		return
	}
	if j.cadences == nil || j.cache == nil || j.client == nil || j.notifier == nil {
		return
	}
	for _, period := range []digest.Period{digest.PeriodWeekly, digest.PeriodMonthly} {
		j.tickPeriod(ctx, period)
	}
}

func (j *DigestJob) tickPeriod(ctx context.Context, period digest.Period) {
	cadence := string(period)
	userIDs, err := j.cadences.ListUserIDsForDigestCadence(ctx, cadence)
	if err != nil {
		j.logger.Error("digest: failed to list users due", "period", period, "error", err.Error())
		return
	}

	start, end, _ := digest.Bounds(period, j.clock())
	for _, userID := range userIDs {
		j.generateAndDeliver(ctx, userID, period, start, end)
	}
}

func (j *DigestJob) generateAndDeliver(ctx context.Context, userID uuid.UUID, period digest.Period, start, end time.Time) {
	existing, err := j.cache.GetCached(ctx, userID, period, start)
	if err != nil {
		j.logger.Error("digest: cache lookup failed", "user_id", userID.String(), "error", err.Error())
		return
	}
	if existing != nil && existing.DeliveredAt != nil {
		// Already generated and delivered for this exact period — the cache
		// is the durable guard against double-send across ticks/replicas.
		return
	}

	result, err := j.client.GenerateDigest(ctx, intelligence.DigestGenerateRequest{
		UserID: userID.String(),
		Period: string(period),
	})
	if err != nil {
		j.logger.Warn("digest: generation failed", "user_id", userID.String(), "period", period, "error", err.Error())
		return
	}

	factsJSON, _ := json.Marshal(result.Facts)
	attentionJSON, _ := json.Marshal(result.AttentionItems)

	saved, err := j.cache.Save(ctx, digest.CachedDigest{
		UserID:             userID,
		Period:             period,
		PeriodStart:        start,
		PeriodEnd:          end,
		FactsHash:          result.FactsHash,
		FactsJSON:          string(factsJSON),
		Narrative:          result.Narrative,
		AttentionItemsJSON: string(attentionJSON),
		HonestZeroPeriod:   result.HonestZeroPeriod,
		GeneratedAt:        j.clock(),
	})
	if err != nil {
		j.logger.Error("digest: save failed", "user_id", userID.String(), "error", err.Error())
		return
	}

	title := digestTitle(period)
	sendErr := j.notifier.Send(ctx, userID, notifications.EventFinancialDigest, title, result.Narrative, map[string]any{
		"period":          string(period),
		"period_start":    start.Format(time.RFC3339),
		"period_end":      end.Format(time.RFC3339),
		"attention_items": result.AttentionItems,
	})
	if sendErr != nil {
		j.logger.Warn("digest: notification dispatch failed", "user_id", userID.String(), "error", sendErr.Error())
		// Not delivered — leave DeliveredAt unset so a later tick retries
		// dispatch without re-generating (the cache row already exists).
		return
	}

	if err := j.cache.MarkDelivered(ctx, saved.ID, j.clock()); err != nil {
		j.logger.Warn("digest: mark delivered failed", "user_id", userID.String(), "error", err.Error())
	}
}

func digestTitle(period digest.Period) string {
	if period == digest.PeriodWeekly {
		return "Your weekly savings digest"
	}
	return "Your monthly savings digest"
}
