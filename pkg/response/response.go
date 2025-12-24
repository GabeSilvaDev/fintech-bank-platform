// ═══════════════════════════════════════════════════════════════════════════
// Package response - HTTP response helpers
// ═══════════════════════════════════════════════════════════════════════════

package response

import (
	"encoding/json"
	"net/http"

	"github.com/fintech-bank-platform/pkg/errors"
)

// Response represents a standardized API response
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *ErrorInfo  `json:"error,omitempty"`
	Meta    *Meta       `json:"meta,omitempty"`
}

// ErrorInfo represents error information in a response
type ErrorInfo struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

// Meta represents metadata in a response
type Meta struct {
	RequestID  string `json:"request_id,omitempty"`
	Page       int    `json:"page,omitempty"`
	PerPage    int    `json:"per_page,omitempty"`
	Total      int64  `json:"total,omitempty"`
	TotalPages int    `json:"total_pages,omitempty"`
}

// ═══════════════════════════════════════════════════════════════════════════
// Response Writers
// ═══════════════════════════════════════════════════════════════════════════

// JSON writes a JSON response
func JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// Success writes a successful response with data
func Success(w http.ResponseWriter, status int, data interface{}) {
	response := Response{
		Success: true,
		Data:    data,
	}
	JSON(w, status, response)
}

// SuccessWithMeta writes a successful response with data and metadata
func SuccessWithMeta(w http.ResponseWriter, status int, data interface{}, meta *Meta) {
	response := Response{
		Success: true,
		Data:    data,
		Meta:    meta,
	}
	JSON(w, status, response)
}

// Error writes an error response
func Error(w http.ResponseWriter, status int, code, message string) {
	response := Response{
		Success: false,
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
		},
	}
	JSON(w, status, response)
}

// ErrorWithDetails writes an error response with details
func ErrorWithDetails(w http.ResponseWriter, status int, code, message string, details map[string]string) {
	response := Response{
		Success: false,
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
			Details: details,
		},
	}
	JSON(w, status, response)
}

// AppError writes an AppError as response
func AppError(w http.ResponseWriter, err *errors.AppError) {
	response := Response{
		Success: false,
		Error: &ErrorInfo{
			Code:    err.Code,
			Message: err.Message,
			Details: err.Details,
		},
	}
	JSON(w, err.HTTPStatus, response)
}

// FromError writes any error as response
func FromError(w http.ResponseWriter, err error) {
	if appErr, ok := errors.AsAppError(err); ok {
		AppError(w, appErr)
		return
	}
	Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
}

// ═══════════════════════════════════════════════════════════════════════════
// Common Responses
// ═══════════════════════════════════════════════════════════════════════════

// OK writes a 200 OK response
func OK(w http.ResponseWriter, data interface{}) {
	Success(w, http.StatusOK, data)
}

// Created writes a 201 Created response
func Created(w http.ResponseWriter, data interface{}) {
	Success(w, http.StatusCreated, data)
}

// Accepted writes a 202 Accepted response
func Accepted(w http.ResponseWriter, data interface{}) {
	Success(w, http.StatusAccepted, data)
}

// NoContent writes a 204 No Content response
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// BadRequest writes a 400 Bad Request response
func BadRequest(w http.ResponseWriter, code, message string) {
	Error(w, http.StatusBadRequest, code, message)
}

// Unauthorized writes a 401 Unauthorized response
func Unauthorized(w http.ResponseWriter, code, message string) {
	Error(w, http.StatusUnauthorized, code, message)
}

// Forbidden writes a 403 Forbidden response
func Forbidden(w http.ResponseWriter, code, message string) {
	Error(w, http.StatusForbidden, code, message)
}

// NotFound writes a 404 Not Found response
func NotFound(w http.ResponseWriter, code, message string) {
	Error(w, http.StatusNotFound, code, message)
}

// Conflict writes a 409 Conflict response
func Conflict(w http.ResponseWriter, code, message string) {
	Error(w, http.StatusConflict, code, message)
}

// UnprocessableEntity writes a 422 Unprocessable Entity response
func UnprocessableEntity(w http.ResponseWriter, code, message string) {
	Error(w, http.StatusUnprocessableEntity, code, message)
}

// TooManyRequests writes a 429 Too Many Requests response
func TooManyRequests(w http.ResponseWriter, code, message string) {
	Error(w, http.StatusTooManyRequests, code, message)
}

// InternalServerError writes a 500 Internal Server Error response
func InternalServerError(w http.ResponseWriter, code, message string) {
	Error(w, http.StatusInternalServerError, code, message)
}

// ServiceUnavailable writes a 503 Service Unavailable response
func ServiceUnavailable(w http.ResponseWriter, code, message string) {
	Error(w, http.StatusServiceUnavailable, code, message)
}
