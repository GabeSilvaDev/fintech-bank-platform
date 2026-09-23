package unit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/messaging"
	"github.com/fintech-bank-platform/api-gateway/tests"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/sony/gobreaker/v2"
	"github.com/stretchr/testify/assert"
)

func breakerStateValue(t *testing.T, m *metrics.Metrics) float64 {
	t.Helper()
	gauge := m.GaugeVec(messaging.CircuitBreakerStateName, messaging.CircuitBreakerStateHelp)
	return testutil.ToFloat64(gauge.WithLabelValues())
}

func breakerConfig(threshold uint32, timeout time.Duration) contracts.KafkaConfig {
	return contracts.KafkaConfig{BreakerThreshold: threshold, BreakerTimeout: timeout}
}

func TestBreakerPassesThroughOnSuccess(t *testing.T) {
	pub := &tests.FakePublisher{}
	b := messaging.NewBreaker(pub, breakerConfig(2, time.Minute), nil)
	ev := events.NewAccountCommand(events.EventTypes.CreateAccount, nil)

	err := b.Publish(context.Background(), events.Topics.AccountCommands, "k", ev)

	assert.NoError(t, err)
	assert.Len(t, pub.Published, 1)
	assert.Equal(t, "k", pub.Last().Key)
}

func TestBreakerReturnsInnerErrorWhileClosed(t *testing.T) {
	inner := apperrors.ServiceUnavailable("PUBLISH_FAILED", "boom")
	pub := &tests.FakePublisher{Err: inner}
	b := messaging.NewBreaker(pub, breakerConfig(2, time.Minute), nil)
	ev := events.NewAccountCommand(events.EventTypes.CreateAccount, nil)

	err := b.Publish(context.Background(), events.Topics.AccountCommands, "k", ev)

	assert.Same(t, inner, err)
}

func TestBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	pub := &tests.FakePublisher{Err: errors.New("down")}
	b := messaging.NewBreaker(pub, breakerConfig(2, time.Minute), nil)
	ev := events.NewAccountCommand(events.EventTypes.CreateAccount, nil)
	ctx := context.Background()

	_ = b.Publish(ctx, events.Topics.AccountCommands, "k", ev)
	_ = b.Publish(ctx, events.Topics.AccountCommands, "k", ev)
	pub.Err = nil
	err := b.Publish(ctx, events.Topics.AccountCommands, "k", ev)

	assert.ErrorIs(t, err, gobreaker.ErrOpenState)
	appErr, ok := apperrors.AsAppError(err)
	assert.True(t, ok)
	assert.Equal(t, "PUBLISH_FAILED", appErr.Code)
	assert.Empty(t, pub.Published)
}

func TestBreakerRecoversAfterTimeout(t *testing.T) {
	pub := &tests.FakePublisher{Err: errors.New("down")}
	b := messaging.NewBreaker(pub, breakerConfig(1, 20*time.Millisecond), nil)
	ev := events.NewAccountCommand(events.EventTypes.CreateAccount, nil)
	ctx := context.Background()

	_ = b.Publish(ctx, events.Topics.AccountCommands, "k", ev)
	assert.ErrorIs(t, b.Publish(ctx, events.Topics.AccountCommands, "k", ev), gobreaker.ErrOpenState)

	time.Sleep(40 * time.Millisecond)
	pub.Err = nil
	err := b.Publish(ctx, events.Topics.AccountCommands, "k", ev)

	assert.NoError(t, err)
	assert.Len(t, pub.Published, 1)
}

func TestBreakerInitializesGaugeToClosed(t *testing.T) {
	m := metrics.New("test-breaker-initial")
	pub := &tests.FakePublisher{}

	messaging.NewBreaker(pub, breakerConfig(2, time.Minute), m)

	assert.Equal(t, float64(0), breakerStateValue(t, m))
}

func TestBreakerGaugeReflectsOpenAndRecoveredStates(t *testing.T) {
	m := metrics.New("test-breaker-transitions")
	pub := &tests.FakePublisher{Err: errors.New("down")}
	b := messaging.NewBreaker(pub, breakerConfig(1, 20*time.Millisecond), m)
	ev := events.NewAccountCommand(events.EventTypes.CreateAccount, nil)
	ctx := context.Background()

	assert.Equal(t, float64(0), breakerStateValue(t, m))

	_ = b.Publish(ctx, events.Topics.AccountCommands, "k", ev)
	assert.Equal(t, float64(2), breakerStateValue(t, m))

	time.Sleep(40 * time.Millisecond)
	pub.Err = nil
	err := b.Publish(ctx, events.Topics.AccountCommands, "k", ev)

	assert.NoError(t, err)
	assert.Equal(t, float64(0), breakerStateValue(t, m))
}

func TestBreakerIgnoresCanceledContexts(t *testing.T) {
	pub := &tests.FakePublisher{Err: context.Canceled}
	b := messaging.NewBreaker(pub, breakerConfig(1, time.Minute), nil)
	ev := events.NewAccountCommand(events.EventTypes.CreateAccount, nil)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		assert.ErrorIs(t, b.Publish(ctx, events.Topics.AccountCommands, "k", ev), context.Canceled)
	}
	pub.Err = nil
	err := b.Publish(ctx, events.Topics.AccountCommands, "k", ev)

	assert.NoError(t, err)
	assert.Len(t, pub.Published, 1)
}
