package unit

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/fintech-bank-platform/transaction-service/internal/app/handlers"
	"github.com/fintech-bank-platform/transaction-service/internal/app/services"
	appHttp "github.com/fintech-bank-platform/transaction-service/internal/infrastructure/http"
	"github.com/fintech-bank-platform/transaction-service/tests"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func testDependencies() appHttp.Dependencies {
	service := services.NewTransactionService(tests.NewFakeTransactionRepo(), tests.FakeClock{T: time.Now().UTC()}, uuid.New)
	return appHttp.Dependencies{
		Reads:  handlers.NewReadHandler(service),
		Ping:   func(context.Context) error { return nil },
		Logger: logger.New(logger.Config{Output: io.Discard}),
	}
}

func TestSetupRouterHealthEndpoint(t *testing.T) {
	router := chi.NewRouter()

	appHttp.SetupRouter(router, testDependencies())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "healthy")
}

func TestSetupRouterServesMetricsWhenEnabled(t *testing.T) {
	router := chi.NewRouter()
	deps := testDependencies()
	deps.Metrics = metrics.New("test-transaction-router-metrics-enabled")

	appHttp.SetupRouter(router, deps)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "go_goroutines")
}

func TestSetupRouterHidesMetricsWhenDisabled(t *testing.T) {
	router := chi.NewRouter()

	appHttp.SetupRouter(router, testDependencies())

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
