package config

import (
	"os"
	"path/filepath"
	"sync"
	"strings"
	"testing"
	"time"
)

// baseEnv clears all known config keys so each test starts from a clean slate,
// preventing ambient environment variables in CI from affecting results.
func baseEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"APP_ENV",
		"SERVER_HOST", "SERVER_PORT",
		"SERVER_READ_TIMEOUT", "SERVER_WRITE_TIMEOUT", "SERVER_SHUTDOWN_TIMEOUT",
		"DATABASE_DSN", "DATABASE_POOL_SIZE", "DATABASE_CONNECTION_TIMEOUT",
		"STELLAR_NETWORK_PASSPHRASE", "STELLAR_RPC_URL", "STELLAR_HORIZON_URL",
		"AUTH_JWT_SECRET", "AUTH_TOKEN_EXPIRY", "AUTH_CHALLENGE_EXPIRY",
		"RATELIMIT_GLOBAL_LIMIT", "RATELIMIT_GLOBAL_WINDOW", "RATELIMIT_WRITE_LIMIT", "RATELIMIT_WRITE_WINDOW",
		"LOG_LEVEL", "LOG_FORMAT",
	} {
		t.Setenv(key, "")
	}
}

// requiredEnv sets the minimum required fields so a test can focus on a specific key.
func requiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_DSN", "postgres://postgres:postgres@localhost:5432/nester?sslmode=disable")
	t.Setenv("STELLAR_NETWORK_PASSPHRASE", "Test Network")
	t.Setenv("STELLAR_RPC_URL", "https://rpc.example.com")
	t.Setenv("STELLAR_HORIZON_URL", "https://horizon.example.com")
	t.Setenv("AUTH_JWT_SECRET", "this-is-a-very-secret-jwt-key-that-is-at-least-thirty-two-bytes")
}

func TestLoadFromDotEnv(t *testing.T) {
	baseEnv(t)
	t.Setenv("DATABASE_DSN", "")
	t.Setenv("STELLAR_NETWORK_PASSPHRASE", "")
	t.Setenv("STELLAR_RPC_URL", "")
	t.Setenv("STELLAR_HORIZON_URL", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("LOG_FORMAT", "")

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), strings.Join([]string{
		"APP_ENV=staging",
		"DATABASE_DSN=postgres://postgres:postgres@localhost:5432/nester?sslmode=disable",
		"STELLAR_NETWORK_PASSPHRASE=Test Network",
		"STELLAR_RPC_URL=https://rpc.example.com",
		"STELLAR_HORIZON_URL=https://horizon.example.com",
		"AUTH_JWT_SECRET=this-is-a-very-secret-jwt-key-that-is-at-least-thirty-two-bytes",
	}, "\n"))

	chdir(t, dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Environment() != "staging" {
		t.Fatalf("expected environment staging, got %q", cfg.Environment())
	}
	if cfg.Server().Port() != 8080 {
		t.Fatalf("expected default port 8080, got %d", cfg.Server().Port())
	}
	if cfg.Log().Format() != "json" {
		t.Fatalf("expected staging to default to json format, got %q", cfg.Log().Format())
	}
	if cfg.Database().PoolSize() != 25 {
		t.Fatalf("expected default pool size 25, got %d", cfg.Database().PoolSize())
	}
	if cfg.Server().GracefulShutdown() != 20*time.Second {
		t.Fatalf("expected default shutdown timeout 20s, got %s", cfg.Server().GracefulShutdown())
	}
}

func TestLoadMissingRequiredFields(t *testing.T) {
	baseEnv(t)
	t.Setenv("DATABASE_DSN", "")
	t.Setenv("STELLAR_NETWORK_PASSPHRASE", "")
	t.Setenv("STELLAR_RPC_URL", "")
	t.Setenv("STELLAR_HORIZON_URL", "")

	chdir(t, t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load() to fail")
	}

	message := err.Error()
	for _, expected := range []string{
		"DATABASE_DSN is required",
		"STELLAR_NETWORK_PASSPHRASE is required",
		"STELLAR_RPC_URL is required",
		"STELLAR_HORIZON_URL is required",
		"AUTH_JWT_SECRET is required",
	} {
		if !strings.Contains(message, expected) {
			t.Fatalf("expected error to contain %q, got %q", expected, message)
		}
	}
}

func TestLoadTypeCoercionErrors(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("SERVER_PORT", "not-a-number")
	t.Setenv("DATABASE_CONNECTION_TIMEOUT", "forever")

	chdir(t, t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load() to fail")
	}

	message := err.Error()
	if !strings.Contains(message, `SERVER_PORT must be an integer, got "not-a-number"`) {
		t.Fatalf("expected integer coercion error, got %q", message)
	}
	if !strings.Contains(message, `DATABASE_CONNECTION_TIMEOUT must be a valid duration, got "forever"`) {
		t.Fatalf("expected duration coercion error, got %q", message)
	}
}

// TestLoadFromEnvVars verifies that config loads correctly when all values come
// from environment variables and no .env file is present.
func TestLoadFromEnvVars(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("SERVER_PORT", "9090")
	t.Setenv("LOG_LEVEL", "debug")

	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Environment() != "development" {
		t.Fatalf("expected development, got %q", cfg.Environment())
	}
	if cfg.Server().Port() != 9090 {
		t.Fatalf("expected port 9090, got %d", cfg.Server().Port())
	}
	if cfg.Log().Level() != "debug" {
		t.Fatalf("expected log level debug, got %q", cfg.Log().Level())
	}
	wantDSN := "postgres://postgres:postgres@localhost:5432/nester?sslmode=disable"
	if cfg.Database().DSN() != wantDSN {
		t.Fatalf("unexpected DSN: %q", cfg.Database().DSN())
	}
}

// TestLoadEnvVarsTakePrecedenceOverDotEnv verifies that environment variables
// override values defined in .env files.
func TestLoadEnvVarsTakePrecedenceOverDotEnv(t *testing.T) {
	baseEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("SERVER_PORT", "9000")
	t.Setenv("DATABASE_DSN", "postgres://envvar:secret@localhost:5432/nester?sslmode=disable")
	t.Setenv("STELLAR_NETWORK_PASSPHRASE", "From EnvVar")
	t.Setenv("STELLAR_RPC_URL", "https://envvar-rpc.example.com")
	t.Setenv("STELLAR_HORIZON_URL", "https://envvar-horizon.example.com")

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), strings.Join([]string{
		"APP_ENV=development",
		"SERVER_PORT=8080",
		"DATABASE_DSN=postgres://dotenv:secret@localhost:5432/nester?sslmode=disable",
		"STELLAR_NETWORK_PASSPHRASE=From DotEnv",
		"STELLAR_RPC_URL=https://dotenv-rpc.example.com",
		"STELLAR_HORIZON_URL=https://dotenv-horizon.example.com",
		"AUTH_JWT_SECRET=this-is-a-very-secret-jwt-key-that-is-at-least-thirty-two-bytes",
	}, "\n"))
	chdir(t, dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Environment() != "production" {
		t.Fatalf("expected production from env var, got %q", cfg.Environment())
	}
	if cfg.Server().Port() != 9000 {
		t.Fatalf("expected port 9000 from env var, got %d", cfg.Server().Port())
	}
	if cfg.Stellar().NetworkPassphrase() != "From EnvVar" {
		t.Fatalf("expected stellar passphrase from env var, got %q", cfg.Stellar().NetworkPassphrase())
	}
}

// TestLoadConcurrentCalls verifies repeated concurrent Load calls remain stable
// and return consistent values.
func TestLoadConcurrentCalls(t *testing.T) {
	baseEnv(t)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), strings.Join([]string{
		"APP_ENV=staging",
		"SERVER_PORT=8088",
		"DATABASE_DSN=postgres://postgres:postgres@localhost:5432/nester?sslmode=disable",
		"STELLAR_NETWORK_PASSPHRASE=Concurrent Network",
		"STELLAR_RPC_URL=https://rpc.example.com",
		"STELLAR_HORIZON_URL=https://horizon.example.com",
		"AUTH_JWT_SECRET=this-is-a-very-secret-jwt-key-that-is-at-least-thirty-two-bytes",
	}, "\n"))
	chdir(t, dir)

	const goroutines = 32

	errCh := make(chan error, goroutines)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			cfg, err := Load()
			if err != nil {
				errCh <- err
				return
			}

			if cfg.Environment() != "staging" {
				errCh <- &testErr{message: "unexpected environment"}
				return
			}
			if cfg.Server().Port() != 8088 {
				errCh <- &testErr{message: "unexpected server port"}
				return
			}
			if cfg.Stellar().NetworkPassphrase() != "Concurrent Network" {
				errCh <- &testErr{message: "unexpected stellar passphrase"}
				return
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent Load() failed: %v", err)
	}
}

// TestLoadProcessEnvOverridesDotEnvAndFallsBack verifies that process env
// values win when set, while unset keys continue to fall back to .env values.
func TestLoadProcessEnvOverridesDotEnvAndFallsBack(t *testing.T) {
	baseEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("SERVER_PORT", "9091")
	t.Setenv("DATABASE_DSN", "postgres://env:secret@localhost:5432/nester?sslmode=disable")

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), strings.Join([]string{
		"APP_ENV=development",
		"SERVER_PORT=8080",
		"DATABASE_DSN=postgres://dotenv:secret@localhost:5432/nester?sslmode=disable",
		"STELLAR_NETWORK_PASSPHRASE=From DotEnv",
		"STELLAR_RPC_URL=https://dotenv-rpc.example.com",
		"STELLAR_HORIZON_URL=https://dotenv-horizon.example.com",
		"AUTH_JWT_SECRET=this-is-a-very-secret-jwt-key-that-is-at-least-thirty-two-bytes",
		"LOG_LEVEL=warn",
	}, "\n"))
	chdir(t, dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Environment() != "production" {
		t.Fatalf("expected APP_ENV from process env, got %q", cfg.Environment())
	}
	if cfg.Server().Port() != 9091 {
		t.Fatalf("expected SERVER_PORT from process env, got %d", cfg.Server().Port())
	}
	if cfg.Database().DSN() != "postgres://env:secret@localhost:5432/nester?sslmode=disable" {
		t.Fatalf("expected DATABASE_DSN from process env, got %q", cfg.Database().DSN())
	}
	if cfg.Stellar().NetworkPassphrase() != "From DotEnv" {
		t.Fatalf("expected STELLAR_NETWORK_PASSPHRASE from .env fallback, got %q", cfg.Stellar().NetworkPassphrase())
	}
	if cfg.Log().Level() != "warn" {
		t.Fatalf("expected LOG_LEVEL from .env fallback, got %q", cfg.Log().Level())
	}
}

// TestLoadMissingRequiredFieldsPartial verifies targeted error messages when
// only a subset of required fields are missing.
func TestLoadMissingRequiredFieldsPartial(t *testing.T) {
	cases := []struct {
		name          string
		set           func(t *testing.T)
		wantMissing   []string
		wantNotInErr  []string
	}{
		{
			name: "missing database dsn only",
			set: func(t *testing.T) {
				baseEnv(t)
				t.Setenv("DATABASE_DSN", "")
				t.Setenv("STELLAR_NETWORK_PASSPHRASE", "Test Network")
				t.Setenv("STELLAR_RPC_URL", "https://rpc.example.com")
				t.Setenv("STELLAR_HORIZON_URL", "https://horizon.example.com")
			},
			wantMissing:  []string{"DATABASE_DSN is required"},
			wantNotInErr: []string{"STELLAR_NETWORK_PASSPHRASE is required", "STELLAR_RPC_URL is required", "STELLAR_HORIZON_URL is required"},
		},
		{
			name: "missing both stellar urls",
			set: func(t *testing.T) {
				baseEnv(t)
				t.Setenv("DATABASE_DSN", "postgres://postgres:postgres@localhost:5432/nester?sslmode=disable")
				t.Setenv("STELLAR_NETWORK_PASSPHRASE", "Test Network")
				t.Setenv("STELLAR_RPC_URL", "")
				t.Setenv("STELLAR_HORIZON_URL", "")
			},
			wantMissing:  []string{"STELLAR_RPC_URL is required", "STELLAR_HORIZON_URL is required"},
			wantNotInErr: []string{"DATABASE_DSN is required", "STELLAR_NETWORK_PASSPHRASE is required"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.set(t)
			chdir(t, t.TempDir())

			_, err := Load()
			if err == nil {
				t.Fatal("expected Load() to fail")
			}

			message := err.Error()
			for _, expected := range tc.wantMissing {
				if !strings.Contains(message, expected) {
					t.Fatalf("expected error to contain %q, got %q", expected, message)
				}
			}

			for _, unexpected := range tc.wantNotInErr {
				if strings.Contains(message, unexpected) {
					t.Fatalf("did not expect error to contain %q, got %q", unexpected, message)
				}
			}
		})
	}
}

// TestLoadAllDefaults verifies sensible defaults when only required fields are set.
func TestLoadAllDefaults(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), strings.Join([]string{
		"DATABASE_DSN=postgres://postgres:postgres@localhost:5432/nester?sslmode=disable",
		"STELLAR_NETWORK_PASSPHRASE=Test Network",
		"STELLAR_RPC_URL=https://rpc.example.com",
		"STELLAR_HORIZON_URL=https://horizon.example.com",
		"AUTH_JWT_SECRET=this-is-a-very-secret-jwt-key-that-is-at-least-thirty-two-bytes",
	}, "\n"))
	chdir(t, dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	cases := []struct {
		name string
		got  any
		want any
	}{
		{"environment", cfg.Environment(), "development"},
		{"server host", cfg.Server().Host(), "0.0.0.0"},
		{"server port", cfg.Server().Port(), 8080},
		{"server read timeout", cfg.Server().ReadTimeout(), 15 * time.Second},
		{"server write timeout", cfg.Server().WriteTimeout(), 15 * time.Second},
		{"server graceful shutdown", cfg.Server().GracefulShutdown(), 20 * time.Second},
		{"database pool size", cfg.Database().PoolSize(), 25},
		{"database connection timeout", cfg.Database().ConnectionTimeout(), 5 * time.Second},
		{"log level", cfg.Log().Level(), "info"},
		{"log format", cfg.Log().Format(), "text"},
	}

	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("default %s: got %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}

// TestLoadDevelopmentMode verifies development-specific defaults.
func TestLoadDevelopmentMode(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")

	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Environment() != "development" {
		t.Fatalf("expected development, got %q", cfg.Environment())
	}
	if cfg.Log().Format() != "text" {
		t.Fatalf("development should default to text log format, got %q", cfg.Log().Format())
	}
}

// TestLoadProductionMode verifies production-specific defaults.
func TestLoadProductionMode(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "production")

	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Environment() != "production" {
		t.Fatalf("expected production, got %q", cfg.Environment())
	}
	if cfg.Log().Format() != "json" {
		t.Fatalf("production should default to json log format, got %q", cfg.Log().Format())
	}
}

// TestLoadUnknownKeysIgnored verifies that extra or unknown keys in .env are silently ignored.
func TestLoadUnknownKeysIgnored(t *testing.T) {
	baseEnv(t)

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), strings.Join([]string{
		"APP_ENV=test",
		"DATABASE_DSN=postgres://postgres:postgres@localhost:5432/nester?sslmode=disable",
		"STELLAR_NETWORK_PASSPHRASE=Test Network",
		"STELLAR_RPC_URL=https://rpc.example.com",
		"STELLAR_HORIZON_URL=https://horizon.example.com",
		"AUTH_JWT_SECRET=this-is-a-very-secret-jwt-key-that-is-at-least-thirty-two-bytes",
		"UNKNOWN_KEY_ONE=some-value",
		"ANOTHER_UNKNOWN=ignored",
		"TOTALLY_MADE_UP=whatever",
	}, "\n"))
	chdir(t, dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() should not fail on unknown keys, got error = %v", err)
	}

	if cfg.Environment() != "test" {
		t.Fatalf("expected test environment, got %q", cfg.Environment())
	}
}

// TestLoadEmptyEnvVarsTreatedAsUnset verifies that blank env var values fall
// through to .env file values.
func TestLoadEmptyEnvVarsTreatedAsUnset(t *testing.T) {
	baseEnv(t)
	// APP_ENV is already blanked by baseEnv; .env should supply the value.

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), strings.Join([]string{
		"APP_ENV=test",
		"DATABASE_DSN=postgres://postgres:postgres@localhost:5432/nester?sslmode=disable",
		"STELLAR_NETWORK_PASSPHRASE=Test Network",
		"STELLAR_RPC_URL=https://rpc.example.com",
		"STELLAR_HORIZON_URL=https://horizon.example.com",
		"AUTH_JWT_SECRET=this-is-a-very-secret-jwt-key-that-is-at-least-thirty-two-bytes",
	}, "\n"))
	chdir(t, dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Environment() != "test" {
		t.Fatalf("expected test environment from .env fallback, got %q", cfg.Environment())
	}
}

// TestLoadInvalidAppEnv verifies that an unrecognised APP_ENV triggers an error.
func TestLoadInvalidAppEnv(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "unknown-env")

	chdir(t, t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load() to fail for invalid APP_ENV")
	}
	if !strings.Contains(err.Error(), "APP_ENV") {
		t.Fatalf("expected error to mention APP_ENV, got %q", err.Error())
	}
}

// TestLoadInvalidLogLevel verifies that an unrecognised LOG_LEVEL triggers an error.
func TestLoadInvalidLogLevel(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("LOG_LEVEL", "verbose")

	chdir(t, t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load() to fail for invalid LOG_LEVEL")
	}
	if !strings.Contains(err.Error(), "LOG_LEVEL") {
		t.Fatalf("expected error to mention LOG_LEVEL, got %q", err.Error())
	}
}

// TestLoadInvalidLogFormat verifies that an unrecognised LOG_FORMAT triggers an error.
func TestLoadInvalidLogFormat(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("LOG_FORMAT", "yaml")

	chdir(t, t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load() to fail for invalid LOG_FORMAT")
	}
	if !strings.Contains(err.Error(), "LOG_FORMAT") {
		t.Fatalf("expected error to mention LOG_FORMAT, got %q", err.Error())
	}
}

// TestLoadInvalidStellarURLs verifies that malformed Stellar URLs trigger descriptive errors.
func TestLoadInvalidStellarURLs(t *testing.T) {
	cases := []struct {
		name        string
		rpcURL      string
		horizonURL  string
		wantInError string
	}{
		{
			name:        "non-absolute RPC URL",
			rpcURL:      "not-a-url",
			horizonURL:  "https://horizon.example.com",
			wantInError: "STELLAR_RPC_URL",
		},
		{
			name:        "non-absolute horizon URL",
			rpcURL:      "https://rpc.example.com",
			horizonURL:  "not-a-url",
			wantInError: "STELLAR_HORIZON_URL",
		},
		{
			name:        "relative RPC URL",
			rpcURL:      "/relative/path",
			horizonURL:  "https://horizon.example.com",
			wantInError: "STELLAR_RPC_URL",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseEnv(t)
			t.Setenv("APP_ENV", "development")
			t.Setenv("DATABASE_DSN", "postgres://postgres:postgres@localhost:5432/nester?sslmode=disable")
			t.Setenv("STELLAR_NETWORK_PASSPHRASE", "Test Network")
			t.Setenv("STELLAR_RPC_URL", tc.rpcURL)
			t.Setenv("STELLAR_HORIZON_URL", tc.horizonURL)

			chdir(t, t.TempDir())

			_, err := Load()
			if err == nil {
				t.Fatal("expected Load() to fail for invalid URL")
			}
			if !strings.Contains(err.Error(), tc.wantInError) {
				t.Fatalf("expected error to contain %q, got %q", tc.wantInError, err.Error())
			}
		})
	}
}

// TestLoadInvalidServerPort verifies that out-of-range SERVER_PORT values trigger errors.
func TestLoadInvalidServerPort(t *testing.T) {
	cases := []struct {
		name string
		port string
	}{
		{"zero port", "0"},
		{"negative port", "-1"},
		{"above max port", "65536"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseEnv(t)
			requiredEnv(t)
			t.Setenv("APP_ENV", "development")
			t.Setenv("SERVER_PORT", tc.port)

			chdir(t, t.TempDir())

			_, err := Load()
			if err == nil {
				t.Fatalf("expected Load() to fail for SERVER_PORT=%s", tc.port)
			}
			if !strings.Contains(err.Error(), "SERVER_PORT") {
				t.Fatalf("expected error to mention SERVER_PORT, got %q", err.Error())
			}
		})
	}
}

// TestServerConfigAddress verifies the Address() helper formats host:port correctly.
func TestServerConfigAddress(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("SERVER_HOST", "127.0.0.1")
	t.Setenv("SERVER_PORT", "3000")

	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := "127.0.0.1:3000"
	if got := cfg.Server().Address(); got != want {
		t.Fatalf("Server().Address() = %q, want %q", got, want)
	}
}

// TestLoadMultipleValidationErrors verifies that all validation errors are collected
// and reported together rather than failing on the first error.
func TestLoadMultipleValidationErrors(t *testing.T) {
	baseEnv(t)
	t.Setenv("APP_ENV", "badenv")
	t.Setenv("DATABASE_DSN", "postgres://postgres:postgres@localhost:5432/nester?sslmode=disable")
	t.Setenv("STELLAR_NETWORK_PASSPHRASE", "Test Network")
	t.Setenv("STELLAR_RPC_URL", "https://rpc.example.com")
	t.Setenv("STELLAR_HORIZON_URL", "https://horizon.example.com")
	t.Setenv("LOG_LEVEL", "verbose")
	t.Setenv("LOG_FORMAT", "yaml")

	chdir(t, t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load() to fail")
	}

	message := err.Error()
	for _, expected := range []string{"APP_ENV", "LOG_LEVEL", "LOG_FORMAT"} {
		if !strings.Contains(message, expected) {
			t.Errorf("expected error to contain %q, got:\n%s", expected, message)
		}
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()

	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%q) error = %v", dir, err)
	}

	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

type testErr struct {
	message string
}

func (e *testErr) Error() string {
	return e.message
}
