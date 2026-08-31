package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/moneypath"
)

// memorySwitchRepo is an in-memory stand-in for the Postgres table, so the
// switch semantics can be tested without a database.
type memorySwitchRepo struct {
	state map[moneypath.Operation]moneypath.Switch
	// failGet forces the read path to error, to exercise fail-closed.
	failGet error
	reads   int
}

func newMemorySwitchRepo() *memorySwitchRepo {
	return &memorySwitchRepo{state: map[moneypath.Operation]moneypath.Switch{
		moneypath.OperationDeposit:    {Operation: moneypath.OperationDeposit},
		moneypath.OperationWithdrawal: {Operation: moneypath.OperationWithdrawal},
	}}
}

func (m *memorySwitchRepo) GetSwitch(_ context.Context, op moneypath.Operation) (moneypath.Switch, error) {
	m.reads++
	if m.failGet != nil {
		return moneypath.Switch{}, m.failGet
	}
	s, ok := m.state[op]
	if !ok {
		return moneypath.Switch{}, moneypath.ErrUnknownOperation
	}
	return s, nil
}

func (m *memorySwitchRepo) ListSwitches(context.Context) ([]moneypath.Switch, error) {
	if m.failGet != nil {
		return nil, m.failGet
	}
	out := make([]moneypath.Switch, 0, len(m.state))
	for _, op := range moneypath.Operations() {
		out = append(out, m.state[op])
	}
	return out, nil
}

func (m *memorySwitchRepo) SetSwitch(
	_ context.Context, op moneypath.Operation, paused bool, reason string, changedBy *uuid.UUID,
) (moneypath.Switch, error) {
	s, ok := m.state[op]
	if !ok {
		return moneypath.Switch{}, moneypath.ErrUnknownOperation
	}
	s.Paused = paused
	s.Reason = reason
	s.ChangedBy = changedBy
	s.UpdatedAt = time.Now()
	m.state[op] = s
	return s, nil
}

// recordingAudit captures entries so the audit criterion can be asserted.
type recordingAudit struct{ entries []AuditEntry }

func (r *recordingAudit) Log(_ context.Context, e AuditEntry) error {
	r.entries = append(r.entries, e)
	return nil
}

// TestEngageBlocksReleaseRestores is the acceptance test #1120 asks for:
// engage, verify blocked, release, verify restored.
func TestEngageBlocksReleaseRestores(t *testing.T) {
	ctx := context.Background()
	repo := newMemorySwitchRepo()
	svc := NewMoneyPathSwitchService(repo, &recordingAudit{})

	if err := svc.EnsureAllowed(ctx, moneypath.OperationDeposit); err != nil {
		t.Fatalf("deposits should be allowed before any pause: %v", err)
	}

	if _, err := svc.SetPaused(ctx, moneypath.OperationDeposit, true, "incident 1234", nil, ""); err != nil {
		t.Fatalf("engage: %v", err)
	}

	err := svc.EnsureAllowed(ctx, moneypath.OperationDeposit)
	if !errors.Is(err, moneypath.ErrPaused) {
		t.Fatalf("deposits should be blocked while paused, got %v", err)
	}

	if _, err := svc.SetPaused(ctx, moneypath.OperationDeposit, false, "", nil, ""); err != nil {
		t.Fatalf("release: %v", err)
	}

	if err := svc.EnsureAllowed(ctx, moneypath.OperationDeposit); err != nil {
		t.Fatalf("deposits should be restored after release: %v", err)
	}
}

// The switches must be independent: stopping money entering while still
// letting users withdraw is the common incident response.
func TestPausingDepositsLeavesWithdrawalsAlone(t *testing.T) {
	ctx := context.Background()
	svc := NewMoneyPathSwitchService(newMemorySwitchRepo(), nil)

	if _, err := svc.SetPaused(ctx, moneypath.OperationDeposit, true, "incident", nil, ""); err != nil {
		t.Fatalf("engage deposit pause: %v", err)
	}

	if err := svc.EnsureAllowed(ctx, moneypath.OperationDeposit); !errors.Is(err, moneypath.ErrPaused) {
		t.Fatalf("deposit should be paused, got %v", err)
	}
	if err := svc.EnsureAllowed(ctx, moneypath.OperationWithdrawal); err != nil {
		t.Fatalf("withdrawal must stay open when only deposits are paused: %v", err)
	}
}

// Engaging must take effect immediately on the instance that made the change,
// not after the read cache expires.
func TestEngageTakesEffectImmediately(t *testing.T) {
	ctx := context.Background()
	repo := newMemorySwitchRepo()
	svc := NewMoneyPathSwitchService(repo, nil)

	// Prime the cache with the released state.
	if err := svc.EnsureAllowed(ctx, moneypath.OperationDeposit); err != nil {
		t.Fatalf("prime: %v", err)
	}

	if _, err := svc.SetPaused(ctx, moneypath.OperationDeposit, true, "now", nil, ""); err != nil {
		t.Fatalf("engage: %v", err)
	}

	if err := svc.EnsureAllowed(ctx, moneypath.OperationDeposit); !errors.Is(err, moneypath.ErrPaused) {
		t.Fatalf("pause must apply immediately, not after the cache TTL; got %v", err)
	}
}

// A change made elsewhere must be picked up once the cached entry expires,
// otherwise a pause engaged on another instance would never reach this one.
func TestExternalChangeIsSeenAfterTTL(t *testing.T) {
	ctx := context.Background()
	repo := newMemorySwitchRepo()
	svc := NewMoneyPathSwitchService(repo, nil)

	now := time.Now()
	svc.nowFn = func() time.Time { return now }

	if err := svc.EnsureAllowed(ctx, moneypath.OperationDeposit); err != nil {
		t.Fatalf("prime: %v", err)
	}

	// Another instance engages the switch directly in the store.
	if _, err := repo.SetSwitch(ctx, moneypath.OperationDeposit, true, "elsewhere", nil); err != nil {
		t.Fatalf("external engage: %v", err)
	}

	now = now.Add(switchCacheTTL + time.Second)

	if err := svc.EnsureAllowed(ctx, moneypath.OperationDeposit); !errors.Is(err, moneypath.ErrPaused) {
		t.Fatalf("a pause engaged elsewhere must be seen after the TTL, got %v", err)
	}
}

// The gate exists to stop money moving during an incident. A store it cannot
// read is itself an incident, so it must refuse rather than wave traffic
// through at exactly the moment the control matters.
func TestUnreadableSwitchFailsClosed(t *testing.T) {
	ctx := context.Background()
	repo := newMemorySwitchRepo()
	repo.failGet = errors.New("database unreachable")
	svc := NewMoneyPathSwitchService(repo, nil)

	if err := svc.EnsureAllowed(ctx, moneypath.OperationDeposit); !errors.Is(err, moneypath.ErrPaused) {
		t.Fatalf("unreadable switch must fail closed, got %v", err)
	}
}

// The operator's reason has to survive to the caller, because the UI is
// required to explain the pause rather than show a generic error.
func TestPauseReasonReachesTheCaller(t *testing.T) {
	ctx := context.Background()
	svc := NewMoneyPathSwitchService(newMemorySwitchRepo(), nil)

	const reason = "suspected pricing bug, paused while we investigate"
	if _, err := svc.SetPaused(ctx, moneypath.OperationWithdrawal, true, reason, nil, ""); err != nil {
		t.Fatalf("engage: %v", err)
	}

	err := svc.EnsureAllowed(ctx, moneypath.OperationWithdrawal)
	var paused *moneypath.PausedError
	if !errors.As(err, &paused) {
		t.Fatalf("expected a *moneypath.PausedError, got %T (%v)", err, err)
	}
	if paused.Reason != reason {
		t.Fatalf("reason = %q, want %q", paused.Reason, reason)
	}
}

// Engaging and releasing are both audit-logged, with the before and after
// state, so an incident review can reconstruct who stopped what and when.
func TestSwitchChangesAreAuditLogged(t *testing.T) {
	ctx := context.Background()
	audit := &recordingAudit{}
	svc := NewMoneyPathSwitchService(newMemorySwitchRepo(), audit)

	actor := uuid.New()
	if _, err := svc.SetPaused(ctx, moneypath.OperationDeposit, true, "incident", &actor, "203.0.113.7"); err != nil {
		t.Fatalf("engage: %v", err)
	}
	if _, err := svc.SetPaused(ctx, moneypath.OperationDeposit, false, "", &actor, "203.0.113.7"); err != nil {
		t.Fatalf("release: %v", err)
	}

	if len(audit.entries) != 2 {
		t.Fatalf("audit entries = %d, want 2", len(audit.entries))
	}
	if audit.entries[0].Action != "money_path.pause" {
		t.Errorf("first action = %q, want money_path.pause", audit.entries[0].Action)
	}
	if audit.entries[1].Action != "money_path.release" {
		t.Errorf("second action = %q, want money_path.release", audit.entries[1].Action)
	}
	for i, e := range audit.entries {
		if e.UserID == nil || *e.UserID != actor {
			t.Errorf("entry %d: actor not recorded", i)
		}
		if e.IPAddress != "203.0.113.7" {
			t.Errorf("entry %d: ip = %q", i, e.IPAddress)
		}
		if e.OldValue == nil || e.NewValue == nil {
			t.Errorf("entry %d: before/after state missing", i)
		}
	}
}

// An audit-log failure must not stop an operator halting the money path.
func TestAuditFailureDoesNotBlockThePause(t *testing.T) {
	ctx := context.Background()
	svc := NewMoneyPathSwitchService(newMemorySwitchRepo(), failingAudit{})

	if _, err := svc.SetPaused(ctx, moneypath.OperationDeposit, true, "incident", nil, ""); err != nil {
		t.Fatalf("a failing audit log must not prevent the pause: %v", err)
	}
	if err := svc.EnsureAllowed(ctx, moneypath.OperationDeposit); !errors.Is(err, moneypath.ErrPaused) {
		t.Fatalf("pause should be in force, got %v", err)
	}
}

type failingAudit struct{}

func (failingAudit) Log(context.Context, AuditEntry) error {
	return errors.New("audit store unavailable")
}

func TestUnknownOperationIsRejected(t *testing.T) {
	ctx := context.Background()
	svc := NewMoneyPathSwitchService(newMemorySwitchRepo(), nil)

	if err := svc.EnsureAllowed(ctx, moneypath.Operation("transfer")); !errors.Is(err, moneypath.ErrUnknownOperation) {
		t.Fatalf("EnsureAllowed on an unknown operation: got %v", err)
	}
	if _, err := svc.SetPaused(ctx, moneypath.Operation("transfer"), true, "", nil, ""); !errors.Is(err, moneypath.ErrUnknownOperation) {
		t.Fatalf("SetPaused on an unknown operation: got %v", err)
	}
}

// A nil service is the "switch not wired" configuration used by tooling and
// tests, and must allow everything rather than panicking.
func TestNilServiceAllowsEverything(t *testing.T) {
	var svc *MoneyPathSwitchService
	if err := svc.EnsureAllowed(context.Background(), moneypath.OperationDeposit); err != nil {
		t.Fatalf("nil service should allow: %v", err)
	}
}

// Failing closed is right; losing the cause is not. The handler maps
// ErrPaused to a 503 before reaching any logging branch, so if the underlying
// error is discarded an outage is indistinguishable from a deliberate pause —
// the operator sees a reason nobody set and nothing names the real fault.
func TestUnreadableSwitchPreservesTheUnderlyingCause(t *testing.T) {
	ctx := context.Background()
	repo := newMemorySwitchRepo()
	dbDown := errors.New("database unreachable")
	repo.failGet = dbDown
	svc := NewMoneyPathSwitchService(repo, nil)

	err := svc.EnsureAllowed(ctx, moneypath.OperationDeposit)

	if !errors.Is(err, moneypath.ErrPaused) {
		t.Fatalf("must still fail closed: got %v", err)
	}
	if !errors.Is(err, dbDown) {
		t.Fatalf("the underlying cause must survive for the logs: got %v", err)
	}

	var paused *moneypath.PausedError
	if !errors.As(err, &paused) {
		t.Fatalf("expected *moneypath.PausedError, got %T", err)
	}
	if paused.Cause == nil {
		t.Fatal("PausedError.Cause must be set when the switch could not be read")
	}
}

// A genuine operator pause carries no Cause, so the two are distinguishable
// by more than the reason string.
func TestDeliberatePauseCarriesNoCause(t *testing.T) {
	ctx := context.Background()
	svc := NewMoneyPathSwitchService(newMemorySwitchRepo(), nil)

	if _, err := svc.SetPaused(ctx, moneypath.OperationDeposit, true, "incident", nil, ""); err != nil {
		t.Fatalf("engage: %v", err)
	}

	var paused *moneypath.PausedError
	if !errors.As(svc.EnsureAllowed(ctx, moneypath.OperationDeposit), &paused) {
		t.Fatal("expected a *moneypath.PausedError")
	}
	if paused.Cause != nil {
		t.Fatalf("a deliberate pause must not carry a cause, got %v", paused.Cause)
	}
}
