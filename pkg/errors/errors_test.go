// ═══════════════════════════════════════════════════════════════════════════
// Package errors - Tests
// ═══════════════════════════════════════════════════════════════════════════

package errors

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	err := New("TEST_ERROR", "Test message", http.StatusBadRequest)

	assert.Equal(t, "TEST_ERROR", err.Code)
	assert.Equal(t, "Test message", err.Message)
	assert.Equal(t, http.StatusBadRequest, err.HTTPStatus)
}

func TestAppError_Error(t *testing.T) {
	err := New("TEST_ERROR", "Test message", http.StatusBadRequest)
	assert.Equal(t, "TEST_ERROR: Test message", err.Error())
}

func TestAppError_ErrorWithWrapped(t *testing.T) {
	underlying := errors.New("underlying error")
	err := New("TEST_ERROR", "Test message", http.StatusBadRequest).Wrap(underlying)

	assert.Contains(t, err.Error(), "TEST_ERROR")
	assert.Contains(t, err.Error(), "Test message")
	assert.Contains(t, err.Error(), "underlying error")
}

func TestAppError_Unwrap(t *testing.T) {
	underlying := errors.New("underlying error")
	err := New("TEST_ERROR", "Test message", http.StatusBadRequest).Wrap(underlying)

	assert.Equal(t, underlying, err.Unwrap())
}

func TestAppError_WithDetails(t *testing.T) {
	details := map[string]string{"field": "value"}
	err := New("TEST_ERROR", "Test message", http.StatusBadRequest).WithDetails(details)

	assert.Equal(t, details, err.Details)
}

func TestAppError_WithDetail(t *testing.T) {
	err := New("TEST_ERROR", "Test message", http.StatusBadRequest).
		WithDetail("field1", "value1").
		WithDetail("field2", "value2")

	assert.Equal(t, "value1", err.Details["field1"])
	assert.Equal(t, "value2", err.Details["field2"])
}

func TestBadRequest(t *testing.T) {
	err := BadRequest("BAD_REQUEST", "Bad request")
	assert.Equal(t, http.StatusBadRequest, err.HTTPStatus)
}

func TestUnauthorized(t *testing.T) {
	err := Unauthorized("UNAUTHORIZED", "Unauthorized")
	assert.Equal(t, http.StatusUnauthorized, err.HTTPStatus)
}

func TestForbidden(t *testing.T) {
	err := Forbidden("FORBIDDEN", "Forbidden")
	assert.Equal(t, http.StatusForbidden, err.HTTPStatus)
}

func TestNotFound(t *testing.T) {
	err := NotFound("NOT_FOUND", "Not found")
	assert.Equal(t, http.StatusNotFound, err.HTTPStatus)
}

func TestConflict(t *testing.T) {
	err := Conflict("CONFLICT", "Conflict")
	assert.Equal(t, http.StatusConflict, err.HTTPStatus)
}

func TestUnprocessableEntity(t *testing.T) {
	err := UnprocessableEntity("UNPROCESSABLE", "Unprocessable")
	assert.Equal(t, http.StatusUnprocessableEntity, err.HTTPStatus)
}

func TestTooManyRequests(t *testing.T) {
	err := TooManyRequests("TOO_MANY", "Too many requests")
	assert.Equal(t, http.StatusTooManyRequests, err.HTTPStatus)
}

func TestInternalServer(t *testing.T) {
	err := InternalServer("INTERNAL", "Internal error")
	assert.Equal(t, http.StatusInternalServerError, err.HTTPStatus)
}

func TestServiceUnavailable(t *testing.T) {
	err := ServiceUnavailable("UNAVAILABLE", "Service unavailable")
	assert.Equal(t, http.StatusServiceUnavailable, err.HTTPStatus)
}

func TestIsAppError(t *testing.T) {
	appErr := New("TEST", "test", http.StatusBadRequest)
	stdErr := errors.New("standard error")

	assert.True(t, IsAppError(appErr))
	assert.False(t, IsAppError(stdErr))
}

func TestAsAppError(t *testing.T) {
	appErr := New("TEST", "test", http.StatusBadRequest)
	stdErr := errors.New("standard error")

	result, ok := AsAppError(appErr)
	assert.True(t, ok)
	assert.Equal(t, appErr, result)

	_, ok = AsAppError(stdErr)
	assert.False(t, ok)
}

func TestGetHTTPStatus(t *testing.T) {
	appErr := New("TEST", "test", http.StatusBadRequest)
	stdErr := errors.New("standard error")

	assert.Equal(t, http.StatusBadRequest, GetHTTPStatus(appErr))
	assert.Equal(t, http.StatusInternalServerError, GetHTTPStatus(stdErr))
}

func TestCommonErrors(t *testing.T) {
	assert.Equal(t, http.StatusBadRequest, ErrValidation.HTTPStatus)
	assert.Equal(t, http.StatusUnauthorized, ErrUnauthorized.HTTPStatus)
	assert.Equal(t, http.StatusForbidden, ErrForbidden.HTTPStatus)
	assert.Equal(t, http.StatusNotFound, ErrNotFound.HTTPStatus)
	assert.Equal(t, http.StatusConflict, ErrConflict.HTTPStatus)
	assert.Equal(t, http.StatusTooManyRequests, ErrRateLimitExceeded.HTTPStatus)
	assert.Equal(t, http.StatusInternalServerError, ErrInternalServer.HTTPStatus)
	assert.Equal(t, http.StatusServiceUnavailable, ErrServiceUnavailable.HTTPStatus)
}
