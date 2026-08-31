package stellar

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// memStore is an in-memory SubmissionStore with the same atomicity contract
// the Postgres implementation provides, so the submit and reconcile flows can
// be driven end to end without a database. The uniqueness guarantee is under
// a mutex here and a unique index there; both make Claim atomic, which is
// what the flow depends on.
type memStore struct {
	mu       sync.Mutex
	byID     map[string]SubmissionIntent
	byRef    map[string]string
	nextID   int
	claimErr error
}

func newMemStore() *memStore {
	return &memStore{byID: map[string]SubmissionIntent{}, byRef: map[string]string{}}
}

func (s *memStore) Claim(_ context.Context, intent SubmissionIntent) (SubmissionIntent, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.claimErr != nil {
		return SubmissionIntent{}, false, s.claimErr
	}

	if id, exists := s.byRef[intent.IdempotencyReference]; exists {
		stored := s.byID[id]
		if stored.TransactionHash != intent.TransactionHash {
			return SubmissionIntent{}, false, ErrReferenceReused
		}
		return stored, false, nil
	}

	s.nextID++
	intent.ID = fmt.Sprintf("sub-%d", s.nextID)
	if intent.State == "" {
		intent.State = SubmissionPending
	}
	s.byID[intent.ID] = intent
	s.byRef[intent.IdempotencyReference] = intent.ID
	return intent, true, nil
}

func (s *memStore) MarkSubmitted(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	intent, ok := s.byID[id]
	if !ok {
		return ErrIntentNotFound
	}
	intent.SubmittedAt = &at
	s.byID[id] = intent
	return nil
}

func (s *memStore) Resolve(_ context.Context, id string, state SubmissionState, detail string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	intent, ok := s.byID[id]
	if !ok {
		return ErrIntentNotFound
	}
	if intent.State.Terminal() {
		// Mirrors the Postgres guard: a settled submission is never reopened.
		return ErrIntentNotFound
	}
	intent.State = state
	intent.OutcomeDetail = detail
	intent.ResolvedAt = &at
	s.byID[id] = intent
	return nil
}

func (s *memStore) Get(_ context.Context, id string) (SubmissionIntent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	intent, ok := s.byID[id]
	if !ok {
		return SubmissionIntent{}, ErrIntentNotFound
	}
	return intent, nil
}

func (s *memStore) ClaimPendingForReconcile(_ context.Context, limit int, _ time.Time) ([]SubmissionIntent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.claimErr != nil {
		return nil, s.claimErr
	}

	var out []SubmissionIntent
	for _, intent := range s.byID {
		if intent.State.Terminal() || len(out) >= limit {
			continue
		}
		out = append(out, intent)
	}
	return out, nil
}

func (s *memStore) state(t *testing.T, id string) SubmissionState {
	t.Helper()
	intent, err := s.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get(%s): %v", id, err)
	}
	return intent.State
}

// scriptedChain answers LookupTransaction from a script and counts how many
// times it was asked, so a test can prove the reconciler reads rather than
// writes.
type scriptedChain struct {
	mu      sync.Mutex
	status  TransactionStatus
	view    ChainView
	err     error
	lookups int
}

func (c *scriptedChain) LookupTransaction(_ context.Context, _ string) (TransactionStatus, ChainView, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lookups++
	return c.status, c.view, c.err
}

func (c *scriptedChain) set(status TransactionStatus, view ChainView, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status, c.view, c.err = status, view, err
}

func (c *scriptedChain) lookupCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lookups
}

// submitOnce records a durable intent and simulates a submission whose
// response was lost, which is the state every scenario below starts from.
func submitOnce(t *testing.T, store *memStore) SubmissionIntent {
	t.Helper()

	intent, claimed, err := store.Claim(context.Background(), SubmissionIntent{
		IdempotencyReference: "vault-deposit-abc",
		TransactionHash:      "a1b2c3",
		ValidUntil:           validUntil,
		SourceAccount:        "GABC",
		DomainAction:         "deposit",
		State:                SubmissionPending,
		CreatedAt:            submittedAt,
	})
	if err != nil || !claimed {
		t.Fatalf("Claim() = (claimed %v, err %v), want a fresh claim", claimed, err)
	}
	if err := store.MarkSubmitted(context.Background(), intent.ID, submittedAt); err != nil {
		t.Fatalf("MarkSubmitted: %v", err)
	}

	intent, _ = store.Get(context.Background(), intent.ID)
	return intent
}

func newTestReconciler(store *memStore, chain ChainLookup, now time.Time) *SubmissionReconciler {
	r := NewSubmissionReconciler(store, chain, nil)
	r.SetClock(func() time.Time { return now })
	return r
}

// ---------------------------------------------------------------------------
// Scenario 1 — response lost after success
// ---------------------------------------------------------------------------

// The whole point of the feature: one logical submission produces exactly one
// chain submission, even though the response was lost and the caller has no
// idea what happened.
func TestReconcilerResolvesASuccessWhoseResponseWasLost(t *testing.T) {
	store := newMemStore()
	intent := submitOnce(t, store)

	if got := store.state(t, intent.ID); got != SubmissionPending {
		t.Fatalf("state after a lost response = %s, want pending", got)
	}

	// The chain says it landed.
	chain := &scriptedChain{status: TxStatusSuccess, view: chainAt(insideWindow)}
	newTestReconciler(store, chain, insideWindow).Tick(context.Background())

	if got := store.state(t, intent.ID); got != SubmissionLanded {
		t.Fatalf("state = %s, want landed", got)
	}

	// The reconciler only ever reads. Nothing about resolving a submission
	// causes another one.
	if chain.lookupCount() != 1 {
		t.Fatalf("chain was consulted %d times, want 1", chain.lookupCount())
	}

	// Once landed, further sweeps leave it alone — it is terminal, so it is
	// not even claimed for reconciliation again.
	newTestReconciler(store, chain, insideWindow).Tick(context.Background())
	if got := store.state(t, intent.ID); got != SubmissionLanded {
		t.Fatalf("a settled submission was reopened: state = %s", got)
	}
	if chain.lookupCount() != 1 {
		t.Fatalf("a settled submission was re-checked; lookups = %d, want 1", chain.lookupCount())
	}
}

// A duplicate request for the same logical operation must not submit again,
// whatever state the original is in.
func TestDuplicateReferenceNeverCreatesASecondSubmission(t *testing.T) {
	store := newMemStore()
	original := submitOnce(t, store)

	for i := 0; i < 5; i++ {
		stored, claimed, err := store.Claim(context.Background(), SubmissionIntent{
			IdempotencyReference: "vault-deposit-abc",
			TransactionHash:      "a1b2c3",
			ValidUntil:           validUntil,
			CreatedAt:            submittedAt,
		})
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		if claimed {
			t.Fatalf("repeat %d claimed a second submission for the same reference", i)
		}
		if stored.ID != original.ID {
			t.Fatalf("repeat %d returned submission %s, want the original %s", i, stored.ID, original.ID)
		}
	}

	if len(store.byID) != 1 {
		t.Fatalf("%d submissions exist for one logical operation, want 1", len(store.byID))
	}
}

// A reference reused for a materially different transaction is refused rather
// than silently attributed to the original.
func TestReferenceReusedForADifferentTransactionIsRefused(t *testing.T) {
	store := newMemStore()
	submitOnce(t, store)

	_, _, err := store.Claim(context.Background(), SubmissionIntent{
		IdempotencyReference: "vault-deposit-abc",
		TransactionHash:      "totally-different",
		ValidUntil:           validUntil,
		CreatedAt:            submittedAt,
	})
	if !errors.Is(err, ErrReferenceReused) {
		t.Fatalf("Claim() = %v, want ErrReferenceReused", err)
	}
}

// ---------------------------------------------------------------------------
// Scenario 2 — response lost after failure
// ---------------------------------------------------------------------------

// The ordering is what matters: no retry is permitted until the chain has
// actually reported the failure.
func TestReconcilerResolvesAFailureWhoseResponseWasLost(t *testing.T) {
	store := newMemStore()
	intent := submitOnce(t, store)

	// While the chain has not answered, the record is pending and no retry
	// is permitted.
	chain := &scriptedChain{status: TxStatusNotFound, view: chainAt(insideWindow)}
	reconciler := newTestReconciler(store, chain, insideWindow)
	reconciler.Tick(context.Background())

	if got := store.state(t, intent.ID); got != SubmissionPending {
		t.Fatalf("state before the chain answered = %s, want pending", got)
	}

	// The chain now reports the transaction landed and failed.
	chain.set(TxStatusFailed, chainAt(insideWindow), nil)
	reconciler.Tick(context.Background())

	if got := store.state(t, intent.ID); got != SubmissionRejected {
		t.Fatalf("state = %s, want rejected", got)
	}

	// Only now does a fresh attempt become permissible, and only because the
	// chain said so.
	resolved, _ := store.Get(context.Background(), intent.ID)
	outcome := DetermineOutcome(TxStatusFailed, chainAt(insideWindow), resolved)
	if !outcome.PermitsNewAttempt() {
		t.Fatal("a chain-confirmed failure did not permit a fresh attempt")
	}
}

// The same shape for a transaction that expired without ever landing.
func TestReconcilerResolvesAnExpiredSubmission(t *testing.T) {
	store := newMemStore()
	intent := submitOnce(t, store)

	chain := &scriptedChain{status: TxStatusNotFound, view: chainAt(insideWindow)}
	reconciler := newTestReconciler(store, chain, insideWindow)

	// Inside the window: still pending, however many times we look.
	for i := 0; i < 10; i++ {
		reconciler.Tick(context.Background())
	}
	if got := store.state(t, intent.ID); got != SubmissionPending {
		t.Fatalf("state inside the validity window = %s, want pending", got)
	}

	// The chain's clock passes the transaction's signed maxTime.
	chain.set(TxStatusNotFound, chainAt(pastTheWindow), nil)
	newTestReconciler(store, chain, pastTheWindow).Tick(context.Background())

	if got := store.state(t, intent.ID); got != SubmissionExpired {
		t.Fatalf("state = %s, want expired", got)
	}
}

// ---------------------------------------------------------------------------
// Scenario 3 — RPC unavailable throughout
// ---------------------------------------------------------------------------

// Neither submission nor reconciliation can reach the chain. The record must
// stay pending: not landed, not failed, and above all not resubmitted.
func TestReconcilerLeavesSubmissionPendingWhileRPCIsUnavailable(t *testing.T) {
	store := newMemStore()
	intent := submitOnce(t, store)

	chain := &scriptedChain{err: errors.New("connection refused")}
	reconciler := newTestReconciler(store, chain, pastTheWindow)

	// Sweep repeatedly, well past the transaction's validity window. An
	// unreachable RPC never becomes evidence, however long the outage lasts.
	for i := 0; i < 50; i++ {
		reconciler.Tick(context.Background())
		if got := store.state(t, intent.ID); got != SubmissionPending {
			t.Fatalf("sweep %d moved the submission to %s while the RPC was down", i, got)
		}
	}

	if chain.lookupCount() != 50 {
		t.Fatalf("chain lookups = %d, want one per sweep", chain.lookupCount())
	}

	// When the RPC comes back, the outcome is determined normally — the
	// record survived the whole outage.
	chain.set(TxStatusSuccess, chainAt(pastTheWindow), nil)
	reconciler.Tick(context.Background())

	if got := store.state(t, intent.ID); got != SubmissionLanded {
		t.Fatalf("state after recovery = %s, want landed", got)
	}
}

// An open circuit breaker is an RPC that cannot be contacted, not a statement
// about the transaction. It must be indistinguishable from any other outage
// as far as the submission record is concerned (nester#1087).
func TestOpenCircuitBreakerIsNotEvidenceAboutTheTransaction(t *testing.T) {
	store := newMemStore()
	intent := submitOnce(t, store)

	chain := &scriptedChain{err: fmt.Errorf("rpc getTransaction: %w", errors.New("circuit breaker is open"))}
	reconciler := newTestReconciler(store, chain, pastTheWindow)

	for i := 0; i < 20; i++ {
		reconciler.Tick(context.Background())
	}

	if got := store.state(t, intent.ID); got != SubmissionPending {
		t.Fatalf("an open breaker resolved the submission to %s, want pending", got)
	}
}

// ---------------------------------------------------------------------------
// Restart recovery
// ---------------------------------------------------------------------------

// No submission state lives in memory. A brand-new reconciler — standing in
// for a restarted process — picks up what the previous one left pending.
func TestPendingSubmissionsSurviveAProcessRestart(t *testing.T) {
	store := newMemStore()
	intent := submitOnce(t, store)

	// The process that submitted dies here, having learned nothing.
	chain := &scriptedChain{err: errors.New("connection refused")}
	newTestReconciler(store, chain, insideWindow).Tick(context.Background())
	if got := store.state(t, intent.ID); got != SubmissionPending {
		t.Fatalf("state = %s, want pending", got)
	}

	// A fresh reconciler, with no knowledge of the original request, resolves
	// it purely from what is in the store.
	chain.set(TxStatusSuccess, chainAt(insideWindow), nil)
	restarted := NewSubmissionReconciler(store, chain, nil)
	restarted.SetClock(func() time.Time { return insideWindow })
	restarted.Tick(context.Background())

	if got := store.state(t, intent.ID); got != SubmissionLanded {
		t.Fatalf("state after restart = %s, want landed", got)
	}
}

// ---------------------------------------------------------------------------
// Concurrency
// ---------------------------------------------------------------------------

// Concurrent duplicate requests collapse to a single submission. The
// uniqueness guarantee is the store's, not the caller's — there is no
// read-then-write in application code that a race could slip between.
func TestConcurrentDuplicateRequestsProduceOneSubmission(t *testing.T) {
	store := newMemStore()

	const callers = 32
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

			stored, claimed, err := store.Claim(context.Background(), SubmissionIntent{
				IdempotencyReference: "vault-deposit-abc",
				TransactionHash:      "a1b2c3",
				ValidUntil:           validUntil,
				CreatedAt:            submittedAt,
			})
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
		t.Fatalf("%d of %d concurrent requests claimed a submission, want exactly 1", claims, callers)
	}
	if len(ids) != 1 {
		t.Fatalf("concurrent requests saw %d distinct submissions, want 1", len(ids))
	}
	if len(store.byID) != 1 {
		t.Fatalf("%d submissions were created, want 1", len(store.byID))
	}
}

// Concurrent reconcilers must not both resolve the same submission, and the
// second must not overwrite the first's outcome.
func TestConcurrentReconcilersResolveASubmissionOnce(t *testing.T) {
	store := newMemStore()
	intent := submitOnce(t, store)

	chain := &scriptedChain{status: TxStatusSuccess, view: chainAt(insideWindow)}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			newTestReconciler(store, chain, insideWindow).Tick(context.Background())
		}()
	}
	wg.Wait()

	if got := store.state(t, intent.ID); got != SubmissionLanded {
		t.Fatalf("state = %s, want landed", got)
	}
}

// A late reconciliation must never reopen a settled submission.
func TestResolvedSubmissionsAreNotReopened(t *testing.T) {
	store := newMemStore()
	intent := submitOnce(t, store)

	if err := store.Resolve(context.Background(), intent.ID, SubmissionLanded, "landed", insideWindow); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// A stale sweep tries to record a different outcome.
	err := store.Resolve(context.Background(), intent.ID, SubmissionExpired, "stale", pastTheWindow)
	if err == nil {
		t.Fatal("a settled submission accepted a second outcome")
	}
	if got := store.state(t, intent.ID); got != SubmissionLanded {
		t.Fatalf("state = %s, want it to remain landed", got)
	}
}

// ---------------------------------------------------------------------------
// Leadership
// ---------------------------------------------------------------------------

type notLeader struct{}

func (notLeader) IsLeader() bool { return false }

// A non-leader instance does no work, so a multi-instance deployment does not
// multiply chain reads by its replica count.
func TestNonLeaderDoesNotSweep(t *testing.T) {
	store := newMemStore()
	submitOnce(t, store)

	chain := &scriptedChain{status: TxStatusSuccess, view: chainAt(insideWindow)}
	reconciler := newTestReconciler(store, chain, insideWindow)
	reconciler.SetLeaderChecker(notLeader{})

	reconciler.Tick(context.Background())

	if chain.lookupCount() != 0 {
		t.Fatalf("a non-leader made %d chain lookups, want 0", chain.lookupCount())
	}
}

// A store that cannot be read leaves everything pending rather than failing
// into a guess.
func TestReconcilerToleratesAStoreFailure(t *testing.T) {
	store := newMemStore()
	intent := submitOnce(t, store)

	chain := &scriptedChain{status: TxStatusSuccess, view: chainAt(insideWindow)}
	reconciler := newTestReconciler(store, chain, insideWindow)

	store.mu.Lock()
	store.claimErr = errors.New("database unavailable")
	store.mu.Unlock()

	reconciler.Tick(context.Background())

	if got := store.state(t, intent.ID); got != SubmissionPending {
		t.Fatalf("state = %s, want pending", got)
	}
}
