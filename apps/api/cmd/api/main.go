package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/suncrestlabs/nester/apps/api/internal/config"
	"github.com/suncrestlabs/nester/apps/api/internal/handler"
	"github.com/suncrestlabs/nester/apps/api/internal/indexer"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/repository"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
	"github.com/suncrestlabs/nester/apps/api/internal/server"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/internal/stellar"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
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

	// Initialize Stellar Client
	stellarClient, err := stellar.NewClient(context.Background(), stellar.Config{
		NetworkID:    cfg.Stellar().NetworkPassphrase(),
		RPCURL:       cfg.Stellar().RPCURL(),
		SourceKey:    cfg.Stellar().SourceKey(),
		Network:      stellar.Testnet, // Default or from config
	})
	if err != nil {
		return fmt.Errorf("stellar client: %w", err)
	}

	// Initialize Indexer
	idx := indexer.New(db, stellarClient, baseLogger)

	// Handle CLI commands
	if len(os.Args) > 1 && os.Args[1] == "index" {
		if len(os.Args) > 2 && os.Args[2] == "backfill" {
			backfillCmd := flag.NewFlagSet("backfill", flag.ExitOnError)
			fromLedger := backfillCmd.Uint64("from-ledger", 0, "Ledger to start backfill from")
			if err := backfillCmd.Parse(os.Args[3:]); err != nil {
				return err
			}
			if *fromLedger == 0 {
				return errors.New("--from-ledger is required and must be > 0")
			}
			baseLogger.Info("starting manual backfill", "ledger", *fromLedger)
			return idx.BackfillManual(context.Background(), *fromLedger)
		}
		return errors.New("unknown index command: " + os.Args[2])
	}

	vaultRepository := postgres.NewVaultRepository(db)
	vaultService := service.NewVaultService(vaultRepository)
	vaultHandler := handler.NewVaultHandler(vaultService)

	settlementRepository := postgres.NewSettlementRepository(db)
	settlementService := service.NewSettlementService(settlementRepository)
	settlementHandler := handler.NewSettlementHandler(settlementService)

<<<<<<< HEAD
	h := server.New(
		baseLogger,
		vaultService,
		settlementService,
		healthHandler(db, cfg.Database().ConnectionTimeout()),
	)

	srv := &http.Server{
		Addr:         cfg.Server().Address(),
		Handler:      h,
=======
	userRepository := postgres.NewUserRepository(db)
	userService := service.NewUserService(userRepository)
	userHandler := handler.NewUserHandler(userService)

	authService := service.NewAuthService(userService, cfg.Auth())
	authHandler := handler.NewAuthHandler(authService)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler(pgPool, cfg.Database().ConnectionTimeout()))
	mux.HandleFunc("GET /healthz", healthHandler(pgPool, cfg.Database().ConnectionTimeout()))
	mux.HandleFunc("GET /readyz", healthHandler(pgPool, cfg.Database().ConnectionTimeout()))
	vaultHandler.Register(mux)
	settlementHandler.Register(mux)
	userHandler.Register(mux)
	authHandler.Register(mux)

	authRules := []middleware.RouteRule{
		{PathPrefix: "/health", Public: true},
		{PathPrefix: "/healthz", Public: true},
		{PathPrefix: "/readyz", Public: true},
		{PathPrefix: "/api/v1/auth/", Public: true},
		{PathPrefix: "/api/v1/", Public: false},
	}
	authenticator := middleware.Authenticate(cfg.Auth().Secret(), authRules)
	// Global rate limit applies to all requests per IP.
	globalLimiter := middleware.IPRateLimiter(cfg.RateLimit().GlobalLimit(), cfg.RateLimit().GlobalWindow())
	// Write rate limit is stricter and applies only to mutating methods (POST/PUT/PATCH/DELETE).
	writeLimiter := middleware.WriteMethodRateLimiter(cfg.RateLimit().WriteLimit(), cfg.RateLimit().WriteWindow())

	server := &http.Server{
		Addr: cfg.Server().Address(),
		Handler: middleware.RecoverPanic(baseLogger)(
			globalLimiter(
				writeLimiter(
					authenticator(
						middleware.LimitRequestBody(1*1024*1024)(
							middleware.Logging(baseLogger)(mux),
						),
					),
				),
			),
		),
>>>>>>> origin/main
		ReadTimeout:  cfg.Server().ReadTimeout(),
		WriteTimeout: cfg.Server().WriteTimeout(),
	}

	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start Indexer in background
	go func() {
		if err := idx.Start(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
			baseLogger.Error("indexer stopped unexpectedly", "error", err)
		}
	}()

	baseLogger.Info("starting server", "addr", cfg.Server().Address(), "environment", cfg.Environment())

	serverErr := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
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

	timeoutCtx, cancel := context.WithTimeout(context.Background(), cfg.Server().GracefulShutdown())
	defer cancel()

	if err := srv.Shutdown(timeoutCtx); err != nil {
		return err
	}

	if err := <-serverErr; err != nil {
		return err
	}

	baseLogger.Info("server stopped")
	return nil
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
