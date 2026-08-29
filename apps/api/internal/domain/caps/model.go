// Package caps enforces the launch-minimum per-user deposit cap and global
// TVL cap for the testnet launch (nester#1119).
//
// Kept dependency-free, like domain/moneypath, so the service layer and the
// postgres repository can both depend on it without a repository -> service
// import cycle.
package caps

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ErrPerUserCapExceeded is returned when a deposit would push a user's total
// vault balance above the configured per-user cap.
var ErrPerUserCapExceeded = errors.New("deposit would exceed the per-user deposit cap")

// ErrGlobalCapExceeded is returned when a deposit would push protocol-wide
// TVL above the configured global cap.
var ErrGlobalCapExceeded = errors.New("deposit would exceed the global TVL cap")

// CapExceededError carries the numbers behind a rejected deposit so the API
// response and logs can explain exactly why, without a second lookup.
type CapExceededError struct {
	// Kind is either ErrPerUserCapExceeded or ErrGlobalCapExceeded.
	Kind Kind
	// Cap is the configured limit that was hit.
	Cap decimal.Decimal
	// CurrentTotal is the balance already on record before this deposit.
	CurrentTotal decimal.Decimal
	// Attempted is the size of the deposit that was rejected.
	Attempted decimal.Decimal
}

// Kind distinguishes which cap was hit.
type Kind string

const (
	KindPerUser Kind = "per_user"
	KindGlobal  Kind = "global"
)

func (e *CapExceededError) Error() string {
	switch e.Kind {
	case KindPerUser:
		return ErrPerUserCapExceeded.Error()
	default:
		return ErrGlobalCapExceeded.Error()
	}
}

func (e *CapExceededError) Unwrap() error {
	if e.Kind == KindPerUser {
		return ErrPerUserCapExceeded
	}
	return ErrGlobalCapExceeded
}

// Totals is the current state used to evaluate a prospective deposit against
// the configured caps.
type Totals interface {
	// UserDepositTotal returns the sum of current_balance across every
	// non-deleted vault owned by userID.
	UserDepositTotal(ctx context.Context, userID uuid.UUID) (decimal.Decimal, error)
	// GlobalDepositTotal returns the sum of current_balance across every
	// non-deleted vault, i.e. protocol-wide TVL.
	GlobalDepositTotal(ctx context.Context) (decimal.Decimal, error)
}

// WarnFunc is invoked when a deposit is allowed but crosses an approach
// threshold (80%/90% of a cap) that wasn't crossed before the deposit. It is
// the alerting hook (nester#1119): production wires this to the structured
// logger (and, downstream, to log-based alerting) rather than blocking or
// slowing the request.
type WarnFunc func(ctx context.Context, w Warning)

// Warning describes one cap-approach event.
type Warning struct {
	Kind         Kind
	UserID       uuid.UUID // zero for KindGlobal
	Cap          decimal.Decimal
	NewTotal     decimal.Decimal
	ThresholdPct int // e.g. 80 or 90
}

// Config is the effective, already-validated cap configuration.
type Config struct {
	// PerUserCap is the maximum a single user may hold across all vaults.
	// Zero or negative disables the per-user cap.
	PerUserCap decimal.Decimal
	// GlobalCap is the maximum protocol-wide TVL. Zero or negative disables
	// the global cap.
	GlobalCap decimal.Decimal
	// WarnThresholdsPct are the percentages of a cap (e.g. []int{80, 90}) at
	// which an approach warning is emitted for a deposit that stays under the
	// cap. Sorted ascending; empty disables warnings.
	WarnThresholdsPct []int
}

// ParseCapValue parses a launch-cap env var value (LAUNCH_PER_USER_DEPOSIT_CAP
// / LAUNCH_GLOBAL_TVL_CAP). Blank or "0" explicitly disables the cap and
// returns decimal.Zero with no error. Any other value must parse as a
// non-negative decimal; a malformed or negative value is an error so
// misconfiguration fails startup instead of silently disabling the cap
// (nester CodeRabbit finding: discarded parse errors produced a zero-value
// decimal — indistinguishable from "disabled" — on malformed input).
func ParseCapValue(raw string) (decimal.Decimal, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "0" {
		return decimal.Zero, nil
	}
	value, err := decimal.NewFromString(trimmed)
	if err != nil {
		return decimal.Zero, fmt.Errorf("invalid cap value %q: %w", raw, err)
	}
	if value.IsNegative() {
		return decimal.Zero, fmt.Errorf("cap value %q must not be negative", raw)
	}
	return value, nil
}

func (c Config) perUserEnabled() bool { return c.PerUserCap.IsPositive() }
func (c Config) globalEnabled() bool  { return c.GlobalCap.IsPositive() }

// Checker evaluates deposits against the configured caps.
type Checker struct {
	cfg    Config
	totals Totals
	warn   WarnFunc
}

// NewChecker builds a Checker. warn may be nil to disable approach alerting.
func NewChecker(cfg Config, totals Totals, warn WarnFunc) *Checker {
	if warn == nil {
		warn = func(context.Context, Warning) {}
	}
	return &Checker{cfg: cfg, totals: totals, warn: warn}
}

// CheckDeposit evaluates a prospective deposit of amount by userID against
// both caps. It returns *CapExceededError when the deposit must be refused.
// When the deposit is allowed but crosses a warn threshold that the prior
// total had not yet crossed, the configured WarnFunc is invoked (best-effort;
// a warning is never itself a reason to reject).
func (c *Checker) CheckDeposit(ctx context.Context, userID uuid.UUID, amount decimal.Decimal) error {
	if c == nil || c.totals == nil {
		return nil
	}
	if !c.cfg.perUserEnabled() && !c.cfg.globalEnabled() {
		return nil
	}
	if amount.Sign() <= 0 {
		return nil
	}

	var warnings []Warning

	if c.cfg.perUserEnabled() {
		current, err := c.totals.UserDepositTotal(ctx, userID)
		if err != nil {
			return err
		}
		newTotal := current.Add(amount)
		if newTotal.GreaterThan(c.cfg.PerUserCap) {
			return &CapExceededError{
				Kind:         KindPerUser,
				Cap:          c.cfg.PerUserCap,
				CurrentTotal: current,
				Attempted:    amount,
			}
		}
		warnings = append(warnings, c.collectWarnings(KindPerUser, userID, c.cfg.PerUserCap, current, newTotal)...)
	}

	if c.cfg.globalEnabled() {
		current, err := c.totals.GlobalDepositTotal(ctx)
		if err != nil {
			return err
		}
		newTotal := current.Add(amount)
		if newTotal.GreaterThan(c.cfg.GlobalCap) {
			return &CapExceededError{
				Kind:         KindGlobal,
				Cap:          c.cfg.GlobalCap,
				CurrentTotal: current,
				Attempted:    amount,
			}
		}
		warnings = append(warnings, c.collectWarnings(KindGlobal, uuid.Nil, c.cfg.GlobalCap, current, newTotal)...)
	}

	c.emitWarnings(ctx, warnings)
	return nil
}

// EvaluateTotals is the transaction-safe counterpart to CheckDeposit
// (nester CodeRabbit TOCTOU finding): CheckDeposit reads totals and returns
// before the caller's deposit is actually committed, so two concurrent
// deposits can each pass the check and collectively exceed a cap. Callers
// that need atomicity (see postgres.VaultRepository.RecordDepositWithCapCheck)
// instead read currentUserTotal/currentGlobalTotal themselves under a lock
// that serializes concurrent deposits, inside the same DB transaction as the
// balance credit, and pass them here — a pure decision with no I/O of its
// own, safe to call while holding that lock.
func (c *Checker) EvaluateTotals(ctx context.Context, userID uuid.UUID, amount, currentUserTotal, currentGlobalTotal decimal.Decimal) error {
	if c == nil {
		return nil
	}
	if !c.cfg.perUserEnabled() && !c.cfg.globalEnabled() {
		return nil
	}
	if amount.Sign() <= 0 {
		return nil
	}

	var warnings []Warning

	if c.cfg.perUserEnabled() {
		newTotal := currentUserTotal.Add(amount)
		if newTotal.GreaterThan(c.cfg.PerUserCap) {
			return &CapExceededError{
				Kind:         KindPerUser,
				Cap:          c.cfg.PerUserCap,
				CurrentTotal: currentUserTotal,
				Attempted:    amount,
			}
		}
		warnings = append(warnings, c.collectWarnings(KindPerUser, userID, c.cfg.PerUserCap, currentUserTotal, newTotal)...)
	}

	if c.cfg.globalEnabled() {
		newTotal := currentGlobalTotal.Add(amount)
		if newTotal.GreaterThan(c.cfg.GlobalCap) {
			return &CapExceededError{
				Kind:         KindGlobal,
				Cap:          c.cfg.GlobalCap,
				CurrentTotal: currentGlobalTotal,
				Attempted:    amount,
			}
		}
		warnings = append(warnings, c.collectWarnings(KindGlobal, uuid.Nil, c.cfg.GlobalCap, currentGlobalTotal, newTotal)...)
	}

	c.emitWarnings(ctx, warnings)
	return nil
}

// Enabled reports whether either cap is configured, so a caller deciding
// whether to pay the cost of a transactional lock (see EvaluateTotals) can
// skip it entirely when caps are off.
func (c *Checker) Enabled() bool {
	if c == nil {
		return false
	}
	return c.cfg.perUserEnabled() || c.cfg.globalEnabled()
}

// collectWarnings returns one Warning per threshold newly crossed by this
// deposit, i.e. when priorTotal was under a configured percentage of cap and
// newTotal is at or above it. This deliberately alerts on the *first*
// deposit that crosses each line, not on every subsequent deposit while
// already over it.
//
// It only builds the Warning values — it does not invoke WarnFunc. Callers
// must collect warnings from every enabled cap check and emit them (via
// emitWarnings) only once all checks have passed, so a deposit that crosses
// the per-user warn threshold but is then rejected by the global cap never
// emits a misleading warning (nester CodeRabbit finding).
func (c *Checker) collectWarnings(kind Kind, userID uuid.UUID, cap_ decimal.Decimal, priorTotal, newTotal decimal.Decimal) []Warning {
	if cap_.Sign() <= 0 {
		return nil
	}
	var warnings []Warning
	for _, pct := range c.cfg.WarnThresholdsPct {
		threshold := cap_.Mul(decimal.NewFromInt(int64(pct))).Div(decimal.NewFromInt(100))
		if priorTotal.LessThan(threshold) && newTotal.GreaterThanOrEqual(threshold) {
			warnings = append(warnings, Warning{
				Kind:         kind,
				UserID:       userID,
				Cap:          cap_,
				NewTotal:     newTotal,
				ThresholdPct: pct,
			})
		}
	}
	return warnings
}

// emitWarnings invokes WarnFunc for every collected warning. Called only
// after all enabled cap checks for a deposit have succeeded.
func (c *Checker) emitWarnings(ctx context.Context, warnings []Warning) {
	for _, w := range warnings {
		c.warn(ctx, w)
	}
}
