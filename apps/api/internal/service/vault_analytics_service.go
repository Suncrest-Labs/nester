package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	perfdom "github.com/suncrestlabs/nester/apps/api/internal/domain/performance"
)

// VaultAnalytics holds risk-adjusted performance metrics for a vault.
type VaultAnalytics struct {
	VaultID      uuid.UUID `json:"vault_id"`
	Period       string    `json:"period"`
	MeanAPY      float64   `json:"mean_apy"`
	APYVolatility float64  `json:"apy_volatility"`
	MaxDrawdown  float64   `json:"max_drawdown"`
	SharpeRatio  float64   `json:"sharpe_ratio"`
	SortinoRatio float64   `json:"sortino_ratio"`
	WinRate      float64   `json:"win_rate"`
}

type analyticsCache struct {
	mu      sync.Mutex
	entries map[string]analyticsCacheEntry
}

type analyticsCacheEntry struct {
	data      VaultAnalytics
	expiresAt time.Time
}

var _analyticsCache = &analyticsCache{
	entries: make(map[string]analyticsCacheEntry),
}

const analyticsCacheTTL = time.Hour

func (c *analyticsCache) get(key string) (VaultAnalytics, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return VaultAnalytics{}, false
	}
	return entry.data, true
}

func (c *analyticsCache) set(key string, data VaultAnalytics) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = analyticsCacheEntry{data: data, expiresAt: time.Now().Add(analyticsCacheTTL)}
}

// VaultAnalyticsService computes higher-order vault performance metrics.
type VaultAnalyticsService struct {
	repo         perfdom.SnapshotRepository
	riskFreeRate float64
}

// NewVaultAnalyticsService creates a VaultAnalyticsService.
// The risk-free rate defaults to the RISK_FREE_RATE env var, or 0.05 (5%).
func NewVaultAnalyticsService(repo perfdom.SnapshotRepository) *VaultAnalyticsService {
	rfr := 0.05
	if v := os.Getenv("RISK_FREE_RATE"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil && parsed >= 0 {
			rfr = parsed
		}
	}
	return &VaultAnalyticsService{repo: repo, riskFreeRate: rfr}
}

// analyticsPeriodDays converts a period string to a day count.
func analyticsPeriodDays(period string) (int, error) {
	if period == "all" {
		return 365 * 5, nil
	}
	if !strings.HasSuffix(period, "d") {
		return 0, errors.New("period must be '30d', '90d', '365d', or 'all'")
	}
	n, err := strconv.Atoi(strings.TrimSuffix(period, "d"))
	if err != nil || n <= 0 || n > 365*5 {
		return 0, errors.New("period must be a positive number of days, capped at 5y")
	}
	return n, nil
}

// Compute returns analytics for the vault over the given period string ("30d", "90d", "365d").
// Results are cached for one hour.
func (s *VaultAnalyticsService) Compute(ctx context.Context, vaultID uuid.UUID, period string) (VaultAnalytics, error) {
	cacheKey := fmt.Sprintf("%s:%s", vaultID, period)
	if cached, ok := _analyticsCache.get(cacheKey); ok {
		return cached, nil
	}

	days, err := analyticsPeriodDays(period)
	if err != nil {
		return VaultAnalytics{}, fmt.Errorf("invalid period %q: %w", period, err)
	}

	since := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	snapshots, err := s.repo.HistoryForVault(ctx, vaultID, since)
	if err != nil {
		return VaultAnalytics{}, fmt.Errorf("fetch snapshots: %w", err)
	}

	result := s.compute(vaultID, period, snapshots)
	_analyticsCache.set(cacheKey, result)
	return result, nil
}

func (s *VaultAnalyticsService) compute(vaultID uuid.UUID, period string, snapshots []perfdom.Snapshot) VaultAnalytics {
	out := VaultAnalytics{VaultID: vaultID, Period: period}
	if len(snapshots) < 2 {
		return out
	}

	// Compute daily APY implied by consecutive share-price changes.
	dailyAPYs := make([]float64, 0, len(snapshots)-1)
	peakPrice := decimal.Zero

	for i, snap := range snapshots {
		if snap.SharePrice.GreaterThan(peakPrice) {
			peakPrice = snap.SharePrice
		}
		if i == 0 {
			continue
		}
		prev := snapshots[i-1]
		if prev.SharePrice.IsZero() || prev.SharePrice.Sign() <= 0 {
			continue
		}

		// Annualised: (price_t / price_t-1)^365 - 1
		ratio, _ := snap.SharePrice.Div(prev.SharePrice).Float64()
		if ratio <= 0 {
			continue
		}
		hoursElapsed := snap.SnapshotAt.Sub(prev.SnapshotAt).Hours()
		if hoursElapsed <= 0 {
			continue
		}
		daysElapsed := hoursElapsed / 24
		apy := (math.Pow(ratio, 365.0/daysElapsed) - 1) * 100
		if math.IsNaN(apy) || math.IsInf(apy, 0) {
			continue
		}
		dailyAPYs = append(dailyAPYs, apy)
	}

	if len(dailyAPYs) == 0 {
		return out
	}

	out.MeanAPY = mean(dailyAPYs)
	out.APYVolatility = stdDev(dailyAPYs, out.MeanAPY)
	out.MaxDrawdown = s.maxDrawdown(snapshots)
	out.WinRate = winRate(dailyAPYs)

	if out.APYVolatility > 0 {
		out.SharpeRatio = (out.MeanAPY - s.riskFreeRate*100) / out.APYVolatility
		out.SortinoRatio = sortinoRatio(dailyAPYs, out.MeanAPY, s.riskFreeRate*100)
	}

	return out
}

func (s *VaultAnalyticsService) maxDrawdown(snapshots []perfdom.Snapshot) float64 {
	peak := decimal.Zero
	maxDD := 0.0
	for _, snap := range snapshots {
		if snap.SharePrice.GreaterThan(peak) {
			peak = snap.SharePrice
		}
		if peak.IsZero() {
			continue
		}
		dd, _ := peak.Sub(snap.SharePrice).Div(peak).Float64()
		if dd > maxDD {
			maxDD = dd
		}
	}
	return maxDD
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func stdDev(xs []float64, avg float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	variance := 0.0
	for _, x := range xs {
		diff := x - avg
		variance += diff * diff
	}
	return math.Sqrt(variance / float64(len(xs)))
}

func sortinoRatio(xs []float64, avg, riskFreeRate float64) float64 {
	downside := 0.0
	count := 0
	for _, x := range xs {
		if x < riskFreeRate {
			diff := x - riskFreeRate
			downside += diff * diff
			count++
		}
	}
	if count == 0 {
		return 0
	}
	downsideVol := math.Sqrt(downside / float64(count))
	if downsideVol == 0 {
		return 0
	}
	return (avg - riskFreeRate) / downsideVol
}

func winRate(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	wins := 0
	for _, x := range xs {
		if x > 0 {
			wins++
		}
	}
	return float64(wins) / float64(len(xs))
}

