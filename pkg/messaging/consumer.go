package messaging

import (
	"context"
	"errors"

	"github.com/segmentio/kafka-go"
)

type Reader interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

type ConsumerConfig struct {
	Brokers []string
	GroupID string
	Topic   string
}

type Handler func(ctx context.Context, msg kafka.Message) error

type Consumer struct {
	reader Reader
}

func NewConsumer(cfg ConsumerConfig) *Consumer {
	return NewConsumerWithReader(kafka.NewReader(kafka.ReaderConfig{
		Brokers:     cfg.Brokers,
		GroupID:     cfg.GroupID,
		Topic:       cfg.Topic,
		StartOffset: kafka.FirstOffset,
		MinBytes:    1,
		MaxBytes:    1 << 20,
	}))
}

func NewConsumerWithReader(r Reader) *Consumer {
	return &Consumer{reader: r}
}

func (c *Consumer) Run(ctx context.Context, handle Handler) error {
	for {
		msg, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return err
		}

		if err := handle(ctx, msg); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}

		if err := c.reader.CommitMessages(ctx, msg); err != nil {
			return err
		}
	}
}

func (c *Consumer) Close() error {
	return c.reader.Close()
}
