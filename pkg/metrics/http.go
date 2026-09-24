package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/fintech-bank-platform/pkg/internal/httpmethod"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	requestsTotalName   = "http_requests_total"
	requestsTotalHelp   = "Total number of HTTP requests"
	requestDurationName = "http_request_duration_seconds"
	requestDurationHelp = "HTTP request duration in seconds"
	otherMethod         = "OTHER"
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

		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}

		method := methodLabel(r.Method)
		requestsTotal.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
		requestDuration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())
	})
}

func methodLabel(method string) string {
	if httpmethod.Known(method) {
		return method
	}
	return otherMethod
}
