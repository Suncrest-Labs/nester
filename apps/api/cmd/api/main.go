package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/redis/go-redis/v9"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/config"
	"github.com/suncrestlabs/nester/apps/api/internal/handler"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/oracle"
	"github.com/suncrestlabs/nester/apps/api/internal/repository"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	performancesvc "github.com/suncrestlabs/nester/apps/api/internal/service/performance"
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

	vaultRepository := postgres.NewVaultRepository(db)
	vaultService := service.NewVaultService(vaultRepository)
	vaultHandler := handler.NewVaultHandler(vaultService)

	settlementRepository := postgres.NewSettlementRepository(db)
	settlementService := service.NewSettlementService(settlementRepository)
	settlementHandler := handler.NewSettlementHandler(settlementService)

	userRepository := postgres.NewUserRepository(db)
	userService := service.NewUserService(userRepository)
	userHandler := handler.NewUserHandler(userService)

	adminRepository := postgres.NewAdminRepository(db)

	var chainInvoker service.VaultChainInvoker
	if secret := cfg.Stellar().OperatorSecret(); secret != "" {
		inv, err := service.NewSorobanVaultChainInvoker(
			cfg.Stellar().RPCURL(),
			cfg.Stellar().HorizonURL(),
			cfg.Stellar().NetworkPassphrase(),
			secret,
		)
		if err != nil {
			return fmt.Errorf("init chain invoker: %w", err)
		}
		chainInvoker = inv
		vaultService.SetDepositInvoker(inv)
	}

	adminService := service.NewAdminService(
		adminRepository,
		chainInvoker,
		cfg.Stellar().HorizonURL(),
		cfg.SettlementProviderURL(),
	)
	adminHandler := handler.NewAdminHandler(adminService)

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

	oracleService := oracle.NewRateService(cfg.Stellar().HorizonURL())
	rateHandler := handler.NewRateHandler(oracleService)

	performanceRepository := postgres.NewPerformanceRepository(db)
	performanceService := performancesvc.NewService(performanceRepository)
	performanceHandler := handler.NewPerformanceHandler(performanceService)

	tracker := performancesvc.NewTracker(
		performanceRepository,
		vaultRepository,
		nil, // BalanceProvider: wire to a Stellar adapter once the on-chain reader is exposed.
		cfg.Performance().SnapshotInterval(),
	)
	trackerCtx, cancelTracker := context.WithCancel(context.Background())
	defer cancelTracker()
	go func() {
		if err := tracker.Run(trackerCtx); err != nil && !errors.Is(err, context.Canceled) {
			baseLogger.Error("performance tracker stopped", "error", err.Error())
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler(pgPool, cfg.Database().ConnectionTimeout()))
	mux.HandleFunc("GET /healthz", healthHandler(pgPool, cfg.Database().ConnectionTimeout()))
	mux.HandleFunc("GET /readyz", healthHandler(pgPool, cfg.Database().ConnectionTimeout()))
	vaultHandler.Register(mux)
	settlementHandler.Register(mux)
	userHandler.Register(mux)
	adminHandler.Register(mux)
	authHandler.Register(mux)
	rateHandler.Register(mux)
	performanceHandler.Register(mux)

	authRules := []middleware.RouteRule{
		{PathPrefix: "/health", Public: true},
		{PathPrefix: "/healthz", Public: true},
		{PathPrefix: "/readyz", Public: true},
		{PathPrefix: "/api/v1/auth/", Public: true},
		{PathPrefix: "/api/v1/admin/", Public: false, Role: "admin"},
		{PathPrefix: "/api/v1/", Public: false},
	}
	authenticator := middleware.Authenticate(cfg.Auth().Secret(), authRules)
	// Global rate limit applies to all requests per IP.
	globalLimiter := middleware.IPRateLimiter(cfg.RateLimit().GlobalLimit(), cfg.RateLimit().GlobalWindow())
	// Write rate limit is stricter and applies only to mutating methods (POST/PUT/PATCH/DELETE).
	writeLimiter := middleware.WriteMethodRateLimiter(cfg.RateLimit().WriteLimit(), cfg.RateLimit().WriteWindow())
	// Wallet rate limit runs inside Authenticate so the caller's wallet address
	// is in context. Unauthenticated requests produce an empty key and pass
	// through — public routes are covered by the IP and write limiters above.
	walletLimiter := middleware.WalletRateLimiter(
		cfg.RateLimit().WalletLimit(),
		cfg.RateLimit().WalletWindow(),
		walletKeyFromContext,
	)
	// CORS sits inside rate limiting (preflights count against the bucket) and
	// outside auth (preflights don't carry credentials and must short-circuit
	// before Authenticate rejects them).
	cors := middleware.CORS(cfg.AllowedOrigins())

	server := &http.Server{
		Addr: cfg.Server().Address(),
		Handler: middleware.RecoverPanic(baseLogger)(
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
		ReadTimeout:  cfg.Server().ReadTimeout(),
		WriteTimeout: cfg.Server().WriteTimeout(),
	}

	baseLogger.Info("starting server", "addr", cfg.Server().Address(), "environment", cfg.Environment())

	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
		baseLogger.Info("shutdown signal received")
	}

	stop()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server().GracefulShutdown())
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		return err
	}

	if err := <-serverErr; err != nil {
		return err
	}

	baseLogger.Info("server stopped")
	return nil
}

// walletKeyFromContext returns the authenticated caller's wallet address for
// per-wallet rate limiting. An empty string means no key is available (public
// route or claims without a wallet) — WalletRateLimiter passes those through.
func walletKeyFromContext(r *http.Request) string {
	u, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		return ""
	}
	return u.WalletAddress
}

func healthHandler(db *repository.PostgresDB, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}
