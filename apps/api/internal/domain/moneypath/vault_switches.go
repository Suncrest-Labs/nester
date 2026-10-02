package moneypath

import (
	"time"

	"github.com/google/uuid"
)

// VaultSwitch is the per-vault pause state of one money-path operation
// (nester#1322).
//
// It is deliberately the same idea as the global Switch, scoped to a single
// vault: the global switches stop an operation across the whole protocol,
// which is the right lever for a protocol-wide incident but far too blunt
// when one vault is misbehaving and every other vault is healthy. A
// VaultSwitch lets operators stop deposits or withdrawals on exactly one
// vault while the rest keep serving.
//
// The shape mirrors Switch, with one important difference: there is no
// assumption that a row exists. The global switches are a fixed pair seeded
// by migration 106, so a missing row is a schema fault the caller must fail
// closed on. Vaults are created and closed at runtime, so there is nothing
// to seed and the absence of a row simply means "not paused".
type VaultSwitch struct {
	VaultID   uuid.UUID
	Operation Operation
	Paused    bool
	// Reason is the operator's explanation, surfaced to clients so the UI
	// can say what is happening instead of showing a generic error.
	Reason    string
	ChangedBy *uuid.UUID
	UpdatedAt time.Time
}

// VaultSwitchKey identifies one switch without carrying the rest of the row.
// Used as the service cache key so the two identifying fields cannot drift
// apart the way a formatted string key could.
type VaultSwitchKey struct {
	VaultID   uuid.UUID
	Operation Operation
}

// Key returns the identity of the switch, for use as a cache key.
func (s VaultSwitch) Key() VaultSwitchKey {
	return VaultSwitchKey{VaultID: s.VaultID, Operation: s.Operation}
}
