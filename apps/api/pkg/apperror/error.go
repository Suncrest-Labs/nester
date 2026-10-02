// Package apperror defines the API's closed error taxonomy (issue #1048).
//
// Every domain error a handler returns must be one of the Kinds below, each
// of which maps to exactly one HTTP status and a fixed retry semantic. This
// is a *closed* set deliberately: an open set of ad-hoc error strings is
// what let the same failure surface differently depending on which handler
// produced it, leaving clients unable to tell "you sent bad input" (do not
// retry) from "the upstream chain is unavailable" (retry after a delay).
//
// The taxonomy is part of the public API contract — Kind values and their
// HTTP/Retryable mapping must not change without a version bump.
package apperror

import "net/http"

// Kind is one of the eight closed error categories every AppError belongs
// to. New values must not be added without updating HTTPStatus, Retryable,
// and the response envelope's documented contract in lockstep.
type Kind string

const (
	KindValidation          Kind = "validation"
	KindUnauthenticated     Kind = "unauthenticated"
	KindForbidden           Kind = "forbidden"
	KindNotFound            Kind = "not_found"
	KindConflict            Kind = "conflict"
	KindQuotaExceeded       Kind = "quota_exceeded"
	KindUpstreamUnavailable Kind = "upstream_unavailable"
	KindInternal            Kind = "internal"
)

// HTTPStatus returns the single HTTP status code a Kind maps to. Every Kind
// maps to exactly one status — this is asserted by TestKindHTTPStatusMapping
// so the mapping can never silently drift.
func (k Kind) HTTPStatus() int {
	switch k {
	case KindValidation:
		return http.StatusBadRequest
	case KindUnauthenticated:
		return http.StatusUnauthorized
	case KindForbidden:
		return http.StatusForbidden
	case KindNotFound:
		return http.StatusNotFound
	case KindConflict:
		return http.StatusConflict
	case KindQuotaExceeded:
		return http.StatusTooManyRequests
	case KindUpstreamUnavailable:
		return http.StatusServiceUnavailable
	case KindInternal:
		return http.StatusInternalServerError
	default:
		// An unrecognised Kind is a programming error, not a client fault —
		// fail safe to 500 rather than guess.
		return http.StatusInternalServerError
	}
}

// Retryable reports whether a client should retry a request that failed
// with this Kind. quota_exceeded and upstream_unavailable are retryable
// (after a delay); every other kind describes a request that will fail
// again unmodified, so retrying it is never correct.
func (k Kind) Retryable() bool {
	switch k {
	case KindQuotaExceeded, KindUpstreamUnavailable:
		return true
	default:
		return false
	}
}

// FieldDetail is one field-level validation failure, included on a
// KindValidation error so a client can highlight the specific offending
// field rather than parsing the message.
type FieldDetail struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// AppError is the single error type every handler should return for a
// domain failure. Code is a stable, machine-readable string scoped within
// its Kind (e.g. "VAULT_NOT_FOUND"); Message is safe to show to an end
// user. Details is populated only for KindValidation and lists individual
// field failures.
//
// AppError deliberately carries no stack trace, driver error, or SQL text —
// wrap the underlying cause with Unwrap() below for server-side logging,
// never for the client-facing Message.
type AppError struct {
	Kind    Kind
	Code    string
	Message string
	Details []FieldDetail
	cause   error
}

func (e *AppError) Error() string {
	return e.Message
}

// Unwrap exposes the underlying cause (if any) for errors.Is/As chains and
// server-side logging — never for the response body.
func (e *AppError) Unwrap() error {
	return e.cause
}

// WithCause attaches an underlying error for server-side logging (e.g. the
// pgx error a NotFound was derived from) without leaking it into the
// client-facing Message. Returns the same *AppError for chaining.
func (e *AppError) WithCause(cause error) *AppError {
	e.cause = cause
	return e
}

func newAppError(kind Kind, code, message string) *AppError {
	return &AppError{Kind: kind, Code: code, Message: message}
}

// New constructs an AppError of the given Kind. Prefer the Kind-specific
// constructors below where one exists — they exist to make the call site
// self-documenting and to make an unsupported Kind (a typo) a compile error
// rather than a runtime surprise for the eight most common cases.
func New(kind Kind, code, message string) *AppError {
	return newAppError(kind, code, message)
}

func NewValidation(code, message string) *AppError {
	return newAppError(KindValidation, code, message)
}

// NewValidationWithDetails builds a KindValidation error carrying
// field-level detail, e.g. from decoding/validating a request body against
// multiple constraints at once.
func NewValidationWithDetails(code, message string, details []FieldDetail) *AppError {
	err := newAppError(KindValidation, code, message)
	err.Details = details
	return err
}

func NewUnauthenticated(code, message string) *AppError {
	return newAppError(KindUnauthenticated, code, message)
}

// NewUnauthorized is an alias for NewUnauthenticated, kept for backward
// compatibility with call sites written against the pre-taxonomy four-kind
// version of this package. "Unauthorized" here means "not authenticated"
// (HTTP 401) — use NewForbidden for "authenticated but not permitted" (403).
func NewUnauthorized(code, message string) *AppError {
	return newAppError(KindUnauthenticated, code, message)
}

func NewForbidden(code, message string) *AppError {
	return newAppError(KindForbidden, code, message)
}

func NewNotFound(code, message string) *AppError {
	return newAppError(KindNotFound, code, message)
}

func NewConflict(code, message string) *AppError {
	return newAppError(KindConflict, code, message)
}

func NewQuotaExceeded(code, message string) *AppError {
	return newAppError(KindQuotaExceeded, code, message)
}

func NewUpstreamUnavailable(code, message string) *AppError {
	return newAppError(KindUpstreamUnavailable, code, message)
}

func NewInternal(code, message string) *AppError {
	return newAppError(KindInternal, code, message)
}
