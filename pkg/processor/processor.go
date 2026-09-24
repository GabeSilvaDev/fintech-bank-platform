package processor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/fintech-bank-platform/pkg/tracing"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	processedTotalName     = "messages_processed_total"
	processedTotalHelp     = "Total number of Kafka messages processed by outcome"
	processingDurationName = "message_processing_duration_seconds"
	processingDurationHelp = "Kafka message processing duration in seconds"
	retriesTotalName       = "message_retries_total"
	retriesTotalHelp       = "Total number of dispatch retries for Kafka messages"
	unknownType            = "unknown"
	outcomeOK              = "ok"
	outcomeDuplicate       = "duplicate"
	outcomeDeadLettered    = "dead_lettered"
	originalTypeKey        = attribute.Key("messaging.message.type_original")
)

var (
	ErrUnknownCommand = errors.New("unknown command")
	ErrBadPayload     = errors.New("bad payload")
	ErrPanic          = errors.New("handler panicked")
)

type Message struct {
	Topic string
	Key   string
	Event *events.Event
}

type Result struct {
	Messages []Message
}

func Reply(topic, key string, event *events.Event) Result {
	return Result{Messages: []Message{{Topic: topic, Key: key, Event: event}}}
}

type Dispatcher interface {
	Dispatch(ctx context.Context, cmd *events.Event) (Result, error)
}

type Store interface {
	MarkProcessed(ctx context.Context, eventID uuid.UUID) (bool, error)
}

type Publisher interface {
	Publish(ctx context.Context, topic, key string, event *events.Event) error
}

type Config struct {
	Source          string
	FailedEventType string
	DLQTopic        string
	Backoff         []time.Duration
	Metrics         *metrics.Metrics
}

type Processor struct {
	dispatcher Dispatcher
	store      Store
	publisher  Publisher
	cfg        Config
	log        *logger.Logger
	processed  *prometheus.CounterVec
	duration   *prometheus.HistogramVec
	retries    *prometheus.CounterVec
}

type outcome struct {
	name     string
	attempts int
	cause    error
	unknown  bool
}

func NewProcessor(dispatcher Dispatcher, store Store, publisher Publisher, cfg Config, log *logger.Logger) *Processor {
	return &Processor{
		dispatcher: dispatcher,
		store:      store,
		publisher:  publisher,
		cfg:        cfg,
		log:        log,
		processed:  cfg.Metrics.CounterVec(processedTotalName, processedTotalHelp, "type", "outcome"),
		duration:   cfg.Metrics.HistogramVec(processingDurationName, processingDurationHelp, prometheus.DefBuckets, "type"),
		retries:    cfg.Metrics.CounterVec(retriesTotalName, retriesTotalHelp, "type"),
	}
}

func (p *Processor) Process(ctx context.Context, key, value []byte) error {
	start := time.Now()
	cmd, decodeErr := events.FromJSON(value)
	eventType := typeLabel(cmd)

	attributes := []attribute.KeyValue{semconv.MessagingSystemKafka, semconv.MessagingOperationTypeProcess}
	if cmd != nil && cmd.ID != "" {
		attributes = append(attributes, semconv.MessagingMessageID(cmd.ID))
	}
	ctx, span := tracing.Tracer().Start(ctx, "process "+eventType,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attributes...),
	)
	defer span.End()

	if eventType == unknownType {
		recordOriginalType(span, cmd)
	}

	result, err := p.process(ctx, key, cmd, decodeErr)
	if err != nil {
		markFailed(span, err)
		return err
	}
	if result.cause != nil {
		markFailed(span, result.cause)
	}
	if result.unknown && eventType != unknownType {
		eventType = unknownType
		span.SetName("process " + unknownType)
		recordOriginalType(span, cmd)
	}

	p.processed.WithLabelValues(eventType, result.name).Inc()
	p.duration.WithLabelValues(eventType).Observe(time.Since(start).Seconds())
	if result.attempts > 1 {
		p.retries.WithLabelValues(eventType).Add(float64(result.attempts - 1))
	}
	return nil
}

func (p *Processor) process(ctx context.Context, key []byte, cmd *events.Event, decodeErr error) (outcome, error) {
	if decodeErr != nil {
		p.deadLetter(ctx, key, nil, "invalid_event", decodeErr.Error(), 0)
		return outcome{name: outcomeDeadLettered, cause: decodeErr}, nil
	}
	eventID, err := uuid.Parse(cmd.ID)
	if err != nil {
		p.deadLetter(ctx, key, cmd, "invalid_event", "event id is not a uuid", 0)
		return outcome{name: outcomeDeadLettered, cause: fmt.Errorf("event id is not a uuid: %w", err)}, nil
	}

	var first bool
	if _, err := p.retry(ctx, func() error {
		var markErr error
		first, markErr = p.store.MarkProcessed(ctx, eventID)
		return markErr
	}); err != nil {
		return outcome{}, err
	}
	if !first {
		withTrace(ctx, p.log.Info()).Str("event_id", cmd.ID).Str("type", cmd.Type).Msg("duplicate event skipped")
		return outcome{name: outcomeDuplicate}, nil
	}

	var res Result
	attempts, err := p.retry(ctx, func() error {
		var dispatchErr error
		res, dispatchErr = p.dispatch(ctx, cmd)
		return dispatchErr
	})
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return outcome{}, err
	}
	if err != nil {
		p.deadLetter(ctx, key, cmd, errorCode(err), err.Error(), attempts)
		return outcome{name: outcomeDeadLettered, attempts: attempts, cause: err, unknown: errors.Is(err, ErrUnknownCommand)}, nil
	}

	var publishErr error
	for _, msg := range res.Messages {
		if err := p.publisher.Publish(ctx, msg.Topic, msg.Key, msg.Event); err != nil {
			p.logUndeliverable(ctx, err, msg.Event, "publish_failed", "result publish failed")
			p.deadLetter(ctx, key, msg.Event, "publish_failed", err.Error(), 0)
			publishErr = err
		}
	}
	return outcome{name: outcomeOK, attempts: attempts, cause: publishErr}, nil
}

func DecodePayload(cmd *events.Event, dst interface{}) error {
	raw, err := json.Marshal(cmd.Payload)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadPayload, err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("%w: %v", ErrBadPayload, err)
	}
	return nil
}

func (p *Processor) dispatch(ctx context.Context, cmd *events.Event) (res Result, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			withTrace(ctx, p.log.Error()).Str("event_id", cmd.ID).Str("stack", string(debug.Stack())).Msgf("handler panicked: %v", recovered)
			err = fmt.Errorf("%w: %v", ErrPanic, recovered)
		}
	}()
	return p.dispatcher.Dispatch(ctx, cmd)
}

func (p *Processor) retry(ctx context.Context, op func() error) (int, error) {
	for attempt := 1; ; attempt++ {
		err := op()
		if err == nil || !isTransient(err) || attempt > len(p.cfg.Backoff) {
			return attempt, err
		}
		if !sleep(ctx, p.cfg.Backoff[attempt-1]) {
			return attempt, ctx.Err()
		}
	}
}

func (p *Processor) deadLetter(ctx context.Context, key []byte, original *events.Event, code, message string, retries int) {
	failed := events.NewEvent(p.cfg.FailedEventType, p.cfg.Source, events.ErrorPayload{
		OriginalEvent: original,
		ErrorCode:     code,
		ErrorMessage:  message,
		Retries:       retries,
	})
	if original != nil {
		failed.WithTraceID(original.TraceID)
	}
	if err := p.publisher.Publish(ctx, p.cfg.DLQTopic, string(key), failed); err != nil {
		p.logUndeliverable(ctx, err, failed, code, "dead-letter publish failed")
	}
}

func (p *Processor) logUndeliverable(ctx context.Context, err error, event *events.Event, code, message string) {
	entry := withTrace(ctx, p.log.Error()).Err(err).Str("event_id", event.ID).Str("trace_id", event.TraceID).Str("type", event.Type).Str("code", code)
	if raw, encodeErr := event.ToJSON(); encodeErr == nil {
		entry = entry.RawJSON("event", raw)
	} else {
		entry = entry.Interface("event", event)
	}
	entry.Msg(message)
}

func typeLabel(event *events.Event) string {
	if event == nil || event.Type == "" || uuid.Validate(event.ID) != nil {
		return unknownType
	}
	return event.Type
}

func recordOriginalType(span trace.Span, event *events.Event) {
	if event != nil && event.Type != "" {
		span.SetAttributes(originalTypeKey.String(event.Type))
	}
}

func markFailed(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

func withTrace(ctx context.Context, entry *zerolog.Event) *zerolog.Event {
	if traceID, spanID, ok := tracing.IDs(ctx); ok {
		return entry.Str("otel_trace_id", traceID).Str("otel_span_id", spanID)
	}
	return entry
}

func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func isTransient(err error) bool {
	return !domain.IsInvalid(err) &&
		!errors.Is(err, domain.ErrNotFound) &&
		!errors.Is(err, domain.ErrAmbiguousWrite) &&
		!errors.Is(err, ErrUnknownCommand) &&
		!errors.Is(err, ErrBadPayload) &&
		!errors.Is(err, ErrPanic)
}

func errorCode(err error) string {
	switch {
	case domain.IsInvalid(err):
		return domain.InvalidCode(err)
	case errors.Is(err, domain.ErrNotFound):
		return "not_found"
	case errors.Is(err, domain.ErrAmbiguousWrite):
		return "ambiguous_write"
	case errors.Is(err, ErrUnknownCommand):
		return "unknown_command"
	case errors.Is(err, ErrBadPayload):
		return "bad_payload"
	case errors.Is(err, ErrPanic):
		return "panic"
	case errors.Is(err, domain.ErrConflict):
		return "conflict"
	}
	return "internal_error"
}
