package unit

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/handlers"
	"github.com/fintech-bank-platform/payment-service/internal/app/services"
	appHttp "github.com/fintech-bank-platform/payment-service/internal/infrastructure/http"
	"github.com/fintech-bank-platform/payment-service/tests"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func testDependencies() appHttp.Dependencies {
	service := services.NewPaymentService(tests.NewFakePaymentRepo(), &tests.FakeGateway{}, tests.FakeClock{T: time.Now().UTC()}, func() uuid.UUID { return uuid.New() })
	log := logger.New(logger.Config{Output: io.Discard})
	return appHttp.Dependencies{
		Reads:    handlers.NewReadHandler(service),
		Webhooks: handlers.NewWebhookHandler(&tests.FakePublisher{}, "s3cret-s3cret-s3cret", 5*time.Minute, tests.FakeClock{T: time.Now().UTC()}, log),
		Ping:     func(context.Context) error { return nil },
		Logger:   log,
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
	deps.Metrics = metrics.New("test-payment-router-metrics-enabled")

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
