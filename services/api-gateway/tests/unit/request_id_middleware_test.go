// ═══════════════════════════════════════════════════════════════════════════
// Unit Test: Request ID Middleware
// ═══════════════════════════════════════════════════════════════════════════

package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/http/middleware"
	"github.com/stretchr/testify/assert"
)

func TestGetRequestIDWithValue(t *testing.T) {
	ctx := context.WithValue(context.Background(), contracts.RequestIDKey, "test-request-id")

	result := middleware.GetRequestID(ctx)

	assert.Equal(t, "test-request-id", result)
}

func TestGetRequestIDWithoutValue(t *testing.T) {
	ctx := context.Background()

	result := middleware.GetRequestID(ctx)

	assert.Empty(t, result)
}

func TestGetRequestIDWithWrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), contracts.RequestIDKey, 12345)

	result := middleware.GetRequestID(ctx)

	assert.Empty(t, result)
}

func TestRequestIDMiddlewareGeneratesID(t *testing.T) {
	handler := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	requestID := rec.Header().Get(contracts.RequestIDHeader)
	assert.NotEmpty(t, requestID)
	assert.Regexp(t, `^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`, requestID)
}

func TestRequestIDMiddlewarePreservesProvidedID(t *testing.T) {
	handler := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(contracts.RequestIDHeader, "custom-id-12345")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "custom-id-12345", rec.Header().Get(contracts.RequestIDHeader))
}

func TestRequestIDMiddlewareSetsContext(t *testing.T) {
	var capturedRequestID string

	handler := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequestID = middleware.GetRequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(contracts.RequestIDHeader, "context-test-id")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "context-test-id", capturedRequestID)
}
