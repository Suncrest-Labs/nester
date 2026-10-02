package config

import (
	"testing"
	"time"
)

// TestGoalNotificationDigestDefaults covers nester#1340: the digest flush
// loop's interval was hardcoded in main.go with no env var to tune it. It
// must now default to the same hour it was hardcoded to.
func TestGoalNotificationDigestDefaults(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if got := cfg.GoalNotificationDigest().Enabled(); !got {
		t.Errorf("GoalNotificationDigest().Enabled() = %v, want true", got)
	}
	if got := cfg.GoalNotificationDigest().Interval(); got != time.Hour {
		t.Errorf("GoalNotificationDigest().Interval() = %v, want 1h", got)
	}
}

func TestGoalNotificationDigestOverrides(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("GOAL_NOTIFICATION_DIGEST_ENABLED", "false")
	t.Setenv("GOAL_NOTIFICATION_DIGEST_INTERVAL", "15m")
	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if got := cfg.GoalNotificationDigest().Enabled(); got {
		t.Errorf("GoalNotificationDigest().Enabled() = %v, want false", got)
	}
	if got := cfg.GoalNotificationDigest().Interval(); got != 15*time.Minute {
		t.Errorf("GoalNotificationDigest().Interval() = %v, want 15m", got)
	}
}
