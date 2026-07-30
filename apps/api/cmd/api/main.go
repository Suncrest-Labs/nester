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
	"github.com/suncrestlabs/nester/apps/api/internal/config"
	cryptopkg "github.com/suncrestlabs/nester/apps/api/internal/crypto"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/nudge"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/usersignal"
	"github.com/suncrestlabs/nester/apps/api/internal/handler"
	"github.com/suncrestlabs/nester/apps/api/internal/harvest"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
	"github.com/suncrestlabs/nester/apps/api/internal/oracle"
	"github.com/suncrestlabs/nester/apps/api/internal/repository"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
	"github.com/suncrestlabs/nester/apps/api/internal/scheduler"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	performancesvc "github.com/suncrestlabs/nester/apps/api/internal/service/performance"
	tvlsvc "github.com/suncrestlabs/nester/apps/api/internal/service/tvl"
	"github.com/suncrestlabs/nester/apps/api/internal/services"
	stellarpkg "github.com/suncrestlabs/nester/apps/api/internal/stellar"
	"github.com/suncrestlabs/nester/apps/api/internal/valuation"
	"github.com/suncrestlabs/nester/apps/api/internal/ws"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
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

	// Created early (rather than just before ListenAndServe, as before) so
	// components that need to release resources as soon as shutdown begins —
	// notably scheduler leadership below — can hook directly into it instead
	// of only unwinding via defer after the HTTP server finishes draining.
	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pgPool, err := repository.NewPostgresDB(cfg.Database())
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

	vaultRepository := postgres.NewVaultRepository(db)
	vaultService := service.NewVaultService(vaultRepository)
	vaultService.SetHarvestDefaultCompound(cfg.Stellar().HarvestDefaultCompound())
	vaultHandler := handler.NewVaultHandler(vaultService)

	yieldHarvestRepository := postgres.NewYieldHarvestRepository(db)
	yieldHarvestService := service.NewYieldHarvestService(yieldHarvestRepository)
	vaultService.SetYieldHarvestRecorder(yieldHarvestService)

	portfolioService := service.NewPortfolioService(vaultRepository)
	portfolioHandler := handler.NewPortfolioHandler(portfolioService)

	transactionRepository := postgres.NewTransactionRepository(db)
	transactionService := service.NewTransactionService(transactionRepository, cfg.Stellar().HorizonURL())
	// Balance is moved only after a deposit/withdrawal is confirmed on-chain
	// (issue #496); the vault repository applies it idempotently by tx hash.
	transactionService.SetBalanceApplier(vaultRepository)
	transactionHandler := handler.NewTransactionHandler(transactionService)
	transactionHandler.SetVaultRepository(vaultRepository)

	bankAccountRepository := postgres.NewBankAccountRepository(db)
	var accountCipher *cryptopkg.AccountCipher
	if ac := cfg.AccountCipher(); ac.Configured() {
		cipher, cipherErr := cryptopkg.NewAccountCipherWithKeys(ac.ActiveVersion(), ac.Keys(), ac.FingerprintKey())
		if cipherErr != nil {
			return fmt.Errorf("bank account cipher: %w", cipherErr)
		}
		accountCipher = cipher
	}

	paystackResolver := service.NewPaystackResolver(cfg.Bank().PaystackKey())
	flutterwaveResolver := service.NewFlutterwaveResolver(cfg.Bank().FlutterwaveKey())
	bankService := service.NewBankService(paystackResolver, flutterwaveResolver)
	bankHandler := handler.NewBankHandler(bankService)

	bankAccountService := service.NewBankAccountService(bankAccountRepository, accountCipher, bankService)
	bankAccountHandler := handler.NewBankAccountHandler(bankAccountService)

	userRepository := postgres.NewUserRepository(db)
	userService := service.NewUserService(userRepository)
	if accountCipher != nil {
		userService.WithCipher(accountCipher)
	}
	userHandler := handler.NewUserHandler(userService)
	userVaultsSvc := service.NewUserVaultsService(vaultRepository)
	userHandler.SetUserVaultsService(userVaultsSvc)
	notificationRepository := postgres.NewNotificationRepository(db)
	notificationHandler := handler.NewNotificationHandler(notificationRepository)

	settlementRepository := postgres.NewSettlementRepository(db)
	settlementService := service.NewSettlementService(settlementRepository, bankAccountService)
	settlementHandler := handler.NewSettlementHandler(settlementService, userService)

	adminRepository := postgres.NewAdminRepository(db)
	goalTemplateRepo := postgres.NewGoalTemplateRepository(db)

	var chainInvoker service.VaultChainInvoker
	if secret := cfg.Stellar().OperatorSecret(); secret != "" {
		inv, err := service.NewSorobanVaultChainInvoker(
			cfg.Stellar().RPCURL(),
			cfg.Stellar().HorizonURL(),
			cfg.Stellar().NetworkPassphrase(),
			secret,
			cfg.Stellar().WithdrawalSlippageBps(),
		)
		if err != nil {
			return fmt.Errorf("init chain invoker: %w", err)
		}
		chainInvoker = inv
		vaultService.SetDepositInvoker(inv)
	}

	adminService := service.NewAdminService(
		adminRepository,
		vaultRepository,
		chainInvoker,
		cfg.Stellar().HorizonURL(),
		cfg.SettlementProviderURL(),
		cfg.Stellar().AllocationStrategyAddress(),
		cfg.Allocation().MinWeightPercent(),
	)
	adminService.SetTemplateRepository(goalTemplateRepo)
	adminHandler := handler.NewAdminHandler(adminService, userService)
	adminHandler.SetEventSyncer(&stellarpkg.EventSyncer{
		DB:      db,
		SysRepo: systemStateRepository,
		RPCURL:  cfg.Stellar().RPCURL(),
		Logger:  baseLogger,
	})
	adminHandler.SetLeadership(schedulerLeadership)

	// A single shared Redis client (nil when REDIS_ADDR is unset) powers both the
	// challenge store and the distributed rate limiters. When nil, both fall back
	// to in-memory implementations suitable for single-instance deployments.
	var redisClient *redis.Client
	if addr := cfg.Redis().Addr(); addr != "" {
		redisClient = redis.NewClient(&redis.Options{Addr: addr})
	}

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

	activityEventRepo := postgres.NewActivityEventRepository(db)
	nudgeHistoryRepo := postgres.NewNudgeHistoryRepository(db)
	nudgeOutcomeService := service.NewNudgeOutcomeService(nudgeHistoryRepo)

	oracleService := oracle.NewRateService(cfg.Stellar().HorizonURL(), cfg.Stellar().USDCIssuer())
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
		Notifier:  valuation.NewWSNotifier(wsHub),
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
	pollerCtx, cancelPoller := context.WithCancel(context.Background())
	defer cancelPoller()
	go txPoller.Run(pollerCtx)

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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", livenessHandler(&ready))
	mux.HandleFunc("GET /healthz", livenessHandler(&ready))
	mux.HandleFunc("GET /readyz", readinessHandler(&ready, pgPool, cfg.Database().ConnectionTimeout()))
	mux.HandleFunc("GET /health/detailed", detailedHealthHandler(detailedHealthDeps{
		ready:        &ready,
		pgPool:       pgPool,
		dbTimeout:    cfg.Database().ConnectionTimeout(),
		httpClient:   depHTTPClient,
		horizonURL:   cfg.Stellar().HorizonURL(),
		rpcURL:       cfg.Stellar().RPCURL(),
		startedAt:    startedAt,
		environment:  cfg.Environment(),
		buildVersion: version,
	}))
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
	settlementHandler.Register(mux)

	// Unified activity feed (deposits/withdrawals/rebalances/settlements/
	// yield harvests) backing the dApp's transaction-history page.
	activityRepository := postgres.NewActivityRepository(db)
	activityService := service.NewActivityService(activityRepository)
	activityHandler := handler.NewActivityHandler(activityService)
	activityHandler.Register(mux)
	userHandler.Register(mux)
	notificationHandler.Register(mux)
	adminHandler.Register(mux)
	authHandler.Register(mux)
	rateHandler.Register(mux)
	performanceHandler.Register(mux)
	tvlHandler.Register(mux)
	projectionHandler.Register(mux)
	analyticsHandler := handler.NewAnalyticsHandler(performanceService)
	analyticsHandler.Register(mux)

	// Risk service
	riskService := services.NewRiskService(vaultRepository)
	riskHandler := handler.NewRiskHandler(riskService)
	riskHandler.Register(mux)

	// Vault analytics (APY volatility, Sharpe, Sortino, drawdown, win rate)
	vaultAnalyticsSvc := service.NewVaultAnalyticsService(performanceRepository)
	vaultAnalyticsHandler := handler.NewVaultAnalyticsHandler(vaultAnalyticsSvc)
	vaultAnalyticsHandler.Register(mux)

	// Yield opportunities (DeFiLlama Stellar pools)
	yieldSvc := service.NewYieldService("")
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
	// Intelligence proxy (forwards to Python service)
	intelURL := cfg.Intelligence().ServiceURL()
	intelProxy := service.NewIntelligenceProxy(intelURL, cfg.Intelligence().Timeout())
	prometheusClient := service.NewPrometheusClient(service.PrometheusConfig{
		BaseURL: intelURL,
		APIKey:  cfg.Intelligence().ServiceAPIKey(),
		Timeout: cfg.Intelligence().Timeout(),
	})

	nudgeCopyGen := service.CompositeCopyGenerator{
		Template: nudge.TemplateCopyGenerator{},
		LLM:      service.LLMCopyGenerator{Client: prometheusClient},
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

	webhookRepo := postgres.NewWebhookRepository(db)
	webhookSvc := service.NewWebhookService(webhookRepo)
	webhookHandler := handler.NewWebhookHandler(webhookSvc)
	webhookHandler.Register(mux)
	// Per-goal notification preferences (mute/digest frequency).
	goalNotificationRepo := postgres.NewGoalNotificationRepository(db)
	goalNotificationPrefSvc := service.NewGoalNotificationPreferenceService(goalNotificationRepo, savingsGoalRepo)
	savingsGoalSvc := service.NewSavingsGoalService(
		savingsGoalRepo,
		vaultRepository,
		service.CompositeGoalMilestoneNotifier{
			Notifiers: []service.GoalMilestoneNotifier{
				service.DispatcherGoalMilestoneNotifier{
					Dispatcher:  notificationDispatcher2,
					Preferences: goalNotificationRepo,
				},
				service.NudgeEngineGoalMilestoneNotifier{NudgeEngine: nudgeEngineSvc},
				service.WebhookGoalMilestoneNotifier{Svc: webhookSvc},
			},
		},
	)
	savingsGoalSvc.SetOutcomeRecorder(nudgeOutcomeService)
	savingsGoalSvc.SetStreakRepository(savingsStreakRepo)
	savingsGoalSvc.SetStreakNotifier(service.DispatcherStreakMilestoneNotifier{Dispatcher: notificationDispatcher2})
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

	ledgerVaultService := service.NewVaultService(vaultRepository)
	scheduledDepositSvc := service.NewScheduledDepositService(ledgerVaultService)
	goalProgressSvc := service.NewGoalProgressService(savingsGoalRepo)

	// Durable async job queue (#824): the shared worker pool and producer
	// client. Handlers are registered below before the worker starts. The
	// client is passed to producers (harvest engine, chain invoker, and —
	// as of #846 — the recurring-deposit job below) so they enqueue durable
	// work instead of doing it inline.
	jobQueueRepo := postgres.NewJobRepository(db)
	jobQueueMetrics := jobqueue.NewStdMetrics()
	jobQueueClient := jobqueue.NewClient(jobQueueRepo, jobQueueMetrics)

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

	intelligenceHandler := handler.NewIntelligenceHandler(intelProxy, prometheusClient)
	intelligenceHandler.Register(mux)

	// AI progress coaching (#112): on-demand endpoint plus a weekly background nudge.
	savingsGoalHandler.SetCoachingProvider(prometheusClient)
	goalCoachingScheduler := service.NewGoalCoachingScheduler(
		savingsGoalRepo,
		prometheusClient,
		nudgeNotificationDispatcher,
		baseLogger.WithGroup("goal-coaching"),
		nudgeHistoryRepo,
	)
	goalCoachingCtx, cancelGoalCoaching := context.WithCancel(context.Background())
	defer cancelGoalCoaching()
	go goalCoachingScheduler.Run(goalCoachingCtx, 7*24*time.Hour)

	intelRelay := service.NewRelayHandler(http.DefaultClient, service.RelayConfig{
		BaseURL: intelURL,
		APIKey:  cfg.Intelligence().ServiceAPIKey(),
		Timeout: cfg.Intelligence().Timeout(),
	})
	intelligenceRelayHandler := handler.NewIntelligenceRelayHandler(intelRelay)
	intelligenceRelayHandler.Register(mux)

	// Periodic financial insight digest (#859): a deterministic ledger
	// source endpoint (consumed by the intelligence service via the relay),
	// a cache/audit table, and a leader-elected daily job that generates and
	// delivers a digest once per user per completed period.
	digestRepository := postgres.NewDigestRepository(db)
	digestLedgerService := service.NewDigestLedgerService(savingsGoalRepo, yieldHarvestRepository, savingsStreakRepo)
	digestHandler := handler.NewDigestHandler(digestLedgerService, digestRepository)
	digestHandler.Register(mux)

	digestJob := scheduler.NewDigestJob(
		scheduler.DigestJobConfig{Enabled: true, Interval: 24 * time.Hour},
		notificationRepository,
		digestRepository,
		prometheusClient,
		nudgeNotificationDispatcher,
		baseLogger.WithGroup("digest"),
	)
	digestJob.SetLeaderChecker(schedulerLeadership)
	digestCtx, cancelDigest := context.WithCancel(context.Background())
	defer cancelDigest()
	go digestJob.Run(digestCtx)

	performanceSnapshotsHandler := handler.NewPerformanceSnapshotsHandler(performanceService)
	performanceSnapshotsHandler.Register(mux)

	toolAuditRepo := postgres.NewToolAuditRepository(db)
	toolAuditSvc := service.NewToolAuditService(toolAuditRepo)
	toolAuditHandler := handler.NewToolAuditHandler(toolAuditSvc)
	toolAuditHandler.Register(mux)

	bankHandler.Register(mux)
	bankAccountHandler.Register(mux)

	mux.HandleFunc("GET /ws", wsHub.ServeWs)

	// APY snapshot scheduler and history endpoint
	apySnapshotRepo := postgres.NewAPYSnapshotRepository(db)
	apySvc := service.NewAPYService(apySnapshotRepo)
	apyHandler := handler.NewAPYHandler(apySvc)
	apyHandler.Register(mux)
	apySchedulerCtx, cancelAPYScheduler := context.WithCancel(context.Background())
	defer cancelAPYScheduler()
	go apySvc.StartScheduler(apySchedulerCtx)

	authRules := []middleware.RouteRule{
		{PathPrefix: "/health", Public: true},
		{PathPrefix: "/healthz", Public: true},
		{PathPrefix: "/readyz", Public: true},
		{PathPrefix: "/ws", Public: true},
		{Method: http.MethodPost, PathPrefix: "/api/v1/auth/challenge", Public: true},
		{Method: http.MethodPost, PathPrefix: "/api/v1/auth/verify", Public: true},
		{Method: http.MethodPost, PathPrefix: "/api/v1/auth/refresh", Public: true},
		// No blanket "/api/v1/auth/" rule: logout, logout-all, and sessions
		// must stay protected and fall through to the "/api/v1/" catch-all.
		{PathPrefix: "/api/v1/banks/", Public: true},
		{PathPrefix: "/api/v1/yields/", Public: true},
		{PathPrefix: "/api/v1/savings-goals/shared/", Public: true},
		{PathPrefix: "/api/v1/admin/", Public: false, Role: "admin"},
		{PathPrefix: "/api/v1/internal/", Role: "service"},
		{PathPrefix: "/api/v1/", Public: false},
	}
	authenticator := middleware.Authenticate(cfg.Auth().Secret(), cfg.Auth().ServiceAPIKey(), authRules, revocationCache)
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
	// settlementLimiter applies a strict per-user limit to settlement creation to
	// prevent settlement spam. Placed after authentication so it keys by user ID.
	settlementLimiter := middleware.SensitiveUserRouteLimiter(
		middleware.NewLimiter(redisClient, "settlement", cfg.RateLimit().SettlementLimit(), cfg.RateLimit().SettlementWindow()),
		[]middleware.RouteMatch{
			{Method: http.MethodPost, Path: "/api/v1/settlements"},
		},
		"settlement rate limit exceeded",
	)
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
		Handler: middleware.SecurityHeaders(cfg.Environment())(
			middleware.RecoverPanic(baseLogger)(
				cors(
					globalLimiter(
						authRouteLimiter(
							writeLimiter(
								authenticator(
									settlementLimiter(
										walletLimiter(
											middleware.LimitRequestBody(1 * 1024 * 1024)(
												middleware.Logging(baseLogger)(mux),
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
		"network_passphrase", cfg.Stellar().NetworkPassphrase(),
		"auto_migrate", cfg.Startup().EnableAutoMigrate(),
	)

	stellarpkg.StartEventIndexer(shutdownCtx, baseLogger, db, systemStateRepository, cfg.Stellar().RPCURL())

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

func walletKeyFromContext(r *http.Request) string {
	u, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		return ""
	}
	return u.WalletAddress
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

func readinessHandler(ready *atomic.Bool, db *repository.PostgresDB, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("draining"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("database unavailable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

type detailedHealthDeps struct {
	ready        *atomic.Bool
	pgPool       *repository.PostgresDB
	dbTimeout    time.Duration
	httpClient   *http.Client
	horizonURL   string
	rpcURL       string
	startedAt    time.Time
	environment  string
	buildVersion string
}

type dependencyStatus struct {
	OK            bool   `json:"ok"`
	Endpoint      string `json:"endpoint,omitempty"`
	LatencyMillis int64  `json:"latency_ms,omitempty"`
	Error         string `json:"error,omitempty"`
	LatestLedger  uint64 `json:"latest_ledger,omitempty"`
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

type detailedHealthResponse struct {
	Status      string           `json:"status"`
	Environment string           `json:"environment"`
	Version     string           `json:"version"`
	UptimeSecs  int64            `json:"uptime_seconds"`
	Database    dbStatus         `json:"database"`
	Horizon     dependencyStatus `json:"horizon"`
	SorobanRPC  dependencyStatus `json:"soroban_rpc"`
	GeneratedAt time.Time        `json:"generated_at"`
}

func detailedHealthHandler(deps detailedHealthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := detailedHealthResponse{
			Status:      "ok",
			Environment: deps.environment,
			Version:     deps.buildVersion,
			UptimeSecs:  int64(time.Since(deps.startedAt).Seconds()),
			GeneratedAt: time.Now().UTC(),
		}

		dbCtx, dbCancel := context.WithTimeout(r.Context(), deps.dbTimeout)
		dbStart := time.Now()
		dbErr := deps.pgPool.Ping(dbCtx)
		dbCancel()
		stat := deps.pgPool.Pool.Stat()
		resp.Database = dbStatus{
			OK:            dbErr == nil,
			LatencyMillis: time.Since(dbStart).Milliseconds(),
			MaxConns:      stat.MaxConns(),
			AcquiredConns: stat.AcquiredConns(),
			IdleConns:     stat.IdleConns(),
			TotalConns:    stat.TotalConns(),
		}
		if dbErr != nil {
			resp.Database.Error = dbErr.Error()
		}

		hStart := time.Now()
		hRes := stellarpkg.PingHorizon(r.Context(), deps.httpClient, deps.horizonURL)
		resp.Horizon = dependencyStatus{
			OK:            hRes.OK,
			Endpoint:      hRes.Endpoint,
			Error:         hRes.Error,
			LatencyMillis: time.Since(hStart).Milliseconds(),
			LatestLedger:  hRes.LatestLedger,
		}

		rStart := time.Now()
		rRes := stellarpkg.PingSorobanRPC(r.Context(), deps.httpClient, deps.rpcURL)
		resp.SorobanRPC = dependencyStatus{
			OK:            rRes.OK,
			Endpoint:      rRes.Endpoint,
			Error:         rRes.Error,
			LatencyMillis: time.Since(rStart).Milliseconds(),
			LatestLedger:  rRes.LatestLedger,
		}

		degraded := !resp.Database.OK || !resp.Horizon.OK || !resp.SorobanRPC.OK
		draining := !deps.ready.Load()
		switch {
		case draining:
			resp.Status = "draining"
		case degraded:
			resp.Status = "degraded"
		}

		status := http.StatusOK
		if draining || !resp.Database.OK {
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
