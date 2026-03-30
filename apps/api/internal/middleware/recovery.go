package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/suncrestlabs/nester/apps/api/pkg/response"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
)

// RecoverPanic catches any panic that escapes a handler, logs the stack trace,
// and writes a standardized 500 JSON error response so the server process keeps running.
func RecoverPanic(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					stack := string(debug.Stack())
					log := logger
					if rid := logpkg.RequestIDFromContext(r.Context()); rid != "" {
						log = logger.With("request_id", rid)
					}
					log.Error(
						"panic recovered",
						"panic", rec,
						"stack", stack,
					)
					
					resp := response.Err(http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "internal server error")
					response.WriteJSON(w, http.StatusInternalServerError, resp)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
