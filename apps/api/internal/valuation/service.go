package valuation

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/portfolio"
	"github.com/suncrestlabs/nester/apps/api/internal/freshness"
)

// PositionSource lists a user's vault positions in asset units.
type PositionSource interface {
	Positions(ctx context.Context, userID uuid.UUID) ([]Position, error)
}

// PendingSource lists a user's unsettled (pending) deposits.
type PendingSource interface {
	PendingDeposits(ctx context.Context, userID uuid.UUID) ([]AssetAmount, error)
}

// RewardSource lists a user's claimable (unclaimed) rewards.
type RewardSource interface {
	ClaimableRewards(ctx context.Context, userID uuid.UUID) ([]AssetAmount, error)
}

// GoalSource lists a user's savings goals with allocated and target amounts.
type GoalSource interface {
	Goals(ctx context.Context, userID uuid.UUID) ([]GoalInput, error)
}

// Notifier pushes a fresh valuation to the user (e.g. over WebSocket).
type Notifier interface {
	PushValuation(userID uuid.UUID, val portfolio.Valuation)
}

// Service assembles a user's valuation from its data sources, prices it through
// the oracle, caches the result, and — on invalidation — recomputes and pushes.
type Service struct {
	positions PositionSource
	pending   PendingSource
	rewards   RewardSource
	goals     GoalSource
	oracle    Oracle
	cache     *Cache
	notifier  Notifier
	logger    *slog.Logger
	clock     func() time.Time
	freshness freshness.Reader
}

// Deps bundles the Service's collaborators. pending, rewards, goals, notifier,
// Freshness may be nil (treated as empty / no-op). A nil Freshness means every
// valuation reports StalenessUnknown rather than a fabricated fresh/stale
// verdict — see portfolio.ClassifyStaleness.
type Deps struct {
	Positions PositionSource
	Pending   PendingSource
	Rewards   RewardSource
	Goals     GoalSource
	Oracle    Oracle
	Cache     *Cache
	Notifier  Notifier
	Logger    *slog.Logger
	Freshness freshness.Reader
}

// NewService constructs a Service.
func NewService(d Deps) *Service {
	logger := d.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	cache := d.Cache
	if cache == nil {
		cache = NewCache(0)
	}
	return &Service{
		positions: d.Positions,
		pending:   d.Pending,
		rewards:   d.Rewards,
		goals:     d.Goals,
		oracle:    d.Oracle,
		cache:     cache,
		notifier:  d.Notifier,
		logger:    logger,
		clock:     time.Now,
		freshness: d.Freshness,
	}
}

// GetValuation returns the user's valuation, serving a fresh cached result when
// available and computing (and caching) otherwise.
func (s *Service) GetValuation(ctx context.Context, userID uuid.UUID) (portfolio.Valuation, error) {
	if v, ok := s.cache.Get(userID); ok {
		return v, nil
	}
	return s.compute(ctx, userID)
}

// compute gathers inputs, prices, aggregates, and caches.
func (s *Service) compute(ctx context.Context, userID uuid.UUID) (portfolio.Valuation, error) {
	in := Inputs{UserID: userID, Now: s.clock()}

	positions, err := s.positions.Positions(ctx, userID)
	if err != nil {
		return portfolio.Valuation{}, err
	}
	in.Positions = positions

	if s.pending != nil {
		p, err := s.pending.PendingDeposits(ctx, userID)
		if err != nil {
			return portfolio.Valuation{}, err
		}
		in.PendingDeposits = p
	}
	if s.rewards != nil {
		r, err := s.rewards.ClaimableRewards(ctx, userID)
		if err != nil {
			return portfolio.Valuation{}, err
		}
		in.ClaimableRewards = r
	}
	if s.goals != nil {
		g, err := s.goals.Goals(ctx, userID)
		if err != nil {
			return portfolio.Valuation{}, err
		}
		in.Goals = g
	}

	prices, err := s.oracle.Prices(ctx, in.Assets())
	if err != nil {
		return portfolio.Valuation{}, err
	}

	val, err := Aggregate(in, prices)
	if err != nil {
		return portfolio.Valuation{}, err
	}
	s.applyFreshness(&val)
	s.cache.Set(userID, val)
	return val, nil
}

// applyFreshness stamps val with the current indexer freshness sample. Left
// at the zero value (AsOfLedger 0, Staleness "") when no freshness reader is
// configured — ClassifyStaleness's caller here always passes sampled=false in
// that case, so the field lands on StalenessUnknown rather than a silently
// wrong "fresh."
func (s *Service) applyFreshness(val *portfolio.Valuation) {
	if s.freshness == nil {
		val.Staleness = portfolio.ClassifyStaleness(false, false)
		return
	}
	snap := s.freshness.Snapshot()
	val.Staleness = portfolio.ClassifyStaleness(snap.Sampled, snap.Stale)
	if snap.Sampled {
		val.AsOfLedger = snap.IndexedLedger
	}
}

// Invalidate drops the user's cached valuation and, when a notifier is
// configured, asynchronously recomputes and pushes the fresh valuation. Wire
// this to events that change a user's positions (deposit/withdrawal confirmed,
// harvest recorded, price refresh).
func (s *Service) Invalidate(userID uuid.UUID) {
	s.cache.Invalidate(userID)
	if s.notifier == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		val, err := s.compute(ctx, userID)
		if err != nil {
			s.logger.Error("valuation: recompute after invalidation failed", "user_id", userID, "error", err)
			return
		}
		s.notifier.PushValuation(userID, val)
	}()
}

// discardWriter is the slog fallback for a nil logger.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
