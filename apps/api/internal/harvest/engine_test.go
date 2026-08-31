package harvest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
)

type fakeVaultSource struct {
	list []VaultYield
	byID map[uuid.UUID]VaultYield
	err  error
}

func (f *fakeVaultSource) ListHarvestable(context.Context) ([]VaultYield, error) {
	return f.list, f.err
}

func (f *fakeVaultSource) GetVaultYield(_ context.Context, id uuid.UUID) (VaultYield, error) {
	v, ok := f.byID[id]
	if !ok {
		return VaultYield{}, errors.New("not found")
	}
	return v, nil
}

type fakeGas struct {
	fee       decimal.Decimal
	congested bool
	feeErr    error
	congErr   error
}

func (g *fakeGas) HarvestFee(context.Context, string) (decimal.Decimal, error) {
	return g.fee, g.feeErr
}

func (g *fakeGas) Congested(context.Context) (bool, error) { return g.congested, g.congErr }

// fakeEnqueuer records enqueues and deduplicates on idempotency key, mirroring
// the queue's behavior so idempotency can be asserted.
type fakeEnqueuer struct {
	calls []jobqueue.EnqueueInput
	seen  map[string]bool
}

func newFakeEnqueuer() *fakeEnqueuer { return &fakeEnqueuer{seen: map[string]bool{}} }

func (e *fakeEnqueuer) Enqueue(_ context.Context, in jobqueue.EnqueueInput) (jobqueue.Job, error) {
	if in.IdempotencyKey != "" && e.seen[in.IdempotencyKey] {
		return jobqueue.Job{}, nil // deduped, no new job
	}
	if in.IdempotencyKey != "" {
		e.seen[in.IdempotencyKey] = true
	}
	e.calls = append(e.calls, in)
	return jobqueue.Job{ID: uuid.New()}, nil
}

func vaultYield(accrued string) VaultYield {
	id := uuid.New()
	return VaultYield{
		VaultID:       id,
		UserID:        uuid.New(),
		WalletAddress: "GABC",
		Currency:      "USDC",
		AccruedYield:  decimal.RequireFromString(accrued),
	}
}

func newEngine(t *testing.T, vs VaultSource, gas GasOracle, q Enqueuer) *Engine {
	t.Helper()
	e := New(Config{
		Enabled:  true,
		Interval: time.Hour,
		Margin:   decimal.RequireFromString("1"),
		Window:   time.Hour,
	}, vs, gas, q, nil)
	return e
}

func TestEngine_EnqueuesWhenGatedIn(t *testing.T) {
	v := vaultYield("10") // accrued 10 > fee 2 + margin 1
	vs := &fakeVaultSource{list: []VaultYield{v}}
	q := newFakeEnqueuer()
	e := newEngine(t, vs, &fakeGas{fee: decimal.RequireFromString("2")}, q)

	e.tick(context.Background())

	if len(q.calls) != 1 {
		t.Fatalf("enqueued %d jobs, want 1", len(q.calls))
	}
	if q.calls[0].Type != DefaultJobType {
		t.Fatalf("job type = %q, want %q", q.calls[0].Type, DefaultJobType)
	}
	if q.calls[0].IdempotencyKey == "" {
		t.Fatal("expected an idempotency key on the enqueued job")
	}
}

func TestEngine_SkipsBelowThreshold(t *testing.T) {
	v := vaultYield("2") // accrued 2 < fee 2 + margin 1
	vs := &fakeVaultSource{list: []VaultYield{v}}
	q := newFakeEnqueuer()
	e := newEngine(t, vs, &fakeGas{fee: decimal.RequireFromString("2")}, q)

	e.tick(context.Background())

	if len(q.calls) != 0 {
		t.Fatalf("enqueued %d jobs, want 0", len(q.calls))
	}
}

func TestEngine_SkipsNotDueForFrequency(t *testing.T) {
	v := vaultYield("10") // would clear the economic gate...
	recent := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	v.HarvestFrequency = "daily"
	v.LastHarvestedAt = &recent
	vs := &fakeVaultSource{list: []VaultYield{v}}
	q := newFakeEnqueuer()
	e := newEngine(t, vs, &fakeGas{fee: decimal.RequireFromString("2")}, q)
	e.clock = func() time.Time { return recent.Add(2 * time.Hour) } // only 2h since last harvest

	e.tick(context.Background())

	if len(q.calls) != 0 {
		t.Fatalf("daily vault harvested 2h ago should not be due; enqueued %d jobs", len(q.calls))
	}
}

func TestEngine_HarvestsWhenFrequencyElapsed(t *testing.T) {
	v := vaultYield("10")
	past := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	v.HarvestFrequency = "daily"
	v.LastHarvestedAt = &past
	vs := &fakeVaultSource{list: []VaultYield{v}}
	q := newFakeEnqueuer()
	e := newEngine(t, vs, &fakeGas{fee: decimal.RequireFromString("2")}, q)
	e.clock = func() time.Time { return past.Add(48 * time.Hour) }

	e.tick(context.Background())

	if len(q.calls) != 1 {
		t.Fatalf("daily vault harvested 48h ago should be due; enqueued %d jobs, want 1", len(q.calls))
	}
}

func TestEngine_DefersWhenCongested(t *testing.T) {
	v := vaultYield("100")
	vs := &fakeVaultSource{list: []VaultYield{v}}
	q := newFakeEnqueuer()
	e := newEngine(t, vs, &fakeGas{fee: decimal.RequireFromString("2"), congested: true}, q)

	e.tick(context.Background())

	if len(q.calls) != 0 {
		t.Fatalf("congested network should defer; enqueued %d jobs", len(q.calls))
	}
}

func TestEngine_IdempotentAcrossTicksInWindow(t *testing.T) {
	v := vaultYield("10")
	vs := &fakeVaultSource{list: []VaultYield{v}}
	q := newFakeEnqueuer()
	e := newEngine(t, vs, &fakeGas{fee: decimal.RequireFromString("2")}, q)

	// Pin the clock so both ticks fall in the same idempotency window.
	fixed := time.Date(2026, 7, 24, 10, 30, 0, 0, time.UTC)
	e.clock = func() time.Time { return fixed }

	e.tick(context.Background())
	e.tick(context.Background())

	if len(q.calls) != 1 {
		t.Fatalf("expected a single deduped enqueue across two ticks, got %d", len(q.calls))
	}
}

func TestEngine_TriggerVault(t *testing.T) {
	v := vaultYield("10")
	vs := &fakeVaultSource{byID: map[uuid.UUID]VaultYield{v.VaultID: v}}
	q := newFakeEnqueuer()
	e := newEngine(t, vs, &fakeGas{fee: decimal.RequireFromString("2")}, q)

	ok, err := e.TriggerVault(context.Background(), v.VaultID)
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if !ok || len(q.calls) != 1 {
		t.Fatalf("event trigger should enqueue: ok=%v calls=%d", ok, len(q.calls))
	}
}

func TestEngine_VaultStatusForUser_Enforcesownership(t *testing.T) {
	v := vaultYield("2")
	vs := &fakeVaultSource{byID: map[uuid.UUID]VaultYield{v.VaultID: v}}
	q := newFakeEnqueuer()
	e := newEngine(t, vs, &fakeGas{fee: decimal.RequireFromString("2")}, q)

	// Owner sees status.
	if _, err := e.VaultStatusForUser(context.Background(), v.VaultID, v.UserID); err != nil {
		t.Fatalf("owner should see status: %v", err)
	}
	// A different user is forbidden.
	if _, err := e.VaultStatusForUser(context.Background(), v.VaultID, uuid.New()); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-owner status = %v, want ErrForbidden", err)
	}
}

func TestEngine_VaultStatus(t *testing.T) {
	v := vaultYield("2")
	v.AccrualRatePerHour = decimal.RequireFromString("1") // 1/hour
	vs := &fakeVaultSource{byID: map[uuid.UUID]VaultYield{v.VaultID: v}}
	q := newFakeEnqueuer()
	e := newEngine(t, vs, &fakeGas{fee: decimal.RequireFromString("2")}, q)

	fixed := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	e.clock = func() time.Time { return fixed }

	st, err := e.VaultStatus(context.Background(), v.VaultID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	// threshold = fee 2 + margin 1 = 3; accrued 2 => not harvestable, need 1 more at 1/hr => 1h.
	if st.Harvestable {
		t.Fatal("should not be harvestable below threshold")
	}
	if !st.Threshold.Equal(decimal.RequireFromString("3")) {
		t.Fatalf("threshold = %s, want 3", st.Threshold)
	}
	if st.EstimatedNextHarvest == nil {
		t.Fatal("expected an estimated next-harvest time")
	}
	if got := st.EstimatedNextHarvest.Sub(fixed); got != time.Hour {
		t.Fatalf("estimated next harvest in %v, want 1h", got)
	}
}
