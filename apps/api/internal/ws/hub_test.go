package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func newTestHub() *Hub {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	return NewHub(logger, func(token string) (string, error) {
		if token == "invalid" {
			return "", os.ErrPermission
		}
		return "user-123", nil
	}, []string{"http://localhost:3000"})
}

func TestHub_SubscriptionManagement(t *testing.T) {
	hub := newTestHub()

	client := &Client{
		hub:  hub,
		send: make(chan Event, 10),
		subs: make(map[string]bool),
	}

	hub.subscribe(client, "vault:1")
	hub.mu.RLock()
	if !hub.channels["vault:1"][client] {
		t.Errorf("Client not subscribed to vault:1")
	}
	hub.mu.RUnlock()

	hub.unsubscribe(client, "vault:1")
	hub.mu.RLock()
	if len(hub.channels["vault:1"]) != 0 {
		t.Errorf("Client not unsubscribed from vault:1")
	}
	hub.mu.RUnlock()
}

func TestHub_EventSerialization(t *testing.T) {
	evt := Event{
		Channel:   "setup:val",
		Type:      EventStatusChanged,
		Data:      map[string]interface{}{"status": "completed"},
		Timestamp: time.Now(),
	}

	bytes, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("Failed to serialize event: %v", err)
	}

	var parsed Event
	if err := json.Unmarshal(bytes, &parsed); err != nil {
		t.Fatalf("Failed to deserialize event: %v", err)
	}

	if parsed.Type != EventStatusChanged {
		t.Errorf("Expected EventStatusChanged, got %v", parsed.Type)
	}
}

func TestHub_Integration(t *testing.T) {
	hub := newTestHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	server := httptest.NewServer(http.HandlerFunc(hub.ServeWs))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=valid-token"
	dialer := websocket.Dialer{}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v, Response: %v", err, resp)
	}
	defer conn.Close()

	// 1. Send Subscribe Message
	subscribeMsg := ClientMessage{
		Action:   "subscribe",
		Channels: []string{"vault:123"},
	}
	if err := conn.WriteJSON(subscribeMsg); err != nil {
		t.Fatalf("Failed to write JSON: %v", err)
	}

	// Give the sub a moment to process
	time.Sleep(100 * time.Millisecond)

	// 2. Broadcast event
	testEvent := Event{
		Channel: "vault:123",
		Type:    EventBalanceUpdated,
		Data:    map[string]interface{}{"change": "50.00"},
	}
	hub.BroadcastEvent(testEvent)

	// 3. Verify receipt
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var received Event
	if err := conn.ReadJSON(&received); err != nil {
		t.Fatalf("Failed to read JSON: %v", err)
	}

	if received.Channel != "vault:123" {
		t.Errorf("Expected channel vault:123, got %s", received.Channel)
	}
	if received.Type != EventBalanceUpdated {
		t.Errorf("Expected event balance_updated, got %s", received.Type)
	}

	dataMap, ok := received.Data.(map[string]interface{})
	if !ok || dataMap["change"] != "50.00" {
		t.Errorf("Expected data change 50.00, got %v", received.Data)
	}
}

func TestHub_UnauthorizedReject(t *testing.T) {
	hub := newTestHub()
	server := httptest.NewServer(http.HandlerFunc(hub.ServeWs))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=invalid"
	dialer := websocket.Dialer{}
	_, resp, err := dialer.Dial(wsURL, nil)
	
	if err == nil {
		t.Fatalf("Expected connection failure on invalid token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized, got %d", resp.StatusCode)
	}
}

func TestHub_registerClient_success(t *testing.T) {
	hub := newTestHub()
	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)

	client := &Client{
		hub:  hub,
		send: make(chan Event, 10),
		subs: make(map[string]bool),
	}
	hub.register <- client

	// Give the run loop a moment to process the registration.
	time.Sleep(20 * time.Millisecond)

	hub.mu.RLock()
	_, registered := hub.clients[client]
	hub.mu.RUnlock()

	if !registered {
		t.Error("client not found in hub.clients after registration")
	}

	// Unregister before cancelling so the hub does not try conn.Close() on a nil conn.
	hub.unregister <- client
	time.Sleep(20 * time.Millisecond)
	cancel()
}

func TestHub_broadcastEvent_noClients_noError(t *testing.T) {
	hub := newTestHub()
	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)
	defer cancel()

	// Broadcasting with no subscribers must not panic or block.
	done := make(chan struct{})
	go func() {
		hub.BroadcastEvent(Event{Channel: "vault:999", Type: EventBalanceUpdated})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("BroadcastEvent blocked with no clients")
	}
}

func TestHub_broadcastEvent_disconnectedClient(t *testing.T) {
	hub := newTestHub()
	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)

	// Subscribe a real client via a test server so conn is not nil.
	server := httptest.NewServer(http.HandlerFunc(hub.ServeWs))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=valid"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		cancel()
		t.Fatalf("dial failed: %v", err)
	}
	// Subscribe to a channel then immediately close the TCP connection
	// to simulate a dropped client.
	if err := conn.WriteJSON(ClientMessage{Action: "subscribe", Channels: []string{"vault:dead"}}); err != nil {
		cancel()
		t.Fatalf("subscribe write failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	conn.Close()
	time.Sleep(50 * time.Millisecond)

	// Broadcasting to a channel whose only subscriber has disconnected must not panic.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("BroadcastEvent panicked: %v", r)
		}
	}()
	hub.BroadcastEvent(Event{Channel: "vault:dead", Type: EventBalanceUpdated})
	time.Sleep(50 * time.Millisecond)
	cancel()
}

func TestHub_gracefulShutdown_allClientsDisconnected(t *testing.T) {
	hub := newTestHub()
	ctx, cancel := context.WithCancel(context.Background())

	server := httptest.NewServer(http.HandlerFunc(hub.ServeWs))
	defer server.Close()

	hubDone := make(chan struct{})
	go func() {
		hub.Run(ctx)
		close(hubDone)
	}()

	dial := func() *websocket.Conn {
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=valid"
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("dial failed: %v", err)
		}
		return conn
	}
	c1 := dial()
	c2 := dial()

	time.Sleep(50 * time.Millisecond)

	hub.mu.RLock()
	before := len(hub.clients)
	hub.mu.RUnlock()
	if before < 2 {
		c1.Close()
		c2.Close()
		cancel()
		t.Fatalf("expected at least 2 connected clients, got %d", before)
	}

	// Cancel the context — hub Run() must exit promptly.
	cancel()
	select {
	case <-hubDone:
	case <-time.After(2 * time.Second):
		t.Error("hub.Run did not exit after context cancellation")
	}

	// Both client connections should now be closed by the hub.
	// The hub sends a CloseMessage via writePump; reading from the client must
	// return an error (close frame or EOF) within a short deadline.
	checkClosed := func(conn *websocket.Conn, name string) {
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, _, err := conn.ReadMessage()
		if err == nil {
			t.Errorf("expected %s read to fail after hub shutdown, got no error", name)
		}
		conn.Close()
	}
	checkClosed(c1, "c1")
	checkClosed(c2, "c2")
}
