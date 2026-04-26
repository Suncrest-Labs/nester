// Package server wires together the HTTP mux, middleware chain, and graceful
// shutdown logic so that each piece can be tested independently.
package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
)

const defaultMaxBodyBytes int64 = 1 << 20 // 1 MiB

// HealthChecker is a function that returns nil when the service is healthy.
// Callers may supply a database ping, a no-op, or a stub for tests.
type HealthChecker func(ctx context.Context) error

// New assembles the full HTTP handler: panic recovery → CORS → request-size
// limit → structured logging → mux.  Routes are registered via the returned
// *Mux. allowedOrigins is the list of origins permitted to make cross-origin
// requests; an empty slice disables cross-origin access.
//
// The returned http.Handler is ready to pass to http.Server.
func New(logger *slog.Logger, checker HealthChecker, allowedOrigins []string) (http.Handler, *http.ServeMux) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler(checker))
	mux.HandleFunc("GET /healthz", healthHandler(checker))

	// Build the middleware stack (outermost first):
	// RecoverPanic → CORS → LimitRequestBody → Logging → mux
	handler := middleware.RecoverPanic(logger)(
		middleware.CORS(allowedOrigins)(
			middleware.LimitRequestBody(defaultMaxBodyBytes)(
				middleware.Logging(logger)(mux),
			),
		),
	)
	return handler, mux
}

// RunWithGracefulShutdown starts srv via ListenAndServe and blocks until ctx
// is cancelled, then shuts down with the given timeout.
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

// ServeWithGracefulShutdown serves on an existing listener and blocks until
// ctx is cancelled, then shuts down with the given timeout. Use this instead
// of RunWithGracefulShutdown when a pre-allocated listener is required (e.g.
// for tests that need a random free port).
func ServeWithGracefulShutdown(ctx context.Context, srv *http.Server, ln net.Listener, timeout time.Duration) error {
	serverErr := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
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

// ---------------------------------------------------------------------------
// Private helpers
// ---------------------------------------------------------------------------

func healthHandler(checker HealthChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if checker != nil {
			if err := checker(r.Context()); err != nil {
				http.Error(w, "service unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}
}
