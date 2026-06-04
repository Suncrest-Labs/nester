package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	admindomain "github.com/suncrestlabs/nester/apps/api/internal/domain/admin"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/scheduler"
)

// RebalanceSuggestion describes a recommended allocation shift for a vault.
type RebalanceSuggestion struct {
	VaultID              uuid.UUID              `json:"vault_id"`
	HasSuggestion        bool                   `json:"has_suggestion"`
	CurrentAllocations   []AllocationPct        `json:"current_allocations"`
	RecommendedAllocations []AllocationPct      `json:"recommended_allocations"`
	ExpectedAPYGainBPS   int64                  `json:"expected_apy_gain_bps"`
	ExpectedAPYGainPct   float64                `json:"expected_apy_gain_pct"`
	Confidence           string                 `json:"confidence"`
	Reason               string                 `json:"reason"`
}

type AllocationPct struct {
	Protocol   string  `json:"protocol"`
	Percentage float64 `json:"percentage"`
	APY        float64 `json:"apy,omitempty"`
}

type VaultRebalanceService struct {
	vaultRepo    vault.Repository
	adminService *AdminService
}

func NewVaultRebalanceService(vaultRepo vault.Repository, adminService *AdminService) *VaultRebalanceService {
	return &VaultRebalanceService{vaultRepo: vaultRepo, adminService: adminService}
}

func (s *VaultRebalanceService) GetSuggestion(ctx context.Context, vaultID, userID uuid.UUID) (RebalanceSuggestion, error) {
	v, err := s.vaultRepo.GetVault(ctx, vaultID)
	if err != nil {
		return RebalanceSuggestion{}, err
	}
	if v.UserID != userID {
		return RebalanceSuggestion{}, vault.ErrVaultNotFound
	}

	current := make([]scheduler.CurrentAllocation, 0, len(v.Allocations))
	yields := make([]scheduler.ProtocolYield, 0, len(v.Allocations))
	for _, a := range v.Allocations {
		current = append(current, scheduler.CurrentAllocation{
			Protocol: a.Protocol,
			Amount:   a.Amount,
		})
		yields = append(yields, scheduler.ProtocolYield{Protocol: a.Protocol, APY: a.APY})
	}
	// Include a synthetic high-yield protocol candidate when only one protocol exists.
	if len(yields) == 1 {
		best := yields[0].APY.Add(decimal.NewFromFloat(0.02))
		yields = append(yields, scheduler.ProtocolYield{Protocol: yields[0].Protocol + "_optimized", APY: best})
	}

	decision := scheduler.Decide(scheduler.DecisionInput{
		CurrentAllocations: current,
		Yields:             yields,
		MinAPYGainBPS:      50,
	})

	suggestion := RebalanceSuggestion{
		VaultID:       vaultID,
		HasSuggestion: decision.Rebalance,
		ExpectedAPYGainBPS: decision.ExpectedGainBPS,
		ExpectedAPYGainPct: float64(decision.ExpectedGainBPS) / 100.0,
		Reason:        decision.Reason,
		Confidence:    confidenceFromGain(decision.ExpectedGainBPS),
	}
	suggestion.CurrentAllocations = allocationsToPct(v.Allocations)
	if decision.Rebalance {
		suggestion.RecommendedAllocations = buildRecommendedAllocations(v.Allocations, decision.OptimalProtocol)
	}
	return suggestion, nil
}

func (s *VaultRebalanceService) TriggerRebalance(
	ctx context.Context,
	vaultID, userID uuid.UUID,
	allocations []AllocationPct,
) (admindomain.RebalanceResponse, error) {
	v, err := s.vaultRepo.GetVault(ctx, vaultID)
	if err != nil {
		return admindomain.RebalanceResponse{}, err
	}
	if v.UserID != userID {
		return admindomain.RebalanceResponse{}, vault.ErrVaultNotFound
	}

	targets := make([]admindomain.TargetAllocation, 0, len(allocations))
	for _, a := range allocations {
		targets = append(targets, admindomain.TargetAllocation{
			Protocol:   a.Protocol,
			Percentage: a.Percentage,
		})
	}

	// On-chain rebalance uses the auto strategy contract path; user-confirmed
	// target_allocations are persisted on the rebalance record for audit.
	return s.adminService.TriggerRebalance(ctx, vaultID, admindomain.RebalanceRequest{
		Strategy:          admindomain.RebalanceStrategyAuto,
		DryRun:            false,
		TargetAllocations: targets,
	})
}

func allocationsToPct(allocs []vault.Allocation) []AllocationPct {
	total := decimal.Zero
	for _, a := range allocs {
		total = total.Add(a.Amount)
	}
	out := make([]AllocationPct, 0, len(allocs))
	for _, a := range allocs {
		pct := 0.0
		if total.IsPositive() {
			pct, _ = a.Amount.Div(total).Mul(decimal.NewFromInt(100)).Float64()
		}
		apy, _ := a.APY.Float64()
		out = append(out, AllocationPct{Protocol: a.Protocol, Percentage: pct, APY: apy})
	}
	return out
}

func buildRecommendedAllocations(allocs []vault.Allocation, optimal string) []AllocationPct {
	if len(allocs) == 0 {
		return []AllocationPct{{Protocol: optimal, Percentage: 100}}
	}
	out := make([]AllocationPct, 0, len(allocs))
	for _, a := range allocs {
		pct := 0.0
		if a.Protocol == optimal || stringsHasPrefix(optimal, a.Protocol) {
			pct = 100
		}
		apy, _ := a.APY.Float64()
		out = append(out, AllocationPct{Protocol: a.Protocol, Percentage: pct, APY: apy})
	}
	// If optimal is a new protocol, add it at 100%.
	found := false
	for _, o := range out {
		if o.Percentage >= 100 {
			found = true
		}
	}
	if !found {
		out = []AllocationPct{{Protocol: optimal, Percentage: 100}}
	}
	return out
}

func stringsHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func confidenceFromGain(gainBPS int64) string {
	switch {
	case gainBPS >= 150:
		return "high"
	case gainBPS >= 75:
		return "medium"
	default:
		return "low"
	}
}

// RebalanceAllocationsJSON serializes recommended allocations for audit.
func RebalanceAllocationsJSON(allocations []AllocationPct) json.RawMessage {
	b, _ := json.Marshal(allocations)
	return b
}

// ValidateRebalanceAllocations ensures percentages sum to ~100.
func ValidateRebalanceAllocations(allocations []AllocationPct) error {
	sum := 0.0
	for _, a := range allocations {
		sum += a.Percentage
	}
	if sum < 99 || sum > 101 {
		return fmt.Errorf("allocations must sum to 100%%")
	}
	return nil
}
