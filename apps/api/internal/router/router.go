package router

import (
	"net/http"

	"github.com/suncrestlabs/nester/apps/api/internal/handler"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"log/slog"
)

// Config holds the dependencies for the router.
type Config struct {
	Logger            *slog.Logger
	VaultHandler      *handler.VaultHandler
	SettlementHandler *handler.SettlementHandler
	HealthCheck       http.HandlerFunc
}

// New assembles all routes and applies global middleware.
func New(cfg Config) http.Handler {
	mux := http.NewServeMux()

	// Register Handlers
	cfg.VaultHandler.Register(mux)
	cfg.SettlementHandler.Register(mux)

	if cfg.HealthCheck != nil {
		mux.HandleFunc("GET /health", cfg.HealthCheck)
		mux.HandleFunc("GET /healthz", cfg.HealthCheck)
	}

	// Build middleware stack (inner to outer)
	// mux -> Logging -> RateLimit -> SecurityHeaders -> RecoverPanic
	
	handler := http.Handler(mux)
	handler = middleware.Logging(cfg.Logger)(handler)
	handler = middleware.RateLimit(handler)
	handler = middleware.SecurityHeaders(handler)
	handler = middleware.RecoverPanic(cfg.Logger)(handler)

	return handler
}

// ValidateURLLength returns a middleware that rejects requests with URLs exceeding maxLen.
func ValidateURLLength(maxLen int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(r.URL.String()) > maxLen {
				http.Error(w, "request URI too long", http.StatusRequestURITooLong)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
