// ═══════════════════════════════════════════════════════════════════════════
// Package response - Tests
// ═══════════════════════════════════════════════════════════════════════════

package response

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	pkgErrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func TestJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	data := map[string]string{"key": "value"}

	JSON(rec, http.StatusOK, data)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var result map[string]string
	json.Unmarshal(rec.Body.Bytes(), &result)
	assert.Equal(t, "value", result["key"])
}

func TestSuccess(t *testing.T) {
	rec := httptest.NewRecorder()
	data := map[string]string{"message": "hello"}

	Success(rec, http.StatusOK, data)

	var result Response
	json.Unmarshal(rec.Body.Bytes(), &result)

	assert.True(t, result.Success)
	assert.NotNil(t, result.Data)
	assert.Nil(t, result.Error)
}

func TestSuccessWithMeta(t *testing.T) {
	rec := httptest.NewRecorder()
	data := []string{"item1", "item2"}
	meta := &Meta{
		Page:       1,
		PerPage:    10,
		Total:      100,
		TotalPages: 10,
	}

	SuccessWithMeta(rec, http.StatusOK, data, meta)

	var result Response
	json.Unmarshal(rec.Body.Bytes(), &result)

	assert.True(t, result.Success)
	assert.NotNil(t, result.Meta)
	assert.Equal(t, 1, result.Meta.Page)
	assert.Equal(t, int64(100), result.Meta.Total)
}

func TestError(t *testing.T) {
	rec := httptest.NewRecorder()

	Error(rec, http.StatusBadRequest, "BAD_REQUEST", "Invalid input")

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var result Response
	json.Unmarshal(rec.Body.Bytes(), &result)

	assert.False(t, result.Success)
	assert.NotNil(t, result.Error)
	assert.Equal(t, "BAD_REQUEST", result.Error.Code)
	assert.Equal(t, "Invalid input", result.Error.Message)
}

func TestErrorWithDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	details := map[string]string{"field": "email", "reason": "invalid format"}

	ErrorWithDetails(rec, http.StatusBadRequest, "VALIDATION_ERROR", "Validation failed", details)

	var result Response
	json.Unmarshal(rec.Body.Bytes(), &result)

	assert.False(t, result.Success)
	assert.Equal(t, details, result.Error.Details)
}

func TestAppError(t *testing.T) {
	rec := httptest.NewRecorder()
	appErr := pkgErrors.BadRequest("INVALID_INPUT", "Invalid input data").
		WithDetail("field", "email")

	AppError(rec, appErr)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var result Response
	json.Unmarshal(rec.Body.Bytes(), &result)

	assert.False(t, result.Success)
	assert.Equal(t, "INVALID_INPUT", result.Error.Code)
}

func TestFromError_AppError(t *testing.T) {
	rec := httptest.NewRecorder()
	appErr := pkgErrors.NotFound("NOT_FOUND", "Resource not found")

	FromError(rec, appErr)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestFromError_StandardError(t *testing.T) {
	rec := httptest.NewRecorder()
	err := errors.New("some error")

	FromError(rec, err)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestOK(t *testing.T) {
	rec := httptest.NewRecorder()
	OK(rec, "success")
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestCreated(t *testing.T) {
	rec := httptest.NewRecorder()
	Created(rec, "created")
	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestAccepted(t *testing.T) {
	rec := httptest.NewRecorder()
	Accepted(rec, "accepted")
	assert.Equal(t, http.StatusAccepted, rec.Code)
}

func TestNoContent(t *testing.T) {
	rec := httptest.NewRecorder()
	NoContent(rec)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestBadRequest(t *testing.T) {
	rec := httptest.NewRecorder()
	BadRequest(rec, "BAD", "bad request")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUnauthorized(t *testing.T) {
	rec := httptest.NewRecorder()
	Unauthorized(rec, "UNAUTH", "unauthorized")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestForbidden(t *testing.T) {
	rec := httptest.NewRecorder()
	Forbidden(rec, "FORBIDDEN", "forbidden")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	NotFound(rec, "NOT_FOUND", "not found")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestConflict(t *testing.T) {
	rec := httptest.NewRecorder()
	Conflict(rec, "CONFLICT", "conflict")
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestUnprocessableEntity(t *testing.T) {
	rec := httptest.NewRecorder()
	UnprocessableEntity(rec, "UNPROCESSABLE", "unprocessable")
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestTooManyRequests(t *testing.T) {
	rec := httptest.NewRecorder()
	TooManyRequests(rec, "RATE_LIMIT", "too many requests")
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}

func TestInternalServerError(t *testing.T) {
	rec := httptest.NewRecorder()
	InternalServerError(rec, "INTERNAL", "internal error")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestServiceUnavailable(t *testing.T) {
	rec := httptest.NewRecorder()
	ServiceUnavailable(rec, "UNAVAILABLE", "service unavailable")
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
