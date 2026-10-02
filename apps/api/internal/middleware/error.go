package middleware

import (
	"errors"
	"net/http"

	"github.com/suncrestlabs/nester/apps/api/pkg/apperror"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// AppHandler is a standard handler signature that can naturally return errors.
type AppHandler func(w http.ResponseWriter, r *http.Request) error

// ErrorHandler wraps an AppHandler and intercepts any domain errors,
// translating them into the standardized JSON envelope responses.
// The correlation request ID (set by the Logging middleware) is included in
// every error envelope so clients can correlate failures back to server logs.
//
// A returned error is translated by its apperror.Kind (issue #1048), which
// covers all eight taxonomy kinds — validation, unauthenticated, forbidden,
// not_found, conflict, quota_exceeded, upstream_unavailable, internal — via
// one dispatch instead of a growing type-switch. Any error that is not an
// *apperror.AppError (a bare `errors.New(...)`, a driver error escaping a
// handler, etc.) is treated as KindInternal: its message is deliberately
// never surfaced to the client, since it may contain SQL or driver detail —
// see TestErrorHandler_NeverLeaksDriverOrSQLText.
func ErrorHandler(h AppHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := h(w, r)
		if err != nil {
			requestID := logpkg.RequestIDFromContext(r.Context())

			var appErr *apperror.AppError
			if !errors.As(err, &appErr) {
				appErr = apperror.NewInternal("INTERNAL_SERVER_ERROR", "internal server error")
			}

			status := appErr.Kind.HTTPStatus()
			resp := response.FromAppError(appErr, requestID)
			response.WriteJSON(w, status, resp)
		}
	}
}
