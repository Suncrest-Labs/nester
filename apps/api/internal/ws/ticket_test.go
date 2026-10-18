package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
)

func TestTicketStore_SingleUseAndExpiry(t *testing.T) {
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	store := NewTicketStore(nil, 30*time.Second)
	store.now = func() time.Time { return now }

	ticket, expiresAt, err := store.Issue(context.Background(), "user-1", "sess-1")
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	if expiresAt.Sub(now) != 30*time.Second {
		t.Fatalf("expected 30s TTL, got %v", expiresAt.Sub(now))
	}

	// First consume succeeds
	uid, sid, err := store.Consume(context.Background(), ticket)
	if err != nil {
		t.Fatalf("first Consume failed: %v", err)
	}
	if uid != "user-1" || sid != "sess-1" {
		t.Fatalf("unexpected user/session: %q / %q", uid, sid)
	}

	// Second consume of the same single-use ticket must be rejected
	if _, _, err := store.Consume(context.Background(), ticket); err == nil {
		t.Fatal("expected second Consume of single-use ticket to fail, got nil")
	}

	// Issue a second ticket and advance clock past TTL
	expiredTicket, _, err := store.Issue(context.Background(), "user-2", "sess-2")
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	now = now.Add(31 * time.Second)
	if _, _, err := store.Consume(context.Background(), expiredTicket); err == nil {
		t.Fatal("expected expired ticket to be rejected, got nil")
	}
}

func TestHub_ServeWs_TicketSingleUseAndExpiredRejected(t *testing.T) {
	hub := newTestHub()
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	store := NewTicketStore(nil, 30*time.Second)
	store.now = func() time.Time { return now }
	hub.SetTicketStore(store)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/ws/ticket", func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(auth.NewContext(r.Context(), auth.User{
			ID:        "user-ticket-1",
			SessionID: "sess-ticket-1",
		}))
		hub.HandleIssueTicket(w, r)
	})
	mux.HandleFunc("GET /ws", hub.ServeWs)

	server := httptest.NewServer(mux)
	defer server.Close()

	// 1. Issue ticket via POST /api/v1/ws/ticket
	resp, err := http.Post(server.URL+"/api/v1/ws/ticket", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/v1/ws/ticket failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from ticket endpoint, got %d", resp.StatusCode)
	}
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Ticket    string `json:"ticket"`
			ExpiresIn int    `json:"expires_in"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode ticket response: %v", err)
	}
	if !body.Success || body.Data.Ticket == "" {
		t.Fatalf("expected non-empty ticket, got %+v", body)
	}
	if body.Data.ExpiresIn <= 0 || body.Data.ExpiresIn > 30 {
		t.Fatalf("expected ticket expires_in <= 30s, got %d", body.Data.ExpiresIn)
	}

	wsBase := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	// 2. First connection with the ticket succeeds
	conn, wsResp, err := websocket.DefaultDialer.Dial(wsBase+"?ticket="+body.Data.Ticket, nil)
	if err != nil {
		t.Fatalf("expected valid ticket connection to succeed: %v (resp=%v)", err, wsResp)
	}
	_ = conn.Close()

	// 3. Replaying the used ticket is rejected with 401
	_, replayResp, err := websocket.DefaultDialer.Dial(wsBase+"?ticket="+body.Data.Ticket, nil)
	if err == nil {
		t.Fatal("expected replayed ticket to be rejected")
	}
	if replayResp == nil || replayResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on replayed ticket, got %v", replayResp)
	}

	// 4. Expired ticket is rejected with 401
	expiredTicket, _, err := store.Issue(ctx, "user-exp", "sess-exp")
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	now = now.Add(31 * time.Second)
	_, expResp, err := websocket.DefaultDialer.Dial(wsBase+"?ticket="+expiredTicket, nil)
	if err == nil {
		t.Fatal("expected expired ticket to be rejected")
	}
	if expResp == nil || expResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on expired ticket, got %v", expResp)
	}
}

func TestHub_ServeWs_RejectsBearerJWTInQueryStringAndAcceptsSubprotocol(t *testing.T) {
	hub := newTestHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	server := httptest.NewServer(http.HandlerFunc(hub.ServeWs))
	defer server.Close()

	wsBase := "ws" + strings.TrimPrefix(server.URL, "http")
	fakeJWT := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1c2VyLTEyMyJ9.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"

	// Long-lived JWT in ?token= query string must be rejected with 401
	_, jwtResp, err := websocket.DefaultDialer.Dial(wsBase+"?token="+fakeJWT, nil)
	if err == nil {
		t.Fatal("expected bearer JWT in query string to be rejected")
	}
	if jwtResp == nil || jwtResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 when JWT passed in query string, got %v", jwtResp)
	}

	// Sec-WebSocket-Protocol authentication without any query parameter succeeds
	dialer := websocket.Dialer{
		Subprotocols: []string{"nester.v1", "nester.auth." + fakeJWT},
	}
	conn, subResp, err := dialer.Dial(wsBase, nil)
	if err != nil {
		t.Fatalf("expected Sec-WebSocket-Protocol auth to succeed: %v (resp=%v)", err, subResp)
	}
	defer conn.Close()
	if conn.Subprotocol() != "nester.v1" {
		t.Fatalf("expected negotiated subprotocol nester.v1, got %q", conn.Subprotocol())
	}
}
