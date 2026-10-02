package logger

import (
	"context"
	"log/slog"
)

type contextKey string

const (
	loggerContextKey        contextKey = "logger"
	requestIDContextKey     contextKey = "request_id"
	correlationIDContextKey contextKey = "correlation_id"
)

func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerContextKey, logger)
}

func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerContextKey).(*slog.Logger); ok && logger != nil {
		return logger
	}
	return slog.Default()
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDContextKey, requestID)
}

func RequestIDFromContext(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDContextKey).(string); ok {
		return requestID
	}
	return ""
}

// WithCorrelationID carries an id for a unit of work that isn't an HTTP
// request - a scheduler tick or background job run - the non-request
// counterpart to WithRequestID (nester#1339). Call once per run/tick so
// every audit entry (or other log line) written during it shares one id.
func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, correlationIDContextKey, correlationID)
}

// CorrelationIDFromContext returns the id set by WithCorrelationID, or ""
// if none was set.
func CorrelationIDFromContext(ctx context.Context) string {
	if correlationID, ok := ctx.Value(correlationIDContextKey).(string); ok {
		return correlationID
	}
	return ""
}
