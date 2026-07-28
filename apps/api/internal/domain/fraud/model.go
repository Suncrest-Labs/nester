package fraud

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Severity represents the risk level of a fraud flag.
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// FlagStatus represents the lifecycle state of a fraud flag.
type FlagStatus string

const (
	FlagStatusOpen        FlagStatus = "open"
	FlagStatusCleared     FlagStatus = "cleared"
	FlagStatusConfirmed   FlagStatus = "confirmed"
	FlagStatusAutoCleared FlagStatus = "auto_cleared"
)

// ActionType represents the protective action taken in response to a flag.
type ActionType string

const (
	ActionLog          ActionType = "log"
	ActionStepUpAuth   ActionType = "step_up_auth"
	ActionHold         ActionType = "hold"
	ActionBlock        ActionType = "block"
)

var (
	ErrFlagNotFound  = errors.New("fraud flag not found")
	ErrInvalidFlag   = errors.New("invalid fraud flag")
	ErrBaselineStale = errors.New("baseline data is stale or insufficient")
)

// UserBaseline stores per-user behavioral baselines for fraud detection.
type UserBaseline struct {
	ID                     uuid.UUID       `json:"id"`
	UserID                 uuid.UUID       `json:"user_id"`
	AvgTransactionAmount   decimal.Decimal `json:"avg_transaction_amount"`
	StddevTransactionAmount decimal.Decimal `json:"stddev_transaction_amount"`
	MaxTransactionAmount   decimal.Decimal `json:"max_transaction_amount"`
	AvgDailyTransactions   float64         `json:"avg_daily_transactions"`
	AvgHourlyTransactions  float64         `json:"avg_hourly_transactions"`
	KnownDestinationCount  int             `json:"known_destination_count"`
	KnownDeviceCount       int             `json:"known_device_count"`
	TypicalHourStart       int             `json:"typical_hour_start"`
	TypicalHourEnd         int             `json:"typical_hour_end"`
	TransactionCount       int             `json:"transaction_count"`
	LastUpdatedAt          time.Time       `json:"last_updated_at"`
	CreatedAt              time.Time       `json:"created_at"`
}

// KnownDestination tracks withdrawal destinations seen for a user.
type KnownDestination struct {
	ID              uuid.UUID `json:"id"`
	UserID          uuid.UUID `json:"user_id"`
	Destination     string    `json:"destination"`
	Label           *string   `json:"label,omitempty"`
	FirstSeenAt     time.Time `json:"first_seen_at"`
	LastSeenAt      time.Time `json:"last_seen_at"`
	OccurrenceCount int       `json:"occurrence_count"`
}

// KnownDevice tracks device fingerprints seen for a user.
type KnownDevice struct {
	ID                uuid.UUID `json:"id"`
	UserID            uuid.UUID `json:"user_id"`
	DeviceFingerprint string    `json:"device_fingerprint"`
	Label             *string   `json:"label,omitempty"`
	FirstSeenAt       time.Time `json:"first_seen_at"`
	LastSeenAt        time.Time `json:"last_seen_at"`
}

// FraudFlag represents a detected anomalous event.
type FraudFlag struct {
	ID              uuid.UUID       `json:"id"`
	UserID          uuid.UUID       `json:"user_id"`
	TransactionID   *uuid.UUID      `json:"transaction_id,omitempty"`
	EventType       string          `json:"event_type"`
	Severity        Severity        `json:"severity"`
	Status          FlagStatus      `json:"status"`
	Signals         []Signal        `json:"signals"`
	RiskScore       float64         `json:"risk_score"`
	Explanation     *string         `json:"explanation,omitempty"`
	UserNotified    bool            `json:"user_notified"`
	ReviewedBy      *uuid.UUID      `json:"reviewed_by,omitempty"`
	ReviewedAt      *time.Time      `json:"reviewed_at,omitempty"`
	ClearedByUser   bool            `json:"cleared_by_user"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// Signal is a single deterministic detection signal contributing to a risk score.
type Signal struct {
	Name       string  `json:"name"`
	Score      float64 `json:"score"`
	Threshold  float64 `json:"threshold"`
	Message    string  `json:"message"`
}

// FraudAction records a protective action taken in response to a flag.
type FraudAction struct {
	ID         uuid.UUID  `json:"id"`
	FlagID     uuid.UUID  `json:"flag_id"`
	Action     ActionType `json:"action"`
	Reason     string     `json:"reason"`
	Metadata   map[string]any `json:"metadata"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// AuthEvent records an authentication-related event for anomaly detection.
type AuthEvent struct {
	ID                uuid.UUID          `json:"id"`
	UserID            *uuid.UUID         `json:"user_id,omitempty"`
	EventType         string             `json:"event_type"`
	IPAddress         *string            `json:"ip_address,omitempty"`
	DeviceFingerprint *string            `json:"device_fingerprint,omitempty"`
	LocationLat       *float64           `json:"location_lat,omitempty"`
	LocationLon       *float64           `json:"location_lon,omitempty"`
	LocationCity      *string            `json:"location_city,omitempty"`
	LocationCountry   *string            `json:"location_country,omitempty"`
	Metadata          map[string]any     `json:"metadata"`
	CreatedAt         time.Time          `json:"created_at"`
}

// FraudMetric records a monitored metric for false-positive tracking.
type FraudMetric struct {
	ID         uuid.UUID       `json:"id"`
	MetricName string          `json:"metric_name"`
	MetricValue float64        `json:"metric_value"`
	Tags       map[string]any  `json:"tags"`
	RecordedAt time.Time       `json:"recorded_at"`
}

// Repository defines persistence operations for fraud detection.
type Repository interface {
	// Baselines
	GetBaseline(ctx context.Context, userID uuid.UUID) (*UserBaseline, error)
	UpsertBaseline(ctx context.Context, baseline UserBaseline) (*UserBaseline, error)

	// Known destinations
	GetKnownDestinations(ctx context.Context, userID uuid.UUID) ([]KnownDestination, error)
	UpsertKnownDestination(ctx context.Context, userID uuid.UUID, destination string) (*KnownDestination, error)
	IsKnownDestination(ctx context.Context, userID uuid.UUID, destination string) (bool, error)

	// Known devices
	GetKnownDevices(ctx context.Context, userID uuid.UUID) ([]KnownDevice, error)
	UpsertKnownDevice(ctx context.Context, userID uuid.UUID, fingerprint string) (*KnownDevice, error)
	IsKnownDevice(ctx context.Context, userID uuid.UUID, fingerprint string) (bool, error)

	// Fraud flags
	CreateFlag(ctx context.Context, flag FraudFlag) (*FraudFlag, error)
	GetFlag(ctx context.Context, id uuid.UUID) (*FraudFlag, error)
	ListOpenFlags(ctx context.Context, userID uuid.UUID) ([]FraudFlag, error)
	UpdateFlagStatus(ctx context.Context, id uuid.UUID, status FlagStatus) error
	ClearFlagByUser(ctx context.Context, id uuid.UUID) error

	// Fraud actions
	CreateAction(ctx context.Context, action FraudAction) (*FraudAction, error)
	GetActiveHolds(ctx context.Context, userID uuid.UUID) ([]FraudAction, error)
	ExpireHold(ctx context.Context, actionID uuid.UUID) error

	// Auth events
	RecordAuthEvent(ctx context.Context, event AuthEvent) error
	RecentAuthEvents(ctx context.Context, userID uuid.UUID, since time.Duration) ([]AuthEvent, error)

	// Metrics
	RecordMetric(ctx context.Context, metric FraudMetric) error
	GetMetricRate(ctx context.Context, metricName string, since time.Duration) (float64, error)
}
