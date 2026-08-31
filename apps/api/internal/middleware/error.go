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
func ErrorHandler(h AppHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := h(w, r)
		if err != nil {
			requestID := logpkg.RequestIDFromContext(r.Context())

			var notFound *apperror.NotFoundError
			var validation *apperror.ValidationError
			var conflict *apperror.ConflictError
			var unauth *apperror.UnauthorizedError

			var resp response.Response
			var status int

			switch {
			case errors.As(err, &notFound):
				status = http.StatusNotFound
				resp = response.ErrWithRequestID(status, notFound.Code, notFound.Message, requestID)
			case errors.As(err, &validation):
				status = http.StatusBadRequest
				resp = response.ErrWithRequestID(status, validation.Code, validation.Message, requestID)
			case errors.As(err, &conflict):
				status = http.StatusConflict
				resp = response.ErrWithRequestID(status, conflict.Code, conflict.Message, requestID)
			case errors.As(err, &unauth):
				status = http.StatusUnauthorized
				resp = response.ErrWithRequestID(status, unauth.Code, unauth.Message, requestID)
			default:
				status = http.StatusInternalServerError
				resp = response.ErrWithRequestID(status, "INTERNAL_SERVER_ERROR", "internal server error", requestID)
			}

			response.WriteJSON(w, status, resp)
		}
	}
}
