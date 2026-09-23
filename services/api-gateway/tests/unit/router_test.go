package unit

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/config"
	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	appHttp "github.com/fintech-bank-platform/api-gateway/internal/infrastructure/http"
	"github.com/fintech-bank-platform/api-gateway/tests"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func testDependencies() appHttp.Dependencies {
	accountService, _ := url.Parse("http://127.0.0.1:1")
	transactionService, _ := url.Parse("http://127.0.0.1:1")
	paymentService, _ := url.Parse("http://127.0.0.1:1")
	notificationService, _ := url.Parse("http://127.0.0.1:1")
	return appHttp.Dependencies{
		Publisher:           &tests.FakePublisher{},
		Logger:              logger.New(logger.Config{Output: io.Discard}),
		AccountService:      accountService,
		TransactionService:  transactionService,
		PaymentService:      paymentService,
		NotificationService: notificationService,
	}
}

func TestSetupRouter(t *testing.T) {
	router := chi.NewRouter()
	cfg := &config.Config{
		Server: contracts.ServerConfig{
			Host: "localhost",
			Port: "8080",
		},
		CORS: contracts.CORSConfig{
			AllowedOrigins:   []string{"*"},
			AllowedMethods:   []string{"GET", "POST"},
			AllowedHeaders:   []string{"Content-Type"},
			ExposedHeaders:   []string{"Link"},
			AllowCredentials: true,
			MaxAge:           300,
		},
		RateLimit: contracts.RateLimitConfig{
			Requests: 100,
			Window:   time.Minute,
		},
	}

	appHttp.SetupRouter(router, cfg, testDependencies())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestSetupRouterHealthEndpoint(t *testing.T) {
	router := chi.NewRouter()
	cfg := &config.Config{
		CORS: contracts.CORSConfig{
			AllowedOrigins:   []string{"*"},
			AllowedMethods:   []string{"GET"},
			AllowedHeaders:   []string{"Content-Type"},
			AllowCredentials: false,
			MaxAge:           0,
		},
		RateLimit: contracts.RateLimitConfig{
			Requests: 1000,
			Window:   time.Minute,
		},
	}

	appHttp.SetupRouter(router, cfg, testDependencies())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "healthy")
}

func TestSetupRouterMountsCommandRoutes(t *testing.T) {
	router := chi.NewRouter()
	cfg := &config.Config{
		CORS:      contracts.CORSConfig{AllowedOrigins: []string{"*"}, AllowedMethods: []string{"POST"}},
		RateLimit: contracts.RateLimitConfig{Requests: 1000, Window: time.Minute},
	}

	appHttp.SetupRouter(router, cfg, testDependencies())

	for _, path := range []string{"/api/v1/accounts", "/api/v1/transactions", "/api/v1/transfers", "/api/v1/payments"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, path)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/x", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestSetupRouterServesMetricsWhenEnabled(t *testing.T) {
	router := chi.NewRouter()
	cfg := &config.Config{
		CORS:      contracts.CORSConfig{AllowedOrigins: []string{"*"}, AllowedMethods: []string{"GET"}},
		RateLimit: contracts.RateLimitConfig{Requests: 1000, Window: time.Minute},
	}
	deps := testDependencies()
	deps.Metrics = metrics.New("test-router-metrics-enabled")

	appHttp.SetupRouter(router, cfg, deps)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "go_goroutines")
}

func TestSetupRouterHidesMetricsWhenDisabled(t *testing.T) {
	router := chi.NewRouter()
	cfg := &config.Config{
		CORS:      contracts.CORSConfig{AllowedOrigins: []string{"*"}, AllowedMethods: []string{"GET"}},
		RateLimit: contracts.RateLimitConfig{Requests: 1000, Window: time.Minute},
	}

	appHttp.SetupRouter(router, cfg, testDependencies())

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSetupRouterRecordsRoutePatternForProxiedReads(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
	}))
	defer upstream.Close()

	router := chi.NewRouter()
	cfg := &config.Config{
		CORS:      contracts.CORSConfig{AllowedOrigins: []string{"*"}, AllowedMethods: []string{"GET"}},
		RateLimit: contracts.RateLimitConfig{Requests: 1000, Window: time.Minute},
	}
	deps := testDependencies()
	m := metrics.New("test-router-route-label")
	deps.Metrics = m
	accountService, _ := url.Parse(upstream.URL)
	deps.AccountService = accountService

	appHttp.SetupRouter(router, cfg, deps)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/abc", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	counter := m.CounterVec("http_requests_total", "Total number of HTTP requests", "method", "route", "status")
	assert.Equal(t, float64(1), testutil.ToFloat64(counter.WithLabelValues("GET", "/api/v1/accounts/{id}", "200")))
}
