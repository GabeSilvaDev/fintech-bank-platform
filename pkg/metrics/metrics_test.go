package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRegistersGoAndProcessCollectorsUnderServiceLabel(t *testing.T) {
	m := New("svc")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `service="svc"`)
	assert.Contains(t, body, "go_goroutines")
}

func TestRegistryReturnsUnderlyingRegistry(t *testing.T) {
	m := New("svc")

	require.NotNil(t, m.Registry())
	assert.Greater(t, testutil.CollectAndCount(m.Registry()), 0)
}

func TestCounterVecFactoryIsIdempotent(t *testing.T) {
	m := New("svc")

	first := m.CounterVec("test_requests_total", "test help", "outcome")
	first.WithLabelValues("ok").Inc()

	second := m.CounterVec("test_requests_total", "test help", "outcome")
	second.WithLabelValues("ok").Inc()

	assert.Same(t, first, second)
	assert.Equal(t, float64(2), testutil.ToFloat64(first.WithLabelValues("ok")))
}

func TestGaugeVecFactoryIsIdempotent(t *testing.T) {
	m := New("svc")

	first := m.GaugeVec("test_gauge", "test help", "state")
	first.WithLabelValues("open").Set(3)

	second := m.GaugeVec("test_gauge", "test help", "state")

	assert.Same(t, first, second)
	assert.Equal(t, float64(3), testutil.ToFloat64(second.WithLabelValues("open")))
}

func TestHistogramVecFactoryIsIdempotent(t *testing.T) {
	m := New("svc")

	first := m.HistogramVec("test_duration_seconds", "test help", prometheus.DefBuckets, "step")
	first.WithLabelValues("write").Observe(0.2)

	second := m.HistogramVec("test_duration_seconds", "test help", prometheus.DefBuckets, "step")
	second.WithLabelValues("write").Observe(0.3)

	assert.Same(t, first, second)
	assert.Equal(t, uint64(2), histogramSampleCount(t, m.Registry(), "test_duration_seconds"))
}

func TestGaugeFuncReadsValueAtScrapeTime(t *testing.T) {
	m := New("svc")
	value := 1.0

	gauge := m.GaugeFunc("test_state", "test help", func() float64 { return value })

	assert.Equal(t, float64(1), testutil.ToFloat64(gauge))
	value = 2
	assert.Equal(t, float64(2), testutil.ToFloat64(gauge))

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Contains(t, rec.Body.String(), `test_state{service="svc"} 2`)
}

func TestGaugeFuncFactoryIsIdempotent(t *testing.T) {
	m := New("svc")

	first := m.GaugeFunc("test_state", "test help", func() float64 { return 1 })
	second := m.GaugeFunc("test_state", "test help", func() float64 { return 2 })

	assert.Same(t, first, second)
	assert.Equal(t, float64(1), testutil.ToFloat64(second))
}

func TestNilMetricsGaugeFuncReturnsUnregisteredGauge(t *testing.T) {
	var m *Metrics

	gauge := m.GaugeFunc("nil_state", "help", func() float64 { return 3 })

	assert.Equal(t, float64(3), testutil.ToFloat64(gauge))
}

func TestFactoryPanicsWhenExistingCollectorHasADifferentType(t *testing.T) {
	m := New("svc")
	m.CounterVec("conflict_metric", "same help", "label")

	assert.Panics(t, func() {
		m.HistogramVec("conflict_metric", "same help", prometheus.DefBuckets, "label")
	})
}

func TestFactoryPanicsOnInconsistentRegistration(t *testing.T) {
	m := New("svc")
	m.CounterVec("mismatched_help_total", "first help", "label")

	assert.Panics(t, func() {
		m.CounterVec("mismatched_help_total", "second help", "label")
	})
}

func TestNilMetricsRegistryReturnsNil(t *testing.T) {
	var m *Metrics
	assert.Nil(t, m.Registry())
}

func TestNilMetricsHandlerReturnsNotFoundHandler(t *testing.T) {
	var m *Metrics

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestNilMetricsMiddlewarePassesThrough(t *testing.T) {
	var m *Metrics
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	m.Middleware(next).ServeHTTP(rec, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestNilMetricsCounterVecReturnsFreshUnregisteredVec(t *testing.T) {
	var m *Metrics
	vec := m.CounterVec("nil_counter_total", "help", "label")

	assert.NotPanics(t, func() {
		vec.WithLabelValues("x").Inc()
	})
}

func TestNilMetricsGaugeVecReturnsFreshUnregisteredVec(t *testing.T) {
	var m *Metrics
	vec := m.GaugeVec("nil_gauge", "help", "label")

	assert.NotPanics(t, func() {
		vec.WithLabelValues("x").Set(1)
	})
}

func TestNilMetricsHistogramVecReturnsFreshUnregisteredVec(t *testing.T) {
	var m *Metrics
	vec := m.HistogramVec("nil_histogram_seconds", "help", prometheus.DefBuckets, "label")

	assert.NotPanics(t, func() {
		vec.WithLabelValues("x").Observe(1)
	})
}

func histogramSampleCount(t *testing.T, reg *prometheus.Registry, name string) uint64 {
	t.Helper()

	families, err := reg.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() == name {
			return family.GetMetric()[0].GetHistogram().GetSampleCount()
		}
	}

	t.Fatalf("metric %s not found", name)
	return 0
}
