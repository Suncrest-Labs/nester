package apperror

type NotFoundError struct {
	Code    string
	Message string
}

func (e *NotFoundError) Error() string { return e.Message }

type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

type ConflictError struct {
	Code    string
	Message string
}

func (e *ConflictError) Error() string { return e.Message }

type UnauthorizedError struct {
	Code    string
	Message string
}

func (e *UnauthorizedError) Error() string { return e.Message }

// Helper constructors

func NewNotFound(code, message string) *NotFoundError {
	return &NotFoundError{Code: code, Message: message}
}

func NewValidation(code, message string) *ValidationError {
	return &ValidationError{Code: code, Message: message}
}

func NewConflict(code, message string) *ConflictError {
	return &ConflictError{Code: code, Message: message}
}

func NewUnauthorized(code, message string) *UnauthorizedError {
	return &UnauthorizedError{Code: code, Message: message}
}
