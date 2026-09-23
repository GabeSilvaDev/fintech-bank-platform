package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
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

type scripted struct {
	Errs   []error
	Panic  bool
	Calls  int
	OnCall func()
	Result Result
}

func (s *scripted) Dispatch(_ context.Context, cmd *events.Event) (Result, error) {
	s.Calls++
	if s.OnCall != nil {
		s.OnCall()
	}
	if s.Panic {
		panic("boom")
	}
	if len(s.Errs) > 0 {
		err := s.Errs[0]
		s.Errs = s.Errs[1:]
		if err != nil {
			return Result{}, err
		}
	}
	if s.Result.Messages == nil {
		return Reply("results", "key-1", events.NewEvent("account.created", "test", map[string]string{"ok": "1"}).WithTraceID(cmd.TraceID)), nil
	}
	return s.Result, nil
}

type fakeStore struct {
	seen map[uuid.UUID]bool
	errs []error
}

func (f *fakeStore) MarkProcessed(_ context.Context, id uuid.UUID) (bool, error) {
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return false, err
		}
	}
	first := !f.seen[id]
	f.seen[id] = true
	return first, nil
}

type published struct {
	Topic string
	Key   string
	Event *events.Event
}

type fakePublisher struct {
	err        error
	errByTopic map[string]error
	published  []published
	contexts   []context.Context
}

func (f *fakePublisher) Publish(ctx context.Context, topic, key string, event *events.Event) error {
	f.contexts = append(f.contexts, ctx)
	if f.err != nil {
		return f.err
	}
	if err, ok := f.errByTopic[topic]; ok {
		return err
	}
	f.published = append(f.published, published{topic, key, event})
	return nil
}

func (f *fakePublisher) byTopic(topic string) []published {
	result := []published{}
	for _, p := range f.published {
		if p.Topic == topic {
			result = append(result, p)
		}
	}
	return result
}

type processorHarness struct {
	dispatcher *scripted
	store      *fakeStore
	publisher  *fakePublisher
	logs       *bytes.Buffer
	processor  *Processor
}

func newProcessor(backoff ...time.Duration) *processorHarness {
	h := &processorHarness{
		dispatcher: &scripted{},
		store:      &fakeStore{seen: map[uuid.UUID]bool{}},
		publisher:  &fakePublisher{},
		logs:       &bytes.Buffer{},
	}
	if backoff == nil {
		backoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	}
	log := logger.New(logger.Config{Level: "debug", Output: h.logs})
	h.processor = NewProcessor(h.dispatcher, h.store, h.publisher, Config{
		Source:          "test",
		FailedEventType: "test.failed",
		DLQTopic:        "dlq",
		Backoff:         backoff,
	}, log)
	return h
}

func command(payload interface{}) *events.Event {
	return events.NewEvent("account.create", "test", payload).WithTraceID("trace-1")
}

func encoded(cmd *events.Event) []byte {
	data, _ := cmd.ToJSON()
	return data
}

func logEntry(t *testing.T, logs *bytes.Buffer, message string) map[string]interface{} {
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err == nil && entry["message"] == message {
			return entry
		}
	}
	t.Fatalf("log entry %q not found in %s", message, logs.String())
	return nil
}

func TestProcessPublishesResult(t *testing.T) {
	h := newProcessor()
	cmd := command(map[string]string{"a": "1"})

	err := h.processor.Process(context.Background(), []byte("k"), encoded(cmd))

	assert.NoError(t, err)
	assert.Equal(t, 1, h.dispatcher.Calls)
	published := h.publisher.byTopic("results")
	assert.Len(t, published, 1)
	assert.Equal(t, "key-1", published[0].Key)
	assert.Equal(t, "trace-1", published[0].Event.TraceID)
	assert.Empty(t, h.publisher.byTopic("dlq"))
}

func TestProcessSkipsDuplicates(t *testing.T) {
	h := newProcessor()
	cmd := command(map[string]string{"a": "1"})

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))
	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	assert.Equal(t, 1, h.dispatcher.Calls)
	assert.Len(t, h.publisher.published, 1)
	assert.Contains(t, h.logs.String(), "duplicate event skipped")
}

func TestProcessDeadLettersUndecodableMessages(t *testing.T) {
	h := newProcessor()

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), []byte("{nope")))
	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), []byte(`{"id":"not-a-uuid","type":"account.create"}`)))

	dlq := h.publisher.byTopic("dlq")
	assert.Len(t, dlq, 2)
	assert.Equal(t, "test.failed", dlq[0].Event.Type)
	assert.Equal(t, "invalid_event", dlq[0].Event.Payload.(events.ErrorPayload).ErrorCode)
	assert.Nil(t, dlq[0].Event.Payload.(events.ErrorPayload).OriginalEvent)
	assert.Equal(t, 0, dlq[0].Event.Payload.(events.ErrorPayload).Retries)
	assert.NotNil(t, dlq[1].Event.Payload.(events.ErrorPayload).OriginalEvent)
	assert.Equal(t, 0, h.dispatcher.Calls)
}

func TestProcessDeadLettersUnprocessableWithoutRetry(t *testing.T) {
	cases := map[string]error{
		"unknown_command": ErrUnknownCommand,
		"bad_payload":     ErrBadPayload,
		"not_found":       domain.ErrNotFound,
		"account_closed":  domain.Invalid("account_closed", "closed"),
		"ambiguous_write": fmt.Errorf("%w: x", domain.ErrAmbiguousWrite),
	}

	for code, dispatchErr := range cases {
		h := newProcessor()
		h.dispatcher.Errs = []error{dispatchErr}
		cmd := command(map[string]string{"a": "1"})

		assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

		assert.Equal(t, 1, h.dispatcher.Calls, code)
		dlq := h.publisher.byTopic("dlq")
		assert.Len(t, dlq, 1, code)
		payload := dlq[0].Event.Payload.(events.ErrorPayload)
		assert.Equal(t, code, payload.ErrorCode, code)
		assert.Equal(t, cmd.ID, payload.OriginalEvent.ID, code)
		assert.Equal(t, 1, payload.Retries, code)
		assert.Equal(t, "trace-1", dlq[0].Event.TraceID, code)
		assert.Equal(t, "k", dlq[0].Key, code)
	}
}

func TestProcessDeadLettersAmbiguousWriteFromCancelledContext(t *testing.T) {
	h := newProcessor()
	h.dispatcher.Errs = []error{fmt.Errorf("%w: %v", domain.ErrAmbiguousWrite, context.Canceled)}
	cmd := command(map[string]string{"a": "1"})

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	assert.Equal(t, 1, h.dispatcher.Calls)
	dlq := h.publisher.byTopic("dlq")
	assert.Len(t, dlq, 1)
	payload := dlq[0].Event.Payload.(events.ErrorPayload)
	assert.Equal(t, "ambiguous_write", payload.ErrorCode)
	assert.Equal(t, cmd.ID, payload.OriginalEvent.ID)
	assert.Equal(t, 1, payload.Retries)
	assert.Equal(t, "trace-1", dlq[0].Event.TraceID)
	assert.Equal(t, "k", dlq[0].Key)
}

func TestProcessRetriesTransientErrorsThenSucceeds(t *testing.T) {
	h := newProcessor()
	h.dispatcher.Errs = []error{errors.New("timeout"), errors.New("timeout"), errors.New("timeout"), nil}
	cmd := command(map[string]string{"a": "1"})

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	assert.Equal(t, 4, h.dispatcher.Calls)
	assert.Len(t, h.publisher.byTopic("results"), 1)
}

func TestProcessDeadLettersAfterRetriesExhausted(t *testing.T) {
	h := newProcessor()
	h.dispatcher.Errs = []error{errors.New("timeout"), errors.New("timeout"), errors.New("timeout"), errors.New("timeout")}
	cmd := command(map[string]string{"a": "1"})

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	assert.Equal(t, 4, h.dispatcher.Calls)
	dlq := h.publisher.byTopic("dlq")
	payload := dlq[0].Event.Payload.(events.ErrorPayload)
	assert.Equal(t, "internal_error", payload.ErrorCode)
	assert.Equal(t, 4, payload.Retries)
}

func TestProcessTreatsConflictAsTransient(t *testing.T) {
	h := newProcessor()
	h.dispatcher.Errs = []error{domain.ErrConflict, domain.ErrConflict, domain.ErrConflict, domain.ErrConflict}
	cmd := command(nil)

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	assert.Equal(t, 4, h.dispatcher.Calls)
	assert.Equal(t, "conflict", h.publisher.byTopic("dlq")[0].Event.Payload.(events.ErrorPayload).ErrorCode)
}

func TestProcessRecoversFromPanics(t *testing.T) {
	h := newProcessor()
	h.dispatcher.Panic = true
	cmd := command(map[string]string{"a": "1"})

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	assert.Equal(t, 1, h.dispatcher.Calls)
	assert.Equal(t, "panic", h.publisher.byTopic("dlq")[0].Event.Payload.(events.ErrorPayload).ErrorCode)
	assert.Contains(t, h.logs.String(), "boom")
}

func TestProcessDeadLettersWhenResultPublishFails(t *testing.T) {
	h := newProcessor()
	h.publisher.errByTopic = map[string]error{"results": errors.New("broker down")}
	cmd := command(map[string]string{"a": "1"})

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	dlq := h.publisher.byTopic("dlq")
	assert.Len(t, dlq, 1)
	payload := dlq[0].Event.Payload.(events.ErrorPayload)
	assert.Equal(t, "publish_failed", payload.ErrorCode)
	assert.Equal(t, 0, payload.Retries)
	assert.Equal(t, "account.created", payload.OriginalEvent.Type)

	entry := logEntry(t, h.logs, "result publish failed")
	assert.Equal(t, payload.OriginalEvent.ID, entry["event_id"])
	assert.Equal(t, "trace-1", entry["trace_id"])
	assert.Equal(t, "account.created", entry["type"])
	assert.Equal(t, "broker down", entry["error"])
	assert.Equal(t, payload.OriginalEvent.ID, entry["event"].(map[string]interface{})["id"])
	assert.Equal(t, "1", entry["event"].(map[string]interface{})["payload"].(map[string]interface{})["ok"])
}

func TestProcessSurvivesDeadLetterPublishFailure(t *testing.T) {
	h := newProcessor()
	h.publisher.err = errors.New("broker down")
	cmd := command(map[string]string{"a": "1"})

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	entry := logEntry(t, h.logs, "dead-letter publish failed")
	assert.NotEmpty(t, entry["event_id"])
	assert.Equal(t, "trace-1", entry["trace_id"])
	assert.Equal(t, "test.failed", entry["type"])
	assert.Equal(t, "publish_failed", entry["code"])
	failed := entry["event"].(map[string]interface{})
	assert.Equal(t, entry["event_id"], failed["id"])
	original := failed["payload"].(map[string]interface{})["original_event"].(map[string]interface{})
	assert.Equal(t, "account.created", original["type"])
	assert.Equal(t, "trace-1", original["trace_id"])
}

func TestProcessLogsUnencodableEventsWhenPublishFails(t *testing.T) {
	h := newProcessor()
	h.publisher.errByTopic = map[string]error{"results": errors.New("encoding failed")}
	h.dispatcher.Result = Reply("results", "key-1", events.NewEvent("account.credited", "test", map[string]float64{"amount": math.NaN()}).WithTraceID("trace-1"))
	cmd := command(nil)

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	entry := logEntry(t, h.logs, "result publish failed")
	assert.Equal(t, h.dispatcher.Result.Messages[0].Event.ID, entry["event_id"])
	assert.Equal(t, "account.credited", entry["type"])
	assert.Contains(t, entry["event"], "NaN")
	assert.Len(t, h.publisher.byTopic("dlq"), 1)
}

func TestProcessRetriesIdempotencyStoreThenFails(t *testing.T) {
	h := newProcessor()
	h.store.errs = []error{errors.New("db down"), errors.New("db down"), errors.New("db down"), errors.New("db down")}
	cmd := command(map[string]string{"a": "1"})

	err := h.processor.Process(context.Background(), []byte("k"), encoded(cmd))

	assert.EqualError(t, err, "db down")
	assert.Equal(t, 0, h.dispatcher.Calls)
	assert.Empty(t, h.publisher.published)
}

func TestProcessStopsRetryingWhenContextCancelled(t *testing.T) {
	h := newProcessor(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	h.dispatcher.Errs = []error{errors.New("timeout")}
	h.dispatcher.OnCall = cancel
	cmd := command(map[string]string{"a": "1"})

	err := h.processor.Process(ctx, []byte("k"), encoded(cmd))

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, h.dispatcher.Calls)
}

func TestProcessRetriesMarkProcessedTransientThenSucceeds(t *testing.T) {
	h := newProcessor()
	h.store.errs = []error{errors.New("db down"), nil}
	cmd := command(map[string]string{"a": "1"})

	err := h.processor.Process(context.Background(), []byte("k"), encoded(cmd))

	assert.NoError(t, err)
	assert.Equal(t, 1, h.dispatcher.Calls)
	assert.Len(t, h.publisher.byTopic("results"), 1)
}

func TestProcessPublishesEveryMessageAndDeadLettersFailures(t *testing.T) {
	h := newProcessor()
	first := events.NewEvent("first", "test", map[string]string{"n": "1"}).WithTraceID("trace-1")
	second := events.NewEvent("second", "test", map[string]string{"n": "2"}).WithTraceID("trace-1")
	third := events.NewEvent("third", "test", map[string]string{"n": "3"}).WithTraceID("trace-1")
	h.dispatcher.Result = Result{Messages: []Message{
		{Topic: "topic-a", Key: "key-a", Event: first},
		{Topic: "topic-b", Key: "key-b", Event: second},
		{Topic: "topic-a", Key: "key-a", Event: third},
	}}
	h.publisher.errByTopic = map[string]error{"topic-a": errors.New("broker down")}
	cmd := command(map[string]string{"a": "1"})

	err := h.processor.Process(context.Background(), []byte("k"), encoded(cmd))

	assert.NoError(t, err)
	assert.Empty(t, h.publisher.byTopic("topic-a"))
	published := h.publisher.byTopic("topic-b")
	assert.Len(t, published, 1)
	assert.Equal(t, second.ID, published[0].Event.ID)
	dlq := h.publisher.byTopic("dlq")
	assert.Len(t, dlq, 2)
	for i, event := range []*events.Event{first, third} {
		payload := dlq[i].Event.Payload.(events.ErrorPayload)
		assert.Equal(t, "publish_failed", payload.ErrorCode, i)
		assert.Equal(t, "broker down", payload.ErrorMessage, i)
		assert.Equal(t, 0, payload.Retries, i)
		assert.Equal(t, event.ID, payload.OriginalEvent.ID, i)
		assert.Equal(t, event.Type, payload.OriginalEvent.Type, i)
		assert.Equal(t, "trace-1", dlq[i].Event.TraceID, i)
		assert.Equal(t, "k", dlq[i].Key, i)
	}
}

func TestDecodePayload(t *testing.T) {
	type payload struct {
		OK string `json:"ok"`
	}
	var dst payload
	cmd := events.NewEvent("account.create", "test", map[string]string{"ok": "1"}).WithTraceID("trace-1")
	assert.NoError(t, DecodePayload(cmd, &dst))
	assert.Equal(t, "1", dst.OK)

	cmd = events.NewEvent("account.create", "test", "not an object").WithTraceID("trace-1")
	assert.ErrorIs(t, DecodePayload(cmd, &dst), ErrBadPayload)

	cmd = events.NewEvent("account.create", "test", make(chan int)).WithTraceID("trace-1")
	assert.ErrorIs(t, DecodePayload(cmd, &dst), ErrBadPayload)
}

const (
	remoteTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	remoteSpanID  = "00f067aa0ba902b7"
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

func remoteContext(t *testing.T) context.Context {
	t.Helper()
	traceID, err := trace.TraceIDFromHex(remoteTraceID)
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex(remoteSpanID)
	require.NoError(t, err)
	return trace.ContextWithRemoteSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	}))
}

func newMeasuredProcessor(backoff ...time.Duration) (*processorHarness, *metrics.Metrics) {
	h := newProcessor(backoff...)
	m := metrics.New("test")
	cfg := h.processor.cfg
	cfg.Metrics = m
	h.processor = NewProcessor(h.dispatcher, h.store, h.publisher, cfg, h.processor.log)
	return h, m
}

func processedCount(m *metrics.Metrics, eventType, outcome string) float64 {
	return testutil.ToFloat64(m.CounterVec(processedTotalName, processedTotalHelp, "type", "outcome").WithLabelValues(eventType, outcome))
}

func retriesCount(m *metrics.Metrics, eventType string) float64 {
	return testutil.ToFloat64(m.CounterVec(retriesTotalName, retriesTotalHelp, "type").WithLabelValues(eventType))
}

func durationSamples(t *testing.T, m *metrics.Metrics, eventType string) uint64 {
	t.Helper()
	families, err := m.Registry().Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != processingDurationName {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == "type" && label.GetValue() == eventType {
					return metric.GetHistogram().GetSampleCount()
				}
			}
		}
	}
	return 0
}

func onlySpan(t *testing.T, recorder *tracetest.SpanRecorder) sdktrace.ReadOnlySpan {
	t.Helper()
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	return spans[0]
}

func spanAttributes(span sdktrace.ReadOnlySpan) map[attribute.Key]string {
	attrs := map[attribute.Key]string{}
	for _, kv := range span.Attributes() {
		attrs[kv.Key] = kv.Value.Emit()
	}
	return attrs
}

func assertFailedSpan(t *testing.T, span sdktrace.ReadOnlySpan, description string) {
	t.Helper()
	assert.Equal(t, codes.Error, span.Status().Code)
	assert.Contains(t, span.Status().Description, description)
	require.Len(t, span.Events(), 1)
	assert.Equal(t, "exception", span.Events()[0].Name)
}

func TestProcessStartsConsumerSpanFromIncomingContext(t *testing.T) {
	recorder := useRecorder(t)
	h := newProcessor()
	cmd := command(map[string]string{"a": "1"})

	assert.NoError(t, h.processor.Process(remoteContext(t), []byte("k"), encoded(cmd)))

	span := onlySpan(t, recorder)
	assert.Equal(t, "process account.create", span.Name())
	assert.Equal(t, trace.SpanKindConsumer, span.SpanKind())
	assert.Equal(t, remoteTraceID, span.SpanContext().TraceID().String())
	assert.Equal(t, remoteSpanID, span.Parent().SpanID().String())
	assert.True(t, span.Parent().IsRemote())
	assert.Equal(t, codes.Unset, span.Status().Code)
	attrs := spanAttributes(span)
	assert.Equal(t, "kafka", attrs["messaging.system"])
	assert.Equal(t, cmd.ID, attrs["messaging.message.id"])
	assert.Equal(t, "process", attrs["messaging.operation.type"])

	require.Len(t, h.publisher.contexts, 1)
	published := trace.SpanContextFromContext(h.publisher.contexts[0])
	assert.Equal(t, remoteTraceID, published.TraceID().String())
	assert.Equal(t, span.SpanContext().SpanID(), published.SpanID())
	assert.Equal(t, "trace-1", h.publisher.byTopic("results")[0].Event.TraceID)
}

func TestProcessRecordsOKOutcome(t *testing.T) {
	h, m := newMeasuredProcessor()
	cmd := command(map[string]string{"a": "1"})

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	assert.Equal(t, float64(1), processedCount(m, "account.create", "ok"))
	assert.Equal(t, uint64(1), durationSamples(t, m, "account.create"))
	assert.Equal(t, float64(0), retriesCount(m, "account.create"))
}

func TestProcessRecordsDuplicateOutcome(t *testing.T) {
	recorder := useRecorder(t)
	h, m := newMeasuredProcessor()
	cmd := command(map[string]string{"a": "1"})

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))
	assert.NoError(t, h.processor.Process(remoteContext(t), []byte("k"), encoded(cmd)))

	assert.Equal(t, float64(1), processedCount(m, "account.create", "ok"))
	assert.Equal(t, float64(1), processedCount(m, "account.create", "duplicate"))
	assert.Equal(t, uint64(2), durationSamples(t, m, "account.create"))

	spans := recorder.Ended()
	require.Len(t, spans, 2)
	entry := logEntry(t, h.logs, "duplicate event skipped")
	assert.Equal(t, remoteTraceID, entry["otel_trace_id"])
	assert.Equal(t, spans[1].SpanContext().SpanID().String(), entry["otel_span_id"])
}

func TestProcessRecordsDeadLetteredOutcome(t *testing.T) {
	recorder := useRecorder(t)
	h, m := newMeasuredProcessor()
	h.dispatcher.Errs = []error{ErrUnknownCommand}
	cmd := command(map[string]string{"a": "1"})

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	assert.Equal(t, float64(1), processedCount(m, "account.create", "dead_lettered"))
	assert.Equal(t, float64(0), processedCount(m, "account.create", "ok"))
	assert.Equal(t, float64(0), retriesCount(m, "account.create"))
	assertFailedSpan(t, onlySpan(t, recorder), "unknown command")
}

func TestProcessRecordsInvalidEventsAsUnknown(t *testing.T) {
	recorder := useRecorder(t)
	h, m := newMeasuredProcessor()

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), []byte("{nope")))
	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), []byte(`{}`)))
	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), []byte(`{"id":"not-a-uuid","type":"account.create"}`)))

	assert.Equal(t, float64(2), processedCount(m, "unknown", "dead_lettered"))
	assert.Equal(t, float64(1), processedCount(m, "account.create", "dead_lettered"))
	assert.Len(t, h.publisher.byTopic("dlq"), 3)

	spans := recorder.Ended()
	require.Len(t, spans, 3)
	assert.Equal(t, "process unknown", spans[0].Name())
	assert.NotContains(t, spanAttributes(spans[0]), attribute.Key("messaging.message.id"))
	assertFailedSpan(t, spans[0], "invalid character")
	assert.Equal(t, "process unknown", spans[1].Name())
	assert.NotContains(t, spanAttributes(spans[1]), attribute.Key("messaging.message.id"))
	assert.Equal(t, "process account.create", spans[2].Name())
	assert.Equal(t, "not-a-uuid", spanAttributes(spans[2])["messaging.message.id"])
	assertFailedSpan(t, spans[2], "event id is not a uuid")
}

func TestProcessRecordsRetriesBeforeSuccess(t *testing.T) {
	h, m := newMeasuredProcessor()
	h.dispatcher.Errs = []error{errors.New("timeout"), errors.New("timeout"), nil}
	cmd := command(map[string]string{"a": "1"})

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	assert.Equal(t, float64(2), retriesCount(m, "account.create"))
	assert.Equal(t, float64(1), processedCount(m, "account.create", "ok"))
}

func TestProcessRecordsRetriesBeforeDeadLetter(t *testing.T) {
	h, m := newMeasuredProcessor()
	h.dispatcher.Errs = []error{errors.New("timeout"), errors.New("timeout"), errors.New("timeout"), errors.New("timeout")}
	cmd := command(map[string]string{"a": "1"})

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	assert.Equal(t, float64(3), retriesCount(m, "account.create"))
	assert.Equal(t, float64(1), processedCount(m, "account.create", "dead_lettered"))
}

func TestProcessRecordsResultPublishFailureAsOKWithFailedSpan(t *testing.T) {
	recorder := useRecorder(t)
	h, m := newMeasuredProcessor()
	h.publisher.errByTopic = map[string]error{"results": errors.New("broker down")}
	cmd := command(map[string]string{"a": "1"})

	assert.NoError(t, h.processor.Process(remoteContext(t), []byte("k"), encoded(cmd)))

	assert.Equal(t, float64(1), processedCount(m, "account.create", "ok"))
	assert.Equal(t, float64(0), processedCount(m, "account.create", "dead_lettered"))
	span := onlySpan(t, recorder)
	assertFailedSpan(t, span, "broker down")

	entry := logEntry(t, h.logs, "result publish failed")
	assert.Equal(t, remoteTraceID, entry["otel_trace_id"])
	assert.Equal(t, span.SpanContext().SpanID().String(), entry["otel_span_id"])
	for _, ctx := range h.publisher.contexts {
		assert.Equal(t, remoteTraceID, trace.SpanContextFromContext(ctx).TraceID().String())
	}
}

func TestProcessLogsPanicsWithTraceIDs(t *testing.T) {
	recorder := useRecorder(t)
	h := newProcessor()
	h.dispatcher.Panic = true
	cmd := command(map[string]string{"a": "1"})

	assert.NoError(t, h.processor.Process(remoteContext(t), []byte("k"), encoded(cmd)))

	span := onlySpan(t, recorder)
	assertFailedSpan(t, span, "handler panicked")
	entry := logEntry(t, h.logs, "handler panicked: boom")
	assert.Equal(t, remoteTraceID, entry["otel_trace_id"])
	assert.Equal(t, span.SpanContext().SpanID().String(), entry["otel_span_id"])
}

func TestProcessLogsWithoutTraceIDsWhenNoSpan(t *testing.T) {
	h := newProcessor()
	cmd := command(map[string]string{"a": "1"})

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))
	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	entry := logEntry(t, h.logs, "duplicate event skipped")
	assert.NotContains(t, entry, "otel_trace_id")
	assert.NotContains(t, entry, "otel_span_id")
}

func TestProcessRecordsNothingWhenContextCancelled(t *testing.T) {
	recorder := useRecorder(t)
	h, m := newMeasuredProcessor(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	h.dispatcher.Errs = []error{errors.New("timeout")}
	h.dispatcher.OnCall = cancel
	cmd := command(map[string]string{"a": "1"})

	assert.ErrorIs(t, h.processor.Process(ctx, []byte("k"), encoded(cmd)), context.Canceled)

	assert.Equal(t, 0, testutil.CollectAndCount(m.Registry(), processedTotalName))
	assert.Equal(t, 0, testutil.CollectAndCount(m.Registry(), processingDurationName))
	assert.Equal(t, 0, testutil.CollectAndCount(m.Registry(), retriesTotalName))
	assertFailedSpan(t, onlySpan(t, recorder), "context canceled")
}

func TestProcessRecordsNothingWhenStoreFails(t *testing.T) {
	h, m := newMeasuredProcessor()
	h.store.errs = []error{errors.New("db down"), errors.New("db down"), errors.New("db down"), errors.New("db down")}
	cmd := command(map[string]string{"a": "1"})

	assert.EqualError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)), "db down")

	assert.Equal(t, 0, testutil.CollectAndCount(m.Registry(), processedTotalName))
}
