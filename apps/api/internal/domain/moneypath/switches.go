// Package moneypath holds the global pause switch for deposits and
// withdrawals (nester#1120).
//
// Kept dependency-free, like domain/audit, so the service layer and the
// postgres repository can both depend on it without repository -> service
// becoming an import cycle.
package moneypath

import (
	"errors"
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
