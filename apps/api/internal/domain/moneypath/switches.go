// Package moneypath holds the pause switches for deposits and withdrawals:
// the global pair (nester#1120) and the per-vault pair (nester#1322).
//
// Kept dependency-free, like domain/audit, so the service layer and the
// postgres repository can both depend on it without repository -> service
// becoming an import cycle.
package moneypath

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
)

// Operation is a money-path operation that can be halted independently.
type Operation string

const (
	OperationDeposit    Operation = "deposit"
	OperationWithdrawal Operation = "withdrawal"
)

// Operations lists every switch the system maintains. Used to seed and to
// report state, so adding one here is the single edit that introduces it.
func Operations() []Operation {
	return []Operation{OperationDeposit, OperationWithdrawal}
}

// Valid reports whether op is an operation the system knows how to halt.
func (o Operation) Valid() bool {
	for _, known := range Operations() {
		if o == known {
			return true
		}
	}
	return false
}

// ErrPaused is returned when an operation is refused because its switch is
// engaged. Handlers map it to 503 with the operator's reason, so the UI can
// say what is happening rather than showing a generic failure.
var ErrPaused = errors.New("money path operation is paused")

// ErrUnknownOperation marks a switch name that is not one of Operations().
var ErrUnknownOperation = errors.New("unknown money path operation")

// ErrTransactionLimitExceeded is returned when a single deposit or withdrawal
// amount exceeds the configured per-transaction ceiling. This is independent of
// daily caps and bounds the blast radius of a single compromised session or
// client bug.
var ErrTransactionLimitExceeded = errors.New("amount exceeds per-transaction limit")

// TransactionLimit is the per-transaction ceiling for a single operation.
// A zero or nil Max means no ceiling is configured for that operation.
type TransactionLimit struct {
	Operation Operation
	// Max is the largest permitted single-transaction amount in base units.
	Max *big.Int
}

// TransactionLimitError carries the offending amount and the configured ceiling
// so a handler can explain the refusal without a second lookup.
type TransactionLimitError struct {
	Operation Operation
	Amount    *big.Int
	Max       *big.Int
}

func (e *TransactionLimitError) Error() string {
	return fmt.Sprintf("%s amount %s exceeds per-transaction limit %s",
		e.Operation, e.Amount.String(), e.Max.String())
}

// Unwrap lets errors.Is(err, ErrTransactionLimitExceeded) match.
func (e *TransactionLimitError) Unwrap() error {
	return ErrTransactionLimitExceeded
}

// CheckTransactionLimit enforces the per-transaction ceiling for op against
// amount. It returns nil when no ceiling is configured (limit nil or Max nil/
// non-positive), when the amount is within the ceiling, or when the amount is
// non-positive (amount validation is the caller's responsibility). It returns a
// *TransactionLimitError when amount exceeds the ceiling.
func CheckTransactionLimit(limit *TransactionLimit, op Operation, amount *big.Int) error {
	if limit == nil || limit.Max == nil || limit.Max.Sign() <= 0 {
		return nil
	}
	if amount == nil || amount.Sign() <= 0 {
		return nil
	}
	if amount.Cmp(limit.Max) > 0 {
		return &TransactionLimitError{
			Operation: op,
			Amount:    new(big.Int).Set(amount),
			Max:       new(big.Int).Set(limit.Max),
		}
	}
	return nil
}

// Switch is the current state of one operation's pause flag.
type Switch struct {
	Operation Operation
	Paused    bool
	// Reason is the operator's explanation, surfaced to clients.
	Reason    string
	ChangedBy *uuid.UUID
	UpdatedAt time.Time
}

// PausedError carries the operator's reason alongside ErrPaused so a handler
// can tell the user why without a second lookup.
type PausedError struct {
	Operation Operation
	Reason    string
	// Cause is set when the refusal came from being unable to read the
	// switch rather than from an operator engaging it. Without it, a
	// database outage is indistinguishable from a deliberate pause: the
	// caller sees "paused" with a reason nobody set, and the real fault
	// never reaches the logs.
	Cause error
}

func (e *PausedError) Error() string {
	if e.Reason == "" {
		return string(e.Operation) + " is paused"
	}
	return string(e.Operation) + " is paused: " + e.Reason
}

// Unwrap lets errors.Is(err, ErrPaused) match a *PausedError, so call sites
// can test for the condition without caring about the reason. When the
// refusal came from an unreadable switch, the underlying cause is returned
// alongside it so errors.Is finds that too.
func (e *PausedError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrPaused, e.Cause}
	}
	return []error{ErrPaused}
}
