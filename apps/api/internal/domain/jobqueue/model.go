// Package jobqueue defines the durable async job queue (#824): the domain
// model, the persistence port, and the worker pool that drives execution.
//
// The queue is Postgres-backed and at-least-once. A job is dequeued with
// `FOR UPDATE SKIP LOCKED` under a lease (visibility timeout); if the worker
// crashes, the lease expires and the job becomes runnable again. Because a job
// may therefore run more than once, handlers MUST be idempotent.
package jobqueue

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned when a job row does not exist.
var ErrNotFound = errors.New("job not found")

// Status is the lifecycle state of a job.
type Status string

const (
	// StatusPending: runnable once next_run_at is due. Retries return here.
	StatusPending Status = "pending"
	// StatusRunning: leased by a worker until leased_until.
	StatusRunning Status = "running"
	// StatusSucceeded: terminal success.
	StatusSucceeded Status = "succeeded"
	// StatusDead: terminal failure — the dead-letter queue.
	StatusDead Status = "dead"
)

// DefaultMaxAttempts is used when EnqueueInput.MaxAttempts is unset (<= 0).
const DefaultMaxAttempts = 5

// Job is a single unit of durable work.
type Job struct {
	ID             uuid.UUID       `json:"id"`
	Type           string          `json:"type"`
	Payload        json.RawMessage `json:"payload"`
	Status         Status          `json:"status"`
	Priority       int             `json:"priority"`
	Attempts       int             `json:"attempts"`
	MaxAttempts    int             `json:"max_attempts"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	CorrelationID  string          `json:"correlation_id,omitempty"`
	NextRunAt      time.Time       `json:"next_run_at"`
	LeasedUntil    *time.Time      `json:"leased_until,omitempty"`
	LastError      string          `json:"last_error,omitempty"`
	Result         json.RawMessage `json:"result,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// EnqueueInput carries the fields needed to enqueue a new job.
type EnqueueInput struct {
	Type    string
	Payload json.RawMessage
	// Priority: higher runs first. Default 0.
	Priority int
	// MaxAttempts caps total execution attempts before dead-lettering.
	// <= 0 uses DefaultMaxAttempts.
	MaxAttempts int
	// IdempotencyKey, when non-empty, deduplicates enqueues: a second enqueue
	// with the same (Type, IdempotencyKey) while a live job exists is a no-op
	// and returns the existing job.
	IdempotencyKey string
	// CorrelationID propagates a trace/correlation identifier into the job so
	// downstream handler logs can be tied back to the originating request.
	CorrelationID string
	// RunAt delays first execution. Zero means "now".
	RunAt time.Time
}

// DequeueParams controls a single lease-and-fetch.
type DequeueParams struct {
	// Type restricts the fetch to one job type. Required.
	Type string
	// Limit caps how many jobs to lease in this call.
	Limit int
	// Lease is the visibility timeout applied to leased jobs.
	Lease time.Duration
	// Now is the reference time (injectable for tests).
	Now time.Time
}

// Stats is an aggregate snapshot of the queue for observability.
type Stats struct {
	// Depth is the number of runnable pending jobs (next_run_at <= now).
	Depth int64 `json:"depth"`
	// Scheduled is pending jobs not yet due (delayed / backing off).
	Scheduled int64 `json:"scheduled"`
	// Running is currently leased jobs.
	Running int64 `json:"running"`
	// DeadLetter is the DLQ depth.
	DeadLetter int64 `json:"dead_letter"`
	// DepthByType breaks pending+running depth down per job type.
	DepthByType map[string]int64 `json:"depth_by_type,omitempty"`
}

// Repository is the persistence port for the job queue. All methods are safe
// for concurrent use across multiple worker processes; Dequeue relies on
// `FOR UPDATE SKIP LOCKED` so competing workers never block one another.
type Repository interface {
	// Enqueue inserts a job. When IdempotencyKey is set and a live job with the
	// same (Type, IdempotencyKey) already exists, it returns that job and
	// created=false without inserting a duplicate.
	Enqueue(ctx context.Context, in EnqueueInput) (job Job, created bool, err error)

	// Dequeue atomically leases up to params.Limit runnable jobs of the given
	// type — pending jobs that are due, plus running jobs whose lease has
	// expired (crash recovery) — marking them running, extending the lease, and
	// incrementing attempts. Returned jobs reflect the post-lease state.
	Dequeue(ctx context.Context, params DequeueParams) ([]Job, error)

	// Complete marks a leased job as succeeded and stores its result.
	Complete(ctx context.Context, id uuid.UUID, result json.RawMessage) error

	// Retry reschedules a job back to pending at nextRunAt and records lastErr.
	Retry(ctx context.Context, id uuid.UUID, nextRunAt time.Time, lastErr string) error

	// DeadLetter moves a job to the dead-letter queue (status=dead).
	DeadLetter(ctx context.Context, id uuid.UUID, lastErr string) error

	// Heartbeat extends the lease of an in-flight job to leasedUntil. It fails
	// with ErrNotFound if the job is no longer running (e.g. lease already lost).
	Heartbeat(ctx context.Context, id uuid.UUID, leasedUntil time.Time) error

	// Stats returns an aggregate snapshot for metrics.
	Stats(ctx context.Context, now time.Time) (Stats, error)
}

// Handler executes a job. Implementations MUST be idempotent because a job can
// be delivered more than once (lease expiry, at-least-once semantics). Returning
// a nil error marks the job succeeded. Returning an error triggers retry with
// backoff, unless the error is permanent (see Permanent), in which case the job
// is dead-lettered immediately.
type Handler interface {
	Handle(ctx context.Context, job Job) error
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(ctx context.Context, job Job) error

// Handle implements Handler.
func (f HandlerFunc) Handle(ctx context.Context, job Job) error { return f(ctx, job) }

// permanentError marks a failure that must skip retries and go straight to the
// dead-letter queue (e.g. malformed payload — retrying cannot help).
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent wraps err so the worker dead-letters the job instead of retrying.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

// IsPermanent reports whether err (or anything it wraps) is a permanent failure.
func IsPermanent(err error) bool {
	var pe permanentError
	return errors.As(err, &pe)
}
