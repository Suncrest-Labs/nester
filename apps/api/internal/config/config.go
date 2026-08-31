package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/suncrestlabs/nester/apps/api/internal/breaker"
	"github.com/suncrestlabs/nester/apps/api/internal/freshness"
	"github.com/suncrestlabs/nester/apps/api/internal/retry"
)

// defaultDevJWTSecret is the placeholder value shipped in .env.example. It is long
// enough to pass the length check, so it is rejected explicitly outside development.
// This is a deny-list entry, not a credential: config validation rejects
// startup when AUTH_JWT_SECRET equals it outside development, so its presence
// in source is what makes the check possible (nester#1035, G101).
const defaultDevJWTSecret = "dev-nester-jwt-secret-change-in-production" // #nosec G101 -- known-bad placeholder that startup validation refuses, not a real secret

// maxKeyVersionLen bounds an account cipher key version label so it fits the
// bank_accounts.key_version VARCHAR(32) column.
const maxKeyVersionLen = 32

// maxDatabasePoolSize bounds DATABASE_POOL_SIZE. The value is narrowed to
// int32 for pgxpool's MaxConns, so it must stay well inside int32 range on
// every architecture; the limit is far above any workable pool size, so it
// only rejects misconfiguration.
const maxDatabasePoolSize = 10000

type Config struct {
	environment           string
	server                ServerConfig
	database              DatabaseConfig
	stellar               StellarConfig
	intelligence          IntelligenceConfig
	allocation            AllocationConfig
	redis                 RedisConfig
	settlementProviderURL string
	auth                  AuthConfig
	rateLimit             RateLimitConfig
	log                   LogConfig
	allowedOrigins        []string
	performance           PerformanceConfig
	tvl                   TVLConfig
	apyRefresh            APYRefreshConfig
	startup               StartupConfig
	bank                  BankConfig
	bankAccountCipherKey  string
	accountCipher         AccountCipherConfig
	transactionPoller     TransactionPollerConfig
	reconciliation        ReconciliationConfig
	recurringDeposit      RecurringDepositConfig
	jobQueue              JobQueueConfig
	outbox                OutboxConfig
	harvest               HarvestConfig
	rebalancer            RebalancerConfig
	schedulerLeadership   SchedulerLeadershipConfig
	tracing               TracingConfig
	metrics               MetricsConfig
	indexer               IndexerConfig
	circuitBreaker        CircuitBreakerConfig
	rpcRetry              RPCRetryConfig
}

// CircuitBreakerConfig is the policy protecting the chain upstreams, Soroban
// RPC and Horizon (nester#1087).
//
// One policy, two independent breakers. The thresholds are shared because both
// upstreams degrade the same way and there is no evidence for different
// numbers; the failure *state* is strictly separate, so a Horizon outage never
// sheds Soroban traffic. See docs/observability/circuit-breakers.md.
type CircuitBreakerConfig struct {
	enabled      bool
	failureRatio float64
	minRequests  int
	window       time.Duration
	openDuration time.Duration
}

// RPCRetryConfig is the bounded, jittered retry policy shared by every Soroban
// RPC call site (nester#1086).
//
// It applies only to idempotent reads. Writes are never retried here — a
// resubmitted transaction is a second attempt to move real money — and go
// through the submission record instead.
type RPCRetryConfig struct {
	maxAttempts int
	baseDelay   time.Duration
	maxDelay    time.Duration
	budget      time.Duration
}

// IndexerConfig holds the event indexer's freshness contract (nester#1088).
//
// The staleness budget is a single number with three consumers — the
// `nester_indexer_staleness_budget_seconds` metric the alert compares against,
// the `X-Indexer-Stale` header the API returns, and the SLO documentation —
// so that the pager, the UI, and the runbook can never disagree about whether
// balances are current.
type IndexerConfig struct {
	stalenessBudget time.Duration
}

// TracingConfig holds the OpenTelemetry tracing settings (nester#1054).
//
// Tracing is opt-in: with TRACING_ENABLED unset the tracer provider is a no-op
// and no exporter connection is attempted, so the application starts and
// serves normally without a collector present.
type TracingConfig struct {
	enabled          bool
	otlpEndpoint     string
	otlpInsecure     bool
	serviceName      string
	exporterTimeout  time.Duration
	sampleRatio      float64
	latencyThreshold time.Duration
}

// MetricsConfig controls the Prometheus exposition endpoint.
//
// The endpoint runs on its own listener, never on the public API router, so
// that scrape traffic and the internal route names it exposes stay off the
// public interface. See internal/metrics/server.go for the reasoning.
type MetricsConfig struct {
	enabled bool
	addr    string
}

// AccountCipherConfig holds the versioned key set used to encrypt sensitive
// account numbers at rest. It supports non-destructive rotation: one active key
// seals new writes while every configured version remains available to decrypt
// historical rows.
type AccountCipherConfig struct {
	activeVersion  string
	keys           map[string]string
	fingerprintKey string
}

// Configured reports whether at least one encryption key is available.
func (a AccountCipherConfig) Configured() bool {
	return len(a.keys) > 0
}

// ActiveVersion is the key version used for new encryptions.
func (a AccountCipherConfig) ActiveVersion() string {
	return a.activeVersion
}

// Keys returns a copy of the version -> base64 key map.
func (a AccountCipherConfig) Keys() map[string]string {
	out := make(map[string]string, len(a.keys))
	for k, v := range a.keys {
		out[k] = v
	}
	return out
}

// FingerprintKey is the optional stable pepper (base64) for uniqueness
// fingerprints. An empty value lets the cipher default to the legacy key.
func (a AccountCipherConfig) FingerprintKey() string {
	return a.fingerprintKey
}

// TransactionPollerConfig governs the background loop that reconciles pending
// transactions against Horizon (see internal/service.TransactionPoller).
type TransactionPollerConfig struct {
	enabled  bool
	interval time.Duration
	minAge   time.Duration
}

// ReconciliationConfig governs the scheduled vault-balance reconciliation job
// (nester#1082, see internal/reconciliation.Runner): it reads authoritative
// balances from the vault contract and compares them to the database,
// recording — never correcting — any divergence.
type ReconciliationConfig struct {
	enabled  bool
	interval time.Duration
	dryRun   bool
}

// StartupConfig governs one-shot work performed before the server begins
// accepting traffic (migrations, dependency reachability checks).
type StartupConfig struct {
	enableAutoMigrate bool
	migrationsDir     string
	dependencyTimeout time.Duration
}

type PerformanceConfig struct {
	snapshotInterval time.Duration
}

// TVLConfig governs the background TVL snapshot worker.
type TVLConfig struct {
	refreshInterval time.Duration
}

// APYRefreshConfig governs polling yield_registry for on-chain APY updates.
type APYRefreshConfig struct {
	refreshInterval       time.Duration
	broadcastThresholdBPS int
}

type ServerConfig struct {
	host              string
	port              int
	readTimeout       time.Duration
	readHeaderTimeout time.Duration
	writeTimeout      time.Duration
	idleTimeout       time.Duration
	gracefulShutdown  time.Duration
	maxHeaderBytes    int
}

type DatabaseConfig struct {
	dsn               string
	poolSize          int
	connectionTimeout time.Duration
}

type StellarConfig struct {
	networkPassphrase string
	rpcURL            string
	horizonURL        string
	operatorSecret    string
	// operatorAddress is the operator's PUBLIC address. It is required when
	// signing is delegated to the isolated signer, because the API still builds
	// transactions against the operator's source account but holds no key.
	operatorAddress string
	// signerSocketPath, when set, routes signing to the isolated signer process
	// over a Unix domain socket instead of holding the key in this process.
	signerSocketPath          string
	stellarUSDCIssuer         string
	yieldRegistryContract     string
	allocationStrategyAddress string
	withdrawalSlippageBps     int
	harvestDefaultCompound    bool
	// operatorFundedDepositsEnabled allows the API to fund deposits from the
	// shared operator account when the user did not sign one themselves.
	// Off by default: the supported path is a wallet-signed deposit, and
	// leaving this on makes the deposit endpoint a value transfer bounded
	// only by the operator balance (nester#1152).
	operatorFundedDepositsEnabled bool
	// operatorFundedDepositVaults is the comma-separated allowlist of vault
	// IDs permitted to use operator funds. Empty means none.
	operatorFundedDepositVaults string
	// operatorFundedDepositMaxAmount caps a single operator-funded deposit.
	operatorFundedDepositMaxAmount string
}

type AllocationConfig struct {
	minWeightPercent int
}

type IntelligenceConfig struct {
	baseURL       string
	serviceAPIKey string
	timeout       time.Duration
}

type AuthConfig struct {
	secret                  string
	serviceAPIKey           string
	accessTokenExpiry       time.Duration
	refreshTokenExpiry      time.Duration
	absoluteSessionLifetime time.Duration
	challengeExpiry         time.Duration
}

type RateLimitConfig struct {
	globalLimit     int
	globalWindow    time.Duration
	writeLimit      int
	writeWindow     time.Duration
	walletLimit     int
	walletWindow    time.Duration
	rebalanceLimit  int
	rebalanceWindow time.Duration
	authLimit       int
	authWindow      time.Duration
	// Auth-failure lockout (nester#1104). Distinct from authLimit/authWindow,
	// which bound request *rate*; these bound repeated *failures* and escalate
	// a backoff the attacker cannot outrun by slowing down.
	authFailureThreshold int
	authFailureWindow    time.Duration
	authLockoutBase      time.Duration
	authLockoutMax       time.Duration
	settlementLimit      int
	settlementWindow     time.Duration
	trustedProxyCount    int

	// Cost-weighted quota (see middleware.CostQuota). This meters downstream
	// work per user rather than request count, so an expensive route can be
	// bounded without throttling ordinary browsing.
	quotaEnabled     bool
	quotaLimit       int
	quotaWindow      time.Duration
	quotaBypassToken string
}

type LogConfig struct {
	level  string
	format string
}

type RedisConfig struct {
	addr string
}

type BankConfig struct {
	paystackKey    string
	flutterwaveKey string
}

func Load() (*Config, error) {
	fileValues, err := loadDotEnvFile(".env")
	if err != nil {
		return nil, err
	}

	loader := envLoader{
		fileValues: fileValues,
		errors:     make([]string, 0),
	}

	environment := loader.stringDefault("APP_ENV", "development")
	if !isOneOf(environment, "development", "staging", "production", "test") {
		loader.addError("APP_ENV must be one of development, staging, production, test")
	}

	cfg := &Config{
		environment: environment,
		server: ServerConfig{
			host:              loader.stringDefault("SERVER_HOST", "0.0.0.0"),
			port:              loader.intDefault("SERVER_PORT", 8080),
			readTimeout:       loader.durationDefault("SERVER_READ_TIMEOUT", 15*time.Second),
			readHeaderTimeout: loader.durationDefault("SERVER_READ_HEADER_TIMEOUT", 10*time.Second),
			writeTimeout:      loader.durationDefault("SERVER_WRITE_TIMEOUT", 15*time.Second),
			idleTimeout:       loader.durationDefault("SERVER_IDLE_TIMEOUT", 60*time.Second),
			gracefulShutdown:  loader.durationDefault("SERVER_SHUTDOWN_TIMEOUT", 20*time.Second),
			maxHeaderBytes:    loader.intDefault("SERVER_MAX_HEADER_BYTES", 1<<20),
		},
		database: DatabaseConfig{
			dsn:               loader.requiredString("DATABASE_DSN"),
			poolSize:          loader.intDefault("DATABASE_POOL_SIZE", 25),
			connectionTimeout: loader.durationDefault("DATABASE_CONNECTION_TIMEOUT", 5*time.Second),
		},
		stellar: StellarConfig{
			networkPassphrase:              loader.requiredString("STELLAR_NETWORK_PASSPHRASE"),
			rpcURL:                         loader.requiredURL("STELLAR_RPC_URL"),
			horizonURL:                     loader.requiredURL("STELLAR_HORIZON_URL"),
			operatorSecret:                 loader.stringDefault("STELLAR_OPERATOR_SECRET", ""),
			operatorFundedDepositsEnabled:  loader.boolDefault("STELLAR_OPERATOR_FUNDED_DEPOSITS_ENABLED", false),
			operatorFundedDepositVaults:    loader.stringDefault("STELLAR_OPERATOR_FUNDED_DEPOSIT_VAULTS", ""),
			operatorFundedDepositMaxAmount: loader.stringDefault("STELLAR_OPERATOR_FUNDED_DEPOSIT_MAX_AMOUNT", "0"),
			operatorAddress:                loader.stringDefault("STELLAR_OPERATOR_ADDRESS", ""),
			signerSocketPath:               loader.stringDefault("SIGNER_SOCKET_PATH", ""),
			stellarUSDCIssuer:              loader.stringDefault("STELLAR_USDC_ISSUER", "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"),
			yieldRegistryContract:          loader.stringDefault("YIELD_REGISTRY_CONTRACT", ""),
			allocationStrategyAddress:      loader.stringDefault("STELLAR_ALLOCATION_STRATEGY_ADDRESS", ""),
			withdrawalSlippageBps:          loader.intDefault("WITHDRAWAL_SLIPPAGE_BPS", 50),
			harvestDefaultCompound:         loader.boolDefault("HARVEST_DEFAULT_COMPOUND", true),
		},
		intelligence: IntelligenceConfig{
			baseURL:       loader.stringDefault("INTELLIGENCE_BASE_URL", loader.stringDefault("INTELLIGENCE_SERVICE_URL", "http://localhost:8000")),
			serviceAPIKey: loader.stringDefault("INTELLIGENCE_SERVICE_API_KEY", ""),
			timeout:       loader.durationDefault("INTELLIGENCE_TIMEOUT", loader.durationDefault("INTELLIGENCE_SERVICE_TIMEOUT", 10*time.Second)),
		},
		allocation: AllocationConfig{
			minWeightPercent: loader.intDefault("MIN_ALLOCATION_WEIGHT", 5),
		},
		redis: RedisConfig{
			addr: loader.stringDefault("REDIS_ADDR", ""),
		},
		tracing: TracingConfig{
			enabled:          loader.boolDefault("TRACING_ENABLED", false),
			otlpEndpoint:     loader.stringDefault("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
			otlpInsecure:     loader.boolDefault("OTEL_EXPORTER_OTLP_INSECURE", true),
			serviceName:      loader.stringDefault("OTEL_SERVICE_NAME", "nester-api"),
			exporterTimeout:  loader.durationDefault("OTEL_EXPORTER_TIMEOUT", 10*time.Second),
			sampleRatio:      loader.floatDefault("TRACING_SAMPLE_RATIO", 0.05),
			latencyThreshold: loader.durationDefault("TRACING_LATENCY_THRESHOLD", 1*time.Second),
		},
		settlementProviderURL: loader.stringDefault("SETTLEMENT_PROVIDER_URL", ""),
		auth: AuthConfig{
			secret:                  loader.requiredString("AUTH_JWT_SECRET"),
			serviceAPIKey:           loader.stringDefault("NESTER_SERVICE_API_KEY", ""),
			accessTokenExpiry:       loader.durationDefault("AUTH_ACCESS_TOKEN_EXPIRY", 5*time.Minute),
			refreshTokenExpiry:      loader.durationDefault("AUTH_REFRESH_TOKEN_EXPIRY", 7*24*time.Hour),
			absoluteSessionLifetime: loader.durationDefault("AUTH_ABSOLUTE_SESSION_LIFETIME", 30*24*time.Hour),
			challengeExpiry:         loader.durationDefault("AUTH_CHALLENGE_EXPIRY", 5*time.Minute),
		},
		rateLimit: RateLimitConfig{
			globalLimit:     loader.intDefault("RATELIMIT_GLOBAL_LIMIT", 100),
			globalWindow:    loader.durationDefault("RATELIMIT_GLOBAL_WINDOW", 1*time.Minute),
			writeLimit:      loader.intDefault("RATELIMIT_WRITE_LIMIT", 20),
			writeWindow:     loader.durationDefault("RATELIMIT_WRITE_WINDOW", 1*time.Minute),
			walletLimit:     loader.intDefault("RATELIMIT_WALLET_LIMIT", 60),
			walletWindow:    loader.durationDefault("RATELIMIT_WALLET_WINDOW", 1*time.Minute),
			rebalanceLimit:  loader.intDefault("RATELIMIT_REBALANCE_LIMIT", 3),
			rebalanceWindow: loader.durationDefault("RATELIMIT_REBALANCE_WINDOW", 1*time.Hour),
			authLimit:       loader.intDefault("RATELIMIT_AUTH_LIMIT", 10),
			authWindow:      loader.durationDefault("RATELIMIT_AUTH_WINDOW", 1*time.Minute),
			// 5 failures in 15 minutes starts the backoff. A legitimate user
			// retrying a flaky wallet signature stays well under it; a
			// signature brute-force does not.
			authFailureThreshold: loader.intDefault("AUTH_FAILURE_THRESHOLD", 5),
			authFailureWindow:    loader.durationDefault("AUTH_FAILURE_WINDOW", 15*time.Minute),
			// Doubling from 30s, capped at 15m: the 6th failure locks for 30s,
			// the 7th for 1m, and so on. The cap keeps a wallet from being
			// locked out indefinitely by someone else spamming its address.
			authLockoutBase:   loader.durationDefault("AUTH_LOCKOUT_BASE", 30*time.Second),
			authLockoutMax:    loader.durationDefault("AUTH_LOCKOUT_MAX", 15*time.Minute),
			settlementLimit:   loader.intDefault("RATELIMIT_SETTLEMENT_LIMIT", 5),
			settlementWindow:  loader.durationDefault("RATELIMIT_SETTLEMENT_WINDOW", 1*time.Minute),
			trustedProxyCount: loader.intDefault("RATELIMIT_TRUSTED_PROXY_COUNT", 0),

			// 300 cost units/minute. An ordinary read costs 1, so normal
			// browsing never approaches it (the global 100 req/min per IP
			// binds first); an intelligence relay call costs 25, so the
			// quota is what actually bounds the expensive traffic.
			// Deliberately per-environment: staging can run tighter.
			quotaEnabled:     loader.boolDefault("RATELIMIT_QUOTA_ENABLED", true),
			quotaLimit:       loader.intDefault("RATELIMIT_QUOTA_LIMIT", 300),
			quotaWindow:      loader.durationDefault("RATELIMIT_QUOTA_WINDOW", 1*time.Minute),
			quotaBypassToken: loader.stringDefault("RATELIMIT_QUOTA_BYPASS_TOKEN", ""),
		},
		log: LogConfig{
			level:  strings.ToLower(loader.stringDefault("LOG_LEVEL", "info")),
			format: strings.ToLower(loader.stringDefault("LOG_FORMAT", defaultLogFormat(environment))),
		},
		allowedOrigins: loader.stringSliceDefault("ALLOWED_ORIGINS", nil),
		performance: PerformanceConfig{
			snapshotInterval: loader.durationDefault("PERFORMANCE_SNAPSHOT_INTERVAL", 1*time.Hour),
		},
		tvl: TVLConfig{
			refreshInterval: loader.durationDefault("TVL_REFRESH_INTERVAL", 15*time.Minute),
		},
		apyRefresh: APYRefreshConfig{
			refreshInterval:       loader.durationDefault("APY_REFRESH_INTERVAL", 5*time.Minute),
			broadcastThresholdBPS: loader.intDefault("APY_BROADCAST_THRESHOLD", 50),
		},
		startup: StartupConfig{
			enableAutoMigrate: loader.boolDefault("RUN_MIGRATIONS", false),
			migrationsDir:     loader.stringDefault("MIGRATIONS_DIR", "./migrations"),
			dependencyTimeout: loader.durationDefault("STARTUP_DEPENDENCY_TIMEOUT", 5*time.Second),
		},
		bank: BankConfig{
			paystackKey:    loader.stringDefault("PAYSTACK_SECRET_KEY", ""),
			flutterwaveKey: loader.stringDefault("FLUTTERWAVE_SECRET_KEY", ""),
		},
		bankAccountCipherKey: loader.stringDefault("BANK_ACCOUNT_ENCRYPTION_KEY", ""),
		transactionPoller: TransactionPollerConfig{
			enabled:  loader.boolDefault("TX_POLLER_ENABLED", true),
			interval: loader.durationDefault("TX_POLLER_INTERVAL", 15*time.Second),
			minAge:   loader.durationDefault("TX_POLLER_MIN_AGE", 30*time.Second),
		},
		reconciliation: ReconciliationConfig{
			enabled: loader.boolDefault("RECONCILE_ENABLED", true),
			// Default matches reconciliation.DefaultCadenceConfig().Balance.
			interval: loader.durationDefault("RECONCILE_INTERVAL", 5*time.Minute),
			dryRun:   loader.boolDefault("RECONCILE_DRY_RUN", false),
		},
		recurringDeposit: RecurringDepositConfig{
			enabled:    loader.boolDefault("RECURRING_DEPOSIT_ENABLED", true),
			interval:   loader.durationDefault("RECURRING_DEPOSIT_INTERVAL", time.Hour),
			minDeposit: loader.stringDefault("MIN_DEPOSIT_AMOUNT", "0"),
		},
		harvest: HarvestConfig{
			enabled:  loader.boolDefault("HARVEST_ENGINE_ENABLED", true),
			interval: loader.durationDefault("HARVEST_ENGINE_INTERVAL", time.Hour),
			window:   loader.durationDefault("HARVEST_ENGINE_WINDOW", time.Hour),
			margin:   loader.stringDefault("HARVEST_ENGINE_MARGIN", "0.10"),
			gasFee:   loader.stringDefault("HARVEST_ENGINE_GAS_FEE", "0.05"),
		},
		jobQueue: JobQueueConfig{
			enabled:            loader.boolDefault("JOB_QUEUE_ENABLED", true),
			pollInterval:       loader.durationDefault("JOB_QUEUE_POLL_INTERVAL", time.Second),
			lease:              loader.durationDefault("JOB_QUEUE_LEASE", 30*time.Second),
			heartbeatInterval:  loader.durationDefault("JOB_QUEUE_HEARTBEAT_INTERVAL", 10*time.Second),
			jobTimeout:         loader.durationDefault("JOB_QUEUE_JOB_TIMEOUT", 2*time.Minute),
			defaultConcurrency: loader.intDefault("JOB_QUEUE_DEFAULT_CONCURRENCY", 4),
			backoffBase:        loader.durationDefault("JOB_QUEUE_BACKOFF_BASE", 2*time.Second),
			backoffMax:         loader.durationDefault("JOB_QUEUE_BACKOFF_MAX", 5*time.Minute),
			statsInterval:      loader.durationDefault("JOB_QUEUE_STATS_INTERVAL", 30*time.Second),
			drainTimeout:       loader.durationDefault("JOB_QUEUE_DRAIN_TIMEOUT", 25*time.Second),
		},
		outbox: OutboxConfig{
			enabled:      loader.boolDefault("OUTBOX_RELAY_ENABLED", true),
			pollInterval: loader.durationDefault("OUTBOX_RELAY_POLL_INTERVAL", time.Second),
			batchSize:    loader.intDefault("OUTBOX_RELAY_BATCH_SIZE", 100),
			lease:        loader.durationDefault("OUTBOX_RELAY_LEASE", 30*time.Second),
			// Hand-off backoff only. Delivery retry belongs to the job
			// queue (JOB_QUEUE_BACKOFF_*), and configuring a second budget
			// for it here is exactly the duplication #1049 avoids.
			backoff:             loader.durationDefault("OUTBOX_RELAY_BACKOFF", 5*time.Second),
			statsInterval:       loader.durationDefault("OUTBOX_RELAY_STATS_INTERVAL", 30*time.Second),
			retentionInterval:   loader.durationDefault("OUTBOX_RETENTION_INTERVAL", 24*time.Hour),
			dispatchedRetention: loader.durationDefault("OUTBOX_DISPATCHED_RETENTION", 7*24*time.Hour),
			// Dead rows outlive delivered ones by a wide margin: a delivered
			// event is history, a dead one is the evidence someone needs to
			// work out which side effect never happened and why.
			deadRetention: loader.durationDefault("OUTBOX_DEAD_RETENTION", 30*24*time.Hour),
		},
		rebalancer: RebalancerConfig{
			enabled:       loader.boolDefault("REBALANCER_ENABLED", true),
			interval:      time.Duration(loader.intDefault("REBALANCER_INTERVAL_MINUTES", 15)) * time.Minute,
			minAPYGainBPS: int64(loader.intDefault("REBALANCER_MIN_APY_GAIN_BPS", 50)),
		},
		schedulerLeadership: SchedulerLeadershipConfig{
			lockKey:           int64(loader.intDefault("SCHEDULER_LEADER_LOCK_KEY", 846000)),
			heartbeatInterval: loader.durationDefault("SCHEDULER_LEADER_HEARTBEAT_INTERVAL", 3*time.Second),
		},
		metrics: MetricsConfig{
			enabled: loader.boolDefault("METRICS_ENABLED", true),
			// Loopback by default: the endpoint exposes internal route names
			// and traffic volumes, so reaching it from another host must be
			// an explicit decision. Containers that need a scraper on the
			// same network override this to 0.0.0.0:9090 and publish no
			// host port.
			addr: loader.stringDefault("METRICS_ADDR", "127.0.0.1:9090"),
		},
		indexer: IndexerConfig{
			stalenessBudget: loader.durationDefault("INDEXER_STALENESS_BUDGET", freshness.DefaultBudget),
		},
		circuitBreaker: CircuitBreakerConfig{
			// A kill switch, because a resilience mechanism can itself cause
			// an outage if its thresholds are wrong for a given environment.
			// Turning it off must not require a code change.
			enabled:      loader.boolDefault("CIRCUIT_BREAKER_ENABLED", true),
			failureRatio: loader.floatDefault("CIRCUIT_BREAKER_FAILURE_RATIO", breaker.DefaultFailureRatio),
			minRequests:  loader.intDefault("CIRCUIT_BREAKER_MIN_REQUESTS", breaker.DefaultMinRequests),
			window:       loader.durationDefault("CIRCUIT_BREAKER_WINDOW", breaker.DefaultWindow),
			openDuration: loader.durationDefault("CIRCUIT_BREAKER_OPEN_DURATION", breaker.DefaultOpenDuration),
		},
		rpcRetry: RPCRetryConfig{
			// 1 disables retrying without disabling the helper: the metrics
			// and the typed error stay, only the second attempt goes away.
			maxAttempts: loader.intDefault("RPC_RETRY_MAX_ATTEMPTS", retry.DefaultMaxAttempts),
			baseDelay:   loader.durationDefault("RPC_RETRY_BASE_DELAY", retry.DefaultBaseDelay),
			maxDelay:    loader.durationDefault("RPC_RETRY_MAX_DELAY", retry.DefaultMaxDelay),
			budget:      loader.durationDefault("RPC_RETRY_BUDGET", retry.DefaultBudget),
		},
	}

	if cfg.bankAccountCipherKey == "" && environment == "development" {
		cfg.bankAccountCipherKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	}

	cfg.accountCipher = loader.accountCipherConfig(cfg.bankAccountCipherKey)

	cfg.validate(&loader)

	if len(loader.errors) > 0 {
		return nil, fmt.Errorf("invalid configuration:\n - %s", strings.Join(loader.errors, "\n - "))
	}

	return cfg, nil
}

func (c Config) Environment() string {
	return c.environment
}

func (c Config) Server() ServerConfig {
	return c.server
}

func (c Config) Metrics() MetricsConfig {
	return c.metrics
}

func (c Config) Indexer() IndexerConfig {
	return c.indexer
}

func (c Config) CircuitBreaker() CircuitBreakerConfig {
	return c.circuitBreaker
}

// Enabled reports whether chain calls are guarded. When false the breakers are
// not installed at all and every request goes straight to the upstream.
func (b CircuitBreakerConfig) Enabled() bool { return b.enabled }

func (c Config) RPCRetry() RPCRetryConfig {
	return c.rpcRetry
}

// Policy returns the retry policy this configuration describes.
func (r RPCRetryConfig) Policy() retry.Policy {
	return retry.Policy{
		MaxAttempts: r.maxAttempts,
		BaseDelay:   r.baseDelay,
		MaxDelay:    r.maxDelay,
		Budget:      r.budget,
	}
}

// Policy returns the breaker policy this configuration describes.
func (b CircuitBreakerConfig) Policy() breaker.Config {
	return breaker.Config{
		FailureRatio: b.failureRatio,
		MinRequests:  b.minRequests,
		Window:       b.window,
		OpenDuration: b.openDuration,
	}
}

// StalenessBudget is how far behind the chain indexed data may fall before the
// API reports it stale and the alert pages.
func (i IndexerConfig) StalenessBudget() time.Duration {
	return i.stalenessBudget
}

// Enabled reports whether the internal metrics listener should be started.
func (m MetricsConfig) Enabled() bool {
	return m.enabled
}

// Addr is the host:port the internal metrics listener binds to. It defaults
// to loopback so that an operator has to make a deliberate choice before the
// endpoint is reachable from another host.
func (m MetricsConfig) Addr() string {
	return m.addr
}

func (c Config) Database() DatabaseConfig {
	return c.database
}

func (c Config) Stellar() StellarConfig {
	return c.stellar
}

func (c Config) Allocation() AllocationConfig {
	return c.allocation
}

func (c Config) Intelligence() IntelligenceConfig {
	return c.intelligence
}

func (i IntelligenceConfig) BaseURL() string {
	return i.baseURL
}

func (i IntelligenceConfig) ServiceURL() string {
	return i.baseURL
}

func (i IntelligenceConfig) ServiceAPIKey() string {
	return i.serviceAPIKey
}

func (i IntelligenceConfig) Timeout() time.Duration {
	return i.timeout
}

func (s StellarConfig) USDCIssuer() string {
	return s.stellarUSDCIssuer
}

func (c Config) SettlementProviderURL() string {
	return c.settlementProviderURL
}

func (c Config) Auth() AuthConfig {
	return c.auth
}

func (c Config) RateLimit() RateLimitConfig {
	return c.rateLimit
}

func (c Config) Log() LogConfig {
	return c.log
}

func (c Config) Redis() RedisConfig {
	return c.redis
}

func (c Config) Performance() PerformanceConfig {
	return c.performance
}

func (c Config) Startup() StartupConfig {
	return c.startup
}

func (s StartupConfig) EnableAutoMigrate() bool {
	return s.enableAutoMigrate
}

func (s StartupConfig) MigrationsDir() string {
	return s.migrationsDir
}

func (s StartupConfig) DependencyTimeout() time.Duration {
	return s.dependencyTimeout
}

func (p PerformanceConfig) SnapshotInterval() time.Duration {
	return p.snapshotInterval
}

func (c Config) TVL() TVLConfig {
	return c.tvl
}

func (t TVLConfig) RefreshInterval() time.Duration {
	return t.refreshInterval
}

func (c Config) APYRefresh() APYRefreshConfig {
	return c.apyRefresh
}

func (a APYRefreshConfig) RefreshInterval() time.Duration {
	return a.refreshInterval
}

func (a APYRefreshConfig) BroadcastThresholdBPS() int {
	return a.broadcastThresholdBPS
}

// AllowedOrigins returns the list of origins permitted to make cross-origin
// requests to the API. An empty slice disables cross-origin access.
func (c Config) AllowedOrigins() []string {
	out := make([]string, len(c.allowedOrigins))
	copy(out, c.allowedOrigins)
	return out
}

func (r RedisConfig) Addr() string {
	return r.addr
}

// Tracing returns the OpenTelemetry tracing settings (nester#1054).
func (c *Config) Tracing() TracingConfig {
	return c.tracing
}

// Enabled reports whether trace export is switched on. When false the
// application installs a no-op tracer provider and never dials a collector.
func (t TracingConfig) Enabled() bool {
	return t.enabled
}

// OTLPEndpoint is the host:port of the OTLP/gRPC collector.
func (t TracingConfig) OTLPEndpoint() string {
	return t.otlpEndpoint
}

// OTLPInsecure reports whether the collector connection skips TLS. This is the
// default for local development against a collector on the same host; deploy
// with it false so spans are not shipped in plaintext.
func (t TracingConfig) OTLPInsecure() bool {
	return t.otlpInsecure
}

// ServiceName is reported as service.name on every span this process emits.
func (t TracingConfig) ServiceName() string {
	return t.serviceName
}

// ExporterTimeout bounds a single export round trip to the collector.
func (t TracingConfig) ExporterTimeout() time.Duration {
	return t.exporterTimeout
}

// SampleRatio is the head-based sampling probability applied to traces that
// are neither errors nor slow. Errors and requests exceeding LatencyThreshold
// are retained regardless of this value.
func (t TracingConfig) SampleRatio() float64 {
	return t.sampleRatio
}

// LatencyThreshold is the server-span duration above which a trace is retained
// irrespective of the base sample ratio.
func (t TracingConfig) LatencyThreshold() time.Duration {
	return t.latencyThreshold
}

func (c Config) Bank() BankConfig {
	return c.bank
}

func (c Config) BankAccountEncryptionKey() string {
	return c.bankAccountCipherKey
}

// AccountCipher returns the versioned key set used to encrypt account numbers.
func (c Config) AccountCipher() AccountCipherConfig {
	return c.accountCipher
}

func (c Config) TransactionPoller() TransactionPollerConfig {
	return c.transactionPoller
}

func (c Config) Reconciliation() ReconciliationConfig {
	return c.reconciliation
}

func (r ReconciliationConfig) Enabled() bool {
	return r.enabled
}

func (r ReconciliationConfig) Interval() time.Duration {
	return r.interval
}

func (r ReconciliationConfig) DryRun() bool {
	return r.dryRun
}

func (t TransactionPollerConfig) Enabled() bool {
	return t.enabled
}

func (t TransactionPollerConfig) Interval() time.Duration {
	return t.interval
}

func (t TransactionPollerConfig) MinAge() time.Duration {
	return t.minAge
}

// RecurringDepositConfig governs the hourly savings schedule deposit loop.
type RecurringDepositConfig struct {
	enabled    bool
	interval   time.Duration
	minDeposit string
}

func (c Config) RecurringDeposit() RecurringDepositConfig {
	return c.recurringDeposit
}

func (r RecurringDepositConfig) Enabled() bool {
	return r.enabled
}

func (r RecurringDepositConfig) Interval() time.Duration {
	return r.interval
}

func (r RecurringDepositConfig) MinDepositAmount() string {
	return r.minDeposit
}

// JobQueueConfig governs the durable async job queue worker pool (#824).
type JobQueueConfig struct {
	enabled            bool
	pollInterval       time.Duration
	lease              time.Duration
	heartbeatInterval  time.Duration
	jobTimeout         time.Duration
	defaultConcurrency int
	backoffBase        time.Duration
	backoffMax         time.Duration
	statsInterval      time.Duration
	drainTimeout       time.Duration
}

// OutboxConfig governs the transactional outbox relay and its retention
// sweep (#1049).
type OutboxConfig struct {
	enabled             bool
	pollInterval        time.Duration
	batchSize           int
	lease               time.Duration
	backoff             time.Duration
	statsInterval       time.Duration
	retentionInterval   time.Duration
	dispatchedRetention time.Duration
	deadRetention       time.Duration
}

func (c Config) Outbox() OutboxConfig                     { return c.outbox }
func (o OutboxConfig) Enabled() bool                      { return o.enabled }
func (o OutboxConfig) PollInterval() time.Duration        { return o.pollInterval }
func (o OutboxConfig) BatchSize() int                     { return o.batchSize }
func (o OutboxConfig) Lease() time.Duration               { return o.lease }
func (o OutboxConfig) Backoff() time.Duration             { return o.backoff }
func (o OutboxConfig) StatsInterval() time.Duration       { return o.statsInterval }
func (o OutboxConfig) RetentionInterval() time.Duration   { return o.retentionInterval }
func (o OutboxConfig) DispatchedRetention() time.Duration { return o.dispatchedRetention }
func (o OutboxConfig) DeadRetention() time.Duration       { return o.deadRetention }

// HarvestConfig governs the yield-harvest orchestration engine (#845).
type HarvestConfig struct {
	enabled  bool
	interval time.Duration
	window   time.Duration
	margin   string
	gasFee   string
}

func (c Config) Harvest() HarvestConfig         { return c.harvest }
func (h HarvestConfig) Enabled() bool           { return h.enabled }
func (h HarvestConfig) Interval() time.Duration { return h.interval }
func (h HarvestConfig) Window() time.Duration   { return h.window }
func (h HarvestConfig) Margin() string          { return h.margin }
func (h HarvestConfig) GasFee() string          { return h.gasFee }

// RebalancerConfig governs the automated vault rebalance-decision loop
// (nester#372; wired into main.go as part of #846). Money-moving: gated
// behind scheduler leadership so only one instance evaluates and submits.
type RebalancerConfig struct {
	enabled       bool
	interval      time.Duration
	minAPYGainBPS int64
}

func (c Config) Rebalancer() RebalancerConfig      { return c.rebalancer }
func (r RebalancerConfig) Enabled() bool           { return r.enabled }
func (r RebalancerConfig) Interval() time.Duration { return r.interval }
func (r RebalancerConfig) MinAPYGainBPS() int64    { return r.minAPYGainBPS }

// SchedulerLeadershipConfig governs the Postgres-advisory-lock leader
// election that gates all five scheduler background job loops (#846). See
// internal/scheduler/leadership.go for the full design rationale.
type SchedulerLeadershipConfig struct {
	lockKey           int64
	heartbeatInterval time.Duration
}

func (c Config) SchedulerLeadership() SchedulerLeadershipConfig { return c.schedulerLeadership }
func (s SchedulerLeadershipConfig) LockKey() int64              { return s.lockKey }
func (s SchedulerLeadershipConfig) HeartbeatInterval() time.Duration {
	return s.heartbeatInterval
}

func (c Config) JobQueue() JobQueueConfig                 { return c.jobQueue }
func (j JobQueueConfig) Enabled() bool                    { return j.enabled }
func (j JobQueueConfig) PollInterval() time.Duration      { return j.pollInterval }
func (j JobQueueConfig) Lease() time.Duration             { return j.lease }
func (j JobQueueConfig) HeartbeatInterval() time.Duration { return j.heartbeatInterval }
func (j JobQueueConfig) JobTimeout() time.Duration        { return j.jobTimeout }
func (j JobQueueConfig) DefaultConcurrency() int          { return j.defaultConcurrency }
func (j JobQueueConfig) BackoffBase() time.Duration       { return j.backoffBase }
func (j JobQueueConfig) BackoffMax() time.Duration        { return j.backoffMax }
func (j JobQueueConfig) StatsInterval() time.Duration     { return j.statsInterval }
func (j JobQueueConfig) DrainTimeout() time.Duration      { return j.drainTimeout }

func (b BankConfig) PaystackKey() string {
	return b.paystackKey
}

func (b BankConfig) FlutterwaveKey() string {
	return b.flutterwaveKey
}

func (c *Config) validate(loader *envLoader) {
	if strings.TrimSpace(c.server.host) == "" {
		loader.addError("SERVER_HOST is required")
	}

	if c.tracing.enabled {
		if strings.TrimSpace(c.tracing.otlpEndpoint) == "" {
			loader.addError("OTEL_EXPORTER_OTLP_ENDPOINT is required when TRACING_ENABLED is true")
		}
		// Spans carry request metadata and must not cross a network in
		// plaintext. The insecure default suits a collector on the same host
		// or compose network, but shipping it to staging or production would
		// send telemetry over unencrypted gRPC — so it is rejected there and
		// must be set explicitly.
		if c.tracing.otlpInsecure && isOneOf(c.environment, "staging", "production") {
			loader.addError("OTEL_EXPORTER_OTLP_INSECURE must be false when TRACING_ENABLED is true outside development")
		}
		if strings.TrimSpace(c.tracing.serviceName) == "" {
			loader.addError("OTEL_SERVICE_NAME is required when TRACING_ENABLED is true")
		}
		if c.tracing.exporterTimeout <= 0 {
			loader.addError("OTEL_EXPORTER_TIMEOUT must be greater than 0")
		}
	}

	if c.tracing.sampleRatio < 0 || c.tracing.sampleRatio > 1 {
		loader.addError("TRACING_SAMPLE_RATIO must be between 0 and 1")
	}

	if c.tracing.latencyThreshold < 0 {
		loader.addError("TRACING_LATENCY_THRESHOLD must not be negative")
	}

	if c.server.port <= 0 || c.server.port > 65535 {
		loader.addError("SERVER_PORT must be between 1 and 65535")
	}

	// Caught at boot rather than when the goroutine starts, so a typo fails
	// the process immediately instead of silently leaving the service
	// unscrapeable.
	if c.metrics.enabled {
		if _, _, err := net.SplitHostPort(c.metrics.addr); err != nil {
			loader.addError("METRICS_ADDR must be a valid host:port")
		} else if c.metrics.addr == c.server.Address() {
			loader.addError("METRICS_ADDR must not equal the public server address")
		}
	}

	if c.server.readTimeout <= 0 {
		loader.addError("SERVER_READ_TIMEOUT must be greater than 0")
	}

	if c.server.readHeaderTimeout <= 0 {
		loader.addError("SERVER_READ_HEADER_TIMEOUT must be greater than 0")
	}

	if c.server.writeTimeout <= 0 {
		loader.addError("SERVER_WRITE_TIMEOUT must be greater than 0")
	}

	if c.server.idleTimeout <= 0 {
		loader.addError("SERVER_IDLE_TIMEOUT must be greater than 0")
	}

	if c.server.gracefulShutdown <= 0 {
		loader.addError("SERVER_SHUTDOWN_TIMEOUT must be greater than 0")
	}

	if c.server.maxHeaderBytes <= 0 {
		loader.addError("SERVER_MAX_HEADER_BYTES must be greater than 0")
	}

	if c.startup.dependencyTimeout <= 0 {
		loader.addError("STARTUP_DEPENDENCY_TIMEOUT must be greater than 0")
	}

	if strings.TrimSpace(c.startup.migrationsDir) == "" {
		loader.addError("MIGRATIONS_DIR must not be empty")
	}

	// Upper bound as well as lower: poolSize is an int parsed from the
	// environment and is later narrowed to int32 for pgxpool's MaxConns, so an
	// oversized value would silently overflow. maxDatabasePoolSize is far above
	// any workable pool size, so this only rejects misconfiguration.
	if c.database.poolSize <= 0 || c.database.poolSize > maxDatabasePoolSize {
		loader.addError(fmt.Sprintf(
			"DATABASE_POOL_SIZE must be between 1 and %d", maxDatabasePoolSize,
		))
	}

	if c.database.connectionTimeout <= 0 {
		loader.addError("DATABASE_CONNECTION_TIMEOUT must be greater than 0")
	}

	if len(strings.TrimSpace(c.auth.secret)) < 32 {
		loader.addError("AUTH_JWT_SECRET must be at least 32 characters")
	}

	if (c.environment == "production" || c.environment == "staging") &&
		strings.TrimSpace(c.auth.secret) == defaultDevJWTSecret {
		loader.addError("AUTH_JWT_SECRET must not use the development default in production or staging")
	}

	if !jwtSecretHasAdequateEntropy(c.auth.secret) {
		loader.addError("AUTH_JWT_SECRET has insufficient entropy: use at least 8 distinct characters")
	}

	// NESTER_SERVICE_API_KEY authenticates service-to-service callers and is
	// shared between them, so a weak value is a shared weak value. It stays
	// optional, but a key that is set must be a real one (nester#1149).
	//
	// Absence is not treated as a failure: an empty key disables service auth
	// outright (the middleware's `serviceAPIKey != ""` guard), which is the
	// safest configuration rather than a weak one. Requiring the key to exist
	// would force every deployment that does not use service-to-service auth
	// to invent a secret it never uses. A key that IS set, however, must be a
	// real one in every environment — a weak shared key is weak everywhere.
	if serviceKey := strings.TrimSpace(c.auth.serviceAPIKey); serviceKey != "" {
		if len(serviceKey) < 32 {
			loader.addError("NESTER_SERVICE_API_KEY must be at least 32 characters")
		}
		if !jwtSecretHasAdequateEntropy(serviceKey) {
			loader.addError("NESTER_SERVICE_API_KEY has insufficient entropy: use at least 8 distinct characters")
		}
		// Reusing the JWT secret would let any holder of the service key mint
		// arbitrary user tokens outright, making every other control moot.
		if serviceKey == strings.TrimSpace(c.auth.secret) {
			loader.addError("NESTER_SERVICE_API_KEY must not reuse AUTH_JWT_SECRET")
		}
	}

	if c.auth.accessTokenExpiry <= 0 {
		loader.addError("AUTH_ACCESS_TOKEN_EXPIRY must be greater than 0")
	}
	if c.auth.refreshTokenExpiry <= 0 {
		loader.addError("AUTH_REFRESH_TOKEN_EXPIRY must be greater than 0")
	}
	if c.auth.absoluteSessionLifetime <= 0 {
		loader.addError("AUTH_ABSOLUTE_SESSION_LIFETIME must be greater than 0")
	}
	if c.auth.accessTokenExpiry >= c.auth.refreshTokenExpiry {
		loader.addError("AUTH_ACCESS_TOKEN_EXPIRY must be less than AUTH_REFRESH_TOKEN_EXPIRY")
	}
	if c.auth.refreshTokenExpiry >= c.auth.absoluteSessionLifetime {
		loader.addError("AUTH_REFRESH_TOKEN_EXPIRY must be less than AUTH_ABSOLUTE_SESSION_LIFETIME")
	}

	if c.auth.challengeExpiry <= 0 {
		loader.addError("AUTH_CHALLENGE_EXPIRY must be greater than 0")
	}

	if c.rateLimit.globalLimit <= 0 {
		loader.addError("RATELIMIT_GLOBAL_LIMIT must be greater than 0")
	}

	if c.rateLimit.globalWindow <= 0 {
		loader.addError("RATELIMIT_GLOBAL_WINDOW must be greater than 0")
	} else if c.rateLimit.globalWindow < time.Millisecond {
		// The Redis limiter converts the window to whole milliseconds for
		// PEXPIRE; a sub-millisecond window truncates to 0 and the counter would
		// expire immediately, silently disabling enforcement.
		loader.addError("RATELIMIT_GLOBAL_WINDOW must be at least 1ms")
	}

	if c.rateLimit.writeLimit <= 0 {
		loader.addError("RATELIMIT_WRITE_LIMIT must be greater than 0")
	}

	if c.rateLimit.writeWindow <= 0 {
		loader.addError("RATELIMIT_WRITE_WINDOW must be greater than 0")
	}

	if c.rateLimit.walletLimit <= 0 {
		loader.addError("RATELIMIT_WALLET_LIMIT must be greater than 0")
	}

	if c.rateLimit.walletWindow <= 0 {
		loader.addError("RATELIMIT_WALLET_WINDOW must be greater than 0")
	}
	if c.rateLimit.rebalanceLimit <= 0 {
		loader.addError("RATELIMIT_REBALANCE_LIMIT must be greater than 0")
	}
	if c.rateLimit.rebalanceWindow <= 0 {
		loader.addError("RATELIMIT_REBALANCE_WINDOW must be greater than 0")
	}
	if c.rateLimit.authLimit <= 0 {
		loader.addError("RATELIMIT_AUTH_LIMIT must be greater than 0")
	}
	if c.rateLimit.authWindow <= 0 {
		loader.addError("RATELIMIT_AUTH_WINDOW must be greater than 0")
	} else if c.rateLimit.authWindow < time.Millisecond {
		loader.addError("RATELIMIT_AUTH_WINDOW must be at least 1ms")
	}
	if c.rateLimit.authFailureThreshold <= 0 {
		loader.addError("AUTH_FAILURE_THRESHOLD must be greater than 0")
	}
	if c.rateLimit.authFailureWindow <= 0 {
		loader.addError("AUTH_FAILURE_WINDOW must be greater than 0")
	}
	if c.rateLimit.authLockoutBase <= 0 {
		loader.addError("AUTH_LOCKOUT_BASE must be greater than 0")
	}
	if c.rateLimit.authLockoutMax < c.rateLimit.authLockoutBase {
		loader.addError("AUTH_LOCKOUT_MAX must be at least AUTH_LOCKOUT_BASE")
	}
	if c.rateLimit.settlementLimit <= 0 {
		loader.addError("RATELIMIT_SETTLEMENT_LIMIT must be greater than 0")
	}
	if c.rateLimit.settlementWindow <= 0 {
		loader.addError("RATELIMIT_SETTLEMENT_WINDOW must be greater than 0")
	} else if c.rateLimit.settlementWindow < time.Millisecond {
		loader.addError("RATELIMIT_SETTLEMENT_WINDOW must be at least 1ms")
	}
	if c.rateLimit.trustedProxyCount < 0 {
		loader.addError("RATELIMIT_TRUSTED_PROXY_COUNT must be zero or greater")
	}
	// Only validated when enabled: a deployment that has turned quotas off
	// should not be forced to keep their numbers meaningful.
	if c.rateLimit.quotaEnabled {
		if c.rateLimit.quotaLimit <= 0 {
			loader.addError("RATELIMIT_QUOTA_LIMIT must be greater than 0")
		}
		if c.rateLimit.quotaWindow <= 0 {
			loader.addError("RATELIMIT_QUOTA_WINDOW must be greater than 0")
		} else if c.rateLimit.quotaWindow < time.Millisecond {
			// The token bucket derives its refill rate from the window in
			// whole milliseconds; a sub-millisecond window truncates to zero
			// and the bucket would never refill.
			loader.addError("RATELIMIT_QUOTA_WINDOW must be at least 1ms")
		}
	}

	if !isOneOf(c.log.level, "debug", "info", "warn", "error") {
		loader.addError("LOG_LEVEL must be one of debug, info, warn, error")
	}

	if !isOneOf(c.log.format, "json", "text") {
		loader.addError("LOG_FORMAT must be one of json, text")
	}

	validateAllowedOrigins(c.environment, c.allowedOrigins, loader)

	if c.performance.snapshotInterval <= 0 {
		loader.addError("PERFORMANCE_SNAPSHOT_INTERVAL must be greater than 0")
	}

	if c.tvl.refreshInterval <= 0 {
		loader.addError("TVL_REFRESH_INTERVAL must be greater than 0")
	}

	if c.apyRefresh.refreshInterval <= 0 {
		loader.addError("APY_REFRESH_INTERVAL must be greater than 0")
	}

	if c.apyRefresh.broadcastThresholdBPS < 0 {
		loader.addError("APY_BROADCAST_THRESHOLD must not be negative")
	}

	// A non-positive budget would mark every response stale and hold the
	// staleness alert permanently firing, so it is refused at startup rather
	// than discovered when the pager will not stop.
	if c.indexer.stalenessBudget <= 0 {
		loader.addError("INDEXER_STALENESS_BUDGET must be greater than 0")
	}

	// Only meaningful when the breakers are actually installed; a disabled
	// breaker's thresholds are never read, so they must not block startup.
	// The policy owns the rules, so they are stated once rather than
	// duplicated here and left to drift.
	if c.circuitBreaker.enabled {
		if err := c.circuitBreaker.Policy().Validate(); err != nil {
			loader.addError("CIRCUIT_BREAKER_* configuration is invalid: " + err.Error())
		}
	}

	// The policy owns the rules, so they are stated once rather than
	// duplicated here and left to drift.
	if err := c.rpcRetry.Policy().Validate(); err != nil {
		loader.addError("RPC_RETRY_* configuration is invalid: " + err.Error())
	}

	if c.transactionPoller.interval <= 0 {
		loader.addError("TX_POLLER_INTERVAL must be greater than 0")
	}

	if c.transactionPoller.minAge < 0 {
		loader.addError("TX_POLLER_MIN_AGE must not be negative")
	}

	if c.recurringDeposit.interval <= 0 {
		loader.addError("RECURRING_DEPOSIT_INTERVAL must be greater than 0")
	}

	if c.stellar.withdrawalSlippageBps <= 0 || c.stellar.withdrawalSlippageBps > 300 {
		loader.addError("WITHDRAWAL_SLIPPAGE_BPS must be between 1 and 300")
	}

	if c.allocation.minWeightPercent < 1 || c.allocation.minWeightPercent > 100 {
		loader.addError("MIN_ALLOCATION_WEIGHT must be between 1 and 100")
	}
}

func validateAllowedOrigins(environment string, origins []string, loader *envLoader) {
	if (environment == "production" || environment == "staging") && len(origins) == 0 {
		loader.addError("ALLOWED_ORIGINS must list at least one origin in production or staging")
	}

	for _, origin := range origins {
		if origin == "*" {
			loader.addError("ALLOWED_ORIGINS must not contain wildcard \"*\"; list explicit origins instead")
			continue
		}
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			loader.addError(fmt.Sprintf("ALLOWED_ORIGINS entry %q is not a valid origin (expected scheme://host[:port])", origin))
			continue
		}
		if parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			loader.addError(fmt.Sprintf("ALLOWED_ORIGINS entry %q must not contain a path, query, or fragment", origin))
		}
	}
}

func (s ServerConfig) Host() string {
	return s.host
}

func (s ServerConfig) Port() int {
	return s.port
}

func (s ServerConfig) ReadTimeout() time.Duration {
	return s.readTimeout
}

func (s ServerConfig) ReadHeaderTimeout() time.Duration {
	return s.readHeaderTimeout
}

func (s ServerConfig) WriteTimeout() time.Duration {
	return s.writeTimeout
}

func (s ServerConfig) IdleTimeout() time.Duration {
	return s.idleTimeout
}

func (s ServerConfig) GracefulShutdown() time.Duration {
	return s.gracefulShutdown
}

func (s ServerConfig) MaxHeaderBytes() int {
	return s.maxHeaderBytes
}

func (s ServerConfig) Address() string {
	return net.JoinHostPort(s.host, strconv.Itoa(s.port))
}

func (d DatabaseConfig) DSN() string {
	return d.dsn
}

func (d DatabaseConfig) PoolSize() int {
	return d.poolSize
}

func (d DatabaseConfig) ConnectionTimeout() time.Duration {
	return d.connectionTimeout
}

func (s StellarConfig) NetworkPassphrase() string {
	return s.networkPassphrase
}

func (s StellarConfig) RPCURL() string {
	return s.rpcURL
}

func (s StellarConfig) HorizonURL() string {
	return s.horizonURL
}

func (s StellarConfig) OperatorSecret() string {
	return s.operatorSecret
}

// OperatorFundedDepositsEnabled reports whether the API may spend operator
// funds on a user's behalf. Off unless explicitly enabled (nester#1152).
func (s StellarConfig) OperatorFundedDepositsEnabled() bool {
	return s.operatorFundedDepositsEnabled
}

// OperatorFundedDepositVaults is the raw comma-separated vault allowlist.
func (s StellarConfig) OperatorFundedDepositVaults() string {
	return s.operatorFundedDepositVaults
}

// OperatorFundedDepositMaxAmount caps a single operator-funded deposit.
func (s StellarConfig) OperatorFundedDepositMaxAmount() string {
	return s.operatorFundedDepositMaxAmount
}

// OperatorAddress returns the operator's public Stellar address. It is public
// data and grants no signing capability.
func (s StellarConfig) OperatorAddress() string {
	return s.operatorAddress
}

// SignerSocketPath returns the isolated signer's socket path, or empty when
// signing is not delegated.
func (s StellarConfig) SignerSocketPath() string {
	return s.signerSocketPath
}

// SigningIsolated reports whether signing is delegated to the separate signer
// process. When true this process holds no operator key.
func (s StellarConfig) SigningIsolated() bool {
	return strings.TrimSpace(s.signerSocketPath) != ""
}

func (s StellarConfig) YieldRegistryContract() string {
	return s.yieldRegistryContract
}

func (s StellarConfig) AllocationStrategyAddress() string {
	return s.allocationStrategyAddress
}

func (s StellarConfig) WithdrawalSlippageBps() int {
	return s.withdrawalSlippageBps
}

func (s StellarConfig) HarvestDefaultCompound() bool {
	return s.harvestDefaultCompound
}

func (a AllocationConfig) MinWeightPercent() int {
	return a.minWeightPercent
}

func (l LogConfig) Level() string {
	return l.level
}

func (l LogConfig) Format() string {
	return l.format
}

func (a AuthConfig) Secret() string {
	return a.secret
}

func (a AuthConfig) ServiceAPIKey() string {
	return a.serviceAPIKey
}

func (a AuthConfig) AccessTokenExpiry() time.Duration {
	return a.accessTokenExpiry
}

func (a AuthConfig) RefreshTokenExpiry() time.Duration {
	return a.refreshTokenExpiry
}

func (a AuthConfig) AbsoluteSessionLifetime() time.Duration {
	return a.absoluteSessionLifetime
}

func (a AuthConfig) ChallengeExpiry() time.Duration {
	return a.challengeExpiry
}

func (r RateLimitConfig) GlobalLimit() int {
	return r.globalLimit
}

func (r RateLimitConfig) GlobalWindow() time.Duration {
	return r.globalWindow
}

func (r RateLimitConfig) WriteLimit() int {
	return r.writeLimit
}

func (r RateLimitConfig) WriteWindow() time.Duration {
	return r.writeWindow
}

func (r RateLimitConfig) WalletLimit() int {
	return r.walletLimit
}

func (r RateLimitConfig) WalletWindow() time.Duration {
	return r.walletWindow
}

func (r RateLimitConfig) RebalanceLimit() int {
	return r.rebalanceLimit
}

func (r RateLimitConfig) RebalanceWindow() time.Duration {
	return r.rebalanceWindow
}

func (r RateLimitConfig) AuthLimit() int {
	return r.authLimit
}

func (r RateLimitConfig) AuthWindow() time.Duration {
	return r.authWindow
}

// AuthFailureThreshold is how many failures inside AuthFailureWindow are
// tolerated before lockouts begin (nester#1104).
func (r RateLimitConfig) AuthFailureThreshold() int {
	return r.authFailureThreshold
}

// AuthFailureWindow is the sliding period over which auth failures accumulate.
func (r RateLimitConfig) AuthFailureWindow() time.Duration {
	return r.authFailureWindow
}

// AuthLockoutBase is the first lockout duration; it doubles per failure beyond
// the threshold.
func (r RateLimitConfig) AuthLockoutBase() time.Duration {
	return r.authLockoutBase
}

// AuthLockoutMax caps the progressive backoff.
func (r RateLimitConfig) AuthLockoutMax() time.Duration {
	return r.authLockoutMax
}

func (r RateLimitConfig) SettlementLimit() int {
	return r.settlementLimit
}

func (r RateLimitConfig) SettlementWindow() time.Duration {
	return r.settlementWindow
}

func (r RateLimitConfig) TrustedProxyCount() int {
	return r.trustedProxyCount
}

// QuotaEnabled reports whether cost-weighted quota accounting is on. Turning it
// off is the documented way to run a load test without re-tuning every limit.
func (r RateLimitConfig) QuotaEnabled() bool {
	return r.quotaEnabled
}

// QuotaLimit is the per-subject bucket capacity in cost units per QuotaWindow.
func (r RateLimitConfig) QuotaLimit() int {
	return r.quotaLimit
}

// QuotaWindow is how long a full bucket takes to refill from empty.
func (r RateLimitConfig) QuotaWindow() time.Duration {
	return r.quotaWindow
}

// QuotaBypassToken, when non-empty, allows a request presenting it in the
// X-RateLimit-Bypass header to skip quota accounting. Empty by default, which
// disables the mechanism entirely.
func (r RateLimitConfig) QuotaBypassToken() string {
	return r.quotaBypassToken
}

type envLoader struct {
	fileValues map[string]string
	errors     []string
}

func (l *envLoader) requiredString(key string) string {
	value, ok := l.lookup(key)
	if !ok {
		l.addError(key + " is required")
		return ""
	}
	return value
}

func (l *envLoader) requiredURL(key string) string {
	value := l.requiredString(key)
	if value == "" {
		return ""
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		l.addError(fmt.Sprintf("%s must be a valid absolute URL", key))
		return ""
	}
	return value
}

func (l *envLoader) stringDefault(key, fallback string) string {
	if value, ok := l.lookup(key); ok {
		return value
	}
	return fallback
}

func (l *envLoader) intDefault(key string, fallback int) int {
	raw, ok := l.lookup(key)
	if !ok {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		l.addError(fmt.Sprintf("%s must be an integer, got %q", key, raw))
		return fallback
	}
	return value
}

func (l *envLoader) floatDefault(key string, fallback float64) float64 {
	raw, ok := l.lookup(key)
	if !ok {
		return fallback
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		l.addError(fmt.Sprintf("%s must be a number, got %q", key, raw))
		return fallback
	}
	return value
}

func (l *envLoader) stringSliceDefault(key string, fallback []string) []string {
	raw, ok := l.lookup(key)
	if !ok {
		return fallback
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func (l *envLoader) boolDefault(key string, fallback bool) bool {
	raw, ok := l.lookup(key)
	if !ok {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		l.addError(fmt.Sprintf("%s must be a boolean (true/false), got %q", key, raw))
		return fallback
	}
	return value
}

func (l *envLoader) durationDefault(key string, fallback time.Duration) time.Duration {
	raw, ok := l.lookup(key)
	if !ok {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		l.addError(fmt.Sprintf("%s must be a valid duration, got %q", key, raw))
		return fallback
	}
	return value
}

// accountCipherConfig parses the versioned encryption key set.
//
// When ACCOUNT_CIPHER_KEYS is set it takes precedence and must be a comma-
// separated list of "version:base64key" pairs, with ACCOUNT_CIPHER_ACTIVE_KEY
// naming one of those versions. Otherwise it falls back to the single legacy
// BANK_ACCOUNT_ENCRYPTION_KEY registered as version "v1" (matching the
// key_version column default), preserving existing single-key deployments.
func (l *envLoader) accountCipherConfig(legacyKey string) AccountCipherConfig {
	fingerprintKey := l.stringDefault("ACCOUNT_CIPHER_FINGERPRINT_KEY", "")
	active := l.stringDefault("ACCOUNT_CIPHER_ACTIVE_KEY", "")

	keysRaw, hasKeys := l.lookup("ACCOUNT_CIPHER_KEYS")
	if hasKeys {
		keys := make(map[string]string)
		for _, pair := range strings.Split(keysRaw, ",") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			version, keyB64, ok := strings.Cut(pair, ":")
			version = strings.TrimSpace(version)
			keyB64 = strings.TrimSpace(keyB64)
			if !ok || version == "" || keyB64 == "" {
				l.addError(`ACCOUNT_CIPHER_KEYS entries must be "version:base64key"`)
				continue
			}
			// key_version is persisted as VARCHAR(32); reject anything that would
			// pass startup only to fail at the database boundary on write/rotation.
			if len(version) > maxKeyVersionLen {
				l.addError(fmt.Sprintf("ACCOUNT_CIPHER_KEYS version %q exceeds %d characters", version, maxKeyVersionLen))
				continue
			}
			if _, dup := keys[version]; dup {
				l.addError(fmt.Sprintf("ACCOUNT_CIPHER_KEYS has duplicate version %q", version))
				continue
			}
			keys[version] = keyB64
		}

		// A non-empty setting that parses to zero usable entries (e.g. "," or
		// ": ") must fail closed rather than silently disabling encryption.
		if len(keys) == 0 {
			l.addError("ACCOUNT_CIPHER_KEYS must contain at least one valid version:base64key entry")
		}
		if active == "" {
			l.addError("ACCOUNT_CIPHER_ACTIVE_KEY is required when ACCOUNT_CIPHER_KEYS is set")
		} else if _, ok := keys[active]; !ok && len(keys) > 0 {
			l.addError("ACCOUNT_CIPHER_ACTIVE_KEY must match a version listed in ACCOUNT_CIPHER_KEYS")
		}
		// Without a v1 key, an empty fingerprint pepper would track the active key
		// and shift the blind index on every rotation, permitting duplicate
		// accounts. Require an explicit, rotation-independent pepper in that case.
		if len(keys) > 0 && fingerprintKey == "" {
			if _, hasV1 := keys["v1"]; !hasV1 {
				l.addError("ACCOUNT_CIPHER_FINGERPRINT_KEY is required when ACCOUNT_CIPHER_KEYS has no v1 key")
			}
		}

		return AccountCipherConfig{activeVersion: active, keys: keys, fingerprintKey: fingerprintKey}
	}

	// Backward compatibility: fall back to the single legacy key as version "v1".
	if strings.TrimSpace(legacyKey) != "" {
		return AccountCipherConfig{
			activeVersion:  "v1",
			keys:           map[string]string{"v1": legacyKey},
			fingerprintKey: fingerprintKey,
		}
	}

	if active != "" || fingerprintKey != "" {
		l.addError("ACCOUNT_CIPHER_ACTIVE_KEY/ACCOUNT_CIPHER_FINGERPRINT_KEY set but no keys are configured (set ACCOUNT_CIPHER_KEYS or BANK_ACCOUNT_ENCRYPTION_KEY)")
	}
	return AccountCipherConfig{}
}

func (l *envLoader) lookup(key string) (string, bool) {
	if value, ok := os.LookupEnv(key); ok {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed, true
		}
	}

	value, ok := l.fileValues[key]
	if !ok {
		return "", false
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

func (l *envLoader) addError(message string) {
	l.errors = append(l.errors, message)
}

func loadDotEnvFile(path string) (map[string]string, error) {
	values, err := godotenv.Read(path)
	if err == nil {
		return values, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	return nil, fmt.Errorf("load .env: %w", err)
}

func defaultLogFormat(environment string) string {
	if environment == "production" || environment == "staging" {
		return "json"
	}
	return "text"
}

func isOneOf(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}

// jwtSecretHasAdequateEntropy returns false when the secret is composed of
// fewer than 8 distinct bytes, catching low-entropy values such as repeated
// characters or trivially predictable sequences.
func jwtSecretHasAdequateEntropy(secret string) bool {
	seen := make(map[byte]struct{}, 8)
	for i := 0; i < len(secret); i++ {
		seen[secret[i]] = struct{}{}
		if len(seen) >= 8 {
			return true
		}
	}
	return false
}
