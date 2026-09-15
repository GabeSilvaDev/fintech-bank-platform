package unit

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/fintech-bank-platform/api-gateway/internal/contracts"
	"github.com/fintech-bank-platform/api-gateway/internal/infrastructure/messaging"
	"github.com/fintech-bank-platform/api-gateway/tests"
	apperrors "github.com/fintech-bank-platform/pkg/errors"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
)

func TestProducerPublishWritesMessage(t *testing.T) {
	w := &tests.FakeWriter{}
	p := messaging.NewProducerWithWriter(w)
	ev := events.NewAccountCommand(events.EventTypes.CreateAccount, map[string]string{"user_id": "u1"}).WithTraceID("trace-1")

	err := p.Publish(context.Background(), events.Topics.AccountCommands, "u1", ev)

	assert.NoError(t, err)
	assert.Len(t, w.Messages, 1)

	msg := w.Messages[0]
	assert.Equal(t, events.Topics.AccountCommands, msg.Topic)
	assert.Equal(t, "u1", string(msg.Key))
	assert.Equal(t, ev.Timestamp, msg.Time)
	assert.Equal(t, []kafka.Header{
		{Key: "event_type", Value: []byte(events.EventTypes.CreateAccount)},
		{Key: "trace_id", Value: []byte("trace-1")},
	}, msg.Headers)

	decoded, err := events.FromJSON(msg.Value)
	assert.NoError(t, err)
	assert.Equal(t, ev.ID, decoded.ID)
	assert.Equal(t, events.EventTypes.CreateAccount, decoded.Type)
	assert.Equal(t, "trace-1", decoded.TraceID)
}

func TestProducerPublishReturnsServiceUnavailableOnWriteError(t *testing.T) {
	w := &tests.FakeWriter{Err: errors.New("broker down")}
	p := messaging.NewProducerWithWriter(w)
	ev := events.NewAccountCommand(events.EventTypes.CreateAccount, nil)

	err := p.Publish(context.Background(), events.Topics.AccountCommands, "k", ev)

	appErr, ok := apperrors.AsAppError(err)
	assert.True(t, ok)
	assert.Equal(t, "PUBLISH_FAILED", appErr.Code)
	assert.Equal(t, http.StatusServiceUnavailable, appErr.HTTPStatus)
	assert.ErrorContains(t, err, "broker down")
}

func TestProducerPublishReturnsInternalErrorWhenEventCannotBeEncoded(t *testing.T) {
	w := &tests.FakeWriter{}
	p := messaging.NewProducerWithWriter(w)
	ev := events.NewAccountCommand(events.EventTypes.CreateAccount, make(chan int))

	err := p.Publish(context.Background(), events.Topics.AccountCommands, "k", ev)

	appErr, ok := apperrors.AsAppError(err)
	assert.True(t, ok)
	assert.Equal(t, "EVENT_ENCODING_FAILED", appErr.Code)
	assert.Equal(t, http.StatusInternalServerError, appErr.HTTPStatus)
	assert.Empty(t, w.Messages)
}

func TestProducerCloseClosesWriter(t *testing.T) {
	w := &tests.FakeWriter{}
	p := messaging.NewProducerWithWriter(w)

	assert.NoError(t, p.Close())
	assert.True(t, w.Closed)
}

func TestNewProducerBuildsKafkaWriter(t *testing.T) {
	p := messaging.NewProducer(contracts.KafkaConfig{
		Brokers:      []string{"localhost:9092"},
		WriteTimeout: time.Second,
		MaxAttempts:  2,
	})

	assert.NotNil(t, p)
	assert.NoError(t, p.Close())
}
