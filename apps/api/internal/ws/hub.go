package ws

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	maxEventHistory = 50 // Buffer last N events per channel
)

type Hub struct {
	clients    map[*Client]bool
	channels   map[string]map[*Client]bool
	broadcast  chan Event
	register   chan *Client
	unregister chan *Client
	history    map[string][]Event
	
	// Optional authenticator callback to validate tokens
	authenticator func(token string) (userID string, err error)
	logger        *slog.Logger
	mu            sync.RWMutex
}

func NewHub(logger *slog.Logger, authFunc func(string) (string, error)) *Hub {
	return &Hub{
		clients:       make(map[*Client]bool),
		channels:      make(map[string]map[*Client]bool),
		broadcast:     make(chan Event, 1000), // Buffer to avoid blocking producers
		register:      make(chan *Client),
		unregister:    make(chan *Client),
		history:       make(map[string][]Event),
		authenticator: authFunc,
		logger:        logger,
	}
}

func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			h.logger.Info("hub stopping")
			// Disconnect all clients gracefully
			h.mu.Lock()
			for client := range h.clients {
				close(client.send)
				client.conn.Close()
			}
			h.mu.Unlock()
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			h.removeClient(client)

		case event := <-h.broadcast:
			h.mu.Lock()
			// Update history
			history := h.history[event.Channel]
			history = append(history, event)
			if len(history) > maxEventHistory {
				history = history[len(history)-maxEventHistory:]
			}
			h.history[event.Channel] = history

			// Broadcast to subscribed clients
			if subbed, ok := h.channels[event.Channel]; ok {
				for client := range subbed {
					select {
					case client.send <- event:
					default:
						// If the client's send buffer is full, drop the event to apply backpressure
						select {
						case client.send <- Event{Channel: event.Channel, Type: EventEventsDropped}:
						default:
							// Client completely blocked, kick them
							h.removeClientLocked(client)
						}
					}
				}
			}
			h.mu.Unlock()
		}
	}
}

func (h *Hub) BroadcastEvent(evt Event) {
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}
	h.broadcast <- evt
}

func (h *Hub) subscribe(client *Client, channel string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.channels[channel]; !ok {
		h.channels[channel] = make(map[*Client]bool)
	}
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
}

func (h *Hub) unsubscribe(client *Client, channel string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if subs, ok := h.channels[channel]; ok {
		delete(subs, client)
		if len(subs) == 0 {
			delete(h.channels, channel)
		}
	}
}

func (h *Hub) removeClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.removeClientLocked(client)
}

func (h *Hub) removeClientLocked(client *Client) {
	if _, ok := h.clients[client]; ok {
		delete(h.clients, client)
		// Remove from all channels
		client.mu.Lock()
		for ch := range client.subs {
			if subs, ok := h.channels[ch]; ok {
				delete(subs, client)
				if len(subs) == 0 {
					delete(h.channels, ch)
				}
			}
		}
		client.mu.Unlock()
		close(client.send)
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allowing all origins for ease, should be constrained in production
	CheckOrigin: func(r *http.Request) bool { return true }, 
}

func (h *Hub) ServeWs(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	var userID string
	var err error
	
	if h.authenticator != nil {
		userID, err = h.authenticator(token)
		if err != nil {
			h.logger.Warn("websocket unauthorized", "error", err)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("websocket upgrade failed", "error", err)
		return
	}

	client := &Client{
		hub:    h,
		conn:   conn,
		send:   make(chan Event, 256),
		userID: userID,
		subs:   make(map[string]bool),
	}
	client.hub.register <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}
