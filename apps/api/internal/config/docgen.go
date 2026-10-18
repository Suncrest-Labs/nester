package config

import (
	"fmt"
	"strings"
)

// EnvVarSpec documents a single environment variable consumed by Config.Load().
type EnvVarSpec struct {
	Name              string
	Type              string
	Default           string
	Required          bool
	Secret            bool
	MainnetConstraint string
}

// EnvSpecs returns the canonical metadata table for every environment variable
// read by apps/api/internal/config.
func EnvSpecs() []EnvVarSpec {
	return []EnvVarSpec{
		{Name: "APP_ENV", Type: "string", Default: `"development"`, Required: false, Secret: false, MainnetConstraint: `Must be "production" on mainnet (allowed: development, staging, production, test)`},
		{Name: "SERVER_HOST", Type: "string", Default: `"0.0.0.0"`, Required: false, Secret: false, MainnetConstraint: "Non-empty bind address"},
		{Name: "SERVER_PORT", Type: "int", Default: "8080", Required: false, Secret: false, MainnetConstraint: "Between 1 and 65535"},
		{Name: "SERVER_READ_TIMEOUT", Type: "duration", Default: "15s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "SERVER_READ_HEADER_TIMEOUT", Type: "duration", Default: "10s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "SERVER_WRITE_TIMEOUT", Type: "duration", Default: "15s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "SERVER_IDLE_TIMEOUT", Type: "duration", Default: "1m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "SERVER_SHUTDOWN_TIMEOUT", Type: "duration", Default: "20s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "SERVER_MAX_HEADER_BYTES", Type: "int", Default: "1048576", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "DATABASE_DSN", Type: "string", Default: "—", Required: true, Secret: true, MainnetConstraint: "Required PostgreSQL DSN; TLS (sslmode=require/verify-full) mandatory on mainnet"},
		{Name: "DATABASE_POOL_SIZE", Type: "int", Default: "25", Required: false, Secret: false, MainnetConstraint: "Between 1 and 10000"},
		{Name: "DATABASE_CONNECTION_TIMEOUT", Type: "duration", Default: "5s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "STELLAR_NETWORK_PASSPHRASE", Type: "string", Default: "—", Required: true, Secret: false, MainnetConstraint: `Must equal "Public Global Stellar Network ; September 2015"`},
		{Name: "STELLAR_RPC_URL", Type: "url", Default: "—", Required: true, Secret: false, MainnetConstraint: "Valid absolute URL; rejected if hostname contains testnet or futurenet when passphrase is mainnet"},
		{Name: "STELLAR_HORIZON_URL", Type: "url", Default: "—", Required: true, Secret: false, MainnetConstraint: "Valid absolute URL; rejected if hostname contains testnet or futurenet when passphrase is mainnet"},
		{Name: "STELLAR_OPERATOR_SECRET", Type: "string", Default: `""`, Required: false, Secret: true, MainnetConstraint: "Leave empty on mainnet when SIGNER_SOCKET_PATH delegates signing to isolated signer"},
		{Name: "STELLAR_OPERATOR_FUNDED_DEPOSITS_ENABLED", Type: "bool", Default: "false", Required: false, Secret: false, MainnetConstraint: "Must remain false on mainnet unless explicitly allowlisted"},
		{Name: "STELLAR_OPERATOR_FUNDED_DEPOSIT_VAULTS", Type: "string", Default: `""`, Required: false, Secret: false, MainnetConstraint: "Comma-separated vault ID allowlist when operator-funded deposits are enabled"},
		{Name: "STELLAR_OPERATOR_FUNDED_DEPOSIT_MAX_AMOUNT", Type: "decimal", Default: `"0"`, Required: false, Secret: false, MainnetConstraint: "Non-negative decimal cap per operator-funded deposit"},
		{Name: "STELLAR_OPERATOR_ADDRESS", Type: "string", Default: `""`, Required: false, Secret: false, MainnetConstraint: "Operator G-address required when SIGNER_SOCKET_PATH is set"},
		{Name: "SIGNER_SOCKET_PATH", Type: "string", Default: `""`, Required: false, Secret: false, MainnetConstraint: "Unix domain socket path to isolated signer process on mainnet"},
		{Name: "STELLAR_USDC_ISSUER", Type: "string", Default: `"GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"`, Required: false, Secret: false, MainnetConstraint: "Mainnet Circle USDC issuer G-address"},
		{Name: "YIELD_REGISTRY_CONTRACT", Type: "string", Default: `""`, Required: false, Secret: false, MainnetConstraint: "Required when STELLAR_NETWORK_PASSPHRASE is the mainnet passphrase"},
		{Name: "STELLAR_ALLOCATION_STRATEGY_ADDRESS", Type: "string", Default: `""`, Required: false, Secret: false, MainnetConstraint: "Required when STELLAR_NETWORK_PASSPHRASE is the mainnet passphrase"},
		{Name: "WITHDRAWAL_SLIPPAGE_BPS", Type: "int", Default: "50", Required: false, Secret: false, MainnetConstraint: "Between 1 and 300"},
		{Name: "HARVEST_DEFAULT_COMPOUND", Type: "bool", Default: "true", Required: false, Secret: false, MainnetConstraint: "None"},
		{Name: "STELLAR_MAINNET_VAULT_TVL_CAP", Type: "decimal", Default: `"0"`, Required: false, Secret: false, MainnetConstraint: "Enforced on mainnet; Stage 1 rollout sets 10000 USDC"},
		{Name: "WITHDRAWAL_BREAKER_ENABLED", Type: "bool", Default: "false", Required: false, Secret: false, MainnetConstraint: "Recommended true on mainnet to halt anomalous vault outflows"},
		{Name: "WITHDRAWAL_BREAKER_THRESHOLD_PERCENT", Type: "decimal", Default: `"25"`, Required: false, Secret: false, MainnetConstraint: "Rolling-window outflow percentage of vault TVL"},
		{Name: "WITHDRAWAL_BREAKER_WINDOW", Type: "duration", Default: "1h0m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "MIN_ALLOCATION_WEIGHT", Type: "int", Default: "5", Required: false, Secret: false, MainnetConstraint: "Between 1 and 100"},
		{Name: "REDIS_ADDR", Type: "string", Default: `""`, Required: false, Secret: false, MainnetConstraint: "Required for shared rate-limiting and auth challenge state across mainnet replicas"},
		{Name: "TRACING_ENABLED", Type: "bool", Default: "false", Required: false, Secret: false, MainnetConstraint: "When true in staging/production, OTEL_EXPORTER_OTLP_INSECURE must be false"},
		{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Type: "string", Default: `"localhost:4317"`, Required: false, Secret: false, MainnetConstraint: "Required when TRACING_ENABLED is true"},
		{Name: "OTEL_EXPORTER_OTLP_INSECURE", Type: "bool", Default: "true", Required: false, Secret: false, MainnetConstraint: "Must be false when TRACING_ENABLED is true outside development"},
		{Name: "OTEL_SERVICE_NAME", Type: "string", Default: `"nester-api"`, Required: false, Secret: false, MainnetConstraint: "Required when TRACING_ENABLED is true"},
		{Name: "OTEL_EXPORTER_TIMEOUT", Type: "duration", Default: "10s", Required: false, Secret: false, MainnetConstraint: "Must be > 0 when TRACING_ENABLED is true"},
		{Name: "TRACING_SAMPLE_RATIO", Type: "float", Default: "0.05", Required: false, Secret: false, MainnetConstraint: "Between 0 and 1"},
		{Name: "TRACING_LATENCY_THRESHOLD", Type: "duration", Default: "1s", Required: false, Secret: false, MainnetConstraint: "Must be >= 0"},
		{Name: "AUTH_JWT_SECRET", Type: "string", Default: "—", Required: true, Secret: true, MainnetConstraint: "At least 32 chars and >= 8 distinct bytes; dev default rejected in staging/production"},
		{Name: "NESTER_SERVICE_API_KEY", Type: "string", Default: `""`, Required: false, Secret: true, MainnetConstraint: "If set: >= 32 chars, >= 8 distinct bytes, and must not equal AUTH_JWT_SECRET"},
		{Name: "AUTH_ACCESS_TOKEN_EXPIRY", Type: "duration", Default: "5m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0 and < AUTH_REFRESH_TOKEN_EXPIRY"},
		{Name: "AUTH_REFRESH_TOKEN_EXPIRY", Type: "duration", Default: "168h0m0s", Required: false, Secret: false, MainnetConstraint: "Must be > AUTH_ACCESS_TOKEN_EXPIRY and < AUTH_ABSOLUTE_SESSION_LIFETIME"},
		{Name: "AUTH_ABSOLUTE_SESSION_LIFETIME", Type: "duration", Default: "720h0m0s", Required: false, Secret: false, MainnetConstraint: "Must be > AUTH_REFRESH_TOKEN_EXPIRY"},
		{Name: "AUTH_CHALLENGE_EXPIRY", Type: "duration", Default: "5m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "RATELIMIT_GLOBAL_LIMIT", Type: "int", Default: "100", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "RATELIMIT_GLOBAL_WINDOW", Type: "duration", Default: "1m0s", Required: false, Secret: false, MainnetConstraint: "Must be >= 1ms"},
		{Name: "RATELIMIT_WRITE_LIMIT", Type: "int", Default: "20", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "RATELIMIT_WRITE_WINDOW", Type: "duration", Default: "1m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "RATELIMIT_WALLET_LIMIT", Type: "int", Default: "60", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "RATELIMIT_WALLET_WINDOW", Type: "duration", Default: "1m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "RATELIMIT_REBALANCE_LIMIT", Type: "int", Default: "3", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "RATELIMIT_REBALANCE_WINDOW", Type: "duration", Default: "1h0m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "RATELIMIT_AUTH_LIMIT", Type: "int", Default: "10", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "RATELIMIT_AUTH_WINDOW", Type: "duration", Default: "1m0s", Required: false, Secret: false, MainnetConstraint: "Must be >= 1ms"},
		{Name: "RATELIMIT_APIKEY_LIMIT", Type: "int", Default: "300", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "RATELIMIT_APIKEY_WINDOW", Type: "duration", Default: "1m0s", Required: false, Secret: false, MainnetConstraint: "Must be >= 1ms"},
		{Name: "AUTH_FAILURE_THRESHOLD", Type: "int", Default: "5", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "AUTH_FAILURE_WINDOW", Type: "duration", Default: "15m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "AUTH_LOCKOUT_BASE", Type: "duration", Default: "30s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "AUTH_LOCKOUT_MAX", Type: "duration", Default: "15m0s", Required: false, Secret: false, MainnetConstraint: "Must be >= AUTH_LOCKOUT_BASE"},
		{Name: "RATELIMIT_TRUSTED_PROXY_COUNT", Type: "int", Default: "0", Required: false, Secret: false, MainnetConstraint: "Must be >= 0; set to ingress proxy hop count on mainnet"},
		{Name: "RATELIMIT_QUOTA_ENABLED", Type: "bool", Default: "true", Required: false, Secret: false, MainnetConstraint: "Must remain true on mainnet"},
		{Name: "RATELIMIT_QUOTA_LIMIT", Type: "int", Default: "300", Required: false, Secret: false, MainnetConstraint: "Must be > 0 when RATELIMIT_QUOTA_ENABLED is true"},
		{Name: "RATELIMIT_QUOTA_WINDOW", Type: "duration", Default: "1m0s", Required: false, Secret: false, MainnetConstraint: "Must be >= 1ms when RATELIMIT_QUOTA_ENABLED is true"},
		{Name: "RATELIMIT_QUOTA_BYPASS_TOKEN", Type: "string", Default: `""`, Required: false, Secret: true, MainnetConstraint: "Leave empty on mainnet"},
		{Name: "LOG_LEVEL", Type: "string", Default: `"info"`, Required: false, Secret: false, MainnetConstraint: "One of debug, info, warn, error"},
		{Name: "LOG_FORMAT", Type: "string", Default: `"text" (dev) / "json" (staging, prod)`, Required: false, Secret: false, MainnetConstraint: "One of json, text (defaults to json in production)"},
		{Name: "ALLOWED_ORIGINS", Type: "[]string", Default: `""`, Required: false, Secret: false, MainnetConstraint: `Required in staging/production; explicit scheme://host[:port] origins only (no "*")`},
		{Name: "PERFORMANCE_SNAPSHOT_INTERVAL", Type: "duration", Default: "1h0m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "TVL_REFRESH_INTERVAL", Type: "duration", Default: "15m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "LAUNCH_PER_USER_DEPOSIT_CAP", Type: "decimal", Default: `""`, Required: false, Secret: false, MainnetConstraint: "Non-negative decimal or empty/0"},
		{Name: "LAUNCH_GLOBAL_TVL_CAP", Type: "decimal", Default: `""`, Required: false, Secret: false, MainnetConstraint: "Non-negative decimal or empty/0"},
		{Name: "LAUNCH_CAP_WARN_THRESHOLDS_PCT", Type: "[]int", Default: `"80,90"`, Required: false, Secret: false, MainnetConstraint: "Strictly increasing integers in 1..100"},
		{Name: "APY_REFRESH_INTERVAL", Type: "duration", Default: "5m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "APY_BROADCAST_THRESHOLD", Type: "int", Default: "50", Required: false, Secret: false, MainnetConstraint: "Must be >= 0"},
		{Name: "RUN_MIGRATIONS", Type: "bool", Default: "false", Required: false, Secret: false, MainnetConstraint: "Run via pre-deploy job on mainnet; keep false on serving pods"},
		{Name: "MIGRATIONS_DIR", Type: "string", Default: `"./migrations"`, Required: false, Secret: false, MainnetConstraint: "Non-empty path"},
		{Name: "STARTUP_DEPENDENCY_TIMEOUT", Type: "duration", Default: "5s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "BANK_ACCOUNT_ENCRYPTION_KEY", Type: "string", Default: `""`, Required: false, Secret: true, MainnetConstraint: "Base64 32-byte key (or use ACCOUNT_CIPHER_KEYS); dev fallback disabled outside development"},
		{Name: "ACCOUNT_CIPHER_KEYS", Type: "map[string]string", Default: `""`, Required: false, Secret: true, MainnetConstraint: `Comma-separated "version:base64key" pairs; version <= 32 chars`},
		{Name: "ACCOUNT_CIPHER_ACTIVE_KEY", Type: "string", Default: `""`, Required: false, Secret: false, MainnetConstraint: "Required when ACCOUNT_CIPHER_KEYS is set; must match a configured key version"},
		{Name: "ACCOUNT_CIPHER_FINGERPRINT_KEY", Type: "string", Default: `""`, Required: false, Secret: true, MainnetConstraint: "Required when ACCOUNT_CIPHER_KEYS does not include a v1 key"},
		{Name: "TX_POLLER_ENABLED", Type: "bool", Default: "true", Required: false, Secret: false, MainnetConstraint: "None"},
		{Name: "TX_POLLER_INTERVAL", Type: "duration", Default: "15s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "TX_POLLER_MIN_AGE", Type: "duration", Default: "30s", Required: false, Secret: false, MainnetConstraint: "Must be >= 0"},
		{Name: "RECONCILE_ENABLED", Type: "bool", Default: "true", Required: false, Secret: false, MainnetConstraint: "Must be true on mainnet"},
		{Name: "RECONCILE_INTERVAL", Type: "duration", Default: "5m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "RECONCILE_DRY_RUN", Type: "bool", Default: "false", Required: false, Secret: false, MainnetConstraint: "Must be false on mainnet"},
		{Name: "PROTOCOL_TVL_ANOMALY_DROP_PCT", Type: "float", Default: "10.0", Required: false, Secret: false, MainnetConstraint: "Must be in (0, 100]"},
		{Name: "RECURRING_DEPOSIT_ENABLED", Type: "bool", Default: "true", Required: false, Secret: false, MainnetConstraint: "None"},
		{Name: "RECURRING_DEPOSIT_INTERVAL", Type: "duration", Default: "1h0m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "MIN_DEPOSIT_AMOUNT", Type: "decimal", Default: `"0"`, Required: false, Secret: false, MainnetConstraint: "Non-negative decimal"},
		{Name: "HARVEST_ENGINE_ENABLED", Type: "bool", Default: "true", Required: false, Secret: false, MainnetConstraint: "None"},
		{Name: "HARVEST_ENGINE_INTERVAL", Type: "duration", Default: "1h0m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "HARVEST_ENGINE_WINDOW", Type: "duration", Default: "1h0m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "HARVEST_ENGINE_MARGIN", Type: "decimal", Default: `"0.10"`, Required: false, Secret: false, MainnetConstraint: "Positive decimal"},
		{Name: "HARVEST_ENGINE_GAS_FEE", Type: "decimal", Default: `"0.05"`, Required: false, Secret: false, MainnetConstraint: "Non-negative decimal"},
		{Name: "CANARY_ENABLED", Type: "bool", Default: "false", Required: false, Secret: false, MainnetConstraint: "Opt-in; requires dedicated CANARY_VAULT_ID on mainnet"},
		{Name: "CANARY_INTERVAL", Type: "duration", Default: "5m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "CANARY_VAULT_ID", Type: "uuid", Default: `"00000000-0000-0000-0000-000000000000"`, Required: false, Secret: false, MainnetConstraint: "Valid UUID when set"},
		{Name: "CANARY_AMOUNT", Type: "decimal", Default: `"0.01"`, Required: false, Secret: false, MainnetConstraint: "Positive decimal"},
		{Name: "CANARY_LATENCY_THRESHOLD", Type: "duration", Default: "1m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "JOB_QUEUE_ENABLED", Type: "bool", Default: "true", Required: false, Secret: false, MainnetConstraint: "None"},
		{Name: "JOB_QUEUE_POLL_INTERVAL", Type: "duration", Default: "1s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "JOB_QUEUE_LEASE", Type: "duration", Default: "30s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "JOB_QUEUE_HEARTBEAT_INTERVAL", Type: "duration", Default: "10s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "JOB_QUEUE_JOB_TIMEOUT", Type: "duration", Default: "2m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "JOB_QUEUE_DEFAULT_CONCURRENCY", Type: "int", Default: "4", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "JOB_QUEUE_BACKOFF_BASE", Type: "duration", Default: "2s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "JOB_QUEUE_BACKOFF_MAX", Type: "duration", Default: "5m0s", Required: false, Secret: false, MainnetConstraint: "Must be >= JOB_QUEUE_BACKOFF_BASE"},
		{Name: "JOB_QUEUE_STATS_INTERVAL", Type: "duration", Default: "30s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "JOB_QUEUE_DRAIN_TIMEOUT", Type: "duration", Default: "25s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "OUTBOX_RELAY_ENABLED", Type: "bool", Default: "true", Required: false, Secret: false, MainnetConstraint: "None"},
		{Name: "OUTBOX_RELAY_POLL_INTERVAL", Type: "duration", Default: "1s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "OUTBOX_RELAY_BATCH_SIZE", Type: "int", Default: "100", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "OUTBOX_RELAY_LEASE", Type: "duration", Default: "30s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "OUTBOX_RELAY_BACKOFF", Type: "duration", Default: "5s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "OUTBOX_RELAY_STATS_INTERVAL", Type: "duration", Default: "30s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "OUTBOX_RETENTION_INTERVAL", Type: "duration", Default: "24h0m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "OUTBOX_DISPATCHED_RETENTION", Type: "duration", Default: "168h0m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "OUTBOX_DEAD_RETENTION", Type: "duration", Default: "720h0m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "REBALANCER_ENABLED", Type: "bool", Default: "true", Required: false, Secret: false, MainnetConstraint: "Gated by scheduler leadership"},
		{Name: "REBALANCER_INTERVAL_MINUTES", Type: "int", Default: "15", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "REBALANCER_MIN_APY_GAIN_BPS", Type: "int", Default: "50", Required: false, Secret: false, MainnetConstraint: "Must be >= 0"},
		{Name: "REBALANCE_APY_THRESHOLD", Type: "int", Default: "200", Required: false, Secret: false, MainnetConstraint: "Must be >= 0"},
		{Name: "GOAL_NOTIFICATION_DIGEST_ENABLED", Type: "bool", Default: "true", Required: false, Secret: false, MainnetConstraint: "None"},
		{Name: "GOAL_NOTIFICATION_DIGEST_INTERVAL", Type: "duration", Default: "1h0m0s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "SCHEDULER_LEADER_LOCK_KEY", Type: "int", Default: "846000", Required: false, Secret: false, MainnetConstraint: "PostgreSQL advisory lock key shared across API replicas"},
		{Name: "SCHEDULER_LEADER_HEARTBEAT_INTERVAL", Type: "duration", Default: "3s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "METRICS_ENABLED", Type: "bool", Default: "true", Required: false, Secret: false, MainnetConstraint: "None"},
		{Name: "METRICS_ADDR", Type: "string", Default: `"127.0.0.1:9090"`, Required: false, Secret: false, MainnetConstraint: "Valid host:port; must not equal public SERVER_HOST:SERVER_PORT"},
		{Name: "INDEXER_STALENESS_BUDGET", Type: "duration", Default: "30s", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "CIRCUIT_BREAKER_ENABLED", Type: "bool", Default: "true", Required: false, Secret: false, MainnetConstraint: "Recommended true on mainnet"},
		{Name: "CIRCUIT_BREAKER_FAILURE_RATIO", Type: "float", Default: "0.5", Required: false, Secret: false, MainnetConstraint: "In (0, 1] when CIRCUIT_BREAKER_ENABLED is true"},
		{Name: "CIRCUIT_BREAKER_MIN_REQUESTS", Type: "int", Default: "10", Required: false, Secret: false, MainnetConstraint: "Must be > 0 when CIRCUIT_BREAKER_ENABLED is true"},
		{Name: "CIRCUIT_BREAKER_WINDOW", Type: "duration", Default: "30s", Required: false, Secret: false, MainnetConstraint: "Must be > 0 when CIRCUIT_BREAKER_ENABLED is true"},
		{Name: "CIRCUIT_BREAKER_OPEN_DURATION", Type: "duration", Default: "15s", Required: false, Secret: false, MainnetConstraint: "Must be > 0 when CIRCUIT_BREAKER_ENABLED is true"},
		{Name: "CIRCUIT_BREAKER_SOROBAN_RPC_FAILURE_RATIO", Type: "*float", Default: "unset", Required: false, Secret: false, MainnetConstraint: "Optional per-upstream override in (0, 1]"},
		{Name: "CIRCUIT_BREAKER_SOROBAN_RPC_MIN_REQUESTS", Type: "*int", Default: "unset", Required: false, Secret: false, MainnetConstraint: "Optional per-upstream override > 0"},
		{Name: "CIRCUIT_BREAKER_SOROBAN_RPC_WINDOW", Type: "*duration", Default: "unset", Required: false, Secret: false, MainnetConstraint: "Optional per-upstream override > 0"},
		{Name: "CIRCUIT_BREAKER_SOROBAN_RPC_OPEN_DURATION", Type: "*duration", Default: "unset", Required: false, Secret: false, MainnetConstraint: "Optional per-upstream override > 0"},
		{Name: "CIRCUIT_BREAKER_HORIZON_FAILURE_RATIO", Type: "*float", Default: "unset", Required: false, Secret: false, MainnetConstraint: "Optional per-upstream override in (0, 1]"},
		{Name: "CIRCUIT_BREAKER_HORIZON_MIN_REQUESTS", Type: "*int", Default: "unset", Required: false, Secret: false, MainnetConstraint: "Optional per-upstream override > 0"},
		{Name: "CIRCUIT_BREAKER_HORIZON_WINDOW", Type: "*duration", Default: "unset", Required: false, Secret: false, MainnetConstraint: "Optional per-upstream override > 0"},
		{Name: "CIRCUIT_BREAKER_HORIZON_OPEN_DURATION", Type: "*duration", Default: "unset", Required: false, Secret: false, MainnetConstraint: "Optional per-upstream override > 0"},
		{Name: "RPC_RETRY_MAX_ATTEMPTS", Type: "int", Default: "3", Required: false, Secret: false, MainnetConstraint: "Must be >= 1"},
		{Name: "RPC_RETRY_BASE_DELAY", Type: "duration", Default: "100ms", Required: false, Secret: false, MainnetConstraint: "Must be > 0"},
		{Name: "RPC_RETRY_MAX_DELAY", Type: "duration", Default: "1s", Required: false, Secret: false, MainnetConstraint: "Must be >= RPC_RETRY_BASE_DELAY"},
		{Name: "RPC_RETRY_BUDGET", Type: "duration", Default: "2s", Required: false, Secret: false, MainnetConstraint: "Must be >= RPC_RETRY_MAX_DELAY"},
	}
}

// GenerateMarkdownReference renders the Markdown reference table for docs/api-config.md.
func GenerateMarkdownReference() string {
	var b strings.Builder
	b.WriteString("# API Environment Variable Reference\n\n")
	b.WriteString("> **Auto-generated — DO NOT EDIT MANUALLY.**\n")
	b.WriteString("> Regenerate from `apps/api` with: `go run ./cmd/config-doc > ../../docs/api-config.md`\n\n")
	b.WriteString("Startup logs a single structured configuration summary (`startup configuration loaded`) at `INFO` with all secret values masked as `[REDACTED]` (or `\"\"` when unset) via `Config.LogValue()` in `apps/api/internal/config/redact.go`.\n\n")
	b.WriteString("| Variable | Type | Default | Required | Secret | Mainnet Constraint |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- |\n")

	for _, spec := range EnvSpecs() {
		reqStr := "false"
		if spec.Required {
			reqStr = "**true**"
		}
		secStr := "false"
		if spec.Secret {
			secStr = "**true**"
		}
		fmt.Fprintf(
			&b,
			"| `%s` | `%s` | `%s` | %s | %s | %s |\n",
			spec.Name,
			spec.Type,
			strings.ReplaceAll(spec.Default, "`", ""),
			reqStr,
			secStr,
			spec.MainnetConstraint,
		)
	}
	return b.String()
}
