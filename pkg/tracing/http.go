package tracing

import (
	"net/http"

	"github.com/fintech-bank-platform/pkg/internal/httpmethod"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	"go.opentelemetry.io/otel/trace"
)

const otherMethodName = "HTTP"

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, methodAttrs := method(r.Method)
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := Tracer().Start(ctx, name,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(append(methodAttrs, semconv.URLPath(r.URL.Path))...),
		)
		defer span.End()

		ww := chiMiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r.WithContext(ctx))

		if route := chi.RouteContext(r.Context()).RoutePattern(); route != "" {
			span.SetName(name + " " + route)
			span.SetAttributes(semconv.HTTPRoute(route))
		}

		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}

		recordStatus(span, status)
	})
}

func Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}

	return &transport{base: base}
}

type transport struct {
	base http.RoundTripper
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	name, methodAttrs := method(req.Method)
	ctx, span := Tracer().Start(req.Context(), name+" "+req.URL.Host,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(append(methodAttrs,
			semconv.ServerAddress(req.URL.Hostname()),
			semconv.URLPath(req.URL.Path),
		)...),
	)
	defer span.End()

	outgoing := req.Clone(ctx)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(outgoing.Header))

	resp, err := t.base.RoundTrip(outgoing)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	recordStatus(span, resp.StatusCode)

	return resp, nil
}

func method(raw string) (string, []attribute.KeyValue) {
	if httpmethod.Known(raw) {
		return raw, []attribute.KeyValue{semconv.HTTPRequestMethodKey.String(raw)}
	}

	return otherMethodName, []attribute.KeyValue{semconv.HTTPRequestMethodOther, semconv.HTTPRequestMethodOriginal(raw)}
}

func recordStatus(span trace.Span, status int) {
	span.SetAttributes(semconv.HTTPResponseStatusCode(status))
	if status >= http.StatusInternalServerError {
		span.SetStatus(codes.Error, http.StatusText(status))
	}
}
