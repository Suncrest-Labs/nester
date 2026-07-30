package notifications

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// --- test doubles specific to #829 behavior ---

type fakeRateLimiter struct {
	mu    sync.Mutex
	allow bool
	calls []string
}

func (f *fakeRateLimiter) Allow(_ context.Context, key string) (bool, time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, key)
	return f.allow, 0
}

func (f *fakeRateLimiter) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// categoryAwarePreferences implements both PreferenceStore and
// CategoryPreferenceStore so tests can prove the dispatcher prefers the
// category-scoped resolution when available.
type categoryAwarePreferences struct {
	mu    sync.Mutex
	byCat map[Category]Preferences
	calls []Category
}

func newCategoryAwarePreferences() *categoryAwarePreferences {
	return &categoryAwarePreferences{byCat: map[Category]Preferences{}}
}

func (c *categoryAwarePreferences) Get(_ context.Context, _ uuid.UUID) (Preferences, error) {
	return DefaultPreferences(), nil
}

func (c *categoryAwarePreferences) GetForCategory(_ context.Context, _ uuid.UUID, category Category) (Preferences, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, category)
	if p, ok := c.byCat[category]; ok {
		return p, nil
	}
	return DefaultPreferencesForCategory(category), nil
}

func (c *categoryAwarePreferences) Set(category Category, p Preferences) *categoryAwarePreferences {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byCat[category] = p
	return c
}

type errPushSender struct{}

func (errPushSender) Send(context.Context, []string, string, string, map[string]any) error {
	return errors.New("push provider down")
}

type recordingRetryEnqueuer struct {
	mu    sync.Mutex
	calls []struct {
		Notification Notification
		Channel      ChannelKind
	}
	err error
}

func (r *recordingRetryEnqueuer) EnqueueRetry(_ context.Context, n Notification, channel ChannelKind) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, struct {
		Notification Notification
		Channel      ChannelKind
	}{n, channel})
	return r.err
}

func (r *recordingRetryEnqueuer) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// --- Category classification ---

func TestCategoryFor_KnownExceptionsAndDefault(t *testing.T) {
	if CategoryFor(EventProtocolHealthAlert) != CategorySafety {
		t.Errorf("protocol health alert must be safety")
	}
	if CategoryFor(EventVaultPaused) != CategorySafety {
		t.Errorf("vault paused must be safety")
	}
	if CategoryFor(EventFinancialDigest) != CategoryPromotional {
		t.Errorf("financial digest must be promotional")
	}
	if CategoryFor(EventDepositConfirmed) != CategoryTransactional {
		t.Errorf("deposit confirmed must default to transactional")
	}
}

// --- Safety bypasses preferences and rate limits ---

func TestDispatcher_SafetyBypassesPreferenceOptOut(t *testing.T) {
	mail := &RecordingMailSender{}
	hub := &RecordingHub{}
	prefs := NewMemoryPreferences()
	uid := uuid.New()
	// Fully opted out of everything.
	prefs.Set(uid, Preferences{Email: false, WebSocket: false, Push: false})

	d := New(
		[]Channel{NewEmailChannel(mail, StaticEmailLookup{Addr: "u@example.com"}), NewWebSocketChannel(hub)},
		prefs,
		nil,
	)

	if err := d.Send(context.Background(), uid, EventVaultPaused, "Vault paused", "body", nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(mail.Calls) != 1 {
		t.Errorf("safety notification must deliver by email despite opt-out, got %d calls", len(mail.Calls))
	}
	if len(hub.Calls) != 1 {
		t.Errorf("safety notification must deliver by websocket despite opt-out, got %d calls", len(hub.Calls))
	}
}

func TestDispatcher_SafetyBypassesRateLimit(t *testing.T) {
	hub := &RecordingHub{}
	mail := &RecordingMailSender{}
	limiter := &fakeRateLimiter{allow: false} // would deny every non-safety call
	d := New(
		// EventVaultPaused's matrix is {Email, WebSocket}; register both so
		// the assertion below is purely about the rate-limit bypass, not
		// muddied by "no adapter registered" outcomes.
		[]Channel{NewWebSocketChannel(hub), NewEmailChannel(mail, StaticEmailLookup{Addr: "u@example.com"})},
		NewMemoryPreferences(),
		nil,
		WithRateLimiter(limiter),
	)

	if err := d.Send(context.Background(), uuid.New(), EventVaultPaused, "x", "y", nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(hub.Calls) != 1 {
		t.Errorf("safety notification must bypass rate limiting, got %d calls", len(hub.Calls))
	}
	if limiter.callCount() != 0 {
		t.Errorf("rate limiter must not even be consulted for safety events, got %d calls", limiter.callCount())
	}
}

// --- Promotional suppression is recorded, not silently dropped ---

func TestDispatcher_PromotionalSuppressedByDefaultOptOut_AndRecorded(t *testing.T) {
	hub := &RecordingHub{}
	persistence := &RecordingPersistenceStore{}
	catPrefs := newCategoryAwarePreferences() // defaults: promotional = Email:false, WebSocket:true, Push:false
	catPrefs.Set(CategoryPromotional, Preferences{Email: false, WebSocket: false, Push: false, DigestCadence: DigestCadenceOff})

	d := New(
		[]Channel{NewWebSocketChannel(hub)},
		catPrefs,
		persistence,
	)

	if err := d.Send(context.Background(), uuid.New(), EventGoalCoaching, "Coaching", "body", nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(hub.Calls) != 0 {
		t.Errorf("expected promotional to be fully suppressed, got %d hub calls", len(hub.Calls))
	}
	if persistence.Count() != 1 {
		t.Fatalf("expected the suppressed notification to still be persisted, got %d saved", persistence.Count())
	}
	saved := persistence.Saved[0]
	if !saved.Suppressed || saved.SuppressedReason != SuppressedByPreference {
		t.Errorf("expected Suppressed=true reason=preference, got suppressed=%v reason=%q", saved.Suppressed, saved.SuppressedReason)
	}
}

func TestDispatcher_CategoryAwarePreferencesPreferredOverFlat(t *testing.T) {
	catPrefs := newCategoryAwarePreferences()
	d := New(nil, catPrefs, nil)

	_ = d.Send(context.Background(), uuid.New(), EventGoalCoaching, "x", "y", nil)

	if len(catPrefs.calls) != 1 || catPrefs.calls[0] != CategoryPromotional {
		t.Errorf("expected GetForCategory to be called with CategoryPromotional, got %+v", catPrefs.calls)
	}
}

// --- Dedup ---

func TestDispatcher_DedupSuppressesRepeatWithinWindow(t *testing.T) {
	hub := &RecordingHub{}
	dedup := NewInMemoryDeduplicator()
	fixedNow := time.Now()
	dedup.nowFunc = func() time.Time { return fixedNow }

	d := New([]Channel{NewWebSocketChannel(hub)}, NewMemoryPreferences(), nil, WithDeduplicator(dedup))
	uid := uuid.New()
	opts := SendOptions{DedupWindow: time.Minute}

	// EventRebalanceExecuted's matrix is websocket-only, so only the
	// registered channel is ever attempted.
	if err := d.SendWithOptions(context.Background(), uid, EventRebalanceExecuted, "a", "b", nil, opts); err != nil {
		t.Fatalf("first Send: %v", err)
	}
	if err := d.SendWithOptions(context.Background(), uid, EventRebalanceExecuted, "a", "b", nil, opts); err != nil {
		t.Fatalf("second Send: %v", err)
	}
	if len(hub.Calls) != 1 {
		t.Errorf("expected exactly one delivery within the dedup window, got %d", len(hub.Calls))
	}

	// After the window elapses, the next Send must go through again.
	fixedNow = fixedNow.Add(2 * time.Minute)
	if err := d.SendWithOptions(context.Background(), uid, EventRebalanceExecuted, "a", "b", nil, opts); err != nil {
		t.Fatalf("third Send: %v", err)
	}
	if len(hub.Calls) != 2 {
		t.Errorf("expected delivery to resume after the dedup window elapsed, got %d calls", len(hub.Calls))
	}
}

func TestDispatcher_DedupSuppressionIsRecorded(t *testing.T) {
	persistence := &RecordingPersistenceStore{}
	dedup := NewInMemoryDeduplicator()
	d := New(nil, NewMemoryPreferences(), persistence, WithDeduplicator(dedup))
	uid := uuid.New()
	opts := SendOptions{DedupWindow: time.Hour}

	_ = d.SendWithOptions(context.Background(), uid, EventDepositConfirmed, "a", "b", nil, opts)
	_ = d.SendWithOptions(context.Background(), uid, EventDepositConfirmed, "a", "b", nil, opts)

	if persistence.Count() != 2 {
		t.Fatalf("expected both the delivered and the suppressed notification to be persisted, got %d", persistence.Count())
	}
	if !persistence.Saved[1].Suppressed || persistence.Saved[1].SuppressedReason != SuppressedByDedup {
		t.Errorf("expected second save to be recorded as dedup-suppressed, got %+v", persistence.Saved[1])
	}
}

// --- Rate limiting ---

func TestDispatcher_RateLimitSuppressesAndRecords(t *testing.T) {
	hub := &RecordingHub{}
	persistence := &RecordingPersistenceStore{}
	limiter := &fakeRateLimiter{allow: false}
	d := New([]Channel{NewWebSocketChannel(hub)}, NewMemoryPreferences(), persistence, WithRateLimiter(limiter))

	if err := d.Send(context.Background(), uuid.New(), EventDepositConfirmed, "a", "b", nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(hub.Calls) != 0 {
		t.Errorf("expected rate-limited send to deliver nowhere, got %d calls", len(hub.Calls))
	}
	if persistence.Count() != 1 || !persistence.Saved[0].Suppressed || persistence.Saved[0].SuppressedReason != SuppressedByRateLimit {
		t.Fatalf("expected a persisted rate-limit-suppressed record, got %+v", persistence.Saved)
	}
}

// --- Fallback ---

func TestDispatcher_PushFailureFallsBackToWebSocket(t *testing.T) {
	hub := &RecordingHub{}
	tokens := NewMemoryDeviceTokens()
	uid := uuid.New()
	tokens.Set(uid, []DeviceToken{{Token: "tok", Enabled: true}})

	d := New(
		[]Channel{NewPushChannel(errPushSender{}, tokens), NewWebSocketChannel(hub)},
		NewMemoryPreferences(),
		nil,
	)

	// EventYieldMilestone's matrix is Push-only, so WebSocket is not
	// normally attempted for it — proving any delivery is the fallback.
	err := d.Send(context.Background(), uid, EventYieldMilestone, "Milestone", "body", nil)
	if err == nil {
		t.Fatal("expected the push failure to surface as an error")
	}
	if len(hub.Calls) != 1 {
		t.Fatalf("expected websocket fallback delivery after push failure, got %d calls", len(hub.Calls))
	}
}

func TestDispatcher_FallbackDoesNotDoubleDeliverWhenWebSocketAlreadyInMatrix(t *testing.T) {
	hub := &RecordingHub{}
	mail := &RecordingMailSender{}
	d := New(
		[]Channel{NewEmailChannel(errMailSender{}, StaticEmailLookup{Addr: "u@example.com"}), NewWebSocketChannel(hub)},
		NewMemoryPreferences(),
		nil,
	)
	_ = mail

	// EventSettlementCompleted's matrix already includes WebSocket, so the
	// fallback triggered by Email's failure must not re-deliver a second
	// time on top of the normal in-matrix WebSocket delivery.
	_ = d.Send(context.Background(), uuid.New(), EventSettlementCompleted, "Done", "body", nil)
	if len(hub.Calls) != 1 {
		t.Errorf("expected exactly one websocket delivery, got %d (fallback must not double-deliver)", len(hub.Calls))
	}
}

func TestDispatcher_DeliveryOutcomesRecorded(t *testing.T) {
	hub := &RecordingHub{}
	persistence := &RecordingPersistenceStore{}
	d := New(
		[]Channel{NewEmailChannel(errMailSender{}, StaticEmailLookup{Addr: "u@example.com"}), NewWebSocketChannel(hub)},
		NewMemoryPreferences(),
		persistence,
	)

	_ = d.Send(context.Background(), uuid.New(), EventSettlementCompleted, "Done", "body", nil)

	if persistence.Count() != 1 {
		t.Fatalf("expected one saved notification, got %d", persistence.Count())
	}
	n := persistence.Saved[0]
	outcomes, ok := persistence.RecordedOutcomes[n.ID]
	if !ok {
		t.Fatalf("expected RecordOutcome to have been called for notification %s", n.ID)
	}

	foundFailedEmail, foundFallbackWebSocket := false, false
	for _, o := range outcomes {
		if o.Channel == ChannelEmail && !o.Delivered && o.Error != "" {
			foundFailedEmail = true
		}
		if o.Channel == ChannelWebSocket && o.Delivered && o.IsFallback {
			foundFallbackWebSocket = true
		}
	}
	if !foundFailedEmail {
		t.Errorf("expected a failed email outcome, got %+v", outcomes)
	}
	if !foundFallbackWebSocket {
		t.Errorf("expected websocket to be recorded as a fallback delivery, got %+v", outcomes)
	}
}

// --- Retry enqueue ---

func TestDispatcher_EnqueuesRetryForFailedEmailButNotWebSocket(t *testing.T) {
	retry := &recordingRetryEnqueuer{}
	d := New(
		[]Channel{NewEmailChannel(errMailSender{}, StaticEmailLookup{Addr: "u@example.com"})},
		NewMemoryPreferences(),
		nil,
		WithRetryEnqueuer(retry),
	)

	// EventKYCRejected is email-only, so there's no websocket fallback to
	// muddy the assertion.
	_ = d.Send(context.Background(), uuid.New(), EventKYCRejected, "x", "y", nil)

	if retry.count() != 1 {
		t.Fatalf("expected exactly one retry enqueue for the failed email, got %d", retry.count())
	}
	if retry.calls[0].Channel != ChannelEmail {
		t.Errorf("expected retry for ChannelEmail, got %q", retry.calls[0].Channel)
	}
}

func TestDispatcher_NoRetryEnqueuedForWebSocketFailure(t *testing.T) {
	retry := &recordingRetryEnqueuer{}
	// A hub whose PushToUser always fails, to exercise a pure websocket
	// failure path (EventRebalanceExecuted is websocket-only).
	failingHub := recordingHubFunc(func(context.Context, uuid.UUID, string, any) error {
		return errors.New("client disconnected")
	})
	d := New(
		[]Channel{NewWebSocketChannel(failingHub)},
		NewMemoryPreferences(),
		nil,
		WithRetryEnqueuer(retry),
	)

	_ = d.Send(context.Background(), uuid.New(), EventRebalanceExecuted, "x", "y", nil)

	if retry.count() != 0 {
		t.Errorf("websocket failures must never be retried through the job queue, got %d enqueues", retry.count())
	}
}

type recordingHubFunc func(context.Context, uuid.UUID, string, any) error

func (f recordingHubFunc) PushToUser(ctx context.Context, userID uuid.UUID, eventName string, payload any) error {
	return f(ctx, userID, eventName, payload)
}

// --- Stats ---

func TestDispatcher_StatsTracksPerCategoryOutcomes(t *testing.T) {
	hub := &RecordingHub{}
	d := New([]Channel{NewWebSocketChannel(hub)}, NewMemoryPreferences(), nil)

	_ = d.Send(context.Background(), uuid.New(), EventRebalanceExecuted, "x", "y", nil) // transactional, delivered
	_ = d.Send(context.Background(), uuid.New(), EventVaultPaused, "x", "y", nil)       // safety, delivered

	stats := d.Stats()
	txn := stats[CategoryTransactional]
	if txn.Attempted != 1 || txn.Delivered != 1 || txn.Failed != 0 {
		t.Errorf("transactional stats = %+v, want Attempted=1 Delivered=1", txn)
	}
	safety := stats[CategorySafety]
	if safety.Attempted != 1 || safety.Delivered != 1 {
		t.Errorf("safety stats = %+v, want Attempted=1 Delivered=1", safety)
	}
}

func TestDispatcher_StatsTracksSuppressed(t *testing.T) {
	limiter := &fakeRateLimiter{allow: false}
	d := New(nil, NewMemoryPreferences(), nil, WithRateLimiter(limiter))

	_ = d.Send(context.Background(), uuid.New(), EventDepositConfirmed, "x", "y", nil)

	stats := d.Stats()
	if stats[CategoryTransactional].Suppressed != 1 {
		t.Errorf("expected 1 suppressed transactional send, got %+v", stats[CategoryTransactional])
	}
}
