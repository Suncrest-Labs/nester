package stellar

import (
	"context"
	"errors"
	"testing"
	"time"
)

type mockAPYUpdater struct {
	calls map[string]uint32
}

func newMockAPYUpdater() *mockAPYUpdater {
	return &mockAPYUpdater{calls: make(map[string]uint32)}
}

func (m *mockAPYUpdater) UpdateAPY(_ context.Context, _ string, protocolID string, apyBPS uint32) error {
	m.calls[protocolID] = apyBPS
	return nil
}

type staticAPYSource struct {
	name      string
	protocols []string
	quotes    []APYQuote
	err       error
}

func (s *staticAPYSource) Name() string {
	return s.name
}

func (s *staticAPYSource) ProtocolIDs() []string {
	out := make([]string, len(s.protocols))
	copy(out, s.protocols)
	return out
}

func (s *staticAPYSource) Fetch(context.Context) ([]APYQuote, error) {
	return s.quotes, s.err
}

type scriptedAPYSource struct {
	name      string
	protocols []string
	fetch     func(call int) ([]APYQuote, error)
	callCount int
}

func (s *scriptedAPYSource) Name() string {
	return s.name
}

func (s *scriptedAPYSource) ProtocolIDs() []string {
	out := make([]string, len(s.protocols))
	copy(out, s.protocols)
	return out
}

func (s *scriptedAPYSource) Fetch(context.Context) ([]APYQuote, error) {
	s.callCount++
	return s.fetch(s.callCount)
}

func TestNewAPYRelayerRequiresAtLeastTwoSources(t *testing.T) {
	updater := newMockAPYUpdater()
	oneSource := []APYSource{
		&staticAPYSource{name: "source-a", protocols: []string{"aave"}},
	}

	_, err := NewAPYRelayer(
		updater,
		"registry-id",
		oneSource,
		time.Minute,
		time.Hour,
		nil,
	)
	if err == nil {
		t.Fatal("expected validation error for fewer than 2 APY sources")
	}
}

func TestAPYRelayerRunOnceMergesNewestQuotePerProtocol(t *testing.T) {
	updater := newMockAPYUpdater()
	t1 := time.Date(2026, 3, 29, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(5 * time.Minute)

	sourceA := &staticAPYSource{
		name:      "source-a",
		protocols: []string{"aave"},
		quotes: []APYQuote{
			{ProtocolID: "aave", APYBPS: 510, UpdatedAt: t1, Source: "source-a"},
		},
	}
	sourceB := &staticAPYSource{
		name:      "source-b",
		protocols: []string{"aave", "blend"},
		quotes: []APYQuote{
			{ProtocolID: "aave", APYBPS: 540, UpdatedAt: t2, Source: "source-b"},
			{ProtocolID: "blend", APYBPS: 700, UpdatedAt: t2, Source: "source-b"},
		},
	}

	relayer, err := NewAPYRelayer(
		updater,
		"registry-id",
		[]APYSource{sourceA, sourceB},
		time.Minute,
		time.Hour,
		nil,
	)
	if err != nil {
		t.Fatalf("NewAPYRelayer() error = %v", err)
	}
	relayer.now = func() time.Time { return t2 }

	if err := relayer.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	if updater.calls["aave"] != 540 {
		t.Fatalf("aave APY update = %d, want 540", updater.calls["aave"])
	}
	if updater.calls["blend"] != 700 {
		t.Fatalf("blend APY update = %d, want 700", updater.calls["blend"])
	}
}

func TestAPYRelayerRunOnceReturnsSourceErrorButAppliesOtherUpdates(t *testing.T) {
	updater := newMockAPYUpdater()

	bad := &staticAPYSource{
		name:      "bad-source",
		protocols: []string{"aave"},
		err:       errors.New("upstream unavailable"),
	}
	good := &staticAPYSource{
		name:      "good-source",
		protocols: []string{"blend"},
		quotes: []APYQuote{
			{ProtocolID: "blend", APYBPS: 615, UpdatedAt: time.Now().UTC(), Source: "good-source"},
		},
	}

	relayer, err := NewAPYRelayer(
		updater,
		"registry-id",
		[]APYSource{bad, good},
		time.Minute,
		time.Hour,
		nil,
	)
	if err != nil {
		t.Fatalf("NewAPYRelayer() error = %v", err)
	}

	runErr := relayer.RunOnce(context.Background())
	if runErr == nil {
		t.Fatal("expected RunOnce() to return source collection error")
	}
	if updater.calls["blend"] != 615 {
		t.Fatalf("blend APY update = %d, want 615", updater.calls["blend"])
	}
}

func TestAPYRelayerStalenessThresholdTriggersAlert(t *testing.T) {
	updater := newMockAPYUpdater()
	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	alerts := make([]StaleAPYAlert, 0)

	sourceA := &scriptedAPYSource{
		name:      "source-a",
		protocols: []string{"aave"},
		fetch: func(call int) ([]APYQuote, error) {
			if call == 1 {
				return []APYQuote{
					{ProtocolID: "aave", APYBPS: 500, UpdatedAt: now, Source: "source-a"},
				}, nil
			}
			return nil, nil
		},
	}
	sourceB := &scriptedAPYSource{
		name:      "source-b",
		protocols: []string{"blend"},
		fetch: func(call int) ([]APYQuote, error) {
			return []APYQuote{
				{ProtocolID: "blend", APYBPS: 650, UpdatedAt: now, Source: "source-b"},
			}, nil
		},
	}

	relayer, err := NewAPYRelayer(
		updater,
		"registry-id",
		[]APYSource{sourceA, sourceB},
		time.Minute,
		time.Hour,
		func(alert StaleAPYAlert) {
			alerts = append(alerts, alert)
		},
	)
	if err != nil {
		t.Fatalf("NewAPYRelayer() error = %v", err)
	}
	relayer.now = func() time.Time { return now }

	if err := relayer.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce(first) error = %v", err)
	}
	if len(alerts) != 0 {
		t.Fatalf("got %d stale alerts on first run, want 0", len(alerts))
	}

	now = now.Add(2 * time.Hour)
	if err := relayer.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce(second) error = %v", err)
	}

	if len(alerts) != 1 {
		t.Fatalf("got %d stale alerts, want 1", len(alerts))
	}
	if alerts[0].ProtocolID != "aave" {
		t.Fatalf("alert protocol = %q, want aave", alerts[0].ProtocolID)
	}
	if alerts[0].Age <= time.Hour {
		t.Fatalf("alert age = %s, want > 1h", alerts[0].Age)
	}
}
