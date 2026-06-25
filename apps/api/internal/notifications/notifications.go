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
	EventSettlementCompleted EventType = "settlement_completed"
	EventSettlementFailed    EventType = "settlement_failed"
	EventDepositConfirmed    EventType = "deposit_confirmed"
	EventYieldMilestone      EventType = "yield_milestone"
	EventVaultAPYDrop        EventType = "vault_apy_drop"
	EventVaultPaused         EventType = "vault_paused"
	EventRebalanceExecuted   EventType = "rebalance_executed"
	EventKYCApproved         EventType = "kyc_approved"
	EventKYCRejected         EventType = "kyc_rejected"
	EventGoalMilestone       EventType = "goal_milestone"
)

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
	EventSettlementCompleted: {ChannelEmail, ChannelWebSocket, ChannelPush},
	EventSettlementFailed:    {ChannelEmail, ChannelWebSocket, ChannelPush},
	EventDepositConfirmed:    {ChannelEmail, ChannelWebSocket, ChannelPush},
	EventYieldMilestone:      {ChannelPush},
	EventVaultAPYDrop:        {ChannelEmail},
	EventVaultPaused:         {ChannelEmail, ChannelWebSocket},
	EventRebalanceExecuted:   {ChannelWebSocket},
	EventKYCApproved:         {ChannelEmail},
	EventKYCRejected:         {ChannelEmail},
	EventGoalMilestone:       {ChannelPush},
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
}

// DefaultPreferences returns the "everything on" baseline new users get
// before they explicitly opt out.
func DefaultPreferences() Preferences { return Preferences{Email: true, WebSocket: true, Push: true} }

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

// Notification is one delivered message. It carries everything the
// channel adapters need to render + transport, plus the metadata the
// future history-read API will return.
type Notification struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Type      EventType
	Title     string
	Body      string
	Payload   map[string]any
	CreatedAt time.Time
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

// RecordingPersistenceStore captures persisted notifications for tests.
type RecordingPersistenceStore struct {
	mu    sync.Mutex
	Saved []Notification
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

// Dispatcher is the service the producers call. Construct with `New`
// and call `Send(ctx, userID, evt, title, body, payload)`.
type Dispatcher struct {
	channels    map[ChannelKind]Channel
	preferences PreferenceStore
	persistence PersistenceStore
	clock       func() time.Time
}

// New constructs a Dispatcher with the given channel adapters. When
// `persistence` is nil, a NoopPersistenceStore is used.
func New(channels []Channel, preferences PreferenceStore, persistence PersistenceStore) *Dispatcher {
	d := &Dispatcher{
		channels:    make(map[ChannelKind]Channel, len(channels)),
		preferences: preferences,
		persistence: persistence,
		clock:       time.Now,
	}
	if d.persistence == nil {
		d.persistence = NoopPersistenceStore{}
	}
	for _, c := range channels {
		d.channels[c.Kind()] = c
	}
	return d
}

// Send dispatches the event to every channel the matrix says should
// carry it, filtered by the user's preferences.
//
// The dispatcher returns the first delivery error it encounters but
// always attempts every eligible channel — so a failed email never
// blocks the websocket fan-out, and vice versa. The returned error is
// joined for visibility but the dispatcher does NOT retry; retry is a
// follow-up.
func (d *Dispatcher) Send(
	ctx context.Context,
	userID uuid.UUID,
	evt EventType,
	title string,
	body string,
	payload map[string]any,
) error {
	prefs, err := d.preferences.Get(ctx, userID)
	if err != nil {
		return fmt.Errorf("notifications: load preferences for %s: %w", userID, err)
	}

	channels := ChannelsFor(evt)
	if len(channels) == 0 {
		return fmt.Errorf("notifications: unknown event type %q", evt)
	}

	n := Notification{
		ID:        uuid.New(),
		UserID:    userID,
		Type:      evt,
		Title:     title,
		Body:      body,
		Payload:   payload,
		CreatedAt: d.clock(),
	}

	if err := d.persistence.Save(ctx, n); err != nil {
		// We intentionally surface this — if we can't record the
		// notification, we shouldn't pretend we delivered it either.
		return fmt.Errorf("notifications: persist %s: %w", n.ID, err)
	}

	var joined []error
	for _, kind := range channels {
		if !prefs.Allow(kind) {
			continue
		}
		ch, ok := d.channels[kind]
		if !ok {
			joined = append(joined, fmt.Errorf("notifications: no adapter for channel %q", kind))
			continue
		}
		if err := ch.Deliver(ctx, n); err != nil {
			joined = append(joined, fmt.Errorf("notifications: channel %q deliver: %w", kind, err))
		}
	}
	if len(joined) > 0 {
		return errors.Join(joined...)
	}
	return nil
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
