package messaging

import (
	"context"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/sony/gobreaker/v2"
)

type Breaker struct {
	next    contracts.Publisher
	breaker *gobreaker.CircuitBreaker[struct{}]
}

func NewBreaker(next contracts.Publisher, cfg contracts.KafkaConfig) *Breaker {
	settings := gobreaker.Settings{
		Name:    "kafka-publisher",
		Timeout: cfg.BreakerTimeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= cfg.BreakerThreshold
		},
	}

	return &Breaker{
		next:    next,
		breaker: gobreaker.NewCircuitBreaker[struct{}](settings),
	}
}

func (b *Breaker) Publish(ctx context.Context, topic, key string, event *events.Event) error {
	_, err := b.breaker.Execute(func() (struct{}, error) {
		return struct{}{}, b.next.Publish(ctx, topic, key, event)
	})

	if err == gobreaker.ErrOpenState || err == gobreaker.ErrTooManyRequests {
		return errors.ServiceUnavailable("PUBLISH_FAILED", "message broker is unavailable").Wrap(err)
	}

	return err
}
