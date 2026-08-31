package response

import (
	"encoding/json"
	"net/http"
)

type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data"`
	Error   *ErrorBody  `json:"error,omitempty"`
	Meta    *Meta       `json:"meta,omitempty"`
}

type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

type Meta struct {
	Page       int    `json:"page,omitempty"`
	PerPage    int    `json:"per_page,omitempty"`
	TotalCount int    `json:"total_count,omitempty"`
	TotalPages int    `json:"total_pages,omitempty"`
	NextCursor string `json:"next_cursor,omitempty"`
	PrevCursor string `json:"prev_cursor,omitempty"`
}

// OK returns a success response with data
func OK(data interface{}) Response {
	return Response{
		Success: true,
		Data:    data,
	}
}

// PaginatedOK returns a success response with list data and pagination metadata.
func PaginatedOK(data interface{}, page, perPage, totalCount int, nextCursor string) Response {
	totalPages := 0
	if perPage > 0 && totalCount > 0 {
		totalPages = (totalCount + perPage - 1) / perPage
	}
	return Response{
		Success: true,
		Data:    data,
		Meta: &Meta{
			Page:       page,
			PerPage:    perPage,
			TotalCount: totalCount,
			TotalPages: totalPages,
			NextCursor: nextCursor,
		},
	}
}

// PaginatedCursorOK returns a success response with list data and
// bidirectional keyset-cursor pagination metadata (no page/per_page/total
// count — those aren't well-defined for a keyset-paginated feed).
func PaginatedCursorOK(data interface{}, nextCursor, prevCursor string) Response {
	return Response{
		Success: true,
		Data:    data,
		Meta: &Meta{
			NextCursor: nextCursor,
			PrevCursor: prevCursor,
		},
	}
}

// Created returns a success response with data (intended for 201 Created)
func Created(data interface{}) Response {
	return Response{
		Success: true,
		Data:    data,
	}
}

// Err returns an error response
func Err(status int, code string, message string) Response {
	return Response{
		Success: false,
		Error: &ErrorBody{
			Code:    code,
			Message: message,
		},
	}
}

// ErrWithRequestID returns an error response that includes the correlation
// request ID so clients and support can map a failed response back to logs.
func ErrWithRequestID(status int, code string, message string, requestID string) Response {
	return Response{
		Success: false,
		Error: &ErrorBody{
			Code:      code,
			Message:   message,
			RequestID: requestID,
		},
	}
}

// NotFound returns a standard Not Found error response
func NotFound(resource string) Response {
	return Response{
		Success: false,
		Error: &ErrorBody{
			Code:    "NOT_FOUND",
			Message: resource + " not found",
		},
	}
}

// ValidationErr returns a standard validation error response
func ValidationErr(details string) Response {
	return Response{
		Success: false,
		Error: &ErrorBody{
			Code:    "VALIDATION_ERROR",
			Message: details,
		},
	}
}

// WriteJSON writes the given response to the ResponseWriter with the specified status code
func WriteJSON(w http.ResponseWriter, status int, data Response) {
	if data.Error != nil && data.Error.RequestID != "" {
		w.Header().Set("X-Request-ID", data.Error.RequestID)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

