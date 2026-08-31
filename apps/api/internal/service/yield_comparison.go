package service

import (
	"context"
	"sort"
	"strings"
)

// YieldComparisonEntry is the protocol-level representation returned by the
// yield comparison endpoint.
type YieldComparisonEntry struct {
	Protocol   string  `json:"protocol"`
	Symbol     string  `json:"symbol,omitempty"`
	CurrentAPY float64 `json:"current_apy"`
	TVLUSD     float64 `json:"tvl_usd"`
	RiskScore  float64 `json:"risk_score"`
	RiskTier   string  `json:"risk_tier"`
}

// YieldComparison is the side-by-side collection of active yield sources.
type YieldComparison struct {
	Protocols []YieldComparisonEntry `json:"protocols"`
}

// GetYieldComparison returns one comparison entry per active protocol. When a
// protocol has more than one active pool, APY and risk are TVL-weighted and TVL
// is summed across those pools.
func (s *YieldService) GetYieldComparison(ctx context.Context, chain string, limit int) (YieldComparison, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 100 {
		limit = 100
	}

	opportunities, err := s.GetYieldOpportunitiesByTier(ctx, chain, limit, "")
	if err != nil {
		return YieldComparison{}, err
	}

	type aggregate struct {
		entry        YieldComparisonEntry
		weightedAPY  float64
		weightedRisk float64
		zeroTVLCount int
	}

	aggregates := make(map[string]*aggregate)
	order := make([]string, 0, len(opportunities.Pools))
	for _, pool := range opportunities.Pools {
		name := strings.TrimSpace(pool.Project)
		if name == "" {
			name = pool.Pool
		}
		key := strings.ToLower(name)
		item, ok := aggregates[key]
		if !ok {
			item = &aggregate{entry: YieldComparisonEntry{
				Protocol: name,
				Symbol:   pool.Symbol,
			}}
			aggregates[key] = item
			order = append(order, key)
		}

		tvl := pool.TVLUsd
		item.entry.TVLUSD += tvl
		if tvl > 0 {
			item.weightedAPY += pool.APY * tvl
			item.weightedRisk += pool.RiskScore * tvl
		} else {
			item.weightedAPY += pool.APY
			item.weightedRisk += pool.RiskScore
			item.zeroTVLCount++
		}
		if item.entry.Symbol == "" {
			item.entry.Symbol = pool.Symbol
		}
	}

	result := make([]YieldComparisonEntry, 0, len(order))
	for _, key := range order {
		item := aggregates[key]
		weight := item.entry.TVLUSD
		if weight > 0 {
			item.entry.CurrentAPY = item.weightedAPY / weight
			item.entry.RiskScore = item.weightedRisk / weight
		} else if item.zeroTVLCount > 0 {
			item.entry.CurrentAPY = item.weightedAPY / float64(item.zeroTVLCount)
			item.entry.RiskScore = item.weightedRisk / float64(item.zeroTVLCount)
		}
		item.entry.RiskTier = RiskTierForScore(item.entry.RiskScore)
		result = append(result, item.entry)
	}

	sort.SliceStable(result, func(i, j int) bool {
		return result[i].CurrentAPY > result[j].CurrentAPY
	})
	if len(result) > limit {
		result = result[:limit]
	}

	return YieldComparison{Protocols: result}, nil
}
