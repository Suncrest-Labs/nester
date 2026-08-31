package reconciliation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// One unreadable contract must not kill the sweep (nester#1082): contract
// addresses are caller-supplied at vault creation, so a run that dies on the
// first bad one is a standing denial of the whole safety net — and it drops
// every finding already collected.
func TestBalanceComparator_SkipsVaultWithFailingChainRead(t *testing.T) {
	healthy := makeVault("addr-ok", decimal.NewFromInt(100))
	poisoned := makeVault("addr-poisoned", decimal.NewFromInt(200))
	drifted := makeVault("addr-drifted", decimal.NewFromInt(300))

	lister := &fakeVaultLister{vaults: []vault.Vault{healthy, poisoned, drifted}}
	chain := &fakeChainReader{
		balances: map[string]decimal.Decimal{
			"addr-ok":      decimal.NewFromInt(100),
			"addr-drifted": decimal.NewFromInt(299),
		},
		errFor: map[string]error{"addr-poisoned": errors.New("simulate failed: contract not initialized")},
	}
	comparator := BalanceComparator{Vaults: lister, Chain: chain, Clock: func() time.Time { return time.Unix(0, 0) }}

	result, err := comparator.Reconcile(context.Background(), Scope{})
	if err != nil {
		t.Fatalf("Reconcile() error = %v — a single bad contract must not fail the run", err)
	}
	// The poisoned vault is not counted as checked: checked_count reflects
	// only vaults actually compared, so partial coverage is visible.
	if result.Checked != 2 {
		t.Fatalf("Checked = %d, want 2 (the unreadable vault must not count as compared)", result.Checked)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("findings = %d, want 1 — the drifted vault's finding must survive the earlier read failure", len(result.Findings))
	}
	if result.Findings[0].EntityID != drifted.ID.String() {
		t.Fatalf("finding entity = %s, want the drifted vault %s", result.Findings[0].EntityID, drifted.ID)
	}
}

// Every read failing is not N vault problems — it is the chain-read path
// being down. Completing the run would report zero divergences because
// nothing was compared: the false-clean reading this system must never
// produce, so the run fails.
func TestBalanceComparator_AllReadsFailingFailsRun(t *testing.T) {
	a := makeVault("addr-a", decimal.NewFromInt(1))
	b := makeVault("addr-b", decimal.NewFromInt(2))

	lister := &fakeVaultLister{vaults: []vault.Vault{a, b}}
	chain := &fakeChainReader{errFor: map[string]error{
		"addr-a": errors.New("rpc unreachable"),
		"addr-b": errors.New("rpc unreachable"),
	}}
	comparator := BalanceComparator{Vaults: lister, Chain: chain, Clock: func() time.Time { return time.Unix(0, 0) }}

	if _, err := comparator.Reconcile(context.Background(), Scope{}); err == nil {
		t.Fatal("expected the run to fail when every chain read failed")
	}
}

// Shutdown is not a per-vault condition: a cancelled context propagates so
// the engine records the aborted run instead of a sweep that "completes"
// after skipping every remaining vault.
func TestBalanceComparator_CancellationPropagates(t *testing.T) {
	v := makeVault("addr-x", decimal.NewFromInt(1))
	lister := &fakeVaultLister{vaults: []vault.Vault{v}}
	chain := &fakeChainReader{errFor: map[string]error{"addr-x": context.Canceled}}
	comparator := BalanceComparator{Vaults: lister, Chain: chain, Clock: func() time.Time { return time.Unix(0, 0) }}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := comparator.Reconcile(ctx, Scope{}); err == nil {
		t.Fatal("expected the cancelled sweep to propagate an error")
	}
}
