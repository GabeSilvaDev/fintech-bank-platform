package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestMiddlewareRecordsRoutePatternAndStatus(t *testing.T) {
	m := New("svc")
	router := chi.NewRouter()
	router.Use(m.Middleware)
	router.Get("/accounts/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	req := httptest.NewRequest(http.MethodGet, "/accounts/123", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	requestsTotal := m.CounterVec(requestsTotalName, requestsTotalHelp, "method", "route", "status")
	assert.Equal(t, float64(1), testutil.ToFloat64(requestsTotal.WithLabelValues(http.MethodGet, "/accounts/{id}", "201")))
}

func TestMiddlewareRecordsUnmatchedRouteOnNotFound(t *testing.T) {
	m := New("svc")
	router := chi.NewRouter()
	router.Use(m.Middleware)
	router.Get("/accounts/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/does-not-exist", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)

	requestsTotal := m.CounterVec(requestsTotalName, requestsTotalHelp, "method", "route", "status")
	assert.Equal(t, float64(1), testutil.ToFloat64(requestsTotal.WithLabelValues(http.MethodGet, "unmatched", "404")))
}

func TestMiddlewareRecordsStatusOKWhenHandlerWritesNothing(t *testing.T) {
	m := New("svc")
	router := chi.NewRouter()
	router.Use(m.Middleware)
	router.Get("/accounts/{id}", func(w http.ResponseWriter, r *http.Request) {})

	req := httptest.NewRequest(http.MethodGet, "/accounts/123", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	requestsTotal := m.CounterVec(requestsTotalName, requestsTotalHelp, "method", "route", "status")
	assert.Equal(t, float64(1), testutil.ToFloat64(requestsTotal.WithLabelValues(http.MethodGet, "/accounts/{id}", "200")))
}

func TestMiddlewareMetricsAreScrapableThroughHandler(t *testing.T) {
	m := New("svc")
	router := chi.NewRouter()
	router.Use(m.Middleware)
	router.Get("/accounts/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/accounts/123", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRec := httptest.NewRecorder()
	m.Handler().ServeHTTP(metricsRec, metricsReq)

	body := metricsRec.Body.String()
	assert.Contains(t, body, `http_requests_total{method="GET",route="/accounts/{id}",service="svc",status="200"} 1`)
}

func TestMiddlewareRecordsFullRoutePatternOnNestedMountedRouter(t *testing.T) {
	m := New("svc")

	accounts := chi.NewRouter()
	accounts.Get("/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	router := chi.NewRouter()
	router.Use(m.Middleware)
	router.Route("/api/v1", func(r chi.Router) {
		r.Mount("/accounts", accounts)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/123", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	requestsTotal := m.CounterVec(requestsTotalName, requestsTotalHelp, "method", "route", "status")
	assert.Equal(t, float64(1), testutil.ToFloat64(requestsTotal.WithLabelValues(http.MethodGet, "/api/v1/accounts/{id}", "200")))
}

func TestMiddlewareObservesRequestDuration(t *testing.T) {
	m := New("svc")
	router := chi.NewRouter()
	router.Use(m.Middleware)
	router.Get("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, uint64(1), histogramSampleCount(t, m.Registry(), requestDurationName))
}

func TestMiddlewareCollapsesNonStandardMethodsIntoOther(t *testing.T) {
	m := New("svc")
	router := chi.NewRouter()
	router.Use(m.Middleware)
	router.Get("/accounts/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for _, method := range []string{"FOO", "BAR", "PROPFIND"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, "/accounts/123", nil))
		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/accounts/123", nil))

	requestsTotal := m.CounterVec(requestsTotalName, requestsTotalHelp, "method", "route", "status")
	assert.Equal(t, 2, testutil.CollectAndCount(requestsTotal))
	assert.Equal(t, 2, testutil.CollectAndCount(m.Registry(), requestDurationName))

	metricsRec := httptest.NewRecorder()
	m.Handler().ServeHTTP(metricsRec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := metricsRec.Body.String()
	assert.Regexp(t, `http_requests_total\{method="OTHER",route="[^"]*",service="svc",status="405"\} 3`, body)
	assert.Contains(t, body, `method="DELETE"`)
	for _, method := range []string{"FOO", "BAR", "PROPFIND"} {
		assert.NotContains(t, body, `method="`+method+`"`)
	}
}
