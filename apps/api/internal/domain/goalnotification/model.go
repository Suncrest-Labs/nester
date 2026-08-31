// Package goalnotification implements per-savings-goal notification settings
// (mute + digest frequency), so a user with many goals can quiet a specific
// goal's milestone/deadline notifications or batch them into a periodic
// digest instead of being notified immediately on every crossing.
package goalnotification

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidPreference is returned for a malformed preference update.
var ErrInvalidPreference = errors.New("invalid goal notification preference")

// ErrNotFound is returned when the referenced goal has no owner match.
var ErrNotFound = errors.New("savings goal not found")

// Frequency controls how often non-muted notifications are delivered for a goal.
type Frequency string

const (
	// FrequencyImmediate delivers each notification as soon as it fires (default).
	FrequencyImmediate Frequency = "immediate"
	// FrequencyDaily batches notifications into a once-a-day digest.
	FrequencyDaily Frequency = "daily"
	// FrequencyWeekly batches notifications into a once-a-week digest.
	FrequencyWeekly Frequency = "weekly"
)

// ParseFrequency validates a digest_frequency value from an API request.
func ParseFrequency(value string) (Frequency, error) {
	switch f := Frequency(strings.ToLower(strings.TrimSpace(value))); f {
	case FrequencyImmediate, FrequencyDaily, FrequencyWeekly:
		return f, nil
	default:
		return "", fmt.Errorf("%w: digest_frequency must be one of immediate, daily, weekly (got %q)", ErrInvalidPreference, value)
	}
}

// Preference is a user's per-goal notification settings: whether
// notifications for the goal are muted entirely, and how often non-muted
// updates are batched into a digest instead of sent immediately.
type Preference struct {
	GoalID           uuid.UUID  `json:"goal_id"`
	UserID           uuid.UUID  `json:"-"`
	Muted            bool       `json:"muted"`
	DigestFrequency  Frequency  `json:"digest_frequency"`
	LastDigestSentAt *time.Time `json:"last_digest_sent_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// Default returns the baseline preference (unmuted, immediate delivery) used
// when a goal has no stored preference row yet.
func Default(goalID, userID uuid.UUID) Preference {
	return Preference{GoalID: goalID, UserID: userID, Muted: false, DigestFrequency: FrequencyImmediate}
}

// DigestItem is a single notification queued for later batched delivery
// because its goal's preference is not "immediate".
type DigestItem struct {
	ID        uuid.UUID
	GoalID    uuid.UUID
	UserID    uuid.UUID
	Title     string
	Body      string
	Payload   map[string]any
	CreatedAt time.Time
}

// Repository persists per-goal notification preferences and queued digest items.
type Repository interface {
	// Get returns the stored preference for goalID, or (nil, nil) if none exists.
	Get(ctx context.Context, goalID uuid.UUID) (*Preference, error)
	// Upsert creates or updates the preference row for pref.GoalID.
	Upsert(ctx context.Context, pref Preference) (Preference, error)
	// EnqueueDigestItem queues a notification for later batched delivery.
	EnqueueDigestItem(ctx context.Context, item DigestItem) error
	// ListDue returns preferences with a non-immediate frequency that have at
	// least one queued digest item and are due for a flush.
	ListDue(ctx context.Context, now time.Time) ([]Preference, error)
	// ListQueuedItems returns the pending digest items for a goal, oldest first.
	ListQueuedItems(ctx context.Context, goalID uuid.UUID) ([]DigestItem, error)
	// ClearQueuedItems removes the given queued items after a digest is sent.
	ClearQueuedItems(ctx context.Context, goalID uuid.UUID, itemIDs []uuid.UUID) error
	// MarkDigestSent records when a digest was last flushed for the goal.
	MarkDigestSent(ctx context.Context, goalID uuid.UUID, sentAt time.Time) error
}
