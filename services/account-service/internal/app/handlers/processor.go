package handlers

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/internal/contracts"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/google/uuid"
)

var ErrPanic = errors.New("handler panicked")

type Processor struct {
	dispatcher CommandDispatcher
	store      contracts.ProcessedEventStore
	publisher  contracts.Publisher
	backoff    []time.Duration
	log        *logger.Logger
}

func NewProcessor(dispatcher CommandDispatcher, store contracts.ProcessedEventStore, publisher contracts.Publisher, backoff []time.Duration, log *logger.Logger) *Processor {
	return &Processor{dispatcher: dispatcher, store: store, publisher: publisher, backoff: backoff, log: log}
}

func (p *Processor) Process(ctx context.Context, key, value []byte) error {
	cmd, err := events.FromJSON(value)
	if err != nil {
		return p.deadLetter(ctx, key, nil, "invalid_event", err.Error())
	}
	eventID, err := uuid.Parse(cmd.ID)
	if err != nil {
		return p.deadLetter(ctx, key, cmd, "invalid_event", "event id is not a uuid")
	}

	var first bool
	if err := p.retry(ctx, func() error {
		var markErr error
		first, markErr = p.store.MarkProcessed(ctx, eventID)
		return markErr
	}); err != nil {
		return err
	}
	if !first {
		p.log.Info().Str("event_id", cmd.ID).Str("type", cmd.Type).Msg("duplicate command skipped")
		return nil
	}

	var res Result
	err = p.retry(ctx, func() error {
		var dispatchErr error
		res, dispatchErr = p.dispatch(ctx, cmd)
		return dispatchErr
	})
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if err != nil {
		return p.deadLetter(ctx, key, cmd, errorCode(err), err.Error())
	}

	if err := p.publisher.Publish(ctx, events.Topics.AccountEvents, res.Key, res.Event); err != nil {
		p.log.Error().Err(err).Str("event_id", cmd.ID).Msg("result publish failed")
		return p.deadLetter(ctx, key, res.Event, "publish_failed", err.Error())
	}
	return nil
}

func (p *Processor) dispatch(ctx context.Context, cmd *events.Event) (res Result, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			p.log.Error().Str("event_id", cmd.ID).Str("stack", string(debug.Stack())).Msgf("handler panicked: %v", recovered)
			err = fmt.Errorf("%w: %v", ErrPanic, recovered)
		}
	}()
	return p.dispatcher.Dispatch(ctx, cmd)
}

func (p *Processor) retry(ctx context.Context, op func() error) error {
	for attempt := 0; ; attempt++ {
		err := op()
		if err == nil || !isTransient(err) || attempt >= len(p.backoff) {
			return err
		}
		if !sleep(ctx, p.backoff[attempt]) {
			return ctx.Err()
		}
	}
}

func (p *Processor) deadLetter(ctx context.Context, key []byte, original *events.Event, code, message string) error {
	failed := events.NewAccountEvent(events.EventTypes.AccountCommandFailed, events.ErrorPayload{
		OriginalEvent: original,
		ErrorCode:     code,
		ErrorMessage:  message,
	})
	if original != nil {
		failed.WithTraceID(original.TraceID)
	}
	if err := p.publisher.Publish(ctx, events.Topics.AccountDLQ, string(key), failed); err != nil {
		p.log.Error().Err(err).Str("code", code).Msg("dead-letter publish failed")
	}
	return nil
}

func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func isTransient(err error) bool {
	return !models.IsInvalid(err) &&
		!errors.Is(err, models.ErrNotFound) &&
		!errors.Is(err, models.ErrAmbiguousWrite) &&
		!errors.Is(err, ErrUnknownCommand) &&
		!errors.Is(err, ErrBadPayload) &&
		!errors.Is(err, ErrPanic)
}

func errorCode(err error) string {
	switch {
	case models.IsInvalid(err):
		return models.InvalidCode(err)
	case errors.Is(err, models.ErrNotFound):
		return "account_not_found"
	case errors.Is(err, models.ErrAmbiguousWrite):
		return "ambiguous_write"
	case errors.Is(err, ErrUnknownCommand):
		return "unknown_command"
	case errors.Is(err, ErrBadPayload):
		return "bad_payload"
	case errors.Is(err, ErrPanic):
		return "panic"
	case errors.Is(err, models.ErrConflict):
		return "conflict"
	}
	return "internal_error"
}
