package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
)

// JobRepository persists the durable job queue (#824) in Postgres.
type JobRepository struct {
	db *sql.DB
}

// NewJobRepository constructs a JobRepository.
func NewJobRepository(db *sql.DB) *JobRepository {
	return &JobRepository{db: db}
}

const jobColumns = `id, type, payload, status, priority, attempts, max_attempts,
	idempotency_key, correlation_id, next_run_at, leased_until, last_error,
	result, created_at, updated_at`

// jobColumnsJ is jobColumns qualified with the UPDATE target alias `j`, for use
// in a RETURNING clause where the bare names would be ambiguous.
const jobColumnsJ = `j.id, j.type, j.payload, j.status, j.priority, j.attempts,
	j.max_attempts, j.idempotency_key, j.correlation_id, j.next_run_at,
	j.leased_until, j.last_error, j.result, j.created_at, j.updated_at`

// Enqueue inserts a job, deduplicating on (type, idempotency_key) while a live
// job exists. When a duplicate is found, the existing live job is returned with
// created=false.
func (r *JobRepository) Enqueue(ctx context.Context, in jobqueue.EnqueueInput) (jobqueue.Job, bool, error) {
	payload := in.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	maxAttempts := in.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = jobqueue.DefaultMaxAttempts
	}
	runAt := in.RunAt
	if runAt.IsZero() {
		runAt = time.Now()
	}
	idem := nullText(in.IdempotencyKey)
	corr := nullText(in.CorrelationID)

	row := r.db.QueryRowContext(ctx, `
		INSERT INTO jobs
		    (type, payload, status, priority, attempts, max_attempts,
		     idempotency_key, correlation_id, next_run_at)
		VALUES ($1, $2, 'pending', $3, 0, $4, $5, $6, $7)
		ON CONFLICT (type, idempotency_key)
		    WHERE idempotency_key IS NOT NULL AND status IN ('pending', 'running')
		    DO NOTHING
		RETURNING `+jobColumns,
		in.Type, []byte(payload), in.Priority, maxAttempts, idem, corr, runAt,
	)

	job, err := scanJob(row)
	if err == nil {
		return job, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return jobqueue.Job{}, false, err
	}

	// Conflict (DO NOTHING returned no row): fetch the existing live job.
	existing := r.db.QueryRowContext(ctx, `
		SELECT `+jobColumns+`
		FROM jobs
		WHERE type = $1 AND idempotency_key = $2
		  AND status IN ('pending', 'running')
		ORDER BY created_at DESC
		LIMIT 1`,
		in.Type, in.IdempotencyKey,
	)
	job, err = scanJob(existing)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Raced with the other job reaching a terminal state between our
			// insert and this select; report not-found so the caller can retry.
			return jobqueue.Job{}, false, jobqueue.ErrNotFound
		}
		return jobqueue.Job{}, false, err
	}
	return job, false, nil
}

// Dequeue leases up to params.Limit runnable jobs of params.Type using
// FOR UPDATE SKIP LOCKED so concurrent workers never contend. Runnable = due
// pending jobs plus running jobs whose lease has expired (crash recovery).
func (r *JobRepository) Dequeue(ctx context.Context, params jobqueue.DequeueParams) ([]jobqueue.Job, error) {
	if params.Limit <= 0 {
		return nil, nil
	}
	leasedUntil := params.Now.Add(params.Lease)

	rows, err := r.db.QueryContext(ctx, `
		WITH ready AS (
		    SELECT id
		    FROM jobs
		    WHERE type = $1
		      AND (
		          (status = 'pending' AND next_run_at <= $2)
		          OR (status = 'running' AND leased_until <= $2)
		      )
		    ORDER BY priority DESC, next_run_at
		    FOR UPDATE SKIP LOCKED
		    LIMIT $4
		)
		UPDATE jobs j
		SET status = 'running',
		    leased_until = $3,
		    attempts = attempts + 1,
		    updated_at = NOW()
		FROM ready
		WHERE j.id = ready.id
		RETURNING `+jobColumnsJ,
		params.Type, params.Now, leasedUntil, params.Limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []jobqueue.Job
	for rows.Next() {
		job, err := scanJobRows(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// Complete marks a leased job succeeded. It is a no-op if the caller no longer
// holds the lease (the job was reclaimed), preserving at-least-once semantics.
func (r *JobRepository) Complete(ctx context.Context, id uuid.UUID, result json.RawMessage) error {
	var res any
	if len(result) > 0 {
		res = []byte(result)
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'succeeded', result = $2, leased_until = NULL,
		    last_error = NULL, updated_at = NOW()
		WHERE id = $1 AND status = 'running'`,
		id, res,
	)
	return err
}

// Retry reschedules a job back to pending at nextRunAt. No-op if the lease was lost.
func (r *JobRepository) Retry(ctx context.Context, id uuid.UUID, nextRunAt time.Time, lastErr string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'pending', next_run_at = $2, last_error = $3,
		    leased_until = NULL, updated_at = NOW()
		WHERE id = $1 AND status = 'running'`,
		id, nextRunAt, nullText(lastErr),
	)
	return err
}

// DeadLetter moves a job to the DLQ. No-op if the lease was lost.
func (r *JobRepository) DeadLetter(ctx context.Context, id uuid.UUID, lastErr string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'dead', last_error = $2, leased_until = NULL, updated_at = NOW()
		WHERE id = $1 AND status = 'running'`,
		id, nullText(lastErr),
	)
	return err
}

// Heartbeat extends a running job's lease. Returns ErrNotFound if the job is no
// longer running (lease lost / job reclaimed).
func (r *JobRepository) Heartbeat(ctx context.Context, id uuid.UUID, leasedUntil time.Time) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE jobs
		SET leased_until = $2, updated_at = NOW()
		WHERE id = $1 AND status = 'running'`,
		id, leasedUntil,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return jobqueue.ErrNotFound
	}
	return nil
}

// Stats returns aggregate queue counters plus per-type live depth.
func (r *JobRepository) Stats(ctx context.Context, now time.Time) (jobqueue.Stats, error) {
	var s jobqueue.Stats
	row := r.db.QueryRowContext(ctx, `
		SELECT
		    COUNT(*) FILTER (WHERE status = 'pending' AND next_run_at <= $1) AS depth,
		    COUNT(*) FILTER (WHERE status = 'pending' AND next_run_at > $1)  AS scheduled,
		    COUNT(*) FILTER (WHERE status = 'running')                        AS running,
		    COUNT(*) FILTER (WHERE status = 'dead')                           AS dead
		FROM jobs`,
		now,
	)
	if err := row.Scan(&s.Depth, &s.Scheduled, &s.Running, &s.DeadLetter); err != nil {
		return jobqueue.Stats{}, err
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT type, COUNT(*)
		FROM jobs
		WHERE status IN ('pending', 'running')
		GROUP BY type`)
	if err != nil {
		return jobqueue.Stats{}, err
	}
	defer rows.Close()
	s.DepthByType = map[string]int64{}
	for rows.Next() {
		var t string
		var n int64
		if err := rows.Scan(&t, &n); err != nil {
			return jobqueue.Stats{}, err
		}
		s.DepthByType[t] = n
	}
	return s, rows.Err()
}

func scanJob(row *sql.Row) (jobqueue.Job, error)       { return scanJobFrom(row) }
func scanJobRows(rows *sql.Rows) (jobqueue.Job, error) { return scanJobFrom(rows) }

func scanJobFrom(s rowScanner) (jobqueue.Job, error) {
	var (
		job         jobqueue.Job
		payload     []byte
		result      []byte
		idem        sql.NullString
		corr        sql.NullString
		lastErr     sql.NullString
		leasedUntil sql.NullTime
		status      string
	)
	if err := s.Scan(
		&job.ID, &job.Type, &payload, &status, &job.Priority, &job.Attempts,
		&job.MaxAttempts, &idem, &corr, &job.NextRunAt, &leasedUntil, &lastErr,
		&result, &job.CreatedAt, &job.UpdatedAt,
	); err != nil {
		return jobqueue.Job{}, err
	}
	job.Status = jobqueue.Status(status)
	if len(payload) > 0 {
		job.Payload = json.RawMessage(payload)
	}
	if len(result) > 0 {
		job.Result = json.RawMessage(result)
	}
	job.IdempotencyKey = idem.String
	job.CorrelationID = corr.String
	job.LastError = lastErr.String
	if leasedUntil.Valid {
		t := leasedUntil.Time
		job.LeasedUntil = &t
	}
	return job, nil
}

// nullText returns a SQL NULL for empty input (so nullable text columns and
// partial unique indexes treat "unset" correctly) and the value otherwise.
// Distinct from this package's nullString, which coerces "" to ” for NOT NULL
// columns.
func nullText(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
