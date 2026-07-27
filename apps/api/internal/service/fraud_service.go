package service

import (
	"context"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/fraud"
)

// FraudDetector computes deterministic fraud signals and manages flags.
type FraudDetector struct {
	repo fraud.Repository
	now  func() time.Time
}

// NewFraudDetector constructs a FraudDetector with the given repository.
func NewFraudDetector(repo fraud.Repository) *FraudDetector {
	return &FraudDetector{
		repo: repo,
		now:  time.Now,
	}
}

// WithNow overrides the clock for testing.
func (d *FraudDetector) WithNow(fn func() time.Time) {
	d.now = fn
}

// ---- Threshold constants (deterministic, documented) ----

const (
	// A transaction more than this many standard deviations above the mean
	// is considered anomalous for amount deviation.
	amountDeviationThreshold = 3.0

	// If a user has fewer than this many transactions, the baseline is
	// considered too thin to compute stddev — we skip amount-deviation
	// and rely on absolute thresholds instead.
	minBaselineTransactions = 5

	// Absolute max transaction amount for new users (below minBaseline).
	absoluteMaxAmount float64 = 50000

	// A withdrawal to a destination not in the user's known list adds
	// this base score.
	newDestinationBaseScore = 0.3

	// If the destination was only seen once before, it still counts as
	// somewhat new.
	occasionalDestinationThreshold = 3

	// Velocity: if this many auth failures happen within the window,
	// it triggers an auth-failure-burst signal.
	authFailureBurstCount = 5
	authFailureBurstWindow = 15 * time.Minute

	// Device: a new device after a credential change (password reset
	// within this window) is high-risk.
	credentialChangeWindow = 1 * time.Hour

	// Impossible travel: minimum distance (km) and maximum time (hours)
	// that would require faster-than-human travel.
	impossibleTravelMinDistanceKm = 500
	impossibleTravelMaxHours     = 1

	// Velocity: a spike is detected when N times the rolling average
	// transactions per hour are seen in the current hour.
	velocitySpikeMultiplier = 5.0

	// Post-credential-change window for flagging any sensitive action.
	postCredentialChangeWindow = 2 * time.Hour

	// Score thresholds for severity bands.
	scoreLowThreshold      = 0.2
	scoreMediumThreshold   = 0.4
	scoreHighThreshold     = 0.7
	scoreCriticalThreshold = 0.9

	// Hold duration for high-severity flags.
	holdDuration = 30 * time.Minute
)

// TransactionContext carries the contextual information needed to score
// a transaction or account event.
type TransactionContext struct {
	UserID               uuid.UUID
	TransactionID        *uuid.UUID
	Amount               decimal.Decimal
	EventType            string
	Destination          string
	DeviceFingerprint    string
	IPAddress            string
	LocationLat          *float64
	LocationLon          *float64
	LocationCity         *string
	LocationCountry      *string
	IsWithdrawal         bool
	IsCredentialChange   bool
	IsAuthFailure        bool
}

// EvaluateResult is the output of fraud scoring.
type EvaluateResult struct {
	Flag     *fraud.FraudFlag
	Signals  []fraud.Signal
	Score    float64
	Severity fraud.Severity
	Actions  []fraud.ActionType
}

// Evaluate scores a transaction/event and creates a flag if anomalous.
func (d *FraudDetector) Evaluate(ctx context.Context, tc TransactionContext) (*EvaluateResult, error) {
	signals := []fraud.Signal{}

	baseline, _ := d.repo.GetBaseline(ctx, tc.UserID)
	destinations, _ := d.repo.GetKnownDestinations(ctx, tc.UserID)
	devices, _ := d.repo.GetKnownDevices(ctx, tc.UserID)

	// Record the auth event
	authEvent := fraud.AuthEvent{
		ID:                uuid.New(),
		UserID:            &tc.UserID,
		EventType:         tc.EventType,
		IPAddress:         strPtr(tc.IPAddress),
		DeviceFingerprint: strPtr(tc.DeviceFingerprint),
		LocationLat:       tc.LocationLat,
		LocationLon:       tc.LocationLon,
		LocationCity:      tc.LocationCity,
		LocationCountry:   tc.LocationCountry,
		Metadata:          map[string]any{},
		CreatedAt:         d.now(),
	}
	_ = d.repo.RecordAuthEvent(ctx, authEvent)

	// --- Signal 1: Amount deviation ---
	if tc.Amount.GreaterThan(decimal.Zero) {
		sig := d.checkAmountDeviation(baseline, tc.Amount)
		if sig != nil {
			signals = append(signals, *sig)
		}
	}

	// --- Signal 2: New destination ---
	if tc.IsWithdrawal && tc.Destination != "" {
		sig := d.checkNewDestination(ctx, tc.UserID, destinations, tc.Destination)
		if sig != nil {
			signals = append(signals, *sig)
		}
	}

	// --- Signal 3: New device ---
	if tc.DeviceFingerprint != "" {
		sig := d.checkNewDevice(ctx, tc.UserID, devices, tc.DeviceFingerprint)
		if sig != nil {
			signals = append(signals, *sig)
		}
	}

	// --- Signal 4: Auth failure burst ---
	if tc.IsAuthFailure || !tc.IsAuthFailure {
		sig := d.checkAuthFailureBurst(ctx, tc.UserID)
		if sig != nil {
			signals = append(signals, *sig)
		}
	}

	// --- Signal 5: Impossible travel ---
	sig := d.checkImpossibleTravel(ctx, tc.UserID, tc.LocationLat, tc.LocationLon, d.now())
	if sig != nil {
		signals = append(signals, *sig)
	}

	// --- Signal 6: Post-credential-change activity ---
	if tc.IsCredentialChange || (!tc.IsCredentialChange && tc.IsWithdrawal) {
		sig := d.checkPostCredentialChange(ctx, tc.UserID, tc.IsCredentialChange)
		if sig != nil {
			signals = append(signals, *sig)
		}
	}

	// --- Signal 7: Velocity spike ---
	sig = d.checkVelocitySpike(ctx, tc.UserID)
	if sig != nil {
		signals = append(signals, *sig)
	}

	// Compute aggregate score
	score := aggregateScore(signals)
	severity := scoreToSeverity(score)
	actions := severityToActions(severity)

	if severity == fraud.SeverityLow && len(signals) == 0 {
		return &EvaluateResult{
			Signals:  signals,
			Score:    score,
			Severity: severity,
			Actions:  []fraud.ActionType{fraud.ActionLog},
		}, nil
	}

	flag := &fraud.FraudFlag{
		ID:            uuid.New(),
		UserID:        tc.UserID,
		TransactionID: tc.TransactionID,
		EventType:     tc.EventType,
		Severity:      severity,
		Status:        fraud.FlagStatusOpen,
		Signals:       signals,
		RiskScore:     score,
		CreatedAt:     d.now(),
		UpdatedAt:     d.now(),
	}

	createdFlag, err := d.repo.CreateFlag(ctx, *flag)
	if err != nil {
		return nil, err
	}

	// Execute protective actions
	for _, action := range actions {
		d.executeAction(ctx, createdFlag, action, tc)
	}

	// Record metric
	_ = d.repo.RecordMetric(ctx, fraud.FraudMetric{
		ID:          uuid.New(),
		MetricName:  "fraud_flag_created",
		MetricValue: score,
		Tags: map[string]any{
			"severity": string(severity),
			"event":    tc.EventType,
		},
		RecordedAt: d.now(),
	})

	return &EvaluateResult{
		Flag:     createdFlag,
		Signals:  signals,
		Score:    score,
		Severity: severity,
		Actions:  actions,
	}, nil
}

// Evaluate returns the current time; used by callers to set location on TransactionContext.

func (d *FraudDetector) checkAmountDeviation(baseline *fraud.UserBaseline, amount decimal.Decimal) *fraud.Signal {
	amountF := amount.InexactFloat64()

	if baseline == nil || baseline.TransactionCount < minBaselineTransactions {
		if amountF > absoluteMaxAmount {
			return &fraud.Signal{
				Name:      "amount_deviation",
				Score:     0.5,
				Threshold: absoluteMaxAmount,
				Message:   "Transaction amount exceeds absolute maximum for new user",
			}
		}
		return nil
	}

	mean := baseline.AvgTransactionAmount.InexactFloat64()
	stddev := baseline.StddevTransactionAmount.InexactFloat64()
	if stddev <= 0 {
		stddev = mean * 0.1
		if stddev <= 0 {
			return nil
		}
	}

	deviation := (amountF - mean) / stddev
	if deviation > amountDeviationThreshold {
		score := math.Min(1.0, deviation/(amountDeviationThreshold*2))
		return &fraud.Signal{
			Name:      "amount_deviation",
			Score:     score,
			Threshold: amountDeviationThreshold,
			Message:   "Transaction amount significantly deviates from user's historical norm",
		}
	}
	return nil
}

func (d *FraudDetector) checkNewDestination(ctx context.Context, userID uuid.UUID, destinations []fraud.KnownDestination, dest string) *fraud.Signal {
	if len(destinations) == 0 {
		return &fraud.Signal{
			Name:      "new_destination",
			Score:     0.4,
			Threshold: 0,
			Message:   "First withdrawal destination for this account",
		}
	}

	for _, kd := range destinations {
		if kd.Destination == dest {
			if kd.OccurrenceCount < occasionalDestinationThreshold {
				return &fraud.Signal{
					Name:      "occasional_destination",
					Score:     0.15,
					Threshold: float64(occasionalDestinationThreshold),
					Message:   "Destination has only been used a few times",
				}
			}
			return nil
		}
	}

	return &fraud.Signal{
		Name:      "new_destination",
		Score:     newDestinationBaseScore,
		Threshold: 0,
		Message:   "Withdrawal to previously unseen destination",
	}
}

func (d *FraudDetector) checkNewDevice(ctx context.Context, userID uuid.UUID, devices []fraud.KnownDevice, fingerprint string) *fraud.Signal {
	for _, kd := range devices {
		if kd.DeviceFingerprint == fingerprint {
			return nil
		}
	}
	return &fraud.Signal{
		Name:      "new_device",
		Score:     0.25,
		Threshold: 0,
		Message:   "Activity from unrecognized device",
	}
}

func (d *FraudDetector) checkAuthFailureBurst(ctx context.Context, userID uuid.UUID) *fraud.Signal {
	events, err := d.repo.RecentAuthEvents(ctx, userID, authFailureBurstWindow)
	if err != nil {
		return nil
	}

	failures := 0
	for _, e := range events {
		if e.EventType == "auth_failure" {
			failures++
		}
	}

	if failures >= authFailureBurstCount {
		score := math.Min(1.0, float64(failures)/float64(authFailureBurstCount*2))
		return &fraud.Signal{
			Name:      "auth_failure_burst",
			Score:     score,
			Threshold: float64(authFailureBurstCount),
			Message:   "Multiple authentication failures in a short window",
		}
	}
	return nil
}

func (d *FraudDetector) checkImpossibleTravel(ctx context.Context, userID uuid.UUID, lat, lon *float64, _ time.Time) *fraud.Signal {
	if lat == nil || lon == nil {
		return nil
	}

	events, err := d.repo.RecentAuthEvents(ctx, userID, impossibleTravelMaxHours*time.Hour)
	if err != nil || len(events) == 0 {
		return nil
	}

	for _, e := range events {
		if e.LocationLat != nil && e.LocationLon != nil {
			distance := haversineDistance(*e.LocationLat, *e.LocationLon, *lat, *lon)
			if distance > float64(impossibleTravelMinDistanceKm) {
				score := math.Min(1.0, distance/float64(impossibleTravelMinDistanceKm*2))
				return &fraud.Signal{
					Name:      "impossible_travel",
					Score:     score,
					Threshold: float64(impossibleTravelMinDistanceKm),
					Message:   "Activity from implausible location given recent activity",
				}
			}
		}
	}
	return nil
}

func (d *FraudDetector) checkPostCredentialChange(ctx context.Context, userID uuid.UUID, isCredentialChange bool) *fraud.Signal {
	if isCredentialChange {
		return nil
	}

	events, err := d.repo.RecentAuthEvents(ctx, userID, postCredentialChangeWindow)
	if err != nil {
		return nil
	}

	for _, e := range events {
		if e.EventType == "password_reset" || e.EventType == "credential_change" {
			score := 0.6
			return &fraud.Signal{
				Name:      "post_credential_change",
				Score:     score,
				Threshold: postCredentialChangeWindow.Seconds(),
				Message:   "Sensitive activity following credential change",
			}
		}
	}
	return nil
}

func (d *FraudDetector) checkVelocitySpike(ctx context.Context, userID uuid.UUID) *fraud.Signal {
	events, err := d.repo.RecentAuthEvents(ctx, userID, 1*time.Hour)
	if err != nil {
		return nil
	}

	baseline, err := d.repo.GetBaseline(ctx, userID)
	if err != nil || baseline == nil || baseline.AvgHourlyTransactions <= 0 {
		return nil
	}

	currentCount := float64(len(events))
	avg := baseline.AvgHourlyTransactions
	if currentCount > avg*velocitySpikeMultiplier && avg > 0 {
		score := math.Min(1.0, currentCount/(avg*velocitySpikeMultiplier*2))
		return &fraud.Signal{
			Name:      "velocity_spike",
			Score:     score,
			Threshold: avg * velocitySpikeMultiplier,
			Message:   "Transaction velocity significantly exceeds user's baseline",
		}
	}
	return nil
}

func (d *FraudDetector) executeAction(ctx context.Context, flag *fraud.FraudFlag, action fraud.ActionType, tc TransactionContext) {
	switch action {
	case fraud.ActionHold:
		expires := d.now().Add(holdDuration)
		_, _ = d.repo.CreateAction(ctx, fraud.FraudAction{
			ID:        uuid.New(),
			FlagID:    flag.ID,
			Action:    fraud.ActionHold,
			Reason:    "High-severity fraud flag triggered temporary hold",
			Metadata:  map[string]any{"duration_minutes": int(holdDuration.Minutes())},
			ExpiresAt: &expires,
			CreatedAt: d.now(),
		})
	case fraud.ActionStepUpAuth:
		_, _ = d.repo.CreateAction(ctx, fraud.FraudAction{
			ID:        uuid.New(),
			FlagID:    flag.ID,
			Action:    fraud.ActionStepUpAuth,
			Reason:    "Medium-severity fraud flag triggered step-up authentication",
			Metadata:  map[string]any{},
			CreatedAt: d.now(),
		})
	}
}

// ---- Helpers ----

func aggregateScore(signals []fraud.Signal) float64 {
	if len(signals) == 0 {
		return 0
	}
	total := 0.0
	for _, s := range signals {
		total += s.Score
	}
	// Normalize: use 1 - product of complements (independent probabilities)
	combined := 1.0
	for _, s := range signals {
		combined *= (1.0 - s.Score)
	}
	return 1.0 - combined
}

func scoreToSeverity(score float64) fraud.Severity {
	switch {
	case score >= scoreCriticalThreshold:
		return fraud.SeverityCritical
	case score >= scoreHighThreshold:
		return fraud.SeverityHigh
	case score >= scoreMediumThreshold:
		return fraud.SeverityMedium
	default:
		return fraud.SeverityLow
	}
}

func severityToActions(severity fraud.Severity) []fraud.ActionType {
	switch severity {
	case fraud.SeverityCritical:
		return []fraud.ActionType{fraud.ActionHold, fraud.ActionStepUpAuth}
	case fraud.SeverityHigh:
		return []fraud.ActionType{fraud.ActionHold, fraud.ActionStepUpAuth}
	case fraud.SeverityMedium:
		return []fraud.ActionType{fraud.ActionStepUpAuth}
	default:
		return []fraud.ActionType{fraud.ActionLog}
	}
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// haversineDistance computes the great-circle distance between two points
// on Earth using the Haversine formula. Returns distance in kilometers.
func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0
	dLat := toRadians(lat2 - lat1)
	dLon := toRadians(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRadians(lat1))*math.Cos(toRadians(lat2))*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusKm * c
}

func toRadians(deg float64) float64 {
	return deg * math.Pi / 180.0
}

// UpdateBaseline recalculates and persists the user's behavioral baseline
// from their transaction history. This is called periodically or after
// each transaction is confirmed.
func (d *FraudDetector) UpdateBaseline(ctx context.Context, userID uuid.UUID, amounts []decimal.Decimal, dailyCounts []float64, hourlyCounts []float64) error {
	if len(amounts) == 0 {
		return nil
	}

	total := decimal.Zero
	for _, a := range amounts {
		total = total.Add(a)
	}
	count := decimal.NewFromFloat(float64(len(amounts)))
	avg := total.Div(count)

	// Compute stddev using float64 for square root
	sumSquaredDiff := 0.0
	avgF := avg.InexactFloat64()
	for _, a := range amounts {
		diff := a.InexactFloat64() - avgF
		sumSquaredDiff += diff * diff
	}
	varianceF := sumSquaredDiff / float64(len(amounts))
	stddev := decimal.NewFromFloat(math.Sqrt(varianceF))

	maxAmt := amounts[0]
	for _, a := range amounts[1:] {
		if a.GreaterThan(maxAmt) {
			maxAmt = a
		}
	}

	avgDaily := 0.0
	if len(dailyCounts) > 0 {
		for _, c := range dailyCounts {
			avgDaily += c
		}
		avgDaily /= float64(len(dailyCounts))
	}

	avgHourly := 0.0
	if len(hourlyCounts) > 0 {
		for _, c := range hourlyCounts {
			avgHourly += c
		}
		avgHourly /= float64(len(hourlyCounts))
	}

	baseline := fraud.UserBaseline{
		ID:                      uuid.New(),
		UserID:                  userID,
		AvgTransactionAmount:    avg,
		StddevTransactionAmount: stddev,
		MaxTransactionAmount:    maxAmt,
		AvgDailyTransactions:    avgDaily,
		AvgHourlyTransactions:   avgHourly,
		TransactionCount:        len(amounts),
		LastUpdatedAt:           d.now(),
		CreatedAt:               d.now(),
	}

	_, err := d.repo.UpsertBaseline(ctx, baseline)
	return err
}

// SelfClearFlag allows a user to clear a medium-severity flag by confirming
// the action was legitimate.
func (d *FraudDetector) SelfClearFlag(ctx context.Context, flagID uuid.UUID, userID uuid.UUID) error {
	flag, err := d.repo.GetFlag(ctx, flagID)
	if err != nil {
		return err
	}
	if flag.UserID != userID {
		return fraud.ErrFlagNotFound
	}
	if flag.Severity != fraud.SeverityMedium {
		return fraud.ErrInvalidFlag
	}
	return d.repo.ClearFlagByUser(ctx, flagID)
}

// IsBlocked checks if a user currently has an active (non-expired) hold.
func (d *FraudDetector) IsBlocked(ctx context.Context, userID uuid.UUID) (bool, error) {
	holds, err := d.repo.GetActiveHolds(ctx, userID)
	if err != nil {
		return false, err
	}
	now := d.now()
	for _, h := range holds {
		if h.ExpiresAt != nil && h.ExpiresAt.After(now) {
			return true, nil
		}
	}
	return false, nil
}

// GetFalsePositiveRate returns the ratio of auto-cleared or user-cleared flags
// to total flags within the given window.
func (d *FraudDetector) GetFalsePositiveRate(ctx context.Context, since time.Duration) (float64, error) {
	totalRate, err := d.repo.GetMetricRate(ctx, "fraud_flag_created", since)
	if err != nil {
		return 0, err
	}
	clearRate, err := d.repo.GetMetricRate(ctx, "fraud_flag_cleared", since)
	if err != nil {
		return 0, err
	}
	if totalRate == 0 {
		return 0, nil
	}
	return clearRate / totalRate, nil
}
