package service

import (
	"github.com/shopspring/decimal"

	admindomain "github.com/suncrestlabs/nester/apps/api/internal/domain/admin"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// projectAutoRebalanceDeltas estimates per-protocol deltas for dry_run using
// current DB allocations and vault balance. Target weights are an equal split
// across active allocation rows — a preview when on-chain strategy weights are
// not mirrored in Postgres.
func projectAutoRebalanceDeltas(
	allocations []vault.Allocation,
	totalBalance decimal.Decimal,
) []admindomain.AllocationDeltaProjection {
	if len(allocations) == 0 || totalBalance.LessThanOrEqual(decimal.Zero) {
		return nil
	}

	n := decimal.NewFromInt(int64(len(allocations)))
	targetEach := totalBalance.Div(n)

	out := make([]admindomain.AllocationDeltaProjection, 0, len(allocations))
	for _, a := range allocations {
		delta := targetEach.Sub(a.Amount)
		if delta.IsZero() {
			continue
		}
		out = append(out, admindomain.AllocationDeltaProjection{
			SourceID: a.Protocol,
			Delta:    delta.StringFixed(vault.MaxAmountScale),
			Current:  a.Amount.StringFixed(vault.MaxAmountScale),
			Target:   targetEach.StringFixed(vault.MaxAmountScale),
		})
	}
	return out
}
