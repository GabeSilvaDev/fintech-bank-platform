package tracing

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func restoreGlobals(t *testing.T) {
	t.Helper()
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		if otel.GetTracerProvider() != previousProvider {
			otel.SetTracerProvider(previousProvider)
		}
		otel.SetTextMapPropagator(previousPropagator)
	})
}

func useRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	restoreGlobals(t)

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
	})

	return recorder
}

func TestInitWithoutEndpointCreatesSpansWithoutExporting(t *testing.T) {
	restoreGlobals(t)

	shutdown, err := Init(context.Background(), Config{Service: "account-service"})
	require.NoError(t, err)

	_, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider)
	assert.True(t, ok)
	assert.ElementsMatch(t, []string{"traceparent", "tracestate", "baggage"}, otel.GetTextMapPropagator().Fields())

	ctx, span := Tracer().Start(context.Background(), "op")
	traceID, spanID, valid := IDs(ctx)
	span.End()

	assert.True(t, valid)
	assert.Equal(t, span.SpanContext().TraceID().String(), traceID)
	assert.Equal(t, span.SpanContext().SpanID().String(), spanID)
	assert.NoError(t, shutdown(context.Background()))
}

func TestInitWithEndpointExportsOnShutdown(t *testing.T) {
	restoreGlobals(t)

	type export struct {
		method string
		path   string
		body   []byte
	}
	received := make(chan export, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- export{method: r.Method, path: r.URL.Path, body: body}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	shutdown, err := Init(context.Background(), Config{Service: "account-service", Endpoint: server.URL, SampleRatio: 1})
	require.NoError(t, err)

	_, span := Tracer().Start(context.Background(), "op")
	span.End()

	require.NoError(t, shutdown(context.Background()))

	select {
	case got := <-received:
		assert.Equal(t, http.MethodPost, got.method)
		assert.Equal(t, "/v1/traces", got.path)
		assert.Contains(t, string(got.body), "account-service")
		assert.Contains(t, string(got.body), "op")
	case <-time.After(5 * time.Second):
		t.Fatal("no export received")
	}
}

func TestInitRejectsInvalidEndpoint(t *testing.T) {
	restoreGlobals(t)

	for _, endpoint := range []string{"://collector", "localhost:4318", "ftp://collector:4318", "http://"} {
		t.Run(endpoint, func(t *testing.T) {
			shutdown, err := Init(context.Background(), Config{Service: "svc", Endpoint: endpoint})
			assert.Error(t, err)
			assert.Nil(t, shutdown)
		})
	}
}

func TestInitReturnsExporterError(t *testing.T) {
	restoreGlobals(t)

	boom := errors.New("boom")
	previous := newExporter
	newExporter = func(context.Context, string) (sdktrace.SpanExporter, error) {
		return nil, boom
	}
	t.Cleanup(func() {
		newExporter = previous
	})

	shutdown, err := Init(context.Background(), Config{Service: "svc", Endpoint: "http://collector:4318"})
	assert.ErrorIs(t, err, boom)
	assert.Nil(t, shutdown)
}

func TestSampleRatioClampsOutOfRangeValues(t *testing.T) {
	cases := map[float64]float64{
		-1:   1,
		0:    1,
		0.25: 0.25,
		1:    1,
		1.5:  1,
	}

	for in, want := range cases {
		assert.Equal(t, want, sampleRatio(in), "ratio %v", in)
	}
}

func TestIDsReturnsValidSpanContext(t *testing.T) {
	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	}))

	gotTrace, gotSpan, ok := IDs(ctx)

	assert.True(t, ok)
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", gotTrace)
	assert.Equal(t, "00f067aa0ba902b7", gotSpan)
}

func TestIDsReturnsFalseWithoutSpan(t *testing.T) {
	traceID, spanID, ok := IDs(context.Background())

	assert.False(t, ok)
	assert.Empty(t, traceID)
	assert.Empty(t, spanID)
}
