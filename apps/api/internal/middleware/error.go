package middleware

import (
	"errors"
	"net/http"

	"github.com/suncrestlabs/nester/apps/api/pkg/apperror"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// AppHandler is a standard handler signature that can naturally return errors.
type AppHandler func(w http.ResponseWriter, r *http.Request) error

// ErrorHandler wraps an AppHandler and intercepts any domain errors, 
// translating them into the standardized JSON envelope responses.
func ErrorHandler(h AppHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := h(w, r)
		if err != nil {
			var notFound *apperror.NotFoundError
			var validation *apperror.ValidationError
			var conflict *apperror.ConflictError
			var unauth *apperror.UnauthorizedError

			var resp response.Response
			var status int

			switch {
			case errors.As(err, &notFound):
				status = http.StatusNotFound
				resp = response.Err(status, notFound.Code, notFound.Message)
			case errors.As(err, &validation):
				status = http.StatusBadRequest
				resp = response.Err(status, validation.Code, validation.Message)
			case errors.As(err, &conflict):
				status = http.StatusConflict
				resp = response.Err(status, conflict.Code, conflict.Message)
			case errors.As(err, &unauth):
				status = http.StatusUnauthorized
				resp = response.Err(status, unauth.Code, unauth.Message)
			default:
				status = http.StatusInternalServerError
				resp = response.Err(status, "INTERNAL_SERVER_ERROR", "internal server error")
			}

			response.WriteJSON(w, status, resp)
		}
	}
}
