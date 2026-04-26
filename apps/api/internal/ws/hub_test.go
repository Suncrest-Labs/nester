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
	})
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
