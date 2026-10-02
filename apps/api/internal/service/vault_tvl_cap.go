package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/caps"
)

// MainnetTVLCapManager enforces a hard total-value-locked cap per vault,
// applied only on mainnet, until mainnet has an operating track record
// (nester#1376). It is intentionally a single configured ceiling shared by
// every vault rather than a per-vault column: the goal is a blanket safety
// limit on how much real money the protocol can hold on mainnet while it is
// unproven, not a per-vault business limit (that is soft_capacity).
//
// A zero-value MainnetTVLCapManager (mainnet=false or cap<=0) enforces
// nothing, so constructing one is safe on testnet and in tests.
type MainnetTVLCapManager struct {
	// mainnet gates the whole check. The cap must never apply on testnet or
	// any other non-production network.
	mainnet bool
	// cap is the hard ceiling on a single vault's total value locked.
	// Non-positive means no cap is enforced even on mainnet.
	cap decimal.Decimal
}

// NewMainnetTVLCapManager builds a cap manager from configuration.
func NewMainnetTVLCapManager(mainnet bool, cap decimal.Decimal) *MainnetTVLCapManager {
	return &MainnetTVLCapManager{mainnet: mainnet, cap: cap}
}

// CheckDepositCap reports whether crediting depositAmount to a vault
// currently holding currentTVL would exceed the configured mainnet cap.
func (m *MainnetTVLCapManager) CheckDepositCap(_ context.Context, _ uuid.UUID, currentTVL decimal.Decimal, depositAmount decimal.Decimal) error {
	if m == nil || !m.mainnet {
		return nil
	}
	return caps.CheckTVLCap(currentTVL, &m.cap, depositAmount)
}
