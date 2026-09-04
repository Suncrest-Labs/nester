package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
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
		"STELLAR_NETWORK_PASSPHRASE", "STELLAR_RPC_URL", "STELLAR_HORIZON_URL", "STELLAR_USDC_ISSUER",
		"AUTH_JWT_SECRET", "AUTH_ACCESS_TOKEN_EXPIRY", "AUTH_REFRESH_TOKEN_EXPIRY", "AUTH_ABSOLUTE_SESSION_LIFETIME", "AUTH_CHALLENGE_EXPIRY",
		"RATELIMIT_GLOBAL_LIMIT", "RATELIMIT_GLOBAL_WINDOW", "RATELIMIT_WRITE_LIMIT", "RATELIMIT_WRITE_WINDOW",
		"RATELIMIT_WALLET_LIMIT", "RATELIMIT_WALLET_WINDOW",
		"RATELIMIT_TRUSTED_PROXY_COUNT",
		"LOG_LEVEL", "LOG_FORMAT",
		"ALLOWED_ORIGINS",
		"RUN_MIGRATIONS", "MIGRATIONS_DIR", "STARTUP_DEPENDENCY_TIMEOUT",
		"METRICS_ENABLED", "METRICS_ADDR",
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
		"ALLOWED_ORIGINS=https://app.example.com",
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

func TestLoadStellarUSDCIssuerDefault(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)

	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	const expected = "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"
	if cfg.Stellar().USDCIssuer() != expected {
		t.Fatalf("expected default USDC issuer %q, got %q", expected, cfg.Stellar().USDCIssuer())
	}
}

func TestLoadStellarUSDCIssuerFromEnv(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("STELLAR_USDC_ISSUER", "GTESTUSDCISSUERADDRESSEXAMPLE12345")

	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Stellar().USDCIssuer() != "GTESTUSDCISSUERADDRESSEXAMPLE12345" {
		t.Fatalf("expected USDC issuer from env, got %q", cfg.Stellar().USDCIssuer())
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
	t.Setenv("ALLOWED_ORIGINS", "https://app.example.com")

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
		"ALLOWED_ORIGINS=https://app.example.com",
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
	t.Setenv("ALLOWED_ORIGINS", "https://app.example.com")

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
		name         string
		set          func(t *testing.T)
		wantMissing  []string
		wantNotInErr []string
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
		{"ratelimit global limit", cfg.RateLimit().GlobalLimit(), 100},
		{"ratelimit global window", cfg.RateLimit().GlobalWindow(), 1 * time.Minute},
		{"ratelimit write limit", cfg.RateLimit().WriteLimit(), 20},
		{"ratelimit write window", cfg.RateLimit().WriteWindow(), 1 * time.Minute},
		{"ratelimit wallet limit", cfg.RateLimit().WalletLimit(), 60},
		{"ratelimit wallet window", cfg.RateLimit().WalletWindow(), 1 * time.Minute},
		{"ratelimit auth limit", cfg.RateLimit().AuthLimit(), 10},
		{"ratelimit auth window", cfg.RateLimit().AuthWindow(), 1 * time.Minute},
		{"ratelimit trusted proxy count", cfg.RateLimit().TrustedProxyCount(), 0},
		{"ratelimit quota enabled", cfg.RateLimit().QuotaEnabled(), true},
		{"ratelimit quota limit", cfg.RateLimit().QuotaLimit(), 300},
		{"ratelimit quota window", cfg.RateLimit().QuotaWindow(), 1 * time.Minute},
		{"ratelimit quota bypass token", cfg.RateLimit().QuotaBypassToken(), ""},
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
	t.Setenv("ALLOWED_ORIGINS", "https://app.example.com")

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

// TestLoadWalletRateLimitRejectsNonPositiveValues verifies validation.
func TestLoadWalletRateLimitRejectsNonPositiveValues(t *testing.T) {
	cases := []struct {
		name string
		key  string
		val  string
		want string
	}{
		{"zero limit", "RATELIMIT_WALLET_LIMIT", "0", "RATELIMIT_WALLET_LIMIT must be greater than 0"},
		{"negative limit", "RATELIMIT_WALLET_LIMIT", "-1", "RATELIMIT_WALLET_LIMIT must be greater than 0"},
		{"zero window", "RATELIMIT_WALLET_WINDOW", "0s", "RATELIMIT_WALLET_WINDOW must be greater than 0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseEnv(t)
			requiredEnv(t)
			t.Setenv("APP_ENV", "development")
			t.Setenv(tc.key, tc.val)

			chdir(t, t.TempDir())

			_, err := Load()
			if err == nil {
				t.Fatalf("expected Load() to fail for %s=%s", tc.key, tc.val)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error to contain %q, got %q", tc.want, err.Error())
			}
		})
	}
}

// TestLoadWalletRateLimitOverrides verifies env overrides are honoured.
func TestLoadWalletRateLimitOverrides(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("RATELIMIT_WALLET_LIMIT", "30")
	t.Setenv("RATELIMIT_WALLET_WINDOW", "15s")

	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.RateLimit().WalletLimit(); got != 30 {
		t.Errorf("WalletLimit() = %d, want 30", got)
	}
	if got := cfg.RateLimit().WalletWindow(); got != 15*time.Second {
		t.Errorf("WalletWindow() = %s, want 15s", got)
	}
}

// TestLoadSensitiveRateLimitRejectsNonPositiveValues verifies validation of the
func TestLoadSensitiveRateLimitRejectsNonPositiveValues(t *testing.T) {
	cases := []struct {
		name string
		key  string
		val  string
		want string
	}{
		{"zero auth limit", "RATELIMIT_AUTH_LIMIT", "0", "RATELIMIT_AUTH_LIMIT must be greater than 0"},
		{"negative auth limit", "RATELIMIT_AUTH_LIMIT", "-1", "RATELIMIT_AUTH_LIMIT must be greater than 0"},
		{"zero auth window", "RATELIMIT_AUTH_WINDOW", "0s", "RATELIMIT_AUTH_WINDOW must be greater than 0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseEnv(t)
			requiredEnv(t)
			t.Setenv("APP_ENV", "development")
			t.Setenv(tc.key, tc.val)

			chdir(t, t.TempDir())

			_, err := Load()
			if err == nil {
				t.Fatalf("expected Load() to fail for %s=%s", tc.key, tc.val)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error to contain %q, got %q", tc.want, err.Error())
			}
		})
	}
}

// TestLoadSensitiveRateLimitOverrides verifies env overrides for the strict auth
func TestLoadSensitiveRateLimitOverrides(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("RATELIMIT_AUTH_LIMIT", "7")
	t.Setenv("RATELIMIT_AUTH_WINDOW", "30s")

	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.RateLimit().AuthLimit(); got != 7 {
		t.Errorf("AuthLimit() = %d, want 7", got)
	}
	if got := cfg.RateLimit().AuthWindow(); got != 30*time.Second {
		t.Errorf("AuthWindow() = %s, want 30s", got)
	}
}

// TestLoadRateLimitRejectsSubMillisecondWindows verifies that Redis-backed
// limiter windows below 1ms are rejected: the Redis limiter converts the window
// to whole milliseconds for PEXPIRE, so a sub-ms window truncates to 0 and would
// silently disable enforcement.
func TestLoadRateLimitRejectsSubMillisecondWindows(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want string
	}{
		{"global", "RATELIMIT_GLOBAL_WINDOW", "RATELIMIT_GLOBAL_WINDOW must be at least 1ms"},
		{"auth", "RATELIMIT_AUTH_WINDOW", "RATELIMIT_AUTH_WINDOW must be at least 1ms"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseEnv(t)
			requiredEnv(t)
			t.Setenv("APP_ENV", "development")
			t.Setenv(tc.key, "500us")

			chdir(t, t.TempDir())

			_, err := Load()
			if err == nil {
				t.Fatalf("expected Load() to fail for %s=500us", tc.key)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error to contain %q, got %q", tc.want, err.Error())
			}
		})
	}
}

// TestLoadTrustedProxyCountOverride verifies RATELIMIT_TRUSTED_PROXY_COUNT is
// honoured and that a negative value is rejected.
func TestLoadTrustedProxyCountOverride(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("RATELIMIT_TRUSTED_PROXY_COUNT", "2")

	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.RateLimit().TrustedProxyCount(); got != 2 {
		t.Errorf("TrustedProxyCount() = %d, want 2", got)
	}
}

func TestLoadTrustedProxyCountRejectsNegative(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("RATELIMIT_TRUSTED_PROXY_COUNT", "-1")

	chdir(t, t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load() to fail for RATELIMIT_TRUSTED_PROXY_COUNT=-1")
	}
	if !strings.Contains(err.Error(), "RATELIMIT_TRUSTED_PROXY_COUNT must be zero or greater") {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}

// TestLoadAllowedOriginsParsed verifies ALLOWED_ORIGINS is split on commas
// with whitespace trimmed and empty entries dropped.
func TestLoadAllowedOriginsParsed(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("ALLOWED_ORIGINS", "https://app.example.com, https://example.com ,,http://localhost:3000")

	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got := cfg.AllowedOrigins()
	want := []string{"https://app.example.com", "https://example.com", "http://localhost:3000"}
	if len(got) != len(want) {
		t.Fatalf("AllowedOrigins() = %v, want %v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("AllowedOrigins()[%d] = %q, want %q", i, got[i], v)
		}
	}
}

// TestLoadAllowedOriginsRequiredInProduction verifies production requires
// ALLOWED_ORIGINS to be populated.
func TestLoadAllowedOriginsRequiredInProduction(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "production")

	chdir(t, t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load() to fail when ALLOWED_ORIGINS is empty in production")
	}
	if !strings.Contains(err.Error(), "ALLOWED_ORIGINS") {
		t.Fatalf("expected error to mention ALLOWED_ORIGINS, got %q", err.Error())
	}
}

// TestLoadAllowedOriginsRejectsWildcard verifies "*" is rejected explicitly.
func TestLoadAllowedOriginsRejectsWildcard(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("ALLOWED_ORIGINS", "*")

	chdir(t, t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load() to fail when ALLOWED_ORIGINS contains a wildcard")
	}
	if !strings.Contains(err.Error(), "wildcard") {
		t.Fatalf("expected wildcard error, got %q", err.Error())
	}
}

// TestLoadRejectsDefaultJWTSecretInProduction verifies the .env.example dev default
// AUTH_JWT_SECRET is rejected in production (it passes the length check, so it needs
// an explicit guard).
func TestLoadRejectsDefaultJWTSecretInProduction(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("ALLOWED_ORIGINS", "https://app.example.com")
	t.Setenv("AUTH_JWT_SECRET", "dev-nester-jwt-secret-change-in-production")

	chdir(t, t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load() to fail when AUTH_JWT_SECRET uses the dev default in production")
	}
	if !strings.Contains(err.Error(), "development default") {
		t.Fatalf("expected dev-default error, got %q", err.Error())
	}
}

// TestLoadAllowsDefaultJWTSecretInDevelopment verifies the guard only applies outside development.
func TestLoadAllowsDefaultJWTSecretInDevelopment(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("AUTH_JWT_SECRET", "dev-nester-jwt-secret-change-in-production")

	chdir(t, t.TempDir())

	if _, err := Load(); err != nil {
		t.Fatalf("expected Load() to succeed in development with the dev default secret, got %v", err)
	}
}

// TestLoadRejectsLowEntropyJWTSecret verifies that a secret long enough to
// pass the length check but composed of too few distinct characters is rejected
// (nester#1106).
func TestLoadRejectsLowEntropyJWTSecret(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")
	// 32 'a's: passes length, fails entropy.
	t.Setenv("AUTH_JWT_SECRET", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	chdir(t, t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load() to fail when AUTH_JWT_SECRET has insufficient entropy")
	}
	if !strings.Contains(err.Error(), "entropy") {
		t.Fatalf("expected entropy error message, got %q", err.Error())
	}
}

// TestJWTSecretHasAdequateEntropy covers the helper directly.
func TestJWTSecretHasAdequateEntropy(t *testing.T) {
	cases := []struct {
		secret string
		want   bool
	}{
		{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},  // single distinct byte
		{"abababababababababababababababab", false},  // only 2 distinct bytes
		{"abcdefg" + strings.Repeat("a", 25), false}, // 7 distinct bytes
		{"abcdefgh" + strings.Repeat("a", 24), true}, // exactly 8 distinct bytes
		{"this-is-a-very-secret-jwt-key-that-is-at-least-thirty-two-bytes", true},
	}
	for _, tc := range cases {
		if got := jwtSecretHasAdequateEntropy(tc.secret); got != tc.want {
			t.Errorf("jwtSecretHasAdequateEntropy(%q) = %v, want %v", tc.secret, got, tc.want)
		}
	}
}

// TestLoadAllowedOriginsRejectsMalformed verifies malformed origins are rejected.
func TestLoadAllowedOriginsRejectsMalformed(t *testing.T) {
	cases := []struct {
		name   string
		origin string
	}{
		{"missing scheme", "app.example.com"},
		{"unsupported scheme", "ftp://example.com"},
		{"has path", "https://example.com/api"},
		{"has query", "https://example.com?foo=1"},
		{"trailing slash", "https://example.com/"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseEnv(t)
			requiredEnv(t)
			t.Setenv("APP_ENV", "development")
			t.Setenv("ALLOWED_ORIGINS", tc.origin)

			chdir(t, t.TempDir())

			_, err := Load()
			if err == nil {
				t.Fatalf("expected Load() to fail for malformed origin %q", tc.origin)
			}
			if !strings.Contains(err.Error(), "ALLOWED_ORIGINS") {
				t.Fatalf("expected error to mention ALLOWED_ORIGINS, got %q", err.Error())
			}
		})
	}
}

// TestLoadPaymentProviderKeysOptional verifies that no environment requires a
// fiat payment provider key. The offramp feature the keys served was dropped
// (#1154); requiring them only blocked staging deploys behind dummy
// credentials, or pushed operators to set a wrong APP_ENV -- which would also
// silently disable the JWT-default and CORS-origin guards.
func TestLoadPaymentProviderKeysOptional(t *testing.T) {
	for _, env := range []string{"production", "staging", "development", "test"} {
		t.Run(env+" succeeds with no provider key", func(t *testing.T) {
			baseEnv(t)
			requiredEnv(t)
			t.Setenv("APP_ENV", env)
			if env == "production" || env == "staging" {
				t.Setenv("ALLOWED_ORIGINS", "https://app.example.com")
			}

			chdir(t, t.TempDir())

			if _, err := Load(); err != nil {
				t.Fatalf("Load() error = %v (no environment should require a fiat provider key)", err)
			}
		})
	}
}

// TestStagingGuardsSurviveProviderKeyRemoval pins the guards that dropping the
// fiat requirement must not weaken: staging still rejects the development JWT
// default and still demands an explicit CORS origin list. These are the guards
// an operator would have lost by setting a wrong APP_ENV to work around the
// provider requirement, so they are asserted here alongside its removal.
func TestStagingGuardsSurviveProviderKeyRemoval(t *testing.T) {
	t.Run("staging still rejects the development JWT default", func(t *testing.T) {
		baseEnv(t)
		requiredEnv(t)
		t.Setenv("APP_ENV", "staging")
		t.Setenv("ALLOWED_ORIGINS", "https://app.example.com")
		t.Setenv("AUTH_JWT_SECRET", defaultDevJWTSecret)

		chdir(t, t.TempDir())

		_, err := Load()
		if err == nil {
			t.Fatal("expected Load() to reject the development JWT default in staging")
		}
		if !strings.Contains(err.Error(), "AUTH_JWT_SECRET") {
			t.Fatalf("expected error to mention AUTH_JWT_SECRET, got %q", err.Error())
		}
	})

	t.Run("staging still requires an explicit CORS origin list", func(t *testing.T) {
		baseEnv(t)
		requiredEnv(t)
		t.Setenv("APP_ENV", "staging")
		t.Setenv("ALLOWED_ORIGINS", "")

		chdir(t, t.TempDir())

		_, err := Load()
		if err == nil {
			t.Fatal("expected Load() to require ALLOWED_ORIGINS in staging")
		}
		if !strings.Contains(err.Error(), "ALLOWED_ORIGINS") {
			t.Fatalf("expected error to mention ALLOWED_ORIGINS, got %q", err.Error())
		}
	})
}

// TestLoadAllowedOriginsOptionalInDevelopment verifies development loads
// successfully with no ALLOWED_ORIGINS set.
func TestLoadAllowedOriginsOptionalInDevelopment(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")

	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.AllowedOrigins()) != 0 {
		t.Fatalf("expected empty AllowedOrigins() in dev with no env, got %v", cfg.AllowedOrigins())
	}
}

// TestLoadRunMigrationsFlag verifies RUN_MIGRATIONS controls startup auto-migrate.
func TestLoadRunMigrationsFlag(t *testing.T) {
	cases := []struct {
		name       string
		envValue   string
		wantEnable bool
	}{
		{"default false when unset", "", false},
		{"true when enabled", "true", true},
		{"false when explicitly disabled", "false", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseEnv(t)
			requiredEnv(t)
			t.Setenv("APP_ENV", "development")
			if tc.envValue != "" {
				t.Setenv("RUN_MIGRATIONS", tc.envValue)
			}

			chdir(t, t.TempDir())

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got := cfg.Startup().EnableAutoMigrate(); got != tc.wantEnable {
				t.Fatalf("EnableAutoMigrate() = %v, want %v", got, tc.wantEnable)
			}
		})
	}
}

// TestLoadMigrationsDir verifies MIGRATIONS_DIR defaults and can be overridden.
func TestLoadMigrationsDir(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		baseEnv(t)
		requiredEnv(t)
		t.Setenv("APP_ENV", "development")

		chdir(t, t.TempDir())

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got := cfg.Startup().MigrationsDir(); got != "./migrations" {
			t.Fatalf("MigrationsDir() = %q, want ./migrations", got)
		}
	})

	t.Run("override", func(t *testing.T) {
		baseEnv(t)
		requiredEnv(t)
		t.Setenv("APP_ENV", "development")
		t.Setenv("MIGRATIONS_DIR", "/custom/migrations")

		chdir(t, t.TempDir())

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if got := cfg.Startup().MigrationsDir(); got != "/custom/migrations" {
			t.Fatalf("MigrationsDir() = %q, want /custom/migrations", got)
		}
	})
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

// Quotas must be tunable per environment — the whole point is that staging can
// run tighter limits than production while a load test opts out entirely.
func TestLoadQuotaOverrides(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("RATELIMIT_QUOTA_LIMIT", "50")
	t.Setenv("RATELIMIT_QUOTA_WINDOW", "30s")
	t.Setenv("RATELIMIT_QUOTA_BYPASS_TOKEN", "load-test-token")

	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.RateLimit().QuotaLimit(); got != 50 {
		t.Errorf("QuotaLimit() = %d, want 50", got)
	}
	if got := cfg.RateLimit().QuotaWindow(); got != 30*time.Second {
		t.Errorf("QuotaWindow() = %s, want 30s", got)
	}
	if got := cfg.RateLimit().QuotaBypassToken(); got != "load-test-token" {
		t.Errorf("QuotaBypassToken() = %q, want %q", got, "load-test-token")
	}
	if !cfg.RateLimit().QuotaEnabled() {
		t.Error("QuotaEnabled() = false, want true by default")
	}
}

// The documented load-test opt-out.
func TestLoadQuotaCanBeDisabled(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("RATELIMIT_QUOTA_ENABLED", "false")

	chdir(t, t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.RateLimit().QuotaEnabled() {
		t.Error("QuotaEnabled() = true, want false")
	}
}

func TestLoadQuotaRejectsNonPositiveLimit(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("RATELIMIT_QUOTA_LIMIT", "0")

	chdir(t, t.TempDir())

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want an error for RATELIMIT_QUOTA_LIMIT=0")
	}
}

// A sub-millisecond window truncates to a zero refill rate, leaving a bucket
// that never refills — worse than no limiter, because it locks every caller out.
func TestLoadQuotaRejectsSubMillisecondWindow(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("RATELIMIT_QUOTA_WINDOW", "100us")

	chdir(t, t.TempDir())

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want an error for a sub-millisecond quota window")
	}
}

// A disabled quota should not force its numbers to stay meaningful.
func TestLoadQuotaValidationSkippedWhenDisabled(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("RATELIMIT_QUOTA_ENABLED", "false")
	t.Setenv("RATELIMIT_QUOTA_LIMIT", "0")

	chdir(t, t.TempDir())

	if _, err := Load(); err != nil {
		t.Fatalf("Load() error = %v, want nil when quotas are disabled", err)
	}
}

// TestLoadLaunchCapsValidation table-drives LAUNCH_PER_USER_DEPOSIT_CAP /
// LAUNCH_GLOBAL_TVL_CAP parsing (nester CodeRabbit finding): blank/"0" must
// disable the cap silently, a valid positive value must be accepted, and a
// negative or malformed value must fail Load() rather than silently
// disabling the cap.
func TestLoadLaunchCapsValidation(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "blank disables cap", value: "", wantErr: false},
		{name: "zero disables cap", value: "0", wantErr: false},
		{name: "valid positive enables cap", value: "1000.50", wantErr: false},
		{name: "negative value errors", value: "-100", wantErr: true},
		{name: "malformed value errors", value: "not-a-number", wantErr: true},
	}

	for _, tt := range tests {
		t.Run("per_user/"+tt.name, func(t *testing.T) {
			baseEnv(t)
			requiredEnv(t)
			t.Setenv("APP_ENV", "development")
			// Clear both cap env vars before setting the one this case
			// actually exercises, so a case can't inherit a leftover value
			// set by a previous subtest for the other cap (nester CodeRabbit
			// post-rebase finding: order-dependent flakiness).
			t.Setenv("LAUNCH_PER_USER_DEPOSIT_CAP", "")
			t.Setenv("LAUNCH_GLOBAL_TVL_CAP", "")
			t.Setenv("LAUNCH_PER_USER_DEPOSIT_CAP", tt.value)
			chdir(t, t.TempDir())

			_, err := Load()
			if tt.wantErr && err == nil {
				t.Fatalf("Load() error = nil, want an error for LAUNCH_PER_USER_DEPOSIT_CAP=%q", tt.value)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Load() error = %v, want nil for LAUNCH_PER_USER_DEPOSIT_CAP=%q", err, tt.value)
			}
		})

		t.Run("global/"+tt.name, func(t *testing.T) {
			baseEnv(t)
			requiredEnv(t)
			t.Setenv("APP_ENV", "development")
			// See the matching comment in the per_user subtest above.
			t.Setenv("LAUNCH_PER_USER_DEPOSIT_CAP", "")
			t.Setenv("LAUNCH_GLOBAL_TVL_CAP", "")
			t.Setenv("LAUNCH_GLOBAL_TVL_CAP", tt.value)
			chdir(t, t.TempDir())

			_, err := Load()
			if tt.wantErr && err == nil {
				t.Fatalf("Load() error = nil, want an error for LAUNCH_GLOBAL_TVL_CAP=%q", tt.value)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Load() error = %v, want nil for LAUNCH_GLOBAL_TVL_CAP=%q", err, tt.value)
			}
		})
	}
}

// TestLoadLaunchCapWarnThresholdsValidation table-drives
// LAUNCH_CAP_WARN_THRESHOLDS_PCT parsing (nester CodeRabbit post-rebase
// finding): every threshold must be in 1..100 and the list must be strictly
// increasing, checked at Load() time rather than silently accepted.
func TestLoadLaunchCapWarnThresholdsValidation(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "default-shaped ascending pair", value: "80,90", wantErr: false},
		{name: "single threshold", value: "50", wantErr: false},
		{name: "empty disables warnings", value: "", wantErr: false},
		{name: "zero is out of range", value: "0,90", wantErr: true},
		{name: "over 100 is out of range", value: "80,101", wantErr: true},
		{name: "duplicate values not strictly increasing", value: "80,80", wantErr: true},
		{name: "descending order rejected", value: "90,80", wantErr: true},
		{name: "non-integer token rejected", value: "80,abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseEnv(t)
			requiredEnv(t)
			t.Setenv("APP_ENV", "development")
			t.Setenv("LAUNCH_PER_USER_DEPOSIT_CAP", "")
			t.Setenv("LAUNCH_GLOBAL_TVL_CAP", "")
			t.Setenv("LAUNCH_CAP_WARN_THRESHOLDS_PCT", tt.value)
			chdir(t, t.TempDir())

			_, err := Load()
			if tt.wantErr && err == nil {
				t.Fatalf("Load() error = nil, want an error for LAUNCH_CAP_WARN_THRESHOLDS_PCT=%q", tt.value)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Load() error = %v, want nil for LAUNCH_CAP_WARN_THRESHOLDS_PCT=%q", err, tt.value)
			}
		})
	}
}
