package metrics

import (
	"errors"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry   *prometheus.Registry
	registerer prometheus.Registerer
}

func New(service string) *Metrics {
	registry := prometheus.NewRegistry()
	registerer := prometheus.WrapRegistererWith(prometheus.Labels{"service": service}, registry)

	registerer.MustRegister(collectors.NewGoCollector())
	registerer.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	return &Metrics{
		registry:   registry,
		registerer: registerer,
	}
}

func (m *Metrics) Registry() *prometheus.Registry {
	if m == nil {
		return nil
	}
	return m.registry
}

func (m *Metrics) Handler() http.Handler {
	if m == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) CounterVec(name, help string, labels ...string) *prometheus.CounterVec {
	return register(m, prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help}, labels))
}

func (m *Metrics) GaugeVec(name, help string, labels ...string) *prometheus.GaugeVec {
	return register(m, prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help}, labels))
}

func (m *Metrics) HistogramVec(name, help string, buckets []float64, labels ...string) *prometheus.HistogramVec {
	return register(m, prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: name, Help: help, Buckets: buckets}, labels))
}

func (m *Metrics) GaugeFunc(name, help string, fn func() float64) prometheus.GaugeFunc {
	return register(m, prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: name, Help: help}, fn))
}

func register[T prometheus.Collector](m *Metrics, collector T) T {
	if m == nil {
		return collector
	}

	if err := m.registerer.Register(collector); err != nil {
		var alreadyRegistered prometheus.AlreadyRegisteredError
		if errors.As(err, &alreadyRegistered) {
			if existing, ok := alreadyRegistered.ExistingCollector.(T); ok {
				return existing
			}
		}
		panic(err)
	}

	return collector
}
