package service

import (
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

// DispatcherNudgeNotifier adapts notifications.Dispatcher for the nudge
// engine's own dispatch call (distinct EventType/routing from the ad hoc
// notifiers it replaces).
type DispatcherNudgeNotifier struct {
	Dispatcher *notifications.Dispatcher
}
