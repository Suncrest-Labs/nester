package notifications

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestChannelsFor_MatchesIssueMatrix(t *testing.T) {
	cases := map[EventType][]ChannelKind{
		EventSettlementCompleted: {ChannelEmail, ChannelWebSocket, ChannelPush},
		EventSettlementFailed:    {ChannelEmail, ChannelWebSocket, ChannelPush},
		EventDepositConfirmed:    {ChannelEmail, ChannelWebSocket, ChannelPush},
		EventYieldMilestone:      {ChannelPush},
		EventVaultAPYDrop:        {ChannelEmail},
		EventVaultPaused:         {ChannelEmail, ChannelWebSocket},
		EventRebalanceExecuted:   {ChannelWebSocket},
		EventKYCApproved:               {ChannelEmail},
		EventKYCRejected:               {ChannelEmail},
		EventGoalMilestone:             {ChannelPush},
		EventScheduledDepositCompleted: {ChannelEmail, ChannelWebSocket, ChannelPush},
	}
	for evt, want := range cases {
		got := ChannelsFor(evt)
		if len(got) != len(want) {
			t.Errorf("%s: expected %d channels, got %d (%v)", evt, len(want), len(got), got)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s[%d]: want %s got %s", evt, i, want[i], got[i])
			}
		}
	}
}

func TestChannelsFor_ReturnsACopy(t *testing.T) {
	a := ChannelsFor(EventSettlementCompleted)
	a[0] = "mutated"
	b := ChannelsFor(EventSettlementCompleted)
	if b[0] == "mutated" {
		t.Errorf("ChannelsFor must defensively copy; in-place mutation leaked across calls")
	}
}

func TestDispatcher_SendDeliversToBothChannels(t *testing.T) {
	mail := &RecordingMailSender{}
	hub := &RecordingHub{}
	push := &RecordingPushSender{}
	tokens := NewMemoryDeviceTokens()
	uid := uuid.New()
	tokens.Set(uid, []DeviceToken{{Token: "ExponentPushToken[test]", Enabled: true}})
	d := New(
		[]Channel{
			NewEmailChannel(mail, StaticEmailLookup{Addr: "u@example.com"}),
			NewWebSocketChannel(hub),
			NewPushChannel(push, tokens),
		},
		NewMemoryPreferences(),
		nil,
	)

	if err := d.Send(context.Background(), uid, EventSettlementCompleted, "Done", "Settled $50", map[string]any{"amount": 50}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(mail.Calls) != 1 || mail.Calls[0].To != "u@example.com" || mail.Calls[0].Subject != "Done" {
		t.Errorf("email channel not delivered correctly: %+v", mail.Calls)
	}
	if len(hub.Calls) != 1 || hub.Calls[0].UserID != uid || hub.Calls[0].EventName != "notification" {
		t.Errorf("websocket channel not delivered correctly: %+v", hub.Calls)
	}
	if len(push.Calls) != 1 || push.Calls[0].Tokens[0] != "ExponentPushToken[test]" || push.Calls[0].Title != "Done" {
		t.Errorf("push channel not delivered correctly: %+v", push.Calls)
	}
}

func TestDispatcher_YieldMilestoneUsesPushOnly(t *testing.T) {
	mail := &RecordingMailSender{}
	hub := &RecordingHub{}
	push := &RecordingPushSender{}
	tokens := NewMemoryDeviceTokens()
	uid := uuid.New()
	tokens.Set(uid, []DeviceToken{{Token: "fcm-device-token", Enabled: true}})

	d := New(
		[]Channel{
			NewEmailChannel(mail, StaticEmailLookup{Addr: "u@example.com"}),
			NewWebSocketChannel(hub),
			NewPushChannel(push, tokens),
		},
		NewMemoryPreferences(),
		nil,
	)

	if err := d.Send(context.Background(), uid, EventYieldMilestone, "Yield milestone", "You earned 10 USDC", nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(mail.Calls) != 0 {
		t.Errorf("yield milestone must not email")
	}
	if len(hub.Calls) != 0 {
		t.Errorf("yield milestone must not websocket")
	}
	if len(push.Calls) != 1 {
		t.Errorf("yield milestone must push once, got %d", len(push.Calls))
	}
}

func TestDispatcher_RespectsPushOptOut(t *testing.T) {
	push := &RecordingPushSender{}
	tokens := NewMemoryDeviceTokens()
	prefs := NewMemoryPreferences()
	uid := uuid.New()
	tokens.Set(uid, []DeviceToken{{Token: "fcm-device-token", Enabled: true}})
	prefs.Set(uid, Preferences{Email: false, WebSocket: false, Push: false})

	d := New(
		[]Channel{NewPushChannel(push, tokens)},
		prefs,
		nil,
	)

	if err := d.Send(context.Background(), uid, EventDepositConfirmed, "Deposit confirmed", "100 USDC confirmed", nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(push.Calls) != 0 {
		t.Errorf("push opt-out not honoured: %+v", push.Calls)
	}
}

func TestDispatcher_RespectsEmailOptOut(t *testing.T) {
	mail := &RecordingMailSender{}
	hub := &RecordingHub{}
	prefs := NewMemoryPreferences()
	uid := uuid.New()
	prefs.Set(uid, Preferences{Email: false, WebSocket: true})

	d := New(
		[]Channel{
			NewEmailChannel(mail, StaticEmailLookup{Addr: "u@example.com"}),
			NewWebSocketChannel(hub),
		},
		prefs,
		nil,
	)

	if err := d.Send(context.Background(), uid, EventSettlementCompleted, "Done", "Settled $50", nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(mail.Calls) != 0 {
		t.Errorf("email opt-out not honoured: %+v", mail.Calls)
	}
	if len(hub.Calls) != 1 {
		t.Errorf("websocket should still receive: %+v", hub.Calls)
	}
}

func TestDispatcher_RebalanceExecuted_OnlyWebSocket(t *testing.T) {
	mail := &RecordingMailSender{}
	hub := &RecordingHub{}
	d := New(
		[]Channel{
			NewEmailChannel(mail, StaticEmailLookup{Addr: "u@example.com"}),
			NewWebSocketChannel(hub),
		},
		NewMemoryPreferences(),
		nil,
	)

	if err := d.Send(context.Background(), uuid.New(), EventRebalanceExecuted, "Rebalanced", "Moved to aave", nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(mail.Calls) != 0 {
		t.Errorf("rebalance executed must not email")
	}
	if len(hub.Calls) != 1 {
		t.Errorf("rebalance executed must websocket once, got %d", len(hub.Calls))
	}
}

func TestDispatcher_UnknownEventErrors(t *testing.T) {
	d := New(nil, NewMemoryPreferences(), nil)
	err := d.Send(context.Background(), uuid.New(), EventType("not_a_real_event"), "", "", nil)
	if err == nil {
		t.Fatalf("expected error for unknown event type")
	}
}

type errPersistence struct{}

func (errPersistence) Save(_ context.Context, _ Notification) error {
	return errors.New("disk full")
}

func TestDispatcher_SurfacesPersistenceFailure(t *testing.T) {
	d := New(
		[]Channel{NewWebSocketChannel(&RecordingHub{})},
		NewMemoryPreferences(),
		errPersistence{},
	)
	err := d.Send(context.Background(), uuid.New(), EventRebalanceExecuted, "x", "y", nil)
	if err == nil {
		t.Fatalf("expected persistence failure to surface")
	}
}

type errMailSender struct{}

func (errMailSender) Send(_ context.Context, _, _, _ string) error {
	return errors.New("smtp down")
}

func TestDispatcher_OneChannelFailureDoesNotBlockOthers(t *testing.T) {
	hub := &RecordingHub{}
	d := New(
		[]Channel{
			NewEmailChannel(errMailSender{}, StaticEmailLookup{Addr: "u@example.com"}),
			NewWebSocketChannel(hub),
		},
		NewMemoryPreferences(),
		nil,
	)

	err := d.Send(context.Background(), uuid.New(), EventSettlementCompleted, "Done", "Settled", nil)
	if err == nil {
		t.Errorf("expected joined error containing the email failure")
	}
	if len(hub.Calls) != 1 {
		t.Errorf("websocket must still have been delivered despite email failure; got %d", len(hub.Calls))
	}
}

func TestPreferences_AllowDefaultsToAllOn(t *testing.T) {
	p := DefaultPreferences()
	if !p.Allow(ChannelEmail) || !p.Allow(ChannelWebSocket) || !p.Allow(ChannelPush) {
		t.Errorf("default preferences must permit every channel")
	}
	if p.Allow(ChannelKind("imaginary")) {
		t.Errorf("unknown channel must not be permitted")
	}
}
