package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
)

// RecoverPanic catches any panic that escapes a handler, logs the stack trace,
// and writes a 500 JSON error response so the server process keeps running.
func RecoverPanic(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					stack := string(debug.Stack())
					// RecoverPanic sits outermost — the Logging middleware may not
					// have injected a logger into the context yet.  Use the base
					// logger and enrich with the request ID if it exists.
					log := logger
					if rid := logpkg.RequestIDFromContext(r.Context()); rid != "" {
						log = logger.With("request_id", rid)
					}
					log.Error(
						"panic recovered",
						"panic", rec,
						"stack", stack,
					)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(
						`{"success":false,"error":{"code":500,"message":"internal server error"}}`,
					))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
