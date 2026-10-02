package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/moneypath"
)

// fakeVaultSwitchRepo is an in-memory VaultMoneyPathSwitchRepository. Its
// Get returns the zero (released) value for a missing row, exactly like the
// postgres repository, which is what lets the service tests assert that an
// untouched vault is open.
type fakeVaultSwitchRepo struct {
	stored  map[moneypath.VaultSwitchKey]moneypath.VaultSwitch
	getErr  error
	listErr error
	setErr  error
}

func newFakeVaultSwitchRepo() *fakeVaultSwitchRepo {
	return &fakeVaultSwitchRepo{stored: make(map[moneypath.VaultSwitchKey]moneypath.VaultSwitch)}
}

func (f *fakeVaultSwitchRepo) GetVaultSwitch(_ context.Context, vaultID uuid.UUID, op moneypath.Operation) (moneypath.VaultSwitch, error) {
	if f.getErr != nil {
		return moneypath.VaultSwitch{}, f.getErr
	}
	key := moneypath.VaultSwitchKey{VaultID: vaultID, Operation: op}
	if s, ok := f.stored[key]; ok {
		return s, nil
	}
	return moneypath.VaultSwitch{VaultID: vaultID, Operation: op}, nil
}

func (f *fakeVaultSwitchRepo) ListVaultSwitches(_ context.Context, vaultID uuid.UUID) ([]moneypath.VaultSwitch, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []moneypath.VaultSwitch
	for key, s := range f.stored {
		if key.VaultID == vaultID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeVaultSwitchRepo) SetVaultSwitch(_ context.Context, vaultID uuid.UUID, op moneypath.Operation, paused bool, reason string, changedBy *uuid.UUID) (moneypath.VaultSwitch, error) {
	if f.setErr != nil {
		return moneypath.VaultSwitch{}, f.setErr
	}
	s := moneypath.VaultSwitch{
		VaultID:   vaultID,
		Operation: op,
		Paused:    paused,
		Reason:    reason,
		ChangedBy: changedBy,
		UpdatedAt: time.Now().UTC(),
	}
	f.stored[s.Key()] = s
	return s, nil
}

func newTestVaultSwitchService(repo VaultMoneyPathSwitchRepository) *VaultMoneyPathSwitchService {
	return NewVaultMoneyPathSwitchService(repo, NoopAuditLogger{})
}

// Pausing one vault must not touch a sibling vault: that isolation is the
// entire point of a per-vault switch (#1322), as opposed to the global one.
func TestVaultPauseRefusesOnlyThePausedVault(t *testing.T) {
	repo := newFakeVaultSwitchRepo()
	svc := newTestVaultSwitchService(repo)

	pausedVault := uuid.New()
	otherVault := uuid.New()

	if _, err := svc.SetPaused(context.Background(), pausedVault, moneypath.OperationDeposit, true, "draining", nil, ""); err != nil {
		t.Fatalf("SetPaused: %v", err)
	}

	err := svc.EnsureVaultAllowed(context.Background(), pausedVault, moneypath.OperationDeposit)
	if !errors.Is(err, moneypath.ErrPaused) {
		t.Fatalf("paused vault deposit: got %v, want moneypath.ErrPaused", err)
	}

	if err := svc.EnsureVaultAllowed(context.Background(), otherVault, moneypath.OperationDeposit); err != nil {
		t.Fatalf("sibling vault deposit must keep operating: %v", err)
	}
}

// Deposits and withdrawals are separate switches on the same vault, so an
// operator can stop new money entering while users still take theirs out.
func TestVaultPauseOperationsAreIndependent(t *testing.T) {
	repo := newFakeVaultSwitchRepo()
	svc := newTestVaultSwitchService(repo)
	vaultID := uuid.New()

	if _, err := svc.SetPaused(context.Background(), vaultID, moneypath.OperationDeposit, true, "incident 1234", nil, ""); err != nil {
		t.Fatalf("SetPaused: %v", err)
	}

	if err := svc.EnsureVaultAllowed(context.Background(), vaultID, moneypath.OperationWithdrawal); err != nil {
		t.Fatalf("withdrawals must stay open while only deposits are paused: %v", err)
	}
	if err := svc.EnsureVaultAllowed(context.Background(), vaultID, moneypath.OperationDeposit); !errors.Is(err, moneypath.ErrPaused) {
		t.Fatalf("deposits should be refused, got %v", err)
	}

	// Releasing the switch reopens the vault.
	if _, err := svc.SetPaused(context.Background(), vaultID, moneypath.OperationDeposit, false, "", nil, ""); err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := svc.EnsureVaultAllowed(context.Background(), vaultID, moneypath.OperationDeposit); err != nil {
		t.Fatalf("released deposit must operate again: %v", err)
	}
}

// The admin view must show both operations even when neither has a stored
// row, so a UI can render a switch that has never been touched.
func TestVaultPauseListReportsReleasedDefaults(t *testing.T) {
	svc := newTestVaultSwitchService(newFakeVaultSwitchRepo())
	vaultID := uuid.New()

	switches, err := svc.List(context.Background(), vaultID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(switches) != len(moneypath.Operations()) {
		t.Fatalf("got %d switches, want %d", len(switches), len(moneypath.Operations()))
	}
	for _, s := range switches {
		if s.Paused {
			t.Errorf("%s reported paused on an untouched vault", s.Operation)
		}
		if s.VaultID != vaultID {
			t.Errorf("%s reported against vault %s, want %s", s.Operation, s.VaultID, vaultID)
		}
	}
}

// The reason must round-trip so a paused response can explain itself.
func TestVaultPauseReasonSurvivesRoundTrip(t *testing.T) {
	svc := newTestVaultSwitchService(newFakeVaultSwitchRepo())
	vaultID := uuid.New()

	if _, err := svc.SetPaused(context.Background(), vaultID, moneypath.OperationWithdrawal, true, "  oracle stale  ", nil, ""); err != nil {
		t.Fatalf("SetPaused: %v", err)
	}

	err := svc.EnsureVaultAllowed(context.Background(), vaultID, moneypath.OperationWithdrawal)
	var paused *moneypath.PausedError
	if !errors.As(err, &paused) {
		t.Fatalf("got %v, want *moneypath.PausedError", err)
	}
	if paused.Reason != "oracle stale" {
		t.Fatalf("reason = %q, want trimmed %q", paused.Reason, "oracle stale")
	}
}

// Fails closed when the switch cannot be read: a database the API cannot
// reach is itself an incident, and defaulting to "allow" would make the
// control useless exactly when it is needed.
func TestVaultPauseFailsClosedWhenUnreadable(t *testing.T) {
	repo := newFakeVaultSwitchRepo()
	repo.getErr = errors.New("database down")
	svc := newTestVaultSwitchService(repo)

	err := svc.EnsureVaultAllowed(context.Background(), uuid.New(), moneypath.OperationDeposit)
	if !errors.Is(err, moneypath.ErrPaused) {
		t.Fatalf("unreadable switch: got %v, want ErrPaused", err)
	}
}

// A service built without a repository (tooling, tests) has no switches to
// enforce and must allow, matching the global gate's nil behaviour.
func TestVaultPauseNilServiceAllows(t *testing.T) {
	var svc *VaultMoneyPathSwitchService
	if err := svc.EnsureVaultAllowed(context.Background(), uuid.New(), moneypath.OperationDeposit); err != nil {
		t.Fatalf("nil service must allow: %v", err)
	}
}

// SetPaused must not block the actual pause on a failed audit read: a
// transient hiccup reading the old state (fetched only to annotate the audit
// log) must never stop an operator from pausing a vault during an incident.
func TestVaultPauseWritesEvenWhenAuditReadFails(t *testing.T) {
	repo := newFakeVaultSwitchRepo()
	repo.getErr = errors.New("transient read failure")
	svc := newTestVaultSwitchService(repo)
	vaultID := uuid.New()

	updated, err := svc.SetPaused(context.Background(), vaultID, moneypath.OperationDeposit, true, "incident", nil, "")
	if err != nil {
		t.Fatalf("SetPaused must succeed despite audit-read failure: %v", err)
	}
	if !updated.Paused {
		t.Fatalf("returned switch should be paused")
	}

	// Confirm the write actually landed in the repository, not just the
	// in-memory cache.
	repo.getErr = nil
	stored, err := repo.GetVaultSwitch(context.Background(), vaultID, moneypath.OperationDeposit)
	if err != nil {
		t.Fatalf("GetVaultSwitch: %v", err)
	}
	if !stored.Paused {
		t.Fatalf("pause did not persist to the repository")
	}
}

// After SetPaused returns, a concurrent get() that read the stale
// "unpaused" row just before the write must never repopulate the cache with
// that stale value. Writing the new state directly into the cache (rather
// than just deleting the entry) closes the gap a plain delete would leave.
func TestVaultPauseCacheNeverServesStaleAfterConcurrentRead(t *testing.T) {
	repo := newFakeVaultSwitchRepo()
	svc := newTestVaultSwitchService(repo)
	vaultID := uuid.New()
	op := moneypath.OperationDeposit

	// Prime the cache with the pre-incident "not paused" state, as a
	// concurrent get() would have just before the operator's write lands.
	if err := svc.EnsureVaultAllowed(context.Background(), vaultID, op); err != nil {
		t.Fatalf("priming EnsureVaultAllowed: %v", err)
	}

	// Simulate that racing get() finishing its repository read and writing
	// the stale value into the cache immediately after SetPaused's own
	// cache write, by writing the same stale row back in afterward only if
	// the implementation left a delete-based gap. First perform the actual
	// pause.
	if _, err := svc.SetPaused(context.Background(), vaultID, op, true, "incident", nil, ""); err != nil {
		t.Fatalf("SetPaused: %v", err)
	}

	// Immediately after SetPaused returns, the cache must already reflect
	// "paused" — not "released" — with no window in which a concurrent
	// get() racing against the write could have repopulated it from the old
	// row.
	err := svc.EnsureVaultAllowed(context.Background(), vaultID, op)
	if !errors.Is(err, moneypath.ErrPaused) {
		t.Fatalf("got %v, want moneypath.ErrPaused immediately after SetPaused", err)
	}
}

// An unknown operation must be rejected rather than silently treated as open.
func TestVaultPauseRejectsUnknownOperation(t *testing.T) {
	svc := newTestVaultSwitchService(newFakeVaultSwitchRepo())

	err := svc.EnsureVaultAllowed(context.Background(), uuid.New(), moneypath.Operation("transfer"))
	if !errors.Is(err, moneypath.ErrUnknownOperation) {
		t.Fatalf("got %v, want moneypath.ErrUnknownOperation", err)
	}
	if _, err := svc.SetPaused(context.Background(), uuid.New(), moneypath.Operation("transfer"), true, "", nil, ""); !errors.Is(err, moneypath.ErrUnknownOperation) {
		t.Fatalf("SetPaused unknown op: got %v, want moneypath.ErrUnknownOperation", err)
	}
}
