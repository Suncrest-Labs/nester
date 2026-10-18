# API Environment Variable Reference

> **Auto-generated — DO NOT EDIT MANUALLY.**
> Regenerate from `apps/api` with: `go run ./cmd/config-doc > ../../docs/api-config.md`

Startup logs a single structured configuration summary (`startup configuration loaded`) at `INFO` with all secret values masked as `[REDACTED]` (or `""` when unset) via `Config.LogValue()` in `apps/api/internal/config/redact.go`.

| Variable | Type | Default | Required | Secret | Mainnet Constraint |
| --- | --- | --- | --- | --- | --- |
| `APP_ENV` | `string` | `"development"` | false | false | Must be "production" on mainnet (allowed: development, staging, production, test) |
| `SERVER_HOST` | `string` | `"0.0.0.0"` | false | false | Non-empty bind address |
| `SERVER_PORT` | `int` | `8080` | false | false | Between 1 and 65535 |
| `SERVER_READ_TIMEOUT` | `duration` | `15s` | false | false | Must be > 0 |
| `SERVER_READ_HEADER_TIMEOUT` | `duration` | `10s` | false | false | Must be > 0 |
| `SERVER_WRITE_TIMEOUT` | `duration` | `15s` | false | false | Must be > 0 |
| `SERVER_IDLE_TIMEOUT` | `duration` | `1m0s` | false | false | Must be > 0 |
| `SERVER_SHUTDOWN_TIMEOUT` | `duration` | `20s` | false | false | Must be > 0 |
| `SERVER_MAX_HEADER_BYTES` | `int` | `1048576` | false | false | Must be > 0 |
| `DATABASE_DSN` | `string` | `—` | **true** | **true** | Required PostgreSQL DSN; TLS (sslmode=require/verify-full) mandatory on mainnet |
| `DATABASE_POOL_SIZE` | `int` | `25` | false | false | Between 1 and 10000 |
| `DATABASE_CONNECTION_TIMEOUT` | `duration` | `5s` | false | false | Must be > 0 |
| `STELLAR_NETWORK_PASSPHRASE` | `string` | `—` | **true** | false | Must equal "Public Global Stellar Network ; September 2015" |
| `STELLAR_RPC_URL` | `url` | `—` | **true** | false | Valid absolute URL; rejected if hostname contains testnet or futurenet when passphrase is mainnet |
| `STELLAR_HORIZON_URL` | `url` | `—` | **true** | false | Valid absolute URL; rejected if hostname contains testnet or futurenet when passphrase is mainnet |
| `STELLAR_OPERATOR_SECRET` | `string` | `""` | false | **true** | Leave empty on mainnet when SIGNER_SOCKET_PATH delegates signing to isolated signer |
| `STELLAR_OPERATOR_FUNDED_DEPOSITS_ENABLED` | `bool` | `false` | false | false | Must remain false on mainnet unless explicitly allowlisted |
| `STELLAR_OPERATOR_FUNDED_DEPOSIT_VAULTS` | `string` | `""` | false | false | Comma-separated vault ID allowlist when operator-funded deposits are enabled |
| `STELLAR_OPERATOR_FUNDED_DEPOSIT_MAX_AMOUNT` | `decimal` | `"0"` | false | false | Non-negative decimal cap per operator-funded deposit |
| `STELLAR_OPERATOR_ADDRESS` | `string` | `""` | false | false | Operator G-address required when SIGNER_SOCKET_PATH is set |
| `SIGNER_SOCKET_PATH` | `string` | `""` | false | false | Unix domain socket path to isolated signer process on mainnet |
| `STELLAR_USDC_ISSUER` | `string` | `"GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"` | false | false | Mainnet Circle USDC issuer G-address |
| `YIELD_REGISTRY_CONTRACT` | `string` | `""` | false | false | Required when STELLAR_NETWORK_PASSPHRASE is the mainnet passphrase |
| `STELLAR_ALLOCATION_STRATEGY_ADDRESS` | `string` | `""` | false | false | Required when STELLAR_NETWORK_PASSPHRASE is the mainnet passphrase |
| `WITHDRAWAL_SLIPPAGE_BPS` | `int` | `50` | false | false | Between 1 and 300 |
| `HARVEST_DEFAULT_COMPOUND` | `bool` | `true` | false | false | None |
| `STELLAR_MAINNET_VAULT_TVL_CAP` | `decimal` | `"0"` | false | false | Enforced on mainnet; Stage 1 rollout sets 10000 USDC |
| `WITHDRAWAL_BREAKER_ENABLED` | `bool` | `false` | false | false | Recommended true on mainnet to halt anomalous vault outflows |
| `WITHDRAWAL_BREAKER_THRESHOLD_PERCENT` | `decimal` | `"25"` | false | false | Rolling-window outflow percentage of vault TVL |
| `WITHDRAWAL_BREAKER_WINDOW` | `duration` | `1h0m0s` | false | false | Must be > 0 |
| `MIN_ALLOCATION_WEIGHT` | `int` | `5` | false | false | Between 1 and 100 |
| `REDIS_ADDR` | `string` | `""` | false | false | Required for shared rate-limiting and auth challenge state across mainnet replicas |
| `TRACING_ENABLED` | `bool` | `false` | false | false | When true in staging/production, OTEL_EXPORTER_OTLP_INSECURE must be false |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `string` | `"localhost:4317"` | false | false | Required when TRACING_ENABLED is true |
| `OTEL_EXPORTER_OTLP_INSECURE` | `bool` | `true` | false | false | Must be false when TRACING_ENABLED is true outside development |
| `OTEL_SERVICE_NAME` | `string` | `"nester-api"` | false | false | Required when TRACING_ENABLED is true |
| `OTEL_EXPORTER_TIMEOUT` | `duration` | `10s` | false | false | Must be > 0 when TRACING_ENABLED is true |
| `TRACING_SAMPLE_RATIO` | `float` | `0.05` | false | false | Between 0 and 1 |
| `TRACING_LATENCY_THRESHOLD` | `duration` | `1s` | false | false | Must be >= 0 |
| `AUTH_JWT_SECRET` | `string` | `—` | **true** | **true** | At least 32 chars and >= 8 distinct bytes; dev default rejected in staging/production |
| `NESTER_SERVICE_API_KEY` | `string` | `""` | false | **true** | If set: >= 32 chars, >= 8 distinct bytes, and must not equal AUTH_JWT_SECRET |
| `AUTH_ACCESS_TOKEN_EXPIRY` | `duration` | `5m0s` | false | false | Must be > 0 and < AUTH_REFRESH_TOKEN_EXPIRY |
| `AUTH_REFRESH_TOKEN_EXPIRY` | `duration` | `168h0m0s` | false | false | Must be > AUTH_ACCESS_TOKEN_EXPIRY and < AUTH_ABSOLUTE_SESSION_LIFETIME |
| `AUTH_ABSOLUTE_SESSION_LIFETIME` | `duration` | `720h0m0s` | false | false | Must be > AUTH_REFRESH_TOKEN_EXPIRY |
| `AUTH_CHALLENGE_EXPIRY` | `duration` | `5m0s` | false | false | Must be > 0 |
| `RATELIMIT_GLOBAL_LIMIT` | `int` | `100` | false | false | Must be > 0 |
| `RATELIMIT_GLOBAL_WINDOW` | `duration` | `1m0s` | false | false | Must be >= 1ms |
| `RATELIMIT_WRITE_LIMIT` | `int` | `20` | false | false | Must be > 0 |
| `RATELIMIT_WRITE_WINDOW` | `duration` | `1m0s` | false | false | Must be > 0 |
| `RATELIMIT_WALLET_LIMIT` | `int` | `60` | false | false | Must be > 0 |
| `RATELIMIT_WALLET_WINDOW` | `duration` | `1m0s` | false | false | Must be > 0 |
| `RATELIMIT_REBALANCE_LIMIT` | `int` | `3` | false | false | Must be > 0 |
| `RATELIMIT_REBALANCE_WINDOW` | `duration` | `1h0m0s` | false | false | Must be > 0 |
| `RATELIMIT_AUTH_LIMIT` | `int` | `10` | false | false | Must be > 0 |
| `RATELIMIT_AUTH_WINDOW` | `duration` | `1m0s` | false | false | Must be >= 1ms |
| `RATELIMIT_APIKEY_LIMIT` | `int` | `300` | false | false | Must be > 0 |
| `RATELIMIT_APIKEY_WINDOW` | `duration` | `1m0s` | false | false | Must be >= 1ms |
| `AUTH_FAILURE_THRESHOLD` | `int` | `5` | false | false | Must be > 0 |
| `AUTH_FAILURE_WINDOW` | `duration` | `15m0s` | false | false | Must be > 0 |
| `AUTH_LOCKOUT_BASE` | `duration` | `30s` | false | false | Must be > 0 |
| `AUTH_LOCKOUT_MAX` | `duration` | `15m0s` | false | false | Must be >= AUTH_LOCKOUT_BASE |
| `RATELIMIT_TRUSTED_PROXY_COUNT` | `int` | `0` | false | false | Must be >= 0; set to ingress proxy hop count on mainnet |
| `RATELIMIT_QUOTA_ENABLED` | `bool` | `true` | false | false | Must remain true on mainnet |
| `RATELIMIT_QUOTA_LIMIT` | `int` | `300` | false | false | Must be > 0 when RATELIMIT_QUOTA_ENABLED is true |
| `RATELIMIT_QUOTA_WINDOW` | `duration` | `1m0s` | false | false | Must be >= 1ms when RATELIMIT_QUOTA_ENABLED is true |
| `RATELIMIT_QUOTA_BYPASS_TOKEN` | `string` | `""` | false | **true** | Leave empty on mainnet |
| `LOG_LEVEL` | `string` | `"info"` | false | false | One of debug, info, warn, error |
| `LOG_FORMAT` | `string` | `"text" (dev) / "json" (staging, prod)` | false | false | One of json, text (defaults to json in production) |
| `ALLOWED_ORIGINS` | `[]string` | `""` | false | false | Required in staging/production; explicit scheme://host[:port] origins only (no "*") |
| `PERFORMANCE_SNAPSHOT_INTERVAL` | `duration` | `1h0m0s` | false | false | Must be > 0 |
| `TVL_REFRESH_INTERVAL` | `duration` | `15m0s` | false | false | Must be > 0 |
| `LAUNCH_PER_USER_DEPOSIT_CAP` | `decimal` | `""` | false | false | Non-negative decimal or empty/0 |
| `LAUNCH_GLOBAL_TVL_CAP` | `decimal` | `""` | false | false | Non-negative decimal or empty/0 |
| `LAUNCH_CAP_WARN_THRESHOLDS_PCT` | `[]int` | `"80,90"` | false | false | Strictly increasing integers in 1..100 |
| `APY_REFRESH_INTERVAL` | `duration` | `5m0s` | false | false | Must be > 0 |
| `APY_BROADCAST_THRESHOLD` | `int` | `50` | false | false | Must be >= 0 |
| `RUN_MIGRATIONS` | `bool` | `false` | false | false | Run via pre-deploy job on mainnet; keep false on serving pods |
| `MIGRATIONS_DIR` | `string` | `"./migrations"` | false | false | Non-empty path |
| `STARTUP_DEPENDENCY_TIMEOUT` | `duration` | `5s` | false | false | Must be > 0 |
| `BANK_ACCOUNT_ENCRYPTION_KEY` | `string` | `""` | false | **true** | Base64 32-byte key (or use ACCOUNT_CIPHER_KEYS); dev fallback disabled outside development |
| `ACCOUNT_CIPHER_KEYS` | `map[string]string` | `""` | false | **true** | Comma-separated "version:base64key" pairs; version <= 32 chars |
| `ACCOUNT_CIPHER_ACTIVE_KEY` | `string` | `""` | false | false | Required when ACCOUNT_CIPHER_KEYS is set; must match a configured key version |
| `ACCOUNT_CIPHER_FINGERPRINT_KEY` | `string` | `""` | false | **true** | Required when ACCOUNT_CIPHER_KEYS does not include a v1 key |
| `TX_POLLER_ENABLED` | `bool` | `true` | false | false | None |
| `TX_POLLER_INTERVAL` | `duration` | `15s` | false | false | Must be > 0 |
| `TX_POLLER_MIN_AGE` | `duration` | `30s` | false | false | Must be >= 0 |
| `RECONCILE_ENABLED` | `bool` | `true` | false | false | Must be true on mainnet |
| `RECONCILE_INTERVAL` | `duration` | `5m0s` | false | false | Must be > 0 |
| `RECONCILE_DRY_RUN` | `bool` | `false` | false | false | Must be false on mainnet |
| `PROTOCOL_TVL_ANOMALY_DROP_PCT` | `float` | `10.0` | false | false | Must be in (0, 100] |
| `RECURRING_DEPOSIT_ENABLED` | `bool` | `true` | false | false | None |
| `RECURRING_DEPOSIT_INTERVAL` | `duration` | `1h0m0s` | false | false | Must be > 0 |
| `MIN_DEPOSIT_AMOUNT` | `decimal` | `"0"` | false | false | Non-negative decimal |
| `HARVEST_ENGINE_ENABLED` | `bool` | `true` | false | false | None |
| `HARVEST_ENGINE_INTERVAL` | `duration` | `1h0m0s` | false | false | Must be > 0 |
| `HARVEST_ENGINE_WINDOW` | `duration` | `1h0m0s` | false | false | Must be > 0 |
| `HARVEST_ENGINE_MARGIN` | `decimal` | `"0.10"` | false | false | Positive decimal |
| `HARVEST_ENGINE_GAS_FEE` | `decimal` | `"0.05"` | false | false | Non-negative decimal |
| `CANARY_ENABLED` | `bool` | `false` | false | false | Opt-in; requires dedicated CANARY_VAULT_ID on mainnet |
| `CANARY_INTERVAL` | `duration` | `5m0s` | false | false | Must be > 0 |
| `CANARY_VAULT_ID` | `uuid` | `"00000000-0000-0000-0000-000000000000"` | false | false | Valid UUID when set |
| `CANARY_AMOUNT` | `decimal` | `"0.01"` | false | false | Positive decimal |
| `CANARY_LATENCY_THRESHOLD` | `duration` | `1m0s` | false | false | Must be > 0 |
| `JOB_QUEUE_ENABLED` | `bool` | `true` | false | false | None |
| `JOB_QUEUE_POLL_INTERVAL` | `duration` | `1s` | false | false | Must be > 0 |
| `JOB_QUEUE_LEASE` | `duration` | `30s` | false | false | Must be > 0 |
| `JOB_QUEUE_HEARTBEAT_INTERVAL` | `duration` | `10s` | false | false | Must be > 0 |
| `JOB_QUEUE_JOB_TIMEOUT` | `duration` | `2m0s` | false | false | Must be > 0 |
| `JOB_QUEUE_DEFAULT_CONCURRENCY` | `int` | `4` | false | false | Must be > 0 |
| `JOB_QUEUE_BACKOFF_BASE` | `duration` | `2s` | false | false | Must be > 0 |
| `JOB_QUEUE_BACKOFF_MAX` | `duration` | `5m0s` | false | false | Must be >= JOB_QUEUE_BACKOFF_BASE |
| `JOB_QUEUE_STATS_INTERVAL` | `duration` | `30s` | false | false | Must be > 0 |
| `JOB_QUEUE_DRAIN_TIMEOUT` | `duration` | `25s` | false | false | Must be > 0 |
| `OUTBOX_RELAY_ENABLED` | `bool` | `true` | false | false | None |
| `OUTBOX_RELAY_POLL_INTERVAL` | `duration` | `1s` | false | false | Must be > 0 |
| `OUTBOX_RELAY_BATCH_SIZE` | `int` | `100` | false | false | Must be > 0 |
| `OUTBOX_RELAY_LEASE` | `duration` | `30s` | false | false | Must be > 0 |
| `OUTBOX_RELAY_BACKOFF` | `duration` | `5s` | false | false | Must be > 0 |
| `OUTBOX_RELAY_STATS_INTERVAL` | `duration` | `30s` | false | false | Must be > 0 |
| `OUTBOX_RETENTION_INTERVAL` | `duration` | `24h0m0s` | false | false | Must be > 0 |
| `OUTBOX_DISPATCHED_RETENTION` | `duration` | `168h0m0s` | false | false | Must be > 0 |
| `OUTBOX_DEAD_RETENTION` | `duration` | `720h0m0s` | false | false | Must be > 0 |
| `REBALANCER_ENABLED` | `bool` | `true` | false | false | Gated by scheduler leadership |
| `REBALANCER_INTERVAL_MINUTES` | `int` | `15` | false | false | Must be > 0 |
| `REBALANCER_MIN_APY_GAIN_BPS` | `int` | `50` | false | false | Must be >= 0 |
| `REBALANCE_APY_THRESHOLD` | `int` | `200` | false | false | Must be >= 0 |
| `GOAL_NOTIFICATION_DIGEST_ENABLED` | `bool` | `true` | false | false | None |
| `GOAL_NOTIFICATION_DIGEST_INTERVAL` | `duration` | `1h0m0s` | false | false | Must be > 0 |
| `SCHEDULER_LEADER_LOCK_KEY` | `int` | `846000` | false | false | PostgreSQL advisory lock key shared across API replicas |
| `SCHEDULER_LEADER_HEARTBEAT_INTERVAL` | `duration` | `3s` | false | false | Must be > 0 |
| `METRICS_ENABLED` | `bool` | `true` | false | false | None |
| `METRICS_ADDR` | `string` | `"127.0.0.1:9090"` | false | false | Valid host:port; must not equal public SERVER_HOST:SERVER_PORT |
| `INDEXER_STALENESS_BUDGET` | `duration` | `30s` | false | false | Must be > 0 |
| `CIRCUIT_BREAKER_ENABLED` | `bool` | `true` | false | false | Recommended true on mainnet |
| `CIRCUIT_BREAKER_FAILURE_RATIO` | `float` | `0.5` | false | false | In (0, 1] when CIRCUIT_BREAKER_ENABLED is true |
| `CIRCUIT_BREAKER_MIN_REQUESTS` | `int` | `10` | false | false | Must be > 0 when CIRCUIT_BREAKER_ENABLED is true |
| `CIRCUIT_BREAKER_WINDOW` | `duration` | `30s` | false | false | Must be > 0 when CIRCUIT_BREAKER_ENABLED is true |
| `CIRCUIT_BREAKER_OPEN_DURATION` | `duration` | `15s` | false | false | Must be > 0 when CIRCUIT_BREAKER_ENABLED is true |
| `CIRCUIT_BREAKER_SOROBAN_RPC_FAILURE_RATIO` | `*float` | `unset` | false | false | Optional per-upstream override in (0, 1] |
| `CIRCUIT_BREAKER_SOROBAN_RPC_MIN_REQUESTS` | `*int` | `unset` | false | false | Optional per-upstream override > 0 |
| `CIRCUIT_BREAKER_SOROBAN_RPC_WINDOW` | `*duration` | `unset` | false | false | Optional per-upstream override > 0 |
| `CIRCUIT_BREAKER_SOROBAN_RPC_OPEN_DURATION` | `*duration` | `unset` | false | false | Optional per-upstream override > 0 |
| `CIRCUIT_BREAKER_HORIZON_FAILURE_RATIO` | `*float` | `unset` | false | false | Optional per-upstream override in (0, 1] |
| `CIRCUIT_BREAKER_HORIZON_MIN_REQUESTS` | `*int` | `unset` | false | false | Optional per-upstream override > 0 |
| `CIRCUIT_BREAKER_HORIZON_WINDOW` | `*duration` | `unset` | false | false | Optional per-upstream override > 0 |
| `CIRCUIT_BREAKER_HORIZON_OPEN_DURATION` | `*duration` | `unset` | false | false | Optional per-upstream override > 0 |
| `RPC_RETRY_MAX_ATTEMPTS` | `int` | `3` | false | false | Must be >= 1 |
| `RPC_RETRY_BASE_DELAY` | `duration` | `100ms` | false | false | Must be > 0 |
| `RPC_RETRY_MAX_DELAY` | `duration` | `1s` | false | false | Must be >= RPC_RETRY_BASE_DELAY |
| `RPC_RETRY_BUDGET` | `duration` | `2s` | false | false | Must be >= RPC_RETRY_MAX_DELAY |
