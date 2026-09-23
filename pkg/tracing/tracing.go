package tracing

import (
	"context"
	"fmt"
	"net/url"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/fintech-bank-platform/pkg/tracing"

type Config struct {
	Service     string
	Endpoint    string
	SampleRatio float64
}

var newExporter = func(ctx context.Context, endpoint string) (sdktrace.SpanExporter, error) {
	return otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
}

func Init(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	if cfg.Endpoint == "" {
		provider := sdktrace.NewTracerProvider()
		otel.SetTracerProvider(provider)
		return provider.Shutdown, nil
	}

	if err := validateEndpoint(cfg.Endpoint); err != nil {
		return nil, err
	}

	exporter, err := newExporter(ctx, cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("tracing: create exporter: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(cfg.Service))),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRatio(cfg.SampleRatio)))),
	)
	otel.SetTracerProvider(provider)

	return provider.Shutdown, nil
}

func Tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

func IDs(ctx context.Context) (traceID, spanID string, ok bool) {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return "", "", false
	}

	return spanContext.TraceID().String(), spanContext.SpanID().String(), true
}

func validateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("tracing: invalid endpoint %q: %w", endpoint, err)
	}

	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("tracing: invalid endpoint %q: want an http or https url with a host", endpoint)
	}

	return nil
}

func sampleRatio(ratio float64) float64 {
	if ratio <= 0 || ratio > 1 {
		return 1
	}

	return ratio
}
