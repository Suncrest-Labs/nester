package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"

	"github.com/golang-migrate/migrate/v4"
	migratedb "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/breaker"
	"github.com/suncrestlabs/nester/apps/api/internal/cache"
	"github.com/suncrestlabs/nester/apps/api/internal/config"
	cryptopkg "github.com/suncrestlabs/nester/apps/api/internal/crypto"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/caps"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/ledger"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/nudge"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/outbox"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/usersignal"
	"github.com/suncrestlabs/nester/apps/api/internal/freshness"
	"github.com/suncrestlabs/nester/apps/api/internal/handler"
	"github.com/suncrestlabs/nester/apps/api/internal/harvest"
	"github.com/suncrestlabs/nester/apps/api/internal/metrics"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
	"github.com/suncrestlabs/nester/apps/api/internal/oracle"
	"github.com/suncrestlabs/nester/apps/api/internal/reconciliation"
	"github.com/suncrestlabs/nester/apps/api/internal/repository"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
	"github.com/suncrestlabs/nester/apps/api/internal/retry"
	"github.com/suncrestlabs/nester/apps/api/internal/scheduler"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	performancesvc "github.com/suncrestlabs/nester/apps/api/internal/service/performance"
	tvlsvc "github.com/suncrestlabs/nester/apps/api/internal/service/tvl"
	"github.com/suncrestlabs/nester/apps/api/internal/services"
	stellarpkg "github.com/suncrestlabs/nester/apps/api/internal/stellar"
	"github.com/suncrestlabs/nester/apps/api/internal/telemetry"
	"github.com/suncrestlabs/nester/apps/api/internal/valuation"
	"github.com/suncrestlabs/nester/apps/api/internal/ws"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
)

// version and commit identify the running build. Both are injected at link
// time (see the ldflags in the Dockerfile and the release workflow); the
// defaults are what a plain `go build` or `go run` produces locally.
//
// commit falls back to the VCS revision Go stamps into the build info, so a
// binary built without explicit ldflags still reports what it was built from
// rather than "unknown" (issue #1117).
var (
	version = "dev"
	commit  = ""
)

// buildCommit returns the commit the binary was built from, preferring the
// ldflags value and falling back to the VCS stamp Go records automatically.
func buildCommit() string {
	if commit != "" {
		return commit
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	var revision, modified string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value
		}
	}
	if revision == "" {
		return "unknown"
	}
	if modified == "true" {
		return revision + "-dirty"
	}
	return revision
}

func main() {
	if err := run(); err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

// stellarNetworkLabel maps a Stellar network passphrase to a short, stable
// label for logs. The passphrase is a public chain identifier rather than a
// credential, but logging it verbatim trips go/clear-text-logging because of
// the name, and the label is the more useful thing to read in a startup line
// anyway. An unrecognised network is reported as "custom" so a misconfigured
// passphrase is never echoed into the log.
func stellarNetworkLabel(passphrase string) string {
	switch passphrase {
	case "Public Global Stellar Network ; September 2015":
		return "pubnet"
	case "Test SDF Network ; September 2015":
		return "testnet"
	case "Test SDF Future Network ; October 2022":
		return "futurenet"
	case "Standalone Network ; February 2017":
		return "standalone"
	case "":
		return "unset"
	default:
		return "custom"
	}
}

func run() error {
	startedAt := time.Now()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	baseLogger, err := logpkg.New(cfg.Log(), version)
	if err != nil {
		return err
	}

	// Stamp the build into the first log line so what is deployed can be read
	// straight off the logs, not only from /health/detailed (issue #1117).
	baseLogger.Info("starting nester api", "version", version, "commit", buildCommit())

	// Created early (rather than just before ListenAndServe, as before) so
	// components that need to release resources as soon as shutdown begins —
	// notably scheduler leadership below — can hook directly into it instead
	// of only unwinding via defer after the HTTP server finishes draining.
	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Distributed tracing (#1054). Installed before any dependency is opened
	// so the pool, cache and HTTP clients below are all created against a
	// configured provider. Disabled by default: Init then installs a no-op
	// provider, dials no collector, and every instrumentation call site
	// becomes a cheap no-op.
	tracingCfg := cfg.Tracing()
	_, shutdownTracing, err := telemetry.Init(shutdownCtx, telemetry.Config{
		Enabled:          tracingCfg.Enabled(),
		Endpoint:         tracingCfg.OTLPEndpoint(),
		Insecure:         tracingCfg.OTLPInsecure(),
		ServiceName:      tracingCfg.ServiceName(),
		ServiceVersion:   version,
		Environment:      cfg.Environment(),
		ExporterTimeout:  tracingCfg.ExporterTimeout(),
		SampleRatio:      tracingCfg.SampleRatio(),
		LatencyThreshold: tracingCfg.LatencyThreshold(),
	}, baseLogger)
	if err != nil {
		return err
	}
	defer func() {
		// Bounded independently of shutdownCtx, which is already cancelled by
		// the time this runs; without a fresh context the final flush would
		// abort and drop the spans from the shutdown itself.
		flushCtx, cancelFlush := context.WithTimeout(context.Background(), tracingCfg.ExporterTimeout())
		defer cancelFlush()
		if err := shutdownTracing(flushCtx); err != nil {
			baseLogger.Warn("tracing shutdown reported an error", "error", err)
		}
	}()

	// The traced pool is chosen up front so every repository built from it
	// emits query spans; NewPostgresDB remains the untraced default.
	newPool := repository.NewPostgresDB
	if tracingCfg.Enabled() {
		newPool = repository.NewPostgresDBTraced
	}

	pgPool, err := newPool(cfg.Database())
	if err != nil {
		return err
	}
	defer pgPool.Pool.Close()

	db := stdlib.OpenDBFromPool(pgPool.Pool)
	defer db.Close()

	if cfg.Startup().EnableAutoMigrate() {
		baseLogger.Info("running database migrations", "dir", cfg.Startup().MigrationsDir())

		// Dedicated *sql.DB for the migrator: m.Close() closes the instance
		// passed to WithInstance, so it must not be the one repositories use.
		migDB := stdlib.OpenDBFromPool(pgPool.Pool)

		driver, err := migratedb.WithInstance(migDB, &migratedb.Config{})
		if err != nil {
			_ = migDB.Close()
			return fmt.Errorf("auto-migrate: init driver: %w", err)
		}

		m, err := migrate.NewWithDatabaseInstance(
			"file://"+cfg.Startup().MigrationsDir(),
			"postgres", driver)
		if err != nil {
			_ = migDB.Close()
			return fmt.Errorf("auto-migrate: new migrate instance: %w", err)
		}

		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			_, _ = m.Close()
			return fmt.Errorf("auto-migrate: up: %w", err)
		}

		if srcErr, dbErr := m.Close(); srcErr != nil || dbErr != nil {
			return fmt.Errorf("auto-migrate: close: source=%v db=%v", srcErr, dbErr)
		}

		baseLogger.Info("database migrations complete")
	} else {
		baseLogger.Info("auto-migrate disabled; skipping migrations")
	}

	if err := pingStellarDependencies(baseLogger, cfg); err != nil {
		return err
	}

	// Scheduler leader election (#846): elects one instance to run the five
	// singleton background job loops below (rebalancer, recurring deposits,
	// APY deviation, goal deadline reminders, protocol health). See
	// internal/scheduler/leadership.go for the advisory-lock design and
	// failover semantics. Hooked directly into shutdownCtx (created above,
	// before the OS signal fires) rather than an independent context, so the
	// lock releases as soon as shutdown begins instead of only once the HTTP
	// server finishes draining — letting another instance take over sooner.
	schedulerLeadership := scheduler.NewLeadership(
		db,
		scheduler.LeadershipConfig{
			LockKey:           cfg.SchedulerLeadership().LockKey(),
			HeartbeatInterval: cfg.SchedulerLeadership().HeartbeatInterval(),
		},
		baseLogger.WithGroup("scheduler-leadership"),
	)
	go schedulerLeadership.Run(shutdownCtx)

	systemStateRepository := postgres.NewSystemStateRepository(db)

	// The Prometheus registry is constructed here rather than further down
	// because the vault service takes it for the deposit and withdrawal SLIs
	// (nester#1056), and it must exist before the first instrumented service.
	// Additional collectors still attach to it below.
	appMetrics := metrics.New()

	// Balance freshness (nester#1088). One tracker is the source of truth for
	// the lag metrics, the staleness alert, and the freshness headers the API
	// returns, so the pager and the UI can never disagree about whether
	// balances are current. It is created here because the middleware chain
	// below and the indexer goroutine further down both read it.
	indexerFreshness := freshness.NewTracker(cfg.Indexer().StalenessBudget())
	if err := appMetrics.RegisterFreshness(indexerFreshness); err != nil {
		// Non-fatal: losing the freshness metrics must not stop the API from
		// serving, and the API still reports staleness in its own headers.
		baseLogger.Error("failed to register indexer freshness collector", "error", err)
	}

	// Circuit breakers for the chain upstreams (nester#1087). Built before the
	// first chain client because every one of them is wired through
	// chainHTTPClient below.
	chainBreakers, err := newChainBreakers(cfg, appMetrics, baseLogger)
	if err != nil {
		return fmt.Errorf("init chain circuit breakers: %w", err)
	}

	// The bounded, jittered retry policy every Soroban RPC call site shares
	// (nester#1086). One Runner and one policy for the whole process: a
	// per-call-site policy is how behaviour drifted between call sites in the
	// first place. Only idempotent reads are retried — the stellar package
	// decides that per RPC method, and sendTransaction is never among them.
	sorobanRPCOptions := stellarpkg.RPCOptions{
		Runner:   retry.New(),
		Policy:   cfg.RPCRetry().Policy(),
		Observer: appMetrics.RPCRecorderFor(metrics.UpstreamSorobanRPC),
	}
	baseLogger.Info("soroban rpc retry policy",
		"max_attempts", sorobanRPCOptions.Policy.MaxAttempts,
		"base_delay", sorobanRPCOptions.Policy.BaseDelay.String(),
		"max_delay", sorobanRPCOptions.Policy.MaxDelay.String(),
		"budget", sorobanRPCOptions.Policy.Budget.String(),
	)

	vaultRepository := postgres.NewVaultRepository(db)
	vaultService := service.NewVaultService(vaultRepository)
	// Deposit and withdrawal SLIs (nester#1056).
	vaultService.SetMetrics(appMetrics)
	vaultService.SetHarvestDefaultCompound(cfg.Stellar().HarvestDefaultCompound())
	vaultHandler := handler.NewVaultHandler(vaultService)

	yieldHarvestRepository := postgres.NewYieldHarvestRepository(db)
	yieldHarvestService := service.NewYieldHarvestService(yieldHarvestRepository)
	vaultService.SetYieldHarvestRecorder(yieldHarvestService)

	// Ledger: authoritative double-entry bookkeeping source of truth
	ledgerRepository := postgres.NewLedgerRepository(db)
	ledgerService := service.NewLedgerService(ledgerRepository, db)
	_ = ledgerService // wired for future use; portfolio reads via repo, postings via vault repo helpers

	portfolioService := service.NewPortfolioService(vaultRepository)
	portfolioService.SetLedgerRepository(ledgerRepository)
	portfolioHandler := handler.NewPortfolioHandler(portfolioService)

	transactionRepository := postgres.NewTransactionRepository(db)
	transactionService := service.NewTransactionService(transactionRepository, cfg.Stellar().HorizonURL())
	// Confirmation polling is the steadiest Horizon caller, so it is the
	// traffic most worth shedding when Horizon degrades (nester#1087).
	transactionService.SetHTTPClient(chainBreakers.client(appMetrics, 10*time.Second, metrics.UpstreamHorizon))
	// Balance is moved only after a deposit/withdrawal is confirmed on-chain
	// (issue #496); the vault repository applies it idempotently by tx hash.
	transactionService.SetBalanceApplier(vaultRepository)
	// A successful hash is not proof it paid this vault. The lookup supplies
	// the vault's real contract address and currency so a confirmation is
	// checked against the transaction's actual operations, and the credited
	// amount is taken from the chain rather than the request body
	// (nester#1145).
	transactionService.SetVaultLookup(vaultRepository)
	transactionHandler := handler.NewTransactionHandler(transactionService)
	transactionHandler.SetVaultRepository(vaultRepository)

	var accountCipher *cryptopkg.AccountCipher
	if ac := cfg.AccountCipher(); ac.Configured() {
		cipher, cipherErr := cryptopkg.NewAccountCipherWithKeys(ac.ActiveVersion(), ac.Keys(), ac.FingerprintKey())
		if cipherErr != nil {
			return fmt.Errorf("account cipher: %w", cipherErr)
		}
		accountCipher = cipher
	}

	userRepository := postgres.NewUserRepository(db)
	userService := service.NewUserService(userRepository)
	userHandler := handler.NewUserHandler(userService)

	notificationRepository := postgres.NewNotificationRepository(db)
	notificationHandler := handler.NewNotificationHandler(notificationRepository)

	adminRepository := postgres.NewAdminRepository(db)
	goalTemplateRepo := postgres.NewGoalTemplateRepository(db)

	// Signing custody. Two configurations are supported, and which one is
	// active is logged at startup so the deployed posture is visible rather
	// than assumed.
	//
	//   - Isolated (recommended): SIGNER_SOCKET_PATH is set, the operator key
	//     lives in the separate signer process, and this process holds none.
	//   - Local: STELLAR_OPERATOR_SECRET is set here. Retained for local
	//     development; see docs/security/signing-isolation.md for why it is not
	//     the recommended production configuration.
	// Durable chain-submission records (nester#1085). Created before any
	// invoker, because every chain write is required to persist an intent
	// through this store before it is sent.
	submissionStore := stellarpkg.NewPostgresSubmissionStore(db)

	var chainInvoker service.VaultChainInvoker
	switch {
	case cfg.Stellar().SigningIsolated():
		operatorAddress := cfg.Stellar().OperatorAddress()
		if operatorAddress == "" {
			return errors.New("STELLAR_OPERATOR_ADDRESS is required when signing is delegated to the signer process")
		}
		if cfg.Stellar().OperatorSecret() != "" {
			// Holding the key while also delegating defeats the isolation: the
			// key would still be extractable from this process. Refuse rather
			// than silently preferring one path.
			return errors.New("STELLAR_OPERATOR_SECRET must not be set when SIGNER_SOCKET_PATH is configured")
		}
		inv, err := service.NewIsolatedSorobanVaultChainInvoker(
			cfg.Stellar().RPCURL(),
			cfg.Stellar().HorizonURL(),
			cfg.Stellar().NetworkPassphrase(),
			operatorAddress,
			cfg.Stellar().SignerSocketPath(),
			cfg.Stellar().WithdrawalSlippageBps(),
		)
		if err != nil {
			return fmt.Errorf("init isolated chain invoker: %w", err)
		}
		// The invoker calls Soroban RPC and Horizon through one client; the
		// breaker routes per request URL, so the two stay independent.
		inv.SetHTTPClient(chainBreakers.client(appMetrics, 30*time.Second, metrics.UpstreamSorobanRPC))
		inv.SetRPCOptions(sorobanRPCOptions)
		// Durable submission records (nester#1085): every chain write now
		// persists an intent before it is sent, so a lost RPC response can
		// never leave a transaction the system knows nothing about.
		inv.SetSubmissionStore(submissionStore, baseLogger.WithGroup("chain-submission"))
		chainInvoker = inv
		vaultService.SetDepositInvoker(inv)
		baseLogger.Info("signing is isolated: this process holds no operator key",
			"signer_socket", cfg.Stellar().SignerSocketPath(),
			"operator_address", operatorAddress)

	case cfg.Stellar().OperatorSecret() != "":
		inv, err := service.NewSorobanVaultChainInvoker(
			cfg.Stellar().RPCURL(),
			cfg.Stellar().HorizonURL(),
			cfg.Stellar().NetworkPassphrase(),
			cfg.Stellar().OperatorSecret(),
			cfg.Stellar().WithdrawalSlippageBps(),
		)
		if err != nil {
			return fmt.Errorf("init chain invoker: %w", err)
		}
		inv.SetHTTPClient(chainBreakers.client(appMetrics, 30*time.Second, metrics.UpstreamSorobanRPC))
		inv.SetRPCOptions(sorobanRPCOptions)
		// Durable submission records (nester#1085): every chain write now
		// persists an intent before it is sent, so a lost RPC response can
		// never leave a transaction the system knows nothing about.
		inv.SetSubmissionStore(submissionStore, baseLogger.WithGroup("chain-submission"))
		chainInvoker = inv
		vaultService.SetDepositInvoker(inv)
		baseLogger.Warn("signing key is held in the API process; " +
			"see docs/security/signing-isolation.md for the isolated configuration")

	default:
		baseLogger.Info("no signing configured: chain write operations are unavailable")
	}

	// Operator-funded deposits (nester#1152).
	//
	// A deposit with no user-signed tx_hash is submitted with the operator as
	// both caller and depositing user, so it spends platform funds on the
	// caller's behalf. Disabled unless explicitly configured, and even then
	// only for allowlisted vaults under a per-deposit cap, with every use
	// logged. A nil policy would refuse everything, but it is always
	// installed so the refusals are logged rather than silent.
	operatorFundedVaults, err := service.ParseOperatorFundedVaultIDs(cfg.Stellar().OperatorFundedDepositVaults())
	if err != nil {
		return fmt.Errorf("parse operator-funded deposit allowlist: %w", err)
	}
	operatorFundedCap, err := decimal.NewFromString(cfg.Stellar().OperatorFundedDepositMaxAmount())
	if err != nil {
		return fmt.Errorf("parse operator-funded deposit cap: %w", err)
	}
	vaultService.SetOperatorFundedDepositPolicy(service.NewOperatorFundedDepositPolicy(
		cfg.Stellar().OperatorFundedDepositsEnabled(),
		operatorFundedVaults,
		operatorFundedCap,
		baseLogger.WithGroup("operator-funded-deposits"),
	))
	if cfg.Stellar().OperatorFundedDepositsEnabled() {
		baseLogger.Warn("operator-funded deposits are ENABLED: the API can spend platform funds on a user's behalf",
			"allowlisted_vaults", len(operatorFundedVaults),
			"per_deposit_cap", operatorFundedCap.String())
	}

	if cfg.Stellar().RPCURL() != "" {
		vaultService.SetChainEventVerifier(service.NewStellarChainEventVerifier(cfg.Stellar().RPCURL()))
	}

	adminService := service.NewAdminService(
		adminRepository,
		vaultRepository,
		chainInvoker,
		cfg.Stellar().HorizonURL(),
		cfg.Stellar().AllocationStrategyAddress(),
		cfg.Allocation().MinWeightPercent(),
	)
	adminService.SetTemplateRepository(goalTemplateRepo)
	adminHandler := handler.NewAdminHandler(adminService, userService)
	adminHandler.SetEventSyncer(&stellarpkg.EventSyncer{
		DB:         db,
		SysRepo:    systemStateRepository,
		RPCURL:     cfg.Stellar().RPCURL(),
		Logger:     baseLogger,
		RPCOptions: sorobanRPCOptions,
	})
	adminHandler.SetLeadership(schedulerLeadership)

	// Historical chain backfill/resync tool (#840): operator-triggered via
	// the admin endpoints below. Reuses applyIndexedEvent (same package,
	// see internal/stellar/backfill.go's doc comment) so backfilled and
	// live-indexed events are processed identically.
	backfillRepo := postgres.NewBackfillRepository(db)
	backfillRunner := &stellarpkg.Runner{
		DB:         db,
		Repo:       backfillRepo,
		RPCURL:     cfg.Stellar().RPCURL(),
		Logger:     baseLogger.WithGroup("backfill"),
		RPCOptions: sorobanRPCOptions,
	}
	adminHandler.SetBackfillRunner(backfillRunner, backfillRepo)

	// A single shared Redis client (nil when REDIS_ADDR is unset) powers both the
	// challenge store and the distributed rate limiters. When nil, both fall back
	// to in-memory implementations suitable for single-instance deployments.
	var redisClient *redis.Client
	if addr := cfg.Redis().Addr(); addr != "" {
		redisClient = redis.NewClient(&redis.Options{Addr: addr})
		// Command-name-only spans; keys and values are never recorded.
		redisClient = cache.InstrumentRedis(redisClient, cfg.Tracing().Enabled())
	}

	// The remaining collectors attach to appMetrics, which is constructed
	// above. Nothing in the request path registers a collector: registration
	// takes the registry lock, and doing it per request would be both a
	// hot-path cost and an unbounded-series risk.
	//
	// The registry is populated before any traffic is served so a scrape that
	// lands during startup returns a consistent set of series rather than a
	// metric appearing partway through.

	// pgxpool and go-redis both maintain their own counters, so these are
	// pull collectors read at scrape time rather than gauges on a ticker.
	if err := appMetrics.RegisterPool(pgPool.Pool); err != nil {
		return fmt.Errorf("register db pool metrics: %w", err)
	}
	appMetrics.InstrumentRedis(redisClient)

	var challengeStore service.ChallengeStore
	var revocationCache service.RevocationCache
	if redisClient != nil {
		challengeStore = service.NewRedisChallengeStore(redisClient, cfg.Auth().ChallengeExpiry())
		revocationCache = service.NewRedisRevocationCache(redisClient)
		baseLogger.Info("challenge store: redis", "addr", cfg.Redis().Addr())
		baseLogger.Info("revocation cache: redis", "addr", cfg.Redis().Addr())
	} else {
		challengeStore = service.NewInMemoryChallengeStore(cfg.Auth().ChallengeExpiry())
		revocationCache = service.NewInMemoryRevocationCache()
		baseLogger.Info("challenge store: in-memory (single-instance only)")
		baseLogger.Info("revocation cache: in-memory (single-instance only)")
	}

	sessionRepository := postgres.NewSessionRepository(db)
	auditLogger := postgres.NewPostgresAuditLogger(db)
	anomalyDetector := service.NoopAnomalyDetector{}

	// Issue #1141: support tooling to inspect a user's money-path state.
	adminHandler.SetMoneyPathServices(portfolioService, transactionService, auditLogger)

	// Global pause switch for the money path (#1120). Gates deposits and
	// withdrawals independently, persisted so an engaged switch survives a
	// restart, and audit-logged on every change.
	//
	// Attached to vaultService rather than passed through its constructor so
	// the many services built for tests and tooling keep working unchanged:
	// a service with no gate allows everything, exactly as before.
	moneyPathSwitchService := service.NewMoneyPathSwitchService(
		postgres.NewMoneyPathSwitchRepository(db), auditLogger)
	vaultService.SetMoneyPathSwitches(moneyPathSwitchService)

	// Launch caps: per-user deposit cap and global TVL cap (#1119).
	// Config-driven (LAUNCH_PER_USER_DEPOSIT_CAP / LAUNCH_GLOBAL_TVL_CAP env
	// vars) so operators can raise, lower, or disable either cap by changing
	// the env var and restarting — no code change. Blank or "0" explicitly
	// disables a cap; any other malformed or negative value fails startup
	// (nester CodeRabbit finding) rather than silently disabling the cap.
	{
		// Blank/"0" explicitly disables a cap; anything else must parse as a
		// non-negative decimal or startup fails loudly rather than silently
		// disabling the cap on a typo (nester CodeRabbit finding).
		perUserCap, err := caps.ParseCapValue(cfg.LaunchCaps().PerUserDepositCap())
		if err != nil {
			return fmt.Errorf("LAUNCH_PER_USER_DEPOSIT_CAP: %w", err)
		}
		globalCap, err := caps.ParseCapValue(cfg.LaunchCaps().GlobalTVLCap())
		if err != nil {
			return fmt.Errorf("LAUNCH_GLOBAL_TVL_CAP: %w", err)
		}
		capsChecker := caps.NewChecker(caps.Config{
			PerUserCap:        perUserCap,
			GlobalCap:         globalCap,
			WarnThresholdsPct: cfg.LaunchCaps().WarnThresholdsPct(),
		}, vaultRepository, func(ctx context.Context, w caps.Warning) {
			logpkg.FromContext(ctx).Warn("launch cap approaching threshold",
				"kind", w.Kind,
				"user_id", w.UserID,
				"cap", w.Cap.String(),
				"new_total", w.NewTotal.String(),
				"threshold_pct", w.ThresholdPct,
			)
		})
		vaultService.SetCapsChecker(capsChecker)
	}

	// Append-only balance-change audit trail (#1124).
	vaultService.SetBalanceAuditRecorder(postgres.NewBalanceAuditRepository(db))

	activityEventRepo := postgres.NewActivityEventRepository(db)
	nudgeHistoryRepo := postgres.NewNudgeHistoryRepository(db)
	nudgeOutcomeService := service.NewNudgeOutcomeService(nudgeHistoryRepo)

	oracleService := oracle.NewRateService(cfg.Stellar().HorizonURL(), cfg.Stellar().USDCIssuer())

	// Each rate provider is instrumented with the upstream it actually
	// calls, matched on the provider's own Name() rather than on its
	// concrete type, so adding a provider does not silently go unmeasured —
	// it lands in "other" and shows up as an unattributed series.
	xlmProviders, fiatProvider := oracleService.Providers()
	for _, provider := range xlmProviders {
		instrumentRateProvider(appMetrics, chainBreakers, provider)
	}
	instrumentRateProvider(appMetrics, chainBreakers, fiatProvider)
	rateHandler := handler.NewRateHandler(oracleService)

	// maxWSConnsPerIP bounds simultaneous WebSocket connections from one
	// client IP (nester#828), mirroring the per-route rate limits already
	// applied via middleware.NewLimiter below. 0 would mean unlimited.
	const maxWSConnsPerIP = 20

	wsHub := ws.NewHub(baseLogger.WithGroup("websocket"), func(token string) (userID, sessionID string, err error) {
		if token == "" {
			return "", "", fmt.Errorf("missing token")
		}
		claims, err := auth.ParseJWT(token, cfg.Auth().Secret())
		if err != nil {
			return "", "", fmt.Errorf("invalid token: %w", err)
		}
		if claims.SessionID != "" {
			revoked, err := revocationCache.IsRevoked(context.Background(), claims.SessionID)
			if err != nil {
				return "", "", fmt.Errorf("session verification unavailable: %w", err)
			}
			if revoked {
				return "", "", fmt.Errorf("session revoked")
			}
		}
		return claims.Subject, claims.SessionID, nil
	}, cfg.AllowedOrigins(), redisClient, maxWSConnsPerIP)

	wsCtx, wsCancel := context.WithCancel(context.Background())
	defer wsCancel()
	go wsHub.Run(wsCtx)
	vaultHandler.SetWSHub(wsHub)

	// Real-time portfolio valuation (#832): aggregates each user's positions,
	// pending deposits, accrued yield, goal allocations, and claimable rewards to
	// the stroop, prices multi-asset holdings through an oracle with confidence
	// propagation, caches per user, and pushes fresh valuations over WebSocket on
	// event-driven invalidation.
	valuationService := valuation.NewService(valuation.Deps{
		Positions: valuation.NewVaultPositionSource(vaultRepository),
		Pending:   valuation.NewTxPendingSource(transactionRepository),
		Goals:     valuation.NewGoalAllocationSource(postgres.NewSavingsGoalRepository(db)),
		Oracle:    valuation.NewStaticOracle(nil),
		Cache:     valuation.NewCache(30 * time.Second),
		Notifier:  valuation.NewWSNotifier(wsHub, baseLogger.WithGroup("valuation")),
		Logger:    baseLogger.WithGroup("valuation"),
	})
	valuationHandler := handler.NewValuationHandler(valuationService)

	authService := service.NewAuthService(challengeStore, userService, sessionRepository, revocationCache, anomalyDetector, auditLogger, wsHub, cfg.Auth())
	authHandler := handler.NewAuthHandler(authService, cfg.Environment() != "development", userService, nudgeOutcomeService, activityEventRepo)

	performanceRepository := postgres.NewPerformanceRepository(db)
	vaultRepository = postgres.NewVaultRepository(db)
	performanceService := performancesvc.NewService(performanceRepository, vaultRepository)
	performanceHandler := handler.NewPerformanceHandler(performanceService, handler.NewVaultOwnerAdapter(vaultRepository))

	// Projection service for compound interest calculations, plus the Monte
	// Carlo savings forecast (#843), which needs the goal/schedule repos to
	// ground contribution behavior in the user's own history.
	projectionCalculator := service.NewCompoundInterestCalculator()
	projectionService := service.NewProjectionService(
		projectionCalculator,
		vaultRepository,
		performanceRepository,
		postgres.NewSavingsGoalRepository(db),
		postgres.NewSavingsScheduleRepository(db),
	)
	projectionHandler := handler.NewProjectionHandler(projectionService)

	contractReader := stellarpkg.NewContractReader(
		cfg.Stellar().RPCURL(),
		cfg.Stellar().NetworkPassphrase(),
		"",
	)
	contractReader.SetHTTPClient(
		chainBreakers.client(appMetrics, 30*time.Second, metrics.UpstreamSorobanRPC),
	)
	contractReader.SetRPCOptions(sorobanRPCOptions)

	tracker := performancesvc.NewTracker(
		performanceRepository,
		vaultRepository,
		contractReader,
		cfg.Performance().SnapshotInterval(),
	)
	trackerCtx, cancelTracker := context.WithCancel(context.Background())
	defer cancelTracker()
	go func() {
		if err := tracker.Run(trackerCtx); err != nil && !errors.Is(err, context.Canceled) {
			baseLogger.Error("performance tracker stopped", "error", err.Error())
		}
	}()

	tvlRepository := postgres.NewTVLRepository(db)
	tvlService := tvlsvc.NewService(tvlRepository, vaultRepository)
	tvlHandler := handler.NewTVLHandler(tvlService)

	tvlTracker := tvlsvc.NewTracker(
		tvlRepository,
		vaultRepository,
		contractReader,
		cfg.TVL().RefreshInterval(),
	).WithLogger(baseLogger.WithGroup("tvl-tracker"))
	tvlCtx, cancelTVL := context.WithCancel(context.Background())
	defer cancelTVL()
	go func() {
		if err := tvlTracker.Run(tvlCtx); err != nil && !errors.Is(err, context.Canceled) {
			baseLogger.Error("tvl tracker stopped", "error", err.Error())
		}
	}()

	apyRefresher := performancesvc.NewAPYRefresher(
		performancesvc.APYRefresherConfig{
			Interval:              cfg.APYRefresh().RefreshInterval(),
			BroadcastThresholdBPS: cfg.APYRefresh().BroadcastThresholdBPS(),
			RegistryAddress:       cfg.Stellar().YieldRegistryContract(),
		},
		performanceRepository,
		vaultRepository,
		&performancesvc.RegistryReader{
			Reader:  contractReader,
			Address: cfg.Stellar().YieldRegistryContract(),
		},
		func(vaultID uuid.UUID, previousBPS, currentBPS uint32) {
			wsHub.BroadcastEvent(ws.Event{
				Channel: "vaults:global",
				Type:    ws.EventYieldAccrued,
				Data: map[string]any{
					"vault_id":     vaultID.String(),
					"previous_bps": previousBPS,
					"current_bps":  currentBPS,
				},
			})
		},
	).WithLogger(baseLogger.WithGroup("apy-refresher"))
	apyCtx, cancelAPY := context.WithCancel(context.Background())
	defer cancelAPY()
	go func() {
		if err := apyRefresher.Run(apyCtx); err != nil && !errors.Is(err, context.Canceled) {
			baseLogger.Error("apy refresher stopped", "error", err.Error())
		}
	}()

	// Background reconciliation of pending transactions: polls Horizon so a
	// transaction's status is confirmed even when the client never calls
	// GET /api/v1/transactions/{hash}. Broadcasts a WebSocket event on change.
	var nudgeEngineSvc *service.NudgeEngineService

	txPoller := service.NewTransactionPoller(
		service.TransactionPollerConfig{
			Enabled:  cfg.TransactionPoller().Enabled(),
			Interval: cfg.TransactionPoller().Interval(),
			MinAge:   cfg.TransactionPoller().MinAge(),
		},
		transactionService,
		func(ctx context.Context, tx transaction.Transaction) {
			wsHub.BroadcastEvent(transactionStatusEvent(tx))
			// A confirmed deposit/withdrawal changes settled net worth: drop the
			// cached valuation and push a fresh one (#832 event-driven invalidation).
			if v, err := vaultRepository.GetVault(ctx, tx.VaultID); err == nil {
				valuationService.Invalidate(v.UserID)
			}
			if tx.Status == transaction.StatusCompleted && tx.Type == transaction.TypeDeposit {
				if v, err := vaultRepository.GetVault(ctx, tx.VaultID); err == nil {
					_ = nudgeOutcomeService.RecordDeposit(ctx, v.UserID, time.Now())
					if nudgeEngineSvc != nil {
						_ = nudgeEngineSvc.EvaluateAndDispatch(ctx, v.UserID)
					}
				}
			}
		},
		baseLogger.WithGroup("tx-poller"),
	)
	// Reconciliation and pending-submission metrics (#1108). Without this the
	// poller's findings reach the log only, so a divergence — a balance that
	// disagrees with the chain — is invisible to alerting.
	txPoller.SetMetrics(appMetrics)
	pollerCtx, cancelPoller := context.WithCancel(context.Background())
	defer cancelPoller()
	go txPoller.Run(pollerCtx)

	// Age the reconcile gauge between passes (#1108). RecordReconcileRun
	// resets it to zero on each completed pass; nothing else would move it, so
	// a poller that dies would leave the gauge frozen at zero and read as
	// "just reconciled" forever — the same failure mode the indexer's
	// lag_last_sample_age gauge exists to prevent.
	go func() {
		const reconcileAgeInterval = 15 * time.Second
		ticker := time.NewTicker(reconcileAgeInterval)
		defer ticker.Stop()
		for {
			select {
			case <-pollerCtx.Done():
				return
			case <-ticker.C:
				last := txPoller.LastTickEnd()
				if last.IsZero() {
					continue
				}
				appMetrics.SetReconcileLastRunAge(time.Since(last))
			}
		}
	}()

	// notificationRateLimit/-Window bound how many notifications a user can
	// receive per category in a burst (#829's "a burst of deposits does not
	// produce a burst of near-identical notifications"). Safety-category
	// events bypass this entirely (see notifications.Category doc comment).
	const notificationRateLimit = 20
	const notificationRateWindow = 5 * time.Minute
	notificationRateLimiter := middleware.NewLimiter(redisClient, "notifications", notificationRateLimit, notificationRateWindow)

	// notificationDedup is process-local when Redis isn't configured, and
	// Redis-backed (cross-instance) otherwise — same dual-mode pattern as
	// middleware.NewLimiter above.
	var notificationDedup notifications.Deduplicator = notifications.NewInMemoryDeduplicator()
	if redisClient != nil {
		notificationDedup = notifications.NewRedisDeduplicator(redisClient)
	}

	notificationDispatcher := notifications.New(
		[]notifications.Channel{
			notifications.NewWebSocketChannel(wsHub),
		},
		notificationRepository,
		nil,
		notifications.WithDeduplicator(notificationDedup),
		notifications.WithRateLimiter(notificationRateLimiter),
	)

	// notificationDispatcher2 carries the real Push channel — separate from
	// notificationDispatcher above (WebSocket-only) because a failed
	// WebSocket delivery is never retried by design (see
	// notifications.RetryEnqueuer's doc comment), while a failed Push send
	// is. NoopPushSender is the same placeholder nudgeNotificationDispatcher
	// already uses below — a real provider integration is deliberately
	// deferred (see #829's commit message).
	notificationDispatcher2 := notifications.New(
		[]notifications.Channel{
			notifications.NewPushChannel(notifications.NoopPushSender{}, notificationRepository),
		},
		notificationRepository,
		nil,
		notifications.WithDeduplicator(notificationDedup),
		notifications.WithRateLimiter(notificationRateLimiter),
	)

	var ready atomic.Bool
	ready.Store(true)

	depHTTPClient := &http.Client{Timeout: cfg.Startup().DependencyTimeout()}

	healthDependencies := healthDeps{
		ready:        &ready,
		pingDB:       pgPool.Ping,
		poolStats:    pgxPoolStats(pgPool),
		probeTimeout: cfg.Database().ConnectionTimeout(),
		httpClient:   depHTTPClient,
		horizonURL:   cfg.Stellar().HorizonURL(),
		rpcURL:       cfg.Stellar().RPCURL(),
		startedAt:    startedAt,
		environment:  cfg.Environment(),
		buildVersion: version,
		buildCommit:  buildCommit(),
		breakers:     chainBreakers.readers(),
	}
	// Left nil when Redis is unconfigured, so readiness does not fail an
	// instance that is deliberately running on the in-memory fallbacks.
	if redisClient != nil {
		healthDependencies.pingRedis = func(ctx context.Context) error {
			return redisClient.Ping(ctx).Err()
		}
	}

	mux := http.NewServeMux()
	registerHealthRoutes(mux, healthDependencies)
	yieldHarvestHandler := handler.NewYieldHarvestHandler(yieldHarvestService)
	yieldHarvestHandler.Register(mux)

	vaultHandler.Register(mux)

	// Read-only history for the fair-exit queue (#814), penalty escrow
	// (#805), and slippage-safe rebalance (#810) event projections.
	fairExitRepo := postgres.NewFairExitRepository(db)
	fairExitHandler := handler.NewFairExitHandler(vaultService, fairExitRepo)
	fairExitHandler.Register(mux)

	portfolioHandler.Register(mux)
	valuationHandler.Register(mux)
	transactionHandler.Register(mux)

	// Unified activity feed (deposits/withdrawals/rebalances/yield harvests)
	// backing the dApp's transaction-history page.
	activityRepository := postgres.NewActivityRepository(db)
	activityService := service.NewActivityService(activityRepository)
	activityHandler := handler.NewActivityHandler(activityService)
	activityHandler.Register(mux)
	userHandler.Register(mux)
	notificationHandler.Register(mux)
	adminHandler.Register(mux)
	handler.NewMoneyPathSwitchHandler(moneyPathSwitchService).Register(mux)
	authHandler.Register(mux)
	rateHandler.Register(mux)
	performanceHandler.Register(mux)
	tvlHandler.Register(mux)
	projectionHandler.Register(mux)
	analyticsHandler := handler.NewAnalyticsHandler(performanceService)
	analyticsHandler.Register(mux)

	// Risk service
	riskService := services.NewRiskService(vaultRepository, db)
	riskHandler := handler.NewRiskHandler(riskService)
	riskHandler.Register(mux)

	// Vault analytics (APY volatility, Sharpe, Sortino, drawdown, win rate)
	vaultAnalyticsSvc := service.NewVaultAnalyticsService(performanceRepository)
	vaultAnalyticsHandler := handler.NewVaultAnalyticsHandler(vaultAnalyticsSvc)
	vaultAnalyticsHandler.Register(mux)

	// Yield opportunities (DeFiLlama Stellar pools)
	yieldSvc := service.NewYieldService("")
	yieldSvc.SetHTTPClient(appMetrics.InstrumentClient(
		&http.Client{Timeout: 15 * time.Second}, metrics.UpstreamDeFiLlama,
	))
	// Warm the Stellar yield cache in the background so the first user request
	// doesn't pay the DeFiLlama round-trip (#667). Failure is non-fatal: the
	// lazy-load path still works.
	go func() {
		warmCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		start := time.Now()
		if pools, err := yieldSvc.WarmCache(warmCtx); err != nil {
			baseLogger.Warn("yield cache warm failed", "error", err)
		} else {
			baseLogger.Info("yield cache warmed", "chain", "Stellar", "pools", pools, "duration_ms", time.Since(start).Milliseconds())
		}
	}()
	yieldBookmarkSvc := service.NewYieldBookmarkService(db, yieldSvc)
	protocolTVLRepo := postgres.NewProtocolTVLRepository(db)
	yieldHandler := handler.NewYieldHandler(yieldSvc, yieldBookmarkSvc)
	yieldHandler.SetTVLRepository(protocolTVLRepo)
	yieldHandler.Register(mux)
	yieldBookmarkHandler := handler.NewYieldBookmarkHandler(yieldBookmarkSvc)
	yieldBookmarkHandler.Register(mux)

	// Protocol health checker — alerts users when a protocol's TVL drops >20% in 24h.
	protocolHealthChecker := scheduler.NewProtocolHealthChecker(
		scheduler.ProtocolHealthConfig{
			Enabled:  true,
			Interval: 30 * time.Minute,
		},
		vaultRepository,
		yieldSvc,
		protocolTVLRepo,
		scheduler.DispatcherProtocolHealthNotifier{Dispatcher: notificationDispatcher},
		baseLogger.WithGroup("protocol-health"),
	)
	protocolHealthChecker.SetLeaderChecker(schedulerLeadership)

	// Predictive deterioration scoring (#857): a continuous, graduated
	// signal alongside the fixed 24h/20%-drop check above. apySnapshotRepo
	// is hoisted here (rather than where it's constructed further down,
	// alongside the APY history endpoint) since the deterioration engine
	// needs both TVL and APY snapshot history to compute indicators.
	apySnapshotRepo := postgres.NewAPYSnapshotRepository(db)
	deteriorationRepo := postgres.NewDeteriorationRepository(db)
	deteriorationEngine := scheduler.NewDeteriorationEngine(
		protocolTVLRepo,
		apySnapshotRepo,
		deteriorationRepo,
		adminService,
		notificationDispatcher,
		baseLogger.WithGroup("protocol-deterioration"),
	)
	protocolHealthChecker.SetDeteriorationEngine(deteriorationEngine)

	protocolHealthCtx, cancelProtocolHealth := context.WithCancel(context.Background())
	defer cancelProtocolHealth()
	go protocolHealthChecker.Run(protocolHealthCtx)

	// APY deviation alert (#846): notifies a vault's users when its APY drops
	// >20% from its 30-day mean. Notification-only, but gated behind
	// scheduler leadership like the other four jobs (see
	// APYDeviationJob.SetLeaderChecker for the shared dedup-race rationale).
	// Previously built and tested (apy_deviation.go/apy_deviation_adapters.go)
	// but never wired into main.go before #846.
	apyDeviationJob := scheduler.NewAPYDeviationJob(
		scheduler.APYDeviationJobFromEnv(),
		scheduler.VaultAPYListerFunc(func(ctx context.Context) ([]scheduler.APYVaultInfo, error) {
			infos, err := vaultRepository.ListActiveVaultsForAPYCheck(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]scheduler.APYVaultInfo, len(infos))
			for i, v := range infos {
				out[i] = scheduler.APYVaultInfo{
					ID:                 v.ID,
					UserID:             v.UserID,
					Currency:           v.Currency,
					LastAPYAlertSentAt: v.LastAPYAlertSentAt,
				}
			}
			return out, nil
		}),
		performanceRepository,
		vaultRepository,
		notificationDispatcher,
		baseLogger.WithGroup("apy-deviation"),
	)
	apyDeviationJob.SetLeaderChecker(schedulerLeadership)
	apyDeviationCtx, cancelAPYDeviation := context.WithCancel(context.Background())
	defer cancelAPYDeviation()
	go apyDeviationJob.Run(apyDeviationCtx)

	// User watchlist
	watchlistSvc := service.NewWatchlistService(db)
	watchlistHandler := handler.NewWatchlistHandler(watchlistSvc)
	watchlistHandler.Register(mux)

	// Savings goals
	savingsGoalRepo := postgres.NewSavingsGoalRepository(db)
	nudgeCopyGen := service.CompositeCopyGenerator{
		Template: nudge.TemplateCopyGenerator{},
	}

	savingsStreakRepo := postgres.NewSavingsStreakRepository(db)

	// Nudges dispatch over their own push-enabled dispatcher: the shared
	// `notificationDispatcher` above is constructed with zero channels
	// (websocket is still disabled), so nudges need their own live channel
	// rather than silently persisting-but-never-delivering.
	nudgeNotificationDispatcher := notifications.New(
		[]notifications.Channel{
			notifications.NewPushChannel(notifications.NoopPushSender{}, notificationRepository),
		},
		notificationRepository,
		nil,
		notifications.WithDeduplicator(notificationDedup),
		notifications.WithRateLimiter(notificationRateLimiter),
	)
	nudgeEngineSvc = service.NewNudgeEngineService(
		savingsGoalRepo,
		savingsStreakRepo,
		transactionRepository,
		userRepository,
		usersignal.HeuristicSegmentProvider{UserRepo: userRepository, GoalRepo: savingsGoalRepo},
		usersignal.HeuristicEngagementProvider{UserRepo: userRepository},
		usersignal.HeuristicTimingProvider{Activity: activityEventRepo, UserRepo: userRepository},
		nudgeHistoryRepo,
		nudgeHistoryRepo,
		nudgeHistoryRepo,
		nudgeCopyGen,
		service.DispatcherNudgeNotifier{Dispatcher: nudgeNotificationDispatcher},
	)

	nudgeEngineJob := scheduler.NewNudgeEngineJob(
		scheduler.NudgeEngineConfig{
			Enabled:  true,
			Interval: 1 * time.Hour,
		},
		savingsGoalRepo,
		nudgeEngineSvc,
		baseLogger.WithGroup("nudge-engine"),
	)
	nudgeCtx, cancelNudge := context.WithCancel(context.Background())
	defer cancelNudge()
	go nudgeEngineJob.Run(nudgeCtx)

	// Durable async job queue (#824): the shared worker pool and producer
	// client. Hoisted here (rather than further down where the harvest/
	// recurring-deposit producers are wired) so the webhook delivery
	// producer below can also enqueue onto it; handlers are registered on
	// jobWorker further down, before the worker starts.
	jobQueueRepo := postgres.NewJobRepository(db)
	jobQueueMetrics := jobqueue.NewStdMetrics()
	jobQueueClient := jobqueue.NewClient(jobQueueRepo, jobQueueMetrics)

	// Outbound webhooks (#836): subscriptions with SSRF-validated targets and
	// encrypted signing secrets; delivery goes through the durable job queue
	// above (WebhookDeliveryJobHandler, registered on jobWorker further down)
	// rather than the old ad-hoc goroutine+sleep retry, so it gets the same
	// at-least-once/backoff/dead-letter guarantees as harvest and recurring
	// deposits. accountCipher may be nil (unconfigured deployment) — Register
	// then fails with service.ErrWebhookCipherNotConfigured rather than
	// panicking.
	webhookRepo := postgres.NewWebhookRepository(db)
	webhookDeliveryRepo := postgres.NewWebhookDeliveryRepository(db)
	webhookSvc := service.NewWebhookService(webhookRepo, webhookDeliveryRepo, accountCipher, jobQueueClient)
	webhookSvc.SetLogger(baseLogger.WithGroup("webhook-service"))
	webhookHandler := handler.NewWebhookHandler(webhookSvc)
	webhookHandler.Register(mux)
	webhookLimiter := middleware.NewLimiter(redisClient, "webhook-delivery", cfg.JobQueue().DefaultConcurrency()*2, time.Minute)
	webhookDeliveryHandler := service.NewWebhookDeliveryJobHandler(
		webhookRepo,
		webhookDeliveryRepo,
		accountCipher,
		webhookLimiter,
		service.DispatcherSuspensionNotifier{Dispatcher: notificationDispatcher2},
		baseLogger.WithGroup("webhook-delivery"),
	)

	// Per-goal notification preferences (mute/digest frequency).
	goalNotificationRepo := postgres.NewGoalNotificationRepository(db)
	goalNotificationPrefSvc := service.NewGoalNotificationPreferenceService(goalNotificationRepo, savingsGoalRepo)
	// The milestone notifier chain. Since #1049 it is driven by the outbox
	// relay's queue job (registered on jobWorker further down) rather than
	// called inline, so every member is handed the milestone's dedupe key
	// and must be idempotent with respect to it — see GoalMilestoneNotifier.
	goalMilestoneNotifier := service.CompositeGoalMilestoneNotifier{
		Notifiers: []service.GoalMilestoneNotifier{
			service.DispatcherGoalMilestoneNotifier{
				Dispatcher:  notificationDispatcher2,
				Preferences: goalNotificationRepo,
			},
			service.NudgeEngineGoalMilestoneNotifier{NudgeEngine: nudgeEngineSvc},
			service.WebhookGoalMilestoneNotifier{
				Svc:    webhookSvc,
				Logger: baseLogger.WithGroup("webhook-milestone"),
			},
		},
	}
	savingsGoalSvc := service.NewSavingsGoalService(
		savingsGoalRepo,
		vaultRepository,
		goalMilestoneNotifier,
	)
	savingsGoalSvc.SetOutcomeRecorder(nudgeOutcomeService)
	savingsGoalSvc.SetStreakRepository(savingsStreakRepo)
	savingsGoalSvc.SetStreakNotifier(service.DispatcherStreakMilestoneNotifier{Dispatcher: notificationDispatcher2})
	savingsGamificationRepo := postgres.NewSavingsGamificationRepository(db)
	savingsGamificationSvc := service.NewSavingsGamificationService(
		savingsGamificationRepo,
		service.DispatcherGamificationNotifier{Dispatcher: notificationDispatcher2},
	)
	savingsGoalSvc.SetGamificationRecorder(savingsGamificationSvc)
	savingsGamificationHandler := handler.NewSavingsGamificationHandler(savingsGamificationSvc)
	savingsGamificationHandler.Register(mux)
	savingsGoalSvc.SetTemplateRepository(goalTemplateRepo)
	// Honor each goal's auto_compound preference when its vault is harvested (#task1).
	vaultService.SetGoalYieldRouter(savingsGoalSvc)

	minDeposit, _ := decimal.NewFromString(cfg.RecurringDeposit().MinDepositAmount())
	savingsScheduleRepo := postgres.NewSavingsScheduleRepository(db)
	savingsScheduleSvc := service.NewSavingsScheduleService(savingsScheduleRepo, savingsGoalRepo, vaultRepository, minDeposit)
	savingsGoalHandler := handler.NewSavingsGoalHandler(savingsGoalSvc, savingsScheduleSvc)
	savingsGoalHandler.SetNotificationPreferenceManager(goalNotificationPrefSvc)
	savingsGoalHandler.Register(mux)

	goalNotificationDigestJob := scheduler.NewGoalNotificationDigestJob(
		scheduler.GoalNotificationDigestConfig{Enabled: true, Interval: time.Hour},
		goalNotificationRepo,
		notificationDispatcher2,
		baseLogger.WithGroup("goal-notification-digest"),
	)
	goalDigestCtx, cancelGoalDigest := context.WithCancel(context.Background())
	defer cancelGoalDigest()
	go goalNotificationDigestJob.Run(goalDigestCtx)

	savingsScheduleHandler := handler.NewSavingsScheduleHandler(savingsScheduleSvc)
	savingsScheduleHandler.Register(mux)

	// Goal deadline reminders are handled by the unified nudge engine
	// (nudge.NudgeTypeDeadlineReminder / EvaluateDeadlineReminderTrigger)
	// rather than a dedicated scheduler job — see nudgeEngineJob below.

	// Scheduled and recurring deposits run through their own vault service
	// instance. They are real user deposits, so they carry the same SLI
	// instrumentation: excluding them would understate both the numerator and
	// the denominator of the deposit success rate.
	ledgerVaultService := service.NewVaultService(vaultRepository)
	ledgerVaultService.SetMetrics(appMetrics)
	// Recurring deposits are still deposits: subject to the same launch
	// caps and the same balance-change audit trail as the interactive path
	// (#1119, #1124).
	ledgerVaultService.SetCapsChecker(vaultService.CapsChecker())
	ledgerVaultService.SetBalanceAuditRecorder(postgres.NewBalanceAuditRepository(db))
	scheduledDepositSvc := service.NewScheduledDepositService(ledgerVaultService)
	goalProgressSvc := service.NewGoalProgressService(savingsGoalRepo)

	// jobQueueRepo / jobQueueMetrics / jobQueueClient (#824) are constructed
	// earlier, alongside the webhook subscription wiring (#836), since that
	// producer needs jobQueueClient too. Handlers (including the webhook
	// delivery handler) are registered on jobWorker below, before it starts.

	// Durable retry for failed notification deliveries (#829), now that the
	// job queue client exists. Only notificationDispatcher2 gets a
	// RetryEnqueuer: it's the only one of the two dispatchers above with a
	// real Push channel registered. notificationDispatcher only has
	// WebSocket registered, and WebSocket failures are never retried by
	// design (see notifications.RetryEnqueuer's doc comment) — wiring retry
	// there would only ever enqueue jobs for Email/Push that it has no
	// adapter to actually redeliver.
	notificationDispatcher2.SetRetryEnqueuer(notifications.NewJobQueueRetryEnqueuer(jobQueueClient))

	// Recurring deposit sweep (#846): classified SINGLETON (money-moving —
	// see RecurringDepositJob's doc comment). The sweep loop itself only
	// enqueues a durable per-occurrence job onto jobQueueClient rather than
	// recording the deposit inline; RecurringDepositJobHandler (registered
	// on jobWorker below) does the actual ledger write, giving it the same
	// lease/retry/backoff at-least-once guarantees as the harvest engine.
	recurringDepositJob := scheduler.NewRecurringDepositJob(
		scheduler.RecurringDepositConfig{
			Enabled:  cfg.RecurringDeposit().Enabled(),
			Interval: cfg.RecurringDeposit().Interval(),
		},
		savingsScheduleRepo,
		jobQueueClient,
		goalProgressSvc,
		baseLogger.WithGroup("recurring-deposit"),
	)
	recurringDepositJob.SetLeaderChecker(schedulerLeadership)
	recurringCtx, cancelRecurring := context.WithCancel(context.Background())
	defer cancelRecurring()
	go recurringDepositJob.Run(recurringCtx)

	// Savings goal soft-delete recovery purge (#924): hard-deletes goals
	// whose deleted_at is older than savingsgoal.SavingsGoalRecoveryWindow.
	// Runs daily; leader-elected like the other sweep jobs to avoid every
	// instance racing to purge the same rows.
	savingsGoalPurgeJob := scheduler.NewSavingsGoalPurgeJob(
		savingsGoalRepo,
		baseLogger.WithGroup("savings-goal-purge"),
	)
	savingsGoalPurgeJob.SetLeaderChecker(schedulerLeadership)
	savingsGoalPurgeCtx, cancelSavingsGoalPurge := context.WithCancel(context.Background())
	defer cancelSavingsGoalPurge()
	go savingsGoalPurgeJob.Run(savingsGoalPurgeCtx, 24*time.Hour)

	// Data retention sweep (#1226): hard-deletes activity_events and
	// nudge_dispatch_log (nudge_outcomes cascades) rows past their retention
	// window — see docs/data-retention.md for the policy and
	// DataRetentionConfig's defaults. Deliberately does NOT touch audit_logs,
	// KYC records, or processed_events — the policy doc explains why each of
	// those is out of scope for this job. Runs daily, leader-elected like the
	// other sweep jobs, and audit-logs every deletion via the same
	// auditLogger every other audited action in this file uses.
	dataRetentionJob := scheduler.NewDataRetentionJob(
		activityEventRepo,
		nudgeHistoryRepo,
		auditLogger,
		scheduler.DataRetentionConfig{},
		baseLogger.WithGroup("data-retention"),
	)
	dataRetentionJob.SetLeaderChecker(schedulerLeadership)
	dataRetentionCtx, cancelDataRetention := context.WithCancel(context.Background())
	defer cancelDataRetention()
	go dataRetentionJob.Run(dataRetentionCtx, 24*time.Hour)

	jobWorker := jobqueue.NewWorker(
		jobQueueRepo,
		jobqueue.Config{
			Enabled:            cfg.JobQueue().Enabled(),
			PollInterval:       cfg.JobQueue().PollInterval(),
			Lease:              cfg.JobQueue().Lease(),
			HeartbeatInterval:  cfg.JobQueue().HeartbeatInterval(),
			JobTimeout:         cfg.JobQueue().JobTimeout(),
			DefaultConcurrency: cfg.JobQueue().DefaultConcurrency(),
			Backoff: jobqueue.BackoffConfig{
				Base: cfg.JobQueue().BackoffBase(),
				Max:  cfg.JobQueue().BackoffMax(),
			},
			StatsInterval: cfg.JobQueue().StatsInterval(),
			DrainTimeout:  cfg.JobQueue().DrainTimeout(),
		},
		baseLogger.WithGroup("job-queue"),
		jobQueueMetrics,
	)
	// Yield harvest orchestration engine (#845): evaluates vaults on a cadence,
	// applies the economic gate (harvest iff accrued yield > gas + margin),
	// defers under network congestion, and submits harvests as idempotent jobs
	// on the queue above. Its job handler is registered on the worker before Run.
	harvestMargin, err := decimal.NewFromString(cfg.Harvest().Margin())
	if err != nil {
		return fmt.Errorf("HARVEST_ENGINE_MARGIN: %w", err)
	}
	harvestGasFee, err := decimal.NewFromString(cfg.Harvest().GasFee())
	if err != nil {
		return fmt.Errorf("HARVEST_ENGINE_GAS_FEE: %w", err)
	}
	harvestExecutor := harvest.NewServiceExecutor(vaultService, userService)
	jobWorker.Register(harvest.DefaultJobType,
		harvest.NewJobHandler(harvestExecutor, baseLogger.WithGroup("harvest-job")), 0)

	// Notification retry (#829): redelivers a failed Push notification via
	// notificationDispatcher2 (see the RetryEnqueuer wiring above for why
	// only that dispatcher is used here).
	jobWorker.Register(notifications.NotificationRetryJobType,
		notifications.NewNotificationRetryJobHandler(notificationDispatcher2), 0)

	// Recurring-deposit occurrence handler (#846): processes the jobs
	// recurringDepositJob (above) enqueues. Fixes the #846 idempotency bug —
	// see scheduled_deposit_adapters.go's RecordScheduledDeposit doc comment.
	jobWorker.Register(scheduler.RecurringDepositJobType,
		scheduler.NewRecurringDepositJobHandler(
			scheduledDepositSvc,
			savingsScheduleRepo,
			scheduler.NotificationDepositNotifier{Dispatcher: notificationDispatcher},
			baseLogger.WithGroup("recurring-deposit-handler"),
		), 0)

	// Webhook delivery (#836): one attempt per job invocation; the queue's
	// own retry/backoff/dead-letter drives everything past that (see
	// WebhookDeliveryJobHandler's doc comment). Concurrency uses the
	// worker's default rather than a dedicated limit — per-subscription
	// throttling is handled inside the handler via webhookLimiter, so a
	// wide worker-level concurrency here is safe.
	jobWorker.Register(service.WebhookDeliveryJobType, webhookDeliveryHandler, 0)

	// Transactional outbox (#1049). The relay hands outbox rows to this same
	// worker pool; these three handlers are the side effects it can route
	// to. Registering them here — before Run — is what makes an event type
	// routable at all: the relay dead-letters anything it has no route for,
	// on the grounds that retrying cannot conjure a handler.
	jobWorker.Register(service.GoalMilestoneJobType,
		service.NewGoalMilestoneJobHandler(goalMilestoneNotifier, baseLogger.WithGroup("goal-milestone-job")), 0)
	jobWorker.Register(service.WebhookFanoutJobType,
		service.NewWebhookFanoutJobHandler(webhookSvc, baseLogger.WithGroup("webhook-fanout")), 0)
	jobWorker.Register(notifications.NotificationSendJobType,
		notifications.NewNotificationSendJobHandler(notificationDispatcher2), 0)

	outboxRepo := postgres.NewOutboxRepository(db)
	outboxMetrics := outbox.NewStdMetrics()
	// Every relay instance runs: ClaimDue takes row locks with SKIP LOCKED,
	// so instances divide the backlog rather than duplicating it, and a
	// leader election here would only turn a horizontal scale-out into a
	// single point of failure for every side effect in the system.
	outboxRelay := outbox.NewRelay(
		outboxRepo,
		jobQueueClient,
		jobQueueRepo,
		outbox.Routes{
			service.OutboxEventGoalMilestone:          service.GoalMilestoneJobType,
			service.OutboxEventWebhookFanout:          service.WebhookFanoutJobType,
			notifications.OutboxEventNotificationSend: notifications.NotificationSendJobType,
		},
		outbox.RelayConfig{
			Enabled:       cfg.Outbox().Enabled(),
			PollInterval:  cfg.Outbox().PollInterval(),
			BatchSize:     cfg.Outbox().BatchSize(),
			Lease:         cfg.Outbox().Lease(),
			Backoff:       cfg.Outbox().Backoff(),
			StatsInterval: cfg.Outbox().StatsInterval(),
		},
		baseLogger.WithGroup("outbox-relay"),
		outboxMetrics,
	)
	outboxCtx, cancelOutbox := context.WithCancel(context.Background())
	defer cancelOutbox()
	go func() { _ = outboxRelay.Run(outboxCtx) }()

	// Retention: without it the outbox only ever grows, on the write path of
	// every domain transaction that inserts into it. Leader-gated because
	// pruning from every instance is correct but wasteful.
	outboxRetentionJob := outbox.NewRetentionJob(outboxRepo, outbox.RetentionConfig{
		DispatchedRetention: cfg.Outbox().DispatchedRetention(),
		DeadRetention:       cfg.Outbox().DeadRetention(),
	}, baseLogger.WithGroup("outbox-retention"), outboxMetrics)
	outboxRetentionJob.SetLeaderChecker(schedulerLeadership)
	outboxRetentionCtx, cancelOutboxRetention := context.WithCancel(context.Background())
	defer cancelOutboxRetention()
	go outboxRetentionJob.Run(outboxRetentionCtx, cfg.Outbox().RetentionInterval())

	harvestEngine := harvest.New(
		harvest.Config{
			Enabled:  cfg.Harvest().Enabled(),
			Interval: cfg.Harvest().Interval(),
			Margin:   harvestMargin,
			Window:   cfg.Harvest().Window(),
		},
		harvest.NewRepoSource(vaultRepository),
		harvest.NewStaticGasOracle(harvestGasFee),
		jobQueueClient,
		baseLogger.WithGroup("harvest-engine"),
	)
	harvestHandler := handler.NewHarvestHandler(harvestEngine)
	harvestHandler.Register(mux)
	harvestCtx, cancelHarvest := context.WithCancel(context.Background())
	defer cancelHarvest()
	go harvestEngine.Run(harvestCtx)

	jobQueueCtx, cancelJobQueue := context.WithCancel(context.Background())
	defer cancelJobQueue()
	go func() {
		if err := jobWorker.Run(jobQueueCtx); err != nil && !errors.Is(err, context.Canceled) {
			baseLogger.Error("job queue worker stopped", "error", err.Error())
		}
	}()

	// User vault rebalance (suggestions + execution)
	vaultRebalanceSvc := service.NewVaultRebalanceService(vaultRepository, adminService)
	vaultHandler.SetRebalanceService(vaultRebalanceSvc)

	// Rebalance rate limiter (3 per hour per user)
	rebalanceRateLimiter := middleware.WalletRateLimiter(
		cfg.RateLimit().RebalanceLimit(),
		cfg.RateLimit().RebalanceWindow(),
		walletKeyFromContext,
	)
	vaultHandler.SetRebalanceRateLimiter(rebalanceRateLimiter)

	// Ledger balance verification job: recomputes balances from raw entries and asserts equality
	ledgerVerificationJob := scheduler.NewLedgerBalanceVerificationJob(
		scheduler.VerificationConfig{Enabled: true, Interval: 10 * time.Minute},
		ledgerRepository,
		baseLogger.WithGroup("ledger-verification"),
	)
	ledgerVerificationJob.SetLeaderChecker(schedulerLeadership)
	verificationCtx, cancelVerification := context.WithCancel(context.Background())
	defer cancelVerification()
	go ledgerVerificationJob.Run(verificationCtx)

	// Ledger reconciliation job: compares ledger vault-pool vs on-chain and sum
	// user positions vs share price.
	chainReaderForLedger := &ledgerChainReaderAdapter{
		contractReader: stellarpkg.NewContractReader(
			cfg.Stellar().RPCURL(),
			cfg.Stellar().NetworkPassphrase(),
			"",
		),
	}
	ledgerReconciliationJob := scheduler.NewLedgerReconciliationJob(scheduler.LedgerReconciliationDeps{
		LedgerRepo:  ledgerRepository,
		VaultLister: &reconciliationVaultListerAdapter{vaultRepo: vaultRepository},
		ChainReader: chainReaderForLedger,
		Logger:      baseLogger.WithGroup("ledger-reconciliation"),
		Config:      ledgerDomainConfig(),
	})
	ledgerReconciliationJob.SetLeaderChecker(schedulerLeadership)
	reconciliationCtx, cancelReconciliation := context.WithCancel(context.Background())
	defer cancelReconciliation()
	go ledgerReconciliationJob.Run(reconciliationCtx)

	performanceSnapshotsHandler := handler.NewPerformanceSnapshotsHandler(performanceService)
	performanceSnapshotsHandler.Register(mux)

	mux.HandleFunc("GET /ws", wsHub.ServeWs)

	// APY snapshot scheduler and history endpoint (apySnapshotRepo is
	// constructed earlier, alongside the deterioration engine wiring above).
	apySvc := service.NewAPYService(apySnapshotRepo)
	apyHandler := handler.NewAPYHandler(apySvc)
	apyHandler.Register(mux)
	apySchedulerCtx, cancelAPYScheduler := context.WithCancel(context.Background())
	defer cancelAPYScheduler()
	go apySvc.StartScheduler(apySchedulerCtx)

	// walletBindingCacheTTL bounds how long a stale wallet binding can still
	// be accepted after the account's wallet changes. Short enough that a
	// relink takes effect promptly, long enough to keep the check off the
	// database on the hot path.
	const walletBindingCacheTTL = 60 * time.Second

	// Defined in the middleware package so the authorization matrix test
	// exercises the same table the server serves, rather than a copy of it.
	authRules := middleware.ProductionAuthRules()
	authenticator := middleware.Authenticate(cfg.Auth().Secret(), cfg.Auth().ServiceAPIKey(), authRules, revocationCache)
	// walletBinding ties a session to the wallet it was issued for (#1102). It
	// rejects a token replayed against a different wallet's endpoints, and a
	// token whose wallet is no longer the one linked to the account, so
	// relinking a wallet invalidates sessions minted before the change.
	//
	// The resolver memoises the account's wallet briefly: the check runs on
	// every authenticated request, and the lookup behind it is a row read.
	// The TTL bounds how long a superseded binding can still be honoured.
	walletBinding := middleware.WalletBindingCheck(
		middleware.NewCachedWalletResolver(userRepository, walletBindingCacheTTL),
	)
	// Tell the rate-limit client-IP extractor how many trusted proxies sit in
	// front of the API so it derives the originating client IP from
	// X-Forwarded-For instead of collapsing all traffic onto the proxy address.
	middleware.ConfigureClientIP(cfg.RateLimit().TrustedProxyCount())

	// globalLimiter bounds every request per client IP, but skips liveness /
	// readiness / metrics endpoints so orchestrators can always reach them. It is
	// distributed across instances when Redis is configured.
	globalLimiter := middleware.GlobalRateLimiter(
		middleware.NewLimiter(redisClient, "global", cfg.RateLimit().GlobalLimit(), cfg.RateLimit().GlobalWindow()),
		[]string{"/health", "/healthz", "/readyz", "/metrics"},
	)
	// authRouteLimiter applies a strict per-IP limit to the unauthenticated auth
	// handshake to blunt credential-stuffing. Keyed by IP because no user exists
	// yet at challenge/verify time.
	authRouteLimiter := middleware.SensitiveRouteLimiter(
		middleware.NewLimiter(redisClient, "auth", cfg.RateLimit().AuthLimit(), cfg.RateLimit().AuthWindow()),
		[]middleware.RouteMatch{
			{Method: http.MethodPost, Path: "/api/v1/auth/challenge"},
			{Method: http.MethodPost, Path: "/api/v1/auth/verify"},
		},
		"authentication rate limit exceeded",
	)
	// authGuard hardens the same handshake beyond request rate (nester#1104):
	// a per-wallet limit (the limiter above keys only on IP, so a distributed
	// client could flood the challenge store for one wallet without tripping
	// it), and a progressive lockout on repeated FAILURES tracked per wallet
	// and per IP. Slowing down does not evade the lockout, because the backoff
	// escalates with the failure count rather than resetting with time.
	authLockoutCfg := middleware.AuthLockoutConfig{
		Threshold: cfg.RateLimit().AuthFailureThreshold(),
		Window:    cfg.RateLimit().AuthFailureWindow(),
		Base:      cfg.RateLimit().AuthLockoutBase(),
		Max:       cfg.RateLimit().AuthLockoutMax(),
	}
	authGuard := middleware.NewAuthGuard(
		middleware.NewLimiter(redisClient, "authwallet", cfg.RateLimit().AuthLimit(), cfg.RateLimit().AuthWindow()),
		middleware.NewAuthLockout(redisClient, "wallet", authLockoutCfg),
		middleware.NewAuthLockout(redisClient, "ip", authLockoutCfg),
		appMetrics,
		[]middleware.AuthGuardStage{
			{Stage: metrics.AuthStageChallenge, Route: middleware.RouteMatch{Method: http.MethodPost, Path: "/api/v1/auth/challenge"}},
			{Stage: metrics.AuthStageVerify, Route: middleware.RouteMatch{Method: http.MethodPost, Path: "/api/v1/auth/verify"}},
		},
	).Middleware()
	// idempotencyMiddleware (#835) makes the designated write endpoints safe
	// to retry: a client-supplied Idempotency-Key header is required on
	// them, and a repeated key returns the original stored response instead
	// of re-executing the handler. Explicit per-route rather than blanket,
	// per the issue's own guidance — starting with the two endpoints most
	// exposed to "client retried after a lost response" (a deposit/withdraw
	// posted as a transaction, and creating a savings goal). Requires auth
	// context, so it must sit after authenticator.
	idempotencyStore := postgres.NewIdempotencyRepository(db)
	idempotencyRoutes := []middleware.RouteMatch{
		{Method: http.MethodPost, Path: "/api/v1/transactions"},
		{Method: http.MethodPost, Path: "/api/v1/users/savings-goals"},
	}
	idempotencyRoutes = append(idempotencyRoutes, middleware.VaultMoneyPathIdempotencyRoutes()...)
	idempotencyMiddleware := middleware.IdempotencyMiddleware(idempotencyStore, idempotencyRoutes)
	idempotencyPurgeCtx, cancelIdempotencyPurge := context.WithCancel(context.Background())
	defer cancelIdempotencyPurge()
	go runIdempotencyPurge(idempotencyPurgeCtx, idempotencyStore, baseLogger.WithGroup("idempotency-purge"))

	// costQuota meters downstream *work* per authenticated user, where the
	// limiters above meter request *count* per IP. Both apply: a caller can
	// sit well inside 100 requests/minute while saturating DeFiLlama and
	// Soroban RPC, because an expensive call and a profile read are not the
	// same request.
	//
	// Placed after the authenticator so it keys by user (falling back to IP
	// for anything still anonymous), and after idempotencyMiddleware so a
	// replayed idempotent write — which returns a stored response and calls
	// nothing downstream — is not charged as though it did.
	costQuotaLimiter := middleware.NewQuotaLimiter(
		redisClient,
		"cost",
		cfg.RateLimit().QuotaLimit(),
		cfg.RateLimit().QuotaWindow(),
		baseLogger.WithGroup("ratelimit-quota"),
	)
	if cfg.RateLimit().QuotaBypassToken() != "" && cfg.Environment() == "production" {
		baseLogger.Warn("RATELIMIT_QUOTA_BYPASS_TOKEN is set in production; " +
			"any caller holding it can bypass cost quotas entirely")
	}
	// A quota below the priciest route makes that route permanently
	// unreachable: the bucket can never hold enough tokens to pay for one
	// call, and the Retry-After we hand back would be a lie. Refuse to start
	// rather than serve an API with a silently dead endpoint.
	if cfg.RateLimit().QuotaEnabled() && cfg.RateLimit().QuotaLimit() < middleware.MaxRouteCost() {
		return fmt.Errorf(
			"RATELIMIT_QUOTA_LIMIT is %d but the most expensive route costs %d; "+
				"every call to it would be rejected forever",
			cfg.RateLimit().QuotaLimit(), middleware.MaxRouteCost())
	}
	if !cfg.RateLimit().QuotaEnabled() {
		baseLogger.Warn("cost-weighted rate limit quotas are disabled; " +
			"expensive routes are bounded only by request-rate limits")
	}
	costQuota := middleware.CostQuota(costQuotaLimiter, middleware.QuotaConfig{
		Enabled:         cfg.RateLimit().QuotaEnabled(),
		BypassToken:     cfg.RateLimit().QuotaBypassToken(),
		ExcludePrefixes: []string{"/health", "/healthz", "/readyz", "/metrics"},
		Logger:          baseLogger.WithGroup("ratelimit-quota"),
	})

	writeLimiter := middleware.WriteMethodRateLimiter(cfg.RateLimit().WriteLimit(), cfg.RateLimit().WriteWindow())
	walletLimiter := middleware.WalletRateLimiter(
		cfg.RateLimit().WalletLimit(),
		cfg.RateLimit().WalletWindow(),
		walletKeyFromContext,
	)
	cors := middleware.CORS(cfg.AllowedOrigins())

	server := &http.Server{
		Addr: cfg.Server().Address(),
		// cors is outermost of the request-processing middleware (after only
		// SecurityHeaders/RecoverPanic) so that rate-limit 429 responses from
		// globalLimiter and authRouteLimiter still carry CORS headers and remain
		// readable to browser clients. OPTIONS preflights are short-circuited by
		// cors and never reach the limiters.
		// The metrics middleware sits directly inside RecoverPanic and
		// outside every other layer, so that latency and status include time
		// spent in CORS, rate limiting, and auth. A 429 from the limiter or a
		// 401 from the authenticator is a real outcome of a real request; a
		// metrics layer placed further in would report the service as
		// healthy while the edge rejected everything.
		//
		// It resolves the route label from mux, which performs the same match
		// ServeHTTP will. r.Pattern is not usable here: the mux populates it
		// only on the request it hands to the matched handler, so at this
		// depth it is still empty.
		Handler: middleware.SecurityHeaders(cfg.Environment())(
			middleware.RecoverPanic(baseLogger)(
				appMetrics.Middleware(mux)(
					cors(
						// Inside cors so its Access-Control-Expose-Headers
						// covers the freshness headers, and outside every
						// rejection layer so a rate-limited or unauthorised
						// response still tells the client how current the
						// indexed data is.
						middleware.IndexerFreshness(indexerFreshness)(
							globalLimiter(
								authRouteLimiter(
									// Inside the per-IP limiter so an already
									// rate-limited request never reaches the
									// lockout bookkeeping (nester#1104).
									authGuard(
										writeLimiter(
											authenticator(
												walletBinding(
													idempotencyMiddleware(
														costQuota(
															walletLimiter(
																middleware.LimitRequestBody(1 * 1024 * 1024)(
																	middleware.Logging(baseLogger)(
																		middleware.Tracing(
																			cfg.Tracing().ServiceName(),
																			cfg.Tracing().LatencyThreshold(),
																		)(mux),
																	),
																),
															),
														),
													),
												),
											),
										),
									),
								),
							),
						),
					),
				),
			),
		),
		ReadTimeout:       cfg.Server().ReadTimeout(),
		ReadHeaderTimeout: cfg.Server().ReadHeaderTimeout(),
		WriteTimeout:      cfg.Server().WriteTimeout(),
		IdleTimeout:       cfg.Server().IdleTimeout(),
		MaxHeaderBytes:    cfg.Server().MaxHeaderBytes(),
	}

	baseLogger.Info("starting server",
		"addr", cfg.Server().Address(),
		"environment", cfg.Environment(),
		"version", version,
		"horizon_url", cfg.Stellar().HorizonURL(),
		"rpc_url", cfg.Stellar().RPCURL(),
		"network", stellarNetworkLabel(cfg.Stellar().NetworkPassphrase()),
		"auto_migrate", cfg.Startup().EnableAutoMigrate(),
	)

	// Submission reconciler (nester#1085). It resolves pending chain writes
	// by asking the chain about a specific transaction hash — the only thing
	// in the system permitted to decide that a submission ended.
	//
	// Its chain lookup is a read-only invoker (nil signer), deliberately
	// independent of whether this deployment can sign: submissions left
	// pending by a previous deployment still need resolving, and reconciling
	// requires no key material.
	if reconcileLookup, err := stellarpkg.NewContractInvokerWithSigner(
		cfg.Stellar().RPCURL(),
		cfg.Stellar().HorizonURL(),
		cfg.Stellar().NetworkPassphrase(),
		nil,
	); err != nil {
		baseLogger.Error("failed to build submission reconciler chain lookup", "error", err)
	} else {
		reconcileLookup.SetHTTPClient(chainBreakers.client(appMetrics, 30*time.Second, metrics.UpstreamSorobanRPC))
		reconcileLookup.SetRPCOptions(sorobanRPCOptions)

		submissionReconciler := stellarpkg.NewSubmissionReconciler(
			submissionStore, reconcileLookup, baseLogger.WithGroup("submission-reconciler"),
		)
		// Same leader gate the rebalancer and protocol-health jobs use, so
		// one instance sweeps rather than every replica.
		submissionReconciler.SetLeaderChecker(schedulerLeadership)
		go submissionReconciler.Run(shutdownCtx)
	}

	// Vault-balance reconciliation (nester#1082). The scheduled safety net for
	// the money path: reads the authoritative balance from each vault contract
	// and compares it against vaults.current_balance, recording — never
	// correcting — any divergence to the reconciliation audit tables, the log,
	// and the divergence metric the ReconciliationDivergence alert pages on.
	//
	// The comparison happens in RAW STROOPS, the unit the event indexer stores
	// (docs/event-indexer-replay.md; migration 103) — see
	// stellarpkg.ContractReader.TotalAssetsStroops for why rescaling either
	// side would hide bookkeeping errors.
	//
	// Like the submission reconciler above it needs no key material, runs on
	// the shared leader gate so one instance sweeps, and stops the moment
	// shutdown begins. RECONCILE_DRY_RUN rehearses a pass against production
	// data without writing or alerting anything.
	balanceReconciler := reconciliation.NewRunner(
		reconciliation.RunnerConfig{
			Enabled:  cfg.Reconciliation().Enabled(),
			Interval: cfg.Reconciliation().Interval(),
			DryRun:   cfg.Reconciliation().DryRun(),
		},
		reconciliation.NewPostgresRepository(db),
		[]reconciliation.Comparator{
			reconciliation.BalanceComparator{
				Vaults: vaultRepository,
				Chain:  stellarpkg.StroopsBalanceReader{Reader: contractReader},
				// Severity thresholds are stroop-denominated because the
				// comparison is: warn on any nonzero disagreement (with exact
				// integer bookkeeping there is no acceptable dust), escalate
				// to critical at 1 USDC (1e7 stroops). The dust tolerance is
				// sub-integer so no whole-stroop difference can be waved off.
				Classifier: reconciliation.Classifier{
					DustTolerance:     decimal.RequireFromString("0.5"),
					WarningThreshold:  decimal.NewFromInt(1),
					CriticalThreshold: decimal.NewFromInt(10_000_000),
				},
				Logger: baseLogger.WithGroup("balance-reconciler"),
			},
		},
		reconciliation.NewLogAlerter(baseLogger.WithGroup("balance-reconciler")),
		baseLogger.WithGroup("balance-reconciler"),
	)
	balanceReconciler.SetLeaderChecker(schedulerLeadership)
	balanceReconciler.SetMetrics(appMetrics)
	// Liveness is a scrape-time series (freshness-collector pattern): a dead
	// reconciler's age keeps climbing on the clock, and a non-leader replica
	// emits nothing rather than a misleading idle age.
	if err := appMetrics.RegisterBalanceReconcileAge(balanceReconciler.AgeSample); err != nil {
		baseLogger.Error("failed to register balance reconcile age collector", "error", err)
	}
	go balanceReconciler.Run(shutdownCtx)

	// Balance-freshness SLI (nester#1056, nester#1088): the indexer samples
	// its own position against the network tip on every tick and publishes it
	// to the freshness tracker, which the metrics collector and the API
	// freshness headers both read.
	stellarpkg.StartEventIndexer(shutdownCtx, baseLogger, db, systemStateRepository, stellarpkg.IndexerOptions{
		RPCURL:     cfg.Stellar().RPCURL(),
		HTTPClient: chainBreakers.client(appMetrics, stellarpkg.IndexerRequestTimeout, metrics.UpstreamSorobanRPC),
		RPCOptions: sorobanRPCOptions,
		Recorder:   indexerFreshness,
	})

	// The metrics endpoint runs on its own listener so it is never reachable
	// through the public port. It is not registered on mux at any point, so
	// a request for /metrics on the public interface 404s like any unknown
	// path — there is no rule to misorder and no auth bypass to get wrong.
	var metricsServer *metrics.Server
	if cfg.Metrics().Enabled() {
		metricsServer = metrics.NewServer(
			cfg.Metrics().Addr(),
			appMetrics.Handler(),
			baseLogger.WithGroup("metrics"),
		)
		go metricsServer.Start()
	}

	serverErr := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case err := <-serverErr:
		return err
	case <-shutdownCtx.Done():
		baseLogger.Info("shutdown signal received, draining")
	}

	stop()

	ready.Store(false)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server().GracefulShutdown())
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		baseLogger.Error("graceful shutdown timed out", "error", err.Error())
		return err
	}

	// Stopped after the public server so that a scrape during the drain
	// still reports the in-flight requests being drained. A failure to shut
	// it down cleanly is logged, not returned: the process is exiting and
	// losing the metrics listener is not worth a non-zero exit code.
	if metricsServer != nil {
		if err := metricsServer.Shutdown(ctx); err != nil {
			baseLogger.Error("metrics listener shutdown failed", "error", err.Error())
		}
	}

	if err := <-serverErr; err != nil {
		return err
	}

	baseLogger.Info("server stopped",
		"uptime", time.Since(startedAt).String(),
	)
	return nil
}

// transactionStatusEvent maps a reconciled transaction to the WebSocket event
// the dApp listens for on the "vaults:global" channel. Confirmed deposits and
// withdrawals get their dedicated event type; everything else (failures, other
// types) uses the generic status_changed event.
func transactionStatusEvent(tx transaction.Transaction) ws.Event {
	eventType := ws.EventStatusChanged
	if tx.Status == transaction.StatusCompleted {
		switch tx.Type {
		case transaction.TypeDeposit:
			eventType = ws.EventDepositConfirmed
		case transaction.TypeWithdrawal:
			eventType = ws.EventWithdrawalConfirmed
		}
	}
	return ws.Event{
		Channel: "vaults:global",
		Type:    eventType,
		Data:    tx,
	}
}

// idempotencyPurgeInterval bounds how often expired idempotency keys are
// swept, so the table stays bounded without a purge running on every
// request (#835's TTL requirement).
const idempotencyPurgeInterval = 15 * time.Minute

// runIdempotencyPurge periodically deletes idempotency_keys rows past
// their expires_at. Runs until ctx is cancelled (server shutdown).
func runIdempotencyPurge(ctx context.Context, store *postgres.IdempotencyRepository, logger *slog.Logger) {
	ticker := time.NewTicker(idempotencyPurgeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := store.PurgeExpired(ctx)
			if err != nil {
				logger.Error("idempotency key purge failed", "error", err)
				continue
			}
			if n > 0 {
				logger.Info("purged expired idempotency keys", "count", n)
			}
		}
	}
}

func walletKeyFromContext(r *http.Request) string {
	u, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		return ""
	}
	return u.WalletAddress
}

// httpClientSetter is implemented by the outbound clients that accept an
// instrumented transport at startup.
type httpClientSetter interface {
	SetHTTPClient(*http.Client)
}

// chainBreakerSet holds the circuit breakers guarding Soroban RPC and Horizon,
// plus the router that dispatches an outbound request to the right one
// (nester#1087).
//
// A nil set means the breakers are disabled, and every method on it degrades
// to "no guard" so no call site needs to branch.
type chainBreakerSet struct {
	router *breaker.Router
}

// newChainBreakers builds one breaker per chain upstream, wires them to the
// metrics collector, and returns the router the HTTP clients are built on.
//
// Two breakers, not one: Soroban RPC and Horizon fail independently, and a
// Horizon outage shedding Soroban traffic would take deposits offline for a
// dependency they do not need. They share a *policy* because both degrade the
// same way, but never state.
func newChainBreakers(cfg *config.Config, m *metrics.Metrics, logger *slog.Logger) (*chainBreakerSet, error) {
	if !cfg.CircuitBreaker().Enabled() {
		logger.Warn("chain circuit breakers are disabled; a degraded Soroban RPC or Horizon will not be shed")
		return nil, nil
	}

	policy := cfg.CircuitBreaker().Policy()
	onTransition := chainBreakerLogger(logger.WithGroup("circuit-breaker"))

	sorobanBreaker := breaker.New(string(metrics.UpstreamSorobanRPC), policy, onTransition)
	horizonBreaker := breaker.New(string(metrics.UpstreamHorizon), policy, onTransition)

	router := breaker.NewRouter()
	if err := router.Register(cfg.Stellar().RPCURL(), sorobanBreaker); err != nil {
		return nil, err
	}
	if err := router.Register(cfg.Stellar().HorizonURL(), horizonBreaker); err != nil {
		return nil, err
	}

	if err := m.RegisterBreakers(map[metrics.Upstream]metrics.BreakerReader{
		metrics.UpstreamSorobanRPC: sorobanBreaker,
		metrics.UpstreamHorizon:    horizonBreaker,
	}); err != nil {
		// Non-fatal, for the same reason as the freshness collector: losing
		// the metric must not stop the API serving, and the breakers still
		// protect the upstreams and still appear in /health/detailed.
		logger.Error("failed to register circuit breaker collector", "error", err)
	}

	logger.Info("chain circuit breakers enabled",
		"failure_ratio", policy.FailureRatio,
		"min_requests", policy.MinRequests,
		"window", policy.Window.String(),
		"open_duration", policy.OpenDuration.String(),
	)

	return &chainBreakerSet{router: router}, nil
}

// chainBreakerLogger logs state transitions only.
//
// Rejections are deliberately not logged: an open breaker can reject thousands
// of calls a second, and logging each one turns an upstream outage into a
// logging outage. The rejection counter metric carries that volume instead.
func chainBreakerLogger(logger *slog.Logger) breaker.TransitionFunc {
	return func(name string, from, to breaker.State, snapshot breaker.Snapshot) {
		attrs := []any{
			"upstream", name,
			"from", from.String(),
			"to", to.String(),
			"failure_ratio", snapshot.FailureRatio,
			"observed_requests", snapshot.Total,
		}

		// Opening is the operator-visible event: chain calls are now being
		// shed. Recovery and probing are informational.
		if to == breaker.StateOpen {
			logger.Warn("circuit breaker opened; shedding calls to upstream", attrs...)
			return
		}
		logger.Info("circuit breaker state changed", attrs...)
	}
}

// readers exposes the breakers for the health response, keyed by upstream.
func (s *chainBreakerSet) readers() map[metrics.Upstream]*breaker.Breaker {
	if s == nil {
		return nil
	}
	out := make(map[metrics.Upstream]*breaker.Breaker, len(s.router.Breakers()))
	for _, b := range s.router.Breakers() {
		out[metrics.Upstream(b.Name())] = b
	}
	return out
}

// client returns an HTTP client for one chain upstream: metrics innermost,
// breaker outermost.
//
// The order matters. A rejected call never reaches the metrics transport, so
// it is not counted as an outbound request and does not plant a near-zero
// sample in the latency histogram or a transport error under a made-up kind.
// nester_outbound_* therefore keeps meaning "calls we actually made", and the
// shed load is reported by the breaker's own rejection counter instead.
func (s *chainBreakerSet) client(m *metrics.Metrics, timeout time.Duration, upstream metrics.Upstream) *http.Client {
	client := m.InstrumentClient(&http.Client{Timeout: timeout}, upstream)
	if s == nil {
		return client
	}
	client.Transport = s.router.Transport(client.Transport)
	return client
}

// instrumentRateProvider installs a metrics-instrumented, circuit-broken HTTP
// client on an exchange-rate provider.
//
// The upstream label is derived from the provider's own Name(), which returns
// a fixed string per implementation, so the label set stays bounded by the
// number of provider types rather than by anything at runtime. An unknown
// provider is still instrumented, under "other", so a new one is never
// silently invisible.
func instrumentRateProvider(m *metrics.Metrics, breakers *chainBreakerSet, provider oracle.Provider) {
	setter, ok := provider.(httpClientSetter)
	if !ok {
		return
	}

	upstream := metrics.UpstreamOther
	switch provider.Name() {
	case "horizon":
		upstream = metrics.UpstreamHorizon
	case "defillama":
		upstream = metrics.UpstreamDeFiLlama
	case "coingecko":
		upstream = metrics.UpstreamCoinGecko
	}

	// Every provider gets the same client factory. Only the chain upstreams
	// have a breaker registered, so the router passes CoinGecko and DeFiLlama
	// straight through — this issue scopes the breaker to Soroban and Horizon.
	setter.SetHTTPClient(breakers.client(m, 10*time.Second, upstream))
}

func livenessHandler(ready *atomic.Bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("draining"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

// healthProbe reports whether one dependency is reachable, returning nil when
// it is.
//
// The health handlers take probes rather than concrete clients so the endpoint
// contract can be exercised deterministically: a stub standing in for a
// stopped PostgreSQL or Redis is enough, and no test has to actually stop one.
type healthProbe func(ctx context.Context) error

// poolStats is the subset of pgxpool.Stat that /health/detailed reports. Taken
// as a snapshot function for the same reason as healthProbe: the handler never
// needs a live pool, only the numbers.
type poolStats struct {
	MaxConns      int32
	AcquiredConns int32
	IdleConns     int32
	TotalConns    int32
}

// healthDeps is everything the four health endpoints need.
type healthDeps struct {
	ready  *atomic.Bool
	pingDB healthProbe
	// pingRedis is nil when REDIS_ADDR is unset and the in-memory fallbacks
	// are in use (see the redisClient construction in run): readiness then has
	// no Redis to be blocked on.
	pingRedis healthProbe
	// poolStats may be nil, in which case /health/detailed reports zeroed pool
	// counters rather than panicking.
	poolStats    func() poolStats
	probeTimeout time.Duration

	httpClient   *http.Client
	horizonURL   string
	rpcURL       string
	startedAt    time.Time
	environment  string
	buildVersion string
	buildCommit  string

	// breakers is keyed by upstream; nil entries mean that dependency is not
	// guarded. The probes below deliberately do NOT go through these clients:
	// a health check is a diagnostic, and an open breaker must not be able to
	// report the upstream as unreachable when it has in fact recovered. The
	// probe result and the breaker state are two independent facts, and seeing
	// "reachable, but breaker still open" is exactly what tells an operator
	// recovery is one probe away.
	breakers map[metrics.Upstream]*breaker.Breaker
}

// registerHealthRoutes wires the liveness, readiness, and diagnostic health
// endpoints onto mux.
//
// /healthz is the canonical liveness path (#1042). It is the sibling of
// /readyz, and the path the internal metrics listener already serves on its
// own port (metrics.NewServer), so the whole fleet answers liveness at one
// name. /health is a permanent alias — it is what the compose healthcheck, the
// staging smoke tests, and the deployed probes were pointed at, and both paths
// are the same handler, so they cannot drift apart.
//
// Routing lives in one function, called by run and by the contract test, so a
// route that moves in production cannot leave the test asserting the old one.
func registerHealthRoutes(mux *http.ServeMux, deps healthDeps) {
	mux.HandleFunc("GET /healthz", livenessHandler(deps.ready))
	mux.HandleFunc("GET /health", livenessHandler(deps.ready))
	mux.HandleFunc("GET /readyz", readinessHandler(deps))
	mux.HandleFunc("GET /health/detailed", detailedHealthHandler(deps))
}

// readinessHandler reports whether this instance should be sent traffic.
//
// It fails closed on every dependency the instance cannot serve correct
// responses without: PostgreSQL, and — when configured — Redis, which backs
// the token-revocation cache and the distributed rate limiters, so an instance
// that has lost it would honour revoked sessions and under-count limits. A
// pool that is saturated rather than down surfaces identically: the ping
// blocks waiting for a free connection and probeTimeout turns that into a
// failure.
func readinessHandler(deps healthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if !deps.ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("draining"))
			return
		}
		dbCtx, dbCancel := context.WithTimeout(r.Context(), deps.probeTimeout)
		dbErr := deps.pingDB(dbCtx)
		dbCancel()
		if dbErr != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("database unavailable"))
			return
		}
		// Each probe gets its own budget. Sharing one deadline would let a
		// slow-but-healthy database consume it and report Redis as down.
		if deps.pingRedis != nil {
			redisCtx, redisCancel := context.WithTimeout(r.Context(), deps.probeTimeout)
			redisErr := deps.pingRedis(redisCtx)
			redisCancel()
			if redisErr != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("redis unavailable"))
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

// pgxPoolStats adapts the live pgxpool statistics to the snapshot
// /health/detailed reports.
func pgxPoolStats(db *repository.PostgresDB) func() poolStats {
	return func() poolStats {
		stat := db.Pool.Stat()
		return poolStats{
			MaxConns:      stat.MaxConns(),
			AcquiredConns: stat.AcquiredConns(),
			IdleConns:     stat.IdleConns(),
			TotalConns:    stat.TotalConns(),
		}
	}
}

type dependencyStatus struct {
	OK            bool   `json:"ok"`
	Endpoint      string `json:"endpoint,omitempty"`
	LatencyMillis int64  `json:"latency_ms,omitempty"`
	Error         string `json:"error,omitempty"`
	LatestLedger  uint64 `json:"latest_ledger,omitempty"`

	// CircuitBreaker reports whether calls to this dependency are currently
	// being shed (nester#1087): "closed", "half_open", or "open". Omitted when
	// the breakers are disabled, so the field's absence means "not guarded"
	// rather than "guarded and healthy".
	CircuitBreaker *breakerStatus `json:"circuit_breaker,omitempty"`
}

// breakerStatus is one breaker's state as it appears in the health response.
//
// It reports the ratio and sample size alongside the state because "open" on
// its own does not tell an operator whether the upstream is badly broken or
// marginally over the threshold, and that is the first thing they need.
type breakerStatus struct {
	State        string  `json:"state"`
	FailureRatio float64 `json:"failure_ratio"`
	Observed     int     `json:"observed_requests"`
	Rejected     uint64  `json:"rejected_total"`
	RetrySeconds float64 `json:"retry_in_seconds,omitempty"`
}

// breakerDegraded reports whether a breaker is shedding or about to probe.
// Half-open counts: calls are still being rejected while the single probe runs.
func breakerDegraded(s *breakerStatus) bool {
	return s != nil && s.State != breaker.StateClosed.String()
}

func newBreakerStatus(b *breaker.Breaker) *breakerStatus {
	if b == nil {
		return nil
	}

	snapshot := b.Snapshot()
	return &breakerStatus{
		State:        snapshot.State.String(),
		FailureRatio: snapshot.FailureRatio,
		Observed:     snapshot.Total,
		Rejected:     snapshot.Rejected,
		RetrySeconds: snapshot.RetryIn.Seconds(),
	}
}

type dbStatus struct {
	OK            bool   `json:"ok"`
	LatencyMillis int64  `json:"latency_ms,omitempty"`
	Error         string `json:"error,omitempty"`
	MaxConns      int32  `json:"max_conns"`
	AcquiredConns int32  `json:"acquired_conns"`
	IdleConns     int32  `json:"idle_conns"`
	TotalConns    int32  `json:"total_conns"`
}

// redisStatus is reported separately from dependencyStatus because Redis is
// optional: an instance with REDIS_ADDR unset runs on in-memory fallbacks and
// is healthy, which "ok" alone cannot distinguish from a Redis that answered.
type redisStatus struct {
	OK            bool   `json:"ok"`
	Configured    bool   `json:"configured"`
	LatencyMillis int64  `json:"latency_ms,omitempty"`
	Error         string `json:"error,omitempty"`
}

type detailedHealthResponse struct {
	Status      string           `json:"status"`
	Environment string           `json:"environment"`
	Version     string           `json:"version"`
	Commit      string           `json:"commit"`
	UptimeSecs  int64            `json:"uptime_seconds"`
	Database    dbStatus         `json:"database"`
	Redis       redisStatus      `json:"redis"`
	Horizon     dependencyStatus `json:"horizon"`
	SorobanRPC  dependencyStatus `json:"soroban_rpc"`
	GeneratedAt time.Time        `json:"generated_at"`
}

// safeDependencyError reduces a dependency failure to a coarse reason that
// reveals nothing about how this service reaches that dependency.
//
// /health/detailed is unauthenticated (see middleware.ProductionAuthRules), and
// driver errors are not fit to publish: a pgx dial failure carries the DSN's
// user, host, and database name; a go-redis failure carries the resolved
// address; an upstream HTTP probe echoes back up to 512 bytes of the remote
// body. The operator reads the real error in the logs — the public payload
// says only whether the dependency answered, and whether it ran out of time.
func safeDependencyError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "timeout"
	default:
		return "unavailable"
	}
}

// safeProbeError is safeDependencyError for the Stellar probes, which report
// failure as a pre-formatted string rather than an error. That string can
// contain the upstream's own response body, so none of it is echoed.
func safeProbeError(res stellarpkg.HealthResult) string {
	if res.OK {
		return ""
	}
	return "unavailable"
}

func detailedHealthHandler(deps healthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := detailedHealthResponse{
			Status:      "ok",
			Environment: deps.environment,
			Version:     deps.buildVersion,
			Commit:      deps.buildCommit,
			UptimeSecs:  int64(time.Since(deps.startedAt).Seconds()),
			GeneratedAt: time.Now().UTC(),
		}

		// A nil pingDB means "no database wired into this handler" rather than
		// "the database is healthy": report it as unavailable instead of
		// dereferencing nil, so a misconfigured build fails the probe loudly
		// rather than serving a 200 that claims a database it never checked.
		dbStart := time.Now()
		dbErr := errors.New("database probe unavailable")
		if deps.pingDB != nil {
			dbCtx, dbCancel := context.WithTimeout(r.Context(), deps.probeTimeout)
			dbErr = deps.pingDB(dbCtx)
			dbCancel()
		}
		var stat poolStats
		if deps.poolStats != nil {
			stat = deps.poolStats()
		}
		resp.Database = dbStatus{
			OK:            dbErr == nil,
			LatencyMillis: time.Since(dbStart).Milliseconds(),
			Error:         safeDependencyError(dbErr),
			MaxConns:      stat.MaxConns,
			AcquiredConns: stat.AcquiredConns,
			IdleConns:     stat.IdleConns,
			TotalConns:    stat.TotalConns,
		}

		// An unconfigured Redis is a supported single-instance mode, not a
		// fault: report it as healthy but unconfigured rather than probing nil.
		resp.Redis = redisStatus{OK: true, Configured: deps.pingRedis != nil}
		if deps.pingRedis != nil {
			redisCtx, redisCancel := context.WithTimeout(r.Context(), deps.probeTimeout)
			redisStart := time.Now()
			redisErr := deps.pingRedis(redisCtx)
			redisCancel()
			resp.Redis.OK = redisErr == nil
			resp.Redis.LatencyMillis = time.Since(redisStart).Milliseconds()
			resp.Redis.Error = safeDependencyError(redisErr)
		}

		hStart := time.Now()
		hRes := stellarpkg.PingHorizon(r.Context(), deps.httpClient, deps.horizonURL)
		resp.Horizon = dependencyStatus{
			OK:             hRes.OK,
			Endpoint:       hRes.Endpoint,
			Error:          safeProbeError(hRes),
			LatencyMillis:  time.Since(hStart).Milliseconds(),
			LatestLedger:   hRes.LatestLedger,
			CircuitBreaker: newBreakerStatus(deps.breakers[metrics.UpstreamHorizon]),
		}

		rStart := time.Now()
		rRes := stellarpkg.PingSorobanRPC(r.Context(), deps.httpClient, deps.rpcURL)
		resp.SorobanRPC = dependencyStatus{
			OK:             rRes.OK,
			Endpoint:       rRes.Endpoint,
			Error:          safeProbeError(rRes),
			LatencyMillis:  time.Since(rStart).Milliseconds(),
			LatestLedger:   rRes.LatestLedger,
			CircuitBreaker: newBreakerStatus(deps.breakers[metrics.UpstreamSorobanRPC]),
		}

		// Redis only counts against this instance when it is configured; the
		// in-memory fallback path has nothing to lose.
		redisDown := resp.Redis.Configured && !resp.Redis.OK
		// An open breaker is "degraded" even when the probe alongside it
		// succeeded, because callers are still being shed until the next probe
		// closes it. It does not affect the HTTP status: chain dependencies
		// have never gated readiness here, and making an open breaker return
		// 503 would evict the pod from its load balancer over an upstream
		// outage — turning the partial failure into the total one this feature
		// exists to prevent. Only the database, Redis, and draining do that.
		degraded := !resp.Database.OK || redisDown || !resp.Horizon.OK || !resp.SorobanRPC.OK ||
			breakerDegraded(resp.Horizon.CircuitBreaker) ||
			breakerDegraded(resp.SorobanRPC.CircuitBreaker)
		draining := !deps.ready.Load()
		switch {
		case draining:
			resp.Status = "draining"
		case degraded:
			resp.Status = "degraded"
		}

		// The 503 set matches /readyz: Postgres and (when configured) Redis are
		// the dependencies this instance cannot serve correct responses without.
		// Horizon and Soroban RPC degrade individual routes, so they report
		// "degraded" at 200 rather than pulling the instance out of rotation.
		status := http.StatusOK
		if draining || !resp.Database.OK || redisDown {
			status = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func pingStellarDependencies(logger *slog.Logger, cfg *config.Config) error {
	timeout := cfg.Startup().DependencyTimeout()
	client := &http.Client{Timeout: timeout}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if res := stellarpkg.PingHorizon(ctx, client, cfg.Stellar().HorizonURL()); !res.OK {
		return fmt.Errorf("horizon unreachable at %s: %s", cfg.Stellar().HorizonURL(), res.Error)
	} else {
		logger.Info("horizon reachable", "url", cfg.Stellar().HorizonURL(), "latest_ledger", res.LatestLedger)
	}

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), timeout)
	defer rpcCancel()
	if res := stellarpkg.PingSorobanRPC(rpcCtx, client, cfg.Stellar().RPCURL()); !res.OK {
		return fmt.Errorf("soroban rpc unreachable at %s: %s", cfg.Stellar().RPCURL(), res.Error)
	} else {
		logger.Info("soroban rpc reachable", "url", cfg.Stellar().RPCURL(), "latest_ledger", res.LatestLedger)
	}

	return nil
}

// ledgerChainReaderAdapter wraps stellar ContractReader to implement
// ledger.ChainReader for the reconciliation job.
type ledgerChainReaderAdapter struct {
	contractReader *stellarpkg.ContractReader
}

func (a *ledgerChainReaderAdapter) ReadVaultBalance(ctx context.Context, contractAddress string) (int64, error) {
	if a.contractReader == nil {
		return 0, fmt.Errorf("contract reader not configured")
	}
	dec, err := a.contractReader.VaultBalance(ctx, contractAddress)
	if err != nil {
		return 0, err
	}
	// Convert decimal USDC to stroops int64.
	return dec.Mul(decimal.NewFromInt(10_000_000)).Round(0).IntPart(), nil
}

func (a *ledgerChainReaderAdapter) ReadTotalSharesTimesPrice(ctx context.Context, contractAddress string) (int64, error) {
	if a.contractReader == nil {
		return 0, nil
	}
	// total_shares*share_price is total assets, which the contract exposes
	// directly.
	dec, err := a.contractReader.TotalAssets(ctx, contractAddress)
	if err != nil {
		return 0, nil // skip this check if not available
	}
	return dec.Mul(decimal.NewFromInt(10_000_000)).Round(0).IntPart(), nil
}

// reconciliationVaultListerAdapter adapts the vault repository to
// scheduler.ReconciliationVaultLister.
type reconciliationVaultListerAdapter struct {
	vaultRepo *postgres.VaultRepository
}

func (a *reconciliationVaultListerAdapter) ListActiveForReconciliation(ctx context.Context) ([]scheduler.ReconcileVaultInfo, error) {
	if a.vaultRepo == nil {
		return nil, nil
	}
	vaults, err := a.vaultRepo.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]scheduler.ReconcileVaultInfo, 0, len(vaults))
	for _, v := range vaults {
		out = append(out, scheduler.ReconcileVaultInfo{
			ID:              v.ID,
			ContractAddress: v.ContractAddress,
			Currency:        v.Currency,
		})
	}
	return out, nil
}

func ledgerDomainConfig() ledger.ReconciliationConfig {
	return ledger.ReconciliationConfig{
		Enabled:          true,
		Interval:         5 * time.Minute,
		ToleranceStroops: 1_000_000, // 0.1 USDC
	}
}
