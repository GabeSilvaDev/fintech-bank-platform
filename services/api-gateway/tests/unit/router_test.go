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
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
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

func TestSetupRouterRateLimitSharesBucketAcrossForwardedForWhenUntrusted(t *testing.T) {
	router := chi.NewRouter()
	cfg := &config.Config{
		CORS:      contracts.CORSConfig{AllowedOrigins: []string{"*"}, AllowedMethods: []string{"GET"}},
		RateLimit: contracts.RateLimitConfig{Requests: 1, Window: time.Minute},
	}

	appHttp.SetupRouter(router, cfg, testDependencies())

	req1 := httptest.NewRequest(http.MethodGet, "/health", nil)
	req1.RemoteAddr = "203.0.113.5:1111"
	req1.Header.Set("X-Forwarded-For", "9.9.9.1")
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	assert.Equal(t, http.StatusOK, rec1.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/health", nil)
	req2.RemoteAddr = "203.0.113.5:2222"
	req2.Header.Set("X-Forwarded-For", "9.9.9.2")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusTooManyRequests, rec2.Code)
}

func TestSetupRouterRateLimitSeparatesBucketsByForwardedForWhenTrusted(t *testing.T) {
	router := chi.NewRouter()
	cfg := &config.Config{
		Server:    contracts.ServerConfig{TrustProxyHeaders: true},
		CORS:      contracts.CORSConfig{AllowedOrigins: []string{"*"}, AllowedMethods: []string{"GET"}},
		RateLimit: contracts.RateLimitConfig{Requests: 1, Window: time.Minute},
	}

	appHttp.SetupRouter(router, cfg, testDependencies())

	req1 := httptest.NewRequest(http.MethodGet, "/health", nil)
	req1.RemoteAddr = "203.0.113.5:1111"
	req1.Header.Set("X-Forwarded-For", "9.9.9.1")
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	assert.Equal(t, http.StatusOK, rec1.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/health", nil)
	req2.RemoteAddr = "203.0.113.5:2222"
	req2.Header.Set("X-Forwarded-For", "9.9.9.2")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusOK, rec2.Code)
}

func TestSetupRouterRateLimitTrustedKeysOnRightmostForwardedForEntry(t *testing.T) {
	router := chi.NewRouter()
	cfg := &config.Config{
		Server:    contracts.ServerConfig{TrustProxyHeaders: true, TrustedProxyHops: 1},
		CORS:      contracts.CORSConfig{AllowedOrigins: []string{"*"}, AllowedMethods: []string{"GET"}},
		RateLimit: contracts.RateLimitConfig{Requests: 1, Window: time.Minute},
	}

	appHttp.SetupRouter(router, cfg, testDependencies())

	req1 := httptest.NewRequest(http.MethodGet, "/health", nil)
	req1.RemoteAddr = "203.0.113.9:1111"
	req1.Header.Set("X-Forwarded-For", "10.0.0.1, 198.51.100.7")
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	assert.Equal(t, http.StatusOK, rec1.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/health", nil)
	req2.RemoteAddr = "203.0.113.9:2222"
	req2.Header.Set("X-Forwarded-For", "10.0.0.2, 198.51.100.7")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusTooManyRequests, rec2.Code)
}

func TestSetupRouterRateLimitIgnoresTrueClientIPAndXRealIP(t *testing.T) {
	router := chi.NewRouter()
	cfg := &config.Config{
		Server:    contracts.ServerConfig{TrustProxyHeaders: true, TrustedProxyHops: 1},
		CORS:      contracts.CORSConfig{AllowedOrigins: []string{"*"}, AllowedMethods: []string{"GET"}},
		RateLimit: contracts.RateLimitConfig{Requests: 1, Window: time.Minute},
	}

	appHttp.SetupRouter(router, cfg, testDependencies())

	req1 := httptest.NewRequest(http.MethodGet, "/health", nil)
	req1.RemoteAddr = "203.0.113.10:1111"
	req1.Header.Set("True-Client-IP", "9.9.9.9")
	req1.Header.Set("X-Real-IP", "9.9.9.9")
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	assert.Equal(t, http.StatusOK, rec1.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/health", nil)
	req2.RemoteAddr = "203.0.113.10:2222"
	req2.Header.Set("True-Client-IP", "8.8.8.8")
	req2.Header.Set("X-Real-IP", "8.8.8.8")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	assert.Equal(t, http.StatusTooManyRequests, rec2.Code)
}

func rateLimitedPair(t *testing.T, server contracts.ServerConfig, first, second func(*http.Request)) (int, int) {
	t.Helper()
	router := chi.NewRouter()
	cfg := &config.Config{
		Server:    server,
		CORS:      contracts.CORSConfig{AllowedOrigins: []string{"*"}, AllowedMethods: []string{"GET"}},
		RateLimit: contracts.RateLimitConfig{Requests: 1, Window: time.Minute},
	}
	appHttp.SetupRouter(router, cfg, testDependencies())

	codes := make([]int, 0, 2)
	for _, prepare := range []func(*http.Request){first, second} {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		prepare(req)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		codes = append(codes, rec.Code)
	}
	return codes[0], codes[1]
}

func fromRemoteAddr(addr string) func(*http.Request) {
	return func(r *http.Request) { r.RemoteAddr = addr }
}

func fromForwardedFor(addr string) func(*http.Request) {
	return func(r *http.Request) {
		r.RemoteAddr = "203.0.113.20:1111"
		r.Header.Set("X-Forwarded-For", addr)
	}
}

func TestSetupRouterRateLimitSharesBucketAcrossIPv6NetworkWhenUntrusted(t *testing.T) {
	first, second := rateLimitedPair(t, contracts.ServerConfig{},
		fromRemoteAddr("[2001:db8:1:2::1]:1234"),
		fromRemoteAddr("[2001:db8:1:2:ffff::9]:1234"))

	assert.Equal(t, http.StatusOK, first)
	assert.Equal(t, http.StatusTooManyRequests, second)
}

func TestSetupRouterRateLimitSeparatesIPv6NetworksWhenUntrusted(t *testing.T) {
	first, second := rateLimitedPair(t, contracts.ServerConfig{},
		fromRemoteAddr("[2001:db8:1:2::1]:1234"),
		fromRemoteAddr("[2001:db8:1:3::1]:1234"))

	assert.Equal(t, http.StatusOK, first)
	assert.Equal(t, http.StatusOK, second)
}

func TestSetupRouterRateLimitSharesBucketAcrossIPv6NetworkWhenTrusted(t *testing.T) {
	trusted := contracts.ServerConfig{TrustProxyHeaders: true, TrustedProxyHops: 1}
	first, second := rateLimitedPair(t, trusted,
		fromForwardedFor("2001:db8:5:6::1"),
		fromForwardedFor("2001:db8:5:6:abcd::2"))

	assert.Equal(t, http.StatusOK, first)
	assert.Equal(t, http.StatusTooManyRequests, second)
}

func TestSetupRouterRateLimitSeparatesIPv6NetworksWhenTrusted(t *testing.T) {
	trusted := contracts.ServerConfig{TrustProxyHeaders: true, TrustedProxyHops: 1}
	first, second := rateLimitedPair(t, trusted,
		fromForwardedFor("2001:db8:5:6::1"),
		fromForwardedFor("2001:db8:5:7::1"))

	assert.Equal(t, http.StatusOK, first)
	assert.Equal(t, http.StatusOK, second)
}

func TestSetupRouterRateLimitKeepsIPv4AddressesApart(t *testing.T) {
	first, second := rateLimitedPair(t, contracts.ServerConfig{},
		fromRemoteAddr("198.51.100.1:1234"),
		fromRemoteAddr("198.51.100.2:1234"))

	assert.Equal(t, http.StatusOK, first)
	assert.Equal(t, http.StatusOK, second)
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
		Auth:      tests.AuthConfig(),
	}

	appHttp.SetupRouter(router, cfg, testDependencies())

	for _, path := range []string{"/api/v1/accounts", "/api/v1/transactions", "/api/v1/transfers", "/api/v1/payments"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		req.Header.Set("Authorization", tests.BearerToken(uuid.New()))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, path)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+tests.UUID(), nil)
	req.Header.Set("Authorization", tests.BearerToken(uuid.New()))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/accounts/x", nil)
	req.Header.Set("Authorization", tests.BearerToken(uuid.New()))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
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
	cfg.Auth = tests.AuthConfig()
	deps := testDependencies()
	m := metrics.New("test-router-route-label")
	deps.Metrics = m
	accountService, _ := url.Parse(upstream.URL)
	deps.AccountService = accountService
	userID := uuid.New()
	deps.Owners = tests.OwnedBy(userID)

	appHttp.SetupRouter(router, cfg, deps)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+tests.UUID(), nil)
	req.Header.Set("Authorization", tests.BearerToken(userID))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	counter := m.CounterVec("http_requests_total", "Total number of HTTP requests", "method", "route", "status")
	assert.Equal(t, float64(1), testutil.ToFloat64(counter.WithLabelValues("GET", "/api/v1/accounts/{id}", "200")))
}

func TestSetupRouterStartsNewTraceForClientTraceContext(t *testing.T) {
	recorder := useTraceRecorder(t)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	var upstreamHeader http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHeader = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
	}))
	defer upstream.Close()

	router := chi.NewRouter()
	cfg := &config.Config{
		CORS:      contracts.CORSConfig{AllowedOrigins: []string{"*"}, AllowedMethods: []string{"GET"}},
		RateLimit: contracts.RateLimitConfig{Requests: 1000, Window: time.Minute},
	}
	cfg.Auth = tests.AuthConfig()
	deps := testDependencies()
	accountService, _ := url.Parse(upstream.URL)
	deps.AccountService = accountService
	userID := uuid.New()
	deps.Owners = tests.OwnedBy(userID)

	appHttp.SetupRouter(router, cfg, deps)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/"+tests.UUID(), nil)
	req.Header.Set("Authorization", tests.BearerToken(userID))
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	req.Header.Set("tracestate", "vendor=value")
	req.Header.Set("baggage", "user_id=attacker")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var server sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if span.SpanKind() == trace.SpanKindServer {
			server = span
		}
	}
	require.NotNil(t, server)
	assert.False(t, server.Parent().IsValid())
	assert.NotEqual(t, "4bf92f3577b34da6a3ce929d0e0e4736", server.SpanContext().TraceID().String())
	require.Len(t, server.Links(), 1)
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", server.Links()[0].SpanContext.TraceID().String())

	assert.Contains(t, upstreamHeader.Get("traceparent"), server.SpanContext().TraceID().String())
	assert.Empty(t, upstreamHeader.Get("tracestate"))
	assert.Empty(t, upstreamHeader.Get("baggage"))
}

type accessRule string

const (
	ruleSelf          accessRule = "self"
	ruleAccountPath   accessRule = "account in path"
	ruleGuardedRead   accessRule = "guarded read"
	ruleAccountBody   accessRule = "account in body"
	ruleCreateForSelf accessRule = "create for self"
)

var protectedRoutes = map[string]accessRule{
	"POST /api/v1/accounts":                          ruleCreateForSelf,
	"PATCH /api/v1/accounts/{id}":                    ruleAccountPath,
	"DELETE /api/v1/accounts/{id}":                   ruleAccountPath,
	"GET /api/v1/accounts/{id}":                      ruleAccountPath,
	"GET /api/v1/users/{user_id}/accounts":           ruleSelf,
	"GET /api/v1/accounts/{account_id}/transactions": ruleAccountPath,
	"POST /api/v1/transactions":                      ruleAccountBody,
	"POST /api/v1/transfers":                         ruleAccountBody,
	"GET /api/v1/transactions/{id}":                  ruleGuardedRead,
	"GET /api/v1/accounts/{account_id}/payments":     ruleAccountPath,
	"POST /api/v1/payments":                          ruleAccountBody,
	"GET /api/v1/payments/{id}":                      ruleGuardedRead,
	"GET /api/v1/users/{user_id}/notifications":      ruleSelf,
}

var publicRoutes = map[string]bool{
	"GET /health":                true,
	"GET /metrics":               true,
	"GET /api/v1/openapi.yaml":   true,
	"POST /api/v1/auth/register": true,
	"POST /api/v1/auth/login":    true,
}

func accountBody(route, accountID string) string {
	switch route {
	case "POST /api/v1/transfers":
		return `{"from_account_id":"` + accountID + `","to_account_id":"` + uuid.NewString() + `","amount":"1.00","currency":"BRL","idempotency_key":"k-1"}`
	case "POST /api/v1/payments":
		return `{"account_id":"` + accountID + `","payment_method":"pix","amount":"1.00","currency":"BRL","recipient":"Loja","pix_key":"11999887766","idempotency_key":"k-1"}`
	case "PATCH /api/v1/accounts/{id}":
		return `{"status":"blocked"}`
	}
	return `{"account_id":"` + accountID + `","type":"deposit","amount":"1.00","currency":"BRL","idempotency_key":"k-1"}`
}

type ruleHarness struct {
	router  http.Handler
	userID  uuid.UUID
	owners  *tests.FakeOwners
	foreign string
}

func newRuleHarness(t *testing.T, metricsName string) *ruleHarness {
	t.Helper()
	h := &ruleHarness{userID: uuid.New(), owners: tests.NewFakeOwners()}
	h.foreign = h.owners.Own(uuid.NewString(), uuid.New())
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"account_id":"` + h.foreign + `"}}`))
	}))
	t.Cleanup(upstream.Close)

	router := chi.NewRouter()
	cfg := &config.Config{
		CORS:          contracts.CORSConfig{AllowedOrigins: []string{"*"}, AllowedMethods: []string{"GET"}},
		RateLimit:     contracts.RateLimitConfig{Requests: 1000, Window: time.Minute},
		AuthRateLimit: contracts.RateLimitConfig{Requests: 1000, Window: time.Minute},
		Auth:          tests.AuthConfig(),
	}
	deps := testDependencies()
	deps.Metrics = metrics.New(metricsName)
	deps.Owners = h.owners
	target, _ := url.Parse(upstream.URL)
	deps.AccountService, deps.TransactionService, deps.PaymentService, deps.NotificationService = target, target, target, target
	appHttp.SetupRouter(router, cfg, deps)
	h.router = router
	return h
}

func (h *ruleHarness) do(method, path, body string, authenticated bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authenticated {
		req.Header.Set("Authorization", tests.BearerToken(h.userID))
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func (h *ruleHarness) assertRule(t *testing.T, operation string, rule accessRule) {
	t.Helper()
	method, route, _ := strings.Cut(operation, " ")
	unknown := uuid.NewString()
	withID := func(id string) string { return routeParamRegexp.ReplaceAllString(route, id) }

	var denied, missing *httptest.ResponseRecorder
	switch rule {
	case ruleSelf:
		denied = h.do(method, withID(uuid.NewString()), "", true)
	case ruleAccountPath:
		denied = h.do(method, withID(h.foreign), accountBody(operation, h.foreign), true)
		missing = h.do(method, withID(unknown), accountBody(operation, unknown), true)
	case ruleGuardedRead:
		denied = h.do(method, withID("t-1"), "", true)
	case ruleAccountBody:
		denied = h.do(method, route, accountBody(operation, h.foreign), true)
		missing = h.do(method, route, accountBody(operation, unknown), true)
	case ruleCreateForSelf:
		denied = h.do(method, route, `{"user_id":"`+uuid.NewString()+`","account_type":"checking","name":"Ana Souza","email":"ana@example.com","document":"52998224725"}`, true)
	default:
		t.Fatalf("%s has no access rule", operation)
	}

	assert.Equal(t, http.StatusForbidden, denied.Code, operation)
	assert.Equal(t, "FORBIDDEN", errorCode(tests.FromJson(denied.Body.String())), operation)
	if missing != nil {
		assert.Equal(t, http.StatusNotFound, missing.Code, operation)
		assert.Equal(t, "ACCOUNT_NOT_FOUND", errorCode(tests.FromJson(missing.Body.String())), operation)
	}
}

func TestEveryProtectedRouteRequiresATokenAndHasAnAccessRule(t *testing.T) {
	h := newRuleHarness(t, "test-router-access-rules")

	seen := make(map[string]bool)
	err := chi.Walk(h.router.(*chi.Mux), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		operation := method + " " + normaliseRoute(route)
		if publicRoutes[operation] {
			return nil
		}
		seen[operation] = true
		rule, ok := protectedRoutes[operation]
		if !assert.True(t, ok, "%s is protected but has no access rule in the table", operation) {
			return nil
		}

		path := routeParamRegexp.ReplaceAllString(normaliseRoute(route), uuid.NewString())
		for _, candidate := range []string{path, path + "/"} {
			rec := h.do(method, candidate, "{}", false)
			assert.Equal(t, http.StatusUnauthorized, rec.Code, method+" "+candidate)
			assert.Equal(t, "Bearer", rec.Header().Get("WWW-Authenticate"), method+" "+candidate)
		}

		h.assertRule(t, operation, rule)
		return nil
	})

	require.NoError(t, err)
	assert.Len(t, seen, len(protectedRoutes))
}
