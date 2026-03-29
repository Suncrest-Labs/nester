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
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Meta struct {
	Page       int `json:"page,omitempty"`
	PerPage    int `json:"per_page,omitempty"`
	TotalCount int `json:"total_count,omitempty"`
}

// OK returns a success response with data
func OK(data interface{}) Response {
	return Response{
		Success: true,
		Data:    data,
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

