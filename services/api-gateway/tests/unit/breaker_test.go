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
	"github.com/sony/gobreaker/v2"
	"github.com/stretchr/testify/assert"
)

func breakerConfig(threshold uint32, timeout time.Duration) contracts.KafkaConfig {
	return contracts.KafkaConfig{BreakerThreshold: threshold, BreakerTimeout: timeout}
}

func TestBreakerPassesThroughOnSuccess(t *testing.T) {
	pub := &tests.FakePublisher{}
	b := messaging.NewBreaker(pub, breakerConfig(2, time.Minute))
	ev := events.NewAccountCommand(events.EventTypes.CreateAccount, nil)

	err := b.Publish(context.Background(), events.Topics.AccountCommands, "k", ev)

	assert.NoError(t, err)
	assert.Len(t, pub.Published, 1)
	assert.Equal(t, "k", pub.Last().Key)
}

func TestBreakerReturnsInnerErrorWhileClosed(t *testing.T) {
	inner := apperrors.ServiceUnavailable("PUBLISH_FAILED", "boom")
	pub := &tests.FakePublisher{Err: inner}
	b := messaging.NewBreaker(pub, breakerConfig(2, time.Minute))
	ev := events.NewAccountCommand(events.EventTypes.CreateAccount, nil)

	err := b.Publish(context.Background(), events.Topics.AccountCommands, "k", ev)

	assert.Same(t, inner, err)
}

func TestBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	pub := &tests.FakePublisher{Err: errors.New("down")}
	b := messaging.NewBreaker(pub, breakerConfig(2, time.Minute))
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
	b := messaging.NewBreaker(pub, breakerConfig(1, 20*time.Millisecond))
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
