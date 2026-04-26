package middleware

import (
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"

	"log/slog"

	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func Logging(baseLogger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := uuid.NewString()
			requestLogger := baseLogger.With("request_id", requestID)

			ctx := logpkg.WithRequestID(r.Context(), requestID)
			ctx = logpkg.WithLogger(ctx, requestLogger)

			startedAt := time.Now()
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			request := r.WithContext(ctx)

			// Add request ID to response header for client-side tracing
			w.Header().Set("X-Request-ID", requestID)

			requestLogger.Info("request started",
				"method", r.Method,
				"path", r.URL.Path,
				"remote_addr", r.RemoteAddr,
			)

			next.ServeHTTP(recorder, request)

			duration := time.Since(startedAt)
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", recorder.status,
				"duration_ms", duration.Milliseconds(),
			}

			if recorder.status >= http.StatusInternalServerError {
				attrs = append(attrs, "stack", string(debug.Stack()))
				requestLogger.Error("request completed", attrs...)
				return
			}

			requestLogger.Info("request completed", attrs...)
		})
	}
}

// LimitRequestBody wraps an http.Handler and enforces a maximum request body size.
// Requests exceeding the limit will receive a 413 Payload Too Large response.
func LimitRequestBody(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}
