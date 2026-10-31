package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savings"
	stellarpkg "github.com/suncrestlabs/nester/apps/api/internal/stellar"
)

// ErrInvalidSavingsPeriod is returned by History for a period other than
// 7d, 30d or 90d.
var ErrInvalidSavingsPeriod = errors.New("period must be one of 7d, 30d, 90d")

var savingsPeriods = map[string]time.Duration{
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
	"90d": 90 * 24 * time.Hour,
}

// SavingsChain is the on-chain read surface the savings service needs.
// stellar.ChainSavingsReader implements it.
type SavingsChain interface {
	State(ctx context.Context) (savings.ChainState, error)
	// Balance returns the wallet's raw share balance in the core contract.
	Balance(ctx context.Context, wallet string) (decimal.Decimal, error)
	// ConvertToAssets returns the raw asset value of a raw share amount.
	ConvertToAssets(ctx context.Context, shares decimal.Decimal) (decimal.Decimal, error)
}

// SavingsService serves the savings-rate vault: live contract state, trailing
// realized rates from stored snapshots, and per-wallet positions.
type SavingsService struct {
	chain  SavingsChain
	repo   savings.SavingsRepository
	clock  func() time.Time
	leader LeaderChecker
	logger *slog.Logger
}

// LeaderChecker gates the snapshot job so only one API instance writes.
type LeaderChecker interface {
	IsLeader() bool
}

func NewSavingsService(chain SavingsChain, repo savings.SavingsRepository) *SavingsService {
	return &SavingsService{
		chain:  chain,
		repo:   repo,
		clock:  func() time.Time { return time.Now().UTC() },
		logger: slog.Default(),
	}
}

// SetLeaderChecker wires leader election for the snapshot job. A nil checker
// means always leader.
func (s *SavingsService) SetLeaderChecker(l LeaderChecker) { s.leader = l }

// SetLogger overrides the logger used by the snapshot job.
func (s *SavingsService) SetLogger(l *slog.Logger) { s.logger = l }

type SavingsReserve struct {
	Shares string `json:"shares"`
	Value  string `json:"value"`
}

type SavingsAdapter struct {
	Address   string `json:"address"`
	Name      string `json:"name"`
	Value     string `json:"value"`
	AbsCap    string `json:"absCap"`
	RelCapBps uint32 `json:"relCapBps"`
	Share     string `json:"share"`
}

type SavingsPending struct {
	ID   uint32 `json:"id"`
	Kind string `json:"kind"`
	// ETA is the unix time in seconds after which anyone may execute the change.
	ETA int64 `json:"eta"`
}

// SavingsSummary is the public savings-rate overview. Amounts are decimal USDC
// strings, share price is USDC per share, and rates are annualized realized
// percentages (null until enough history exists).
type SavingsSummary struct {
	Rate7d          *string          `json:"rate7d"`
	Rate30d         *string          `json:"rate30d"`
	SharePrice      string           `json:"sharePrice"`
	TotalAssets     string           `json:"totalAssets"`
	Idle            string           `json:"idle"`
	FeeBps          uint32           `json:"feeBps"`
	TimelockSeconds int64            `json:"timelockSeconds"`
	Reserve         SavingsReserve   `json:"reserve"`
	Adapters        []SavingsAdapter `json:"adapters"`
	Pending         []SavingsPending `json:"pending"`
}

type SavingsHistoryPoint struct {
	TakenAt    time.Time `json:"takenAt"`
	SharePrice string    `json:"sharePrice"`
}

// SavingsPosition is one wallet's holding. Deposited is null: per-wallet
// deposit totals are not tracked for the savings core.
type SavingsPosition struct {
	Shares    string  `json:"shares"`
	Value     string  `json:"value"`
	Deposited *string `json:"deposited"`
}

func units(raw decimal.Decimal) string { return stellarpkg.StroopsToAssetUnits(raw).String() }

func (s *SavingsService) rate(snaps []savings.Snapshot, window time.Duration, now time.Time) *string {
	r := savings.TrailingRate(snaps, window, now)
	if r == nil {
		return nil
	}
	out := r.String()
	return &out
}

// Summary reads live contract state and derives the trailing rates from
// stored snapshots.
func (s *SavingsService) Summary(ctx context.Context) (SavingsSummary, error) {
	st, err := s.chain.State(ctx)
	if err != nil {
		return SavingsSummary{}, fmt.Errorf("read savings state: %w", err)
	}
	reserveValue, err := s.chain.ConvertToAssets(ctx, st.ReserveShares)
	if err != nil {
		return SavingsSummary{}, fmt.Errorf("value reserve shares: %w", err)
	}

	now := s.clock()
	snaps, err := s.repo.Since(ctx, now.Add(-savingsPeriods["30d"]))
	if err != nil {
		return SavingsSummary{}, fmt.Errorf("load savings snapshots: %w", err)
	}

	out := SavingsSummary{
		Rate7d:          s.rate(snaps, savingsPeriods["7d"], now),
		Rate30d:         s.rate(snaps, savingsPeriods["30d"], now),
		SharePrice:      units(st.SharePrice),
		TotalAssets:     units(st.TotalAssets),
		Idle:            units(st.Idle),
		FeeBps:          st.FeeBps,
		TimelockSeconds: int64(st.Timelock / time.Second),
		Reserve:         SavingsReserve{Shares: units(st.ReserveShares), Value: units(reserveValue)},
		Adapters:        make([]SavingsAdapter, 0, len(st.Adapters)),
		Pending:         make([]SavingsPending, 0, len(st.Pending)),
	}
	for _, a := range st.Adapters {
		share := decimal.Zero
		if st.TotalAssets.IsPositive() {
			share = a.Value.Div(st.TotalAssets)
		}
		out.Adapters = append(out.Adapters, SavingsAdapter{
			Address:   a.Address,
			Name:      a.Name,
			Value:     units(a.Value),
			AbsCap:    units(a.AbsCap),
			RelCapBps: a.RelCapBps,
			Share:     share.String(),
		})
	}
	for _, p := range st.Pending {
		out.Pending = append(out.Pending, SavingsPending{ID: p.ID, Kind: p.Kind, ETA: p.ETA.Unix()})
	}
	return out, nil
}

// History returns share-price snapshots for period (7d, 30d or 90d).
func (s *SavingsService) History(ctx context.Context, period string) ([]SavingsHistoryPoint, error) {
	window, ok := savingsPeriods[period]
	if !ok {
		return nil, ErrInvalidSavingsPeriod
	}
	snaps, err := s.repo.Since(ctx, s.clock().Add(-window))
	if err != nil {
		return nil, fmt.Errorf("load savings snapshots: %w", err)
	}
	out := make([]SavingsHistoryPoint, 0, len(snaps))
	for _, sn := range snaps {
		out = append(out, SavingsHistoryPoint{TakenAt: sn.TakenAt, SharePrice: units(sn.SharePrice)})
	}
	return out, nil
}

// Position returns the wallet's share balance and its current asset value.
func (s *SavingsService) Position(ctx context.Context, wallet string) (SavingsPosition, error) {
	shares, err := s.chain.Balance(ctx, wallet)
	if err != nil {
		return SavingsPosition{}, fmt.Errorf("read savings balance: %w", err)
	}
	value, err := s.chain.ConvertToAssets(ctx, shares)
	if err != nil {
		return SavingsPosition{}, fmt.Errorf("value savings shares: %w", err)
	}
	return SavingsPosition{Shares: units(shares), Value: units(value)}, nil
}

// Snapshot reads the contract state and stores one snapshot.
func (s *SavingsService) Snapshot(ctx context.Context) error {
	st, err := s.chain.State(ctx)
	if err != nil {
		return fmt.Errorf("read savings state: %w", err)
	}
	return s.repo.Insert(ctx, savings.Snapshot{
		TakenAt:       s.clock(),
		SharePrice:    st.SharePrice,
		TotalAssets:   st.TotalAssets,
		TotalSupply:   st.TotalSupply,
		ReserveShares: st.ReserveShares,
	})
}

// SnapshotTick runs one snapshot pass (job savings.snapshot), skipping when
// this instance is not the scheduler leader. Failures are logged, not fatal.
func (s *SavingsService) SnapshotTick(ctx context.Context) {
	if s.leader != nil && !s.leader.IsLeader() {
		return
	}
	if err := s.Snapshot(ctx); err != nil {
		s.logger.Error("savings.snapshot failed", "error", err.Error())
	}
}

// RunSnapshots ticks every interval until ctx is cancelled, firing once
// immediately.
func (s *SavingsService) RunSnapshots(ctx context.Context, interval time.Duration) {
	s.SnapshotTick(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.SnapshotTick(ctx)
		}
	}
}
