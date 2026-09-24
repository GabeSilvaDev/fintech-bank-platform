package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/response"
	"github.com/fintech-bank-platform/pkg/tracing"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

func Recovery(log *logger.Logger) func(http.Handler) http.Handler {
	if log == nil {
		log = logger.NewDefault()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := chiMiddleware.NewWrapResponseWriter(w, r.ProtoMajor)

			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if rec == http.ErrAbortHandler {
					panic(rec)
				}

				entry := log.Error().
					Interface("panic", rec).
					Bytes("stack", debug.Stack()).
					Str("request_id", GetRequestID(r.Context())).
					Str("method", r.Method).
					Str("path", r.URL.Path)

				if traceID, spanID, ok := tracing.IDs(r.Context()); ok {
					entry = entry.Str("otel_trace_id", traceID).Str("otel_span_id", spanID)
				}

				entry.Msg("handler panicked")

				if ww.Status() == 0 {
					response.AppError(ww, errors.InternalServer("INTERNAL_ERROR", "internal server error"))
				}
			}()

			next.ServeHTTP(ww, r)
		})
	}
}
