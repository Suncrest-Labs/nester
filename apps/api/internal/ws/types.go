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
