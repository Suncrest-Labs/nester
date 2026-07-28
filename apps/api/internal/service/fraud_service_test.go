package service

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/fraud"
)

// mockFraudRepository is an in-memory implementation of fraud.Repository for tests.
type mockFraudRepository struct {
	mu            sync.Mutex
	baselines     map[uuid.UUID]*fraud.UserBaseline
	destinations  map[uuid.UUID][]fraud.KnownDestination
	devices       map[uuid.UUID][]fraud.KnownDevice
	flags         map[uuid.UUID]*fraud.FraudFlag
	actions       []fraud.FraudAction
	authEvents    []fraud.AuthEvent
	metrics       []fraud.FraudMetric
}

func newMockFraudRepo() *mockFraudRepository {
	return &mockFraudRepository{
		baselines:    make(map[uuid.UUID]*fraud.UserBaseline),
		destinations: make(map[uuid.UUID][]fraud.KnownDestination),
		devices:      make(map[uuid.UUID][]fraud.KnownDevice),
		flags:        make(map[uuid.UUID]*fraud.FraudFlag),
	}
}

func (m *mockFraudRepository) GetBaseline(_ context.Context, userID uuid.UUID) (*fraud.UserBaseline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.baselines[userID]
	if !ok {
		return nil, fraud.ErrBaselineStale
	}
	return b, nil
}

func (m *mockFraudRepository) UpsertBaseline(_ context.Context, b fraud.UserBaseline) (*fraud.UserBaseline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.baselines[b.UserID] = &b
	return &b, nil
}

func (m *mockFraudRepository) GetKnownDestinations(_ context.Context, userID uuid.UUID) ([]fraud.KnownDestination, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.destinations[userID], nil
}

func (m *mockFraudRepository) UpsertKnownDestination(_ context.Context, userID uuid.UUID, dest string) (*fraud.KnownDestination, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kd := fraud.KnownDestination{
		ID:              uuid.New(),
		UserID:          userID,
		Destination:     dest,
		FirstSeenAt:     time.Now(),
		LastSeenAt:      time.Now(),
		OccurrenceCount: 1,
	}
	m.destinations[userID] = append(m.destinations[userID], kd)
	return &kd, nil
}

func (m *mockFraudRepository) IsKnownDestination(_ context.Context, userID uuid.UUID, dest string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.destinations[userID] {
		if d.Destination == dest {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockFraudRepository) GetKnownDevices(_ context.Context, userID uuid.UUID) ([]fraud.KnownDevice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.devices[userID], nil
}

func (m *mockFraudRepository) UpsertKnownDevice(_ context.Context, userID uuid.UUID, fp string) (*fraud.KnownDevice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kd := fraud.KnownDevice{
		ID:                uuid.New(),
		UserID:            userID,
		DeviceFingerprint: fp,
		FirstSeenAt:       time.Now(),
		LastSeenAt:        time.Now(),
	}
	m.devices[userID] = append(m.devices[userID], kd)
	return &kd, nil
}

func (m *mockFraudRepository) IsKnownDevice(_ context.Context, userID uuid.UUID, fp string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.devices[userID] {
		if d.DeviceFingerprint == fp {
			return true, nil
		}
	}
	return false, nil
}

func (m *mockFraudRepository) CreateFlag(_ context.Context, f fraud.FraudFlag) (*fraud.FraudFlag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flags[f.ID] = &f
	return &f, nil
}

func (m *mockFraudRepository) GetFlag(_ context.Context, id uuid.UUID) (*fraud.FraudFlag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.flags[id]
	if !ok {
		return nil, fraud.ErrFlagNotFound
	}
	return f, nil
}

func (m *mockFraudRepository) ListOpenFlags(_ context.Context, userID uuid.UUID) ([]fraud.FraudFlag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var flags []fraud.FraudFlag
	for _, f := range m.flags {
		if f.UserID == userID && f.Status == fraud.FlagStatusOpen {
			flags = append(flags, *f)
		}
	}
	return flags, nil
}

func (m *mockFraudRepository) UpdateFlagStatus(_ context.Context, id uuid.UUID, status fraud.FlagStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.flags[id]; ok {
		f.Status = status
	}
	return nil
}

func (m *mockFraudRepository) ClearFlagByUser(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.flags[id]; ok {
		f.Status = fraud.FlagStatusCleared
		f.ClearedByUser = true
	}
	return nil
}

func (m *mockFraudRepository) CreateAction(_ context.Context, a fraud.FraudAction) (*fraud.FraudAction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.actions = append(m.actions, a)
	return &a, nil
}

func (m *mockFraudRepository) GetActiveHolds(_ context.Context, userID uuid.UUID) ([]fraud.FraudAction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var holds []fraud.FraudAction
	for _, a := range m.actions {
		if a.Action == fraud.ActionHold && a.ExpiresAt != nil {
			holds = append(holds, a)
		}
	}
	return holds, nil
}

func (m *mockFraudRepository) ExpireHold(_ context.Context, actionID uuid.UUID) error {
	return nil
}

func (m *mockFraudRepository) RecordAuthEvent(_ context.Context, e fraud.AuthEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.authEvents = append(m.authEvents, e)
	return nil
}

func (m *mockFraudRepository) RecentAuthEvents(_ context.Context, userID uuid.UUID, _ time.Duration) ([]fraud.AuthEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var events []fraud.AuthEvent
	for _, e := range m.authEvents {
		if e.UserID != nil && *e.UserID == userID {
			events = append(events, e)
		}
	}
	return events, nil
}

func (m *mockFraudRepository) RecordMetric(_ context.Context, metric fraud.FraudMetric) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metrics = append(m.metrics, metric)
	return nil
}

func (m *mockFraudRepository) GetMetricRate(_ context.Context, metricName string, since time.Duration) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-since)
	count := 0
	for _, met := range m.metrics {
		if met.MetricName == metricName && met.RecordedAt.After(cutoff) {
			count++
		}
	}
	return float64(count), nil
}

// Helper to create a test baseline
func testBaseline(userID uuid.UUID, avgAmount, stddev float64, txCount int) *fraud.UserBaseline {
	return &fraud.UserBaseline{
		ID:                      uuid.New(),
		UserID:                  userID,
		AvgTransactionAmount:    decimal.NewFromFloat(avgAmount),
		StddevTransactionAmount: decimal.NewFromFloat(stddev),
		MaxTransactionAmount:    decimal.NewFromFloat(avgAmount + 3*stddev),
		AvgDailyTransactions:    2.0,
		AvgHourlyTransactions:   0.5,
		KnownDestinationCount:   5,
		KnownDeviceCount:        2,
		TransactionCount:        txCount,
		LastUpdatedAt:           time.Now(),
		CreatedAt:               time.Now().Add(-90 * 24 * time.Hour),
	}
}

// Context key for test timestamps
type testClockKey struct{}

func withTestClock(ctx context.Context, t time.Time) context.Context {
	return context.WithValue(ctx, testClockKey{}, t)
}
