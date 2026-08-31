package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// The DApp keepalive is application-level because browser JavaScript cannot
// send WebSocket protocol ping frames. These tests pin the server half of
// that contract: {"action":"ping"} must come back as an EventPong inside the
// client's heartbeat timeout, otherwise every client tears down a perfectly
// healthy connection once per heartbeat interval.

func TestClient_AppLevelPingIsAnswered(t *testing.T) {
	hub := newTestHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	server := httptest.NewServer(http.HandlerFunc(hub.ServeWs))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=valid-token"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(ClientMessage{Action: ActionPing}); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var received Event
	if err := conn.ReadJSON(&received); err != nil {
		t.Fatalf("read pong: %v", err)
	}

	if received.Type != EventPong {
		t.Errorf("expected event %q, got %q", EventPong, received.Type)
	}
}

// A ping must not disturb the client's subscriptions — the heartbeat shares
// the message channel with domain traffic, so it has to be inert.
func TestClient_PingDoesNotAffectSubscriptions(t *testing.T) {
	hub := newTestHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	server := httptest.NewServer(http.HandlerFunc(hub.ServeWs))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=valid-token"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(ClientMessage{Action: ActionSubscribe, Channels: []string{"vault:9"}}); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	if err := conn.WriteJSON(ClientMessage{Action: ActionPing}); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var pong Event
	if err := conn.ReadJSON(&pong); err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if pong.Type != EventPong {
		t.Fatalf("expected pong first, got %q", pong.Type)
	}

	hub.BroadcastEvent(Event{
		Channel: "vault:9",
		Type:    EventBalanceUpdated,
		Data:    map[string]interface{}{"balance": "10.00"},
	})

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var received Event
	if err := conn.ReadJSON(&received); err != nil {
		t.Fatalf("read broadcast after ping: %v", err)
	}
	if received.Type != EventBalanceUpdated || received.Channel != "vault:9" {
		t.Errorf("subscription broken by ping: got %q on %q", received.Type, received.Channel)
	}
}

// An unknown action must be ignored rather than closing the connection: a
// newer client speaking a verb this server does not know yet should degrade,
// not disconnect.
func TestClient_UnknownActionIsIgnored(t *testing.T) {
	hub := newTestHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	server := httptest.NewServer(http.HandlerFunc(hub.ServeWs))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=valid-token"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(ClientMessage{Action: "not-a-real-action"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := conn.WriteJSON(ClientMessage{Action: ActionPing}); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var received Event
	if err := conn.ReadJSON(&received); err != nil {
		t.Fatalf("connection dropped after unknown action: %v", err)
	}
	if received.Type != EventPong {
		t.Errorf("expected pong, got %q", received.Type)
	}
}

// The read pump answering a ping runs concurrently with the hub closing that
// same client's send channel. Sending on a closed channel panics and takes the
// whole process down, so these pin the two teardown paths that close it.

func TestHub_PongAfterShutdownDoesNotPanic(t *testing.T) {
	hub := newTestHub()
	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)

	server := httptest.NewServer(http.HandlerFunc(hub.ServeWs))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=valid-token"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	time.Sleep(50 * time.Millisecond)

	client := hub.onlyClient(t)

	// Shut the hub down, which closes client.send.
	cancel()
	time.Sleep(100 * time.Millisecond)

	// A ping arriving now must be dropped, not delivered onto a closed channel.
	// This is the call the read pump makes.
	client.pong()

	if hub.sendToClient(client, Event{Type: EventPong}) {
		t.Error("sendToClient reported success after the hub shut the client down")
	}
}

// onlyClient returns the hub's single registered client, failing if there is
// not exactly one.
func (h *Hub) onlyClient(t *testing.T) *Client {
	t.Helper()
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.clients) != 1 {
		t.Fatalf("expected exactly 1 registered client, got %d", len(h.clients))
	}
	for c := range h.clients {
		return c
	}
	return nil
}

func TestHub_PongAfterUnregisterDoesNotPanic(t *testing.T) {
	hub := newTestHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	client := &Client{
		hub:  hub,
		send: make(chan Event, 10),
		subs: make(map[string]bool),
	}
	hub.register <- client
	time.Sleep(20 * time.Millisecond)

	hub.unregister <- client
	time.Sleep(20 * time.Millisecond)

	client.pong()

	if hub.sendToClient(client, Event{Type: EventPong}) {
		t.Error("sendToClient reported success after the client was unregistered")
	}
}

// Run with -race. Before the pong was routed through the hub this raced the
// unregister path and panicked on a closed channel.
func TestHub_ConcurrentPongAndUnregister(t *testing.T) {
	hub := newTestHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		client := &Client{
			hub:  hub,
			send: make(chan Event, 4),
			subs: make(map[string]bool),
		}
		hub.register <- client

		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				client.pong()
			}
		}()
		go func() {
			defer wg.Done()
			hub.unregister <- client
		}()
	}
	wg.Wait()

	// Let the hub drain the unregisters before the deferred cancel runs the
	// shutdown loop, so this test exercises the unregister path rather than
	// racing it against shutdown.
	deadline := time.Now().Add(2 * time.Second)
	for {
		hub.mu.RLock()
		remaining := len(hub.clients)
		hub.mu.RUnlock()
		if remaining == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d clients still registered after unregistering all", remaining)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A full buffer must be reported as a failed send, not silently swallowed:
// withholding the pong is what lets a stuck client's own heartbeat time out.
func TestHub_SendToClientReportsFullBuffer(t *testing.T) {
	hub := newTestHub()
	client := &Client{
		hub:  hub,
		send: make(chan Event, 1),
		subs: make(map[string]bool),
	}
	hub.mu.Lock()
	hub.clients[client] = true
	hub.mu.Unlock()

	if !hub.sendToClient(client, Event{Type: EventPong}) {
		t.Fatal("first send failed on an empty buffer")
	}
	if hub.sendToClient(client, Event{Type: EventPong}) {
		t.Error("second send reported success on a full buffer")
	}
}
