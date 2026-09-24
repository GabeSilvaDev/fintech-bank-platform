package messaging

import (
	"context"
	"errors"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/sony/gobreaker/v2"
)

const (
	CircuitBreakerStateName = "circuit_breaker_state"
	CircuitBreakerStateHelp = "Circuit breaker state (0=closed, 1=half-open, 2=open)"
)

type Breaker struct {
	next    contracts.Publisher
	breaker *gobreaker.CircuitBreaker[struct{}]
}

func NewBreaker(next contracts.Publisher, cfg contracts.KafkaConfig, m *metrics.Metrics) *Breaker {
	settings := gobreaker.Settings{
		Name:    "kafka-publisher",
		Timeout: cfg.BreakerTimeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= cfg.BreakerThreshold
		},
		IsSuccessful: func(err error) bool {
			return err == nil || errors.Is(err, context.Canceled)
		},
	}

	breaker := gobreaker.NewCircuitBreaker[struct{}](settings)
	m.GaugeFunc(CircuitBreakerStateName, CircuitBreakerStateHelp, func() float64 {
		return breakerStateValue(breaker.State())
	})

	return &Breaker{
		next:    next,
		breaker: breaker,
	}
}

func breakerStateValue(state gobreaker.State) float64 {
	switch state {
	case gobreaker.StateHalfOpen:
		return 1
	case gobreaker.StateOpen:
		return 2
	default:
		return 0
	}
}

func (b *Breaker) Publish(ctx context.Context, topic, key string, event *events.Event) error {
	_, err := b.breaker.Execute(func() (struct{}, error) {
		return struct{}{}, b.next.Publish(ctx, topic, key, event)
	})

	if err == gobreaker.ErrOpenState || err == gobreaker.ErrTooManyRequests {
		return apperrors.ServiceUnavailable("PUBLISH_FAILED", "message broker is unavailable").Wrap(err)
	}

	return err
}
