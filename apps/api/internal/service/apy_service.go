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
	// apyAnomalyLookback bounds how far back the poller will look for a
	// "previous" snapshot to compare against when flagging implausible jumps
	// (#941). A prior snapshot older than this is treated as no baseline,
	// same as having none, since too much may have legitimately changed.
	apyAnomalyLookback = 48 * time.Hour
)

type APYHistoryEntry struct {
	Date string `json:"date"`
	APY  string `json:"apy"`
	TVL  string `json:"tvl"`
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
}

func NewAPYService(repo apysnapshot.Repository) *APYService {
	return &APYService{
		repo:            repo,
		httpClient:      &http.Client{Timeout: 15 * time.Second},
		defiLlamaURL:    "https://yields.llama.fi/pools",
		logger:          slog.Default(),
	}
}

// NewAPYServiceWithClient creates an APYService with a custom HTTP client and base URL,
// used for testing with a mock DeFiLlama server.
func NewAPYServiceWithClient(repo apysnapshot.Repository, defiLlamaURL string, client *http.Client) *APYService {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &APYService{
		repo:         repo,
		httpClient:   client,
		defiLlamaURL: defiLlamaURL + "/pools",
		logger:       slog.Default(),
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
		return
	}
	for _, snap := range snapshots {
		snap = s.flagIfAnomalous(ctx, snap)
		if err := s.repo.Upsert(ctx, snap); err != nil {
			s.logger.Error("apy poller: upsert failed", "protocol", snap.ProtocolSlug, "error", err.Error())
		}
	}
	if err := s.repo.PruneOlderThan(ctx, apyPruneAge); err != nil {
		s.logger.Error("apy poller: prune failed", "error", err.Error())
	}
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
	minAPY := snaps[0].APY
	maxAPY := snaps[0].APY
	sumAPY := decimal.Zero

	for i, snap := range snaps {
		entries[i] = APYHistoryEntry{
			Date: snap.CapturedAt.Format("2006-01-02"),
			APY:  snap.APY.StringFixed(4),
			TVL:  snap.TVL.StringFixed(6),
		}
		if snap.APY.LessThan(minAPY) {
			minAPY = snap.APY
		}
		if snap.APY.GreaterThan(maxAPY) {
			maxAPY = snap.APY
		}
		sumAPY = sumAPY.Add(snap.APY)
	}

	avgAPY := sumAPY.Div(decimal.NewFromInt(int64(len(snaps))))

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
