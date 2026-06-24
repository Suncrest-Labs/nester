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

	"github.com/golang-migrate/migrate/v4"
	migratedb "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	cryptopkg "github.com/suncrestlabs/nester/apps/api/internal/crypto"
	"github.com/suncrestlabs/nester/apps/api/internal/config"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
	"github.com/suncrestlabs/nester/apps/api/internal/handler"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/oracle"
	"github.com/suncrestlabs/nester/apps/api/internal/repository"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	performancesvc "github.com/suncrestlabs/nester/apps/api/internal/service/performance"
	tvlsvc "github.com/suncrestlabs/nester/apps/api/internal/service/tvl"
	"github.com/suncrestlabs/nester/apps/api/internal/services"
	stellarpkg "github.com/suncrestlabs/nester/apps/api/internal/stellar"
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

	pgPool, err := repository.NewPostgresDB(cfg.Database())
	if err != nil {
		return err
	}
	defer pgPool.Pool.Close()

	db := stdlib.OpenDBFromPool(pgPool.Pool)
	defer db.Close()

	if cfg.Startup().EnableAutoMigrate() {
		baseLogger.Info("running database migrations", "dir", cfg.Startup().MigrationsDir())

		driver, err := migratedb.WithInstance(db, &migratedb.Config{})
		if err != nil {
			return fmt.Errorf("auto-migrate: init driver: %w", err)
		}

		m, err := migrate.NewWithDatabaseInstance(
			"file://"+cfg.Startup().MigrationsDir(),
			"postgres", driver)
		if err != nil {
			return fmt.Errorf("auto-migrate: new migrate instance: %w", err)
		}

		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			return fmt.Errorf("auto-migrate: up: %w", err)
		}

		baseLogger.Info("database migrations complete")
	} else {
		baseLogger.Info("auto-migrate disabled; skipping migrations")
	}

	if err := pingStellarDependencies(baseLogger, cfg); err != nil {
		return err
	}

	systemStateRepository := postgres.NewSystemStateRepository(db)

	vaultRepository := postgres.NewVaultRepository(db)
	vaultService := service.NewVaultService(vaultRepository)
	vaultService.SetHarvestDefaultCompound(cfg.Stellar().HarvestDefaultCompound())
	vaultHandler := handler.NewVaultHandler(vaultService)

	portfolioService := service.NewPortfolioService(vaultRepository)
	portfolioHandler := handler.NewPortfolioHandler(portfolioService)

	transactionRepository := postgres.NewTransactionRepository(db)
	transactionService := service.NewTransactionService(transactionRepository, cfg.Stellar().HorizonURL())
	// Balance is moved only after a deposit/withdrawal is confirmed on-chain
	// (issue #496); the vault repository applies it idempotently by tx hash.
	transactionService.SetBalanceApplier(vaultRepository)
	transactionHandler := handler.NewTransactionHandler(transactionService)

	bankAccountRepository := postgres.NewBankAccountRepository(db)
	var accountCipher *cryptopkg.AccountCipher
	if key := cfg.BankAccountEncryptionKey(); key != "" {
		cipher, cipherErr := cryptopkg.NewAccountCipher(key)
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
	userHandler := handler.NewUserHandler(userService)
	userVaultsSvc := service.NewUserVaultsService(vaultRepository)
	userHandler.SetUserVaultsService(userVaultsSvc)
	notificationRepository := postgres.NewNotificationRepository(db)
	notificationHandler := handler.NewNotificationHandler(notificationRepository)

	settlementRepository := postgres.NewSettlementRepository(db)
	settlementService := service.NewSettlementService(settlementRepository, bankAccountService)
	settlementHandler := handler.NewSettlementHandler(settlementService, userService)

	adminRepository := postgres.NewAdminRepository(db)

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
	adminHandler := handler.NewAdminHandler(adminService, userService)
	adminHandler.SetEventSyncer(&stellarpkg.EventSyncer{
		DB:      db,
		SysRepo: systemStateRepository,
		RPCURL:  cfg.Stellar().RPCURL(),
		Logger:  baseLogger,
	})

	var challengeStore service.ChallengeStore
	if addr := cfg.Redis().Addr(); addr != "" {
		redisClient := redis.NewClient(&redis.Options{Addr: addr})
		challengeStore = service.NewRedisChallengeStore(redisClient, cfg.Auth().ChallengeExpiry())
		baseLogger.Info("challenge store: redis", "addr", addr)
	} else {
		challengeStore = service.NewInMemoryChallengeStore(cfg.Auth().ChallengeExpiry())
		baseLogger.Info("challenge store: in-memory (single-instance only)")
	}

	authService := service.NewAuthService(challengeStore, userService, cfg.Auth())
	authHandler := handler.NewAuthHandler(authService)

	oracleService := oracle.NewRateService(cfg.Stellar().HorizonURL(), cfg.Stellar().USDCIssuer())
	rateHandler := handler.NewRateHandler(oracleService)

	wsHub := ws.NewHub(baseLogger.WithGroup("websocket"), func(token string) (string, error) {
		if token == "" {
			return "", fmt.Errorf("missing token")
		}
		claims, err := auth.ParseJWT(token, cfg.Auth().Secret())
		if err != nil {
			return "", fmt.Errorf("invalid token: %w", err)
		}
		return claims.Subject, nil
	}, cfg.AllowedOrigins())

	wsCtx, wsCancel := context.WithCancel(context.Background())
	defer wsCancel()
	go wsHub.Run(wsCtx)
	vaultHandler.SetWSHub(wsHub)

	performanceRepository := postgres.NewPerformanceRepository(db)
	vaultRepository = postgres.NewVaultRepository(db)
	performanceService := performancesvc.NewService(performanceRepository, vaultRepository)
	performanceHandler := handler.NewPerformanceHandler(performanceService, handler.NewVaultOwnerAdapter(vaultRepository))

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
	txPoller := service.NewTransactionPoller(
		service.TransactionPollerConfig{
			Enabled:  cfg.TransactionPoller().Enabled(),
			Interval: cfg.TransactionPoller().Interval(),
			MinAge:   cfg.TransactionPoller().MinAge(),
		},
		transactionService,
		func(_ context.Context, tx transaction.Transaction) {
			wsHub.BroadcastEvent(transactionStatusEvent(tx))
		},
		baseLogger.WithGroup("tx-poller"),
	)
	pollerCtx, cancelPoller := context.WithCancel(context.Background())
	defer cancelPoller()
	go txPoller.Run(pollerCtx)

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
	vaultHandler.Register(mux)
	portfolioHandler.Register(mux)
	transactionHandler.Register(mux)
	settlementHandler.Register(mux)
	userHandler.Register(mux)
	notificationHandler.Register(mux)
	adminHandler.Register(mux)
	authHandler.Register(mux)
	rateHandler.Register(mux)
	performanceHandler.Register(mux)
	tvlHandler.Register(mux)
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
	yieldHandler := handler.NewYieldHandler(yieldSvc)
	yieldHandler.Register(mux)

	// User watchlist
	watchlistSvc := service.NewWatchlistService(db)
	watchlistHandler := handler.NewWatchlistHandler(watchlistSvc)
	watchlistHandler.Register(mux)

	// Savings goals
	savingsGoalRepo := postgres.NewSavingsGoalRepository(db)
	savingsGoalSvc := service.NewSavingsGoalService(savingsGoalRepo)
	savingsGoalHandler := handler.NewSavingsGoalHandler(savingsGoalSvc)
	savingsGoalHandler.Register(mux)

	// User vault rebalance (suggestions + execution)
	vaultRebalanceSvc := service.NewVaultRebalanceService(vaultRepository, adminService)
	vaultHandler.SetRebalanceService(vaultRebalanceSvc)

	// Intelligence proxy (forwards to Python service)
	intelURL := cfg.Intelligence().ServiceURL()
	intelProxy := service.NewIntelligenceProxy(intelURL, cfg.Intelligence().Timeout())
	prometheusClient := service.NewPrometheusClient(service.PrometheusConfig{
		BaseURL: intelURL,
		APIKey:  cfg.Intelligence().ServiceAPIKey(),
		Timeout: cfg.Intelligence().Timeout(),
	})
	intelligenceHandler := handler.NewIntelligenceHandler(intelProxy, prometheusClient)
	intelligenceHandler.Register(mux)

	intelRelay := service.NewRelayHandler(http.DefaultClient, service.RelayConfig{
		BaseURL: intelURL,
		APIKey:  cfg.Intelligence().ServiceAPIKey(),
		Timeout: cfg.Intelligence().Timeout(),
	})
	intelligenceRelayHandler := handler.NewIntelligenceRelayHandler(intelRelay)
	intelligenceRelayHandler.Register(mux)

	performanceSnapshotsHandler := handler.NewPerformanceSnapshotsHandler(performanceService)
	performanceSnapshotsHandler.Register(mux)

	bankHandler.Register(mux)
	bankAccountHandler.Register(mux)

	mux.HandleFunc("GET /ws", wsHub.ServeWs)

	authRules := []middleware.RouteRule{
		{PathPrefix: "/health", Public: true},
		{PathPrefix: "/healthz", Public: true},
		{PathPrefix: "/readyz", Public: true},
		{PathPrefix: "/ws", Public: true},
		{PathPrefix: "/api/v1/auth/", Public: true},
		{PathPrefix: "/api/v1/banks/", Public: true},
		{PathPrefix: "/api/v1/admin/", Public: false, Role: "admin"},
		{PathPrefix: "/api/v1/", Public: false},
	}
	authenticator := middleware.Authenticate(cfg.Auth().Secret(), cfg.Auth().ServiceAPIKey(), authRules)
	globalLimiter := middleware.IPRateLimiter(cfg.RateLimit().GlobalLimit(), cfg.RateLimit().GlobalWindow())
	writeLimiter := middleware.WriteMethodRateLimiter(cfg.RateLimit().WriteLimit(), cfg.RateLimit().WriteWindow())
	walletLimiter := middleware.WalletRateLimiter(
		cfg.RateLimit().WalletLimit(),
		cfg.RateLimit().WalletWindow(),
		walletKeyFromContext,
	)
	cors := middleware.CORS(cfg.AllowedOrigins())

	server := &http.Server{
		Addr: cfg.Server().Address(),
		Handler: middleware.SecurityHeaders(cfg.Environment())(
			middleware.RecoverPanic(baseLogger)(
				globalLimiter(
					cors(
						writeLimiter(
							authenticator(
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

	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
