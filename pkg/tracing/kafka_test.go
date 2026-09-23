package tracing

import (
	"context"
	"testing"

	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func header(headers []kafka.Header, key string) (string, bool) {
	for _, h := range headers {
		if h.Key == key {
			return string(h.Value), true
		}
	}

	return "", false
}

func TestInjectExtractRoundTripKeepsTraceID(t *testing.T) {
	useRecorder(t)

	ctx, span := Tracer().Start(context.Background(), "publish")
	defer span.End()

	original := []kafka.Header{{Key: "content-type", Value: []byte("application/json")}}
	headers := Inject(ctx, original)

	require.Len(t, original, 1)
	contentType, ok := header(headers, "content-type")
	assert.True(t, ok)
	assert.Equal(t, "application/json", contentType)
	traceParent, ok := header(headers, "traceparent")
	assert.True(t, ok)
	assert.Contains(t, traceParent, span.SpanContext().TraceID().String())

	extracted := trace.SpanContextFromContext(Extract(context.Background(), headers))
	assert.True(t, extracted.IsValid())
	assert.True(t, extracted.IsRemote())
	assert.Equal(t, span.SpanContext().TraceID(), extracted.TraceID())
	assert.Equal(t, span.SpanContext().SpanID(), extracted.SpanID())
}

func TestInjectAddsTraceState(t *testing.T) {
	useRecorder(t)

	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	state, err := trace.ParseTraceState("vendor=value")
	require.NoError(t, err)
	ctx := trace.ContextWithRemoteSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		TraceState: state,
	}))

	headers := Inject(ctx, nil)

	traceParent, _ := header(headers, "traceparent")
	assert.Equal(t, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", traceParent)
	traceState, ok := header(headers, "tracestate")
	assert.True(t, ok)
	assert.Equal(t, "vendor=value", traceState)
}

func TestInjectWithoutSpanAddsNothing(t *testing.T) {
	useRecorder(t)

	headers := Inject(context.Background(), []kafka.Header{{Key: "k", Value: []byte("v")}})

	assert.Equal(t, []kafka.Header{{Key: "k", Value: []byte("v")}}, headers)
}

func TestInjectReplacesStaleTraceParentWithoutMutatingInput(t *testing.T) {
	useRecorder(t)

	ctx, span := Tracer().Start(context.Background(), "publish")
	defer span.End()

	stale := "00-11111111111111111111111111111111-2222222222222222-01"
	original := []kafka.Header{{Key: "traceparent", Value: []byte(stale)}}
	headers := Inject(ctx, original)

	require.Len(t, headers, 1)
	assert.Contains(t, string(headers[0].Value), span.SpanContext().TraceID().String())
	assert.Equal(t, stale, string(original[0].Value))
}

func TestExtractWithoutHeadersReturnsInvalidSpanContext(t *testing.T) {
	useRecorder(t)

	ctx := Extract(context.Background(), nil)

	assert.False(t, trace.SpanContextFromContext(ctx).IsValid())
}

func TestHeaderCarrierGetSetKeys(t *testing.T) {
	carrier := &headerCarrier{headers: []kafka.Header{
		{Key: "traceparent", Value: []byte("old")},
		{Key: "traceparent", Value: []byte("duplicate")},
	}}

	assert.Equal(t, "old", carrier.Get("traceparent"))
	assert.Empty(t, carrier.Get("missing"))

	carrier.Set("traceparent", "new")
	carrier.Set("tracestate", "vendor=value")

	assert.Equal(t, "new", carrier.Get("traceparent"))
	assert.Equal(t, "vendor=value", carrier.Get("tracestate"))
	assert.Equal(t, []string{"traceparent", "traceparent", "tracestate"}, carrier.Keys())
}
