package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

var (
	ErrEmptyVault     = errors.New("empty vault: no allocations")
	ErrRiskNotFound   = errors.New("risk score not found")
)

type RiskFactor struct {
	Name       string  `json:"name"`
	Score      float64 `json:"score"`
	Weight     float64 `json:"weight"`
	Reason     string  `json:"reason"`
	Available  bool    `json:"available"`
	Confidence float64 `json:"confidence"`
}

type RiskScore struct {
	ID          uuid.UUID    `json:"id"`
	VaultID     uuid.UUID    `json:"vault_id"`
	Overall     float64      `json:"overall"`
	Confidence  float64      `json:"confidence"`
	Tier        string       `json:"tier"`
	Factors     []RiskFactor `json:"factors"`
	ComputedAt  time.Time    `json:"computed_at"`
}

type RiskScoreHistory struct {
	Scores []RiskScore `json:"scores"`
	Trend  string      `json:"trend"`
}

type RiskWeights struct {
	Concentration float64
	Protocol      float64
	YieldVol      float64
	Liquidity     float64
	Drawdown      float64
	Age           float64
}

type RiskService struct {
	vaultRepo vault.Repository
	db        *sql.DB
	cache     map[uuid.UUID]*RiskScore
	cacheMu   sync.RWMutex
	cacheTTL  time.Duration
	weights   RiskWeights
}

func NewRiskService(vaultRepo vault.Repository, db *sql.DB) *RiskService {
	return &RiskService{
		vaultRepo: vaultRepo,
		db:        db,
		cache:     make(map[uuid.UUID]*RiskScore),
		cacheTTL:  time.Hour,
		weights: RiskWeights{
			Concentration: 0.25,
			Protocol:      0.20,
			YieldVol:      0.20,
			Liquidity:     0.15,
			Drawdown:      0.10,
			Age:           0.10,
		},
	}
}

func (s *RiskService) Score(ctx context.Context, vaultID uuid.UUID) (*RiskScore, error) {
	s.cacheMu.RLock()
	if cached, found := s.cache[vaultID]; found {
		if time.Since(cached.ComputedAt) < s.cacheTTL {
			s.cacheMu.RUnlock()
			return cached, nil
		}
	}
	s.cacheMu.RUnlock()

	v, err := s.vaultRepo.GetVault(ctx, vaultID)
	if err != nil {
		if errors.Is(err, vault.ErrVaultNotFound) {
			return nil, err
		}
		return nil, err
	}

	if len(v.Allocations) == 0 {
		return nil, ErrEmptyVault
	}

	score := s.computeRiskScore(ctx, &v)
	score.VaultID = vaultID
	score.ID = uuid.New()

	if s.db != nil {
		if err := s.persistScore(ctx, score); err != nil {
			return score, nil
		}
	}

	s.cacheMu.Lock()
	s.cache[vaultID] = score
	s.cacheMu.Unlock()

	return score, nil
}

func (s *RiskService) ScoreOnDemand(ctx context.Context, vaultID uuid.UUID) (*RiskScore, error) {
	s.cacheMu.Lock()
	delete(s.cache, vaultID)
	s.cacheMu.Unlock()
	return s.Score(ctx, vaultID)
}

func (s *RiskService) GetHistory(ctx context.Context, vaultID uuid.UUID, limit int) (*RiskScoreHistory, error) {
	if s.db == nil {
		return nil, errors.New("database not available")
	}

	if limit <= 0 || limit > 100 {
		limit = 20
	}

	query := `
		SELECT id, vault_id, overall_score, confidence, tier, factors, computed_at
		FROM risk_scores
		WHERE vault_id = $1
		ORDER BY computed_at DESC
		LIMIT $2
	`

	rows, err := s.db.QueryContext(ctx, query, vaultID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scores []RiskScore
	for rows.Next() {
		var score RiskScore
		var factorsJSON []byte
		err := rows.Scan(
			&score.ID,
			&score.VaultID,
			&score.Overall,
			&score.Confidence,
			&score.Tier,
			&factorsJSON,
			&score.ComputedAt,
		)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(factorsJSON, &score.Factors); err != nil {
			return nil, err
		}
		scores = append(scores, score)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	trend := s.computeTrend(scores)

	return &RiskScoreHistory{
		Scores: scores,
		Trend:  trend,
	}, nil
}

func (s *RiskService) CheckDeterioration(ctx context.Context, vaultID uuid.UUID, threshold float64) (bool, error) {
	history, err := s.GetHistory(ctx, vaultID, 5)
	if err != nil {
		return false, err
	}

	if len(history.Scores) < 2 {
		return false, nil
	}

	latest := history.Scores[0].Overall
	previous := history.Scores[1].Overall

	deterioration := latest - previous
	return deterioration >= threshold, nil
}

func (s *RiskService) computeTrend(scores []RiskScore) string {
	if len(scores) < 2 {
		return "insufficient_data"
	}

	latest := scores[0].Overall
	oldest := scores[len(scores)-1].Overall

	change := latest - oldest
	if change > 5 {
		return "increasing"
	} else if change < -5 {
		return "decreasing"
	}
	return "stable"
}

func (s *RiskService) persistScore(ctx context.Context, score *RiskScore) error {
	factorsJSON, err := json.Marshal(score.Factors)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO risk_scores (id, vault_id, overall_score, confidence, tier, factors, computed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err = s.db.ExecContext(
		ctx,
		query,
		score.ID,
		score.VaultID,
		score.Overall,
		score.Confidence,
		score.Tier,
		factorsJSON,
		score.ComputedAt,
	)
	return err
}

func (s *RiskService) computeRiskScore(ctx context.Context, v *vault.Vault) *RiskScore {
	factors := make([]RiskFactor, 0, 6)

	concentrationFactor := s.computeConcentrationRisk(v)
	factors = append(factors, concentrationFactor)

	protocolFactor := s.computeProtocolRisk(v)
	factors = append(factors, protocolFactor)

	yieldVolFactor := s.computeYieldVolatility(ctx, v)
	factors = append(factors, yieldVolFactor)

	liquidityFactor := s.computeLiquidityRisk(ctx, v)
	factors = append(factors, liquidityFactor)

	drawdownFactor := s.computeDrawdownRisk(ctx, v)
	factors = append(factors, drawdownFactor)

	ageFactor := s.computeProtocolAgeRisk(ctx, v)
	factors = append(factors, ageFactor)

	overall, confidence := s.computeWeightedScore(factors)

	tier := s.computeTier(overall)

	return &RiskScore{
		Overall:    overall,
		Confidence: confidence,
		Tier:       tier,
		Factors:    factors,
		ComputedAt: time.Now().UTC(),
	}
}

func (s *RiskService) computeConcentrationRisk(v *vault.Vault) RiskFactor {
	var hhi float64
	totalBalance := v.CurrentBalance.InexactFloat64()

	if totalBalance > 0 {
		for _, alloc := range v.Allocations {
			allocFraction := alloc.Amount.InexactFloat64() / totalBalance
			hhi += allocFraction * allocFraction
		}
	}

	score := hhi * 100
	reason := "HHI concentration across protocols"

	if len(v.Allocations) == 1 {
		reason = "single protocol concentration"
	} else if hhi > 0.5 {
		reason = "high concentration in few protocols"
	}

	return RiskFactor{
		Name:       "concentration",
		Score:      score,
		Weight:     s.weights.Concentration,
		Reason:     reason,
		Available:  true,
		Confidence: 1.0,
	}
}

func (s *RiskService) computeProtocolRisk(v *vault.Vault) RiskFactor {
	protocolRiskRatings := map[string]float64{
		"aave":     0.2,
		"blend":    0.3,
		"compound": 0.25,
		"unknown":  0.8,
	}

	var protocolRisk float64
	totalBalance := v.CurrentBalance.InexactFloat64()

	if totalBalance > 0 {
		for _, alloc := range v.Allocations {
			allocFraction := alloc.Amount.InexactFloat64() / totalBalance
			protocol := alloc.Protocol
			risk := protocolRiskRatings[protocol]
			if risk == 0 {
				risk = protocolRiskRatings["unknown"]
			}
			protocolRisk += allocFraction * risk
		}
	}

	score := protocolRisk * 100
	reason := "weighted protocol risk ratings"

	return RiskFactor{
		Name:       "protocol",
		Score:      score,
		Weight:     s.weights.Protocol,
		Reason:     reason,
		Available:  true,
		Confidence: 0.9,
	}
}

func (s *RiskService) computeYieldVolatility(ctx context.Context, v *vault.Vault) RiskFactor {
	if len(v.Allocations) == 0 {
		return RiskFactor{
			Name:       "yield_volatility",
			Score:      50,
			Weight:     s.weights.YieldVol,
			Reason:     "no allocations to measure",
			Available:  false,
			Confidence: 0,
		}
	}

	var totalAPYVariance float64
	var apyCount int

	for _, alloc := range v.Allocations {
		apy := alloc.APY.InexactFloat64()
		if apy > 0 {
			variance := s.estimateAPYVariance(apy)
			totalAPYVariance += variance
			apyCount++
		}
	}

	if apyCount == 0 {
		return RiskFactor{
			Name:       "yield_volatility",
			Score:      50,
			Weight:     s.weights.YieldVol,
			Reason:     "APY data unavailable",
			Available:  false,
			Confidence: 0,
		}
	}

	avgVariance := totalAPYVariance / float64(apyCount)
	score := s.normalizeVarianceToScore(avgVariance)

	reason := "APY volatility over measurement period"
	confidence := 0.7

	return RiskFactor{
		Name:       "yield_volatility",
		Score:      score,
		Weight:     s.weights.YieldVol,
		Reason:     reason,
		Available:  true,
		Confidence: confidence,
	}
}

func (s *RiskService) estimateAPYVariance(currentAPY float64) float64 {
	if currentAPY > 0.5 {
		return 0.15
	} else if currentAPY > 0.2 {
		return 0.08
	} else if currentAPY > 0.1 {
		return 0.04
	}
	return 0.02
}

func (s *RiskService) normalizeVarianceToScore(variance float64) float64 {
	if variance > 0.20 {
		variance = 0.20
	}
	return (variance / 0.20) * 100
}

func (s *RiskService) computeLiquidityRisk(ctx context.Context, v *vault.Vault) RiskFactor {
	totalBalance := v.CurrentBalance.InexactFloat64()

	if totalBalance == 0 {
		return RiskFactor{
			Name:       "liquidity",
			Score:      50,
			Weight:     s.weights.Liquidity,
			Reason:     "no balance to assess",
			Available:  false,
			Confidence: 0,
		}
	}

	liquidityRisk := 0.05

	score := liquidityRisk * 100
	reason := "protocol TVL vs vault balance ratio"

	return RiskFactor{
		Name:       "liquidity",
		Score:      score,
		Weight:     s.weights.Liquidity,
		Reason:     reason,
		Available:  true,
		Confidence: 0.6,
	}
}

func (s *RiskService) computeDrawdownRisk(ctx context.Context, v *vault.Vault) RiskFactor {
	totalDeposited := v.TotalDeposited.InexactFloat64()
	currentBalance := v.CurrentBalance.InexactFloat64()

	if totalDeposited == 0 {
		return RiskFactor{
			Name:       "drawdown",
			Score:      0,
			Weight:     s.weights.Drawdown,
			Reason:     "no deposit history",
			Available:  false,
			Confidence: 0,
		}
	}

	drawdown := 0.0
	if currentBalance < totalDeposited {
		drawdown = (totalDeposited - currentBalance) / totalDeposited
	}

	score := drawdown * 100
	if score > 100 {
		score = 100
	}

	reason := "historical drawdown from peak"
	if drawdown > 0.1 {
		reason = "significant drawdown detected"
	}

	return RiskFactor{
		Name:       "drawdown",
		Score:      score,
		Weight:     s.weights.Drawdown,
		Reason:     reason,
		Available:  true,
		Confidence: 0.8,
	}
}

func (s *RiskService) computeProtocolAgeRisk(ctx context.Context, v *vault.Vault) RiskFactor {
	protocolAges := map[string]int{
		"aave":     36,
		"blend":    12,
		"compound": 48,
	}

	var totalAgeMonths int
	var protocolCount int

	for _, alloc := range v.Allocations {
		age := protocolAges[alloc.Protocol]
		if age > 0 {
			totalAgeMonths += age
			protocolCount++
		}
	}

	if protocolCount == 0 {
		return RiskFactor{
			Name:       "protocol_age",
			Score:      60,
			Weight:     s.weights.Age,
			Reason:     "protocol age data unavailable",
			Available:  false,
			Confidence: 0,
		}
	}

	avgAgeMonths := float64(totalAgeMonths) / float64(protocolCount)

	score := s.ageToRiskScore(avgAgeMonths)
	reason := "weighted average protocol age"

	return RiskFactor{
		Name:       "protocol_age",
		Score:      score,
		Weight:     s.weights.Age,
		Reason:     reason,
		Available:  true,
		Confidence: 0.95,
	}
}

func (s *RiskService) ageToRiskScore(ageMonths float64) float64 {
	if ageMonths >= 36 {
		return 10
	} else if ageMonths >= 24 {
		return 25
	} else if ageMonths >= 12 {
		return 50
	} else if ageMonths >= 6 {
		return 75
	}
	return 90
}

func (s *RiskService) computeWeightedScore(factors []RiskFactor) (float64, float64) {
	var totalWeight float64
	var weightedSum float64
	var totalConfidence float64
	var availableCount int

	for _, factor := range factors {
		if factor.Available {
			weightedSum += factor.Score * factor.Weight
			totalWeight += factor.Weight
			totalConfidence += factor.Confidence
			availableCount++
		}
	}

	if totalWeight == 0 {
		return 50, 0
	}

	overall := weightedSum / totalWeight

	confidence := 0.0
	if availableCount > 0 {
		confidence = (totalConfidence / float64(availableCount)) * (float64(availableCount) / float64(len(factors)))
	}

	return overall, confidence
}

// computeTier maps a 0-100 risk score onto a tier. The bounds are contiguous
// rather than integer-spaced: a fractional score such as 33.5 previously
// matched no case and fell through to the default, which reported "high" and
// made the mapping non-monotonic across the 33-34 and 66-67 gaps.
func (s *RiskService) computeTier(overall float64) string {
	switch {
	case overall < 34:
		return "low"
	case overall < 67:
		return "medium"
	default:
		return "high"
	}
}
