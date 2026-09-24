package unit

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/handlers"
	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/fintech-bank-platform/account-service/internal/contracts"
	appHttp "github.com/fintech-bank-platform/account-service/internal/infrastructure/http"
	"github.com/fintech-bank-platform/account-service/tests"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func testDependencies() appHttp.Dependencies {
	service := services.NewAccountService(
		tests.NewFakeAccountRepo(),
		tests.NewFakeCustomerRepo(),
		tests.NewFakeOperationRepo(),
		tests.FakeClock{T: time.Now().UTC()},
		func() string { return "00000001" },
	)
	identities, err := services.NewIdentityService(tests.NewFakeIdentityRepo(), &tests.FakeHasher{}, tests.FakeClock{T: time.Now().UTC()})
	if err != nil {
		panic(err)
	}
	return appHttp.Dependencies{
		Reads:      handlers.NewReadHandler(service),
		Identities: handlers.NewIdentityHandler(identities),
		Sessions:   handlers.NewSessionHandler(services.NewSessionService(tests.NewFakeRefreshTokenRepo(), tests.FakeClock{T: time.Now().UTC()}, contracts.SessionConfig{RefreshTokenTTL: time.Hour})),
		Ping:       func(context.Context) error { return nil },
		Logger:     logger.New(logger.Config{Output: io.Discard}),
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
	deps.Metrics = metrics.New("test-account-router-metrics-enabled")

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
