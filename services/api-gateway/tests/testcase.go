package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/config"
	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	appHttp "github.com/fintech-bank-platform/api-gateway/internal/infrastructure/http"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/suite"
)

// ═══════════════════════════════════════════════════════════════════════════
// TestCase - Base struct for all feature tests
// ═══════════════════════════════════════════════════════════════════════════

type TestCase struct {
	suite.Suite
	Router  *chi.Mux
	Config  *config.Config
	Logger  zerolog.Logger
	headers map[string]string
}

// ═══════════════════════════════════════════════════════════════════════════
// Setup & Teardown (setUp/tearDown)
// ═══════════════════════════════════════════════════════════════════════════

func (tc *TestCase) SetupSuite() {
	tc.Config = testConfig()
	tc.Logger = zerolog.Nop()
	tc.headers = make(map[string]string)

	tc.Router = chi.NewRouter()
	appHttp.SetupRouter(tc.Router, tc.Config)
}

func (tc *TestCase) SetupTest() {
	tc.headers = make(map[string]string)
}

func (tc *TestCase) TearDownTest() {
	//
}

func (tc *TestCase) TearDownSuite() {
	//
}

// ═══════════════════════════════════════════════════════════════════════════
// HTTP Request Methods
// ═══════════════════════════════════════════════════════════════════════════

func (tc *TestCase) Get(uri string) *TestResponse {
	return tc.request(http.MethodGet, uri, nil)
}

func (tc *TestCase) Post(uri string, data interface{}) *TestResponse {
	return tc.requestWithBody(http.MethodPost, uri, data)
}

func (tc *TestCase) Put(uri string, data interface{}) *TestResponse {
	return tc.requestWithBody(http.MethodPut, uri, data)
}

func (tc *TestCase) Patch(uri string, data interface{}) *TestResponse {
	return tc.requestWithBody(http.MethodPatch, uri, data)
}

func (tc *TestCase) Delete(uri string) *TestResponse {
	return tc.request(http.MethodDelete, uri, nil)
}

func (tc *TestCase) DeleteWithBody(uri string, data interface{}) *TestResponse {
	return tc.requestWithBody(http.MethodDelete, uri, data)
}

func (tc *TestCase) Options(uri string) *TestResponse {
	return tc.request(http.MethodOptions, uri, nil)
}

func (tc *TestCase) Head(uri string) *TestResponse {
	return tc.request(http.MethodHead, uri, nil)
}

// ═══════════════════════════════════════════════════════════════════════════
// Header Methods (withHeaders())
// ═══════════════════════════════════════════════════════════════════════════

func (tc *TestCase) WithHeader(key, value string) *TestCase {
	tc.headers[key] = value
	return tc
}

func (tc *TestCase) WithHeaders(headers map[string]string) *TestCase {
	for key, value := range headers {
		tc.headers[key] = value
	}
	return tc
}

func (tc *TestCase) WithToken(token string) *TestCase {
	return tc.WithHeader("Authorization", "Bearer "+token)
}

func (tc *TestCase) WithContentType(contentType string) *TestCase {
	return tc.WithHeader("Content-Type", contentType)
}

// ═══════════════════════════════════════════════════════════════════════════
// Internal Request Helpers
// ═══════════════════════════════════════════════════════════════════════════

func (tc *TestCase) request(method, uri string, body io.Reader) *TestResponse {
	req := httptest.NewRequest(method, uri, body)
	tc.applyHeaders(req)

	rec := httptest.NewRecorder()
	tc.Router.ServeHTTP(rec, req)

	return newTestResponse(tc.T(), rec)
}

func (tc *TestCase) requestWithBody(method, uri string, data interface{}) *TestResponse {
	var body io.Reader

	if data != nil {
		jsonData, err := json.Marshal(data)
		tc.Require().NoError(err, "Failed to marshal request body")
		body = bytes.NewBuffer(jsonData)
	}

	req := httptest.NewRequest(method, uri, body)
	req.Header.Set("Content-Type", "application/json")
	tc.applyHeaders(req)

	rec := httptest.NewRecorder()
	tc.Router.ServeHTTP(rec, req)

	return newTestResponse(tc.T(), rec)
}

func (tc *TestCase) applyHeaders(req *http.Request) {
	for key, value := range tc.headers {
		req.Header.Set(key, value)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Test Configuration
// ═══════════════════════════════════════════════════════════════════════════

func testConfig() *config.Config {
	return &config.Config{
		Server:    testServerConfig(),
		CORS:      testCORSConfig(),
		RateLimit: testRateLimitConfig(),
	}
}

func testServerConfig() contracts.ServerConfig {
	return contracts.ServerConfig{
		Host:            "127.0.0.1",
		Port:            "8080",
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    30 * time.Second,
		IdleTimeout:     120 * time.Second,
		ShutdownTimeout: 10 * time.Second,
	}
}

func testCORSConfig() contracts.CORSConfig {
	return contracts.CORSConfig{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}
}

func testRateLimitConfig() contracts.RateLimitConfig {
	return contracts.RateLimitConfig{
		Requests: 1000,
		Window:   1 * time.Minute,
	}
}
