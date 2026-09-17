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
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func testDependencies() appHttp.Dependencies {
	accountService, _ := url.Parse("http://127.0.0.1:1")
	transactionService, _ := url.Parse("http://127.0.0.1:1")
	return appHttp.Dependencies{
		Publisher:          &tests.FakePublisher{},
		Logger:             logger.New(logger.Config{Output: io.Discard}),
		AccountService:     accountService,
		TransactionService: transactionService,
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
