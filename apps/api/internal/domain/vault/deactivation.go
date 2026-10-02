package vault

import (
	"strings"

	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/pkg/apperror"
)

const (
	AllocationStatusActive      = "active"
	AllocationStatusDeactivated = "deactivated"
)

// Retrofitted onto apperror.AppError (nester#1341); see the larger doc
// comment on model.go's sentinel block for why each stays the exact same
// error value (errors.Is-compatible with every existing call site) and how
// Kind was chosen. ErrYieldSourceNotFound is the only NotFound here - every
// other sentinel describes a malformed migration request.
var (
	ErrYieldSourceNotFound         error = apperror.NewNotFound("VAULT_YIELD_SOURCE_NOT_FOUND", "yield source not found")
	ErrYieldSourceDeactivated      error = apperror.NewValidation("VAULT_YIELD_SOURCE_DEACTIVATED", "yield source is deactivated")
	ErrMigrationTargetRequired     error = apperror.NewValidation("VAULT_MIGRATION_TARGET_REQUIRED", "migration target is required")
	ErrMigrationTargetInactive     error = apperror.NewValidation("VAULT_MIGRATION_TARGET_INACTIVE", "migration target is not active")
	ErrMigrationSourceActive       error = apperror.NewValidation("VAULT_MIGRATION_SOURCE_ACTIVE", "migration source is still active")
	ErrMigrationSourceEqualsTarget error = apperror.NewValidation("VAULT_MIGRATION_SOURCE_EQUALS_TARGET", "migration source and target must differ")
	ErrMigrationAmountInvalid      error = apperror.NewValidation("VAULT_MIGRATION_AMOUNT_INVALID", "migration amount must be greater than zero")
	ErrMigrationAmountExceeded     error = apperror.NewValidation("VAULT_MIGRATION_AMOUNT_EXCEEDED", "migration amount exceeds source position")
)

// MigrationTarget describes an active allocation that can receive a position
// migrated away from a deactivated yield source.
type MigrationTarget struct {
	Protocol string          `json:"protocol"`
	Amount   decimal.Decimal `json:"amount"`
	APY      decimal.Decimal `json:"apy"`
}

// MigrationGuide is the information needed by a caller to present a guided
// migration flow for a deactivated source.
type MigrationGuide struct {
	SourceProtocol string            `json:"source_protocol"`
	SourceAmount   decimal.Decimal   `json:"source_amount"`
	Targets        []MigrationTarget `json:"targets"`
}

// DeactivateYieldSource marks a vault allocation as unavailable for new
// deposits. Existing value remains in the allocation until it is migrated or
// withdrawn. The operation is idempotent.
func (v *Vault) DeactivateYieldSource(protocol string) error {
	protocol = normalizeProtocol(protocol)
	if protocol == "" {
		return ErrYieldSourceNotFound
	}

	for i := range v.Allocations {
		if normalizeProtocol(v.Allocations[i].Protocol) == protocol {
			v.Allocations[i].Status = AllocationStatusDeactivated
			return nil
		}
	}

	return ErrYieldSourceNotFound
}

// CanAcceptDepositTo checks both vault capacity and whether the selected
// yield source still accepts new deposits. An empty allocation status is
// treated as active for backwards compatibility with existing allocations.
func (v *Vault) CanAcceptDepositTo(protocol string, amount decimal.Decimal) error {
	protocol = normalizeProtocol(protocol)
	if protocol == "" {
		return ErrYieldSourceNotFound
	}

	var allocation *Allocation
	for i := range v.Allocations {
		if normalizeProtocol(v.Allocations[i].Protocol) == protocol {
			allocation = &v.Allocations[i]
			break
		}
	}
	if allocation == nil {
		return ErrYieldSourceNotFound
	}
	if allocation.Status == AllocationStatusDeactivated {
		return ErrYieldSourceDeactivated
	}

	return v.CanAcceptDeposit(amount)
}

// MigrationGuideFor returns active destinations for a deactivated source.
// The returned guide is intentionally read-only data; callers must still
// validate and apply a migration through MigrateYieldSourcePosition.
func (v *Vault) MigrationGuideFor(sourceProtocol string) (MigrationGuide, error) {
	sourceProtocol = normalizeProtocol(sourceProtocol)
	if sourceProtocol == "" {
		return MigrationGuide{}, ErrYieldSourceNotFound
	}

	var source *Allocation
	for i := range v.Allocations {
		if normalizeProtocol(v.Allocations[i].Protocol) == sourceProtocol {
			source = &v.Allocations[i]
			break
		}
	}
	if source == nil {
		return MigrationGuide{}, ErrYieldSourceNotFound
	}
	if source.Status != AllocationStatusDeactivated {
		return MigrationGuide{}, ErrMigrationSourceActive
	}

	guide := MigrationGuide{
		SourceProtocol: source.Protocol,
		SourceAmount:   source.Amount,
		Targets:        make([]MigrationTarget, 0),
	}
	for _, allocation := range v.Allocations {
		if normalizeProtocol(allocation.Protocol) == sourceProtocol || allocation.Status == AllocationStatusDeactivated {
			continue
		}
		guide.Targets = append(guide.Targets, MigrationTarget{
			Protocol: allocation.Protocol,
			Amount:   allocation.Amount,
			APY:      allocation.APY,
		})
	}

	return guide, nil
}

// MigrateYieldSourcePosition moves amount from a deactivated allocation to an
// active allocation. Both allocations are updated together in memory, so a
// caller can persist the resulting allocation set as one replacement.
func (v *Vault) MigrateYieldSourcePosition(sourceProtocol, targetProtocol string, amount decimal.Decimal) error {
	sourceProtocol = normalizeProtocol(sourceProtocol)
	targetProtocol = normalizeProtocol(targetProtocol)
	if sourceProtocol == "" {
		return ErrYieldSourceNotFound
	}
	if targetProtocol == "" {
		return ErrMigrationTargetRequired
	}
	if sourceProtocol == targetProtocol {
		return ErrMigrationSourceEqualsTarget
	}
	if !amount.GreaterThan(decimal.Zero) {
		return ErrMigrationAmountInvalid
	}

	sourceIndex := -1
	targetIndex := -1
	for i := range v.Allocations {
		protocol := normalizeProtocol(v.Allocations[i].Protocol)
		switch protocol {
		case sourceProtocol:
			sourceIndex = i
		case targetProtocol:
			targetIndex = i
		}
	}
	if sourceIndex < 0 {
		return ErrYieldSourceNotFound
	}
	if targetIndex < 0 {
		return ErrMigrationTargetRequired
	}
	if v.Allocations[sourceIndex].Status != AllocationStatusDeactivated {
		return ErrMigrationSourceActive
	}
	if v.Allocations[targetIndex].Status == AllocationStatusDeactivated {
		return ErrMigrationTargetInactive
	}
	if amount.GreaterThan(v.Allocations[sourceIndex].Amount) {
		return ErrMigrationAmountExceeded
	}

	v.Allocations[sourceIndex].Amount = v.Allocations[sourceIndex].Amount.Sub(amount)
	v.Allocations[targetIndex].Amount = v.Allocations[targetIndex].Amount.Add(amount)
	return nil
}

func normalizeProtocol(protocol string) string {
	return strings.ToLower(strings.TrimSpace(protocol))
}
