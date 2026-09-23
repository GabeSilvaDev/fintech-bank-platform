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
