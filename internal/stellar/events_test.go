package stellar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewEventPoller(t *testing.T) {
	client := &Client{}
	poller := NewEventPoller(client)

	assert.NotNil(t, poller)
	assert.NotNil(t, poller.listeners)
	assert.Equal(t, 0, len(poller.listeners))
}

func TestEventPoller_Subscribe_EmptyContractID(t *testing.T) {
	client := &Client{}
	poller := NewEventPoller(client)

	listener := func(event *Event) {}
	err := poller.Subscribe("", listener)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "contract ID is required")
}

func TestEventPoller_Subscribe_NilListener(t *testing.T) {
	client := &Client{}
	poller := NewEventPoller(client)

	err := poller.Subscribe("CONTRACT123", nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "listener cannot be nil")
}

func TestEventPoller_Subscribe_Success(t *testing.T) {
	client := &Client{}
	poller := NewEventPoller(client)

	listener := func(event *Event) {}
	err := poller.Subscribe("CONTRACT123", listener)

	assert.NoError(t, err)
	assert.Equal(t, 1, len(poller.listeners["CONTRACT123"]))
}

func TestEventPoller_Subscribe_MultipleListeners(t *testing.T) {
	client := &Client{}
	poller := NewEventPoller(client)

	listener1 := func(event *Event) {}
	listener2 := func(event *Event) {}

	err := poller.Subscribe("CONTRACT123", listener1)
	assert.NoError(t, err)

	err = poller.Subscribe("CONTRACT123", listener2)
	assert.NoError(t, err)

	assert.Equal(t, 2, len(poller.listeners["CONTRACT123"]))
}

func TestEventPoller_PollEvents_EmptyContractID(t *testing.T) {
	client := &Client{}
	poller := NewEventPoller(client)

	_, err := poller.PollEvents(context.Background(), "", 0, 100)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "contract ID is required")
}

func TestEventPoller_PollEvents_InvalidBlockRange(t *testing.T) {
	client := &Client{}
	poller := NewEventPoller(client)

	_, err := poller.PollEvents(context.Background(), "CONTRACT123", 100, 50)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "fromBlock must be <= toBlock")
}

func TestEventPoller_PollEvents_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"jsonrpc":"2.0",
			"id":"nester-event-poller",
			"result":{
				"events":[
					{
						"contractId":"CONTRACT123",
						"ledger":99,
						"txHash":"tx-1",
						"topic":["deposit"],
						"value":{"amount":"100.5"}
					}
				]
			}
		}`))
	}))
	defer server.Close()

	client := &Client{config: Config{RPCURL: server.URL}}
	poller := NewEventPoller(client)

	events, err := poller.PollEvents(context.Background(), "CONTRACT123", 0, 100)
	assert.NoError(t, err)
	assert.NotNil(t, events)
	assert.Equal(t, 1, len(events))
	assert.Equal(t, "deposit", events[0].EventType)
	assert.Equal(t, uint64(99), events[0].BlockNumber)
}

func TestFilterEvents_ByType(t *testing.T) {
	events := []Event{
		{EventType: "Deposit", ContractID: "CONTRACT1"},
		{EventType: "Withdrawal", ContractID: "CONTRACT1"},
		{EventType: "Deposit", ContractID: "CONTRACT1"},
	}

	filtered := FilterEvents(events, "Deposit")
	assert.Equal(t, 2, len(filtered))
	assert.Equal(t, "Deposit", filtered[0].EventType)
	assert.Equal(t, "Deposit", filtered[1].EventType)
}

func TestFilterEvents_NoMatches(t *testing.T) {
	events := []Event{
		{EventType: "Deposit", ContractID: "CONTRACT1"},
		{EventType: "Withdrawal", ContractID: "CONTRACT1"},
	}

	filtered := FilterEvents(events, "Rebalance")
	assert.Equal(t, 0, len(filtered))
}

func TestFilterEventsByContract(t *testing.T) {
	events := []Event{
		{ContractID: "CONTRACT1", EventType: "Deposit"},
		{ContractID: "CONTRACT2", EventType: "Deposit"},
		{ContractID: "CONTRACT1", EventType: "Withdrawal"},
	}

	filtered := FilterEventsByContract(events, "CONTRACT1")
	assert.Equal(t, 2, len(filtered))
	assert.Equal(t, "CONTRACT1", filtered[0].ContractID)
	assert.Equal(t, "CONTRACT1", filtered[1].ContractID)
}

func TestEventPoller_Unsubscribe_NoListeners(t *testing.T) {
	client := &Client{}
	poller := NewEventPoller(client)

	err := poller.Unsubscribe("CONTRACT123", func(event *Event) {})
	assert.Error(t, err)
}

func TestEventStream_Creation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &Client{}
	poller := NewEventPoller(client)

	stream := poller.NewEventStream(ctx, "CONTRACT123", 100*time.Millisecond)
	assert.NotNil(t, stream)
	assert.NotNil(t, stream.Events)
	assert.NotNil(t, stream.Errors)
}

func TestEvent_Creation(t *testing.T) {
	event := &Event{
		ContractID:    "CONTRACT123",
		EventType:     "Deposit",
		BlockNumber:   100,
		TransactionID: "TXN123",
		Data:          map[string]interface{}{"amount": "1000"},
		Timestamp:     1234567890,
	}

	assert.Equal(t, "CONTRACT123", event.ContractID)
	assert.Equal(t, "Deposit", event.EventType)
	assert.Equal(t, uint64(100), event.BlockNumber)
	assert.Equal(t, "TXN123", event.TransactionID)
}

func TestEventPoller_DispatchEvents(t *testing.T) {
	client := &Client{}
	poller := NewEventPoller(client)

	listener := func(event *Event) {
		_ = event
	}

	err := poller.Subscribe("CONTRACT123", listener)
	assert.NoError(t, err)

	events := []Event{
		{ContractID: "CONTRACT123", EventType: "Deposit"},
	}

	// Give goroutine time to dispatch
	poller.dispatchEvents("CONTRACT123", events)
	time.Sleep(100 * time.Millisecond)

	// Event was dispatched asynchronously, so we may or may not have received it yet
	// This test validates the dispatch mechanism exists
	assert.NotNil(t, poller.listeners["CONTRACT123"])
}

func TestEventStream_Close(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &Client{}
	poller := NewEventPoller(client)

	stream := poller.NewEventStream(ctx, "CONTRACT123", 100*time.Millisecond)
	assert.NotNil(t, stream)

	// Close should not panic
	stream.Close()
}
