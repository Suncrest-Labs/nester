// Package backfill defines the domain model and persistence port for
// historical chain backfill/resync runs (nester#840).
package backfill

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrRunNotFound = errors.New("backfill run not found")

type Status string

const (
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

type Mode string

const (
	// ModeBackfill processes a range relying solely on the indexer's
	// existing per-event dedup — safe for any range, including one that
	// partially overlaps already-processed ledgers.
	ModeBackfill Mode = "backfill"
	// ModeRebuild additionally clears derived rows + their processed_events
	// entries within the range before reprocessing. Only offered for
	// append-only event types (see resettableEventTypes in the stellar
	// package) — never for deposit/withdraw, whose derived columns
	// (vaults.total_deposited/current_balance) are incremental deltas, not
	// idempotent absolute writes, so resetting and replaying them would
	// double-count everything applied before the reset.
	ModeRebuild Mode = "rebuild"
)

// Run is one operator-initiated backfill/rebuild over [FromLedger, ToLedger].
type Run struct {
	ID                     uuid.UUID  `json:"id"`
	FromLedger             uint64     `json:"from_ledger"`
	ToLedger               uint64     `json:"to_ledger"`
	ContractIDs            []string   `json:"contract_ids"`
	Mode                   Mode       `json:"mode"`
	DryRun                 bool       `json:"dry_run"`
	Status                 Status     `json:"status"`
	LastLedgerDone         *uint64    `json:"last_ledger_done,omitempty"`
	EventsProcessed        int64      `json:"events_processed"`
	EventsSkippedDuplicate int64      `json:"events_skipped_duplicate"`
	LastError              string     `json:"last_error,omitempty"`
	InitiatedBy            string     `json:"initiated_by"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
	CompletedAt            *time.Time `json:"completed_at,omitempty"`
}

// ResumeFrom is where a resumed run should start fetching events: the
// ledger after the last one fully committed, or FromLedger for a fresh run.
func (r Run) ResumeFrom() uint64 {
	if r.LastLedgerDone == nil {
		return r.FromLedger
	}
	return *r.LastLedgerDone + 1
}

// Repository is the persistence port for backfill_runs.
type Repository interface {
	Create(ctx context.Context, run *Run) error
	GetByID(ctx context.Context, id uuid.UUID) (*Run, error)
	// UpdateProgress persists a checkpoint after each processed batch — the
	// resumability requirement: a crash between calls resumes from the last
	// successfully persisted checkpoint rather than restarting.
	UpdateProgress(ctx context.Context, id uuid.UUID, lastLedgerDone uint64, eventsProcessed, eventsSkippedDuplicate int64) error
	Complete(ctx context.Context, id uuid.UUID) error
	Fail(ctx context.Context, id uuid.UUID, errMsg string) error
	List(ctx context.Context, limit int) ([]Run, error)
}
