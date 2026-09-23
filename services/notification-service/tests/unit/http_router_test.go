package unit

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fintech-bank-platform/notification-service/internal/app/handlers"
	appHttp "github.com/fintech-bank-platform/notification-service/internal/infrastructure/http"
	"github.com/fintech-bank-platform/notification-service/tests"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func httpTestDependencies() appHttp.Dependencies {
	return appHttp.Dependencies{
		History: handlers.NewHistoryHandler(&tests.FakeHistory{}),
		Ping:    func(context.Context) error { return nil },
		Logger:  logger.New(logger.Config{Output: io.Discard}),
	}
}

func TestSetupRouterHealthEndpoint(t *testing.T) {
	router := chi.NewRouter()

	appHttp.SetupRouter(router, httpTestDependencies())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "healthy")
}

func TestSetupRouterServesMetricsWhenEnabled(t *testing.T) {
	router := chi.NewRouter()
	deps := httpTestDependencies()
	deps.Metrics = metrics.New("test-notification-router-metrics-enabled")

	appHttp.SetupRouter(router, deps)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "go_goroutines")
}

func TestSetupRouterHidesMetricsWhenDisabled(t *testing.T) {
	router := chi.NewRouter()

	appHttp.SetupRouter(router, httpTestDependencies())

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
