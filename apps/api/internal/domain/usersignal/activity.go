package usersignal

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type EventType string

const (
	EventTypeLogin   EventType = "login"
	EventTypeDeposit EventType = "deposit"
)

type ActivityEvent struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	EventType  EventType
	OccurredAt time.Time
}

type ActivityEventRepository interface {
	RecordEvent(ctx context.Context, userID uuid.UUID, eventType EventType, occurredAt time.Time) error
	ListRecentEvents(ctx context.Context, userID uuid.UUID, since time.Time) ([]ActivityEvent, error)
}
