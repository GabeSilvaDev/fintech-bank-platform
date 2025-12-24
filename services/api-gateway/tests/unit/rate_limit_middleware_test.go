// ═══════════════════════════════════════════════════════════════════════════
// Unit Test: Rate Limit Middleware
// ═══════════════════════════════════════════════════════════════════════════

package unit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/http/middleware"
	"github.com/stretchr/testify/assert"
)

func TestRateLimitMiddlewareAllowsRequests(t *testing.T) {
	cfg := contracts.RateLimitConfig{
		Requests: 100,
		Window:   time.Minute,
	}

	handler := middleware.RateLimit(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRateLimitMiddlewareBlocksExcessiveRequests(t *testing.T) {
	cfg := contracts.RateLimitConfig{
		Requests: 2,
		Window:   time.Minute,
	}

	handler := middleware.RateLimit(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	testIP := "10.0.0.99:12345"

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = testIP
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = testIP
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Contains(t, rec.Body.String(), "RATE_LIMIT_EXCEEDED")
}

func TestRateLimitMiddlewareReturnsJsonResponse(t *testing.T) {
	cfg := contracts.RateLimitConfig{
		Requests: 1,
		Window:   time.Minute,
	}

	handler := middleware.RateLimit(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	testIP := "10.0.0.100:12345"

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = testIP
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = testIP
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), `"success":false`)
}
