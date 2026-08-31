package config

import (
	"strings"
	"testing"
)

// NESTER_SERVICE_API_KEY authenticates service-to-service callers and is
// shared between them, so a weak value is a weak value for every caller at
// once. Startup must refuse it rather than serve with it (nester#1149).
func TestLoadRejectsWeakServiceAPIKey(t *testing.T) {
	// Written as readable words rather than random-looking hex, matching the
	// convention in redact_test.go: a high-entropy literal in a _test.go file
	// still looks like a leaked credential to a secret scanner, and the
	// honest fix is a value that is not key-shaped. These only need to be
	// long enough to clear the 32-character minimum and varied enough to
	// clear the entropy check.
	const strongJWTSecret = "SENTINEL-jwt-secret-not-a-real-secret"

	tests := []struct {
		name       string
		serviceKey string
		wantErr    string
	}{
		{
			name:       "too short",
			serviceKey: "short-service-key",
			wantErr:    "at least 32 characters",
		},
		{
			// Long enough to pass the length check, too few distinct bytes
			// to be a real secret.
			name:       "low entropy",
			serviceKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			wantErr:    "entropy",
		},
		{
			// Reuse would let any holder of the service key mint arbitrary
			// user JWTs, making every other control on it pointless.
			name:       "reuses the JWT secret",
			serviceKey: strongJWTSecret,
			wantErr:    "must not reuse AUTH_JWT_SECRET",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseEnv(t)
			requiredEnv(t)
			t.Setenv("APP_ENV", "development")
			t.Setenv("AUTH_JWT_SECRET", strongJWTSecret)
			t.Setenv("NESTER_SERVICE_API_KEY", tt.serviceKey)

			chdir(t, t.TempDir())

			_, err := Load()
			if err == nil {
				t.Fatalf("expected Load() to fail for a %s service key", tt.name)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want it to mention %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// A strong key must still boot, and an absent key must still boot: an empty
// value disables service auth entirely, which is the safe configuration
// rather than a weak one.
func TestLoadAcceptsStrongOrAbsentServiceAPIKey(t *testing.T) {
	tests := []struct {
		name       string
		serviceKey string
	}{
		{name: "strong key", serviceKey: "SENTINEL-service-key-not-a-real-secret"},
		{name: "absent key disables service auth", serviceKey: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseEnv(t)
			requiredEnv(t)
			t.Setenv("APP_ENV", "development")
			t.Setenv("AUTH_JWT_SECRET", "SENTINEL-jwt-secret-not-a-real-secret")
			t.Setenv("NESTER_SERVICE_API_KEY", tt.serviceKey)

			chdir(t, t.TempDir())

			if _, err := Load(); err != nil {
				t.Fatalf("Load() error = %v", err)
			}
		})
	}
}
