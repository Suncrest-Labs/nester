package stellar

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/backfill"
)

// fakeBackfillRepo is an in-memory backfill.Repository for unit tests that
// don't need a real Postgres instance (the full-apply/reset path is covered
// separately by the TEST_DATABASE_DSN-gated integration test).
type fakeBackfillRepo struct {
	mu   sync.Mutex
	runs map[uuid.UUID]*backfill.Run
}

func newFakeBackfillRepo() *fakeBackfillRepo {
	return &fakeBackfillRepo{runs: map[uuid.UUID]*backfill.Run{}}
}

func (r *fakeBackfillRepo) Create(_ context.Context, run *backfill.Run) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	run.Status = backfill.StatusRunning
	run.CreatedAt = time.Now()
	run.UpdatedAt = time.Now()
	cp := *run
	r.runs[run.ID] = &cp
	return nil
}

func (r *fakeBackfillRepo) GetByID(_ context.Context, id uuid.UUID) (*backfill.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[id]
	if !ok {
		return nil, backfill.ErrRunNotFound
	}
	cp := *run
	return &cp, nil
}

func (r *fakeBackfillRepo) UpdateProgress(_ context.Context, id uuid.UUID, lastLedgerDone uint64, eventsProcessed, eventsSkippedDuplicate int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[id]
	if !ok {
		return backfill.ErrRunNotFound
	}
	run.LastLedgerDone = &lastLedgerDone
	run.EventsProcessed = eventsProcessed
	run.EventsSkippedDuplicate = eventsSkippedDuplicate
	return nil
}

func (r *fakeBackfillRepo) Complete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[id]
	if !ok {
		return backfill.ErrRunNotFound
	}
	run.Status = backfill.StatusCompleted
	return nil
}

func (r *fakeBackfillRepo) Fail(_ context.Context, id uuid.UUID, errMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[id]
	if !ok {
		return backfill.ErrRunNotFound
	}
	run.Status = backfill.StatusFailed
	run.LastError = errMsg
	return nil
}

func (r *fakeBackfillRepo) List(_ context.Context, _ int) ([]backfill.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]backfill.Run, 0, len(r.runs))
	for _, run := range r.runs {
		out = append(out, *run)
	}
	return out, nil
}

// mockRPCEvent is one event the fake RPC server returns.
type mockRPCEvent struct {
	ID         string
	ContractID string
	Ledger     uint64
	EventType  string
	Data       map[string]any
}

// newMockRPCServer serves getEvents requests from a fixed in-memory event
// set, filtering by the request's startLedger the same way real Soroban RPC
// would (return events at or after startLedger, up to a page limit).
// latestLedger is always reported as the server's configured chain head.
func newMockRPCServer(t *testing.T, events []mockRPCEvent, latestLedger uint64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				StartLedger uint64 `json:"startLedger"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("mock rpc: decode request: %v", err)
		}

		type rpcEvent struct {
			ID         string         `json:"id"`
			ContractID string         `json:"contractId"`
			Ledger     uint64         `json:"ledger"`
			Topic      []string       `json:"topic"`
			Value      map[string]any `json:"value"`
		}
		var out []rpcEvent
		for _, e := range events {
			if e.Ledger < req.Params.StartLedger {
				continue
			}
			out = append(out, rpcEvent{ID: e.ID, ContractID: e.ContractID, Ledger: e.Ledger, Topic: []string{e.EventType}, Value: e.Data})
			if len(out) == 200 {
				break
			}
		}

		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      "test",
			"result": map[string]any{
				"latestLedger": latestLedger,
				"events":       out,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestRunner_Start_RejectsInvalidRange(t *testing.T) {
	r := &Runner{Repo: newFakeBackfillRepo()}
	_, err := r.Start(context.Background(), StartInput{FromLedger: 200, ToLedger: 100, ContractIDs: []string{"C1"}, InitiatedBy: "op"})
	if err == nil {
		t.Fatal("expected an error for to_ledger < from_ledger")
	}
}

func TestRunner_Start_RequiresInitiatedBy(t *testing.T) {
	r := &Runner{Repo: newFakeBackfillRepo()}
	_, err := r.Start(context.Background(), StartInput{FromLedger: 100, ToLedger: 200, ContractIDs: []string{"C1"}})
	if err == nil {
		t.Fatal("expected an error for missing initiated_by (audit requirement)")
	}
}

func TestRunner_Start_RejectsUnrecognizedMode(t *testing.T) {
	// admin_handler.go validates mode too, but cmd/backfill/main.go passes
	// backfill.Mode(*mode) straight through from an unchecked CLI flag — Start
	// is the one place both callers go through, so it must reject an unknown
	// mode itself rather than silently treating it as a plain backfill.
	r := &Runner{Repo: newFakeBackfillRepo()}
	_, err := r.Start(context.Background(), StartInput{
		FromLedger: 100, ToLedger: 200, ContractIDs: []string{"C1"}, InitiatedBy: "op", Mode: "rebild",
	})
	if err == nil {
		t.Fatal("expected an error for an unrecognized mode")
	}
}

func TestRunner_Start_DryRunReportsWithoutTouchingDB(t *testing.T) {
	srv := newMockRPCServer(t, []mockRPCEvent{
		{ID: "e1", ContractID: "C1", Ledger: 105, EventType: "pnlty_chg", Data: map[string]any{"user": "GABC", "amount": "10"}},
		{ID: "e2", ContractID: "C1", Ledger: 110, EventType: "pnlty_chg", Data: map[string]any{"user": "GABC", "amount": "5"}},
	}, 100000) // far chain head, well past the safety margin
	defer srv.Close()

	repo := newFakeBackfillRepo()
	r := &Runner{Repo: repo, Client: srv.Client(), RPCURL: srv.URL, Throttle: time.Millisecond}
	// r.DB is deliberately left nil: dry-run must never call applyIndexedEvent.

	run, err := r.Start(context.Background(), StartInput{
		FromLedger: 100, ToLedger: 150, ContractIDs: []string{"C1"}, DryRun: true, InitiatedBy: "op",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if run.Status != backfill.StatusCompleted {
		t.Errorf("status = %q, want completed", run.Status)
	}
	if run.EventsProcessed != 2 {
		t.Errorf("events_processed = %d, want 2", run.EventsProcessed)
	}
}

func TestRunner_Start_RejectsTooCloseToHead(t *testing.T) {
	srv := newMockRPCServer(t, nil, 150) // head at 150; safety margin 100 -> ledgers > 50 are off-limits
	defer srv.Close()

	repo := newFakeBackfillRepo()
	r := &Runner{Repo: repo, Client: srv.Client(), RPCURL: srv.URL}

	_, err := r.Start(context.Background(), StartInput{
		FromLedger: 100, ToLedger: 200, ContractIDs: []string{"C1"}, DryRun: true, InitiatedBy: "op",
	})
	if !errors.Is(err, ErrTooCloseToHead) {
		t.Fatalf("got %v, want ErrTooCloseToHead", err)
	}
}

func TestRunner_Start_RebuildRefusedForNonResettableEventTypes(t *testing.T) {
	srv := newMockRPCServer(t, []mockRPCEvent{
		{ID: "e1", ContractID: "C1", Ledger: 105, EventType: "deposit", Data: map[string]any{"amount": "10"}},
	}, 100000)
	defer srv.Close()

	repo := newFakeBackfillRepo()
	r := &Runner{Repo: repo, Client: srv.Client(), RPCURL: srv.URL}

	_, err := r.Start(context.Background(), StartInput{
		FromLedger: 100, ToLedger: 150, ContractIDs: []string{"C1"}, Mode: backfill.ModeRebuild, InitiatedBy: "op",
	})
	if err == nil {
		t.Fatal("expected rebuild to be refused when the range contains a deposit event")
	}
}

func TestRunner_Start_RebuildAllowedForResettableEventTypes(t *testing.T) {
	srv := newMockRPCServer(t, []mockRPCEvent{
		{ID: "e1", ContractID: "C1", Ledger: 105, EventType: "pnlty_chg", Data: map[string]any{"user": "GABC", "amount": "10"}},
	}, 100000)
	defer srv.Close()

	repo := newFakeBackfillRepo()
	r := &Runner{Repo: repo, Client: srv.Client(), RPCURL: srv.URL, Throttle: time.Millisecond}

	// Dry-run so no real DB/resetScope write path is exercised, but the
	// mode=rebuild pre-flight check still runs against the mock RPC.
	run, err := r.Start(context.Background(), StartInput{
		FromLedger: 100, ToLedger: 150, ContractIDs: []string{"C1"}, Mode: backfill.ModeRebuild, DryRun: true, InitiatedBy: "op",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if run.Status != backfill.StatusCompleted {
		t.Errorf("status = %q, want completed", run.Status)
	}
}

func TestRunner_Resume_RejectsNonRunningRun(t *testing.T) {
	repo := newFakeBackfillRepo()
	run := &backfill.Run{FromLedger: 100, ToLedger: 200, ContractIDs: []string{"C1"}, InitiatedBy: "op"}
	_ = repo.Create(context.Background(), run)
	_ = repo.Complete(context.Background(), run.ID)

	r := &Runner{Repo: repo}
	_, err := r.Resume(context.Background(), run.ID)
	if err == nil {
		t.Fatal("expected an error resuming an already-completed run")
	}
}

func TestRunner_Resume_ContinuesFromCheckpoint(t *testing.T) {
	srv := newMockRPCServer(t, []mockRPCEvent{
		{ID: "e1", ContractID: "C1", Ledger: 105, EventType: "pnlty_chg", Data: map[string]any{"user": "GABC", "amount": "10"}},
		{ID: "e2", ContractID: "C1", Ledger: 199, EventType: "pnlty_chg", Data: map[string]any{"user": "GABC", "amount": "20"}},
	}, 100000)
	defer srv.Close()

	repo := newFakeBackfillRepo()
	run := &backfill.Run{FromLedger: 100, ToLedger: 200, ContractIDs: []string{"C1"}, DryRun: true, InitiatedBy: "op"}
	_ = repo.Create(context.Background(), run)
	// Simulate a crash after ledger 150: e1 already "processed" (in a real
	// crash e1 would be committed via applyIndexedEvent's dedup; here dry
	// run never touches the DB, so this checkpoint alone is what proves
	// Resume starts from ResumeFrom() rather than FromLedger).
	checkpoint := uint64(150)
	_ = repo.UpdateProgress(context.Background(), run.ID, checkpoint, 1, 0)
	run.LastLedgerDone = &checkpoint
	run.EventsProcessed = 1

	r := &Runner{Repo: repo, Client: srv.Client(), RPCURL: srv.URL, Throttle: time.Millisecond}
	resumed, err := r.Resume(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	// Only e2 (ledger 199) should have been counted on resume, on top of
	// the 1 already recorded from before the simulated crash.
	if resumed.EventsProcessed != 2 {
		t.Errorf("events_processed = %d, want 2 (1 pre-crash + 1 on resume)", resumed.EventsProcessed)
	}
}

func TestNonResettableEventTypesInRange_ReportsDepositAndWithdraw(t *testing.T) {
	srv := newMockRPCServer(t, []mockRPCEvent{
		{ID: "e1", ContractID: "C1", Ledger: 101, EventType: "deposit", Data: map[string]any{"amount": "1"}},
		{ID: "e2", ContractID: "C1", Ledger: 102, EventType: "pnlty_chg", Data: map[string]any{"user": "G", "amount": "1"}},
		{ID: "e3", ContractID: "C1", Ledger: 103, EventType: "withdraw", Data: map[string]any{"amount": "1"}},
	}, 100000)
	defer srv.Close()

	found, err := NonResettableEventTypesInRange(context.Background(), srv.Client(), srv.URL, []string{"C1"}, 100, 110)
	if err != nil {
		t.Fatalf("NonResettableEventTypesInRange: %v", err)
	}
	got := map[string]bool{}
	for _, t := range found {
		got[t] = true
	}
	if !got["deposit"] || !got["withdraw"] {
		t.Errorf("expected deposit and withdraw flagged, got %v", found)
	}
	if got["pnlty_chg"] {
		t.Errorf("did not expect pnlty_chg (a resettable type) flagged: %v", found)
	}
}

func TestNonResettableEventTypesInRange_EmptyForFullyResettableRange(t *testing.T) {
	srv := newMockRPCServer(t, []mockRPCEvent{
		{ID: "e1", ContractID: "C1", Ledger: 101, EventType: "pnlty_chg", Data: map[string]any{"user": "G", "amount": "1"}},
		{ID: "e2", ContractID: "C1", Ledger: 102, EventType: "rebal_leg", Data: map[string]any{"source_id": "s", "delta": "1"}},
	}, 100000)
	defer srv.Close()

	found, err := NonResettableEventTypesInRange(context.Background(), srv.Client(), srv.URL, []string{"C1"}, 100, 110)
	if err != nil {
		t.Fatalf("NonResettableEventTypesInRange: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("expected no non-resettable types, got %v", found)
	}
}
