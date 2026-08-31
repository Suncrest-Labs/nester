package reconciliation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// fakeVaultLister is an in-memory VaultLister that honors Limit/Offset like
// the real postgres-backed implementation: it returns a genuine total count
// independent of the page size, so a caller that pages via that count
// eventually terminates and collects every vault.
type fakeVaultLister struct {
	vaults []vault.Vault
	// calls records each (Limit, Offset) pair Reconcile requested, so tests
	// can assert on the exact paging behavior, not just the result.
	calls []vault.ListFilter
	// failOnCall, when > 0, makes the Nth ListVaults call (1-indexed) return
	// an error instead of a page — simulating a page fetch failing mid-run.
	failOnCall int
}

func (f *fakeVaultLister) ListVaults(_ context.Context, filter vault.ListFilter) ([]vault.Vault, int, error) {
	f.calls = append(f.calls, filter)
	if f.failOnCall > 0 && len(f.calls) == f.failOnCall {
		return nil, 0, errors.New("db unavailable")
	}
	total := len(f.vaults)
	start := filter.Offset
	if start > total {
		start = total
	}
	end := start + filter.Limit
	if end > total {
		end = total
	}
	return f.vaults[start:end], total, nil
}

// fakeChainReader reports a fixed on-chain balance per contract address; any
// address not in balances is treated as matching whatever CurrentBalance the
// test set, i.e. no finding.
type fakeChainReader struct {
	balances map[string]decimal.Decimal
	// errFor makes reads for these addresses fail, simulating an
	// uninitialized contract or an i128 past the reader's int64 range.
	errFor map[string]error
}

func (f *fakeChainReader) TotalAssets(_ context.Context, contractAddress string) (decimal.Decimal, error) {
	if err, ok := f.errFor[contractAddress]; ok {
		return decimal.Zero, err
	}
	if bal, ok := f.balances[contractAddress]; ok {
		return bal, nil
	}
	return decimal.Zero, nil
}

func makeVault(contractAddress string, balance decimal.Decimal) vault.Vault {
	return vault.Vault{
		ID:              uuid.New(),
		ContractAddress: contractAddress,
		CurrentBalance:  balance,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	}
}

func TestBalanceComparator_Reconcile_ChecksEveryVaultAcrossPages(t *testing.T) {
	// More vaults than a single page (vaultReconcilePageSize) would return,
	// so this only passes if Reconcile actually pages rather than
	// truncating at the first response (nester#1192).
	var vaults []vault.Vault
	balances := map[string]decimal.Decimal{}
	vaultCount := vaultReconcilePageSize*2 + 37
	for i := 0; i < vaultCount; i++ {
		addr := uuid.New().String()
		bal := decimal.NewFromInt(int64(i))
		vaults = append(vaults, makeVault(addr, bal))
		balances[addr] = bal // matches, so no findings — this test is about coverage, not mismatches
	}

	lister := &fakeVaultLister{vaults: vaults}
	chain := &fakeChainReader{balances: balances}
	comparator := BalanceComparator{Vaults: lister, Chain: chain, Clock: func() time.Time { return time.Unix(0, 0) }}

	result, err := comparator.Reconcile(context.Background(), Scope{})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.Checked != vaultCount {
		t.Fatalf("Checked = %d, want %d (some vaults were silently skipped)", result.Checked, vaultCount)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings (balances match), got %d", len(result.Findings))
	}

	if len(lister.calls) < 3 {
		t.Fatalf("expected at least 3 paged calls for %d vaults at page size %d, got %d calls",
			vaultCount, vaultReconcilePageSize, len(lister.calls))
	}
	for i, call := range lister.calls {
		if call.Limit != vaultReconcilePageSize {
			t.Fatalf("call %d: Limit = %d, want %d", i, call.Limit, vaultReconcilePageSize)
		}
		if call.Status != string(vault.StatusActive) {
			t.Fatalf("call %d: Status = %q, want %q", i, call.Status, vault.StatusActive)
		}
	}
}

func TestBalanceComparator_Reconcile_DetectsMismatchOnASecondPage(t *testing.T) {
	// A drifted balance on a vault that only exists past the first page must
	// still be caught — proves comparison logic runs on every page, not just
	// the first.
	var vaults []vault.Vault
	balances := map[string]decimal.Decimal{}
	for i := 0; i < vaultReconcilePageSize+5; i++ {
		addr := uuid.New().String()
		bal := decimal.NewFromInt(int64(i))
		vaults = append(vaults, makeVault(addr, bal))
		balances[addr] = bal
	}
	// The last vault (past page 1) has drifted on-chain.
	driftedAddr := vaults[len(vaults)-1].ContractAddress
	balances[driftedAddr] = decimal.NewFromInt(999999)

	lister := &fakeVaultLister{vaults: vaults}
	chain := &fakeChainReader{balances: balances}
	comparator := BalanceComparator{Vaults: lister, Chain: chain, Clock: func() time.Time { return time.Unix(0, 0) }}

	result, err := comparator.Reconcile(context.Background(), Scope{})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected exactly 1 finding for the drifted vault past page 1, got %d", len(result.Findings))
	}
}

func TestBalanceComparator_Reconcile_EmptyResultSinglePage(t *testing.T) {
	lister := &fakeVaultLister{}
	chain := &fakeChainReader{}
	comparator := BalanceComparator{Vaults: lister, Chain: chain, Clock: func() time.Time { return time.Unix(0, 0) }}

	result, err := comparator.Reconcile(context.Background(), Scope{})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.Checked != 0 {
		t.Fatalf("Checked = %d, want 0", result.Checked)
	}
	if len(lister.calls) != 1 {
		t.Fatalf("expected exactly one call for an empty result, got %d", len(lister.calls))
	}
}

func TestBalanceComparator_Reconcile_SinglePageDoesNotOverFetch(t *testing.T) {
	vaults := []vault.Vault{makeVault("addr-1", decimal.NewFromInt(1)), makeVault("addr-2", decimal.NewFromInt(2))}
	lister := &fakeVaultLister{vaults: vaults}
	chain := &fakeChainReader{}
	comparator := BalanceComparator{Vaults: lister, Chain: chain, Clock: func() time.Time { return time.Unix(0, 0) }}

	if _, err := comparator.Reconcile(context.Background(), Scope{}); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(lister.calls) != 1 {
		t.Fatalf("expected exactly one call when everything fits on one page, got %d", len(lister.calls))
	}
}

func TestBalanceComparator_Reconcile_PageFetchFailureMidRunFailsTheWholeRun(t *testing.T) {
	// Enough vaults to require a second page, which is made to fail.
	var vaults []vault.Vault
	for i := 0; i < vaultReconcilePageSize+5; i++ {
		vaults = append(vaults, makeVault(uuid.New().String(), decimal.NewFromInt(int64(i))))
	}
	lister := &fakeVaultLister{vaults: vaults, failOnCall: 2}
	chain := &fakeChainReader{}
	comparator := BalanceComparator{Vaults: lister, Chain: chain, Clock: func() time.Time { return time.Unix(0, 0) }}

	result, err := comparator.Reconcile(context.Background(), Scope{})
	if err == nil {
		t.Fatal("Reconcile() error = nil, want the page-fetch error to propagate")
	}
	// The run must not silently report success/partial coverage as if it
	// were complete — nester#1192's fourth acceptance criterion.
	if result.Checked != vaultReconcilePageSize {
		t.Fatalf("Checked = %d, want %d (only the vaults from the successful first page)",
			result.Checked, vaultReconcilePageSize)
	}
}

func TestBalanceComparator_Reconcile_ScopedToOneVaultStillPagesThroughAll(t *testing.T) {
	// scope.VaultID filters which vault produces findings, but Reconcile
	// must still fetch every page to find it — it cannot stop paging just
	// because the target vault might be on an earlier page.
	var vaults []vault.Vault
	balances := map[string]decimal.Decimal{}
	for i := 0; i < vaultReconcilePageSize+5; i++ {
		addr := uuid.New().String()
		bal := decimal.NewFromInt(int64(i))
		vaults = append(vaults, makeVault(addr, bal))
		balances[addr] = bal
	}
	target := vaults[len(vaults)-1]
	balances[target.ContractAddress] = decimal.NewFromInt(999999)

	lister := &fakeVaultLister{vaults: vaults}
	chain := &fakeChainReader{balances: balances}
	comparator := BalanceComparator{Vaults: lister, Chain: chain, Clock: func() time.Time { return time.Unix(0, 0) }}

	result, err := comparator.Reconcile(context.Background(), Scope{VaultID: target.ID})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("expected the scoped vault's drift to be found even though it's past page 1, got %d findings", len(result.Findings))
	}
	if len(lister.calls) < 2 {
		t.Fatalf("expected Reconcile to page past the first page even when scoped to one vault, got %d calls", len(lister.calls))
	}
}
