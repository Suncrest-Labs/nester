package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/scheduler"
)

// fakeVaultRepo implements the tiny slice of vault.Repository this file's
// tests exercise; every other method panics if called, so a test that
// unexpectedly reaches one fails loudly instead of returning a zero value.
type fakeVaultRepo struct {
	vault.Repository
	vaults []vault.Vault
}

func (f *fakeVaultRepo) GetVault(_ context.Context, id uuid.UUID) (vault.Vault, error) {
	for _, v := range f.vaults {
		if v.ID == id {
			return v, nil
		}
	}
	return vault.Vault{}, vault.ErrVaultNotFound
}

func (f *fakeVaultRepo) ListVaults(_ context.Context, filter vault.ListFilter) ([]vault.Vault, int, error) {
	return f.vaults, len(f.vaults), nil
}

// fakeYields returns a fixed registry, or an error/empty slice when configured to.
type fakeYields struct {
	yields []scheduler.ProtocolYield
	err    error
}

func (f *fakeYields) FetchAPYRegistry(_ context.Context) ([]scheduler.ProtocolYield, error) {
	return f.yields, f.err
}

// fakeEnqueuer records every EnqueueJSON call so tests can assert on job
// type, payload, and idempotency key without a real queue repository.
type fakeEnqueuer struct {
	calls []fakeEnqueueCall
	err   error
}

type fakeEnqueueCall struct {
	jobType        string
	payload        any
	idempotencyKey string
	correlationID  string
}

func (f *fakeEnqueuer) EnqueueJSON(_ context.Context, jobType string, payload any, opts ...jobqueue.EnqueueOption) (jobqueue.Job, error) {
	if f.err != nil {
		return jobqueue.Job{}, f.err
	}
	in := jobqueue.EnqueueInput{}
	for _, opt := range opts {
		opt(&in)
	}
	f.calls = append(f.calls, fakeEnqueueCall{jobType: jobType, payload: payload, idempotencyKey: in.IdempotencyKey, correlationID: in.CorrelationID})
	return jobqueue.Job{ID: uuid.New(), Type: jobType, CorrelationID: in.CorrelationID}, nil
}

func apyYield(protocol string, apyPercent float64) scheduler.ProtocolYield {
	return scheduler.ProtocolYield{Protocol: protocol, APY: decimalFromPercent(apyPercent)}
}

func vaultWithAllocation(id uuid.UUID, protocol string, amount int64) vault.Vault {
	return vault.Vault{
		ID:     id,
		Status: vault.StatusActive,
		Allocations: []vault.Allocation{
			{Protocol: protocol, Amount: decimal.NewFromInt(amount), APY: decimalFromPercent(1)},
		},
	}
}

func TestGetDriftState_ReturnsRebalanceWhenDriftExceedsThreshold(t *testing.T) {
	vaultID := uuid.New()
	repo := &fakeVaultRepo{vaults: []vault.Vault{
		vaultWithAllocation(vaultID, "protocol-a", 1000),
	}}
	yields := &fakeYields{yields: []scheduler.ProtocolYield{
		apyYield("protocol-a", 1),
		apyYield("protocol-b", 10), // 9pp = 900bps of gain, clears any sane threshold
	}}
	d := NewAPYDriftDetector(repo, yields, nil, 200, 0, nil)

	state, err := d.GetDriftState(context.Background(), vaultID)
	if err != nil {
		t.Fatalf("GetDriftState: %v", err)
	}
	if !state.ExceedsThreshold {
		t.Fatalf("expected ExceedsThreshold=true, got state=%+v", state)
	}
	if state.OptimalProtocol != "protocol-b" {
		t.Errorf("expected optimal protocol-b, got %q", state.OptimalProtocol)
	}
	if state.ThresholdBPS != 200 {
		t.Errorf("expected threshold 200bps echoed back, got %d", state.ThresholdBPS)
	}
}

func TestGetDriftState_NoActionWhenAlreadyOptimal(t *testing.T) {
	vaultID := uuid.New()
	repo := &fakeVaultRepo{vaults: []vault.Vault{
		vaultWithAllocation(vaultID, "protocol-a", 1000),
	}}
	yields := &fakeYields{yields: []scheduler.ProtocolYield{
		apyYield("protocol-a", 10), // already the best
	}}
	d := NewAPYDriftDetector(repo, yields, nil, 200, 0, nil)

	state, err := d.GetDriftState(context.Background(), vaultID)
	if err != nil {
		t.Fatalf("GetDriftState: %v", err)
	}
	if state.ExceedsThreshold {
		t.Fatalf("expected no drift when already optimal, got state=%+v", state)
	}
	if state.Reason != "already_optimal" {
		t.Errorf("expected reason already_optimal, got %q", state.Reason)
	}
}

func TestGetDriftState_PropagatesVaultLookupError(t *testing.T) {
	repo := &fakeVaultRepo{}
	yields := &fakeYields{yields: []scheduler.ProtocolYield{apyYield("protocol-a", 5)}}
	d := NewAPYDriftDetector(repo, yields, nil, 200, 0, nil)

	_, err := d.GetDriftState(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected an error for an unknown vault, got nil")
	}
}

func TestGetDriftState_PropagatesYieldRegistryError(t *testing.T) {
	vaultID := uuid.New()
	repo := &fakeVaultRepo{vaults: []vault.Vault{vaultWithAllocation(vaultID, "protocol-a", 1000)}}
	yields := &fakeYields{err: assertErr("registry down")}
	d := NewAPYDriftDetector(repo, yields, nil, 200, 0, nil)

	_, err := d.GetDriftState(context.Background(), vaultID)
	if err == nil {
		t.Fatal("expected the yield-registry error to propagate, got nil")
	}
}

func TestCheckAll_EnqueuesOnlyVaultsAboveThreshold(t *testing.T) {
	aboveID, belowID := uuid.New(), uuid.New()
	repo := &fakeVaultRepo{vaults: []vault.Vault{
		vaultWithAllocation(aboveID, "protocol-a", 1000), // far from optimal -> should enqueue
		vaultWithAllocation(belowID, "protocol-b", 1000), // already optimal -> no-op
	}}
	yields := &fakeYields{yields: []scheduler.ProtocolYield{
		apyYield("protocol-a", 1),
		apyYield("protocol-b", 10),
	}}
	jobs := &fakeEnqueuer{}
	d := NewAPYDriftDetector(repo, yields, jobs, 200, 0, nil)

	if err := d.CheckAll(context.Background()); err != nil {
		t.Fatalf("CheckAll: %v", err)
	}

	if len(jobs.calls) != 1 {
		t.Fatalf("expected exactly 1 enqueued job, got %d: %+v", len(jobs.calls), jobs.calls)
	}
	call := jobs.calls[0]
	if call.jobType != RebalanceDriftJobType {
		t.Errorf("expected job type %q, got %q", RebalanceDriftJobType, call.jobType)
	}
	payload, ok := call.payload.(RebalanceDriftJobPayload)
	if !ok {
		t.Fatalf("expected payload type RebalanceDriftJobPayload, got %T", call.payload)
	}
	if payload.VaultID != aboveID {
		t.Errorf("expected the drifted vault %s to be enqueued, got %s", aboveID, payload.VaultID)
	}
	if call.idempotencyKey == "" {
		t.Error("expected a non-empty idempotency key")
	}
	// nester#1339: the enqueued job must carry a correlation id so the
	// handler's audit entry (written on successful submission) can be
	// traced back to this detection pass.
	if call.correlationID == "" {
		t.Error("expected a non-empty correlation id")
	}
}

func TestCheckAll_NoOpWhenJobEnqueuerNotYetWired(t *testing.T) {
	vaultID := uuid.New()
	repo := &fakeVaultRepo{vaults: []vault.Vault{vaultWithAllocation(vaultID, "protocol-a", 1000)}}
	yields := &fakeYields{yields: []scheduler.ProtocolYield{
		apyYield("protocol-a", 1),
		apyYield("protocol-b", 10),
	}}
	// jobs is nil until SetJobEnqueuer is called, matching how main.go
	// constructs this detector before the durable job queue client exists.
	d := NewAPYDriftDetector(repo, yields, nil, 200, 0, nil)

	if err := d.CheckAll(context.Background()); err != nil {
		t.Fatalf("CheckAll should no-op cleanly with no enqueuer wired, got error: %v", err)
	}
}

func TestCheckAll_SetJobEnqueuerWiresLateBoundQueue(t *testing.T) {
	vaultID := uuid.New()
	repo := &fakeVaultRepo{vaults: []vault.Vault{vaultWithAllocation(vaultID, "protocol-a", 1000)}}
	yields := &fakeYields{yields: []scheduler.ProtocolYield{
		apyYield("protocol-a", 1),
		apyYield("protocol-b", 10),
	}}
	d := NewAPYDriftDetector(repo, yields, nil, 200, 0, nil)

	jobs := &fakeEnqueuer{}
	d.SetJobEnqueuer(jobs)

	if err := d.CheckAll(context.Background()); err != nil {
		t.Fatalf("CheckAll: %v", err)
	}
	if len(jobs.calls) != 1 {
		t.Fatalf("expected 1 enqueued job after SetJobEnqueuer, got %d", len(jobs.calls))
	}
}

func TestCheckAll_NoOpWhenYieldRegistryEmpty(t *testing.T) {
	vaultID := uuid.New()
	repo := &fakeVaultRepo{vaults: []vault.Vault{vaultWithAllocation(vaultID, "protocol-a", 1000)}}
	yields := &fakeYields{yields: nil}
	jobs := &fakeEnqueuer{}
	d := NewAPYDriftDetector(repo, yields, jobs, 200, 0, nil)

	if err := d.CheckAll(context.Background()); err != nil {
		t.Fatalf("CheckAll: %v", err)
	}
	if len(jobs.calls) != 0 {
		t.Fatalf("expected no enqueued jobs with an empty yield registry, got %d", len(jobs.calls))
	}
}

func TestCheckAll_ContinuesPastAPerVaultEnqueueFailure(t *testing.T) {
	firstID, secondID := uuid.New(), uuid.New()
	repo := &fakeVaultRepo{vaults: []vault.Vault{
		vaultWithAllocation(firstID, "protocol-a", 1000),
		vaultWithAllocation(secondID, "protocol-a", 1000),
	}}
	yields := &fakeYields{yields: []scheduler.ProtocolYield{
		apyYield("protocol-a", 1),
		apyYield("protocol-b", 10),
	}}
	jobs := &fakeEnqueuer{err: assertErr("queue unavailable")}
	d := NewAPYDriftDetector(repo, yields, jobs, 200, 0, nil)

	// CheckAll's own error return is only for list/fetch failures (see its
	// doc comment: "errors for one vault are logged and do not abort the
	// pass"), so a per-vault enqueue failure must not surface here.
	if err := d.CheckAll(context.Background()); err != nil {
		t.Fatalf("CheckAll must not fail the whole pass on a per-vault enqueue error: %v", err)
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
