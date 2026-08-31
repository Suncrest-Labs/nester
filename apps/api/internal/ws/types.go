package ws

import (
	"time"
)

// EventType defines the type of event being broadcast
type EventType string

const (
	// Vault events
	EventBalanceUpdated       EventType = "balance_updated"
	EventDepositConfirmed     EventType = "deposit_confirmed"
	EventWithdrawalConfirmed  EventType = "withdrawal_confirmed"
	EventYieldAccrued         EventType = "yield_accrued"
	EventHarvestCompleted     EventType = "harvest_completed"
	EventVaultPaused          EventType = "vault_paused"
	EventVaultUnpaused        EventType = "vault_unpaused"

	// Settlement events
	EventStatusChanged        EventType = "status_changed"
	EventSettlementCompleted  EventType = "settlement_completed"
	EventSettlementFailed     EventType = "settlement_failed"

	// System events
	EventMaintenanceScheduled EventType = "maintenance_scheduled"
	EventNetworkStatus        EventType = "network_status"
	EventEventsDropped        EventType = "events_dropped"

	// EventPong answers an application-level ping. Browsers cannot send
	// WebSocket protocol ping frames from JavaScript, so the DApp keepalive
	// rides on top of the normal message channel: the client sends
	// {"action":"ping"} and expects this event back inside its pong timeout.
	// Without it a client cannot distinguish a live link from a socket that
	// has been silently blackholed (proxy idle timeout, laptop sleep), and
	// would keep rendering a stale balance as if it were current.
	EventPong EventType = "pong"
)

// Actions a client may send on the wire.
const (
	ActionSubscribe   = "subscribe"
	ActionUnsubscribe = "unsubscribe"
	ActionPing        = "ping"
)

// Event represents a broadcastable event to clients
type Event struct {
	Channel   string      `json:"channel"`
	Type      EventType   `json:"event"`
	Data      interface{} `json:"data"`
	Timestamp time.Time   `json:"timestamp,omitempty"`
}

// ClientMessage is sent from the client to the server
type ClientMessage struct {
	Action   string   `json:"action"` // e.g. "subscribe", "unsubscribe"
	Channels []string `json:"channels"`
}
