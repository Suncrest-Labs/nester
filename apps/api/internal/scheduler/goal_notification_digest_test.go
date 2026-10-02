package scheduler

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/goalnotification"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

// fakeGoalDigestStore is an in-memory stand-in for GoalDigestStore, keyed
// the same way the real postgres repository is (per goal_id), so these
// tests exercise the job's own batching logic rather than a persistence
// layer.
type fakeGoalDigestStore struct {
	due           []goalnotification.Preference
	queuedByGoal  map[uuid.UUID][]goalnotification.DigestItem
	clearedByGoal map[uuid.UUID][]uuid.UUID
	sentAtByGoal  map[uuid.UUID]time.Time
	listDueErr    error
}

func newFakeGoalDigestStore() *fakeGoalDigestStore {
	return &fakeGoalDigestStore{
		queuedByGoal:  make(map[uuid.UUID][]goalnotification.DigestItem),
		clearedByGoal: make(map[uuid.UUID][]uuid.UUID),
		sentAtByGoal:  make(map[uuid.UUID]time.Time),
	}
}

func (f *fakeGoalDigestStore) ListDue(context.Context, time.Time) ([]goalnotification.Preference, error) {
	if f.listDueErr != nil {
		return nil, f.listDueErr
	}
	return f.due, nil
}

func (f *fakeGoalDigestStore) ListQueuedItems(_ context.Context, goalID uuid.UUID) ([]goalnotification.DigestItem, error) {
	return f.queuedByGoal[goalID], nil
}

func (f *fakeGoalDigestStore) ClearQueuedItems(_ context.Context, goalID uuid.UUID, itemIDs []uuid.UUID) error {
	f.clearedByGoal[goalID] = itemIDs
	return nil
}

func (f *fakeGoalDigestStore) MarkDigestSent(_ context.Context, goalID uuid.UUID, sentAt time.Time) error {
	f.sentAtByGoal[goalID] = sentAt
	return nil
}

// recordingChannel is a minimal in-memory notifications.Channel that
// records every delivered notification, so these tests can assert on how
// many separate sends reached a user without a real push/email provider or
// a database-backed PreferenceStore/PersistenceStore.
type recordingChannel struct {
	kind notifications.ChannelKind
	mu   sync.Mutex
	sent []notifications.Notification
}

func (c *recordingChannel) Kind() notifications.ChannelKind { return c.kind }

func (c *recordingChannel) Deliver(_ context.Context, n notifications.Notification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, n)
	return nil
}

func (c *recordingChannel) sentToUser(userID uuid.UUID) []notifications.Notification {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []notifications.Notification
	for _, n := range c.sent {
		if n.UserID == userID {
			out = append(out, n)
		}
	}
	return out
}

// allowAllPreferenceStore permits every channel for every user, so these
// tests exercise the digest job's own batching rather than preference
// filtering.
type allowAllPreferenceStore struct{}

func (allowAllPreferenceStore) Get(context.Context, uuid.UUID) (notifications.Preferences, error) {
	return notifications.DefaultPreferences(), nil
}

func newTestDispatcher(channel *recordingChannel) *notifications.Dispatcher {
	return notifications.New([]notifications.Channel{channel}, allowAllPreferenceStore{}, notifications.NoopPersistenceStore{})
}

func digestItem(goalID, userID uuid.UUID, body string) goalnotification.DigestItem {
	return goalnotification.DigestItem{ID: uuid.New(), GoalID: goalID, UserID: userID, Body: body, CreatedAt: time.Now().UTC()}
}

// TestTick_BatchesMultipleDueGoalsForTheSameUserIntoOneSend covers
// nester#1340: a user with two goals both due for a daily digest in the
// same pass previously received two separate "Savings goal digest" sends.
// They must now receive exactly one, covering both goals.
func TestTick_BatchesMultipleDueGoalsForTheSameUserIntoOneSend(t *testing.T) {
	userID := uuid.New()
	goalA, goalB := uuid.New(), uuid.New()

	store := newFakeGoalDigestStore()
	store.due = []goalnotification.Preference{
		{GoalID: goalA, UserID: userID, DigestFrequency: goalnotification.FrequencyDaily},
		{GoalID: goalB, UserID: userID, DigestFrequency: goalnotification.FrequencyDaily},
	}
	store.queuedByGoal[goalA] = []goalnotification.DigestItem{digestItem(goalA, userID, "goal A milestone")}
	store.queuedByGoal[goalB] = []goalnotification.DigestItem{digestItem(goalB, userID, "goal B milestone")}

	channel := &recordingChannel{kind: notifications.ChannelPush}
	job := NewGoalNotificationDigestJob(GoalNotificationDigestConfig{Enabled: true}, store, newTestDispatcher(channel), nil)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	job.SetClock(func() time.Time { return now })

	job.Tick(context.Background())

	sent := channel.sentToUser(userID)
	if len(sent) != 1 {
		t.Fatalf("expected exactly 1 digest notification for the user, got %d", len(sent))
	}
	if !strings.Contains(sent[0].Body, "goal A milestone") || !strings.Contains(sent[0].Body, "goal B milestone") {
		t.Errorf("expected the single digest to mention both goals' items, got body: %q", sent[0].Body)
	}

	// Both goals' queues must still be individually cleared and marked sent.
	if len(store.clearedByGoal[goalA]) != 1 || len(store.clearedByGoal[goalB]) != 1 {
		t.Errorf("expected both goals' queues cleared, got cleared=%v", store.clearedByGoal)
	}
	if store.sentAtByGoal[goalA] != now || store.sentAtByGoal[goalB] != now {
		t.Errorf("expected both goals marked sent at %v, got %v", now, store.sentAtByGoal)
	}
}

// TestTick_DoesNotMixItemsFromDifferentUsers confirms batching groups
// strictly by UserID: two different users' due goals must still produce two
// separate sends, each containing only that user's own items.
func TestTick_DoesNotMixItemsFromDifferentUsers(t *testing.T) {
	userA, userB := uuid.New(), uuid.New()
	goalA, goalB := uuid.New(), uuid.New()

	store := newFakeGoalDigestStore()
	store.due = []goalnotification.Preference{
		{GoalID: goalA, UserID: userA, DigestFrequency: goalnotification.FrequencyDaily},
		{GoalID: goalB, UserID: userB, DigestFrequency: goalnotification.FrequencyDaily},
	}
	store.queuedByGoal[goalA] = []goalnotification.DigestItem{digestItem(goalA, userA, "user A's item")}
	store.queuedByGoal[goalB] = []goalnotification.DigestItem{digestItem(goalB, userB, "user B's item")}

	channel := &recordingChannel{kind: notifications.ChannelPush}
	job := NewGoalNotificationDigestJob(GoalNotificationDigestConfig{Enabled: true}, store, newTestDispatcher(channel), nil)
	job.Tick(context.Background())

	sentA := channel.sentToUser(userA)
	sentB := channel.sentToUser(userB)
	if len(sentA) != 1 || len(sentB) != 1 {
		t.Fatalf("expected exactly 1 send per user, got userA=%d userB=%d", len(sentA), len(sentB))
	}
	if strings.Contains(sentA[0].Body, "user B's item") {
		t.Errorf("user A's digest must not contain user B's item, got body: %q", sentA[0].Body)
	}
	if strings.Contains(sentB[0].Body, "user A's item") {
		t.Errorf("user B's digest must not contain user A's item, got body: %q", sentB[0].Body)
	}
}

// TestTick_SkipsGoalsWithNoQueuedItems confirms a due preference whose
// queue has since emptied (e.g. raced by another flush) contributes nothing
// to the user's batched send instead of adding an empty entry.
func TestTick_SkipsGoalsWithNoQueuedItems(t *testing.T) {
	userID := uuid.New()
	emptyGoal, realGoal := uuid.New(), uuid.New()

	store := newFakeGoalDigestStore()
	store.due = []goalnotification.Preference{
		{GoalID: emptyGoal, UserID: userID, DigestFrequency: goalnotification.FrequencyDaily},
		{GoalID: realGoal, UserID: userID, DigestFrequency: goalnotification.FrequencyDaily},
	}
	store.queuedByGoal[realGoal] = []goalnotification.DigestItem{digestItem(realGoal, userID, "the only real item")}

	channel := &recordingChannel{kind: notifications.ChannelPush}
	job := NewGoalNotificationDigestJob(GoalNotificationDigestConfig{Enabled: true}, store, newTestDispatcher(channel), nil)
	job.Tick(context.Background())

	sent := channel.sentToUser(userID)
	if len(sent) != 1 {
		t.Fatalf("expected exactly 1 send, got %d", len(sent))
	}
	if _, cleared := store.clearedByGoal[emptyGoal]; cleared {
		t.Errorf("expected the empty goal's queue to never be touched, got clearedByGoal=%v", store.clearedByGoal)
	}
	if _, marked := store.sentAtByGoal[emptyGoal]; marked {
		t.Errorf("expected the empty goal to never be marked sent, got sentAtByGoal=%v", store.sentAtByGoal)
	}
}

// TestTick_NoDueGoalsSendsNothing confirms an empty ListDue result is a
// pure no-op.
func TestTick_NoDueGoalsSendsNothing(t *testing.T) {
	store := newFakeGoalDigestStore()
	channel := &recordingChannel{kind: notifications.ChannelPush}
	job := NewGoalNotificationDigestJob(GoalNotificationDigestConfig{Enabled: true}, store, newTestDispatcher(channel), nil)
	job.Tick(context.Background())

	if len(channel.sent) != 0 {
		t.Fatalf("expected no sends, got %d", len(channel.sent))
	}
}
