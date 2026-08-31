package stellar

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/suncrestlabs/nester/apps/api/internal/testutil"
)

// These exercise the two guarantees that only the database can provide and
// that the in-memory store in reconciler_test.go can only imitate: the UNIQUE
// constraint that makes Claim atomic across processes, and SKIP LOCKED, which
// stops two reconcilers taking the same submission.
//
// They skip without TEST_DATABASE_DSN, matching the replay harness, and run in
// CI's database job.

func submissionDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if strings.TrimSpace(dsn) == "" {
		t.Skip("TEST_DATABASE_DSN is not set; skipping submission store integration tests")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.Ping(); err != nil {
		t.Fatalf("ping test db: %v", err)
	}

	testutil.ApplyAllMigrations(t, db, "../../migrations")

	if _, err := db.Exec(`TRUNCATE submission_intents`); err != nil {
		t.Fatalf("truncate submission_intents: %v", err)
	}
	return db
}

func newIntent(reference, hash string, createdAt time.Time) SubmissionIntent {
	return SubmissionIntent{
		IdempotencyReference: reference,
		TransactionHash:      hash,
		ValidUntil:           createdAt.Add(5 * time.Minute),
		SourceAccount:        "GABC",
		DomainAction:         "deposit",
		CreatedAt:            createdAt,
	}
}

func TestPostgresStoreClaimIsIdempotent(t *testing.T) {
	db := submissionDB(t)
	store := NewPostgresSubmissionStore(db)
	ctx := context.Background()
	now := time.Now().UTC()

	first, claimed, err := store.Claim(ctx, newIntent("ref-1", "hash-1", now))
	if err != nil || !claimed {
		t.Fatalf("first Claim = (claimed %v, err %v), want a fresh claim", claimed, err)
	}
	if first.State != SubmissionPending {
		t.Fatalf("state = %s, want pending", first.State)
	}

	second, claimed, err := store.Claim(ctx, newIntent("ref-1", "hash-1", now))
	if err != nil {
		t.Fatalf("second Claim: %v", err)
	}
	if claimed {
		t.Fatal("a repeated reference claimed a second submission")
	}
	if second.ID != first.ID {
		t.Fatalf("repeat returned %s, want the original %s", second.ID, first.ID)
	}
}

// The core concurrency guarantee: the unique index, not application code, is
// what collapses simultaneous duplicates into one submission.
func TestPostgresStoreConcurrentClaimsProduceOneSubmission(t *testing.T) {
	db := submissionDB(t)
	store := NewPostgresSubmissionStore(db)
	now := time.Now().UTC()

	const callers = 24
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		claims int
		ids    = map[string]struct{}{}
	)

	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			stored, claimed, err := store.Claim(context.Background(), newIntent("ref-concurrent", "hash-c", now))
			if err != nil {
				t.Errorf("Claim: %v", err)
				return
			}

			mu.Lock()
			if claimed {
				claims++
			}
			ids[stored.ID] = struct{}{}
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()

	if claims != 1 {
		t.Fatalf("%d of %d concurrent claims won, want exactly 1", claims, callers)
	}
	if len(ids) != 1 {
		t.Fatalf("callers saw %d distinct submissions, want 1", len(ids))
	}

	var rows int
	if err := db.QueryRow(
		`SELECT count(*) FROM submission_intents WHERE idempotency_reference = 'ref-concurrent'`,
	).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Fatalf("%d rows exist for one logical submission, want 1", rows)
	}
}

func TestPostgresStoreRejectsReferenceReuse(t *testing.T) {
	db := submissionDB(t)
	store := NewPostgresSubmissionStore(db)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, _, err := store.Claim(ctx, newIntent("ref-2", "hash-original", now)); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	_, _, err := store.Claim(ctx, newIntent("ref-2", "hash-different", now))
	if !errors.Is(err, ErrReferenceReused) {
		t.Fatalf("Claim with a reused reference = %v, want ErrReferenceReused", err)
	}
}

// A settled submission is never reopened, so a late or concurrent
// reconciliation cannot overwrite one outcome with another.
func TestPostgresStoreResolveIsTerminal(t *testing.T) {
	db := submissionDB(t)
	store := NewPostgresSubmissionStore(db)
	ctx := context.Background()
	now := time.Now().UTC()

	intent, _, err := store.Claim(ctx, newIntent("ref-3", "hash-3", now))
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	if err := store.Resolve(ctx, intent.ID, SubmissionLanded, "chain reported SUCCESS", now); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// A stale sweep tries to record a different outcome.
	err = store.Resolve(ctx, intent.ID, SubmissionExpired, "stale", now.Add(time.Minute))
	if !errors.Is(err, ErrIntentNotFound) {
		t.Fatalf("second Resolve = %v, want ErrIntentNotFound (the guard)", err)
	}

	stored, err := store.Get(ctx, intent.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.State != SubmissionLanded {
		t.Fatalf("state = %s, want it to remain landed", stored.State)
	}

	// Resolve must also refuse to move a submission back to pending.
	if err := store.Resolve(ctx, intent.ID, SubmissionPending, "", now); err == nil {
		t.Fatal("Resolve accepted a non-terminal state")
	}
}

// SKIP LOCKED: two reconcilers sweeping at once take disjoint batches, so
// neither works a submission the other already holds.
func TestPostgresStoreConcurrentReconcilersTakeDisjointBatches(t *testing.T) {
	db := submissionDB(t)
	store := NewPostgresSubmissionStore(db)
	ctx := context.Background()
	now := time.Now().UTC()

	const total = 20
	for i := 0; i < total; i++ {
		ref := fmt.Sprintf("ref-batch-%d", i)
		if _, _, err := store.Claim(ctx, newIntent(ref, fmt.Sprintf("hash-%d", i), now)); err != nil {
			t.Fatalf("Claim %s: %v", ref, err)
		}
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		seen = map[string]int{}
	)

	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, err := store.ClaimPendingForReconcile(context.Background(), total, now)
			if err != nil {
				t.Errorf("ClaimPendingForReconcile: %v", err)
				return
			}
			mu.Lock()
			for _, intent := range claimed {
				seen[intent.ID]++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	for id, count := range seen {
		if count != 1 {
			t.Fatalf("submission %s was taken by %d reconcilers, want 1", id, count)
		}
	}
}

// A claimed submission is pushed past its next check, so an RPC outage does
// not turn the sweep into a tight polling loop.
func TestPostgresStoreClaimBacksOffTheNextCheck(t *testing.T) {
	db := submissionDB(t)
	store := NewPostgresSubmissionStore(db)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, _, err := store.Claim(ctx, newIntent("ref-backoff", "hash-b", now)); err != nil {
		t.Fatalf("Claim: %v", err)
	}

	first, err := store.ClaimPendingForReconcile(ctx, 10, now)
	if err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first sweep claimed %d, want 1", len(first))
	}

	// Immediately again: nothing is due.
	second, err := store.ClaimPendingForReconcile(ctx, 10, now)
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second sweep claimed %d, want 0 until the backoff elapses", len(second))
	}

	// Once the backoff has passed it is due again.
	third, err := store.ClaimPendingForReconcile(ctx, 10, now.Add(DefaultReconcileBackoff+time.Second))
	if err != nil {
		t.Fatalf("third sweep: %v", err)
	}
	if len(third) != 1 {
		t.Fatalf("third sweep claimed %d, want 1", len(third))
	}
}

// Resolved submissions drop out of the sweep entirely, so a settled record is
// never re-examined and the reconciler's work shrinks as submissions settle.
func TestPostgresStoreResolvedSubmissionsAreNotSwept(t *testing.T) {
	db := submissionDB(t)
	store := NewPostgresSubmissionStore(db)
	ctx := context.Background()
	now := time.Now().UTC()

	intent, _, err := store.Claim(ctx, newIntent("ref-settled", "hash-s", now))
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := store.Resolve(ctx, intent.ID, SubmissionLanded, "landed", now); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	swept, err := store.ClaimPendingForReconcile(ctx, 10, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	for _, s := range swept {
		if s.ID == intent.ID {
			t.Fatal("a settled submission was claimed for reconciliation")
		}
	}
}

// The durability guarantee, end to end: an intent written by one store handle
// is visible to a completely separate one, which is what makes recovery after
// a process restart work.
func TestPostgresStoreIntentSurvivesANewProcess(t *testing.T) {
	db := submissionDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	original := NewPostgresSubmissionStore(db)
	intent, _, err := original.Claim(ctx, newIntent("ref-restart", "hash-r", now))
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := original.MarkSubmitted(ctx, intent.ID, now); err != nil {
		t.Fatalf("MarkSubmitted: %v", err)
	}

	// A fresh store, standing in for a restarted process with no memory of
	// the request.
	restarted := NewPostgresSubmissionStore(db)
	recovered, err := restarted.Get(ctx, intent.ID)
	if err != nil {
		t.Fatalf("Get after restart: %v", err)
	}
	if recovered.State != SubmissionPending {
		t.Fatalf("state = %s, want pending", recovered.State)
	}
	if recovered.SubmittedAt == nil {
		t.Fatal("submitted_at was not persisted; the RPC-memory check depends on it")
	}
	if recovered.TransactionHash != "hash-r" {
		t.Fatalf("transaction hash = %q, want it preserved", recovered.TransactionHash)
	}

	// And it is still due for reconciliation.
	swept, err := restarted.ClaimPendingForReconcile(ctx, 10, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("sweep after restart: %v", err)
	}
	found := false
	for _, s := range swept {
		if s.ID == intent.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("a pending submission was not picked up after restart")
	}
}
