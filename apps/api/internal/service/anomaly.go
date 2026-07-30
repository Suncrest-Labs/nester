package service

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AnomalyDetector is a narrow hook point for the companion fraud/anomaly-
// detection engine (separate issue). It is intentionally minimal — this
// package does not implement fraud detection itself, only the call sites a
// real detector would need (login success, refresh-token reuse).
type AnomalyDetector interface {
	OnLoginSuccess(ctx context.Context, evt LoginEvent)
	OnRefreshReuseDetected(ctx context.Context, evt ReuseEvent)
}

type LoginEvent struct {
	UserID            uuid.UUID
	SessionID         uuid.UUID
	WalletAddress     string
	IPAddress         string
	UserAgent         string
	DeviceFingerprint string
	At                time.Time
}

type ReuseEvent struct {
	SessionID uuid.UUID
	UserID    uuid.UUID
	Reason    string
	IPAddress string
	UserAgent string
	At        time.Time
}

// NoopAnomalyDetector is the default implementation until the fraud/anomaly
// engine (companion issue) is built.
type NoopAnomalyDetector struct{}

func (NoopAnomalyDetector) OnLoginSuccess(context.Context, LoginEvent)         {}
func (NoopAnomalyDetector) OnRefreshReuseDetected(context.Context, ReuseEvent) {}
