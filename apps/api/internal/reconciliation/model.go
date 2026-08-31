// Package reconciliation compares recorded application state against
// authoritative on-chain state and records every discrepancy for review.
package reconciliation

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Level string

const (
	LevelBalance     Level = "balance"
	LevelTransaction Level = "transaction"
	LevelInvariant   Level = "invariant"
)

type DiscrepancyType string

const (
	TypeMissing  DiscrepancyType = "missing"
	TypeExtra    DiscrepancyType = "extra"
	TypeMismatch DiscrepancyType = "mismatch"
	TypeStuck    DiscrepancyType = "stuck"
)

type Severity string

const (
	SeverityInformational Severity = "informational"
	SeverityWarning       Severity = "warning"
	SeverityCritical      Severity = "critical"
)

type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
)

type ResolutionState string

const (
	ResolutionOpen      ResolutionState = "open"
	ResolutionReviewing ResolutionState = "reviewing"
	ResolutionResolved  ResolutionState = "resolved"
)

type Scope struct {
	FullSweep bool
	VaultID   uuid.UUID
	EventID   string
	StartedAt time.Time
}

type Run struct {
	ID             uuid.UUID
	Level          Level
	Comparator     string
	Status         RunStatus
	Scope          Scope
	StartedAt      time.Time
	CompletedAt    *time.Time
	CheckedCount   int
	FindingCount   int
	CriticalCount  int
	CheckpointKey  string
	CheckpointFrom string
	CheckpointTo   string
	Error           string
}

type Finding struct {
	ID              uuid.UUID
	RunID           uuid.UUID
	Level           Level
	Type            DiscrepancyType
	Severity        Severity
	EntityType      string
	EntityID        string
	RecordedValue   *decimal.Decimal
	OnChainValue    *decimal.Decimal
	Difference      *decimal.Decimal
	Tolerance       decimal.Decimal
	ObservedAt      time.Time
	Details         map[string]string
	ResolutionState ResolutionState
}

type FindingInput struct {
	Level         Level
	Type          DiscrepancyType
	EntityType    string
	EntityID      string
	RecordedValue *decimal.Decimal
	OnChainValue  *decimal.Decimal
	Age           time.Duration
	Details       map[string]string
}

type Stats struct {
	Checked  int
	Findings int
	Critical int
}

func DecimalPtr(value decimal.Decimal) *decimal.Decimal {
	return &value
}
