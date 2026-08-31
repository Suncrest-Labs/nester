package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// Distinct reasons an operator-funded deposit was refused. All wrap
// vault.ErrOperatorFundedDepositRefused so callers and the HTTP layer can
// treat them uniformly while logs keep the specific cause.
var (
	ErrOperatorFundedDepositDisabled = fmt.Errorf(
		"%w (operator-funded deposits are disabled)", vault.ErrOperatorFundedDepositRefused)
	ErrOperatorFundedDepositNotAllowed = fmt.Errorf(
		"%w (vault is not allowlisted for operator-funded deposits)", vault.ErrOperatorFundedDepositRefused)
	ErrOperatorFundedDepositCapExceeded = fmt.Errorf(
		"%w (amount exceeds the operator-funded deposit cap)", vault.ErrOperatorFundedDepositRefused)
)

// OperatorFundedDepositPolicy decides whether the API may spend the shared
// operator account's funds to make a deposit on a user's behalf.
//
// The vault contract's deposit takes the operator as both caller and
// depositing user, so a server-submitted deposit moves platform money, not the
// user's. POST /vaults/{id}/deposit was therefore a value transfer bounded
// only by the operator balance (nester#1152).
//
// The real fix is the wallet-signed path that already exists: the client signs
// its own deposit and submits a tx_hash the chain verifier checks. This policy
// governs what remains — an operator-funded fallback that is off unless
// explicitly configured, capped per deposit, restricted to named vaults, and
// audit-logged on every use.
type OperatorFundedDepositPolicy struct {
	// enabled gates the whole path. Zero value is disabled, so a
	// OperatorFundedDepositPolicy{} refuses everything.
	enabled bool
	// allowedVaults is the set of vault IDs permitted to use operator funds.
	// Empty means none: an enabled policy with no allowlist still refuses,
	// so enabling the feature is never on its own enough to move money.
	allowedVaults map[uuid.UUID]struct{}
	// maxAmount caps a single operator-funded deposit. Non-positive means
	// no deposit is permitted, for the same reason.
	maxAmount decimal.Decimal

	logger *slog.Logger

	mu    sync.Mutex
	spent map[uuid.UUID]decimal.Decimal
}

// NewOperatorFundedDepositPolicy builds a policy from configuration.
//
// Every argument must be deliberately set for the path to work at all:
// disabled, an empty allowlist, or a non-positive cap each independently
// refuse. Defaults fail closed because the failure mode here is spending
// platform funds on a stranger's behalf.
func NewOperatorFundedDepositPolicy(
	enabled bool,
	allowedVaults []uuid.UUID,
	maxAmount decimal.Decimal,
	logger *slog.Logger,
) *OperatorFundedDepositPolicy {
	allowed := make(map[uuid.UUID]struct{}, len(allowedVaults))
	for _, id := range allowedVaults {
		if id != uuid.Nil {
			allowed[id] = struct{}{}
		}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &OperatorFundedDepositPolicy{
		enabled:       enabled,
		allowedVaults: allowed,
		maxAmount:     maxAmount,
		logger:        logger,
		spent:         make(map[uuid.UUID]decimal.Decimal),
	}
}

// ParseOperatorFundedVaultIDs turns a comma-separated allowlist into vault
// IDs. A malformed entry is an error rather than a silent skip: quietly
// dropping an unparseable id would widen or narrow the allowlist without
// anyone noticing.
func ParseOperatorFundedVaultIDs(raw string) ([]uuid.UUID, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	parts := strings.Split(trimmed, ",")
	ids := make([]uuid.UUID, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := uuid.Parse(part)
		if err != nil {
			return nil, fmt.Errorf("invalid vault id %q in operator-funded deposit allowlist: %w", part, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// Authorize reports whether an operator-funded deposit may proceed, and logs
// the decision either way.
//
// Both outcomes are logged at a level that survives production filtering: a
// permitted one because it is a movement of platform funds and the audit trail
// is the whole point, and a refused one because a rejected attempt is exactly
// what an operator wants to see.
func (p *OperatorFundedDepositPolicy) Authorize(
	_ context.Context,
	vaultID, userID uuid.UUID,
	amount decimal.Decimal,
) error {
	if p == nil || !p.enabled {
		return ErrOperatorFundedDepositDisabled
	}

	if _, ok := p.allowedVaults[vaultID]; !ok {
		p.logRefusal(vaultID, userID, amount, "vault is not in the operator-funded allowlist")
		return ErrOperatorFundedDepositNotAllowed
	}

	if p.maxAmount.Sign() <= 0 || amount.GreaterThan(p.maxAmount) {
		p.logRefusal(vaultID, userID, amount, "amount exceeds the operator-funded deposit cap")
		return ErrOperatorFundedDepositCapExceeded
	}

	p.mu.Lock()
	p.spent[vaultID] = p.spent[vaultID].Add(amount)
	total := p.spent[vaultID]
	p.mu.Unlock()

	// Logged after the decision so the entry records a deposit that was
	// actually authorized, and carries the running total so a slow drain
	// across many small deposits is visible in one field.
	p.logger.Warn("operator-funded deposit authorized: platform funds are being spent on a user's behalf",
		"vault_id", vaultID.String(),
		"user_id", userID.String(),
		"amount", amount.String(),
		"cap", p.maxAmount.String(),
		"vault_total_since_start", total.String(),
		"issue", "nester#1152",
	)
	return nil
}

func (p *OperatorFundedDepositPolicy) logRefusal(vaultID, userID uuid.UUID, amount decimal.Decimal, reason string) {
	p.logger.Warn("operator-funded deposit refused",
		"vault_id", vaultID.String(),
		"user_id", userID.String(),
		"amount", amount.String(),
		"reason", reason,
		"issue", "nester#1152",
	)
}

// allowOperatorFundedForTest builds a policy permitting operator-funded
// deposits for the given vaults with an effectively unbounded cap.
//
// It lives here rather than in a _test.go file because several test files in
// this package need it, and it is deliberately explicit: a test that wants a
// server-submitted deposit has to say so, which is what keeps the production
// default a refusal.
func allowOperatorFundedForTest(vaultIDs ...uuid.UUID) *OperatorFundedDepositPolicy {
	return NewOperatorFundedDepositPolicy(
		true,
		vaultIDs,
		decimal.NewFromInt(1_000_000_000),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}
