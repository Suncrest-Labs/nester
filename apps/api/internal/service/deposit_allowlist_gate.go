package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/flags"
)

// flagName is the feature-flag name used to control the mainnet deposit
// allowlist. The flag must be TypeCohort (an explicit allowlist of user IDs)
// or TypePercentage (a percentage-based rollout). When the flag is disabled or
// missing the gate evaluates as "not allowed" so that the safe position is
// closed — no one gets in if the flag store is unreachable.
const depositAllowlistFlagName = "mainnet_deposit_allowlist"

// FlagDepositAllowlistGate implements DepositAllowlistGate backed by a
// feature-flag evaluator (nester#1389). Create one via NewFlagDepositAllowlistGate
// and wire it into VaultService with SetDepositAllowlist.
//
// The flag must be of TypeCohort (explicit user-ID allowlist) or
// TypePercentage (percentage-based rollout) and must be enabled. Disabling
// the flag pauses all deposits through this gate. Remove the gate from the
// service (pass nil to SetDepositAllowlist) to open deposits to all users.
type FlagDepositAllowlistGate struct {
	evaluator *flags.Evaluator
}

// NewFlagDepositAllowlistGate creates a FlagDepositAllowlistGate.
//
// The evaluator is expected to have the fail-safe for depositAllowlistFlagName
// registered as false (deny) — this ensures the gate closes, not opens, if
// the flag store is unreachable. The constructor registers it automatically.
func NewFlagDepositAllowlistGate(evaluator *flags.Evaluator) *FlagDepositAllowlistGate {
	evaluator.RegisterFailSafe(depositAllowlistFlagName, false)
	return &FlagDepositAllowlistGate{evaluator: evaluator}
}

// EnsureDepositAllowed returns nil when userID is in the current cohort, or
// vault.ErrDepositNotAllowlisted when they are not. It uses BoolValue so
// TypeCohort (explicit allowlist) and TypePercentage (percentage rollout) both
// work — the flag evaluator resolves both to a per-user boolean.
func (g *FlagDepositAllowlistGate) EnsureDepositAllowed(ctx context.Context, userID uuid.UUID) error {
	ec := flags.EvalContext{UserID: userID.String()}
	if g.evaluator.BoolValue(ctx, depositAllowlistFlagName, ec) {
		return nil
	}
	return vault.ErrDepositNotAllowlisted
}
