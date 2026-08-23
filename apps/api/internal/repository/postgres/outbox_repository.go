package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/outbox"
)

// OutboxRepository persists the transactional outbox (#1049) in Postgres.
type OutboxRepository struct {
	db *sql.DB
}

// NewOutboxRepository constructs an OutboxRepository.
func NewOutboxRepository(db *sql.DB) *OutboxRepository {
	return &OutboxRepository{db: db}
}

const outboxColumns = `id, aggregate_type, aggregate_id, event_type, payload,
	dedupe_key, status, attempts, max_attempts, next_attempt_at, leased_until,
	job_id, last_error, created_at, updated_at, dispatched_at`

// outboxColumnsO is outboxColumns qualified with the UPDATE target alias `o`,
// for RETURNING clauses where the bare names would be ambiguous.
const outboxColumnsO = `o.id, o.aggregate_type, o.aggregate_id, o.event_type,
	o.payload, o.dedupe_key, o.status, o.attempts, o.max_attempts,
	o.next_attempt_at, o.leased_until, o.job_id, o.last_error, o.created_at,
	o.updated_at, o.dispatched_at`

// Insert writes a pending event on the caller's transaction handle. The
// ON CONFLICT DO NOTHING makes a producer that retries its own transaction
// idempotent: the dedupe key already present means this exact side effect is
// already scheduled, which is a success, not a collision to report.
func (r *OutboxRepository) Insert(ctx context.Context, tx outbox.Execer, e outbox.Event) error {
	if tx == nil {
		return errors.New("outbox: Insert requires a transaction handle")
	}
	payload := e.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	maxAttempts := e.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = outbox.DefaultMaxAttempts
	}
	nextAttempt := e.NextAttemptAt
	if nextAttempt.IsZero() {
		nextAttempt = time.Now()
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO outbox
		    (id, aggregate_type, aggregate_id, event_type, payload, dedupe_key,
		     status, attempts, max_attempts, next_attempt_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending', 0, $7, $8)
		ON CONFLICT (dedupe_key) DO NOTHING`,
		e.ID, e.AggregateType, e.AggregateID, e.EventType, []byte(payload),
		e.DedupeKey, maxAttempts, nextAttempt,
	)
	return err
}

// ClaimDue leases one event per aggregate — the oldest that has not reached a
// terminal state — using FOR UPDATE SKIP LOCKED so competing relays never
// block one another.
//
// The per-aggregate ordering guarantee lives in the `heads` CTE: DISTINCT ON
// over (aggregate_type, aggregate_id) ordered by (created_at, id) picks each
// aggregate's oldest non-terminal row and nothing else, so event N+1 is
// invisible to the relay until event N is dispatched or dead. The due-ness
// filter is applied AFTER that selection, deliberately — filtering it inside
// the DISTINCT ON would let a backing-off head be skipped over in favour of
// the event behind it, silently breaking the ordering it exists to preserve.
func (r *OutboxRepository) ClaimDue(ctx context.Context, params outbox.ClaimParams) ([]outbox.Event, error) {
	if params.Limit <= 0 {
		return nil, nil
	}
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}
	leasedUntil := now.Add(params.Lease)

	rows, err := r.db.QueryContext(ctx, `
		WITH heads AS (
		    SELECT DISTINCT ON (aggregate_type, aggregate_id)
		           id, status, job_id, next_attempt_at, leased_until, attempts, max_attempts
		    FROM outbox
		    WHERE status IN ('pending', 'dispatching')
		    ORDER BY aggregate_type, aggregate_id, created_at, id
		),
		ready AS (
		    SELECT o.id
		    FROM outbox o
		    JOIN heads h ON h.id = o.id
		    WHERE h.attempts < h.max_attempts
		      AND (
		          (h.status = 'pending' AND h.next_attempt_at <= $1)
		          -- Abandoned mid-hand-off: claimed, but the relay died
		          -- before it recorded a job. No job owns the delivery, so
		          -- reclaiming cannot double-dispatch.
		          OR (h.status = 'dispatching' AND h.job_id IS NULL
		              AND h.leased_until IS NOT NULL AND h.leased_until <= $1)
		      )
		    ORDER BY o.created_at, o.id
		    FOR UPDATE OF o SKIP LOCKED
		    LIMIT $3
		)
		UPDATE outbox o
		SET status = 'dispatching',
		    job_id = NULL,
		    leased_until = $2,
		    attempts = o.attempts + 1,
		    updated_at = NOW()
		FROM ready
		WHERE o.id = ready.id
		RETURNING `+outboxColumnsO,
		now, leasedUntil, params.Limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []outbox.Event
	for rows.Next() {
		e, err := scanOutboxRows(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// MarkDispatching records the job that owns this event's delivery.
func (r *OutboxRepository) MarkDispatching(ctx context.Context, id, jobID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE outbox
		SET job_id = $2, updated_at = NOW()
		WHERE id = $1 AND status = 'dispatching'`,
		id, jobID,
	)
	return err
}

// MarkDispatched moves an event to the terminal success state.
func (r *OutboxRepository) MarkDispatched(ctx context.Context, id uuid.UUID, at time.Time) error {
	if at.IsZero() {
		at = time.Now()
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE outbox
		SET status = 'dispatched', dispatched_at = $2, leased_until = NULL,
		    last_error = NULL, updated_at = NOW()
		WHERE id = $1 AND status <> 'dispatched'`,
		id, at,
	)
	return err
}

// MarkDead moves an event to the terminal poison state. The aggregate's
// remaining events become claimable on the next tick — a poison message must
// not hold its own aggregate hostage forever, let alone anyone else's.
func (r *OutboxRepository) MarkDead(ctx context.Context, id uuid.UUID, lastErr string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE outbox
		SET status = 'dead', last_error = $2, leased_until = NULL, updated_at = NOW()
		WHERE id = $1 AND status <> 'dead'`,
		id, nullText(lastErr),
	)
	return err
}

// Release returns an event to pending for another hand-off attempt.
func (r *OutboxRepository) Release(ctx context.Context, id uuid.UUID, nextAttemptAt time.Time, lastErr string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE outbox
		SET status = 'pending', next_attempt_at = $2, last_error = $3,
		    leased_until = NULL, job_id = NULL, updated_at = NOW()
		WHERE id = $1 AND status = 'dispatching'`,
		id, nextAttemptAt, nullText(lastErr),
	)
	return err
}

// InFlight returns dispatching events that have a job to reconcile against,
// oldest first so a backlog is worked off in the order it accumulated.
func (r *OutboxRepository) InFlight(ctx context.Context, limit int) ([]outbox.Event, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+outboxColumns+`
		FROM outbox
		WHERE status = 'dispatching' AND job_id IS NOT NULL
		ORDER BY created_at, id
		LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []outbox.Event
	for rows.Next() {
		e, err := scanOutboxRows(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// Stats returns aggregate counters plus the age of the oldest undispatched
// event — the signal that surfaces a wedged relay before anything else does.
func (r *OutboxRepository) Stats(ctx context.Context, now time.Time) (outbox.Stats, error) {
	if now.IsZero() {
		now = time.Now()
	}
	var (
		s            outbox.Stats
		oldestPendng sql.NullTime
	)
	row := r.db.QueryRowContext(ctx, `
		SELECT
		    COUNT(*) FILTER (WHERE status = 'pending' AND next_attempt_at <= $1) AS pending,
		    COUNT(*) FILTER (WHERE status = 'dispatching')                       AS dispatching,
		    COUNT(*) FILTER (WHERE status = 'dead')                              AS dead,
		    MIN(created_at) FILTER (WHERE status IN ('pending', 'dispatching'))  AS oldest
		FROM outbox`,
		now,
	)
	if err := row.Scan(&s.Pending, &s.Dispatching, &s.Dead, &oldestPendng); err != nil {
		return outbox.Stats{}, err
	}
	if oldestPendng.Valid {
		if age := now.Sub(oldestPendng.Time); age > 0 {
			s.OldestPendingAge = age
		}
	}
	return s, nil
}

// PruneTerminal deletes terminal rows past their retention window. Dispatched
// and dead rows get separate cutoffs: a delivered event is pure history, while
// a dead one is the evidence somebody will need to work out what broke.
func (r *OutboxRepository) PruneTerminal(ctx context.Context, dispatchedBefore, deadBefore time.Time) (int64, int64, error) {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM outbox
		WHERE status = 'dispatched' AND updated_at < $1`,
		dispatchedBefore,
	)
	if err != nil {
		return 0, 0, err
	}
	dispatched, err := res.RowsAffected()
	if err != nil {
		return 0, 0, err
	}

	res, err = r.db.ExecContext(ctx, `
		DELETE FROM outbox
		WHERE status = 'dead' AND updated_at < $1`,
		deadBefore,
	)
	if err != nil {
		return dispatched, 0, err
	}
	dead, err := res.RowsAffected()
	if err != nil {
		return dispatched, 0, err
	}
	return dispatched, dead, nil
}

func scanOutboxRows(rows *sql.Rows) (outbox.Event, error) { return scanOutboxFrom(rows) }

func scanOutboxFrom(s rowScanner) (outbox.Event, error) {
	var (
		e            outbox.Event
		payload      []byte
		status       string
		leasedUntil  sql.NullTime
		jobID        uuid.NullUUID
		lastErr      sql.NullString
		dispatchedAt sql.NullTime
	)
	if err := s.Scan(
		&e.ID, &e.AggregateType, &e.AggregateID, &e.EventType, &payload,
		&e.DedupeKey, &status, &e.Attempts, &e.MaxAttempts, &e.NextAttemptAt,
		&leasedUntil, &jobID, &lastErr, &e.CreatedAt, &e.UpdatedAt, &dispatchedAt,
	); err != nil {
		return outbox.Event{}, err
	}
	e.Status = outbox.Status(status)
	if len(payload) > 0 {
		e.Payload = json.RawMessage(payload)
	}
	if leasedUntil.Valid {
		t := leasedUntil.Time
		e.LeasedUntil = &t
	}
	if jobID.Valid {
		id := jobID.UUID
		e.JobID = &id
	}
	e.LastError = lastErr.String
	if dispatchedAt.Valid {
		t := dispatchedAt.Time
		e.DispatchedAt = &t
	}
	return e, nil
}
