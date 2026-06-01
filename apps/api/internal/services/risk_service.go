package services

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// RiskScore represents the risk score for a vault.
type RiskScore struct {
	Overall           float64
	Tier              string
	ConcentrationRisk float64
	ProtocolRisk      float64
	YieldVolatility   float64
	LiquidityRisk     float64
	ComputedAt        time.Time
}

// RiskService computes risk scores for vaults.
type RiskService struct {
	vaultRepo vault.Repository
	cache     map[uuid.UUID]*RiskScore
	cacheMu   sync.RWMutex
	cacheTTL  time.Duration
}

// NewRiskService creates a new RiskService.
func NewRiskService(vaultRepo vault.Repository) *RiskService {
	return &RiskService{
		vaultRepo: vaultRepo,
		cache:     make(map[uuid.UUID]*RiskScore),
		cacheTTL:  time.Hour, // 1 hour cache
	}
}

// Score computes the risk score for a vault.
// It caches the result for 1 hour.
// It returns an error if the vault is not found or if there are no allocations (empty vault).
func (s *RiskService) Score(ctx context.Context, vaultID uuid.UUID) (*RiskScore, error) {
	// Check cache first
	s.cacheMu.RLock()
	if cached, found := s.cache[vaultID]; found {
		if time.Since(cached.ComputedAt) < s.cacheTTL {
			s.cacheMu.RUnlock()
			return cached, nil
		}
	}
	s.cacheMu.RUnlock()

	// Fetch the vault
	v, err := s.vaultRepo.GetVault(ctx, vaultID)
	if err != nil {
		if errors.Is(err, vault.ErrVaultNotFound) {
			return nil, err
		}
		return nil, err
	}

	// If the vault has no allocations, return an error (as per requirement)
	if len(v.Allocations) == 0 {
		return nil, errors.New("empty vault: no allocations")
	}

	// Compute the risk score
	score := s.computeRiskScore(&v)

	// Store in cache
	s.cacheMu.Lock()
	s.cache[vaultID] = score
	s.cacheMu.Unlock()

	// TODO: In a real implementation, we would insert into the vault_risk_snapshots table.
	// For now, we'll just return the score.

	return score, nil
}

// computeRiskScore calculates the risk score for a vault based on its allocations.
// This is where the actual scoring logic lives.
func (s *RiskService) computeRiskScore(vault *vault.Vault) *RiskScore {
	// Weights for each risk dimension
	const (
		weightConcentration = 0.35
		weightProtocol      = 0.30
		weightYieldVol      = 0.20
		weightLiquidity     = 0.15
	)

	// 1. Concentration Risk (HHI)
	var hhi float64
	totalBalance := vault.CurrentBalance.InexactFloat64()
	if totalBalance > 0 {
		for _, alloc := range vault.Allocations {
			allocFraction := alloc.Amount.InexactFloat64() / totalBalance
			hhi += allocFraction * allocFraction
		}
	}
	// HHI is already in [0,1] where 1 is fully concentrated.

	// 2. Protocol Risk
	// Protocol risk ratings (hardcoded as per spec)
	protocolRiskRatings := map[string]float64{
		"Aave":    0.2,
		"Blend":   0.3,
		"Compound":0.25,
		"unknown": 0.8,
	}
	var protocolRisk float64
	if totalBalance > 0 {
		for _, alloc := range vault.Allocations {
			allocFraction := alloc.Amount.InexactFloat64() / totalBalance
			risk := protocolRiskRatings[alloc.Protocol]
			if risk == 0 { // if not found, use unknown
				risk = protocolRiskRatings["unknown"]
			}
			protocolRisk += allocFraction * risk
		}
	}

	// 3. Yield Volatility
	// For simplicity, we'll use a placeholder. In a real implementation, we would
	// look at the historical APY for the vault over the last 30 days.
	// Since we don't have that data readily available, we'll set it to 0.1 (10% volatility)
	// and then normalize by 0.20 (so 0.1/0.20 = 0.5).
	// But note: the spec says to clamp to [0, 20%] then divide by 0.20.
	// We'll assume we have a function to get the volatility. For now, we'll use a mock.
	yieldVolatility := 0.1 // placeholder for standard deviation of daily APY over last 30 days
	if yieldVolatility > 0.20 {
		yieldVolatility = 0.20
	}
	normalizedYieldVol := yieldVolatility / 0.20

	// 4. Liquidity Risk
	// We need the protocol TVL for each vault's protocol to compute the ratio.
	// Since we don't have that data, we'll use a placeholder.
	// In a real implementation, we would fetch the TVL for each protocol and then
	// compute the vault's balance as a fraction of the protocol's TVL.
	// For now, we'll assume a low liquidity risk (0.05) for demonstration.
	liquidityRisk := 0.05 // placeholder for vault_balance / protocol_TVL ratio, clamped to [0,1]

	// Overall score (before multiplying by 100)
	overall := (hhi*weightConcentration +
		protocolRisk*weightProtocol +
		normalizedYieldVol*weightYieldVol +
		liquidityRisk*weightLiquidity) * 100

	// Determine tier
	var tier string
	switch {
	case overall >= 0 && overall <= 33:
		tier = "low"
	case overall >= 34 && overall <= 66:
		tier = "medium"
	case overall >= 67 && overall <= 100:
		tier = "high"
	default:
		// This should not happen if the math is correct, but just in case.
		if overall < 0 {
			tier = "low"
		} else {
			tier = "high"
		}
	}

	return &RiskScore{
		Overall:           overall,
		Tier:              tier,
		ConcentrationRisk: hhi * 100, // Convert to 0-100 scale for consistency in output
		ProtocolRisk:      protocolRisk * 100,
		YieldVolatility:   normalizedYieldVol * 100,
		LiquidityRisk:     liquidityRisk * 100,
		ComputedAt:        time.Now().UTC(),
	}
}