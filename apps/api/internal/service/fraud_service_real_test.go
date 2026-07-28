package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/fraud"
)

func newTestDetector(repo *mockFraudRepository) (*FraudDetector, time.Time) {
	detector := NewFraudDetector(repo)
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	detector.WithNow(func() time.Time { return now })
	return detector, now
}

func TestEvaluate_NormalTransaction_NoFlag(t *testing.T) {
	repo := newMockFraudRepo()
	detector, _ := newTestDetector(repo)
	userID := uuid.New()

	// Seed a baseline: avg $100, stddev $20, 20 transactions
	baseline := testBaseline(userID, 100, 20, 20)
	repo.baselines[userID] = baseline

	// Seed known destination and device
	repo.destinations[userID] = []fraud.KnownDestination{
		{Destination: "dest-1", OccurrenceCount: 10},
	}
	repo.devices[userID] = []fraud.KnownDevice{
		{DeviceFingerprint: "device-1"},
	}

	result, err := detector.Evaluate(context.Background(), TransactionContext{
		UserID:            userID,
		Amount:            decimal.NewFromFloat(120),
		EventType:         "withdrawal",
		Destination:       "dest-1",
		DeviceFingerprint: "device-1",
		IsWithdrawal:      true,
	})
	require.NoError(t, err)
	assert.Nil(t, result.Flag, "normal transaction should not create a flag")
	assert.Equal(t, fraud.SeverityLow, result.Severity)
}

func TestEvaluate_LargeWithdrawalNewDestination_HighSeverity(t *testing.T) {
	repo := newMockFraudRepo()
	detector, _ := newTestDetector(repo)
	userID := uuid.New()

	baseline := testBaseline(userID, 100, 20, 20)
	repo.baselines[userID] = baseline

	repo.devices[userID] = []fraud.KnownDevice{
		{DeviceFingerprint: "device-1"},
	}

	// Large withdrawal to new destination
	result, err := detector.Evaluate(context.Background(), TransactionContext{
		UserID:            userID,
		Amount:            decimal.NewFromFloat(500),
		EventType:         "withdrawal",
		Destination:       "new-unknown-dest",
		DeviceFingerprint: "device-1",
		IsWithdrawal:      true,
	})
	require.NoError(t, err)
	assert.NotNil(t, result.Flag, "anomalous withdrawal should create a flag")
	assert.True(t, result.Score > 0, "score should be positive")
	assert.Contains(t, []fraud.Severity{fraud.SeverityMedium, fraud.SeverityHigh, fraud.SeverityCritical}, result.Severity)
}

func TestEvaluate_PostCredentialChangeWithdrawal_HighSeverity(t *testing.T) {
	repo := newMockFraudRepo()
	detector, now := newTestDetector(repo)
	userID := uuid.New()

	baseline := testBaseline(userID, 100, 20, 20)
	repo.baselines[userID] = baseline

	repo.destinations[userID] = []fraud.KnownDestination{
		{Destination: "dest-1", OccurrenceCount: 10},
	}
	repo.devices[userID] = []fraud.KnownDevice{
		{DeviceFingerprint: "device-1"},
	}

	// Record a password reset 30 minutes ago
	resetTime := now.Add(-30 * time.Minute)
	repo.authEvents = append(repo.authEvents, fraud.AuthEvent{
		ID:        uuid.New(),
		UserID:    &userID,
		EventType: "password_reset",
		CreatedAt: resetTime,
	})

	// Now a withdrawal
	result, err := detector.Evaluate(context.Background(), TransactionContext{
		UserID:            userID,
		Amount:            decimal.NewFromFloat(150),
		EventType:         "withdrawal",
		Destination:       "dest-1",
		DeviceFingerprint: "device-1",
		IsWithdrawal:      true,
	})
	require.NoError(t, err)
	assert.NotNil(t, result.Flag)
	assert.True(t, result.Score > 0.2, "post-credential-change activity should increase score")
}

func TestEvaluate_AuthFailureBurst_Detected(t *testing.T) {
	repo := newMockFraudRepo()
	detector, now := newTestDetector(repo)
	userID := uuid.New()

	// Record 5 auth failures in the last 10 minutes
	for i := 0; i < 5; i++ {
		repo.authEvents = append(repo.authEvents, fraud.AuthEvent{
			ID:        uuid.New(),
			UserID:    &userID,
			EventType: "auth_failure",
			CreatedAt: now.Add(-time.Duration(i) * time.Minute),
		})
	}

	result, err := detector.Evaluate(context.Background(), TransactionContext{
		UserID:            userID,
		Amount:            decimal.NewFromFloat(50),
		EventType:         "auth_failure",
		DeviceFingerprint: "device-1",
	})
	require.NoError(t, err)
	assert.NotNil(t, result.Flag)

	// Check that auth_failure_burst signal is present
	found := false
	for _, sig := range result.Signals {
		if sig.Name == "auth_failure_burst" {
			found = true
			break
		}
	}
	assert.True(t, found, "auth_failure_burst signal should be present")
}

func TestEvaluate_ImpossibleTravel_Detected(t *testing.T) {
	repo := newMockFraudRepo()
	detector, now := newTestDetector(repo)
	userID := uuid.New()

	// Record login from New York 30 minutes ago
	nyLat, nyLon := 40.7128, -74.0060
	_ = now
	repo.authEvents = append(repo.authEvents, fraud.AuthEvent{
		ID:          uuid.New(),
		UserID:      &userID,
		EventType:   "login",
		LocationLat: &nyLat,
		LocationLon: &nyLon,
		CreatedAt:   now.Add(-30 * time.Minute),
	})

	// Now login from Tokyo (impossible in 30 minutes)
	tokyoLat, tokyoLon := 35.6762, 139.6503
	result, err := detector.Evaluate(context.Background(), TransactionContext{
		UserID:            userID,
		Amount:            decimal.NewFromFloat(50),
		EventType:         "login",
		LocationLat:       &tokyoLat,
		LocationLon:       &tokyoLon,
		DeviceFingerprint: "device-1",
	})
	require.NoError(t, err)
	assert.NotNil(t, result.Flag)

	found := false
	for _, sig := range result.Signals {
		if sig.Name == "impossible_travel" {
			found = true
			break
		}
	}
	assert.True(t, found, "impossible_travel signal should be present")
}

func TestEvaluate_NewUser_LargeTransaction_AbsoluteThreshold(t *testing.T) {
	repo := newMockFraudRepo()
	detector, _ := newTestDetector(repo)
	userID := uuid.New()

	// No baseline set — new user

	result, err := detector.Evaluate(context.Background(), TransactionContext{
		UserID:            userID,
		Amount:            decimal.NewFromFloat(60000),
		EventType:         "withdrawal",
		Destination:       "some-dest",
		DeviceFingerprint: "device-1",
		IsWithdrawal:      true,
	})
	require.NoError(t, err)
	assert.NotNil(t, result.Flag, "new user with large transaction should be flagged")
}

func TestEvaluate_GraduatedResponse_MediumGetsStepUpAuth(t *testing.T) {
	repo := newMockFraudRepo()
	detector, _ := newTestDetector(repo)
	userID := uuid.New()

	baseline := testBaseline(userID, 100, 20, 20)
	repo.baselines[userID] = baseline

	// New device + occasional destination should produce medium-ish severity
	result, err := detector.Evaluate(context.Background(), TransactionContext{
		UserID:            userID,
		Amount:            decimal.NewFromFloat(130),
		EventType:         "withdrawal",
		Destination:       "occasional-dest",
		DeviceFingerprint: "new-device-fp",
		IsWithdrawal:      true,
	})
	require.NoError(t, err)
	assert.NotNil(t, result.Flag)

	if result.Severity == fraud.SeverityMedium {
		assert.Contains(t, result.Actions, fraud.ActionStepUpAuth, "medium severity should include step-up auth")
		assert.NotContains(t, result.Actions, fraud.ActionHold, "medium severity should not hold")
	}
}

func TestEvaluate_GraduatedResponse_HighGetsHold(t *testing.T) {
	repo := newMockFraudRepo()
	detector, now := newTestDetector(repo)
	userID := uuid.New()

	baseline := testBaseline(userID, 100, 20, 20)
	repo.baselines[userID] = baseline

	// Combine multiple signals: new destination + post credential change + new device
	resetTime := now.Add(-30 * time.Minute)
	repo.authEvents = append(repo.authEvents, fraud.AuthEvent{
		ID:        uuid.New(),
		UserID:    &userID,
		EventType: "password_reset",
		CreatedAt: resetTime,
	})

	result, err := detector.Evaluate(context.Background(), TransactionContext{
		UserID:            userID,
		Amount:            decimal.NewFromFloat(300),
		EventType:         "withdrawal",
		Destination:       "brand-new-dest",
		DeviceFingerprint: "unknown-device",
		IsWithdrawal:      true,
	})
	require.NoError(t, err)
	assert.NotNil(t, result.Flag)

	if result.Severity == fraud.SeverityHigh || result.Severity == fraud.SeverityCritical {
		assert.Contains(t, result.Actions, fraud.ActionHold, "high+ severity should include hold")
	}
}

func TestSelfClearFlag_MediumOnly(t *testing.T) {
	repo := newMockFraudRepo()
	detector, _ := newTestDetector(repo)
	userID := uuid.New()

	// Create a medium flag
	flag := &fraud.FraudFlag{
		ID:       uuid.New(),
		UserID:   userID,
		Severity: fraud.SeverityMedium,
		Status:   fraud.FlagStatusOpen,
	}
	repo.flags[flag.ID] = flag

	err := detector.SelfClearFlag(context.Background(), flag.ID, userID)
	require.NoError(t, err)
	assert.Equal(t, fraud.FlagStatusCleared, repo.flags[flag.ID].Status)
	assert.True(t, repo.flags[flag.ID].ClearedByUser)
}

func TestSelfClearFlag_RejectsHighSeverity(t *testing.T) {
	repo := newMockFraudRepo()
	detector, _ := newTestDetector(repo)
	userID := uuid.New()

	flag := &fraud.FraudFlag{
		ID:       uuid.New(),
		UserID:   userID,
		Severity: fraud.SeverityHigh,
		Status:   fraud.FlagStatusOpen,
	}
	repo.flags[flag.ID] = flag

	err := detector.SelfClearFlag(context.Background(), flag.ID, userID)
	assert.ErrorIs(t, err, fraud.ErrInvalidFlag)
}

func TestSelfClearFlag_RejectsWrongUser(t *testing.T) {
	repo := newMockFraudRepo()
	detector, _ := newTestDetector(repo)
	userID := uuid.New()
	otherUser := uuid.New()

	flag := &fraud.FraudFlag{
		ID:       uuid.New(),
		UserID:   otherUser,
		Severity: fraud.SeverityMedium,
		Status:   fraud.FlagStatusOpen,
	}
	repo.flags[flag.ID] = flag

	err := detector.SelfClearFlag(context.Background(), flag.ID, userID)
	assert.ErrorIs(t, err, fraud.ErrFlagNotFound)
}

func TestIsBlocked_ChecksActiveHolds(t *testing.T) {
	repo := newMockFraudRepo()
	detector, now := newTestDetector(repo)
	userID := uuid.New()

	// No holds
	blocked, err := detector.IsBlocked(context.Background(), userID)
	require.NoError(t, err)
	assert.False(t, blocked)

	// Add an active hold
	futureExpiry := now.Add(30 * time.Minute)
	repo.actions = append(repo.actions, fraud.FraudAction{
		ID:        uuid.New(),
		FlagID:    uuid.New(),
		Action:    fraud.ActionHold,
		ExpiresAt: &futureExpiry,
		CreatedAt: now,
	})

	blocked, err = detector.IsBlocked(context.Background(), userID)
	require.NoError(t, err)
	assert.True(t, blocked)
}

func TestIsBlocked_ExpiredHoldNotBlocked(t *testing.T) {
	repo := newMockFraudRepo()
	detector, now := newTestDetector(repo)
	userID := uuid.New()

	// Add an expired hold
	pastExpiry := now.Add(-5 * time.Minute)
	repo.actions = append(repo.actions, fraud.FraudAction{
		ID:        uuid.New(),
		FlagID:    uuid.New(),
		Action:    fraud.ActionHold,
		ExpiresAt: &pastExpiry,
		CreatedAt: now.Add(-35 * time.Minute),
	})

	blocked, err := detector.IsBlocked(context.Background(), userID)
	require.NoError(t, err)
	assert.False(t, blocked, "expired hold should not block")
}

func TestUpdateBaseline_ComputesCorrectly(t *testing.T) {
	repo := newMockFraudRepo()
	detector, _ := newTestDetector(repo)
	userID := uuid.New()

	amounts := []decimal.Decimal{
		decimal.NewFromFloat(100),
		decimal.NewFromFloat(120),
		decimal.NewFromFloat(80),
		decimal.NewFromFloat(110),
		decimal.NewFromFloat(90),
	}

	err := detector.UpdateBaseline(context.Background(), userID, amounts, []float64{2, 3, 1}, []float64{0.5, 0.3})
	require.NoError(t, err)

	baseline, err := repo.GetBaseline(context.Background(), userID)
	require.NoError(t, err)
	assert.Equal(t, 100.0, baseline.AvgTransactionAmount.InexactFloat64())
	assert.Equal(t, 5, baseline.TransactionCount)
	assert.True(t, baseline.StddevTransactionAmount.GreaterThan(decimal.Zero))
}

func TestNewDestination_FirstEver_GetsHigherScore(t *testing.T) {
	repo := newMockFraudRepo()
	detector, _ := newTestDetector(repo)
	userID := uuid.New()

	// No known destinations
	result, err := detector.Evaluate(context.Background(), TransactionContext{
		UserID:            userID,
		Amount:            decimal.NewFromFloat(100),
		EventType:         "withdrawal",
		Destination:       "first-dest-ever",
		DeviceFingerprint: "device-1",
		IsWithdrawal:      true,
	})
	require.NoError(t, err)

	found := false
	for _, sig := range result.Signals {
		if sig.Name == "new_destination" {
			found = true
			assert.Equal(t, 0.4, sig.Score, "first destination should get 0.4 score")
			break
		}
	}
	assert.True(t, found, "new_destination signal should be present for first withdrawal")
}

func TestOccasionalDestination_LowerScore(t *testing.T) {
	repo := newMockFraudRepo()
	detector, _ := newTestDetector(repo)
	userID := uuid.New()

	// Destination seen only once
	repo.destinations[userID] = []fraud.KnownDestination{
		{Destination: "rare-dest", OccurrenceCount: 1},
	}

	result, err := detector.Evaluate(context.Background(), TransactionContext{
		UserID:            userID,
		Amount:            decimal.NewFromFloat(100),
		EventType:         "withdrawal",
		Destination:       "rare-dest",
		DeviceFingerprint: "device-1",
		IsWithdrawal:      true,
	})
	require.NoError(t, err)

	found := false
	for _, sig := range result.Signals {
		if sig.Name == "occasional_destination" {
			found = true
			assert.Equal(t, 0.15, sig.Score, "occasional destination should get 0.15 score")
			break
		}
	}
	assert.True(t, found, "occasional_destination signal should be present")
}

func TestAggregateScore_ZeroSignals(t *testing.T) {
	score := aggregateScore([]fraud.Signal{})
	assert.Equal(t, 0.0, score)
}

func TestAggregateScore_SingleSignal(t *testing.T) {
	signals := []fraud.Signal{{Score: 0.5}}
	score := aggregateScore(signals)
	assert.InDelta(t, 0.5, score, 0.001)
}

func TestAggregateScore_MultipleSignals(t *testing.T) {
	signals := []fraud.Signal{
		{Score: 0.3},
		{Score: 0.4},
	}
	score := aggregateScore(signals)
	// 1 - (1-0.3)*(1-0.4) = 1 - 0.42 = 0.58
	assert.InDelta(t, 0.58, score, 0.001)
}

func TestScoreToSeverity_Bands(t *testing.T) {
	tests := []struct {
		score    float64
		expected fraud.Severity
	}{
		{0.0, fraud.SeverityLow},
		{0.1, fraud.SeverityLow},
		{0.39, fraud.SeverityLow},
		{0.4, fraud.SeverityMedium},
		{0.5, fraud.SeverityMedium},
		{0.69, fraud.SeverityMedium},
		{0.7, fraud.SeverityHigh},
		{0.8, fraud.SeverityHigh},
		{0.89, fraud.SeverityHigh},
		{0.9, fraud.SeverityCritical},
		{1.0, fraud.SeverityCritical},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.expected, scoreToSeverity(tc.score), "score %.1f", tc.score)
	}
}

func TestSeverityToActions_Graduated(t *testing.T) {
	logActions := severityToActions(fraud.SeverityLow)
	assert.Contains(t, logActions, fraud.ActionLog)

	mediumActions := severityToActions(fraud.SeverityMedium)
	assert.Contains(t, mediumActions, fraud.ActionStepUpAuth)
	assert.NotContains(t, mediumActions, fraud.ActionHold)

	highActions := severityToActions(fraud.SeverityHigh)
	assert.Contains(t, highActions, fraud.ActionHold)
	assert.Contains(t, highActions, fraud.ActionStepUpAuth)

	criticalActions := severityToActions(fraud.SeverityCritical)
	assert.Contains(t, criticalActions, fraud.ActionHold)
	assert.Contains(t, criticalActions, fraud.ActionStepUpAuth)
}

func TestHaversineDistance_SamePoint(t *testing.T) {
	dist := haversineDistance(40.7128, -74.0060, 40.7128, -74.0060)
	assert.InDelta(t, 0.0, dist, 0.001)
}

func TestHaversineDistance_NYToLondon(t *testing.T) {
	dist := haversineDistance(40.7128, -74.0060, 51.5074, -0.1278)
	assert.True(t, dist > 5000, "NY to London should be ~5570km, got %.0f", dist)
	assert.True(t, dist < 6000, "NY to London should be ~5570km, got %.0f", dist)
}

func TestNewDevice_SignalPresent(t *testing.T) {
	repo := newMockFraudRepo()
	detector, _ := newTestDetector(repo)
	userID := uuid.New()

	repo.devices[userID] = []fraud.KnownDevice{
		{DeviceFingerprint: "known-device"},
	}

	result, err := detector.Evaluate(context.Background(), TransactionContext{
		UserID:            userID,
		Amount:            decimal.NewFromFloat(50),
		EventType:         "login",
		DeviceFingerprint: "brand-new-device",
	})
	require.NoError(t, err)

	found := false
	for _, sig := range result.Signals {
		if sig.Name == "new_device" {
			found = true
			break
		}
	}
	assert.True(t, found, "new_device signal should be present for unknown device")
}

func TestKnownDevice_NoSignal(t *testing.T) {
	repo := newMockFraudRepo()
	detector, _ := newTestDetector(repo)
	userID := uuid.New()

	repo.devices[userID] = []fraud.KnownDevice{
		{DeviceFingerprint: "known-device"},
	}

	result, err := detector.Evaluate(context.Background(), TransactionContext{
		UserID:            userID,
		Amount:            decimal.NewFromFloat(50),
		EventType:         "login",
		DeviceFingerprint: "known-device",
	})
	require.NoError(t, err)

	for _, sig := range result.Signals {
		assert.NotEqual(t, "new_device", sig.Name, "known device should not trigger new_device signal")
	}
}

func TestUserEstablishedNormalBehavior_NoFlag(t *testing.T) {
	repo := newMockFraudRepo()
	detector, _ := newTestDetector(repo)
	userID := uuid.New()

	// User who regularly makes $500 transfers
	baseline := testBaseline(userID, 500, 50, 50)
	baseline.AvgDailyTransactions = 3.0
	baseline.AvgHourlyTransactions = 1.0
	repo.baselines[userID] = baseline

	repo.destinations[userID] = []fraud.KnownDestination{
		{Destination: "regular-dest", OccurrenceCount: 20},
	}
	repo.devices[userID] = []fraud.KnownDevice{
		{DeviceFingerprint: "regular-device"},
	}

	// Their normal $500 transfer should not be flagged
	result, err := detector.Evaluate(context.Background(), TransactionContext{
		UserID:            userID,
		Amount:            decimal.NewFromFloat(500),
		EventType:         "withdrawal",
		Destination:       "regular-dest",
		DeviceFingerprint: "regular-device",
		IsWithdrawal:      true,
	})
	require.NoError(t, err)
	assert.Nil(t, result.Flag, "established normal behavior should not trigger a flag")
}

func TestActionsRecordedForHighSeverity(t *testing.T) {
	repo := newMockFraudRepo()
	detector, now := newTestDetector(repo)
	userID := uuid.New()

	baseline := testBaseline(userID, 100, 20, 20)
	repo.baselines[userID] = baseline

	// Record password reset + new device + new destination
	resetTime := now.Add(-15 * time.Minute)
	repo.authEvents = append(repo.authEvents, fraud.AuthEvent{
		ID:        uuid.New(),
		UserID:    &userID,
		EventType: "password_reset",
		CreatedAt: resetTime,
	})

	result, err := detector.Evaluate(context.Background(), TransactionContext{
		UserID:            userID,
		Amount:            decimal.NewFromFloat(400),
		EventType:         "withdrawal",
		Destination:       "completely-new-dest",
		DeviceFingerprint: "unrecognized-fingerprint",
		IsWithdrawal:      true,
	})
	require.NoError(t, err)

	if result.Severity == fraud.SeverityHigh || result.Severity == fraud.SeverityCritical {
		// Actions should have been recorded
		actionCount := 0
		for _, a := range repo.actions {
			if a.FlagID == result.Flag.ID {
				actionCount++
			}
		}
		assert.True(t, actionCount > 0, "high severity should record protective actions")
	}
}

func TestFraudMetricRecordedOnFlag(t *testing.T) {
	repo := newMockFraudRepo()
	detector, _ := newTestDetector(repo)
	userID := uuid.New()

	baseline := testBaseline(userID, 100, 20, 20)
	repo.baselines[userID] = baseline

	// Trigger a flag with new destination + new device
	result, err := detector.Evaluate(context.Background(), TransactionContext{
		UserID:            userID,
		Amount:            decimal.NewFromFloat(300),
		EventType:         "withdrawal",
		Destination:       "new-dest",
		DeviceFingerprint: "new-device",
		IsWithdrawal:      true,
	})
	require.NoError(t, err)

	if result.Flag != nil {
		found := false
		for _, m := range repo.metrics {
			if m.MetricName == "fraud_flag_created" {
				found = true
				break
			}
		}
		assert.True(t, found, "fraud_flag_created metric should be recorded when a flag is created")
	}
}

func TestNewUserBelowThreshold_NoAmountDeviationSignal(t *testing.T) {
	repo := newMockFraudRepo()
	detector, _ := newTestDetector(repo)
	userID := uuid.New()

	// No baseline at all
	result, err := detector.Evaluate(context.Background(), TransactionContext{
		UserID:            userID,
		Amount:            decimal.NewFromFloat(500),
		EventType:         "withdrawal",
		Destination:       "some-dest",
		DeviceFingerprint: "device-1",
		IsWithdrawal:      true,
	})
	require.NoError(t, err)

	// Should not have amount_deviation signal since there's no baseline
	for _, sig := range result.Signals {
		assert.NotEqual(t, "amount_deviation", sig.Name, "no amount_deviation signal without baseline")
	}
}
