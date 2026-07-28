package harvest

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ErrForbidden is returned when a caller requests status for a vault they do not
// own.
var ErrForbidden = errors.New("vault does not belong to user")

// Status is the user-facing view of a vault's harvest state.
type Status struct {
	VaultID uuid.UUID `json:"vault_id"`
	// PendingYield is the currently accrued, not-yet-harvested yield.
	PendingYield decimal.Decimal `json:"pending_yield"`
	// Threshold is the minimum yield (gas + margin) required to harvest.
	Threshold decimal.Decimal `json:"harvest_threshold"`
	// Harvestable is true when PendingYield already clears the threshold.
	Harvestable bool `json:"harvestable"`
	// EstimatedNextHarvest is when the threshold is projected to be met. Nil when
	// unknown (no accrual-rate signal) or already harvestable.
	EstimatedNextHarvest *time.Time `json:"estimated_next_harvest,omitempty"`
}

// VaultStatusForUser is VaultStatus with an ownership guard: it returns
// ErrForbidden unless the vault belongs to userID. Retrieval is user-scoped so a
// caller can never read another user's harvest state.
func (e *Engine) VaultStatusForUser(ctx context.Context, vaultID, userID uuid.UUID) (Status, error) {
	v, err := e.vaults.GetVaultYield(ctx, vaultID)
	if err != nil {
		return Status{}, err
	}
	if v.UserID != userID {
		return Status{}, ErrForbidden
	}
	return e.statusFor(ctx, v)
}

// VaultStatus computes the harvest status for a single vault: pending yield, the
// economic threshold, whether it is harvestable now, and — when an accrual rate
// is known — the projected next-harvest time.
func (e *Engine) VaultStatus(ctx context.Context, vaultID uuid.UUID) (Status, error) {
	v, err := e.vaults.GetVaultYield(ctx, vaultID)
	if err != nil {
		return Status{}, err
	}
	return e.statusFor(ctx, v)
}

func (e *Engine) statusFor(ctx context.Context, v VaultYield) (Status, error) {
	fee, err := e.gas.HarvestFee(ctx, v.Currency)
	if err != nil {
		return Status{}, err
	}
	decision := Evaluate(GatingInput{
		AccruedYield: v.AccruedYield,
		GasFee:       fee,
		Margin:       e.cfg.Margin,
	})

	due := DueForHarvest(e.clock(), v.HarvestFrequency, v.LastHarvestedAt)
	if !due {
		decision.Harvest = false
		decision.Reason = ReasonNotDue
	}

	status := Status{
		VaultID:      v.VaultID,
		PendingYield: v.AccruedYield,
		Threshold:    decision.Threshold,
		Harvestable:  decision.Harvest,
	}
	if !decision.Harvest {
		if d, ok := EstimateNextHarvest(v.AccruedYield, decision.Threshold, v.AccrualRatePerHour); ok {
			t := e.clock().Add(d).UTC()
			status.EstimatedNextHarvest = &t
		}
	}
	return status, nil
}
