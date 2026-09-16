package messaging

import (
	"context"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/segmentio/kafka-go"
)

type Writer interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

type Producer struct {
	writer  Writer
	timeout time.Duration
}

func NewProducer(cfg contracts.KafkaConfig) *Producer {
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
	return &Producer{writer: w, timeout: publishTimeout}
}

func (p *Producer) Publish(ctx context.Context, topic, key string, event *events.Event) error {
	body, err := event.ToJSON()
	if err != nil {
		return errors.InternalServer("EVENT_ENCODING_FAILED", "could not encode event").Wrap(err)
	}

	msg := kafka.Message{
		Topic: topic,
		Key:   []byte(key),
		Value: body,
		Time:  event.Timestamp,
		Headers: []kafka.Header{
			{Key: "event_type", Value: []byte(event.Type)},
			{Key: "trace_id", Value: []byte(event.TraceID)},
		},
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
