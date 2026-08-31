package postgres

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/outbox"
	"github.com/suncrestlabs/nester/apps/api/internal/testutil"
)

// setupOutboxRepo applies the full migration chain and returns a repository
// plus the raw handle, which the transaction tests need directly.
func setupOutboxRepo(t *testing.T) (*OutboxRepository, *sql.DB) {
	t.Helper()
	db := openIntegrationDB(t)
	testutil.ApplyAllMigrations(t, db, filepath.Join("..", "..", "..", "migrations"))
	return NewOutboxRepository(db), db
}

func newOutboxEvent(t *testing.T, aggregateID, dedupeKey string) outbox.Event {
	t.Helper()
	e, err := outbox.NewEvent("savings_goal", aggregateID, "test.event", dedupeKey, map[string]any{"k": dedupeKey})
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	return e
}

func insertInTx(t *testing.T, repo *OutboxRepository, db *sql.DB, e outbox.Event) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := repo.Insert(context.Background(), tx, e); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func countOutbox(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM outbox`).Scan(&n); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	return n
}

// TestOutboxRollbackLeavesNoRow is the acceptance criterion "rolling back the
// domain transaction leaves no outbox row". This is the property the entire
// pattern rests on and it cannot be tested without a real transaction, which
// is why it lives here rather than beside the relay's unit tests.
func TestOutboxRollbackLeavesNoRow(t *testing.T) {
	repo, db := setupOutboxRepo(t)
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}

	// A domain write and its side effect, in one transaction.
	goalID := uuid.New()
	userID := seedIntegrationUser(t, db)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO savings_goals (id, user_id, target_amount, currency, deadline, description)
		VALUES ($1, $2, 100, 'USDC', NOW() + INTERVAL '30 days', 'rollback test')`,
		goalID, userID,
	); err != nil {
		t.Fatalf("insert domain row: %v", err)
	}
	if err := repo.Insert(ctx, tx, newOutboxEvent(t, goalID.String(), "dedupe-rollback")); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	if n := countOutbox(t, db); n != 0 {
		t.Fatalf("outbox rows after rollback = %d, want 0", n)
	}
	var domainRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM savings_goals WHERE id = $1`, goalID).Scan(&domainRows); err != nil {
		t.Fatalf("count domain rows: %v", err)
	}
	if domainRows != 0 {
		t.Fatalf("domain rows after rollback = %d, want 0", domainRows)
	}
}

// TestOutboxCommitPersistsBothWrites is the mirror image: commit and the
// side-effect intent is durable, which is what survives the crash.
func TestOutboxCommitPersistsBothWrites(t *testing.T) {
	repo, db := setupOutboxRepo(t)
	ctx := context.Background()

	userID := seedIntegrationUser(t, db)
	goalID := uuid.New()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO savings_goals (id, user_id, target_amount, currency, deadline, description)
		VALUES ($1, $2, 100, 'USDC', NOW() + INTERVAL '30 days', 'commit test')`,
		goalID, userID,
	); err != nil {
		t.Fatalf("insert domain row: %v", err)
	}
	if err := repo.Insert(ctx, tx, newOutboxEvent(t, goalID.String(), "dedupe-commit")); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if n := countOutbox(t, db); n != 1 {
		t.Fatalf("outbox rows after commit = %d, want 1", n)
	}
}

// TestOutboxInsertIsIdempotentOnDedupeKey: a producer that retries its own
// transaction must not schedule the same side effect twice.
func TestOutboxInsertIsIdempotentOnDedupeKey(t *testing.T) {
	repo, db := setupOutboxRepo(t)

	insertInTx(t, repo, db, newOutboxEvent(t, "goal-1", "dedupe-same"))
	insertInTx(t, repo, db, newOutboxEvent(t, "goal-1", "dedupe-same"))

	if n := countOutbox(t, db); n != 1 {
		t.Fatalf("outbox rows = %d, want 1 — the dedupe key must collapse the retry", n)
	}
}

// TestClaimDueReturnsOneHeadPerAggregate pins the per-aggregate ordering
// guarantee in the SQL itself, where the DISTINCT ON lives.
func TestClaimDueReturnsOneHeadPerAggregate(t *testing.T) {
	repo, db := setupOutboxRepo(t)
	ctx := context.Background()

	insertInTx(t, repo, db, newOutboxEvent(t, "goal-a", "a-1"))
	insertInTx(t, repo, db, newOutboxEvent(t, "goal-a", "a-2"))
	insertInTx(t, repo, db, newOutboxEvent(t, "goal-b", "b-1"))

	claimed, err := repo.ClaimDue(ctx, outbox.ClaimParams{Limit: 10, Lease: time.Minute, Now: time.Now()})
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("claimed %d events, want 2 (one head per aggregate)", len(claimed))
	}
	keys := map[string]bool{}
	for _, e := range claimed {
		keys[e.DedupeKey] = true
		if e.Status != outbox.StatusDispatching {
			t.Fatalf("claimed event %s status = %q, want dispatching", e.DedupeKey, e.Status)
		}
		if e.Attempts != 1 {
			t.Fatalf("claimed event %s attempts = %d, want 1", e.DedupeKey, e.Attempts)
		}
	}
	if !keys["a-1"] || !keys["b-1"] {
		t.Fatalf("claimed %v, want the oldest of each aggregate (a-1, b-1)", keys)
	}
	if keys["a-2"] {
		t.Fatal("claimed a-2 while a-1 is still in flight — per-aggregate ordering is broken")
	}
}

// TestClaimDueDoesNotSkipABackingOffHead: filtering due-ness before picking
// the head would let the relay jump over a backing-off event to the one
// behind it, delivering out of order. The head must simply not be claimed.
func TestClaimDueDoesNotSkipABackingOffHead(t *testing.T) {
	repo, db := setupOutboxRepo(t)
	ctx := context.Background()

	head := newOutboxEvent(t, "goal-a", "a-head")
	head.NextAttemptAt = time.Now().Add(time.Hour)
	insertInTx(t, repo, db, head)
	insertInTx(t, repo, db, newOutboxEvent(t, "goal-a", "a-behind"))

	claimed, err := repo.ClaimDue(ctx, outbox.ClaimParams{Limit: 10, Lease: time.Minute, Now: time.Now()})
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("claimed %d events while the head backs off, want 0", len(claimed))
	}
}

// TestClaimDueReclaimsAbandonedHandoff: a relay that died between claiming a
// row and recording its job leaves no job owning the delivery, so the lease
// has to make the row claimable again.
func TestClaimDueReclaimsAbandonedHandoff(t *testing.T) {
	repo, db := setupOutboxRepo(t)
	ctx := context.Background()
	insertInTx(t, repo, db, newOutboxEvent(t, "goal-a", "a-1"))

	now := time.Now()
	if _, err := repo.ClaimDue(ctx, outbox.ClaimParams{Limit: 10, Lease: time.Minute, Now: now}); err != nil {
		t.Fatalf("first ClaimDue: %v", err)
	}

	// Still leased: nobody else may take it.
	again, err := repo.ClaimDue(ctx, outbox.ClaimParams{Limit: 10, Lease: time.Minute, Now: now.Add(30 * time.Second)})
	if err != nil {
		t.Fatalf("ClaimDue during lease: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("claimed %d events during an active lease, want 0", len(again))
	}

	// Lease lapsed: reclaimable.
	reclaimed, err := repo.ClaimDue(ctx, outbox.ClaimParams{Limit: 10, Lease: time.Minute, Now: now.Add(2 * time.Minute)})
	if err != nil {
		t.Fatalf("ClaimDue after lease: %v", err)
	}
	if len(reclaimed) != 1 {
		t.Fatalf("reclaimed %d events after lease expiry, want 1", len(reclaimed))
	}
	if reclaimed[0].Attempts != 2 {
		t.Fatalf("attempts = %d after reclaim, want 2", reclaimed[0].Attempts)
	}
}

// TestClaimDueStillClaimsAnExhaustedHandoff pins the absence of an attempts
// cap in the claim. Capping it would leave a row that reached max_attempts
// stuck in 'dispatching' with no job carrying it, blocking its aggregate
// forever; it has to come back so the relay can dead-letter it.
func TestClaimDueStillClaimsAnExhaustedHandoff(t *testing.T) {
	repo, db := setupOutboxRepo(t)
	ctx := context.Background()

	e := newOutboxEvent(t, "goal-a", "a-exhausted")
	e.MaxAttempts = 1
	insertInTx(t, repo, db, e)

	now := time.Now()
	claimed, err := repo.ClaimDue(ctx, outbox.ClaimParams{Limit: 10, Lease: time.Minute, Now: now})
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(claimed) != 1 || claimed[0].Attempts != 1 {
		t.Fatalf("first claim = %+v, want one row at attempts 1", claimed)
	}

	// The relay died before recording a job; the lease lapses.
	reclaimed, err := repo.ClaimDue(ctx, outbox.ClaimParams{Limit: 10, Lease: time.Minute, Now: now.Add(2 * time.Minute)})
	if err != nil {
		t.Fatalf("ClaimDue after lease: %v", err)
	}
	if len(reclaimed) != 1 {
		t.Fatalf("reclaimed %d rows at the attempts cap, want 1 — otherwise it blocks its aggregate forever", len(reclaimed))
	}
	if reclaimed[0].Attempts <= reclaimed[0].MaxAttempts {
		t.Fatalf("attempts = %d, max = %d; the relay needs attempts > max to recognise it as exhausted",
			reclaimed[0].Attempts, reclaimed[0].MaxAttempts)
	}
}

// TestClaimDueLeavesRowsWithAJobAlone: once a job owns the delivery, lease
// expiry must NOT reclaim the row — that would enqueue a second job chain
// for a delivery already in flight.
func TestClaimDueLeavesRowsWithAJobAlone(t *testing.T) {
	repo, db := setupOutboxRepo(t)
	ctx := context.Background()
	insertInTx(t, repo, db, newOutboxEvent(t, "goal-a", "a-1"))

	now := time.Now()
	claimed, err := repo.ClaimDue(ctx, outbox.ClaimParams{Limit: 10, Lease: time.Minute, Now: now})
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if err := repo.MarkDispatching(ctx, claimed[0].ID, uuid.New()); err != nil {
		t.Fatalf("MarkDispatching: %v", err)
	}

	again, err := repo.ClaimDue(ctx, outbox.ClaimParams{Limit: 10, Lease: time.Minute, Now: now.Add(time.Hour)})
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("reclaimed %d events that a job already owns, want 0", len(again))
	}
}

// TestMarkDeadUnblocksAggregate: dead is terminal, so the events behind a
// poison message become claimable.
func TestMarkDeadUnblocksAggregate(t *testing.T) {
	repo, db := setupOutboxRepo(t)
	ctx := context.Background()
	insertInTx(t, repo, db, newOutboxEvent(t, "goal-a", "a-head"))
	insertInTx(t, repo, db, newOutboxEvent(t, "goal-a", "a-behind"))

	now := time.Now()
	claimed, err := repo.ClaimDue(ctx, outbox.ClaimParams{Limit: 10, Lease: time.Minute, Now: now})
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if err := repo.MarkDead(ctx, claimed[0].ID, "consumer returns 400 forever"); err != nil {
		t.Fatalf("MarkDead: %v", err)
	}

	next, err := repo.ClaimDue(ctx, outbox.ClaimParams{Limit: 10, Lease: time.Minute, Now: now})
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(next) != 1 || next[0].DedupeKey != "a-behind" {
		t.Fatalf("after dead-lettering the head, claimed %v, want [a-behind]", next)
	}
}

func TestInFlightAndStats(t *testing.T) {
	repo, db := setupOutboxRepo(t)
	ctx := context.Background()
	insertInTx(t, repo, db, newOutboxEvent(t, "goal-a", "a-1"))
	insertInTx(t, repo, db, newOutboxEvent(t, "goal-b", "b-1"))

	now := time.Now()
	claimed, err := repo.ClaimDue(ctx, outbox.ClaimParams{Limit: 1, Lease: time.Minute, Now: now})
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	jobID := uuid.New()
	if err := repo.MarkDispatching(ctx, claimed[0].ID, jobID); err != nil {
		t.Fatalf("MarkDispatching: %v", err)
	}

	inFlight, err := repo.InFlight(ctx, 10)
	if err != nil {
		t.Fatalf("InFlight: %v", err)
	}
	if len(inFlight) != 1 || inFlight[0].JobID == nil || *inFlight[0].JobID != jobID {
		t.Fatalf("InFlight = %v, want one row carrying job %s", inFlight, jobID)
	}

	stats, err := repo.Stats(ctx, now)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Dispatching != 1 {
		t.Fatalf("dispatching = %d, want 1", stats.Dispatching)
	}
	if stats.Pending != 1 {
		t.Fatalf("pending = %d, want 1", stats.Pending)
	}
}

// TestPruneTerminalRemovesOnlyTerminalRows: pruning an undelivered event is
// silently dropping the side effect the outbox exists to guarantee.
func TestPruneTerminalRemovesOnlyTerminalRows(t *testing.T) {
	repo, db := setupOutboxRepo(t)
	ctx := context.Background()

	insertInTx(t, repo, db, newOutboxEvent(t, "goal-pending", "keep-pending"))
	insertInTx(t, repo, db, newOutboxEvent(t, "goal-done", "prune-dispatched"))
	insertInTx(t, repo, db, newOutboxEvent(t, "goal-dead", "prune-dead"))

	now := time.Now()
	claimed, err := repo.ClaimDue(ctx, outbox.ClaimParams{Limit: 10, Lease: time.Minute, Now: now})
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	for _, e := range claimed {
		switch e.DedupeKey {
		case "prune-dispatched":
			if err := repo.MarkDispatched(ctx, e.ID, now); err != nil {
				t.Fatalf("MarkDispatched: %v", err)
			}
		case "prune-dead":
			if err := repo.MarkDead(ctx, e.ID, "poison"); err != nil {
				t.Fatalf("MarkDead: %v", err)
			}
		case "keep-pending":
			if err := repo.Release(ctx, e.ID, now, "transient"); err != nil {
				t.Fatalf("Release: %v", err)
			}
		}
	}

	// Age every row well past both cutoffs; only the terminal ones may go.
	if _, err := db.ExecContext(ctx, `UPDATE outbox SET updated_at = NOW() - INTERVAL '365 days'`); err != nil {
		t.Fatalf("age rows: %v", err)
	}

	dispatched, dead, err := repo.PruneTerminal(ctx, now.Add(-time.Hour), now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("PruneTerminal: %v", err)
	}
	if dispatched != 1 || dead != 1 {
		t.Fatalf("pruned dispatched=%d dead=%d, want 1 and 1", dispatched, dead)
	}

	var remaining string
	if err := db.QueryRow(`SELECT dedupe_key FROM outbox`).Scan(&remaining); err != nil {
		t.Fatalf("read remaining row: %v", err)
	}
	if remaining != "keep-pending" {
		t.Fatalf("remaining row = %q, want %q", remaining, "keep-pending")
	}
}

// TestJobStatusReportsNotFoundForMissingJob backs the relay's "the job row is
// gone, hand the event over again" path.
func TestJobStatusReportsNotFoundForMissingJob(t *testing.T) {
	db := openIntegrationDB(t)
	testutil.ApplyAllMigrations(t, db, filepath.Join("..", "..", "..", "migrations"))
	jobs := NewJobRepository(db)

	if _, err := jobs.JobStatus(context.Background(), uuid.New()); !errors.Is(err, jobqueue.ErrNotFound) {
		t.Fatalf("JobStatus for a missing job = %v, want ErrNotFound", err)
	}
}
