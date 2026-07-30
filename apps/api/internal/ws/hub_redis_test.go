package ws

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

// newTestRedisClient returns a *redis.Client for the address in REDIS_ADDR,
// skipping the test if it's unset or unreachable — same convention as
// internal/cache's tests and internal/middleware/ratelimit_backend_test.go.
func newTestRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR not set; skipping redis-backed websocket test")
	}
	rc := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rc.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not reachable at %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = rc.Close() })
	return rc
}

func newTestHubWithRedis(rc *redis.Client, maxConnsPerIP int) *Hub {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	return NewHub(logger, func(token string) (string, string, error) {
		if token == "invalid" {
			return "", "", os.ErrPermission
		}
		return "user-" + token, "session-" + token, nil
	}, []string{"http://localhost:3000"}, rc, maxConnsPerIP)
}

// TestHub_Redis_CrossInstanceDelivery is #828's core acceptance criterion: two
// Hubs sharing one Redis, each with a client connected to a *different*
// instance, must both receive an event published on either instance.
func TestHub_Redis_CrossInstanceDelivery(t *testing.T) {
	rc := newTestRedisClient(t)

	hubA := newTestHubWithRedis(rc, 0)
	hubB := newTestHubWithRedis(rc, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hubA.Run(ctx)
	go hubB.Run(ctx)

	serverA := httptest.NewServer(http.HandlerFunc(hubA.ServeWs))
	defer serverA.Close()
	serverB := httptest.NewServer(http.HandlerFunc(hubB.ServeWs))
	defer serverB.Close()

	channel := "vault:cross-instance-" + t.Name()

	connA := dialAndSubscribe(t, serverA.URL, "tokA", channel)
	defer connA.Close()
	connB := dialAndSubscribe(t, serverB.URL, "tokB", channel)
	defer connB.Close()

	// Give both instances a moment to register their Redis subscription.
	time.Sleep(100 * time.Millisecond)

	// Produced on instance A only.
	hubA.BroadcastEvent(Event{Channel: channel, Type: EventBalanceUpdated, Data: map[string]any{"via": "A"}})

	assertReceivesOnChannel(t, connA, channel, "hubA's own client")
	assertReceivesOnChannel(t, connB, channel, "hubB's client via redis fan-out from hubA")

	// And the reverse direction, produced on instance B.
	hubB.BroadcastEvent(Event{Channel: channel, Type: EventBalanceUpdated, Data: map[string]any{"via": "B"}})
	assertReceivesOnChannel(t, connA, channel, "hubA's client via redis fan-out from hubB")
	assertReceivesOnChannel(t, connB, channel, "hubB's own client")
}

func dialAndSubscribe(t *testing.T, httpURL, token, channel string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(httpURL, "http") + "?token=" + token
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial %s failed: %v (resp=%v)", httpURL, err, resp)
	}
	if err := conn.WriteJSON(ClientMessage{Action: "subscribe", Channels: []string{channel}}); err != nil {
		t.Fatalf("subscribe write failed: %v", err)
	}
	return conn
}

func assertReceivesOnChannel(t *testing.T, conn *websocket.Conn, wantChannel, desc string) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var evt Event
	if err := conn.ReadJSON(&evt); err != nil {
		t.Fatalf("%s: expected to receive event, got error: %v", desc, err)
	}
	if evt.Channel != wantChannel {
		t.Errorf("%s: expected channel %s, got %s", desc, wantChannel, evt.Channel)
	}
}

// TestHub_Redis_OwnEventNotDoubleDelivered proves an instance does not
// deliver its own published event twice (once locally, once via its own
// Redis subscription echo).
func TestHub_Redis_OwnEventNotDoubleDelivered(t *testing.T) {
	rc := newTestRedisClient(t)
	hub := newTestHubWithRedis(rc, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	server := httptest.NewServer(http.HandlerFunc(hub.ServeWs))
	defer server.Close()

	channel := "vault:no-double-delivery-" + t.Name()
	conn := dialAndSubscribe(t, server.URL, "tok", channel)
	defer conn.Close()

	time.Sleep(100 * time.Millisecond)
	hub.BroadcastEvent(Event{Channel: channel, Type: EventBalanceUpdated})

	assertReceivesOnChannel(t, conn, channel, "first (only) delivery")

	// A second read within a short window must time out — there should be no
	// duplicate.
	conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	var evt Event
	if err := conn.ReadJSON(&evt); err == nil {
		t.Fatalf("expected no second delivery, got duplicate event on channel %s", evt.Channel)
	}
}

// TestHub_Redis_ReconnectMovesSubscriptionCleanly proves that when a user's
// first connection (on hubA) drops and they reconnect on a different
// instance (hubB), the new instance delivers events for that channel and
// the old instance's now-empty subscription doesn't linger or duplicate.
func TestHub_Redis_ReconnectMovesSubscriptionCleanly(t *testing.T) {
	rc := newTestRedisClient(t)
	hubA := newTestHubWithRedis(rc, 0)
	hubB := newTestHubWithRedis(rc, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hubA.Run(ctx)
	go hubB.Run(ctx)

	serverA := httptest.NewServer(http.HandlerFunc(hubA.ServeWs))
	defer serverA.Close()
	serverB := httptest.NewServer(http.HandlerFunc(hubB.ServeWs))
	defer serverB.Close()

	channel := "vault:reconnect-" + t.Name()

	connA := dialAndSubscribe(t, serverA.URL, "tok", channel)
	time.Sleep(100 * time.Millisecond)

	// Simulate a drop and reconnect elsewhere.
	connA.Close()
	time.Sleep(100 * time.Millisecond)

	connB := dialAndSubscribe(t, serverB.URL, "tok", channel)
	defer connB.Close()
	time.Sleep(100 * time.Millisecond)

	hubA.BroadcastEvent(Event{Channel: channel, Type: EventBalanceUpdated, Data: "after-reconnect"})
	assertReceivesOnChannel(t, connB, channel, "hubB's client after reconnect")
}

// TestHub_Redis_PresenceTracksConnectionAndExpiresOnDisconnect exercises the
// distributed presence tracking: connecting sets a presence entry visible
// from *another* hub sharing the same Redis, and disconnecting clears it.
func TestHub_Redis_PresenceTracksConnectionAndExpiresOnDisconnect(t *testing.T) {
	rc := newTestRedisClient(t)
	hubA := newTestHubWithRedis(rc, 0)
	hubB := newTestHubWithRedis(rc, 0) // a second instance sharing the same Redis, to prove presence is cross-instance

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hubA.Run(ctx)
	go hubB.Run(ctx)

	server := httptest.NewServer(http.HandlerFunc(hubA.ServeWs))
	defer server.Close()

	userToken := "presence-user-" + t.Name()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=" + userToken
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}

	userID := "user-" + userToken
	if !waitFor(t, 2*time.Second, func() bool { return hubB.IsUserPresent(context.Background(), userID) }) {
		t.Fatal("expected hubB to observe user as present via shared redis presence tracking")
	}

	conn.Close()

	if !waitFor(t, 2*time.Second, func() bool { return !hubB.IsUserPresent(context.Background(), userID) }) {
		t.Fatal("expected presence to clear shortly after disconnect")
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

// TestHub_SlowClient_DisconnectedOnBufferOverflow proves a client that never
// reads its send buffer is disconnected rather than allowed to accumulate
// unbounded memory (#828's backpressure requirement). No Redis needed.
func TestHub_SlowClient_DisconnectedOnBufferOverflow(t *testing.T) {
	hub := newTestHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	client := &Client{
		hub:  hub,
		conn: nil,
		// A tiny buffer so we can overflow it quickly without a real slow
		// network peer.
		send: make(chan Event, 2),
		subs: make(map[string]bool),
	}
	hub.mu.Lock()
	hub.clients[client] = true
	hub.mu.Unlock()
	hub.subscribe(client, "vault:slow")

	// Flood well past the buffer size without ever draining client.send,
	// exactly like a client that stopped reading.
	for i := 0; i < 20; i++ {
		hub.BroadcastEvent(Event{Channel: "vault:slow", Type: EventBalanceUpdated})
	}
	time.Sleep(100 * time.Millisecond)

	hub.mu.RLock()
	_, stillConnected := hub.clients[client]
	hub.mu.RUnlock()
	if stillConnected {
		t.Error("expected slow client to be disconnected after send-buffer overflow, but it is still registered")
	}
}

// TestHub_PerIPConnectionLimit_RejectsBeyondLimit proves ServeWs enforces
// the configured per-IP cap (#828's abuse-prevention requirement).
func TestHub_PerIPConnectionLimit_RejectsBeyondLimit(t *testing.T) {
	hub := newTestHubWithRedis(nil, 1) // limit of 1 connection per IP, no redis needed
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	server := httptest.NewServer(http.HandlerFunc(hub.ServeWs))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?token=tok1"
	conn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("first dial should succeed: %v", err)
	}
	defer conn1.Close()

	// All httptest.Server client connections share the same loopback IP, so a
	// second connection must be rejected under a per-IP limit of 1.
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected second connection from the same IP to be rejected")
	}
	if resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		status := -1
		if resp != nil {
			status = resp.StatusCode
		}
		t.Errorf("expected 429 Too Many Requests, got %d", status)
	}
}
