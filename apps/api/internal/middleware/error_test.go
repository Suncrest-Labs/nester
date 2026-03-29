package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/suncrestlabs/nester/apps/api/pkg/apperror"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

func TestErrorHandler(t *testing.T) {
	tests := []struct {
		name           string
		returnedError  error
		expectedStatus int
		expectedCode   string
		expectedMsg    string
	}{
		{
			name:           "Not Found Error",
			returnedError:  apperror.NewNotFound("NOT_FOUND", "vault not found"),
			expectedStatus: http.StatusNotFound,
			expectedCode:   "NOT_FOUND",
			expectedMsg:    "vault not found",
		},
		{
			name:           "Validation Error",
			returnedError:  apperror.NewValidation("VALIDATION_ERROR", "invalid input"),
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "VALIDATION_ERROR",
			expectedMsg:    "invalid input",
		},
		{
			name:           "Conflict Error",
			returnedError:  apperror.NewConflict("CONFLICT", "resource exists"),
			expectedStatus: http.StatusConflict,
			expectedCode:   "CONFLICT",
			expectedMsg:    "resource exists",
		},
		{
			name:           "Unauthorized Error",
			returnedError:  apperror.NewUnauthorized("UNAUTHORIZED", "invalid token"),
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "UNAUTHORIZED",
			expectedMsg:    "invalid token",
		},
		{
			name:           "Unknown Error",
			returnedError:  errors.New("some unexpected database error"),
			expectedStatus: http.StatusInternalServerError,
			expectedCode:   "INTERNAL_SERVER_ERROR",
			expectedMsg:    "internal server error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := ErrorHandler(func(w http.ResponseWriter, r *http.Request) error {
				return tc.returnedError
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			assert.Equal(t, tc.expectedStatus, w.Code)
			
			var body response.Response
			err := json.NewDecoder(w.Body).Decode(&body)
			assert.NoError(t, err)
			
			assert.False(t, body.Success)
			assert.NotNil(t, body.Error)
			assert.Equal(t, tc.expectedCode, body.Error.Code)
			assert.Equal(t, tc.expectedMsg, body.Error.Message)
		})
	}
}
