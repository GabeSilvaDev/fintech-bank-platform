package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/fintech-bank-platform/api-gateway/internal/app/handlers"
	"github.com/fintech-bank-platform/api-gateway/tests"
	"github.com/fintech-bank-platform/pkg/middleware"
	"github.com/fintech-bank-platform/pkg/tracing"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func useTraceRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})

	return recorder
}

func proxyRouter(upstream, serviceName string) http.Handler {
	target, _ := url.Parse(upstream)
	proxy := http.StripPrefix("/api/v1", handlers.NewReadProxy(target, serviceName))
	r := chi.NewRouter()
	r.Get("/api/v1/accounts/{id}", proxy.ServeHTTP)
	r.Get("/api/v1/users/{user_id}/accounts", proxy.ServeHTTP)
	r.Get("/api/v1/transactions/{id}", proxy.ServeHTTP)
	r.Get("/api/v1/accounts/{account_id}/transactions", proxy.ServeHTTP)
	r.Get("/api/v1/payments/{id}", proxy.ServeHTTP)
	r.Get("/api/v1/accounts/{account_id}/payments", proxy.ServeHTTP)
	r.Get("/api/v1/users/{user_id}/notifications", proxy.ServeHTTP)
	return r
}

func TestReadProxyForwardsPathAndRequestID(t *testing.T) {
	var gotPath, gotRequestID string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRequestID = r.Header.Get(middleware.RequestIDHeader)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":{"account_id":"abc"}}`))
	}))
	defer upstream.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/abc", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.RequestIDKey, "req-9"))
	rec := httptest.NewRecorder()
	proxyRouter(upstream.URL, "account service").ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/accounts/abc", gotPath)
	assert.Equal(t, "req-9", gotRequestID)
	assert.Equal(t, "abc", tests.FromJson(rec.Body.String())["data"].(map[string]interface{})["account_id"])
}

func TestReadProxyKeepsASingleRequestIDHeader(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(middleware.RequestIDHeader, r.Header.Get(middleware.RequestIDHeader))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
	}))
	defer upstream.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/abc", nil)
	req.Header.Set(middleware.RequestIDHeader, "req-9")
	rec := httptest.NewRecorder()
	middleware.RequestID(proxyRouter(upstream.URL, "account service")).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"req-9"}, rec.Header().Values(middleware.RequestIDHeader))
}

func TestReadProxyPassesUpstreamStatusThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":"ACCOUNT_NOT_FOUND"}}`))
	}))
	defer upstream.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/u1/accounts", nil)
	rec := httptest.NewRecorder()
	proxyRouter(upstream.URL, "account service").ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "ACCOUNT_NOT_FOUND")
}

func TestReadProxyAnswers502WhenUpstreamIsDown(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	upstream.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/abc", nil)
	rec := httptest.NewRecorder()
	proxyRouter(upstream.URL, "account service").ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	errorBody := tests.FromJson(rec.Body.String())["error"].(map[string]interface{})
	assert.Equal(t, "UPSTREAM_UNAVAILABLE", errorBody["code"])
	assert.Equal(t, "account service is unavailable", errorBody["message"])
}

func TestReadProxyAnswers502NamingTheTransactionServiceWhenItIsDown(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	upstream.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions/abc", nil)
	rec := httptest.NewRecorder()
	proxyRouter(upstream.URL, "transaction service").ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	errorBody := tests.FromJson(rec.Body.String())["error"].(map[string]interface{})
	assert.Equal(t, "UPSTREAM_UNAVAILABLE", errorBody["code"])
	assert.Equal(t, "transaction service is unavailable", errorBody["message"])
}

func TestReadProxyForwardsTransactionPaths(t *testing.T) {
	var paths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
	}))
	defer upstream.Close()

	for _, path := range []string{"/api/v1/transactions/abc", "/api/v1/accounts/a1/transactions?limit=5"} {
		rec := httptest.NewRecorder()
		proxyRouter(upstream.URL, "account service").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusOK, rec.Code)
	}
	assert.Equal(t, []string{"/transactions/abc", "/accounts/a1/transactions?limit=5"}, paths)
}

func TestReadProxyForwardsNextBeforeHeaderAndBeforeQuery(t *testing.T) {
	var gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("X-Next-Before", "2026-09-20T10:00:00.000Z")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
	}))
	defer upstream.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/a1/transactions?limit=2&before=2026-09-20T10%3A01%3A00Z", nil)
	rec := httptest.NewRecorder()
	proxyRouter(upstream.URL, "transaction service").ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "limit=2&before=2026-09-20T10%3A01%3A00Z", gotQuery)
	assert.Equal(t, "2026-09-20T10:00:00.000Z", rec.Header().Get("X-Next-Before"))
}

func TestReadProxyForwardsPaymentPaths(t *testing.T) {
	var paths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
	}))
	defer upstream.Close()

	for _, path := range []string{"/api/v1/payments/abc", "/api/v1/accounts/a1/payments?limit=5"} {
		rec := httptest.NewRecorder()
		proxyRouter(upstream.URL, "payment service").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusOK, rec.Code)
	}
	assert.Equal(t, []string{"/payments/abc", "/accounts/a1/payments?limit=5"}, paths)
}

func TestReadProxyAnswers502NamingThePaymentServiceWhenItIsDown(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	upstream.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/payments/abc", nil)
	rec := httptest.NewRecorder()
	proxyRouter(upstream.URL, "payment service").ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	errorBody := tests.FromJson(rec.Body.String())["error"].(map[string]interface{})
	assert.Equal(t, "UPSTREAM_UNAVAILABLE", errorBody["code"])
	assert.Equal(t, "payment service is unavailable", errorBody["message"])
}

func TestReadProxyForwardsNotificationPaths(t *testing.T) {
	var paths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
	}))
	defer upstream.Close()

	rec := httptest.NewRecorder()
	proxyRouter(upstream.URL, "notification service").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/users/u1/notifications?limit=5", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"/users/u1/notifications?limit=5"}, paths)
}

func TestReadProxyAnswers502NamingTheNotificationServiceWhenItIsDown(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	upstream.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/u1/notifications", nil)
	rec := httptest.NewRecorder()
	proxyRouter(upstream.URL, "notification service").ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	errorBody := tests.FromJson(rec.Body.String())["error"].(map[string]interface{})
	assert.Equal(t, "UPSTREAM_UNAVAILABLE", errorBody["code"])
	assert.Equal(t, "notification service is unavailable", errorBody["message"])
}

func TestReadProxyForwardsTraceParentToUpstream(t *testing.T) {
	recorder := useTraceRecorder(t)

	var gotTraceParent string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceParent = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
	}))
	defer upstream.Close()

	ctx, span := tracing.Tracer().Start(context.Background(), "incoming request")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/abc", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	proxyRouter(upstream.URL, "account service").ServeHTTP(rec, req)
	span.End()

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotEmpty(t, gotTraceParent)
	assert.Contains(t, gotTraceParent, span.SpanContext().TraceID().String())

	spans := recorder.Ended()
	require.Len(t, spans, 2)
	var clientSpan sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.SpanKind() == trace.SpanKindClient {
			clientSpan = s
		}
	}
	require.NotNil(t, clientSpan)
	assert.Contains(t, gotTraceParent, clientSpan.SpanContext().SpanID().String())
}

func TestReadProxyDoesNotForwardTheAccessToken(t *testing.T) {
	var authorization []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Values("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
	}))
	defer upstream.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/abc", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	proxyRouter(upstream.URL, "account service").ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, authorization)
}
