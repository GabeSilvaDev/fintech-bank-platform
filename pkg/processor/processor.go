package processor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/google/uuid"
)

var (
	ErrUnknownCommand = errors.New("unknown command")
	ErrBadPayload     = errors.New("bad payload")
	ErrPanic          = errors.New("handler panicked")
)

type Message struct {
	Topic string
	Key   string
	Event *events.Event
}

type Result struct {
	Messages []Message
}

func Reply(topic, key string, event *events.Event) Result {
	return Result{Messages: []Message{{Topic: topic, Key: key, Event: event}}}
}

type Dispatcher interface {
	Dispatch(ctx context.Context, cmd *events.Event) (Result, error)
}

type Store interface {
	MarkProcessed(ctx context.Context, eventID uuid.UUID) (bool, error)
}

type Publisher interface {
	Publish(ctx context.Context, topic, key string, event *events.Event) error
}

type Config struct {
	Source          string
	FailedEventType string
	DLQTopic        string
	Backoff         []time.Duration
}

type Processor struct {
	dispatcher Dispatcher
	store      Store
	publisher  Publisher
	cfg        Config
	log        *logger.Logger
}

func NewProcessor(dispatcher Dispatcher, store Store, publisher Publisher, cfg Config, log *logger.Logger) *Processor {
	return &Processor{dispatcher: dispatcher, store: store, publisher: publisher, cfg: cfg, log: log}
}

func (p *Processor) Process(ctx context.Context, key, value []byte) error {
	cmd, err := events.FromJSON(value)
	if err != nil {
		return p.deadLetter(ctx, key, nil, "invalid_event", err.Error(), 0)
	}
	eventID, err := uuid.Parse(cmd.ID)
	if err != nil {
		return p.deadLetter(ctx, key, cmd, "invalid_event", "event id is not a uuid", 0)
	}

	var first bool
	if _, err := p.retry(ctx, func() error {
		var markErr error
		first, markErr = p.store.MarkProcessed(ctx, eventID)
		return markErr
	}); err != nil {
		return err
	}
	if !first {
		p.log.Info().Str("event_id", cmd.ID).Str("type", cmd.Type).Msg("duplicate event skipped")
		return nil
	}

	var res Result
	attempts, err := p.retry(ctx, func() error {
		var dispatchErr error
		res, dispatchErr = p.dispatch(ctx, cmd)
		return dispatchErr
	})
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if err != nil {
		return p.deadLetter(ctx, key, cmd, errorCode(err), err.Error(), attempts)
	}

	for _, msg := range res.Messages {
		if err := p.publisher.Publish(ctx, msg.Topic, msg.Key, msg.Event); err != nil {
			p.logUndeliverable(err, msg.Event, "publish_failed", "result publish failed")
			_ = p.deadLetter(ctx, key, msg.Event, "publish_failed", err.Error(), 0)
		}
	}
	return nil
}

func DecodePayload(cmd *events.Event, dst interface{}) error {
	raw, err := json.Marshal(cmd.Payload)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadPayload, err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("%w: %v", ErrBadPayload, err)
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

func (p *Processor) retry(ctx context.Context, op func() error) (int, error) {
	for attempt := 1; ; attempt++ {
		err := op()
		if err == nil || !isTransient(err) || attempt > len(p.cfg.Backoff) {
			return attempt, err
		}
		if !sleep(ctx, p.cfg.Backoff[attempt-1]) {
			return attempt, ctx.Err()
		}
	}
}

func (p *Processor) deadLetter(ctx context.Context, key []byte, original *events.Event, code, message string, retries int) error {
	failed := events.NewEvent(p.cfg.FailedEventType, p.cfg.Source, events.ErrorPayload{
		OriginalEvent: original,
		ErrorCode:     code,
		ErrorMessage:  message,
		Retries:       retries,
	})
	if original != nil {
		failed.WithTraceID(original.TraceID)
	}
	if err := p.publisher.Publish(ctx, p.cfg.DLQTopic, string(key), failed); err != nil {
		p.logUndeliverable(err, failed, code, "dead-letter publish failed")
	}
	return nil
}

func (p *Processor) logUndeliverable(err error, event *events.Event, code, message string) {
	entry := p.log.Error().Err(err).Str("event_id", event.ID).Str("trace_id", event.TraceID).Str("type", event.Type).Str("code", code)
	if raw, encodeErr := event.ToJSON(); encodeErr == nil {
		entry = entry.RawJSON("event", raw)
	} else {
		entry = entry.Interface("event", event)
	}
	entry.Msg(message)
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
	return !domain.IsInvalid(err) &&
		!errors.Is(err, domain.ErrNotFound) &&
		!errors.Is(err, domain.ErrAmbiguousWrite) &&
		!errors.Is(err, ErrUnknownCommand) &&
		!errors.Is(err, ErrBadPayload) &&
		!errors.Is(err, ErrPanic)
}

func errorCode(err error) string {
	switch {
	case domain.IsInvalid(err):
		return domain.InvalidCode(err)
	case errors.Is(err, domain.ErrNotFound):
		return "not_found"
	case errors.Is(err, domain.ErrAmbiguousWrite):
		return "ambiguous_write"
	case errors.Is(err, ErrUnknownCommand):
		return "unknown_command"
	case errors.Is(err, ErrBadPayload):
		return "bad_payload"
	case errors.Is(err, ErrPanic):
		return "panic"
	case errors.Is(err, domain.ErrConflict):
		return "conflict"
	}
	return "internal_error"
}
