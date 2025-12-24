// ═══════════════════════════════════════════════════════════════════════════
// Package errors - Standardized error handling
// ═══════════════════════════════════════════════════════════════════════════

package errors

import (
	"fmt"
	"net/http"
)

// AppError represents a standardized application error
type AppError struct {
	Code       string            `json:"code"`
	Message    string            `json:"message"`
	Details    map[string]string `json:"details,omitempty"`
	HTTPStatus int               `json:"-"`
	Err        error             `json:"-"`
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying error
func (e *AppError) Unwrap() error {
	return e.Err
}

// WithDetails adds details to the error
func (e *AppError) WithDetails(details map[string]string) *AppError {
	e.Details = details
	return e
}

// WithDetail adds a single detail to the error
func (e *AppError) WithDetail(key, value string) *AppError {
	if e.Details == nil {
		e.Details = make(map[string]string)
	}
	e.Details[key] = value
	return e
}

// Wrap wraps an underlying error
func (e *AppError) Wrap(err error) *AppError {
	e.Err = err
	return e
}

// ═══════════════════════════════════════════════════════════════════════════
// Error Constructors
// ═══════════════════════════════════════════════════════════════════════════

// New creates a new AppError
func New(code, message string, httpStatus int) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: httpStatus,
	}
}

// BadRequest creates a 400 Bad Request error
func BadRequest(code, message string) *AppError {
	return New(code, message, http.StatusBadRequest)
}

// Unauthorized creates a 401 Unauthorized error
func Unauthorized(code, message string) *AppError {
	return New(code, message, http.StatusUnauthorized)
}

// Forbidden creates a 403 Forbidden error
func Forbidden(code, message string) *AppError {
	return New(code, message, http.StatusForbidden)
}

// NotFound creates a 404 Not Found error
func NotFound(code, message string) *AppError {
	return New(code, message, http.StatusNotFound)
}

// Conflict creates a 409 Conflict error
func Conflict(code, message string) *AppError {
	return New(code, message, http.StatusConflict)
}

// UnprocessableEntity creates a 422 Unprocessable Entity error
func UnprocessableEntity(code, message string) *AppError {
	return New(code, message, http.StatusUnprocessableEntity)
}

// TooManyRequests creates a 429 Too Many Requests error
func TooManyRequests(code, message string) *AppError {
	return New(code, message, http.StatusTooManyRequests)
}

// InternalServer creates a 500 Internal Server Error
func InternalServer(code, message string) *AppError {
	return New(code, message, http.StatusInternalServerError)
}

// ServiceUnavailable creates a 503 Service Unavailable error
func ServiceUnavailable(code, message string) *AppError {
	return New(code, message, http.StatusServiceUnavailable)
}

// ═══════════════════════════════════════════════════════════════════════════
// Common Errors
// ═══════════════════════════════════════════════════════════════════════════

var (
	ErrValidation         = BadRequest("VALIDATION_ERROR", "Validation failed")
	ErrInvalidJSON        = BadRequest("INVALID_JSON", "Invalid JSON payload")
	ErrMissingField       = BadRequest("MISSING_FIELD", "Required field is missing")
	ErrInvalidField       = BadRequest("INVALID_FIELD", "Field value is invalid")
	ErrUnauthorized       = Unauthorized("UNAUTHORIZED", "Authentication required")
	ErrInvalidToken       = Unauthorized("INVALID_TOKEN", "Invalid authentication token")
	ErrExpiredToken       = Unauthorized("EXPIRED_TOKEN", "Authentication token has expired")
	ErrForbidden          = Forbidden("FORBIDDEN", "Access denied")
	ErrNotFound           = NotFound("NOT_FOUND", "Resource not found")
	ErrAccountNotFound    = NotFound("ACCOUNT_NOT_FOUND", "Account not found")
	ErrUserNotFound       = NotFound("USER_NOT_FOUND", "User not found")
	ErrConflict           = Conflict("CONFLICT", "Resource already exists")
	ErrDuplicateEmail     = Conflict("DUPLICATE_EMAIL", "Email already registered")
	ErrDuplicateAccount   = Conflict("DUPLICATE_ACCOUNT", "Account already exists")
	ErrRateLimitExceeded  = TooManyRequests("RATE_LIMIT_EXCEEDED", "Too many requests")
	ErrInternalServer     = InternalServer("INTERNAL_ERROR", "Internal server error")
	ErrDatabaseError      = InternalServer("DATABASE_ERROR", "Database operation failed")
	ErrServiceUnavailable = ServiceUnavailable("SERVICE_UNAVAILABLE", "Service temporarily unavailable")
)

// ═══════════════════════════════════════════════════════════════════════════
// Error Helpers
// ═══════════════════════════════════════════════════════════════════════════

// IsAppError checks if an error is an AppError
func IsAppError(err error) bool {
	_, ok := err.(*AppError)
	return ok
}

// AsAppError converts an error to AppError
func AsAppError(err error) (*AppError, bool) {
	appErr, ok := err.(*AppError)
	return appErr, ok
}

// GetHTTPStatus returns the HTTP status code from an error
func GetHTTPStatus(err error) int {
	if appErr, ok := AsAppError(err); ok {
		return appErr.HTTPStatus
	}
	return http.StatusInternalServerError
}
