package stellar

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/systemstate"
)

// ---------------------------------------------------------------------------
// Cold start (B-02)
// ---------------------------------------------------------------------------

// stubSysRepo is an in-memory systemstate.Repository.
//
// Cold-start behaviour is decided entirely by the cursor value and the RPC
// tip, so these tests need no database at all — which keeps them running in
// the default unit-test job rather than only where PostgreSQL is available.
type stubSysRepo struct {
	values map[string]string
	setErr error
}

func newStubSysRepo() *stubSysRepo {
	return &stubSysRepo{values: make(map[string]string)}
}

func (s *stubSysRepo) Get(_ context.Context, key string) (string, error) {
	v, ok := s.values[key]
	if !ok {
		return "", systemstate.ErrKeyNotFound
	}
	return v, nil
}

func (s *stubSysRepo) Set(_ context.Context, key, value string) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.values[key] = value
	return nil
}

// TestColdStart_DerivesValidLedgerFromTip is the B-02 regression test.
//
// A fresh deployment has no cursor. The old indexer passed startLedger=0 to
// getEvents, which the Stellar RPC rejects, so the indexer never started. The
// poller must instead derive a real ledger from the network tip.
func TestColdStart_DerivesValidLedgerFromTip(t *testing.T) {
	const tip uint64 = 493_812

	t.Run("absent cursor derives from tip", func(t *testing.T) {
		sysRepo := newStubSysRepo()
		poller := &EventPoller{SysRepo: sysRepo, Fetcher: &scriptedFetcher{tip: tip}}

		start, err := poller.resolveStartLedger(context.Background())
		if err != nil {
			t.Fatalf("resolveStartLedger: %v", err)
		}

		want := tip - DefaultColdStartOffset
		if start != want {
			t.Fatalf("start ledger = %d, want %d (tip %d - offset %d)", start, want, tip, DefaultColdStartOffset)
		}
		if start == 0 {
			t.Fatal("start ledger is 0; the Stellar RPC rejects this (B-02)")
		}
	})

	// Migration 025 seeds the cursor key with the literal value '0', so a fresh
	// database has a present-but-zero cursor rather than a missing one. Both
	// must be treated as "never indexed" — reading '0' as a real ledger is the
	// precise mechanism by which B-02 reached production.
	t.Run("seeded zero cursor is treated as cold start", func(t *testing.T) {
		sysRepo := newStubSysRepo()
		sysRepo.values[systemstate.KeyLastLedger] = "0"

		poller := &EventPoller{SysRepo: sysRepo, Fetcher: &scriptedFetcher{tip: tip}}

		start, err := poller.resolveStartLedger(context.Background())
		if err != nil {
			t.Fatalf("resolveStartLedger: %v", err)
		}
		if start != tip-DefaultColdStartOffset {
			t.Fatalf("start ledger = %d, want %d", start, tip-DefaultColdStartOffset)
		}
	})

	t.Run("configured offset is honoured", func(t *testing.T) {
		sysRepo := newStubSysRepo()
		poller := &EventPoller{SysRepo: sysRepo, Fetcher: &scriptedFetcher{tip: tip}, ColdStartOffset: 100}

		start, err := poller.resolveStartLedger(context.Background())
		if err != nil {
			t.Fatalf("resolveStartLedger: %v", err)
		}
		if start != tip-100 {
			t.Fatalf("start ledger = %d, want %d", start, tip-100)
		}
	})

	// Near genesis the offset would underflow a uint64. Clamping to 1 keeps the
	// value valid; wrapping to a huge number would be worse than starting at 0.
	t.Run("tip below offset clamps to ledger 1", func(t *testing.T) {
		sysRepo := newStubSysRepo()
		poller := &EventPoller{SysRepo: sysRepo, Fetcher: &scriptedFetcher{tip: 3}}

		start, err := poller.resolveStartLedger(context.Background())
		if err != nil {
			t.Fatalf("resolveStartLedger: %v", err)
		}
		if start != 1 {
			t.Fatalf("start ledger = %d, want 1", start)
		}
	})

	t.Run("existing cursor is resumed, not re-derived", func(t *testing.T) {
		sysRepo := newStubSysRepo()
		sysRepo.values[systemstate.KeyLastLedger] = "58101"

		fetcher := &scriptedFetcher{tip: tip}
		poller := &EventPoller{SysRepo: sysRepo, Fetcher: fetcher}

		start, err := poller.resolveStartLedger(context.Background())
		if err != nil {
			t.Fatalf("resolveStartLedger: %v", err)
		}
		if start != 58101 {
			t.Fatalf("start ledger = %d, want persisted cursor 58101", start)
		}
	})

	// A cold start that cannot reach the RPC must fail loudly. Falling back to
	// ledger 0 is what B-02 was.
	t.Run("rpc failure surfaces instead of falling back to zero", func(t *testing.T) {
		sysRepo := newStubSysRepo()
		poller := &EventPoller{
			SysRepo: sysRepo,
			Fetcher: &scriptedFetcher{tipErr: errors.New("rpc unreachable")},
		}

		if _, err := poller.resolveStartLedger(context.Background()); err == nil {
			t.Fatal("expected an error when the tip cannot be read")
		}
	})
}

// TestColdStart_NeverRequestsLedgerZero drives a full poll on an empty
// database and asserts the value actually handed to the RPC.
//
// Asserting the derived number is not quite enough: the guarantee that matters
// is that no getEvents call is ever issued with startLedger=0.
func TestColdStart_NeverRequestsLedgerZero(t *testing.T) {
	db := replayDB(t)
	freshState(t, db)

	// Remove the cursor entirely: a genuinely fresh deployment.
	if _, err := db.Exec(`DELETE FROM system_state WHERE key = $1`, systemstate.KeyLastLedger); err != nil {
		t.Fatalf("clear cursor: %v", err)
	}

	fetcher := &scriptedFetcher{events: loadFixtureEvents(t), tip: 58130}
	poller := newPoller(db, fetcher)

	if _, err := poller.PollEvents(context.Background()); err != nil {
		t.Fatalf("cold-start poll: %v", err)
	}

	if len(fetcher.fetchCalls) == 0 {
		t.Fatal("no getEvents call was made")
	}
	for i, start := range fetcher.fetchCalls {
		if start == 0 {
			t.Fatalf("getEvents call %d used startLedger=0, which the Stellar RPC rejects (B-02)", i)
		}
	}

	if got := cursorValue(t, db); got == 0 {
		t.Fatal("cursor remained 0 after a cold start")
	}
}

// ---------------------------------------------------------------------------
// Transactional cursor + balance commit
// ---------------------------------------------------------------------------

// TestIntegrationCursorAndBalanceCommitAtomically is the mandatory
// transactionality test.
//
// It forces the balance write to fail and asserts three things: the cursor did
// not move, no partial state was committed, and the event is still retryable.
// Without a shared transaction the cursor would advance while the balance
// write rolled back, and the event would be lost permanently.
//
// The failure is induced with a real database constraint rather than a mock:
// a withdrawal larger than the balance violates CHECK (current_balance >= 0),
// which is exactly how a bad write fails in production.
func TestIntegrationCursorAndBalanceCommitAtomically(t *testing.T) {
	db := replayDB(t)
	freshState(t, db)

	cursorBefore := cursorValue(t, db)

	// A withdrawal against a zero balance. The CHECK constraint rejects it.
	failing := indexedEvent{
		ID:         "evt-overdraw",
		ContractID: vaultA,
		EventType:  "withdraw",
		Ledger:     58200,
		Data:       map[string]any{"amount": json.Number("999000000000")},
	}

	applied, err := applyIndexedEventWithCursor(context.Background(), db, failing)
	if err == nil {
		t.Fatal("expected the balance write to fail against CHECK (current_balance >= 0)")
	}
	if applied {
		t.Fatal("event reported as applied despite the failure")
	}

	// 1. The cursor must not have moved.
	if got := cursorValue(t, db); got != cursorBefore {
		t.Fatalf("cursor advanced to %d despite a failed balance write (was %d); the event is now lost", got, cursorBefore)
	}

	// 2. No partial state may be committed — neither the balance change nor
	//    the processed_events row that would suppress a retry.
	assertVault(t, db, vaultA, vaultExpectation{
		totalDeposited: "0.00000000",
		currentBalance: "0.00000000",
		yieldEarned:    "0.00000000",
		status:         "active",
	})

	var processedRows int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM processed_events WHERE event_id = $1`, failing.ID,
	).Scan(&processedRows); err != nil {
		t.Fatalf("count processed events: %v", err)
	}
	if processedRows != 0 {
		t.Fatalf("processed_events has %d rows for the failed event; a retry would be suppressed and the event lost", processedRows)
	}

	// 3. The event must still be retryable. Fund the vault so the withdrawal
	//    becomes valid, then reapply the very same event.
	fundingEvent := indexedEvent{
		ID:         "evt-funding",
		ContractID: vaultA,
		EventType:  "deposit",
		Ledger:     58201,
		Data:       map[string]any{"amount": json.Number("999000000000")},
	}
	if _, err := applyIndexedEventWithCursor(context.Background(), db, fundingEvent); err != nil {
		t.Fatalf("funding deposit: %v", err)
	}

	applied, err = applyIndexedEventWithCursor(context.Background(), db, failing)
	if err != nil {
		t.Fatalf("retry of the previously failed event: %v", err)
	}
	if !applied {
		t.Fatal("retry did not apply the event; it was wrongly recorded as processed")
	}

	assertVault(t, db, vaultA, vaultExpectation{
		totalDeposited: "999000000000.00000000",
		currentBalance: "0.00000000",
		yieldEarned:    "0.00000000",
		status:         "active",
	})
}

// TestIntegrationPollStopsAtFailedEvent proves the poller does not step over a
// failing event.
//
// Skipping a failure while later events advance the cursor would strand the
// failed event behind the cursor forever — silent, permanent loss.
func TestIntegrationPollStopsAtFailedEvent(t *testing.T) {
	db := replayDB(t)
	freshState(t, db)

	events := []indexedEvent{
		{
			ID: "evt-ok-1", ContractID: vaultA, EventType: "deposit", Ledger: 58101,
			Data: map[string]any{"amount": json.Number("100000000")},
		},
		{
			// Overdraws vaultB, which has a zero balance.
			ID: "evt-bad", ContractID: vaultB, EventType: "withdraw", Ledger: 58102,
			Data: map[string]any{"amount": json.Number("500000000")},
		},
		{
			ID: "evt-ok-2", ContractID: vaultA, EventType: "deposit", Ledger: 58103,
			Data: map[string]any{"amount": json.Number("200000000")},
		},
	}

	poller := newPoller(db, &scriptedFetcher{events: events, tip: 58130})

	if _, err := poller.PollEvents(context.Background()); err == nil {
		t.Fatal("expected the poll to surface the failing event")
	}

	// The cursor must sit at the last successful event, not past the failure.
	if got := cursorValue(t, db); got != 58101 {
		t.Fatalf("cursor = %d, want 58101 (the last successfully applied ledger)", got)
	}

	// The event after the failure must not have been applied.
	var processed int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM processed_events WHERE event_id = $1`, "evt-ok-2",
	).Scan(&processed); err != nil {
		t.Fatalf("count: %v", err)
	}
	if processed != 0 {
		t.Fatal("an event after the failure was applied; the failed event is now stranded behind the cursor")
	}
}

// ---------------------------------------------------------------------------
// Ordering
// ---------------------------------------------------------------------------

// TestSortEventsForApply_EnforcesLedgerOrder documents and locks in the
// ordering contract: ledger order is enforced regardless of delivery order,
// and ties within a ledger resolve deterministically by event ID.
func TestSortEventsForApply_EnforcesLedgerOrder(t *testing.T) {
	events := []indexedEvent{
		{ID: "c", Ledger: 30},
		{ID: "a", Ledger: 10},
		{ID: "b", Ledger: 20},
		{ID: "a2", Ledger: 10},
	}

	sortEventsForApply(events)

	var got []string
	var ledgers []uint64
	for _, e := range events {
		got = append(got, e.ID)
		ledgers = append(ledgers, e.Ledger)
	}

	for i := 1; i < len(ledgers); i++ {
		if ledgers[i] < ledgers[i-1] {
			t.Fatalf("events not in ledger order: %v", ledgers)
		}
	}
	if got[0] != "a" || got[1] != "a2" {
		t.Fatalf("within-ledger tie broken non-deterministically: %v", got)
	}
}

// ---------------------------------------------------------------------------
// Integer precision (B-11)
// ---------------------------------------------------------------------------

// TestIntegrationLargeAmountRoundTripsExactly proves a large i128 stroop
// amount survives parsing, arithmetic, persistence, and read-back with no
// precision loss.
//
// 1e18 and the odd 17-digit values below are all above float64's 2^53
// exact-integer limit, so any float64 anywhere in this path corrupts them.
func TestIntegrationLargeAmountRoundTripsExactly(t *testing.T) {
	db := replayDB(t)

	cases := []struct {
		name   string
		amount string
	}{
		{"one quintillion stroops", "1000000000000000000"},
		{"odd value above 2^53", "90071992547409931"},
		{"max-ish i64 stroops", "9223372036854775807"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			freshState(t, db)

			event := indexedEvent{
				ID:         "evt-precision-" + tc.amount,
				ContractID: vaultA,
				EventType:  "deposit",
				Ledger:     58150,
				Data:       map[string]any{"amount": json.Number(tc.amount)},
			}

			if _, err := applyIndexedEventWithCursor(context.Background(), db, event); err != nil {
				t.Fatalf("apply deposit: %v", err)
			}

			var stored string
			if err := db.QueryRow(
				`SELECT total_deposited::text FROM vaults WHERE contract_address = $1`, vaultA,
			).Scan(&stored); err != nil {
				t.Fatalf("read balance: %v", err)
			}

			want := tc.amount + ".00000000"
			if stored != want {
				t.Fatalf("amount lost precision: stored %s, want %s", stored, want)
			}
		})
	}
}

// TestAmountPathHasNoFloat64 is a source-level guard.
//
// The acceptance criterion is not merely that the precision test passes today,
// but that no float64 conversion is reintroduced into the amount path later.
// The extractor keeps an explicit `case float64:` that REJECTS unsafe values —
// that is a guard, not a conversion — so this checks that no arithmetic or
// cast turns an amount into a float64.
func TestAmountPathHasNoFloat64(t *testing.T) {
	for _, file := range []string{"indexer.go", "poller.go", "fetcher.go"} {
		// #nosec G304 -- test-only: fixed in-package source file names.
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}

		for i, line := range strings.Split(string(src), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			// float64(...) conversions are how precision is silently lost.
			// The one legitimate use is the rejection guard's own bound check.
			if strings.Contains(trimmed, "float64(") && !strings.Contains(trimmed, "float64(1<<53)") {
				t.Errorf("%s:%d reintroduces a float64 conversion in the amount path: %s", file, i+1, trimmed)
			}
			if strings.Contains(trimmed, ".Float64()") || strings.Contains(trimmed, "ParseFloat") {
				t.Errorf("%s:%d converts an amount through float64: %s", file, i+1, trimmed)
			}
		}
	}
}

// TestExtractEventAmount_RejectsUnsafeFloat64 keeps the rejection guard
// honest: an amount that arrives as a float64 beyond the exact-integer range
// has already lost precision before the indexer saw it, so writing it would
// store a wrong balance. Rejecting surfaces the problem instead.
func TestExtractEventAmount_RejectsUnsafeFloat64(t *testing.T) {
	if _, ok := extractEventAmount(indexedEvent{
		Data: map[string]any{"amount": float64(1e18)},
	}); ok {
		t.Fatal("a float64 amount above 2^53 was accepted; it has already lost precision (B-11)")
	}

	got, ok := extractEventAmount(indexedEvent{
		Data: map[string]any{"amount": json.Number("1000000000000000000")},
	})
	if !ok {
		t.Fatal("a json.Number amount was rejected")
	}
	if got.String() != "1000000000000000000" {
		t.Fatalf("json.Number amount = %s, want exact 1000000000000000000", got.String())
	}
}

// ---------------------------------------------------------------------------
// Poller wiring
// ---------------------------------------------------------------------------

func TestPollEvents_RequiresDependencies(t *testing.T) {
	cases := []struct {
		name   string
		poller *EventPoller
	}{
		{"missing db", &EventPoller{SysRepo: newStubSysRepo(), Fetcher: &scriptedFetcher{}}},
		{"missing sys repo", &EventPoller{DB: &sql.DB{}, Fetcher: &scriptedFetcher{}}},
		{"missing fetcher", &EventPoller{DB: &sql.DB{}, SysRepo: newStubSysRepo()}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.poller.PollEvents(context.Background()); err == nil {
				t.Fatal("expected a configuration error")
			}
		})
	}
}

// TestIntegrationPollEvents_AdvancesCursorOnEmptyRange proves the cursor does
// not stall when a poll returns no events: a quiet network must still move the
// cursor forward, otherwise every later poll re-scans from an ever-staler
// ledger.
func TestIntegrationPollEvents_AdvancesCursorOnEmptyRange(t *testing.T) {
	db := replayDB(t)
	freshState(t, db)

	const tip uint64 = 59000
	poller := newPoller(db, &scriptedFetcher{events: nil, tip: tip})

	if _, err := poller.PollEvents(context.Background()); err != nil {
		t.Fatalf("poll: %v", err)
	}

	if got := cursorValue(t, db); got != tip {
		t.Fatalf("cursor = %d, want tip %d on an empty range", got, tip)
	}
}
