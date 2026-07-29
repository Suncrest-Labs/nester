package jobqueue

import (
	"context"
	"encoding/json"
	"fmt"
)

// Client is the producer-facing façade for enqueueing work. It wraps the
// Repository so callers (chain invoker, harvest engine, scheduler) depend on a
// small surface and enqueue metrics are recorded in one place.
type Client struct {
	repo    Repository
	metrics Metrics
}

// NewClient constructs a Client. metrics may be nil.
func NewClient(repo Repository, metrics Metrics) *Client {
	if metrics == nil {
		metrics = NoopMetrics{}
	}
	return &Client{repo: repo, metrics: metrics}
}

// Enqueue adds a job. When in.IdempotencyKey is set and a live job already
// exists for (Type, IdempotencyKey), the existing job is returned and no
// duplicate is created — the enqueue-metric is only counted on real inserts.
func (c *Client) Enqueue(ctx context.Context, in EnqueueInput) (Job, error) {
	if in.Type == "" {
		return Job{}, fmt.Errorf("jobqueue: enqueue requires a type")
	}
	job, created, err := c.repo.Enqueue(ctx, in)
	if err != nil {
		return Job{}, err
	}
	if created {
		c.metrics.IncEnqueued(in.Type)
	}
	return job, nil
}

// EnqueueJSON marshals payload and enqueues a job. It is a convenience over
// Enqueue for the common case of a struct payload.
func (c *Client) EnqueueJSON(ctx context.Context, jobType string, payload any, opts ...EnqueueOption) (Job, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Job{}, fmt.Errorf("jobqueue: marshal payload: %w", err)
	}
	in := EnqueueInput{Type: jobType, Payload: raw}
	for _, opt := range opts {
		opt(&in)
	}
	return c.Enqueue(ctx, in)
}

// EnqueueOption configures an EnqueueInput built by EnqueueJSON.
type EnqueueOption func(*EnqueueInput)

// WithIdempotencyKey sets the dedupe key.
func WithIdempotencyKey(key string) EnqueueOption {
	return func(in *EnqueueInput) { in.IdempotencyKey = key }
}

// WithCorrelationID propagates a correlation/trace id into the job.
func WithCorrelationID(id string) EnqueueOption {
	return func(in *EnqueueInput) { in.CorrelationID = id }
}

// WithPriority sets job priority (higher runs first).
func WithPriority(p int) EnqueueOption {
	return func(in *EnqueueInput) { in.Priority = p }
}

// WithMaxAttempts overrides the retry cap.
func WithMaxAttempts(n int) EnqueueOption {
	return func(in *EnqueueInput) { in.MaxAttempts = n }
}
