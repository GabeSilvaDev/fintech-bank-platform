package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/fintech-bank-platform/pkg/events"
)

var (
	ErrUnknownCommand = errors.New("unknown command")
	ErrBadPayload     = errors.New("bad payload")
)

type Result struct {
	Event *events.Event
	Key   string
}

type CommandDispatcher interface {
	Dispatch(ctx context.Context, cmd *events.Event) (Result, error)
}

type Dispatcher struct {
	accounts *services.AccountService
}

func NewDispatcher(accounts *services.AccountService) *Dispatcher {
	return &Dispatcher{accounts: accounts}
}

func (d *Dispatcher) Dispatch(ctx context.Context, cmd *events.Event) (Result, error) {
	switch cmd.Type {
	case events.EventTypes.CreateAccount:
		var payload events.CreateAccountPayload
		if err := decodePayload(cmd, &payload); err != nil {
			return Result{}, err
		}
		created, err := d.accounts.Create(ctx, payload)
		if err != nil {
			return Result{}, err
		}
		return result(cmd, events.EventTypes.AccountCreated, created, created.UserID), nil

	case events.EventTypes.UpdateAccount:
		var payload events.UpdateAccountPayload
		if err := decodePayload(cmd, &payload); err != nil {
			return Result{}, err
		}
		updated, err := d.accounts.Update(ctx, payload)
		if err != nil {
			return Result{}, err
		}
		return result(cmd, events.EventTypes.AccountUpdated, updated, updated.AccountID), nil

	case events.EventTypes.DeleteAccount:
		var payload events.DeleteAccountPayload
		if err := decodePayload(cmd, &payload); err != nil {
			return Result{}, err
		}
		deleted, err := d.accounts.Close(ctx, payload)
		if err != nil {
			return Result{}, err
		}
		return result(cmd, events.EventTypes.AccountDeleted, deleted, deleted.AccountID), nil

	case events.EventTypes.CreditAccount:
		var payload events.CreditAccountPayload
		if err := decodePayload(cmd, &payload); err != nil {
			return Result{}, err
		}
		credited, err := d.accounts.Credit(ctx, payload)
		if err != nil {
			return Result{}, err
		}
		return result(cmd, events.EventTypes.AccountCredited, credited, credited.AccountID), nil

	case events.EventTypes.DebitAccount:
		var payload events.DebitAccountPayload
		if err := decodePayload(cmd, &payload); err != nil {
			return Result{}, err
		}
		outcome, err := d.accounts.Debit(ctx, payload)
		if err != nil {
			return Result{}, err
		}
		if outcome.Rejected != nil {
			return result(cmd, events.EventTypes.DebitRejected, *outcome.Rejected, outcome.Rejected.AccountID), nil
		}
		return result(cmd, events.EventTypes.AccountDebited, *outcome.Debited, outcome.Debited.AccountID), nil
	}

	return Result{}, fmt.Errorf("%w: %s", ErrUnknownCommand, cmd.Type)
}

func decodePayload(cmd *events.Event, dst interface{}) error {
	raw, err := json.Marshal(cmd.Payload)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadPayload, err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("%w: %v", ErrBadPayload, err)
	}
	return nil
}

func result(cmd *events.Event, eventType string, payload interface{}, key string) Result {
	return Result{Event: events.NewAccountEvent(eventType, payload).WithTraceID(cmd.TraceID), Key: key}
}
