package middleware

import (
	"net/http"
	"time"

	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/tracing"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

func Logger(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chiMiddleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			entry := log.Info()
			if ww.Status() >= http.StatusInternalServerError {
				entry = log.Error()
			}

			if traceID, spanID, ok := tracing.IDs(r.Context()); ok {
				entry = entry.Str("otel_trace_id", traceID).Str("otel_span_id", spanID)
			}

			if clientIP := chiMiddleware.GetClientIP(r.Context()); clientIP != "" {
				entry = entry.Str("client_ip", clientIP)
			}

			entry.
				Str("request_id", GetRequestID(r.Context())).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Str("remote_addr", r.RemoteAddr).
				Int("status", ww.Status()).
				Int("bytes", ww.BytesWritten()).
				Dur("duration", time.Since(start)).
				Msg("request completed")
		})
	}
}
