package messaging

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/fintech-bank-platform/pkg/tracing"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func useRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})

	return recorder
}

func headerValue(headers []kafka.Header, key string) (string, bool) {
	for _, h := range headers {
		if h.Key == key {
			return string(h.Value), true
		}
	}
	return "", false
}

func spanAttributes(span sdktrace.ReadOnlySpan) map[attribute.Key]string {
	attrs := map[attribute.Key]string{}
	for _, kv := range span.Attributes() {
		attrs[kv.Key] = kv.Value.Emit()
	}
	return attrs
}

func publishedCount(m *metrics.Metrics, topic, outcome string) float64 {
	return testutil.ToFloat64(m.CounterVec(publishedTotalName, publishedTotalHelp, "topic", "outcome").WithLabelValues(topic, outcome))
}

type fakeWriter struct {
	Err      error
	Messages []kafka.Message
	Closed   bool
}

func (f *fakeWriter) WriteMessages(_ context.Context, msgs ...kafka.Message) error {
	if f.Err != nil {
		return f.Err
	}
	f.Messages = append(f.Messages, msgs...)
	return nil
}

func (f *fakeWriter) Close() error {
	f.Closed = true
	return nil
}

func TestProducerPublishWritesMessage(t *testing.T) {
	w := &fakeWriter{}
	p := NewProducerWithWriter(w, 0)
	ev := events.NewAccountCommand(events.EventTypes.CreateAccount, map[string]string{"user_id": "u1"}).WithTraceID("trace-1")

	err := p.Publish(context.Background(), events.Topics.AccountCommands, "u1", ev)

	assert.NoError(t, err)
	assert.Len(t, w.Messages, 1)

	msg := w.Messages[0]
	assert.Equal(t, events.Topics.AccountCommands, msg.Topic)
	assert.Equal(t, "u1", string(msg.Key))
	assert.Equal(t, ev.Timestamp, msg.Time)
	assert.Equal(t, []kafka.Header{
		{Key: "event_type", Value: []byte(events.EventTypes.CreateAccount)},
		{Key: "trace_id", Value: []byte("trace-1")},
	}, msg.Headers)

	decoded, err := events.FromJSON(msg.Value)
	assert.NoError(t, err)
	assert.Equal(t, ev.ID, decoded.ID)
	assert.Equal(t, events.EventTypes.CreateAccount, decoded.Type)
	assert.Equal(t, "trace-1", decoded.TraceID)
}

func TestProducerPublishReturnsServiceUnavailableOnWriteError(t *testing.T) {
	w := &fakeWriter{Err: errors.New("broker down")}
	p := NewProducerWithWriter(w, 0)
	ev := events.NewAccountCommand(events.EventTypes.CreateAccount, nil)

	err := p.Publish(context.Background(), events.Topics.AccountCommands, "k", ev)

	appErr, ok := apperrors.AsAppError(err)
	assert.True(t, ok)
	assert.Equal(t, "PUBLISH_FAILED", appErr.Code)
	assert.Equal(t, http.StatusServiceUnavailable, appErr.HTTPStatus)
	assert.ErrorContains(t, err, "broker down")
}

func TestProducerPublishReturnsInternalErrorWhenEventCannotBeEncoded(t *testing.T) {
	w := &fakeWriter{}
	p := NewProducerWithWriter(w, 0)
	ev := events.NewAccountCommand(events.EventTypes.CreateAccount, make(chan int))

	err := p.Publish(context.Background(), events.Topics.AccountCommands, "k", ev)

	appErr, ok := apperrors.AsAppError(err)
	assert.True(t, ok)
	assert.Equal(t, "EVENT_ENCODING_FAILED", appErr.Code)
	assert.Equal(t, http.StatusInternalServerError, appErr.HTTPStatus)
	assert.Empty(t, w.Messages)
}

type blockingWriter struct{}

func (blockingWriter) WriteMessages(ctx context.Context, _ ...kafka.Message) error {
	<-ctx.Done()
	return ctx.Err()
}

func (blockingWriter) Close() error {
	return nil
}

func TestProducerPublishTimesOutAfterPublishTimeout(t *testing.T) {
	p := NewProducerWithWriter(blockingWriter{}, 20*time.Millisecond)
	ev := events.NewAccountCommand(events.EventTypes.CreateAccount, nil)

	err := p.Publish(context.Background(), events.Topics.AccountCommands, "k", ev)

	appErr, ok := apperrors.AsAppError(err)
	assert.True(t, ok)
	assert.Equal(t, "PUBLISH_FAILED", appErr.Code)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestProducerCloseClosesWriter(t *testing.T) {
	w := &fakeWriter{}
	p := NewProducerWithWriter(w, 0)

	assert.NoError(t, p.Close())
	assert.True(t, w.Closed)
}

func TestNewProducerBuildsKafkaWriter(t *testing.T) {
	p := NewProducer(ProducerConfig{
		Brokers:        []string{"localhost:9092"},
		WriteTimeout:   time.Second,
		BatchTimeout:   10 * time.Millisecond,
		PublishTimeout: 20 * time.Second,
		MaxAttempts:    2,
	})

	assert.NotNil(t, p)
	assert.NoError(t, p.Close())
}

func TestProducerWithMetricsReturnsSameProducer(t *testing.T) {
	p := NewProducerWithWriter(&fakeWriter{}, 0)

	assert.Same(t, p, p.WithMetrics(metrics.New("svc")))
}

func TestProducerPublishStartsProducerSpanAndInjectsTraceParent(t *testing.T) {
	recorder := useRecorder(t)
	m := metrics.New("svc")
	w := &fakeWriter{}
	p := NewProducerWithWriter(w, 0).WithMetrics(m)
	ev := events.NewAccountCommand(events.EventTypes.CreateAccount, nil).WithTraceID("trace-1")

	ctx, parent := tracing.Tracer().Start(context.Background(), "request")
	err := p.Publish(ctx, events.Topics.AccountCommands, "k", ev)
	parent.End()

	require.NoError(t, err)
	spans := recorder.Ended()
	require.Len(t, spans, 2)
	span := spans[0]
	assert.Equal(t, "publish "+events.Topics.AccountCommands, span.Name())
	assert.Equal(t, trace.SpanKindProducer, span.SpanKind())
	assert.Equal(t, parent.SpanContext().SpanID(), span.Parent().SpanID())
	assert.Equal(t, parent.SpanContext().TraceID(), span.SpanContext().TraceID())
	assert.Equal(t, codes.Unset, span.Status().Code)
	attrs := spanAttributes(span)
	assert.Equal(t, "kafka", attrs["messaging.system"])
	assert.Equal(t, events.Topics.AccountCommands, attrs["messaging.destination.name"])
	assert.Equal(t, "send", attrs["messaging.operation.type"])

	require.Len(t, w.Messages, 1)
	headers := w.Messages[0].Headers
	eventType, _ := headerValue(headers, "event_type")
	assert.Equal(t, events.EventTypes.CreateAccount, eventType)
	traceID, _ := headerValue(headers, "trace_id")
	assert.Equal(t, "trace-1", traceID)
	traceParent, ok := headerValue(headers, "traceparent")
	assert.True(t, ok)
	assert.Equal(t, "00-"+span.SpanContext().TraceID().String()+"-"+span.SpanContext().SpanID().String()+"-01", traceParent)

	assert.Equal(t, float64(1), publishedCount(m, events.Topics.AccountCommands, "ok"))
	assert.Equal(t, float64(0), publishedCount(m, events.Topics.AccountCommands, "error"))
}

func TestProducerPublishMarksSpanAndCounterOnFailure(t *testing.T) {
	recorder := useRecorder(t)
	m := metrics.New("svc")
	p := NewProducerWithWriter(&fakeWriter{Err: errors.New("broker down")}, 0).WithMetrics(m)
	ev := events.NewAccountCommand(events.EventTypes.CreateAccount, nil)

	err := p.Publish(context.Background(), events.Topics.AccountCommands, "k", ev)

	require.Error(t, err)
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assert.Contains(t, spans[0].Status().Description, "broker down")
	require.Len(t, spans[0].Events(), 1)
	assert.Equal(t, "exception", spans[0].Events()[0].Name)
	assert.Equal(t, float64(1), publishedCount(m, events.Topics.AccountCommands, "error"))
	assert.Equal(t, float64(0), publishedCount(m, events.Topics.AccountCommands, "ok"))
}

func TestProducerPublishCountsEncodingFailuresAsErrors(t *testing.T) {
	m := metrics.New("svc")
	p := NewProducerWithWriter(&fakeWriter{}, 0).WithMetrics(m)
	ev := events.NewAccountCommand(events.EventTypes.CreateAccount, make(chan int))

	assert.Error(t, p.Publish(context.Background(), events.Topics.AccountCommands, "k", ev))
	assert.Equal(t, float64(1), publishedCount(m, events.Topics.AccountCommands, "error"))
}
