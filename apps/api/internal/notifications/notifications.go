// Package notifications implements the dispatcher scaffolding for the
// notification system described in nester#373.
//
// What's in this MVP
//
//   - Event + Channel + Preferences types (the shape every other layer
//     will need).
//   - Dispatcher service that picks the right channels per event and
//     respects per-user preferences before fanning out to a set of
//     pluggable `Channel` adapters.
//   - In-memory channel implementations the unit tests pin against
//     (a recording email channel and a recording websocket channel).
//
// What's deferred to a follow-up PR (called out in README)
//
//   - The Postgres `notifications` table migration + history-read API.
//   - HTTP handlers (`GET /api/v1/users/{userId}/notifications`,
//     `PATCH .../{id}`, mark-all-read).
//   - Frontend page and badge counter.
//   - Concrete SMTP / Resend providers (the SMTPChannel placeholder
//     here uses a `MailSender` seam so a real provider can be wired
//     in without changing the dispatcher).
//
// Splitting this way lets the dispatcher service, the channel matrix,
// and the preference logic land + be tested first while the noisier
// migration / handler / frontend work stays out of the critical path.
package notifications

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// EventType enumerates the notification triggers from the issue.
type EventType string

const (
	EventSettlementCompleted       EventType = "settlement_completed"
	EventSettlementFailed          EventType = "settlement_failed"
	EventDepositConfirmed          EventType = "deposit_confirmed"
	EventYieldMilestone            EventType = "yield_milestone"
	EventVaultAPYDrop              EventType = "vault_apy_drop"
	EventVaultPaused               EventType = "vault_paused"
	EventRebalanceExecuted         EventType = "rebalance_executed"
	EventKYCApproved               EventType = "kyc_approved"
	EventKYCRejected               EventType = "kyc_rejected"
	EventGoalMilestone             EventType = "goal_milestone"
	EventScheduledDepositCompleted EventType = "scheduled_deposit_completed"
	EventSavingsStreak             EventType = "savings_streak_milestone"
	EventProtocolHealthAlert       EventType = "protocol_health_alert"
	EventGoalCoaching              EventType = "goal_coaching"
	// EventFinancialDigest is the periodic (weekly/monthly) personalized
	// savings narrative (#859). Cadence and opt-out are controlled by
	// Preferences.DigestCadence rather than a boolean, but delivery still
	// goes through the same per-channel Allow() gate as every other event.
	EventFinancialDigest EventType = "financial_digest"
	EventSavingsNudge    EventType = "savings_nudge"
)

// DigestCadence values accepted for Preferences.DigestCadence.
const (
	DigestCadenceOff     = "off"
	DigestCadenceWeekly  = "weekly"
	DigestCadenceMonthly = "monthly"
)

// ValidDigestCadence reports whether cadence is a recognized value.
func ValidDigestCadence(cadence string) bool {
	switch cadence {
	case DigestCadenceOff, DigestCadenceWeekly, DigestCadenceMonthly:
		return true
	default:
		return false
	}
}

// Category classifies an EventType for suppressibility policy (#829).
// Safety notifications always deliver regardless of user preferences or
// rate limits — suppressing a safety message to respect a preference is
// the wrong trade-off. Promotional notifications fully honor opt-out.
// Transactional sits in between (on by default, opt-out like promotional).
type Category string

const (
	CategorySafety        Category = "safety"
	CategoryTransactional Category = "transactional"
	CategoryPromotional   Category = "promotional"
)

// safetyEvents and promotionalEvents are the explicit exception sets;
// everything else defaults to CategoryTransactional. Kept as small,
// explicit sets rather than a second full matrix so the common case (a new
// event type is transactional) needs no entry here at all.
var safetyEvents = map[EventType]bool{
	EventProtocolHealthAlert: true,
	EventVaultPaused:         true,
	EventSettlementFailed:    true,
}

var promotionalEvents = map[EventType]bool{
	EventGoalCoaching:    true,
	EventFinancialDigest: true,
}

// CategoryFor returns t's suppressibility category.
func CategoryFor(t EventType) Category {
	if safetyEvents[t] {
		return CategorySafety
	}
	if promotionalEvents[t] {
		return CategoryPromotional
	}
	return CategoryTransactional
}

// ChannelKind is the transport a notification is delivered over.
type ChannelKind string

const (
	ChannelEmail     ChannelKind = "email"
	ChannelWebSocket ChannelKind = "websocket"
	ChannelPush      ChannelKind = "push"
)

// eventChannelMatrix is the routing table from the issue. The dispatcher
// computes the union of channels per event, then filters by the user's
// preferences.
var eventChannelMatrix = map[EventType][]ChannelKind{
	EventSettlementCompleted:       {ChannelEmail, ChannelWebSocket, ChannelPush},
	EventSettlementFailed:          {ChannelEmail, ChannelWebSocket, ChannelPush},
	EventDepositConfirmed:          {ChannelEmail, ChannelWebSocket, ChannelPush},
	EventYieldMilestone:            {ChannelPush},
	EventVaultAPYDrop:              {ChannelEmail, ChannelPush},
	EventVaultPaused:               {ChannelEmail, ChannelWebSocket},
	EventRebalanceExecuted:         {ChannelWebSocket},
	EventKYCApproved:               {ChannelEmail},
	EventKYCRejected:               {ChannelEmail},
	EventGoalMilestone:             {ChannelPush},
	EventScheduledDepositCompleted: {ChannelEmail, ChannelWebSocket, ChannelPush},
	EventSavingsStreak:             {ChannelPush},
	EventProtocolHealthAlert:       {ChannelEmail, ChannelPush, ChannelWebSocket},
	EventGoalCoaching:              {ChannelPush},
	EventFinancialDigest:           {ChannelEmail, ChannelWebSocket, ChannelPush},
	EventSavingsNudge:              {ChannelPush, ChannelWebSocket},
}

// ChannelsFor returns the channels configured to deliver the given event,
// per the matrix in the issue.
func ChannelsFor(t EventType) []ChannelKind {
	cs, ok := eventChannelMatrix[t]
	if !ok {
		return nil
	}
	out := make([]ChannelKind, len(cs))
	copy(out, cs)
	return out
}

// Preferences captures the user's per-channel opt-out. All channels default to
// `true` (notifications on) when no row exists yet.
type Preferences struct {
	Email     bool `json:"email"`
	WebSocket bool `json:"websocket"`
	Push      bool `json:"push"`
	// DigestCadence is one of DigestCadenceOff/Weekly/Monthly (#859). The
	// digest is delivered on the channels above like any other event; this
	// field only controls whether/how often it fires at all.
	DigestCadence string `json:"digest_cadence"`
}

// DefaultPreferences returns the "everything on" baseline new users get
// before they explicitly opt out. Digest defaults to monthly rather than
// off so the feature is opt-out, matching every other notification type
// here, but a user can turn it off entirely via DigestCadenceOff.
func DefaultPreferences() Preferences {
	return Preferences{Email: true, WebSocket: true, Push: true, DigestCadence: DigestCadenceMonthly}
}

// Allow returns whether the given channel is permitted by the preferences.
func (p Preferences) Allow(c ChannelKind) bool {
	switch c {
	case ChannelEmail:
		return p.Email
	case ChannelWebSocket:
		return p.WebSocket
	case ChannelPush:
		return p.Push
	default:
		return false
	}
}

// DefaultPreferencesForCategory returns the sensible default for a given
// category (#829): transactional and safety default to everything on
// (safety bypasses preferences entirely at send time regardless, but a
// sensible default still matters for what a settings page would show);
// promotional defaults to minimal — off except the free in-app bell,
// matching the issue's "promotional off or minimal" guidance.
func DefaultPreferencesForCategory(c Category) Preferences {
	if c == CategoryPromotional {
		return Preferences{Email: false, WebSocket: true, Push: false, DigestCadence: DigestCadenceOff}
	}
	return DefaultPreferences()
}

// CategoryPreferenceStore is an optional upgrade to PreferenceStore: a store
// that can resolve preferences per category rather than one flat set for
// every event (#829). The dispatcher type-asserts for this at send time; a
// store that only implements PreferenceStore keeps working exactly as
// before (every category uses the same flat preferences).
type CategoryPreferenceStore interface {
	GetForCategory(ctx context.Context, userID uuid.UUID, category Category) (Preferences, error)
}

// SuppressionReason records why a notification was not attempted on any
// channel, so the suppression is auditable rather than silently dropped
// (#829's "suppressed notifications are recorded with their reason").
type SuppressionReason string

const (
	SuppressedByPreference SuppressionReason = "preference"
	SuppressedByDedup      SuppressionReason = "dedup"
	SuppressedByRateLimit  SuppressionReason = "rate_limit"
)

// ChannelOutcome records what happened when delivery was attempted on one
// channel — the per-channel delivery tracking (#829).
type ChannelOutcome struct {
	Channel    ChannelKind `json:"channel"`
	Delivered  bool        `json:"delivered"`
	Error      string      `json:"error,omitempty"`
	IsFallback bool        `json:"is_fallback,omitempty"`
}

// Notification is one delivered message. It carries everything the
// channel adapters need to render + transport, plus the metadata the
// future history-read API will return.
type Notification struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Type      EventType
	Category  Category
	Title     string
	Body      string
	Payload   map[string]any
	CreatedAt time.Time

	// Suppressed and SuppressedReason are set when the notification was not
	// attempted on any channel (preference/dedup/rate-limit). Delivered
	// notifications — even ones with partial per-channel failures — leave
	// these zero-valued: "suppressed" specifically means zero delivery
	// attempts were made, which is a distinct, auditable outcome from "we
	// tried and some channels failed" (see DeliveryOutcomes).
	Suppressed       bool              `json:"suppressed,omitempty"`
	SuppressedReason SuppressionReason `json:"suppressed_reason,omitempty"`
	// DeliveryOutcomes has one entry per channel actually attempted.
	DeliveryOutcomes []ChannelOutcome `json:"delivery_outcomes,omitempty"`
}

// DeviceToken is a mobile push destination registered by a user device.
type DeviceToken struct {
	ID         uuid.UUID `json:"id"`
	UserID     uuid.UUID `json:"user_id"`
	Token      string    `json:"token"`
	Platform   string    `json:"platform"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
	LastSeenAt time.Time `json:"last_seen_at,omitempty"`
}

// Channel is one transport adapter. Implementations must be safe to call
// concurrently — the dispatcher fans out events on a single goroutine
// today, but the contract reserves the right to parallelize later.
type Channel interface {
	Kind() ChannelKind
	Deliver(ctx context.Context, n Notification) error
}

// PreferenceStore is the seam the dispatcher uses to resolve a user's
// preferences. Production wiring reads from Postgres; tests pass a fake.
type PreferenceStore interface {
	Get(ctx context.Context, userID uuid.UUID) (Preferences, error)
}

// PersistenceStore is the seam for the eventual `notifications` table.
// MVP wiring passes a no-op store; the follow-up PR will swap in a
// Postgres-backed implementation along with the migration.
type PersistenceStore interface {
	Save(ctx context.Context, n Notification) error
}

// DeviceTokenStore lists active mobile push destinations for a user.
type DeviceTokenStore interface {
	ListDeviceTokens(ctx context.Context, userID uuid.UUID) ([]DeviceToken, error)
}

// NoopPersistenceStore is the MVP default — it discards. The dispatcher
// still calls Save so the wiring is exercised; replacing the store is a
// one-liner once the migration lands.
type NoopPersistenceStore struct{}

func (NoopPersistenceStore) Save(_ context.Context, _ Notification) error { return nil }

// RecordingPersistenceStore captures persisted notifications for tests. It
// also implements DeliveryOutcomeRecorder so tests can assert on recorded
// per-channel outcomes (#829).
type RecordingPersistenceStore struct {
	mu               sync.Mutex
	Saved            []Notification
	RecordedOutcomes map[uuid.UUID][]ChannelOutcome
}

func (r *RecordingPersistenceStore) Save(_ context.Context, n Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Saved = append(r.Saved, n)
	return nil
}

func (r *RecordingPersistenceStore) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.Saved)
}

// RecordOutcome implements DeliveryOutcomeRecorder.
func (r *RecordingPersistenceStore) RecordOutcome(_ context.Context, notificationID uuid.UUID, outcomes []ChannelOutcome) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.RecordedOutcomes == nil {
		r.RecordedOutcomes = make(map[uuid.UUID][]ChannelOutcome)
	}
	r.RecordedOutcomes[notificationID] = outcomes
	return nil
}

// Deduplicator suppresses repeat sends of the same logical event within a
// window (#829). SeenRecently records key on first call and returns false;
// subsequent calls for the same key within window return true without
// resetting the window's own expiry.
//
// This is a separate, generic mechanism from goal_milestone_notifier's
// notified_milestones table (see that file's doc comment), not a
// replacement for it — that table is a permanent, non-windowed
// source-of-truth dedup on a correctness-sensitive path (never send the
// same milestone twice, ever), whereas this is a short-window "don't fire
// the same logical event twice in a burst" primitive. Migrating
// notified_milestones onto this is exactly the kind of unreviewed,
// correctness-sensitive change this PR intentionally defers rather than
// risks — see the PR description.
type Deduplicator interface {
	SeenRecently(ctx context.Context, key string, window time.Duration) (bool, error)
}

// InMemoryDeduplicator is a process-local Deduplicator: the default when no
// Redis is configured. It does not coordinate across instances — a
// horizontally-scaled deployment should use RedisDeduplicator (see
// dedup_redis.go) instead.
type InMemoryDeduplicator struct {
	mu        sync.Mutex
	expiresAt map[string]time.Time
	nowFunc   func() time.Time
}

// NewInMemoryDeduplicator constructs an InMemoryDeduplicator.
func NewInMemoryDeduplicator() *InMemoryDeduplicator {
	return &InMemoryDeduplicator{expiresAt: make(map[string]time.Time), nowFunc: time.Now}
}

// SeenRecently implements Deduplicator.
func (d *InMemoryDeduplicator) SeenRecently(_ context.Context, key string, window time.Duration) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.nowFunc()
	if exp, ok := d.expiresAt[key]; ok && now.Before(exp) {
		return true, nil
	}
	d.expiresAt[key] = now.Add(window)

	// Opportunistic cleanup so this map does not grow unbounded across a
	// long process lifetime. Each key's own expiry is stored (rather than a
	// "last seen" timestamp compared against the current call's window), so
	// this is correct even when different calls use different windows.
	for k, exp := range d.expiresAt {
		if !now.Before(exp) {
			delete(d.expiresAt, k)
		}
	}
	return false, nil
}

// RateLimiter caps how often a given key may proceed within a window
// (#829). Deliberately structurally compatible with middleware.Limiter so
// middleware.NewLimiter(...) can be passed directly without notifications
// importing the middleware package.
type RateLimiter interface {
	Allow(ctx context.Context, key string) (allowed bool, retryAfter time.Duration)
}

// RetryEnqueuer schedules a durable retry for a channel delivery that
// failed on a retryable channel (#829). WebSocket delivery is intentionally
// never retried through this path — a socket that failed is, by
// definition, not connected right now, and the persisted notification row
// (always saved regardless of channel outcome) is the correct fallback for
// that case, not a queued retry. See retry_job.go for the jobqueue-backed
// implementation.
type RetryEnqueuer interface {
	EnqueueRetry(ctx context.Context, n Notification, channel ChannelKind) error
}

// CategoryStats is a point-in-time count of dispatcher activity for one
// category, exposed via Dispatcher.Stats() for a metrics endpoint to scrape
// (#829's "per-category delivery success rates exposed as metrics").
type CategoryStats struct {
	// Attempted counts Send calls that resulted in at least one delivery
	// attempt (i.e. were not suppressed).
	Attempted int64
	// Delivered/Failed count individual channel deliveries, not Send calls —
	// one Send can contribute to both if e.g. email fails and websocket
	// succeeds.
	Delivered int64
	Failed    int64
	// Suppressed counts Send calls suppressed before any delivery attempt
	// (by preference, dedup, or rate limit — see Notification.SuppressedReason
	// for the breakdown on individual records).
	Suppressed int64
}

// Dispatcher is the service the producers call. Construct with `New`
// and call `Send(ctx, userID, evt, title, body, payload)`.
type Dispatcher struct {
	channels    map[ChannelKind]Channel
	preferences PreferenceStore
	persistence PersistenceStore
	clock       func() time.Time

	dedup       Deduplicator
	rateLimiter RateLimiter
	retry       RetryEnqueuer

	statsMu sync.Mutex
	stats   map[Category]*CategoryStats
}

// DispatcherOption configures optional Dispatcher capabilities added in
// #829. Kept as functional options (rather than new New() parameters) so
// every existing New(channels, preferences, persistence) call site keeps
// compiling unchanged.
type DispatcherOption func(*Dispatcher)

// WithDeduplicator enables dedup for SendWithOptions calls that set a
// non-zero SendOptions.DedupWindow.
func WithDeduplicator(dedup Deduplicator) DispatcherOption {
	return func(d *Dispatcher) { d.dedup = dedup }
}

// WithRateLimiter enables per-user-per-category rate limiting. Safety-
// category events always bypass this.
func WithRateLimiter(rl RateLimiter) DispatcherOption {
	return func(d *Dispatcher) { d.rateLimiter = rl }
}

// WithRetryEnqueuer enables durable job-queue retry of failed Email/Push
// deliveries.
func WithRetryEnqueuer(r RetryEnqueuer) DispatcherOption {
	return func(d *Dispatcher) { d.retry = r }
}

// SetRetryEnqueuer wires (or replaces) the RetryEnqueuer after
// construction. Exists alongside WithRetryEnqueuer because some callers
// build their job-queue client after the dispatcher already exists (job
// queue setup naturally happens once, later, after other wiring) — see
// cmd/api/main.go. Like handler.SetWSHub elsewhere in this codebase, this
// is a startup-only wiring seam: call it once before the dispatcher starts
// serving Send calls, not concurrently with them.
func (d *Dispatcher) SetRetryEnqueuer(r RetryEnqueuer) {
	d.retry = r
}

// New constructs a Dispatcher with the given channel adapters. When
// `persistence` is nil, a NoopPersistenceStore is used.
func New(channels []Channel, preferences PreferenceStore, persistence PersistenceStore, opts ...DispatcherOption) *Dispatcher {
	d := &Dispatcher{
		channels:    make(map[ChannelKind]Channel, len(channels)),
		preferences: preferences,
		persistence: persistence,
		clock:       time.Now,
		stats:       make(map[Category]*CategoryStats),
	}
	if d.persistence == nil {
		d.persistence = NoopPersistenceStore{}
	}
	for _, c := range channels {
		d.channels[c.Kind()] = c
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Stats returns a snapshot of cumulative activity per category.
func (d *Dispatcher) Stats() map[Category]CategoryStats {
	d.statsMu.Lock()
	defer d.statsMu.Unlock()
	out := make(map[Category]CategoryStats, len(d.stats))
	for cat, s := range d.stats {
		out[cat] = *s
	}
	return out
}

// countFor returns (creating if needed) the counters for cat. Caller must
// hold d.statsMu.
func (d *Dispatcher) countFor(cat Category) *CategoryStats {
	s, ok := d.stats[cat]
	if !ok {
		s = &CategoryStats{}
		d.stats[cat] = s
	}
	return s
}

func (d *Dispatcher) recordAttempted(cat Category) {
	d.statsMu.Lock()
	defer d.statsMu.Unlock()
	d.countFor(cat).Attempted++
}

func (d *Dispatcher) recordSuppressed(cat Category) {
	d.statsMu.Lock()
	defer d.statsMu.Unlock()
	d.countFor(cat).Suppressed++
}

func (d *Dispatcher) recordChannelOutcome(cat Category, delivered bool) {
	d.statsMu.Lock()
	defer d.statsMu.Unlock()
	s := d.countFor(cat)
	if delivered {
		s.Delivered++
	} else {
		s.Failed++
	}
}

// SendOptions configures one Send call beyond the defaults (#829). The zero
// value disables dedup (DedupWindow <= 0 means "no dedup check").
type SendOptions struct {
	// DedupKey optionally scopes dedup more specifically than userID+evt
	// alone (e.g. a specific vault ID within EventVaultAPYDrop). Empty means
	// dedup solely on userID+evt.
	DedupKey string
	// DedupWindow, if > 0 (and a Deduplicator is configured via
	// WithDeduplicator), suppresses a repeat Send for the same
	// (userID, evt, DedupKey) within this window.
	DedupWindow time.Duration
}

// Send dispatches the event to every channel the matrix says should
// carry it, filtered by the user's preferences. Equivalent to
// SendWithOptions with the zero SendOptions (no dedup).
func (d *Dispatcher) Send(
	ctx context.Context,
	userID uuid.UUID,
	evt EventType,
	title string,
	body string,
	payload map[string]any,
) error {
	return d.SendWithOptions(ctx, userID, evt, title, body, payload, SendOptions{})
}

// SendWithOptions is Send with dedup control (#829). See SendOptions.
//
// Behavior:
//   - Category (CategoryFor(evt)) drives suppressibility: CategorySafety
//     bypasses preference and rate-limit checks entirely (dedup still
//     applies — that's "don't repeat the identical alert", a different
//     concern from opt-out).
//   - Dedup and rate-limit checks run before anything else; a suppressed
//     Send is still persisted (Notification.Suppressed = true with a
//     SuppressedReason) so it is auditable, never silently dropped.
//   - Delivery is durability-first: the notification is persisted BEFORE
//     any channel.Deliver call, so a persistence failure means nothing was
//     "delivered" that wasn't durably recorded first.
//   - Every eligible channel is attempted independently — a failed email
//     never blocks the websocket fan-out, and vice versa (this dispatcher
//     intentionally delivers to every eligible channel simultaneously
//     rather than a single preferred channel, since e.g.
//     EventSettlementCompleted wants email+push+in-app together, not
//     "whichever one works"). Per-channel fallback still applies: if Push
//     or Email fails, WebSocket is attempted as a live fallback (skipped if
//     already attempted as part of the normal matrix). Failed Email/Push
//     deliveries are hard-routed through the durable job queue for retry,
//     if a RetryEnqueuer is configured.
//   - Every attempted channel's outcome is recorded in
//     Notification.DeliveryOutcomes and (if the persistence store also
//     implements DeliveryOutcomeRecorder) persisted as a best-effort
//     follow-up update after delivery completes.
func (d *Dispatcher) SendWithOptions(
	ctx context.Context,
	userID uuid.UUID,
	evt EventType,
	title string,
	body string,
	payload map[string]any,
	opts SendOptions,
) error {
	category := CategoryFor(evt)

	if opts.DedupWindow > 0 && d.dedup != nil {
		key := userID.String() + ":" + string(evt) + ":" + opts.DedupKey
		if seen, err := d.dedup.SeenRecently(ctx, key, opts.DedupWindow); err == nil && seen {
			return d.suppress(ctx, userID, evt, category, title, body, payload, SuppressedByDedup)
		}
	}

	if category != CategorySafety && d.rateLimiter != nil {
		key := userID.String() + ":" + string(category)
		if allowed, _ := d.rateLimiter.Allow(ctx, key); !allowed {
			return d.suppress(ctx, userID, evt, category, title, body, payload, SuppressedByRateLimit)
		}
	}

	prefs, err := d.resolvePreferences(ctx, userID, category)
	if err != nil {
		return fmt.Errorf("notifications: load preferences for %s: %w", userID, err)
	}

	matrix := ChannelsFor(evt)
	if len(matrix) == 0 {
		return fmt.Errorf("notifications: unknown event type %q", evt)
	}

	var toAttempt []ChannelKind
	for _, kind := range matrix {
		if category == CategorySafety || prefs.Allow(kind) {
			toAttempt = append(toAttempt, kind)
		}
	}

	n := Notification{
		ID:        uuid.New(),
		UserID:    userID,
		Type:      evt,
		Category:  category,
		Title:     title,
		Body:      body,
		Payload:   payload,
		CreatedAt: d.clock(),
	}

	if len(toAttempt) == 0 {
		n.Suppressed = true
		n.SuppressedReason = SuppressedByPreference
		d.recordSuppressed(category)
		if err := d.persistence.Save(ctx, n); err != nil {
			return fmt.Errorf("notifications: persist suppressed %s: %w", n.ID, err)
		}
		return nil
	}

	// Durability-first: persist before any delivery attempt. If this fails
	// we must not pretend anything was delivered.
	if err := d.persistence.Save(ctx, n); err != nil {
		return fmt.Errorf("notifications: persist %s: %w", n.ID, err)
	}

	attempted := make(map[ChannelKind]bool, len(toAttempt)+1)
	var outcomes []ChannelOutcome
	var joined []error

	deliver := func(kind ChannelKind, isFallback bool) bool {
		if attempted[kind] {
			return false
		}
		attempted[kind] = true

		ch, ok := d.channels[kind]
		if !ok {
			joined = append(joined, fmt.Errorf("notifications: no adapter for channel %q", kind))
			outcomes = append(outcomes, ChannelOutcome{Channel: kind, Delivered: false, Error: "no adapter registered", IsFallback: isFallback})
			d.recordChannelOutcome(category, false)
			return false
		}

		deliverErr := ch.Deliver(ctx, n)
		outcome := ChannelOutcome{Channel: kind, Delivered: deliverErr == nil, IsFallback: isFallback}
		if deliverErr != nil {
			outcome.Error = deliverErr.Error()
			joined = append(joined, fmt.Errorf("notifications: channel %q deliver: %w", kind, deliverErr))
		}
		outcomes = append(outcomes, outcome)
		d.recordChannelOutcome(category, deliverErr == nil)
		return deliverErr == nil
	}

	for _, kind := range toAttempt {
		if ok := deliver(kind, false); ok {
			continue
		}
		if fb := fallbackChannel(kind); fb != "" {
			deliver(fb, true)
		}
		if (kind == ChannelEmail || kind == ChannelPush) && d.retry != nil {
			if err := d.retry.EnqueueRetry(ctx, n, kind); err != nil {
				joined = append(joined, fmt.Errorf("notifications: enqueue retry for %q: %w", kind, err))
			}
		}
	}

	d.recordAttempted(category)
	n.DeliveryOutcomes = outcomes
	if rec, ok := d.persistence.(DeliveryOutcomeRecorder); ok {
		if err := rec.RecordOutcome(ctx, n.ID, outcomes); err != nil {
			// Best-effort: the notification is already durably saved above,
			// so losing this update is an audit/metrics gap, not a lost
			// notification — it does not fail the Send call.
			joined = append(joined, fmt.Errorf("notifications: record delivery outcome: %w", err))
		}
	}

	if len(joined) > 0 {
		return errors.Join(joined...)
	}
	return nil
}

// resolvePreferences uses category-scoped preferences when the configured
// PreferenceStore supports it (see CategoryPreferenceStore), falling back
// to the flat PreferenceStore.Get for stores that don't.
func (d *Dispatcher) resolvePreferences(ctx context.Context, userID uuid.UUID, category Category) (Preferences, error) {
	if cp, ok := d.preferences.(CategoryPreferenceStore); ok {
		return cp.GetForCategory(ctx, userID, category)
	}
	return d.preferences.Get(ctx, userID)
}

// suppress persists a Notification marked Suppressed with the given reason,
// without attempting any delivery. Shared by the dedup, rate-limit, and
// preference suppression paths so "record a suppressed notification" has
// exactly one implementation.
func (d *Dispatcher) suppress(
	ctx context.Context,
	userID uuid.UUID,
	evt EventType,
	category Category,
	title, body string,
	payload map[string]any,
	reason SuppressionReason,
) error {
	n := Notification{
		ID:               uuid.New(),
		UserID:           userID,
		Type:             evt,
		Category:         category,
		Title:            title,
		Body:             body,
		Payload:          payload,
		CreatedAt:        d.clock(),
		Suppressed:       true,
		SuppressedReason: reason,
	}
	d.recordSuppressed(category)
	if err := d.persistence.Save(ctx, n); err != nil {
		return fmt.Errorf("notifications: persist suppressed %s: %w", n.ID, err)
	}
	return nil
}

// fallbackChannel returns the next channel to try when kind fails, or ""
// if kind has no further fallback. WebSocket is the terminal live channel —
// beyond it, the persisted notification row (always saved regardless of
// channel outcome, above) is the true final fallback, not another live
// channel, matching "in-app is always available as the terminal fallback
// since it is just a database row the client reads."
func fallbackChannel(kind ChannelKind) ChannelKind {
	switch kind {
	case ChannelPush, ChannelEmail:
		return ChannelWebSocket
	default:
		return ""
	}
}

// RedeliverChannel attempts delivery of n on exactly one channel, bypassing
// preferences/dedup/rate-limiting (those already ran on the original Send
// call that enqueued the retry) and stats recording (the job queue's own
// success/failure signal is authoritative for a retry attempt). Used by the
// notification-retry job handler in retry_job.go.
func (d *Dispatcher) RedeliverChannel(ctx context.Context, n Notification, channel ChannelKind) error {
	ch, ok := d.channels[channel]
	if !ok {
		return fmt.Errorf("notifications: no adapter for channel %q", channel)
	}
	return ch.Deliver(ctx, n)
}

// DeliveryOutcomeRecorder is an optional upgrade to PersistenceStore
// (#829): a store that can record per-channel delivery outcomes for an
// already-saved notification. The dispatcher type-asserts for this after
// delivery completes; a store that only implements PersistenceStore keeps
// working exactly as before (the notification is saved once, pre-delivery,
// with no outcome update).
type DeliveryOutcomeRecorder interface {
	RecordOutcome(ctx context.Context, notificationID uuid.UUID, outcomes []ChannelOutcome) error
}

// --- Concrete channels (MVP) ---

// MailSender is the seam between the email channel and whichever SMTP
// or transactional-email provider is configured. The MVP includes a
// `RecordingMailSender` for tests; the follow-up PR will wire `net/smtp`
// or a SendGrid/Resend client behind the same interface.
type MailSender interface {
	Send(ctx context.Context, to string, subject string, body string) error
}

// EmailLookup returns the destination email for the given user. The
// production wiring reads from the `users` table; tests pass a fake.
type EmailLookup interface {
	EmailFor(ctx context.Context, userID uuid.UUID) (string, error)
}

// EmailChannel is the email transport adapter.
type EmailChannel struct {
	sender MailSender
	lookup EmailLookup
}

// NewEmailChannel constructs an EmailChannel.
func NewEmailChannel(sender MailSender, lookup EmailLookup) *EmailChannel {
	return &EmailChannel{sender: sender, lookup: lookup}
}

// Kind reports ChannelEmail.
func (c *EmailChannel) Kind() ChannelKind { return ChannelEmail }

// Deliver looks up the user's email and hands the rendered message to
// the underlying MailSender.
func (c *EmailChannel) Deliver(ctx context.Context, n Notification) error {
	to, err := c.lookup.EmailFor(ctx, n.UserID)
	if err != nil {
		return err
	}
	return c.sender.Send(ctx, to, n.Title, n.Body)
}

// WebSocketHub is the seam between the websocket channel and the
// connected-client hub. The repo's existing internal/ws hub will
// satisfy this when wired up in the follow-up handler PR.
type WebSocketHub interface {
	PushToUser(ctx context.Context, userID uuid.UUID, eventName string, payload any) error
}

// WebSocketChannel is the websocket transport adapter.
type WebSocketChannel struct {
	hub WebSocketHub
}

// NewWebSocketChannel constructs a WebSocketChannel.
func NewWebSocketChannel(hub WebSocketHub) *WebSocketChannel {
	return &WebSocketChannel{hub: hub}
}

// Kind reports ChannelWebSocket.
func (c *WebSocketChannel) Kind() ChannelKind { return ChannelWebSocket }

// Deliver pushes a JSON `notification` event to the user's connected
// clients via the hub.
func (c *WebSocketChannel) Deliver(ctx context.Context, n Notification) error {
	return c.hub.PushToUser(ctx, n.UserID, "notification", n)
}

// PushSender is the provider seam for FCM, Expo, or any other mobile push
// transport. The API stores device tokens; the concrete sender owns provider
// credentials and delivery details.
type PushSender interface {
	Send(ctx context.Context, tokens []string, title string, body string, payload map[string]any) error
}

// PushChannel sends notifications to every active device token for the user.
type PushChannel struct {
	sender PushSender
	tokens DeviceTokenStore
}

// NewPushChannel constructs a push notification transport adapter.
func NewPushChannel(sender PushSender, tokens DeviceTokenStore) *PushChannel {
	return &PushChannel{sender: sender, tokens: tokens}
}

// Kind reports ChannelPush.
func (c *PushChannel) Kind() ChannelKind { return ChannelPush }

// Deliver looks up active device tokens and sends one provider request.
func (c *PushChannel) Deliver(ctx context.Context, n Notification) error {
	devices, err := c.tokens.ListDeviceTokens(ctx, n.UserID)
	if err != nil {
		return err
	}

	tokens := make([]string, 0, len(devices))
	for _, device := range devices {
		if !device.Enabled || device.Token == "" {
			continue
		}
		tokens = append(tokens, device.Token)
	}
	if len(tokens) == 0 {
		return nil
	}

	return c.sender.Send(ctx, tokens, n.Title, n.Body, n.Payload)
}

// --- Test doubles for use by external callers' integration tests ---

// RecordingMailSender captures every send. Safe for concurrent use.
type RecordingMailSender struct {
	mu    sync.Mutex
	Calls []RecordedMail
}

// RecordedMail is one captured Send call.
type RecordedMail struct {
	To, Subject, Body string
}

// Send records the call.
func (r *RecordingMailSender) Send(_ context.Context, to, subject, body string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls = append(r.Calls, RecordedMail{To: to, Subject: subject, Body: body})
	return nil
}

// RecordingHub captures every push.
type RecordingHub struct {
	mu    sync.Mutex
	Calls []RecordedPush
}

// RecordedPush is one captured PushToUser call.
type RecordedPush struct {
	UserID    uuid.UUID
	EventName string
	Payload   any
}

// PushToUser records the call.
func (r *RecordingHub) PushToUser(_ context.Context, userID uuid.UUID, eventName string, payload any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls = append(r.Calls, RecordedPush{UserID: userID, EventName: eventName, Payload: payload})
	return nil
}

// NoopPushSender discards push provider requests.
type NoopPushSender struct{}

func (NoopPushSender) Send(context.Context, []string, string, string, map[string]any) error {
	return nil
}

// RecordingPushSender captures every push send. Safe for concurrent use.
type RecordingPushSender struct {
	mu    sync.Mutex
	Calls []RecordedPushNotification
}

// RecordedPushNotification is one captured push provider call.
type RecordedPushNotification struct {
	Tokens  []string
	Title   string
	Body    string
	Payload map[string]any
}

// Send records the push request.
func (r *RecordingPushSender) Send(_ context.Context, tokens []string, title string, body string, payload map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	copiedTokens := make([]string, len(tokens))
	copy(copiedTokens, tokens)
	r.Calls = append(r.Calls, RecordedPushNotification{
		Tokens:  copiedTokens,
		Title:   title,
		Body:    body,
		Payload: payload,
	})
	return nil
}

// CallCount returns the number of captured push sends.
func (r *RecordingPushSender) CallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.Calls)
}

// SnapshotCalls returns a copy of captured push sends.
func (r *RecordingPushSender) SnapshotCalls() []RecordedPushNotification {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RecordedPushNotification, len(r.Calls))
	copy(out, r.Calls)
	return out
}

// MemoryDeviceTokens is an in-memory DeviceTokenStore for tests.
type MemoryDeviceTokens struct {
	mu     sync.Mutex
	tokens map[uuid.UUID][]DeviceToken
}

// NewMemoryDeviceTokens returns an empty in-memory token store.
func NewMemoryDeviceTokens() *MemoryDeviceTokens {
	return &MemoryDeviceTokens{tokens: map[uuid.UUID][]DeviceToken{}}
}

// ListDeviceTokens implements DeviceTokenStore.
func (m *MemoryDeviceTokens) ListDeviceTokens(_ context.Context, userID uuid.UUID) ([]DeviceToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	items := m.tokens[userID]
	out := make([]DeviceToken, len(items))
	copy(out, items)
	return out, nil
}

// Set replaces a user's device tokens. Returns the receiver for chaining.
func (m *MemoryDeviceTokens) Set(userID uuid.UUID, tokens []DeviceToken) *MemoryDeviceTokens {
	m.mu.Lock()
	defer m.mu.Unlock()

	copied := make([]DeviceToken, len(tokens))
	copy(copied, tokens)
	m.tokens[userID] = copied
	return m
}

// StaticEmailLookup is a fake EmailLookup that returns a fixed address.
type StaticEmailLookup struct{ Addr string }

// EmailFor returns the static address.
func (s StaticEmailLookup) EmailFor(_ context.Context, _ uuid.UUID) (string, error) {
	return s.Addr, nil
}

// MemoryPreferences is an in-memory PreferenceStore.
type MemoryPreferences struct {
	mu    sync.Mutex
	Prefs map[uuid.UUID]Preferences
}

// NewMemoryPreferences returns an empty store; missing users get DefaultPreferences().
func NewMemoryPreferences() *MemoryPreferences {
	return &MemoryPreferences{Prefs: map[uuid.UUID]Preferences{}}
}

// Get implements PreferenceStore.
func (m *MemoryPreferences) Get(_ context.Context, userID uuid.UUID) (Preferences, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.Prefs[userID]; ok {
		return p, nil
	}
	return DefaultPreferences(), nil
}

// Set replaces a user's preferences. Returns the receiver for chaining.
func (m *MemoryPreferences) Set(userID uuid.UUID, p Preferences) *MemoryPreferences {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Prefs[userID] = p
	return m
}
