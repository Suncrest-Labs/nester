package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/apysnapshot"
)

const (
	apyPollInterval  = 6 * time.Hour
	apyHistoryWindow = 30 * 24 * time.Hour
	apyPruneAge      = 90 * 24 * time.Hour
	// apyDownsampleAge is how old a snapshot must be before it is collapsed
	// from hourly to one-per-day. Chosen well inside apyHistoryWindow so the
	// 30-day history view always sees full hourly resolution (#1318).
	apyDownsampleAge = 7 * 24 * time.Hour
	// apyAnomalyLookback bounds how far back the poller will look for a
	// "previous" snapshot to compare against when flagging implausible jumps
	// (#941). A prior snapshot older than this is treated as no baseline,
	// same as having none, since too much may have legitimately changed.
	apyAnomalyLookback = 48 * time.Hour
	// apyGapFailThreshold is how many consecutive failed collection cycles
	// must occur before we persist explicit gap markers (#1319). A single
	// transient failure is logged only; sustained outages must not leave
	// comparison endpoints showing an unlabeled last-good value.
	apyGapFailThreshold = 2
)

type APYHistoryEntry struct {
	Date string `json:"date"`
	APY  string `json:"apy"`
	TVL  string `json:"tvl"`
	// Gap is true when this point is an explicit oracle-unreachable marker
	// (#1319). APY/TVL still carry the last-good values for chart continuity.
	Gap bool `json:"gap,omitempty"`
	// FlagReason is set when the underlying snapshot was flagged (anomaly or gap).
	FlagReason string `json:"flag_reason,omitempty"`
}

type APYHistorySummary struct {
	AvgAPY string `json:"avg_apy"`
	MinAPY string `json:"min_apy"`
	MaxAPY string `json:"max_apy"`
}

type APYHistoryResponse struct {
	ProtocolSlug string            `json:"protocol_slug"`
	Snapshots    []APYHistoryEntry `json:"snapshots"`
	Summary      APYHistorySummary `json:"summary"`
}

type APYService struct {
	repo         apysnapshot.Repository
	httpClient   *http.Client
	defiLlamaURL string
	logger       *slog.Logger

	// consecutiveFails counts how many poll cycles in a row failed to produce
	// any usable snapshots (fetch error or empty Stellar set). Reset on success.
	consecutiveFails int
	// knownProtocols are protocol slugs seen on a successful poll; used to
	// write per-protocol gap markers when a later cycle fails entirely.
	knownProtocols map[string]struct{}
}

func NewAPYService(repo apysnapshot.Repository) *APYService {
	return &APYService{
		repo:           repo,
		httpClient:     &http.Client{Timeout: 15 * time.Second},
		defiLlamaURL:   "https://yields.llama.fi/pools",
		logger:         slog.Default(),
		knownProtocols: make(map[string]struct{}),
	}
}

// NewAPYServiceWithClient creates an APYService with a custom HTTP client and base URL,
// used for testing with a mock DeFiLlama server.
func NewAPYServiceWithClient(repo apysnapshot.Repository, defiLlamaURL string, client *http.Client) *APYService {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &APYService{
		repo:           repo,
		httpClient:     client,
		defiLlamaURL:   defiLlamaURL + "/pools",
		logger:         slog.Default(),
		knownProtocols: make(map[string]struct{}),
	}
}

// PollOnce runs a single poll cycle (fetch + upsert + prune). Exported for testing.
func (s *APYService) PollOnce(ctx context.Context) {
	s.poll(ctx)
}

func (s *APYService) StartScheduler(ctx context.Context) {
	ticker := time.NewTicker(apyPollInterval)
	defer ticker.Stop()
	s.poll(ctx)
	for {
		select {
		case <-ticker.C:
			s.poll(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (s *APYService) poll(ctx context.Context) {
	snapshots, err := s.fetchFromDeFiLlama(ctx)
	if err != nil {
		s.logger.Error("apy poller: fetch failed", "error", err.Error())
		s.consecutiveFails++
		s.writeGapMarkersIfNeeded(ctx, time.Now().UTC())
		return
	}
	if len(snapshots) == 0 {
		s.logger.Warn("apy poller: fetch returned no stellar snapshots")
		s.consecutiveFails++
		s.writeGapMarkersIfNeeded(ctx, time.Now().UTC())
		return
	}

	s.consecutiveFails = 0
	if s.knownProtocols == nil {
		s.knownProtocols = make(map[string]struct{})
	}
	for _, snap := range snapshots {
		s.knownProtocols[snap.ProtocolSlug] = struct{}{}
		snap = s.flagIfAnomalous(ctx, snap)
		if err := s.repo.Upsert(ctx, snap); err != nil {
			s.logger.Error("apy poller: upsert failed", "protocol", snap.ProtocolSlug, "error", err.Error())
		}
	}
	if err := s.repo.DownsampleOlderThan(ctx, apyDownsampleAge); err != nil {
		s.logger.Error("apy poller: downsample failed", "error", err.Error())
	}
	if err := s.repo.PruneOlderThan(ctx, apyPruneAge); err != nil {
		s.logger.Error("apy poller: prune failed", "error", err.Error())
	}
}

// writeGapMarkersIfNeeded persists explicit gap rows for every known protocol
// once consecutive collection failures reach apyGapFailThreshold (#1319).
func (s *APYService) writeGapMarkersIfNeeded(ctx context.Context, now time.Time) {
	if s.consecutiveFails < apyGapFailThreshold {
		return
	}
	if len(s.knownProtocols) == 0 {
		s.logger.Warn("apy poller: consecutive failures but no known protocols to gap-mark",
			"consecutive_fails", s.consecutiveFails)
		return
	}

	for slug := range s.knownProtocols {
		last, err := s.latestSnapshot(ctx, slug, now)
		if err != nil || last == nil {
			msg := ""
			if err != nil {
				msg = err.Error()
			}
			s.logger.Warn("apy poller: cannot write gap marker without last-good snapshot",
				"protocol", slug, "error", msg)
			continue
		}
		// Avoid double-marking the same cycle if CapturedAt collides.
		if apysnapshot.IsGapMarker(*last) && !last.CapturedAt.Before(now.Add(-apyPollInterval/2)) {
			continue
		}
		marker := apysnapshot.NewGapMarker(slug, *last, now, s.consecutiveFails)
		if err := s.repo.Upsert(ctx, marker); err != nil {
			s.logger.Error("apy poller: gap marker upsert failed",
				"protocol", slug, "error", err.Error())
			continue
		}
		s.logger.Warn("apy poller: wrote oracle gap marker",
			"protocol", slug, "consecutive_fails", s.consecutiveFails)
	}
}

func (s *APYService) latestSnapshot(ctx context.Context, slug string, now time.Time) (*apysnapshot.APYSnapshot, error) {
	since := now.Add(-apyHistoryWindow)
	history, err := s.repo.ListByProtocol(ctx, slug, since)
	if err != nil {
		return nil, err
	}
	if len(history) == 0 {
		return nil, nil
	}
	last := history[len(history)-1]
	return &last, nil
}

// flagIfAnomalous looks up the protocol's most recent prior snapshot within
// apyAnomalyLookback and, if the new reading is an implausible jump relative
// to it, marks snap as flagged (#941). It never rejects a snapshot outright —
// callers still persist it — and any error looking up history is logged and
// treated as "no baseline" so a lookup failure never blocks ingestion.
func (s *APYService) flagIfAnomalous(ctx context.Context, snap apysnapshot.APYSnapshot) apysnapshot.APYSnapshot {
	since := snap.CapturedAt.Add(-apyAnomalyLookback)
	history, err := s.repo.ListByProtocol(ctx, snap.ProtocolSlug, since)
	if err != nil {
		s.logger.Warn("apy poller: anomaly lookup failed", "protocol", snap.ProtocolSlug, "error", err.Error())
		return snap
	}

	var prev *apysnapshot.APYSnapshot
	for i := range history {
		if history[i].CapturedAt.Before(snap.CapturedAt) && (prev == nil || history[i].CapturedAt.After(prev.CapturedAt)) {
			prev = &history[i]
		}
	}

	if anomalous, reason := apysnapshot.DetectAnomalousJump(prev, snap); anomalous {
		snap.Flagged = true
		snap.FlagReason = reason
		s.logger.Warn("apy poller: flagged anomalous snapshot", "protocol", snap.ProtocolSlug, "reason", reason)
	}
	return snap
}

func (s *APYService) fetchFromDeFiLlama(ctx context.Context) ([]apysnapshot.APYSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.defiLlamaURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			Project string  `json:"project"`
			Chain   string  `json:"chain"`
			APY     float64 `json:"apy"`
			TVLUsd  float64 `json:"tvlUsd"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	var snapshots []apysnapshot.APYSnapshot
	seen := map[string]bool{}
	for _, pool := range result.Data {
		if strings.EqualFold(pool.Chain, "Stellar") && !seen[pool.Project] {
			seen[pool.Project] = true
			snapshots = append(snapshots, apysnapshot.APYSnapshot{
				ID:           uuid.New(),
				ProtocolSlug: pool.Project,
				APY:          decimal.NewFromFloat(pool.APY),
				TVL:          decimal.NewFromFloat(pool.TVLUsd),
				CapturedAt:   now,
			})
		}
	}
	return snapshots, nil
}

func (s *APYService) GetHistory(ctx context.Context, slug string) (*APYHistoryResponse, error) {
	since := time.Now().UTC().Add(-apyHistoryWindow)
	snaps, err := s.repo.ListByProtocol(ctx, slug, since)
	if err != nil {
		return nil, err
	}
	if len(snaps) == 0 {
		return nil, apysnapshot.ErrProtocolNotFound
	}

	entries := make([]APYHistoryEntry, len(snaps))
	var (
		minAPY   decimal.Decimal
		maxAPY   decimal.Decimal
		sumAPY   = decimal.Zero
		liveN    int64
		minMaxSet bool
	)

	for i, snap := range snaps {
		entries[i] = APYHistoryEntry{
			Date:       snap.CapturedAt.Format("2006-01-02"),
			APY:        snap.APY.StringFixed(4),
			TVL:        snap.TVL.StringFixed(6),
			Gap:        apysnapshot.IsGapMarker(snap),
			FlagReason: snap.FlagReason,
		}
		// Summary stats use live readings only so gap placeholders do not
		// dominate averages after a multi-cycle outage (#1319).
		if apysnapshot.IsGapMarker(snap) {
			continue
		}
		if !minMaxSet {
			minAPY = snap.APY
			maxAPY = snap.APY
			minMaxSet = true
		} else {
			if snap.APY.LessThan(minAPY) {
				minAPY = snap.APY
			}
			if snap.APY.GreaterThan(maxAPY) {
				maxAPY = snap.APY
			}
		}
		sumAPY = sumAPY.Add(snap.APY)
		liveN++
	}

	avgAPY := decimal.Zero
	if liveN > 0 {
		avgAPY = sumAPY.Div(decimal.NewFromInt(liveN))
	} else {
		// All points were gaps: fall back to last-good values for a non-empty summary.
		minAPY = snaps[len(snaps)-1].APY
		maxAPY = minAPY
		avgAPY = minAPY
	}

	return &APYHistoryResponse{
		ProtocolSlug: slug,
		Snapshots:    entries,
		Summary: APYHistorySummary{
			AvgAPY: avgAPY.StringFixed(4),
			MinAPY: minAPY.StringFixed(4),
			MaxAPY: maxAPY.StringFixed(4),
		},
	}, nil
}
