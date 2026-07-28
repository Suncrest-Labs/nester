package flags

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// --- deterministic percentage rollout -------------------------------------

// TestPercentageMembershipStableAsRolloutGrows proves the core determinism
// guarantee: every user who is inside the rollout at p% is still inside at
// any q% > p. Membership only ever grows; no user flickers out.
func TestPercentageMembershipStableAsRolloutGrows(t *testing.T) {
	const flag = "new-deposit-path"
	const users = 5000

	steps := []float64{1, 5, 10, 20, 50, 75, 100}
	inAtStep := make([]map[int]bool, len(steps))

	for si, pct := range steps {
		inAtStep[si] = make(map[int]bool)
		for u := 0; u < users; u++ {
			if InPercentage(flag, fmt.Sprintf("user-%d", u), pct) {
				inAtStep[si][u] = true
			}
		}
	}

	for si := 1; si < len(steps); si++ {
		for u := range inAtStep[si-1] {
			if !inAtStep[si][u] {
				t.Fatalf("user-%d was in rollout at %v%% but dropped out at %v%%", u, steps[si-1], steps[si])
			}
		}
	}

	// At 100% everyone is in; at 0% nobody is.
	if got := len(inAtStep[len(steps)-1]); got != users {
		t.Fatalf("at 100%% want all %d users in rollout, got %d", users, got)
	}
	if InPercentage(flag, "user-1", 0) {
		t.Fatal("no user should be in a 0% rollout")
	}
}

// TestPercentageApproximatesTarget sanity-checks the bucket distribution:
// a 20% rollout over many users should include roughly 20% of them.
func TestPercentageApproximatesTarget(t *testing.T) {
	const users = 20000
	in := 0
	for u := 0; u < users; u++ {
		if InPercentage("some-flag", fmt.Sprintf("user-%d", u), 20) {
			in++
		}
	}
	share := float64(in) / users * 100
	if share < 18 || share > 22 {
		t.Fatalf("20%% rollout captured %.2f%% of users; want ~20%%", share)
	}
}

// TestPercentageDiffersPerFlag ensures bucketing is per-flag, not global:
// the same user lands in different buckets for different flags.
func TestPercentageDiffersPerFlag(t *testing.T) {
	same := 0
	const n = 1000
	for u := 0; u < n; u++ {
		id := fmt.Sprintf("user-%d", u)
		if InPercentage("flag-a", id, 30) == InPercentage("flag-b", id, 30) {
			same++
		}
	}
	if same == n {
		t.Fatal("bucketing identical across flags; hash must include the flag name")
	}
}

// --- fail-safe kill switch -------------------------------------------------

type failingReader struct{ err error }

func (f failingReader) Get(context.Context, string) (Flag, error) { return Flag{}, f.err }

// TestKillSwitchFailsSafeWhenServiceUnavailable proves the acceptance
// criterion: when the flag service is unreachable, a kill-switched feature
// evaluates to its registered safe position (off) — never open.
func TestKillSwitchFailsSafeWhenServiceUnavailable(t *testing.T) {
	ev := NewEvaluator(failingReader{err: errors.New("store unreachable")})
	ev.RegisterFailSafe("risky-integration", false)

	if ev.BoolValue(context.Background(), "risky-integration", EvalContext{UserID: "u1"}) {
		t.Fatal("kill switch failed OPEN while flag service unavailable; must fail safe (off)")
	}

	// Unregistered flags also fail closed by default.
	if ev.BoolValue(context.Background(), "never-registered", EvalContext{UserID: "u1"}) {
		t.Fatal("unregistered flag failed open on service outage; default must be closed")
	}

	// A flag whose safe position is "on" (e.g. legacy path stays enabled)
	// honours its registration.
	ev.RegisterFailSafe("legacy-path", true)
	if !ev.BoolValue(context.Background(), "legacy-path", EvalContext{UserID: "u1"}) {
		t.Fatal("fail-safe=on flag should evaluate true during outage")
	}
}

// --- evaluation semantics ---------------------------------------------------

type staticReader map[string]Flag

func (s staticReader) Get(_ context.Context, name string) (Flag, error) {
	f, ok := s[name]
	if !ok {
		return Flag{}, ErrNotFound
	}
	return f, nil
}

func TestEvaluatorSemantics(t *testing.T) {
	reader := staticReader{
		"bool-on":   {Name: "bool-on", Type: TypeBool, Enabled: true},
		"bool-off":  {Name: "bool-off", Type: TypeBool, Enabled: false},
		"cohort":    {Name: "cohort", Type: TypeCohort, Enabled: true, Cohort: []string{"alice", "bob"}},
		"disabled":  {Name: "disabled", Type: TypePercentage, Enabled: false, Percentage: 100},
		"threshold": {Name: "threshold", Type: TypeValue, Enabled: true, Value: "42"},
	}
	ev := NewEvaluator(reader)
	ctx := context.Background()

	if !ev.BoolValue(ctx, "bool-on", EvalContext{UserID: "anyone"}) {
		t.Error("enabled bool flag should be on")
	}
	if ev.BoolValue(ctx, "bool-off", EvalContext{UserID: "anyone"}) {
		t.Error("disabled bool flag should be off")
	}
	if !ev.BoolValue(ctx, "cohort", EvalContext{UserID: "alice"}) {
		t.Error("cohort member should be on")
	}
	if ev.BoolValue(ctx, "cohort", EvalContext{UserID: "mallory"}) {
		t.Error("non-member should be off")
	}
	if ev.BoolValue(ctx, "disabled", EvalContext{UserID: "anyone"}) {
		t.Error("master-disabled flag must be off even at 100% rollout")
	}
	if got := ev.StringValue(ctx, "threshold", "7"); got != "42" {
		t.Errorf("StringValue = %q, want 42", got)
	}
	if got := ev.StringValue(ctx, "missing", "7"); got != "7" {
		t.Errorf("StringValue for missing flag = %q, want default 7", got)
	}
}

// --- secret-rejection guard --------------------------------------------------

// TestSecretGuardRejectsSecretMarkedNames proves the hard boundary: values
// that look like secrets can never enter the flag store.
func TestSecretGuardRejectsSecretMarkedNames(t *testing.T) {
	secretNames := []string{
		"stripe_api_key",
		"DB_PASSWORD",
		"provider-token",
		"signing_secret",
		"tls_private_key",
		"oauth_credentials",
	}
	for _, name := range secretNames {
		err := rejectSecrets(Flag{Name: name, Type: TypeValue})
		if !errors.Is(err, ErrSecretRejected) {
			t.Errorf("rejectSecrets(%q) = %v, want ErrSecretRejected", name, err)
		}
	}

	okNames := []string{"new-deposit-path", "harvest-batch-size", "maintenance-banner"}
	for _, name := range okNames {
		if err := rejectSecrets(Flag{Name: name, Type: TypeBool}); err != nil {
			t.Errorf("rejectSecrets(%q) = %v, want nil", name, err)
		}
	}
}

// --- validation ----------------------------------------------------------------

func TestFlagValidate(t *testing.T) {
	if err := (Flag{Name: "", Type: TypeBool}).Validate(); err == nil {
		t.Error("empty name must be rejected")
	}
	if err := (Flag{Name: "x", Type: Type("mystery")}).Validate(); err == nil {
		t.Error("unknown type must be rejected")
	}
	if err := (Flag{Name: "x", Type: TypePercentage, Percentage: 150}).Validate(); err == nil {
		t.Error("percentage > 100 must be rejected")
	}
	if err := (Flag{Name: "x", Type: TypePercentage, Percentage: 25}).Validate(); err != nil {
		t.Errorf("valid percentage flag rejected: %v", err)
	}
}

// --- cache + invalidation ---------------------------------------------------------

// countingReader counts source reads so tests can observe caching.
type countingReader struct {
	flags staticReader
	reads int
}

func (c *countingReader) Get(ctx context.Context, name string) (Flag, error) {
	c.reads++
	return c.flags.Get(ctx, name)
}

func TestCacheServesFromCacheUntilInvalidated(t *testing.T) {
	src := &countingReader{flags: staticReader{
		"f": {Name: "f", Type: TypeBool, Enabled: true},
	}}
	cache := NewCache(src, time.Hour) // long TTL: only invalidation refreshes
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := cache.Get(ctx, "f"); err != nil {
			t.Fatalf("get: %v", err)
		}
	}
	if src.reads != 1 {
		t.Fatalf("source reads = %d, want 1 (cached)", src.reads)
	}

	// A change lands: flip the flag off and invalidate (as the pub/sub
	// message from any instance would).
	src.flags["f"] = Flag{Name: "f", Type: TypeBool, Enabled: false}
	cache.Invalidate("f")

	f, err := cache.Get(ctx, "f")
	if err != nil {
		t.Fatalf("get after invalidate: %v", err)
	}
	if f.Enabled {
		t.Fatal("cache returned stale flag after invalidation")
	}
	if src.reads != 2 {
		t.Fatalf("source reads = %d, want 2 (one refresh)", src.reads)
	}
}

// TestCacheInvalidationPropagatesAcrossInstances simulates two API instances
// each with their own in-process cache, connected by a pub/sub channel: a
// change published by one is visible to both within the delivery, not a TTL.
func TestCacheInvalidationPropagatesAcrossInstances(t *testing.T) {
	shared := &countingReader{flags: staticReader{
		"kill": {Name: "kill", Type: TypeBool, Enabled: true},
	}}

	a := NewCache(shared, time.Hour)
	b := NewCache(shared, time.Hour)
	ctx := context.Background()

	// Both instances warm their caches.
	if _, err := a.Get(ctx, "kill"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Get(ctx, "kill"); err != nil {
		t.Fatal(err)
	}

	// Instance A trips the kill switch; the pub/sub broadcast reaches both
	// caches (delivery simulated by direct calls, as the Redis subscription
	// would perform them).
	shared.flags["kill"] = Flag{Name: "kill", Type: TypeBool, Enabled: false}
	for _, inst := range []*Cache{a, b} {
		inst.Invalidate("kill")
	}

	for name, inst := range map[string]*Cache{"a": a, "b": b} {
		f, err := inst.Get(ctx, "kill")
		if err != nil {
			t.Fatalf("instance %s: %v", name, err)
		}
		if f.Enabled {
			t.Fatalf("instance %s still sees kill switch enabled after invalidation", name)
		}
	}
}

// TestCacheDoesNotCacheTransientErrors ensures a store outage is retried on
// the next read instead of being served for a whole TTL — fail-safe handling
// depends on distinguishing "store down now" from cached state.
func TestCacheDoesNotCacheTransientErrors(t *testing.T) {
	boom := errors.New("connection refused")
	flaky := &flakyReader{err: boom}
	cache := NewCache(flaky, time.Hour)
	ctx := context.Background()

	if _, err := cache.Get(ctx, "f"); !errors.Is(err, boom) {
		t.Fatalf("want transient error, got %v", err)
	}

	// Store recovers; the very next read must succeed (error was not cached).
	flaky.err = nil
	flaky.flag = Flag{Name: "f", Type: TypeBool, Enabled: true}
	f, err := cache.Get(ctx, "f")
	if err != nil {
		t.Fatalf("read after recovery: %v", err)
	}
	if !f.Enabled {
		t.Fatal("expected recovered flag")
	}
}

type flakyReader struct {
	err  error
	flag Flag
}

func (f *flakyReader) Get(context.Context, string) (Flag, error) {
	if f.err != nil {
		return Flag{}, f.err
	}
	return f.flag, nil
}
