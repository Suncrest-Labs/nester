// Package flags implements a dynamic feature-flag and runtime-configuration
// service: named, typed flags stored in Postgres, evaluated per-user or
// globally, cached in-process with pub/sub invalidation so runtime changes
// propagate across instances within seconds.
//
// Separation of concerns is a hard boundary: this package holds operational
// flags and non-secret tunables ONLY. Secrets (keys, provider tokens,
// database credentials) stay in the environment and the secret store — a
// guard in the store rejects anything that looks secret-marked.
package flags

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"time"
)

// Type enumerates the supported flag kinds.
type Type string

const (
	// TypeBool is a plain on/off flag; kill switches are boolean flags whose
	// SafeValue is "off".
	TypeBool Type = "bool"
	// TypePercentage rolls a feature out to a deterministic, stable slice of
	// users. Membership is derived from a hash of flag name + user ID, so a
	// user who is "in" at 10% remains "in" at 20%.
	TypePercentage Type = "percentage"
	// TypeCohort enables a feature for an explicit allowlist of user IDs.
	TypeCohort Type = "cohort"
	// TypeValue holds an arbitrary non-secret typed value (string form).
	TypeValue Type = "value"
)

// percentBuckets is the resolution of percentage rollouts (0.01% steps).
const percentBuckets = 10000

// Flag is a single feature flag or runtime-config entry.
type Flag struct {
	Name        string
	Type        Type
	Enabled     bool     // TypeBool: on/off. Other types: master enable.
	Percentage  float64  // TypePercentage: 0-100.
	Cohort      []string // TypeCohort: allowed user IDs.
	Value       string   // TypeValue: the configured value.
	Description string
	Owner       string

	// FailSafeOn is the value a boolean flag evaluates to when the flag
	// service is unavailable. A kill-switched feature must set this false
	// (feature off = safe position); it must never fail-open.
	FailSafeOn bool

	CreatedAt time.Time
	UpdatedAt time.Time
}

// ErrNotFound is returned when a flag does not exist.
var ErrNotFound = errors.New("flags: flag not found")

// EvalContext carries the identity a flag is evaluated against.
type EvalContext struct {
	UserID string
}

// Reader is the read path used by feature call sites.
type Reader interface {
	// Get returns the flag by name.
	Get(ctx context.Context, name string) (Flag, error)
}

// Evaluator evaluates flags against a context with explicit fail-safe
// semantics: if the underlying reader errors, boolean evaluation returns the
// flag-declared safe default (via the registered fallback), never an
// arbitrary open state.
type Evaluator struct {
	reader Reader

	// failSafe maps flag name -> value to use when the flag service is
	// unavailable. Registered at call sites for kill switches so the safe
	// position is explicit even when the store cannot be reached at all.
	failSafe map[string]bool
}

// NewEvaluator builds an Evaluator on top of a Reader.
func NewEvaluator(reader Reader) *Evaluator {
	return &Evaluator{reader: reader, failSafe: make(map[string]bool)}
}

// RegisterFailSafe declares the value a flag evaluates to when the flag
// service is unreachable. Kill switches must register false (feature off).
func (e *Evaluator) RegisterFailSafe(name string, on bool) {
	e.failSafe[name] = on
}

// BoolValue evaluates a flag to on/off for the given context.
//
// Fail-safe behaviour: when the reader returns an error (store down, cache
// cold and DB unreachable), the result is the registered fail-safe for the
// flag, defaulting to false (off). A kill switch therefore never fails open.
func (e *Evaluator) BoolValue(ctx context.Context, name string, ec EvalContext) bool {
	f, err := e.reader.Get(ctx, name)
	if err != nil {
		return e.failSafe[name] // zero value false = fail closed
	}
	return evalBool(f, ec)
}

// StringValue returns the configured value of a TypeValue flag, or def when
// the flag is missing, disabled, or the service is unavailable.
func (e *Evaluator) StringValue(ctx context.Context, name, def string) string {
	f, err := e.reader.Get(ctx, name)
	if err != nil || !f.Enabled || f.Type != TypeValue {
		return def
	}
	return f.Value
}

func evalBool(f Flag, ec EvalContext) bool {
	if !f.Enabled {
		return false
	}
	switch f.Type {
	case TypeBool:
		return true
	case TypePercentage:
		return InPercentage(f.Name, ec.UserID, f.Percentage)
	case TypeCohort:
		for _, id := range f.Cohort {
			if id == ec.UserID {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// InPercentage reports whether userID falls inside the rollout percentage
// for flag name. Membership is deterministic and monotonic: the bucket for a
// (flag, user) pair is a stable hash, so raising the percentage only ever
// adds users — a user who saw the feature at 10% still sees it at 20%.
func InPercentage(name, userID string, pct float64) bool {
	if pct <= 0 {
		return false
	}
	if pct >= 100 {
		return true
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	_, _ = h.Write([]byte{':'})
	_, _ = h.Write([]byte(userID))
	bucket := h.Sum64() % percentBuckets
	return float64(bucket) < pct/100*percentBuckets
}

// AuditRecorder records a flag change: who changed which flag, when, and the
// before/after states. Wire the platform audit-log service here so every
// flag flip is traceable in an incident.
type AuditRecorder interface {
	RecordFlagChange(ctx context.Context, actor, name string, before, after *Flag) error
}

// AuditFunc adapts a function to AuditRecorder.
type AuditFunc func(ctx context.Context, actor, name string, before, after *Flag) error

// RecordFlagChange implements AuditRecorder.
func (f AuditFunc) RecordFlagChange(ctx context.Context, actor, name string, before, after *Flag) error {
	return f(ctx, actor, name, before, after)
}

// Validate checks a flag definition for structural problems.
func (f Flag) Validate() error {
	if f.Name == "" {
		return errors.New("flags: name is required")
	}
	switch f.Type {
	case TypeBool, TypeCohort, TypeValue:
	case TypePercentage:
		if f.Percentage < 0 || f.Percentage > 100 {
			return fmt.Errorf("flags: percentage %v out of range [0,100]", f.Percentage)
		}
	default:
		return fmt.Errorf("flags: unknown type %q", f.Type)
	}
	return nil
}
