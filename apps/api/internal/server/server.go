// Package server wires together the HTTP mux, middleware chain, and graceful
// shutdown logic so that each piece can be tested independently.
package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/router"
)

const defaultMaxBodyBytes int64 = 64 * 1024 // 64 KB
const maxURLLength = 2048

// New assembles the HTTP handler and returns it along with the underlying mux
// so callers can register additional routes.  readiness is called by the health
// endpoints (/health, /healthz) to signal whether the server is ready to serve.
func New(logger *slog.Logger, readiness func(context.Context) error) (http.Handler, *http.ServeMux) {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", healthHandler(readiness))
	mux.HandleFunc("GET /healthz", healthHandler(readiness))

	h := http.Handler(mux)
	h = middleware.Logging(logger)(h)
	h = middleware.CORS(h)
	h = middleware.IPRateLimiter(100, time.Minute)(h)
	h = middleware.SecurityHeaders(h)
	h = middleware.RecoverPanic(logger)(h)
	h = router.ValidateURLLength(maxURLLength)(h)
	h = middleware.LimitRequestBody(defaultMaxBodyBytes)(h)

	return h, mux
}

func healthHandler(readiness func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		w.Header().Set("Content-Type", "application/json")

		if err := readiness(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"status": "unavailable", "error": err.Error()})
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
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
