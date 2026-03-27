// Package server wires together the HTTP mux, middleware chain, and graceful
// shutdown logic so that each piece can be tested independently.
package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/handler"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/router"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

const defaultMaxBodyBytes int64 = 64 * 1024 // 64 KB
const maxURLLength = 2048

// New assembles the full HTTP handler using the consolidated router.
func New(logger *slog.Logger, vaultSvc *service.VaultService, settlementSvc *service.SettlementService, healthCheck http.HandlerFunc) http.Handler {
	vh := handler.NewVaultHandler(vaultSvc)
	sh := handler.NewSettlementHandler(settlementSvc)

	h := router.New(router.Config{
		Logger:            logger,
		VaultHandler:      vh,
		SettlementHandler: sh,
		HealthCheck:       healthCheck,
	})

	// Wrap with request limits
	h = router.ValidateURLLength(maxURLLength)(h)
	h = middleware.LimitRequestBody(defaultMaxBodyBytes)(h)

	return h
}

// RunWithGracefulShutdown starts srv and blocks until ctx is cancelled, then
// shuts down with the given timeout.  It returns any server or shutdown error.
func RunWithGracefulShutdown(ctx context.Context, srv *http.Server, timeout time.Duration) error {
	serverErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := srv.Shutdown(shutCtx); err != nil {
		return err
	}
	return <-serverErr
}
