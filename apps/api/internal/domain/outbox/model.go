// Package outbox implements the transactional outbox for webhook and
// notification side effects (#1049).
//
// # The problem
//
// A domain write and the side effect it causes are two different writes to
// two different systems. Sequencing them carefully does not make them
// atomic, and both orderings lose:
//
//   - Write the row, then dispatch: the process dies in between and the
//     side effect never happens. The user's balance changed and nobody told
//     them.
//   - Dispatch, then write the row: the write fails and a notification went
//     out for something that never happened.
//
// # The mechanism
//
// The side effect is inserted into the `outbox` table *inside the domain
// write's own transaction* (see Writer.Insert). Atomicity comes from the
// shared transaction and from nothing else: roll the transaction back and
// the intent to notify rolls back with it; commit it and the intent is
// durable even if the process dies one instruction later. A separate relay
// (see Relay) later hands each row to the durable job queue.
//
// # Delivery semantics
//
// Delivery is AT-LEAST-ONCE. A side effect can be delivered more than once
// — the relay may hand a row over twice after a crash, and the job queue
// itself is at-least-once by design. Every event therefore carries a
// DedupeKey that is stable across every redelivery of the same logical side
// effect, and it is propagated all the way to the consumer (webhook header
// and body, notification dedup key).
//
// Consumers MUST be idempotent. At-least-once delivery without that
// requirement stated is a trap: a consumer that treats a webhook as a
// trigger for a payout will eventually double-pay.
//
// # Ordering
//
// Ordering is guaranteed PER AGGREGATE and is explicitly NOT guaranteed
// globally. For a given (AggregateType, AggregateID) the relay dispatches
// events in insertion order and will not hand over event N+1 until event N
// has reached a terminal state — delivered or dead. Events for different
// aggregates are independent and race freely.
//
// Per-aggregate rather than global is what keeps the poison-message path
// from stalling the world: a permanently-failing side effect blocks only
// its own aggregate's queue, and only until it dead-letters (bounded by
// MaxAttempts and the job queue's own attempt cap), after which the
// aggregate resumes.
package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned when an outbox row does not exist.
var ErrNotFound = errors.New("outbox: event not found")

// Status is the lifecycle state of an outbox row.
type Status string

const (
	// StatusPending: written by the producer, not yet handed to the queue.
	StatusPending Status = "pending"
	// StatusDispatching: a queue job owns the delivery attempt. The row
	// stays here — blocking its aggregate — until that job is terminal.
	StatusDispatching Status = "dispatching"
	// StatusDispatched: the queue job succeeded. Terminal, prunable.
	StatusDispatched Status = "dispatched"
	// StatusDead: poison. Terminal; retained longer than dispatched rows so
	// the failure can actually be investigated.
	StatusDead Status = "dead"
)

// DefaultMaxAttempts bounds hand-off retries — enqueue failures and
// vanished job rows — not delivery attempts. Delivery retries belong to the
// job queue, which has its own cap; a second retry budget layered on top is
// exactly the "two competing retry systems" this package avoids.
const DefaultMaxAttempts = 5

// Event is one side effect awaiting delivery.
type Event struct {
	ID uuid.UUID `json:"id"`

	// AggregateType and AggregateID scope the ordering guarantee. Use the
	// narrowest aggregate that actually needs ordering: the wider it is,
	// the more work one poison message holds up before it dead-letters.
	AggregateType string `json:"aggregate_type"`
	AggregateID   string `json:"aggregate_id"`

	// EventType selects the job type via Routes.
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`

	// DedupeKey is stable across every redelivery of this logical side
	// effect and is what consumers discard repeats on. Unique table-wide.
	DedupeKey string `json:"dedupe_key"`

	Status        Status     `json:"status"`
	Attempts      int        `json:"attempts"`
	MaxAttempts   int        `json:"max_attempts"`
	NextAttemptAt time.Time  `json:"next_attempt_at"`
	LeasedUntil   *time.Time `json:"leased_until,omitempty"`
	JobID         *uuid.UUID `json:"job_id,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DispatchedAt  *time.Time `json:"dispatched_at,omitempty"`
}

// Validate reports whether the event carries the fields the relay needs.
// Producers get this error at insert time — inside their transaction, where
// it can still roll the domain write back — rather than as a permanent
// dead-letter discovered hours later by the relay.
func (e Event) Validate() error {
	switch {
	case e.AggregateType == "":
		return errors.New("outbox: aggregate_type is required")
	case e.AggregateID == "":
		return errors.New("outbox: aggregate_id is required")
	case e.EventType == "":
		return errors.New("outbox: event_type is required")
	case e.DedupeKey == "":
		return errors.New("outbox: dedupe_key is required")
	}
	if len(e.Payload) > 0 && !json.Valid(e.Payload) {
		return errors.New("outbox: payload is not valid JSON")
	}
	return nil
}

// dedupeNamespace seeds the deterministic (v5) UUIDs derived from dedupe
// keys. A fixed namespace means the same key always yields the same id, in
// this process and every future one — that is the whole point.
var dedupeNamespace = uuid.MustParse("6f1b1c2e-9a7d-5c3b-8e4f-0d2a7b6c9e11")

// DeriveID returns a deterministic UUID for the given dedupe key and scope.
// The relay and the side-effect handlers use it to turn a dedupe key into
// stable per-consumer identifiers (e.g. one delivery id per webhook
// subscription) so a re-run produces the same ids rather than fresh ones
// the consumer cannot recognise as a repeat.
func DeriveID(dedupeKey string, scope ...string) uuid.UUID {
	name := dedupeKey
	for _, s := range scope {
		name += "\x00" + s
	}
	return uuid.NewSHA1(dedupeNamespace, []byte(name))
}

// NewEvent builds a pending Event. payload is marshalled to JSON.
func NewEvent(aggregateType, aggregateID, eventType, dedupeKey string, payload any) (Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Event{}, fmt.Errorf("outbox: marshal payload: %w", err)
	}
	e := Event{
		ID:            uuid.New(),
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		EventType:     eventType,
		Payload:       raw,
		DedupeKey:     dedupeKey,
		Status:        StatusPending,
		MaxAttempts:   DefaultMaxAttempts,
	}
	if err := e.Validate(); err != nil {
		return Event{}, err
	}
	return e, nil
}

// Execer is the subset of *sql.Tx (and *sql.DB) that Insert needs. Taking
// the interface rather than *sql.Tx keeps producers free to pass whichever
// handle their transaction lives on, and keeps this package out of the
// business of opening transactions it does not own.
//
// Passing a *sql.DB here compiles and inserts the row, but it defeats the
// entire purpose: the row is then no longer atomic with the domain write.
// Always pass the transaction.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// ClaimParams controls one relay claim.
type ClaimParams struct {
	// Limit caps how many aggregate heads to claim in this call.
	Limit int
	// Lease is how long a claimed row stays invisible to other relays
	// before it is considered abandoned and reclaimable.
	Lease time.Duration
	// Now is the reference time (injectable for tests).
	Now time.Time
}

// Stats is an aggregate snapshot of the outbox for observability.
type Stats struct {
	// Pending is undispatched rows that are due.
	Pending int64 `json:"pending"`
	// Dispatching is rows whose delivery job is in flight.
	Dispatching int64 `json:"dispatching"`
	// Dead is the poison count — the number that matters for alerting.
	Dead int64 `json:"dead"`
	// OldestPendingAge is how long the oldest undispatched row has waited.
	// A relay that has stopped relaying shows up here before it shows up
	// anywhere else.
	OldestPendingAge time.Duration `json:"oldest_pending_age"`
}

// Repository is the persistence port for the outbox.
type Repository interface {
	// Insert writes a pending event using the caller's transaction handle.
	// It MUST be called with the same transaction as the domain write —
	// that is the only thing that makes the pair atomic.
	Insert(ctx context.Context, tx Execer, e Event) error

	// ClaimDue leases up to params.Limit events for dispatch and returns
	// them in StatusDispatching.
	//
	// It returns at most ONE event per (aggregate_type, aggregate_id): the
	// oldest row of that aggregate that is not yet terminal. That is how
	// per-aggregate ordering is enforced — event N+1 is simply not visible
	// to the relay while event N is still in flight.
	//
	// Claimable rows are pending rows that are due, plus rows abandoned
	// mid-hand-off (dispatching, no job_id, lease lapsed) — crash recovery
	// without a separate reaper.
	ClaimDue(ctx context.Context, params ClaimParams) ([]Event, error)

	// MarkDispatching records the queue job that now owns the event's
	// delivery. The event stays in StatusDispatching, blocking its
	// aggregate, until the relay reconciles that job to a terminal state.
	MarkDispatching(ctx context.Context, id, jobID uuid.UUID) error

	// MarkDispatched moves an event to the terminal success state.
	MarkDispatched(ctx context.Context, id uuid.UUID, at time.Time) error

	// MarkDead moves an event to the terminal poison state, unblocking its
	// aggregate so the events behind it can proceed.
	MarkDead(ctx context.Context, id uuid.UUID, lastErr string) error

	// Release returns an event to pending for another hand-off attempt at
	// nextAttemptAt, recording lastErr. Used only when the hand-off itself
	// failed — never to retry a delivery, which is the queue's job.
	Release(ctx context.Context, id uuid.UUID, nextAttemptAt time.Time, lastErr string) error

	// InFlight returns events in StatusDispatching that have a job_id, so
	// the relay can reconcile them against their jobs.
	InFlight(ctx context.Context, limit int) ([]Event, error)

	// Stats returns an aggregate snapshot for metrics.
	Stats(ctx context.Context, now time.Time) (Stats, error)

	// PruneTerminal deletes dispatched events last updated before
	// dispatchedBefore and dead events last updated before deadBefore,
	// returning how many of each were removed.
	PruneTerminal(ctx context.Context, dispatchedBefore, deadBefore time.Time) (dispatched, dead int64, err error)
}

// Writer is the producer-facing façade over Repository.Insert. Domain code
// depends on this two-method surface rather than the full Repository, which
// keeps "what a producer can do" (write an event) visibly separate from
// "what the relay can do" (everything else).
type Writer struct {
	repo Repository
}

// NewWriter constructs a Writer. A nil repo yields a Writer whose Insert is
// a no-op error, so a deployment with the outbox unwired fails loudly at
// the call site instead of silently dropping side effects.
func NewWriter(repo Repository) *Writer { return &Writer{repo: repo} }

// Insert writes e inside the caller's transaction.
func (w *Writer) Insert(ctx context.Context, tx Execer, e Event) error {
	if w == nil || w.repo == nil {
		return errors.New("outbox: no repository configured")
	}
	if err := e.Validate(); err != nil {
		return err
	}
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.MaxAttempts <= 0 {
		e.MaxAttempts = DefaultMaxAttempts
	}
	return w.repo.Insert(ctx, tx, e)
}

// Publish is NewEvent + Insert, the shape almost every producer wants.
func (w *Writer) Publish(ctx context.Context, tx Execer, aggregateType, aggregateID, eventType, dedupeKey string, payload any) error {
	e, err := NewEvent(aggregateType, aggregateID, eventType, dedupeKey, payload)
	if err != nil {
		return err
	}
	return w.Insert(ctx, tx, e)
}
