package unit

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/handlers"
	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/tests"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/stretchr/testify/assert"
)

type scripted struct {
	Errs   []error
	Panic  bool
	Calls  int
	Cancel context.CancelFunc
	Result handlers.Result
	OnCall func()
}

func (s *scripted) Dispatch(_ context.Context, cmd *events.Event) (handlers.Result, error) {
	s.Calls++
	if s.OnCall != nil {
		s.OnCall()
	}
	if s.Panic {
		panic("boom")
	}
	if len(s.Errs) > 0 {
		err := s.Errs[0]
		s.Errs = s.Errs[1:]
		if err != nil {
			return handlers.Result{}, err
		}
	}
	if s.Result.Event == nil {
		s.Result = handlers.Result{Event: events.NewAccountEvent(events.EventTypes.AccountCreated, map[string]string{"ok": "1"}).WithTraceID(cmd.TraceID), Key: "key-1"}
	}
	return s.Result, nil
}

type processorHarness struct {
	dispatcher *scripted
	store      *tests.FakeStore
	publisher  *tests.FakePublisher
	logs       *bytes.Buffer
	processor  *handlers.Processor
}

func newProcessor(backoff ...time.Duration) *processorHarness {
	h := &processorHarness{dispatcher: &scripted{}, store: tests.NewFakeStore(), publisher: &tests.FakePublisher{}, logs: &bytes.Buffer{}}
	if backoff == nil {
		backoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	}
	log := logger.New(logger.Config{Level: "debug", Output: h.logs})
	h.processor = handlers.NewProcessor(h.dispatcher, h.store, h.publisher, backoff, log)
	return h
}

func encoded(cmd *events.Event) []byte {
	data, _ := cmd.ToJSON()
	return data
}

func TestProcessPublishesResult(t *testing.T) {
	h := newProcessor()
	cmd := command(events.EventTypes.CreateAccount, validCreate())

	err := h.processor.Process(context.Background(), []byte("k"), encoded(cmd))

	assert.NoError(t, err)
	assert.Equal(t, 1, h.dispatcher.Calls)
	published := h.publisher.ByTopic(events.Topics.AccountEvents)
	assert.Len(t, published, 1)
	assert.Equal(t, "key-1", published[0].Key)
	assert.Equal(t, "trace-1", published[0].Event.TraceID)
	assert.Empty(t, h.publisher.ByTopic(events.Topics.AccountDLQ))
}

func TestProcessSkipsDuplicates(t *testing.T) {
	h := newProcessor()
	cmd := command(events.EventTypes.CreateAccount, validCreate())

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))
	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	assert.Equal(t, 1, h.dispatcher.Calls)
	assert.Len(t, h.publisher.Published, 1)
	assert.Contains(t, h.logs.String(), "duplicate")
}

func TestProcessDeadLettersUndecodableMessages(t *testing.T) {
	h := newProcessor()

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), []byte("{nope")))
	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), []byte(`{"id":"not-a-uuid","type":"account.create"}`)))

	dlq := h.publisher.ByTopic(events.Topics.AccountDLQ)
	assert.Len(t, dlq, 2)
	assert.Equal(t, events.EventTypes.AccountCommandFailed, dlq[0].Event.Type)
	assert.Equal(t, "invalid_event", dlq[0].Event.Payload.(events.ErrorPayload).ErrorCode)
	assert.Nil(t, dlq[0].Event.Payload.(events.ErrorPayload).OriginalEvent)
	assert.NotNil(t, dlq[1].Event.Payload.(events.ErrorPayload).OriginalEvent)
	assert.Equal(t, 0, h.dispatcher.Calls)
}

func TestProcessDeadLettersUnprocessableWithoutRetry(t *testing.T) {
	cases := map[string]error{
		"unknown_command":   handlers.ErrUnknownCommand,
		"bad_payload":       handlers.ErrBadPayload,
		"account_not_found": models.ErrNotFound,
		"account_closed":    models.Invalid("account_closed", "closed"),
	}

	for code, dispatchErr := range cases {
		h := newProcessor()
		h.dispatcher.Errs = []error{dispatchErr}
		cmd := command(events.EventTypes.CreateAccount, validCreate())

		assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

		assert.Equal(t, 1, h.dispatcher.Calls, code)
		dlq := h.publisher.ByTopic(events.Topics.AccountDLQ)
		assert.Len(t, dlq, 1, code)
		payload := dlq[0].Event.Payload.(events.ErrorPayload)
		assert.Equal(t, code, payload.ErrorCode, code)
		assert.Equal(t, cmd.ID, payload.OriginalEvent.ID, code)
		assert.Equal(t, "trace-1", dlq[0].Event.TraceID, code)
		assert.Equal(t, "k", dlq[0].Key, code)
	}
}

func TestProcessRetriesTransientErrorsThenSucceeds(t *testing.T) {
	h := newProcessor()
	h.dispatcher.Errs = []error{errors.New("timeout"), errors.New("timeout"), errors.New("timeout"), nil}
	cmd := command(events.EventTypes.CreateAccount, validCreate())

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	assert.Equal(t, 4, h.dispatcher.Calls)
	assert.Len(t, h.publisher.ByTopic(events.Topics.AccountEvents), 1)
}

func TestProcessDeadLettersAfterRetriesExhausted(t *testing.T) {
	h := newProcessor()
	h.dispatcher.Errs = []error{errors.New("timeout"), errors.New("timeout"), errors.New("timeout"), errors.New("timeout")}
	cmd := command(events.EventTypes.CreateAccount, validCreate())

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	assert.Equal(t, 4, h.dispatcher.Calls)
	dlq := h.publisher.ByTopic(events.Topics.AccountDLQ)
	assert.Equal(t, "internal_error", dlq[0].Event.Payload.(events.ErrorPayload).ErrorCode)
}

func TestProcessTreatsConflictAsTransient(t *testing.T) {
	h := newProcessor()
	h.dispatcher.Errs = []error{models.ErrConflict, models.ErrConflict, models.ErrConflict, models.ErrConflict}
	cmd := command(events.EventTypes.CreditAccount, nil)

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	assert.Equal(t, 4, h.dispatcher.Calls)
	assert.Equal(t, "conflict", h.publisher.ByTopic(events.Topics.AccountDLQ)[0].Event.Payload.(events.ErrorPayload).ErrorCode)
}

func TestProcessRecoversFromPanics(t *testing.T) {
	h := newProcessor()
	h.dispatcher.Panic = true
	cmd := command(events.EventTypes.CreateAccount, validCreate())

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	assert.Equal(t, 1, h.dispatcher.Calls)
	assert.Equal(t, "panic", h.publisher.ByTopic(events.Topics.AccountDLQ)[0].Event.Payload.(events.ErrorPayload).ErrorCode)
	assert.Contains(t, h.logs.String(), "boom")
}

func TestProcessDeadLettersWhenResultPublishFails(t *testing.T) {
	h := newProcessor()
	h.publisher.ErrByTopic = map[string]error{events.Topics.AccountEvents: errors.New("broker down")}
	cmd := command(events.EventTypes.CreateAccount, validCreate())

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))

	dlq := h.publisher.ByTopic(events.Topics.AccountDLQ)
	assert.Len(t, dlq, 1)
	payload := dlq[0].Event.Payload.(events.ErrorPayload)
	assert.Equal(t, "publish_failed", payload.ErrorCode)
	assert.Equal(t, events.EventTypes.AccountCreated, payload.OriginalEvent.Type)
}

func TestProcessSurvivesDeadLetterPublishFailure(t *testing.T) {
	h := newProcessor()
	h.publisher.Err = errors.New("broker down")
	cmd := command(events.EventTypes.CreateAccount, validCreate())

	assert.NoError(t, h.processor.Process(context.Background(), []byte("k"), encoded(cmd)))
	assert.Contains(t, h.logs.String(), "dead-letter publish failed")
}

func TestProcessRetriesIdempotencyStoreThenFails(t *testing.T) {
	h := newProcessor()
	h.store.Errs = []error{errors.New("db down"), errors.New("db down"), errors.New("db down"), errors.New("db down")}
	cmd := command(events.EventTypes.CreateAccount, validCreate())

	err := h.processor.Process(context.Background(), []byte("k"), encoded(cmd))

	assert.EqualError(t, err, "db down")
	assert.Equal(t, 0, h.dispatcher.Calls)
	assert.Empty(t, h.publisher.Published)
}

func TestProcessStopsRetryingWhenContextCancelled(t *testing.T) {
	h := newProcessor(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	h.dispatcher.Errs = []error{errors.New("timeout")}
	h.dispatcher.OnCall = cancel
	cmd := command(events.EventTypes.CreateAccount, validCreate())

	err := h.processor.Process(ctx, []byte("k"), encoded(cmd))

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, h.dispatcher.Calls)
}

func TestProcessRetriesMarkProcessedTransientThenSucceeds(t *testing.T) {
	h := newProcessor()
	h.store.Errs = []error{errors.New("db down"), nil}
	cmd := command(events.EventTypes.CreateAccount, validCreate())

	err := h.processor.Process(context.Background(), []byte("k"), encoded(cmd))

	assert.NoError(t, err)
	assert.Equal(t, 1, h.dispatcher.Calls)
	assert.Len(t, h.publisher.ByTopic(events.Topics.AccountEvents), 1)
}
