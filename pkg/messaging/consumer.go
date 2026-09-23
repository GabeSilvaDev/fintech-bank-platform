package messaging

import (
	"context"
	"errors"
	"time"

	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/fintech-bank-platform/pkg/tracing"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/segmentio/kafka-go"
)

const (
	DefaultDrainTimeout = 30 * time.Second
	consumerLagName     = "kafka_consumer_lag"
	consumerLagHelp     = "Messages the consumer group is behind the partition high watermark"
)

type Reader interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

type ConsumerConfig struct {
	Brokers      []string
	GroupID      string
	Topic        string
	DrainTimeout time.Duration
	StartOffset  int64
	Metrics      *metrics.Metrics
}

type Handler func(ctx context.Context, msg kafka.Message) error

type statsReader interface {
	Stats() kafka.ReaderStats
}

type Consumer struct {
	reader       Reader
	drainTimeout time.Duration
	lag          prometheus.Gauge
}

func NewConsumer(cfg ConsumerConfig) *Consumer {
	startOffset := cfg.StartOffset
	if startOffset == 0 {
		startOffset = kafka.FirstOffset
	}
	return NewConsumerWithReader(kafka.NewReader(kafka.ReaderConfig{
		Brokers:     cfg.Brokers,
		GroupID:     cfg.GroupID,
		Topic:       cfg.Topic,
		StartOffset: startOffset,
		MinBytes:    1,
		MaxBytes:    1 << 20,
	}), cfg.DrainTimeout).WithMetrics(cfg.Metrics, cfg.Topic, cfg.GroupID)
}

func NewConsumerWithReader(r Reader, drainTimeout time.Duration) *Consumer {
	if drainTimeout <= 0 {
		drainTimeout = DefaultDrainTimeout
	}
	return &Consumer{reader: r, drainTimeout: drainTimeout}
}

func (c *Consumer) WithMetrics(m *metrics.Metrics, topic, group string) *Consumer {
	c.lag = m.GaugeVec(consumerLagName, consumerLagHelp, "topic", "group").WithLabelValues(topic, group)
	return c
}

func (c *Consumer) DrainTimeout() time.Duration {
	return c.drainTimeout
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
		c.recordLag()

		if err := c.process(ctx, msg, handle); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

func (c *Consumer) process(ctx context.Context, msg kafka.Message, handle Handler) error {
	workCtx, cancelWork := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelWork()

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-done:
			return
		case <-ctx.Done():
		}
		timer := time.NewTimer(c.drainTimeout)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			cancelWork()
		}
	}()

	if err := handle(tracing.Extract(workCtx, msg.Headers), msg); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	return c.reader.CommitMessages(workCtx, msg)
}

func (c *Consumer) recordLag() {
	stats, ok := c.reader.(statsReader)
	if !ok || c.lag == nil {
		return
	}
	c.lag.Set(float64(stats.Stats().Lag))
}

func (c *Consumer) Close() error {
	return c.reader.Close()
}

func RunWithRestart(ctx context.Context, newConsumer func() *Consumer, handle Handler, backoff []time.Duration, onError func(error)) {
	for attempt := 0; ; attempt++ {
		consumer := newConsumer()
		err := consumer.Run(ctx, handle)
		_ = consumer.Close()
		if ctx.Err() != nil || err == nil {
			return
		}
		onError(err)
		if !wait(ctx, restartDelay(backoff, attempt)) {
			return
		}
	}
}

func restartDelay(backoff []time.Duration, attempt int) time.Duration {
	if len(backoff) == 0 {
		return 0
	}
	return backoff[min(attempt, len(backoff)-1)]
}

func wait(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
