package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

func isOriginAllowed(r *http.Request, allowedOrigins []string) bool {
	if len(allowedOrigins) == 0 {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Same-origin requests carry no Origin header — allow them.
		return true
	}
	for _, allowed := range allowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}

const (
	maxEventHistory = 50 // Buffer last N events per channel

	// redisPubSubPrefix namespaces this hub's fan-out channels in Redis so
	// they can't collide with keys/channels used by unrelated features.
	redisPubSubPrefix = "ws:pubsub:"
	// presenceKeyPrefix namespaces per-user presence entries in Redis.
	presenceKeyPrefix = "ws:presence:"
	// presenceTTL bounds how long a presence entry survives without a
	// heartbeat refresh — a crashed instance's entries self-expire instead
	// of lingering forever (#828).
	presenceTTL = 90 * time.Second
	// presenceHeartbeatInterval is how often a connected client's presence
	// entry is refreshed. Comfortably inside presenceTTL so a brief Redis
	// hiccup doesn't cause a spurious expiry.
	presenceHeartbeatInterval = 30 * time.Second
	// redisOpTimeout bounds every individual Redis call the hub makes so a
	// degraded Redis never adds unbounded latency to a subscribe/unsubscribe
	// or broadcast — the hub keeps working locally either way.
	redisOpTimeout = 200 * time.Millisecond

	// DefaultMaxConnectionsPerIP bounds how many simultaneous WebSocket
	// connections one client IP may hold, so a single actor can't exhaust
	// connection slots (#828). 0 (the zero value) means "no limit" — see
	// NewHub's maxConnsPerIP parameter. Exported so callers constructing the
	// production Hub (cmd/api/main.go) have a sensible default to pass
	// without hardcoding a magic number at the call site.
	DefaultMaxConnectionsPerIP = 0
)

// wireEvent is what actually crosses Redis pub/sub: the Event plus which
// instance produced it, so that instance can ignore its own echo (it already
// delivered the event to its local clients synchronously in BroadcastEvent)
// rather than double-delivering.
type wireEvent struct {
	Event          Event  `json:"event"`
	OriginInstance string `json:"origin_instance"`
}

// HubStats is a point-in-time snapshot of hub activity for a metrics endpoint
// to scrape (#828's "connected-client count, delivery latency and
// dropped-client metrics").
type HubStats struct {
	ConnectedClients int64
	DroppedSlow      int64 // clients disconnected for a full send buffer
	RejectedPerIP    int64 // connections rejected by the per-IP limit
	RedisErrors      int64
}

type Hub struct {
	clients    map[*Client]bool
	channels   map[string]map[*Client]bool
	broadcast  chan Event
	register   chan *Client
	unregister chan *Client
	history    map[string][]Event

	// Optional authenticator callback to validate tokens. sessionID may be
	// empty (e.g. service-to-service auth carries no session).
	authenticator  func(token string) (userID, sessionID string, err error)
	allowedOrigins []string
	logger         *slog.Logger
	upgrader       websocket.Upgrader
	mu             sync.RWMutex

	// Redis pub/sub fan-out (#828). redis is nil in single-instance
	// deployments (no REDIS_ADDR) — every method below checks for that and
	// degrades to purely in-process behavior, identical to today.
	redis         *redis.Client
	instanceID    string
	redisPubSub   *redis.PubSub
	redisTopics   map[string]int // eventChannel -> local subscriber count, so we know when to (un)subscribe in Redis
	maxConnsPerIP int
	connsByIP     map[string]int

	connectedClients atomic.Int64
	droppedSlow      atomic.Int64
	rejectedPerIP    atomic.Int64
	redisErrors      atomic.Int64
}

// NewHub constructs a Hub. rc may be nil (no Redis configured — the
// single-instance behavior from before #828 is unchanged). maxConnsPerIP
// bounds simultaneous connections from one client IP; 0 means unlimited.
func NewHub(logger *slog.Logger, authFunc func(string) (userID, sessionID string, err error), allowedOrigins []string, rc *redis.Client, maxConnsPerIP int) *Hub {
	h := &Hub{
		clients:        make(map[*Client]bool),
		channels:       make(map[string]map[*Client]bool),
		broadcast:      make(chan Event, 1000), // Buffer to avoid blocking producers
		register:       make(chan *Client),
		unregister:     make(chan *Client),
		history:        make(map[string][]Event),
		authenticator:  authFunc,
		allowedOrigins: allowedOrigins,
		logger:         logger,
		redis:          rc,
		instanceID:     uuid.New().String(),
		redisTopics:    make(map[string]int),
		maxConnsPerIP:  maxConnsPerIP,
		connsByIP:      make(map[string]int),
	}
	h.upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return isOriginAllowed(r, h.allowedOrigins) },
	}
	if rc != nil {
		// An empty-channel PubSub; individual topics are added/removed via
		// Subscribe/Unsubscribe as local subscriber counts transition
		// to/from zero (subscribeRedisTopic / unsubscribeRedisTopic below).
		h.redisPubSub = rc.Subscribe(context.Background())
	}
	return h
}

// Stats returns a snapshot of cumulative hub activity counters.
func (h *Hub) Stats() HubStats {
	return HubStats{
		ConnectedClients: h.connectedClients.Load(),
		DroppedSlow:      h.droppedSlow.Load(),
		RejectedPerIP:    h.rejectedPerIP.Load(),
		RedisErrors:      h.redisErrors.Load(),
	}
}

func (h *Hub) Run(ctx context.Context) {
	if h.redisPubSub != nil {
		go h.consumeRedis(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("hub stopping")
			// Disconnect all clients gracefully, and release Redis-side
			// state (subscriptions + presence) so other instances/clients
			// aren't left waiting on a dead instance (#828's graceful
			// shutdown requirement, coordinated with issue #786).
			h.mu.Lock()
			for client := range h.clients {
				close(client.send)
				client.conn.Close()
				h.clearPresence(client.userID)
			}
			h.mu.Unlock()
			if h.redisPubSub != nil {
				_ = h.redisPubSub.Close()
			}
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			h.connectedClients.Add(1)
			h.markPresent(client.userID)

		case client := <-h.unregister:
			h.removeClient(client)

		case event := <-h.broadcast:
			h.deliverLocally(event)
		}
	}
}

// deliverLocally fans an event out to this instance's own subscribed
// clients and records it in per-channel history. Shared by both
// locally-produced events (via BroadcastEvent) and events received from
// other instances over Redis (via consumeRedis) — one delivery code path.
func (h *Hub) deliverLocally(event Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	history := h.history[event.Channel]
	history = append(history, event)
	if len(history) > maxEventHistory {
		history = history[len(history)-maxEventHistory:]
	}
	h.history[event.Channel] = history

	if subbed, ok := h.channels[event.Channel]; ok {
		for client := range subbed {
			select {
			case client.send <- event:
			default:
				// If the client's send buffer is full, apply backpressure by
				// dropping the event and notifying the client.
				select {
				case client.send <- Event{Channel: event.Channel, Type: EventEventsDropped}:
				default:
					// Client completely blocked, kick them.
					h.droppedSlow.Add(1)
					h.removeClientLocked(client)
				}
			}
		}
	}
}

// BroadcastEvent delivers evt to this instance's own subscribed clients
// immediately, and (when Redis is configured) publishes it so every other
// instance's subscribed clients receive it too (#828).
func (h *Hub) BroadcastEvent(evt Event) {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}
	h.broadcast <- evt
	h.publishToRedis(evt)
}

func (h *Hub) publishToRedis(evt Event) {
	if h.redis == nil {
		return
	}
	payload, err := json.Marshal(wireEvent{Event: evt, OriginInstance: h.instanceID})
	if err != nil {
		h.logger.Warn("websocket: failed to marshal event for redis publish", "error", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	if err := h.redis.Publish(ctx, redisPubSubPrefix+evt.Channel, payload).Err(); err != nil {
		h.redisErrors.Add(1)
		h.logger.Warn("websocket: redis publish failed; other instances will not see this event",
			"channel", evt.Channel, "error", err)
	}
}

// consumeRedis reads events published by any instance (including this one)
// and re-broadcasts locally, skipping this instance's own events since
// BroadcastEvent already delivered those synchronously above.
func (h *Hub) consumeRedis(ctx context.Context) {
	ch := h.redisPubSub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var we wireEvent
			if err := json.Unmarshal([]byte(msg.Payload), &we); err != nil {
				h.logger.Warn("websocket: failed to unmarshal redis event", "error", err)
				continue
			}
			if we.OriginInstance == h.instanceID {
				continue // already delivered locally by BroadcastEvent
			}
			h.deliverLocally(we.Event)
		}
	}
}

// PushToUser satisfies notifications.WebSocketHub. It broadcasts a typed event
// to the user-scoped channel "notifications/{userID}" so any client subscribed
// to that channel receives the payload in real time, on any instance.
func (h *Hub) PushToUser(_ context.Context, userID uuid.UUID, eventName string, payload any) error {
	h.BroadcastEvent(Event{
		Channel:   fmt.Sprintf("notifications/%s", userID),
		Type:      EventType(eventName),
		Data:      payload,
		Timestamp: time.Now(),
	})
	return nil
}

func (h *Hub) subscribe(client *Client, channel string) {
	h.mu.Lock()

	if _, ok := h.channels[channel]; !ok {
		h.channels[channel] = make(map[*Client]bool)
	}
	isFirstLocalSubscriber := len(h.channels[channel]) == 0
	h.channels[channel][client] = true

	// Send history to client upon subscription
	if hist, ok := h.history[channel]; ok {
		for _, evt := range hist {
			select {
			case client.send <- evt:
			default:
			}
		}
	}
	h.mu.Unlock()

	if isFirstLocalSubscriber {
		h.subscribeRedisTopic(channel)
	}
}

func (h *Hub) unsubscribe(client *Client, channel string) {
	h.mu.Lock()
	becameEmpty := false
	if subs, ok := h.channels[channel]; ok {
		delete(subs, client)
		if len(subs) == 0 {
			delete(h.channels, channel)
			becameEmpty = true
		}
	}
	h.mu.Unlock()

	if becameEmpty {
		h.unsubscribeRedisTopic(channel)
	}
}

// subscribeRedisTopic and unsubscribeRedisTopic re-evaluate this instance's
// Redis subscriptions as clients (un)subscribe, so an instance is only woken
// for topics its own connected clients actually care about (#828). Reference
// counted by redisTopics since multiple local clients can subscribe to the
// same channel independently.
func (h *Hub) subscribeRedisTopic(channel string) {
	if h.redis == nil {
		return
	}
	h.mu.Lock()
	h.redisTopics[channel]++
	first := h.redisTopics[channel] == 1
	h.mu.Unlock()
	if !first {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	if err := h.redisPubSub.Subscribe(ctx, redisPubSubPrefix+channel); err != nil {
		h.redisErrors.Add(1)
		h.logger.Warn("websocket: redis subscribe failed", "channel", channel, "error", err)
	}
}

func (h *Hub) unsubscribeRedisTopic(channel string) {
	if h.redis == nil {
		return
	}
	h.mu.Lock()
	h.redisTopics[channel]--
	last := h.redisTopics[channel] <= 0
	if last {
		delete(h.redisTopics, channel)
	}
	h.mu.Unlock()
	if !last {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	if err := h.redisPubSub.Unsubscribe(ctx, redisPubSubPrefix+channel); err != nil {
		h.redisErrors.Add(1)
		h.logger.Warn("websocket: redis unsubscribe failed", "channel", channel, "error", err)
	}
}

// markPresent records that userID is connected on this instance, with a
// heartbeat-refreshed TTL so a crashed instance's entries self-expire
// (#828) rather than falsely reporting a user connected forever.
func (h *Hub) markPresent(userID string) {
	if h.redis == nil || userID == "" {
		return
	}
	h.refreshPresence(userID)
	go h.heartbeatPresence(userID)
}

func (h *Hub) refreshPresence(userID string) {
	if h.redis == nil || userID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	if err := h.redis.Set(ctx, presenceKeyPrefix+userID, h.instanceID, presenceTTL).Err(); err != nil {
		h.redisErrors.Add(1)
	}
}

// heartbeatPresence refreshes userID's presence TTL periodically until the
// user is no longer connected on this instance (checked via isUserConnected
// rather than a dedicated stop channel, keeping this self-contained per
// call — it naturally stops within one heartbeat interval of the last
// connection for userID closing).
func (h *Hub) heartbeatPresence(userID string) {
	ticker := time.NewTicker(presenceHeartbeatInterval)
	defer ticker.Stop()
	for range ticker.C {
		if !h.isUserConnected(userID) {
			return
		}
		h.refreshPresence(userID)
	}
}

func (h *Hub) isUserConnected(userID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		if client.userID == userID {
			return true
		}
	}
	return false
}

// clearPresence releases userID's presence entry immediately. Called on
// disconnect and on graceful shutdown; the TTL is also a self-healing
// backstop if this is never reached (a crash).
func (h *Hub) clearPresence(userID string) {
	if h.redis == nil || userID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), redisOpTimeout)
	defer cancel()
	if err := h.redis.Del(ctx, presenceKeyPrefix+userID).Err(); err != nil {
		h.redisErrors.Add(1)
	}
}

// IsUserPresent reports whether userID has a live connection on any
// instance (per Redis presence tracking). Always false when Redis isn't
// configured — presence is a distributed-deployment feature.
func (h *Hub) IsUserPresent(ctx context.Context, userID string) bool {
	if h.redis == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, redisOpTimeout)
	defer cancel()
	exists, err := h.redis.Exists(ctx, presenceKeyPrefix+userID).Result()
	if err != nil {
		h.redisErrors.Add(1)
		return false
	}
	return exists > 0
}

func (h *Hub) removeClient(client *Client) {
	h.mu.Lock()
	h.removeClientLocked(client)
	h.mu.Unlock()
}

// removeClientLocked must be called with h.mu held.
func (h *Hub) removeClientLocked(client *Client) {
	if _, ok := h.clients[client]; ok {
		delete(h.clients, client)
		h.connectedClients.Add(-1)
		// Remove from all channels using h.channels (already under h.mu),
		// avoiding acquiring client.mu which would invert the lock order.
		var emptiedChannels []string
		for ch, subs := range h.channels {
			if _, in := subs[client]; in {
				delete(subs, client)
				if len(subs) == 0 {
					delete(h.channels, ch)
					emptiedChannels = append(emptiedChannels, ch)
				}
			}
		}
		close(client.send)
		h.decrementIPLocked(client.remoteIP)

		// Redis (un)subscription bookkeeping and presence release must not
		// run while holding h.mu (they make network calls) — dispatch after
		// unlocking. removeClientLocked itself stays synchronous/locked;
		// callers (removeClient, Run's unregister case) unlock right after
		// returning, so do the network work there instead. To keep this
		// simple and avoid every caller repeating that dance, do it via a
		// goroutine here: the topics/user are already fully decided above.
		userID := client.userID
		go func() {
			for _, ch := range emptiedChannels {
				h.unsubscribeRedisTopic(ch)
			}
			if !h.isUserConnected(userID) {
				h.clearPresence(userID)
			}
		}()
	}
}

// CloseConnectionsForSession forcibly disconnects every live WebSocket
// client authenticated under sessionID. Used when a single session is
// revoked (logout, reuse/device-mismatch detected) so an already-open
// connection doesn't outlive the session.
func (h *Hub) CloseConnectionsForSession(sessionID string) {
	if sessionID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for client := range h.clients {
		if client.sessionID == sessionID {
			client.conn.Close()
			h.removeClientLocked(client)
		}
	}
}

// CloseConnectionsForUser forcibly disconnects every live WebSocket client
// belonging to userID. Used for "sign out everywhere".
func (h *Hub) CloseConnectionsForUser(userID string) {
	if userID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for client := range h.clients {
		if client.userID == userID {
			client.conn.Close()
			h.removeClientLocked(client)
		}
	}
}

func (h *Hub) incrementIPLocked(ip string) (allowed bool) {
	if h.maxConnsPerIP <= 0 || ip == "" {
		return true
	}
	if h.connsByIP[ip] >= h.maxConnsPerIP {
		return false
	}
	h.connsByIP[ip]++
	return true
}

func (h *Hub) decrementIPLocked(ip string) {
	if ip == "" {
		return
	}
	if h.connsByIP[ip] <= 1 {
		delete(h.connsByIP, ip)
		return
	}
	h.connsByIP[ip]--
}

// clientIP extracts the caller's IP for per-IP connection limiting,
// preferring a proxy-supplied X-Forwarded-For (first hop) and falling back
// to the raw remote address.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if idx := strings.IndexByte(fwd, ','); idx != -1 {
			return strings.TrimSpace(fwd[:idx])
		}
		return strings.TrimSpace(fwd)
	}
	host := r.RemoteAddr
	if idx := strings.LastIndexByte(host, ':'); idx != -1 {
		return host[:idx]
	}
	return host
}

func (h *Hub) ServeWs(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	var userID, sessionID string
	var err error

	if h.authenticator != nil {
		userID, sessionID, err = h.authenticator(token)
		if err != nil {
			h.logger.Warn("websocket unauthorized", "error", err)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	ip := clientIP(r)
	h.mu.Lock()
	allowed := h.incrementIPLocked(ip)
	h.mu.Unlock()
	if !allowed {
		h.rejectedPerIP.Add(1)
		h.logger.Warn("websocket connection rejected: per-IP limit exceeded", "ip", ip)
		http.Error(w, "Too Many Connections", http.StatusTooManyRequests)
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.mu.Lock()
		h.decrementIPLocked(ip)
		h.mu.Unlock()
		h.logger.Error("websocket upgrade failed", "error", err)
		return
	}

	client := &Client{
		hub:       h,
		conn:      conn,
		send:      make(chan Event, 256),
		userID:    userID,
		sessionID: sessionID,
		remoteIP:  ip,
		subs:      make(map[string]bool),
	}
	client.hub.register <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}
