package stellar

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/systemstate"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
	"github.com/suncrestlabs/nester/apps/api/internal/testutil"
)

// The replay harness (issue #1051).
//
// It proves one invariant: the same logical event stream always produces the
// same final state, no matter how that stream is delivered. The four delivery
// variations tested are clean single pass, restart mid-stream, duplicate
// delivery, and shuffled-within-ledger delivery.
//
// Everything here drives the real production path — EventPoller.PollEvents,
// applyEventMutation, the real SQL, the real processed_events constraint —
// against a real PostgreSQL database. A harness built on mocks would prove
// only that the mocks agree with each other.

const (
	// replayFixture is a recorded getEvents response. It is decoded by the
	// production parser, not by a test-only decoder, so a change to the wire
	// format breaks these tests rather than silently bypassing them.
	replayFixture = "testdata/replay_events.json"

	// vaultA and vaultB are the contract addresses in the fixture.
	vaultA = "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC"
	vaultB = "CBQHNAXSI55GX2GN6D67GK7BHVPSLJUGZQEU7WJ5LKR5PNUCGLIMAO4K"

	// fixtureTip is result.latestLedger in the fixture.
	fixtureTip uint64 = 58130
)

// ---------------------------------------------------------------------------
// Fixture loading
// ---------------------------------------------------------------------------

// loadFixtureEvents decodes the recorded RPC response through the same
// json.Decoder settings the production fetcher uses (UseNumber), so amounts
// arrive as json.Number exactly as they would from a live RPC. This is what
// makes the precision assertions meaningful: if the fixture were decoded into
// float64 here, a float64 regression in production would not be caught.
func loadFixtureEvents(t *testing.T) []indexedEvent {
	t.Helper()

	// #nosec G304 -- test-only: a fixed in-repo fixture path, not user input.
	raw, err := os.ReadFile(replayFixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var payload struct {
		Result struct {
			LatestLedger uint64 `json:"latestLedger"`
			Events       []struct {
				ID         string         `json:"id"`
				ContractID string         `json:"contractId"`
				Ledger     uint64         `json:"ledger"`
				Topic      []any          `json:"topic"`
				Value      map[string]any `json:"value"`
			} `json:"events"`
		} `json:"result"`
	}

	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(payload.Result.Events) == 0 {
		t.Fatal("fixture contains no events")
	}

	events := make([]indexedEvent, 0, len(payload.Result.Events))
	for _, raw := range payload.Result.Events {
		eventType := ""
		if len(raw.Topic) > 0 {
			if topic, ok := raw.Topic[0].(string); ok {
				eventType = topic
			}
		}
		if eventType == "" {
			continue
		}
		events = append(events, indexedEvent{
			ID:         raw.ID,
			ContractID: raw.ContractID,
			EventType:  eventType,
			Ledger:     raw.Ledger,
			Data:       raw.Value,
		})
	}
	return events
}

// ---------------------------------------------------------------------------
// Scripted fetcher
// ---------------------------------------------------------------------------

// scriptedFetcher serves recorded events without network access.
//
// It honours startLedger the way the real RPC does — returning only events at
// or after the cursor — which is what allows the restart scenario to exercise
// genuine resume semantics rather than simply replaying a shorter slice.
type scriptedFetcher struct {
	events []indexedEvent
	tip    uint64

	// tipErr, when set, makes LatestLedger fail. Used to prove cold start
	// surfaces RPC failures instead of falling back to ledger 0.
	tipErr error

	// fetchCalls records the startLedger of every FetchEvents call, so tests
	// can assert the poller never asks the RPC for ledger 0 (B-02).
	fetchCalls []uint64
}

func (f *scriptedFetcher) FetchEvents(_ context.Context, _ []string, startLedger uint64) ([]indexedEvent, uint64, error) {
	f.fetchCalls = append(f.fetchCalls, startLedger)

	out := make([]indexedEvent, 0, len(f.events))
	for _, e := range f.events {
		if e.Ledger >= startLedger {
			out = append(out, e)
		}
	}
	return out, f.tip, nil
}

func (f *scriptedFetcher) LatestLedger(_ context.Context) (uint64, error) {
	if f.tipErr != nil {
		return 0, f.tipErr
	}
	return f.tip, nil
}

// failingDB wraps the scripted fetcher for the interrupted-run scenario: it
// serves only the first n events, simulating a process killed mid-stream with
// the remainder never delivered.
func (f *scriptedFetcher) truncatedTo(n int) *scriptedFetcher {
	limited := make([]indexedEvent, 0, n)
	ordered := append([]indexedEvent(nil), f.events...)
	sortEventsForApply(ordered)
	if n > len(ordered) {
		n = len(ordered)
	}
	limited = append(limited, ordered[:n]...)
	return &scriptedFetcher{events: limited, tip: f.tip}
}

// ---------------------------------------------------------------------------
// Database setup
// ---------------------------------------------------------------------------

func replayDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if strings.TrimSpace(dsn) == "" {
		t.Skip("TEST_DATABASE_DSN is not set; skipping replay harness")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.Ping(); err != nil {
		t.Fatalf("ping test db: %v", err)
	}
	return db
}

// freshState wipes the database, applies the full migration chain, and seeds
// the two vaults the fixture emits events for.
//
// Applying every migration (rather than a hand-picked subset) means the schema
// under test is the schema production runs against — including migration 103,
// which widens the balance columns so i128 stroop amounts fit.
func freshState(t *testing.T, db *sql.DB) {
	t.Helper()

	testutil.ApplyAllMigrations(t, db, "../../migrations")

	userID := "11111111-1111-4111-8111-111111111111"
	if _, err := db.Exec(
		`INSERT INTO users (id, wallet_address, display_name) VALUES ($1, $2, $3)`,
		userID, "GAKX7VYAJTQOZ4ZKQ7GCTAWOAKCXY6BFCHOEQ4RRWJBTAY3EKQXHTKZL", "replay-user",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	seedVault := func(id, contract string) {
		if _, err := db.Exec(`
INSERT INTO vaults (id, user_id, contract_address, currency, status, total_deposited, current_balance, yield_earned)
VALUES ($1, $2, $3, 'USDC', 'active', 0, 0, 0)`,
			id, userID, contract,
		); err != nil {
			t.Fatalf("seed vault %s: %v", contract, err)
		}
	}
	seedVault("22222222-2222-4222-8222-222222222222", vaultA)
	seedVault("33333333-3333-4333-8333-333333333333", vaultB)

	// Seed the cursor just below the fixture's first ledger so the replay
	// scenarios start from a known point and exercise resume behaviour rather
	// than cold-start tip derivation. Cold start is a distinct concern and is
	// covered by its own test, which asserts the tip-offset behaviour directly.
	if _, err := db.Exec(
		`INSERT INTO system_state (key, value, updated_at) VALUES ($1, $2, NOW())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
		systemstate.KeyLastLedger, "58100",
	); err != nil {
		t.Fatalf("seed cursor: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Canonical state snapshot
// ---------------------------------------------------------------------------

// vaultState is the full balance state of one vault.
//
// Every mutated dimension is captured, not just a single total: comparing only
// current_balance would let a corrupted total_deposited or yield_earned pass
// unnoticed, which is exactly the silent-corruption failure this issue exists
// to rule out.
type vaultState struct {
	ContractAddress string `json:"contract_address"`
	TotalDeposited  string `json:"total_deposited"`
	CurrentBalance  string `json:"current_balance"`
	YieldEarned     string `json:"yield_earned"`
	Status          string `json:"status"`
}

// canonicalState renders the complete vault state as deterministic JSON.
//
// The result is compared byte-for-byte across scenarios. Numeric values are
// read as strings so PostgreSQL's exact NUMERIC representation is preserved
// end to end — converting through float64 here would defeat the precision
// assertions the fixture is designed to make.
func canonicalState(t *testing.T, db *sql.DB) string {
	t.Helper()

	rows, err := db.Query(`
SELECT contract_address, total_deposited::text, current_balance::text, yield_earned::text, status
FROM vaults
ORDER BY contract_address`)
	if err != nil {
		t.Fatalf("query vault state: %v", err)
	}
	defer func() { _ = rows.Close() }()

	states := make([]vaultState, 0, 2)
	for rows.Next() {
		var s vaultState
		if err := rows.Scan(&s.ContractAddress, &s.TotalDeposited, &s.CurrentBalance, &s.YieldEarned, &s.Status); err != nil {
			t.Fatalf("scan vault state: %v", err)
		}
		states = append(states, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate vault state: %v", err)
	}

	encoded, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		t.Fatalf("encode vault state: %v", err)
	}
	return string(encoded)
}

func cursorValue(t *testing.T, db *sql.DB) uint64 {
	t.Helper()

	var raw string
	err := db.QueryRow(`SELECT value FROM system_state WHERE key = $1`, systemstate.KeyLastLedger).Scan(&raw)
	if err != nil {
		t.Fatalf("read cursor: %v", err)
	}
	var ledger uint64
	if _, err := fmt.Sscanf(raw, "%d", &ledger); err != nil {
		t.Fatalf("parse cursor %q: %v", raw, err)
	}
	return ledger
}

func newPoller(db *sql.DB, fetcher EventFetcher) *EventPoller {
	return &EventPoller{
		DB:      db,
		SysRepo: postgres.NewSystemStateRepository(db),
		Fetcher: fetcher,
	}
}

// runToCompletion polls until no further events are applied, which is how the
// production loop converges. It is bounded so a non-converging implementation
// fails the test instead of hanging.
func runToCompletion(t *testing.T, poller *EventPoller) {
	t.Helper()

	for i := 0; i < 10; i++ {
		applied, err := poller.PollEvents(context.Background())
		if err != nil {
			t.Fatalf("poll: %v", err)
		}
		if applied == 0 {
			return
		}
	}
	t.Fatal("poller did not converge within 10 passes")
}

// ---------------------------------------------------------------------------
// Scenario A — clean single pass
// ---------------------------------------------------------------------------

func TestIntegrationReplay_CleanSinglePass(t *testing.T) {
	db := replayDB(t)
	freshState(t, db)

	events := loadFixtureEvents(t)
	poller := newPoller(db, &scriptedFetcher{events: events, tip: fixtureTip})

	runToCompletion(t, poller)

	state := canonicalState(t, db)
	t.Logf("clean single-pass state:\n%s", state)

	// Balances are asserted against amounts computed by hand from the fixture,
	// not against whatever the code produced. A test that only compared runs to
	// each other would pass happily on a uniformly wrong implementation.
	//
	// Amounts are stored as emitted: the indexer persists raw contract amounts
	// and does not rescale stroops into display units anywhere in this path.
	//
	// Vault A: deposits 25000000000 + 12500000000 + 750000000 = 38250000000,
	//          harvest 375000000, withdraw 5000000000.
	//          current_balance = 38250000000 + 375000000 - 5000000000.
	// Vault B: deposit 1e18, harvest 2.5e17, withdraw 5e17 — every one of
	//          these is far above float64's 2^53 exact-integer limit, so the
	//          exact totals below are the B-11 precision evidence.
	assertVault(t, db, vaultA, vaultExpectation{
		totalDeposited: "38250000000.00000000",
		currentBalance: "33625000000.00000000",
		yieldEarned:    "375000000.00000000",
		status:         "active",
	})
	assertVault(t, db, vaultB, vaultExpectation{
		totalDeposited: "1000000000000000000.00000000",
		currentBalance: "750000000000000000.00000000",
		yieldEarned:    "250000000000000000.00000000",
		status:         "active",
	})

	// The cursor rests at the last applied event's ledger, not at the network
	// tip. Advancing past the newest event the poller actually committed would
	// mean trusting that the batch was complete, and a wrong guess there loses
	// events silently. Re-reading a few already-processed ledgers is free,
	// because processed_events deduplicates them.
	const lastEventLedger uint64 = 58125
	if got := cursorValue(t, db); got != lastEventLedger {
		t.Fatalf("cursor = %d, want last applied ledger %d", got, lastEventLedger)
	}
}

type vaultExpectation struct {
	totalDeposited string
	currentBalance string
	yieldEarned    string
	status         string
}

func assertVault(t *testing.T, db *sql.DB, contract string, want vaultExpectation) {
	t.Helper()

	var got vaultExpectation
	err := db.QueryRow(`
SELECT total_deposited::text, current_balance::text, yield_earned::text, status
FROM vaults WHERE contract_address = $1`, contract).
		Scan(&got.totalDeposited, &got.currentBalance, &got.yieldEarned, &got.status)
	if err != nil {
		t.Fatalf("read vault %s: %v", contract, err)
	}

	if got != want {
		t.Fatalf("vault %s state mismatch:\n got: %+v\nwant: %+v", contract, got, want)
	}
}

// ---------------------------------------------------------------------------
// Scenario B — restart mid-stream (B-01)
// ---------------------------------------------------------------------------

// TestIntegrationReplay_RestartMidStream proves that killing the indexer
// mid-stream and resuming from the persisted cursor produces exactly the state
// a clean run produces — no event dropped, and critically no event applied
// twice.
//
// This is the regression test for B-01: with an in-memory cursor, the resumed
// poller would restart from zero and re-apply every event before the
// interruption, doubling the balances.
func TestIntegrationReplay_RestartMidStream(t *testing.T) {
	db := replayDB(t)
	events := loadFixtureEvents(t)

	// Reference run.
	freshState(t, db)
	runToCompletion(t, newPoller(db, &scriptedFetcher{events: events, tip: fixtureTip}))
	want := canonicalState(t, db)

	// Interrupted run: a fresh database, and a fetcher that delivers only the
	// first 4 events before the process "dies".
	freshState(t, db)
	full := &scriptedFetcher{events: events, tip: fixtureTip}
	partial := full.truncatedTo(4)

	firstPoller := newPoller(db, partial)
	if _, err := firstPoller.PollEvents(context.Background()); err != nil {
		t.Fatalf("interrupted poll: %v", err)
	}

	cursorAfterInterrupt := cursorValue(t, db)
	if cursorAfterInterrupt == 0 {
		t.Fatal("cursor was not persisted before the interruption; a restart would replay from zero (B-01)")
	}

	midState := canonicalState(t, db)
	if midState == want {
		t.Fatal("interrupted run already reached the final state; the restart scenario proves nothing")
	}

	// The interrupted poller is discarded entirely. The resumed poller is a new
	// instance sharing no memory with it, so the only thing carrying progress
	// across the "restart" is the persisted cursor.
	resumed := newPoller(db, full)
	runToCompletion(t, resumed)

	got := canonicalState(t, db)
	if got != want {
		t.Fatalf("restart produced different state than clean run:\n got:\n%s\nwant:\n%s", got, want)
	}

	// Every event applied exactly once, proven independently of balances.
	assertProcessedOnce(t, db, len(events))
}

// assertProcessedOnce verifies each fixture event has exactly one
// processed_events row, which is the direct evidence that nothing was applied
// twice.
func assertProcessedOnce(t *testing.T, db *sql.DB, wantCount int) {
	t.Helper()

	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM processed_events`).Scan(&total); err != nil {
		t.Fatalf("count processed events: %v", err)
	}
	if total != wantCount {
		t.Fatalf("processed_events has %d rows, want %d", total, wantCount)
	}
}

// ---------------------------------------------------------------------------
// Scenario C — duplicate delivery (B-03 in the issue's numbering)
// ---------------------------------------------------------------------------

// TestIntegrationReplay_DuplicateDelivery proves that delivering every event
// twice leaves the final state identical to a single delivery.
//
// The deduplication is enforced by the processed_events primary key inside the
// same transaction as the balance mutation, so this holds under the real
// database constraint rather than an application-level pre-check that could
// race.
func TestIntegrationReplay_DuplicateDelivery(t *testing.T) {
	db := replayDB(t)
	events := loadFixtureEvents(t)

	freshState(t, db)
	runToCompletion(t, newPoller(db, &scriptedFetcher{events: events, tip: fixtureTip}))
	want := canonicalState(t, db)

	freshState(t, db)
	doubled := make([]indexedEvent, 0, len(events)*2)
	doubled = append(doubled, events...)
	doubled = append(doubled, events...)

	poller := newPoller(db, &scriptedFetcher{events: doubled, tip: fixtureTip})
	runToCompletion(t, poller)

	got := canonicalState(t, db)
	if got != want {
		t.Fatalf("duplicate delivery corrupted state:\n got:\n%s\nwant:\n%s", got, want)
	}

	// Duplicates must not create extra processed_events rows either.
	assertProcessedOnce(t, db, len(events))
}

// TestIntegrationReplay_RepeatedFullReplay is the strongest idempotency
// statement: re-running the entire stream from ledger zero, repeatedly, never
// changes the state. This is the scenario an operator triggers by accident
// when they reset a cursor or re-run a backfill.
func TestIntegrationReplay_RepeatedFullReplay(t *testing.T) {
	db := replayDB(t)
	events := loadFixtureEvents(t)

	freshState(t, db)
	runToCompletion(t, newPoller(db, &scriptedFetcher{events: events, tip: fixtureTip}))
	want := canonicalState(t, db)

	for pass := 0; pass < 3; pass++ {
		// Force a full re-delivery by rewinding the cursor, exactly as a
		// botched operational reset would.
		if _, err := db.Exec(
			`UPDATE system_state SET value = '1' WHERE key = $1`, systemstate.KeyLastLedger,
		); err != nil {
			t.Fatalf("rewind cursor: %v", err)
		}

		runToCompletion(t, newPoller(db, &scriptedFetcher{events: events, tip: fixtureTip}))

		if got := canonicalState(t, db); got != want {
			t.Fatalf("full replay pass %d changed state:\n got:\n%s\nwant:\n%s", pass, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Scenario D — out-of-order delivery within a ledger
// ---------------------------------------------------------------------------

// TestIntegrationReplay_OutOfOrderWithinLedger proves that shuffling events
// inside a ledger boundary does not change the final state.
//
// Ordering guarantees, stated explicitly:
//
//   - ACROSS ledgers, order is REQUIRED and ENFORCED. The cursor advances per
//     event, so applying a higher ledger before a lower one would strand the
//     lower event behind the cursor. sortEventsForApply enforces ledger order
//     regardless of the order the RPC delivered events in.
//   - WITHIN a ledger, order is NOT required. Every balance mutation is an
//     additive or subtractive delta on the vault row, and addition commutes, so
//     any permutation within a ledger converges on the same totals.
//
// The one boundary worth naming: current_balance carries a CHECK (>= 0)
// constraint, so a permutation that moves a withdrawal ahead of the deposit
// funding it can fail the constraint rather than corrupt the balance. Failing
// loudly is the correct outcome, and the poller stops on error rather than
// skipping the event, so the next pass retries it in a valid order. The
// fixture keeps each ledger's events individually satisfiable so the
// commutativity claim is tested rather than the constraint.
func TestIntegrationReplay_OutOfOrderWithinLedger(t *testing.T) {
	db := replayDB(t)
	events := loadFixtureEvents(t)

	freshState(t, db)
	runToCompletion(t, newPoller(db, &scriptedFetcher{events: events, tip: fixtureTip}))
	want := canonicalState(t, db)

	// Deterministic permutations: no RNG, so a failure always reproduces.
	permutations := []func([]indexedEvent) []indexedEvent{
		reverseWithinLedger,
		rotateWithinLedger,
	}

	for i, permute := range permutations {
		freshState(t, db)

		shuffled := permute(append([]indexedEvent(nil), events...))
		assertSameLedgerGrouping(t, events, shuffled)

		runToCompletion(t, newPoller(db, &scriptedFetcher{events: shuffled, tip: fixtureTip}))

		if got := canonicalState(t, db); got != want {
			t.Fatalf("permutation %d changed final state:\n got:\n%s\nwant:\n%s", i, got, want)
		}
	}
}

// reverseWithinLedger reverses the event order inside each ledger, leaving
// ledger order itself intact.
func reverseWithinLedger(events []indexedEvent) []indexedEvent {
	byLedger := groupByLedger(events)
	out := make([]indexedEvent, 0, len(events))
	for _, ledger := range sortedLedgers(byLedger) {
		group := byLedger[ledger]
		for i := len(group) - 1; i >= 0; i-- {
			out = append(out, group[i])
		}
	}
	return out
}

// rotateWithinLedger rotates each ledger's events by one position.
func rotateWithinLedger(events []indexedEvent) []indexedEvent {
	byLedger := groupByLedger(events)
	out := make([]indexedEvent, 0, len(events))
	for _, ledger := range sortedLedgers(byLedger) {
		group := byLedger[ledger]
		if len(group) > 1 {
			group = append(append([]indexedEvent(nil), group[1:]...), group[0])
		}
		out = append(out, group...)
	}
	return out
}

func groupByLedger(events []indexedEvent) map[uint64][]indexedEvent {
	byLedger := make(map[uint64][]indexedEvent)
	for _, e := range events {
		byLedger[e.Ledger] = append(byLedger[e.Ledger], e)
	}
	return byLedger
}

func sortedLedgers(byLedger map[uint64][]indexedEvent) []uint64 {
	ledgers := make([]uint64, 0, len(byLedger))
	for ledger := range byLedger {
		ledgers = append(ledgers, ledger)
	}
	sort.Slice(ledgers, func(i, j int) bool { return ledgers[i] < ledgers[j] })
	return ledgers
}

// assertSameLedgerGrouping guards the test itself: a permutation that moved an
// event to a different ledger would be testing cross-ledger reordering, which
// is a different (and unsupported) claim.
func assertSameLedgerGrouping(t *testing.T, original, permuted []indexedEvent) {
	t.Helper()

	if len(original) != len(permuted) {
		t.Fatalf("permutation changed event count: %d -> %d", len(original), len(permuted))
	}

	ledgerOf := make(map[string]uint64, len(original))
	for _, e := range original {
		ledgerOf[e.ID] = e.Ledger
	}
	for _, e := range permuted {
		if ledgerOf[e.ID] != e.Ledger {
			t.Fatalf("permutation moved event %s across a ledger boundary", e.ID)
		}
	}
}
