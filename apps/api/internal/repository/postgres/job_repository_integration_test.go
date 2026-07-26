package postgres

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
)

// setupJobRepo wipes the public schema and applies only the jobs table
// migration (059) — the jobs table has no foreign keys, so it stands alone.
func setupJobRepo(t *testing.T) *JobRepository {
	t.Helper()
	db := openIntegrationDB(t)

	if _, err := db.Exec(`
		DO $$
		DECLARE r record;
		BEGIN
			FOR r IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP
				EXECUTE 'DROP TABLE IF EXISTS public.' || quote_ident(r.tablename) || ' CASCADE';
			END LOOP;
		END$$;
	`); err != nil {
		t.Fatalf("drop tables: %v", err)
	}

	path := filepath.Join("..", "..", "..", "migrations", "059_create_jobs.up.sql")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := db.Exec(string(contents)); err != nil {
		t.Fatalf("apply 059: %v", err)
	}
	return NewJobRepository(db)
}

func TestJobRepository_EnqueueDequeueComplete(t *testing.T) {
	repo := setupJobRepo(t)
	ctx := context.Background()
	now := time.Now()

	job, created, err := repo.Enqueue(ctx, jobqueue.EnqueueInput{
		Type:          "harvest",
		Payload:       json.RawMessage(`{"vault_id":"v1"}`),
		CorrelationID: "corr-1",
	})
	if err != nil || !created {
		t.Fatalf("enqueue: created=%v err=%v", created, err)
	}

	leased, err := repo.Dequeue(ctx, jobqueue.DequeueParams{
		Type: "harvest", Limit: 10, Lease: time.Minute, Now: now,
	})
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if len(leased) != 1 {
		t.Fatalf("dequeued %d jobs, want 1", len(leased))
	}
	if leased[0].Status != jobqueue.StatusRunning || leased[0].Attempts != 1 {
		t.Fatalf("leased job status=%s attempts=%d", leased[0].Status, leased[0].Attempts)
	}
	if leased[0].CorrelationID != "corr-1" {
		t.Fatalf("correlation id lost: %q", leased[0].CorrelationID)
	}

	// A second immediate dequeue sees nothing — the job is leased.
	again, err := repo.Dequeue(ctx, jobqueue.DequeueParams{
		Type: "harvest", Limit: 10, Lease: time.Minute, Now: now,
	})
	if err != nil || len(again) != 0 {
		t.Fatalf("expected no jobs while leased, got %d (err=%v)", len(again), err)
	}

	if err := repo.Complete(ctx, job.ID, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatalf("complete: %v", err)
	}
}

func TestJobRepository_IdempotentEnqueue(t *testing.T) {
	repo := setupJobRepo(t)
	ctx := context.Background()

	a, createdA, err := repo.Enqueue(ctx, jobqueue.EnqueueInput{Type: "harvest", IdempotencyKey: "vault-9"})
	if err != nil || !createdA {
		t.Fatalf("first enqueue: created=%v err=%v", createdA, err)
	}
	b, createdB, err := repo.Enqueue(ctx, jobqueue.EnqueueInput{Type: "harvest", IdempotencyKey: "vault-9"})
	if err != nil {
		t.Fatalf("second enqueue: %v", err)
	}
	if createdB {
		t.Fatal("second enqueue with same key must not create a duplicate")
	}
	if a.ID != b.ID {
		t.Fatalf("dedupe returned different job: %s != %s", a.ID, b.ID)
	}

	// Two jobs with no idempotency key must both be created (NULL keys never conflict).
	_, c1, _ := repo.Enqueue(ctx, jobqueue.EnqueueInput{Type: "harvest"})
	_, c2, _ := repo.Enqueue(ctx, jobqueue.EnqueueInput{Type: "harvest"})
	if !c1 || !c2 {
		t.Fatalf("null-key enqueues should both create: %v %v", c1, c2)
	}
}

func TestJobRepository_LeaseExpiryReclaim(t *testing.T) {
	repo := setupJobRepo(t)
	ctx := context.Background()
	now := time.Now()

	_, _, err := repo.Enqueue(ctx, jobqueue.EnqueueInput{Type: "recover"})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Lease with a zero-length lease so it is immediately expired.
	leased, err := repo.Dequeue(ctx, jobqueue.DequeueParams{Type: "recover", Limit: 1, Lease: 0, Now: now})
	if err != nil || len(leased) != 1 {
		t.Fatalf("first dequeue: %d (err=%v)", len(leased), err)
	}

	// A later dequeue (after the expiry instant) reclaims the still-running job.
	reclaimed, err := repo.Dequeue(ctx, jobqueue.DequeueParams{
		Type: "recover", Limit: 1, Lease: time.Minute, Now: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("reclaim dequeue: %v", err)
	}
	if len(reclaimed) != 1 {
		t.Fatalf("expected to reclaim expired-lease job, got %d", len(reclaimed))
	}
	if reclaimed[0].Attempts != 2 {
		t.Fatalf("reclaimed attempts = %d, want 2", reclaimed[0].Attempts)
	}
}

func TestJobRepository_RetryAndDeadLetter(t *testing.T) {
	repo := setupJobRepo(t)
	ctx := context.Background()
	now := time.Now()

	job, _, _ := repo.Enqueue(ctx, jobqueue.EnqueueInput{Type: "flaky", MaxAttempts: 2})
	leased, _ := repo.Dequeue(ctx, jobqueue.DequeueParams{Type: "flaky", Limit: 1, Lease: time.Minute, Now: now})
	if len(leased) != 1 {
		t.Fatal("expected one leased job")
	}

	// Retry pushes it back to pending in the future.
	future := now.Add(time.Hour)
	if err := repo.Retry(ctx, job.ID, future, "transient"); err != nil {
		t.Fatalf("retry: %v", err)
	}
	notReady, _ := repo.Dequeue(ctx, jobqueue.DequeueParams{Type: "flaky", Limit: 1, Lease: time.Minute, Now: now})
	if len(notReady) != 0 {
		t.Fatal("job scheduled in the future should not be dequeued yet")
	}

	// Lease again (as if the delay passed) and dead-letter it.
	leased2, _ := repo.Dequeue(ctx, jobqueue.DequeueParams{Type: "flaky", Limit: 1, Lease: time.Minute, Now: future})
	if len(leased2) != 1 {
		t.Fatal("expected to re-lease after delay")
	}
	if err := repo.DeadLetter(ctx, job.ID, "gave up"); err != nil {
		t.Fatalf("dead-letter: %v", err)
	}

	s, err := repo.Stats(ctx, future)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if s.DeadLetter != 1 {
		t.Fatalf("DLQ depth = %d, want 1", s.DeadLetter)
	}
}

func TestJobRepository_HeartbeatExtendsLease(t *testing.T) {
	repo := setupJobRepo(t)
	ctx := context.Background()
	now := time.Now()

	job, _, _ := repo.Enqueue(ctx, jobqueue.EnqueueInput{Type: "long"})
	leased, _ := repo.Dequeue(ctx, jobqueue.DequeueParams{Type: "long", Limit: 1, Lease: time.Second, Now: now})
	if len(leased) != 1 {
		t.Fatal("expected one leased job")
	}

	if err := repo.Heartbeat(ctx, job.ID, now.Add(time.Hour)); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	// Even well past the original lease, the extended lease keeps it invisible.
	after, _ := repo.Dequeue(ctx, jobqueue.DequeueParams{Type: "long", Limit: 1, Lease: time.Second, Now: now.Add(2 * time.Second)})
	if len(after) != 0 {
		t.Fatal("heartbeat should have extended the lease past the reclaim window")
	}

	// Completing releases the lease; heartbeat on a non-running job reports not-found.
	_ = repo.Complete(ctx, job.ID, nil)
	if err := repo.Heartbeat(ctx, job.ID, now.Add(2*time.Hour)); err != jobqueue.ErrNotFound {
		t.Fatalf("heartbeat on completed job = %v, want ErrNotFound", err)
	}
}
