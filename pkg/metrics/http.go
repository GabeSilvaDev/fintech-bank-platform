package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	requestsTotalName   = "http_requests_total"
	requestsTotalHelp   = "Total number of HTTP requests"
	requestDurationName = "http_request_duration_seconds"
	requestDurationHelp = "HTTP request duration in seconds"
)

func (m *Metrics) Middleware(next http.Handler) http.Handler {
	if m == nil {
		return next
	}

	requestsTotal := m.CounterVec(requestsTotalName, requestsTotalHelp, "method", "route", "status")
	requestDuration := m.HistogramVec(requestDurationName, requestDurationHelp, prometheus.DefBuckets, "method", "route")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chiMiddleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}

		requestsTotal.WithLabelValues(r.Method, route, strconv.Itoa(ww.Status())).Inc()
		requestDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
	})
}
