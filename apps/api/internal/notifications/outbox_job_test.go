package notifications

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
)

func sendJob(t *testing.T, p NotificationSendPayload) jobqueue.Job {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return jobqueue.Job{ID: uuid.New(), Type: NotificationSendJobType, Payload: raw}
}

func dispatcherForOutbox(t *testing.T) (*Dispatcher, *RecordingHub, *RecordingPersistenceStore) {
	t.Helper()
	hub := &RecordingHub{}
	store := &RecordingPersistenceStore{}
	d := New(
		[]Channel{NewWebSocketChannel(hub)},
		NewMemoryPreferences(),
		store,
		WithDeduplicator(NewInMemoryDeduplicator()),
	)
	return d, hub, store
}

func TestNotificationSendJobHandler_Dispatches(t *testing.T) {
	d, hub, _ := dispatcherForOutbox(t)
	h := NewNotificationSendJobHandler(d)
	userID := uuid.New()

	err := h.Handle(context.Background(), sendJob(t, NotificationSendPayload{
		UserID:    userID,
		EventType: EventRebalanceExecuted,
		Title:     "Rebalanced",
		Body:      "Your vault was rebalanced.",
		DedupeKey: "vault:v1:rebalance:7",
	}))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(hub.Calls) != 1 {
		t.Fatalf("delivered %d notifications, want 1", len(hub.Calls))
	}
}

// TestNotificationSendJobHandler_RedeliveryIsDedupedNotDuplicated is what
// makes at-least-once tolerable for a human-visible side effect: the queue
// may run this twice, but the user must not see the message twice.
func TestNotificationSendJobHandler_RedeliveryIsDedupedNotDuplicated(t *testing.T) {
	d, hub, store := dispatcherForOutbox(t)
	h := NewNotificationSendJobHandler(d)
	job := sendJob(t, NotificationSendPayload{
		UserID:    uuid.New(),
		EventType: EventRebalanceExecuted,
		Title:     "Rebalanced",
		Body:      "Your vault was rebalanced.",
		DedupeKey: "vault:v1:rebalance:7",
	})

	ctx := context.Background()
	if err := h.Handle(ctx, job); err != nil {
		t.Fatalf("first Handle: %v", err)
	}
	if err := h.Handle(ctx, job); err != nil {
		t.Fatalf("redelivery Handle: %v", err)
	}

	if len(hub.Calls) != 1 {
		t.Fatalf("delivered %d notifications for one event, want 1", len(hub.Calls))
	}
	// The suppressed repeat is still recorded, so a redelivery is auditable
	// rather than invisible.
	if store.Count() != 2 {
		t.Fatalf("persisted %d notifications, want 2 (one delivered, one suppressed)", store.Count())
	}
	suppressed := store.Saved[1]
	if !suppressed.Suppressed || suppressed.SuppressedReason != SuppressedByDedup {
		t.Fatalf("second notification = %+v, want suppressed by dedup", suppressed)
	}
}

// TestNotificationSendJobHandler_DifferentDedupeKeysBothDeliver guards
// against the dedupe window swallowing genuinely distinct events.
func TestNotificationSendJobHandler_DifferentDedupeKeysBothDeliver(t *testing.T) {
	d, hub, _ := dispatcherForOutbox(t)
	h := NewNotificationSendJobHandler(d)
	userID := uuid.New()
	ctx := context.Background()

	for _, key := range []string{"goal:g1:milestone:25", "goal:g1:milestone:50"} {
		err := h.Handle(ctx, sendJob(t, NotificationSendPayload{
			UserID: userID, EventType: EventRebalanceExecuted,
			Title: "t", Body: "b", DedupeKey: key,
		}))
		if err != nil {
			t.Fatalf("Handle(%s): %v", key, err)
		}
	}
	if len(hub.Calls) != 2 {
		t.Fatalf("delivered %d notifications, want 2", len(hub.Calls))
	}
}

func TestNotificationSendJobHandler_MalformedPayloadIsPermanent(t *testing.T) {
	d, _, _ := dispatcherForOutbox(t)
	h := NewNotificationSendJobHandler(d)

	err := h.Handle(context.Background(), jobqueue.Job{Payload: json.RawMessage(`{oops`)})
	if !jobqueue.IsPermanent(err) {
		t.Fatalf("error = %v, want permanent — retrying cannot fix a malformed payload", err)
	}
}

func TestNotificationSendJobHandler_MissingDedupeKeyIsPermanent(t *testing.T) {
	d, _, _ := dispatcherForOutbox(t)
	h := NewNotificationSendJobHandler(d)

	err := h.Handle(context.Background(), sendJob(t, NotificationSendPayload{
		UserID: uuid.New(), EventType: EventRebalanceExecuted, Title: "t", Body: "b",
	}))
	if !jobqueue.IsPermanent(err) {
		t.Fatalf("error = %v, want permanent — without a key a redelivery would show twice", err)
	}
}

// TestNotificationSendJobHandler_UnknownEventTypeIsPermanent: an event the
// channel matrix does not know routes nowhere, and retrying will not teach
// it a route — it must dead-letter rather than block its aggregate.
func TestNotificationSendJobHandler_UnknownEventTypeIsPermanent(t *testing.T) {
	d, _, _ := dispatcherForOutbox(t)
	h := NewNotificationSendJobHandler(d)

	err := h.Handle(context.Background(), sendJob(t, NotificationSendPayload{
		UserID: uuid.New(), EventType: EventType("not_a_real_event"),
		Title: "t", Body: "b", DedupeKey: "k",
	}))
	if !jobqueue.IsPermanent(err) {
		t.Fatalf("error = %v, want permanent", err)
	}
}

func TestNotificationSendJobHandler_NilDispatcherIsPermanent(t *testing.T) {
	h := NewNotificationSendJobHandler(nil)
	err := h.Handle(context.Background(), sendJob(t, NotificationSendPayload{DedupeKey: "k"}))
	if !jobqueue.IsPermanent(err) {
		t.Fatalf("error = %v, want permanent", err)
	}
}

func TestNewNotificationSendEvent_CarriesRoutingAndDedupeKey(t *testing.T) {
	userID := uuid.New()
	e, err := NewNotificationSendEvent("savings_goal", "goal-1", userID,
		EventGoalMilestone, "Halfway there!", "You hit 50%.",
		map[string]any{"milestone": 50}, "savings_goal:goal-1:milestone:50")
	if err != nil {
		t.Fatalf("NewNotificationSendEvent: %v", err)
	}
	if e.EventType != OutboxEventNotificationSend {
		t.Fatalf("event type = %q, want %q", e.EventType, OutboxEventNotificationSend)
	}
	if e.AggregateType != "savings_goal" || e.AggregateID != "goal-1" {
		t.Fatalf("aggregate = %s/%s, want savings_goal/goal-1", e.AggregateType, e.AggregateID)
	}

	var p NotificationSendPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.UserID != userID || p.Title != "Halfway there!" || p.DedupeKey != e.DedupeKey {
		t.Fatalf("payload = %+v, want the rendered copy and dedupe key carried through", p)
	}
}
