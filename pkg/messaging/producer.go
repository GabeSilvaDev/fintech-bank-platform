package messaging

import (
	"context"
	"time"

	"github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/fintech-bank-platform/pkg/tracing"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	publishedTotalName = "messages_published_total"
	publishedTotalHelp = "Total number of Kafka messages published"
	outcomeOK          = "ok"
	outcomeError       = "error"
)

type Writer interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

type ProducerConfig struct {
	Brokers        []string
	WriteTimeout   time.Duration
	BatchTimeout   time.Duration
	PublishTimeout time.Duration
	MaxAttempts    int
}

type Producer struct {
	writer    Writer
	timeout   time.Duration
	published *prometheus.CounterVec
}

func NewProducer(cfg ProducerConfig) *Producer {
	return NewProducerWithWriter(&kafka.Writer{
		Addr:                   kafka.TCP(cfg.Brokers...),
		Balancer:               &kafka.Hash{},
		MaxAttempts:            cfg.MaxAttempts,
		WriteTimeout:           cfg.WriteTimeout,
		BatchTimeout:           cfg.BatchTimeout,
		RequiredAcks:           kafka.RequireAll,
		AllowAutoTopicCreation: true,
	}, cfg.PublishTimeout)
}

func NewProducerWithWriter(w Writer, publishTimeout time.Duration) *Producer {
	return (&Producer{writer: w, timeout: publishTimeout}).WithMetrics(nil)
}

func (p *Producer) WithMetrics(m *metrics.Metrics) *Producer {
	p.published = m.CounterVec(publishedTotalName, publishedTotalHelp, "topic", "outcome")
	return p
}

func (p *Producer) Publish(ctx context.Context, topic, key string, event *events.Event) error {
	ctx, span := tracing.Tracer().Start(ctx, "publish "+topic,
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			semconv.MessagingSystemKafka,
			semconv.MessagingDestinationName(topic),
			semconv.MessagingOperationTypeSend,
		),
	)
	defer span.End()

	err := p.publish(ctx, topic, key, event)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		p.published.WithLabelValues(topic, outcomeError).Inc()
		return err
	}

	p.published.WithLabelValues(topic, outcomeOK).Inc()
	return nil
}

func (p *Producer) publish(ctx context.Context, topic, key string, event *events.Event) error {
	body, err := event.ToJSON()
	if err != nil {
		return errors.InternalServer("EVENT_ENCODING_FAILED", "could not encode event").Wrap(err)
	}

	msg := kafka.Message{
		Topic: topic,
		Key:   []byte(key),
		Value: body,
		Time:  event.Timestamp,
		Headers: tracing.Inject(ctx, []kafka.Header{
			{Key: "event_type", Value: []byte(event.Type)},
			{Key: "trace_id", Value: []byte(event.TraceID)},
		}),
	}

	if p.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.timeout)
		defer cancel()
	}

	if err := p.writer.WriteMessages(ctx, msg); err != nil {
		return errors.ServiceUnavailable("PUBLISH_FAILED", "could not publish command").Wrap(err)
	}

	return nil
}

func (p *Producer) Close() error {
	return p.writer.Close()
}
