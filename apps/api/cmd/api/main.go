package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/suncrestlabs/nester/apps/api/internal/config"
	"github.com/suncrestlabs/nester/apps/api/internal/handler"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
	"github.com/suncrestlabs/nester/apps/api/internal/server"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
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

	db, err := openDatabase(cfg)
	if err != nil {
		return err
	}
	defer db.Close()

	vaultRepository := postgres.NewVaultRepository(db)
	vaultService := service.NewVaultService(vaultRepository)
	vaultHandler := handler.NewVaultHandler(vaultService)

	settlementRepository := postgres.NewSettlementRepository(db)
	settlementService := service.NewSettlementService(settlementRepository)
	settlementHandler := handler.NewSettlementHandler(settlementService)

	h := server.New(
		baseLogger,
		vaultService,
		settlementService,
		healthHandler(db, cfg.Database().ConnectionTimeout()),
	)

	srv := &http.Server{
		Addr:         cfg.Server().Address(),
		Handler:      h,
		ReadTimeout:  cfg.Server().ReadTimeout(),
		WriteTimeout: cfg.Server().WriteTimeout(),
	}

	baseLogger.Info("starting server", "addr", cfg.Server().Address(), "environment", cfg.Environment())

	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server().GracefulShutdown())
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return err
	}

	if err := <-serverErr; err != nil {
		return err
	}

	baseLogger.Info("server stopped")
	return nil
}

func openDatabase(cfg *config.Config) (*sql.DB, error) {
	db, err := sql.Open("pgx", cfg.Database().DSN())
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(cfg.Database().PoolSize())
	db.SetMaxIdleConns(min(5, cfg.Database().PoolSize()))

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Database().ConnectionTimeout())
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func healthHandler(db *sql.DB, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		if err := db.PingContext(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}
