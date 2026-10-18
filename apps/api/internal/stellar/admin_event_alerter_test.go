package stellar

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type recordingAdminAlertSink struct {
	alerts []AdminEventAlert
}

func (s *recordingAdminAlertSink) SendAdminEventAlert(_ context.Context, alert AdminEventAlert) error {
	s.alerts = append(s.alerts, alert)
	return nil
}

func TestAdminEventClassifier_FiresForEveryAdminTopicInEventsMD(t *testing.T) {
	eventsDocPath := filepath.Join("..", "..", "..", "..", "packages", "contracts", "EVENTS.md")
	rawDoc, err := os.ReadFile(eventsDocPath)
	if err != nil {
		t.Fatalf("read EVENTS.md: %v", err)
	}
	eventsDoc := string(rawDoc)

	topics := AdminConfigEventTopics()
	if len(topics) != 26 {
		t.Fatalf("expected 26 admin/config topics, got %d", len(topics))
	}

	sink := &recordingAdminAlertSink{}
	classifier := NewAdminEventClassifier(sink)

	for i, topic := range topics {
		if !strings.Contains(eventsDoc, "### "+topic) {
			t.Fatalf("topic %q is not documented as a section in EVENTS.md", topic)
		}

		evt := indexedEvent{
			ID:         "evt-admin-" + topic,
			ContractID: "CCWAMN2476B3R236R3T7G7M3MMK23R2J4S3I6W3M2A4J6S3I6W3M2A4J",
			EventType:  topic,
			Ledger:     uint64(1000 + i),
			TxHash:     "tx-" + strings.ToLower(topic),
			Data: map[string]any{
				"admin":     "GADMIN56CHARSADDRESS000000000000000000000000000000000000",
				"field":     "MAX_DEP",
				"old_value": "100000000000",
				"new_value": "250000000000",
			},
		}

		fired, err := classifier.InspectAndAlert(context.Background(), evt)
		if err != nil {
			t.Fatalf("InspectAndAlert(%s): %v", topic, err)
		}
		if !fired {
			t.Fatalf("expected alert to fire for admin topic %s", topic)
		}
	}

	if len(sink.alerts) != len(topics) {
		t.Fatalf("fired %d alerts, want %d", len(sink.alerts), len(topics))
	}

	for i, alert := range sink.alerts {
		if alert.Topic != topics[i] {
			t.Fatalf("alert[%d].Topic = %q, want %q", i, alert.Topic, topics[i])
		}
		if alert.TxHash == "" || alert.Signer == "" || alert.Diff == "" {
			t.Fatalf("alert[%d] missing tx_hash, signer, or diff: %+v", i, alert)
		}
		if alert.Severity != "critical" {
			t.Fatalf("alert[%d].Severity = %q, want critical", i, alert.Severity)
		}
	}
}

func TestAdminEventClassifier_IgnoresUserEventsAndRespectsPlannedSuppression(t *testing.T) {
	sink := &recordingAdminAlertSink{}
	classifier := NewAdminEventClassifier(sink)
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	classifier.now = func() time.Time { return now }

	// User deposit event must never page.
	fired, err := classifier.InspectAndAlert(context.Background(), indexedEvent{
		ID:         "evt-user-dep",
		ContractID: "C1",
		EventType:  "DEPOSIT",
		TxHash:     "tx-dep",
	})
	if err != nil || fired {
		t.Fatalf("expected DEPOSIT not to fire alert, fired=%v err=%v", fired, err)
	}

	// Planned CAP_CHG suppressed ahead of time.
	if err := classifier.SuppressPlannedChange("C1", "CAP_CHG", "stage-2 cap rollout #1568", 15*time.Minute); err != nil {
		t.Fatalf("SuppressPlannedChange: %v", err)
	}

	capEvt := indexedEvent{
		ID:         "evt-cap-1",
		ContractID: "C1",
		EventType:  "CAP_CHG",
		TxHash:     "tx-cap-1",
		Data: map[string]any{
			"admin":     "GADMIN",
			"field":     "MAX_DEP",
			"old_value": "50000",
			"new_value": "250000",
		},
	}
	fired, err = classifier.InspectAndAlert(context.Background(), capEvt)
	if err != nil || fired {
		t.Fatalf("expected suppressed CAP_CHG not to fire, fired=%v err=%v", fired, err)
	}

	// A second CAP_CHG after the one-shot suppression is consumed must fire immediately.
	capEvt.ID = "evt-cap-2"
	fired, err = classifier.InspectAndAlert(context.Background(), capEvt)
	if err != nil || !fired {
		t.Fatalf("expected second CAP_CHG to fire, fired=%v err=%v", fired, err)
	}
}
