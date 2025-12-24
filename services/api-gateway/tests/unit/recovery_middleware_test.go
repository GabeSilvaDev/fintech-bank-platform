// ═══════════════════════════════════════════════════════════════════════════
// Unit Test: Recovery Middleware
// ═══════════════════════════════════════════════════════════════════════════

package unit

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/http/middleware"
	"github.com/stretchr/testify/assert"
)

func TestRecoveryMiddlewareHandlesPanic(t *testing.T) {
	handler := middleware.Recovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		handler.ServeHTTP(rec, req)
	})

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestRecoveryMiddlewarePassesThroughNormally(t *testing.T) {
	handler := middleware.Recovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "OK", rec.Body.String())
}

func TestRecoveryMiddlewareHandlesNilPanic(t *testing.T) {
	handler := middleware.Recovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(nil)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		handler.ServeHTTP(rec, req)
	})
}

func TestRecoveryMiddlewareHandlesErrorPanic(t *testing.T) {
	handler := middleware.Recovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("error message")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
