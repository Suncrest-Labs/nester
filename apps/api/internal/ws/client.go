package ws

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 90 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = 30 * time.Second

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

type Client struct {
	hub       *Hub
	conn      *websocket.Conn
	send      chan Event
	userID    string
	sessionID string
	remoteIP  string

	mu   sync.Mutex
	subs map[string]bool
}

// readPump pumps messages from the websocket connection to the hub.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.hub.logger.Error("websocket unexpected close", "error", err)
			}
			break
		}

		var msg ClientMessage
		if err := json.Unmarshal(message, &msg); err == nil {
			switch msg.Action {
			case ActionSubscribe:
				c.mu.Lock()
				for _, ch := range msg.Channels {
					if !c.maySubscribe(ch) {
						c.hub.logger.Warn("websocket subscription rejected",
							"channel", ch, "user_id", c.userID)
						continue
					}
					c.subs[ch] = true
					c.hub.subscribe(c, ch)
				}
				c.mu.Unlock()
			case ActionUnsubscribe:
				c.mu.Lock()
				for _, ch := range msg.Channels {
					delete(c.subs, ch)
					c.hub.unsubscribe(c, ch)
				}
				c.mu.Unlock()
			case ActionPing:
				c.pong()
			}
		}
	}
}

// pong answers an application-level ping (see EventPong).
//
// The send goes through the hub rather than straight to c.send: the hub owns
// that channel's lifetime and closes it during unregister and shutdown, and
// this is the read pump, which runs concurrently with both. See
// Hub.sendToClient.
func (c *Client) pong() {
	c.hub.sendToClient(c, Event{Type: EventPong, Timestamp: time.Now()})
}

// writePump pumps messages from the hub to the websocket connection.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case event, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			c.conn.WriteJSON(event)

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// userScopedChannelPrefixes are the channel namespaces whose second segment is
// a user id. A client may only subscribe to its own.
var userScopedChannelPrefixes = []string{"notifications/"}

// maySubscribe reports whether this client is allowed to subscribe to ch.
//
// Channels outside the user-scoped namespaces are shared and open to any
// authenticated client. Within them the second segment is a user id, and the
// hub previously honoured whatever the client asked for — so one user could
// name another's channel and receive their private events. The client's own
// id comes from the authenticated session, not from the message, so it cannot
// be spoofed by the subscribe frame (nester#1230).
func (c *Client) maySubscribe(ch string) bool {
	for _, prefix := range userScopedChannelPrefixes {
		if !strings.HasPrefix(ch, prefix) {
			continue
		}
		owner := strings.TrimPrefix(ch, prefix)
		if i := strings.IndexByte(owner, '/'); i >= 0 {
			owner = owner[:i]
		}
		// An unauthenticated client has no id and so owns no user channel.
		return c.userID != "" && owner == c.userID
	}
	return true
}
