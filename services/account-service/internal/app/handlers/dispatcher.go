package handlers

import (
	"context"
	"fmt"

	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/processor"
)

type Dispatcher struct {
	accounts *services.AccountService
}

func NewDispatcher(accounts *services.AccountService) *Dispatcher {
	return &Dispatcher{accounts: accounts}
}

func (d *Dispatcher) Dispatch(ctx context.Context, cmd *events.Event) (processor.Result, error) {
	switch cmd.Type {
	case events.EventTypes.CreateAccount:
		var payload events.CreateAccountPayload
		if err := processor.DecodePayload(cmd, &payload); err != nil {
			return processor.Result{}, err
		}
		created, err := d.accounts.Create(ctx, payload)
		if err != nil {
			return processor.Result{}, err
		}
		return processor.Reply(events.Topics.AccountEvents, created.UserID, events.NewAccountEvent(events.EventTypes.AccountCreated, created).WithTraceID(cmd.TraceID)), nil

	case events.EventTypes.UpdateAccount:
		var payload events.UpdateAccountPayload
		if err := processor.DecodePayload(cmd, &payload); err != nil {
			return processor.Result{}, err
		}
		updated, err := d.accounts.Update(ctx, payload)
		if err != nil {
			return processor.Result{}, err
		}
		return processor.Reply(events.Topics.AccountEvents, updated.AccountID, events.NewAccountEvent(events.EventTypes.AccountUpdated, updated).WithTraceID(cmd.TraceID)), nil

	case events.EventTypes.DeleteAccount:
		var payload events.DeleteAccountPayload
		if err := processor.DecodePayload(cmd, &payload); err != nil {
			return processor.Result{}, err
		}
		deleted, err := d.accounts.Close(ctx, payload)
		if err != nil {
			return processor.Result{}, err
		}
		return processor.Reply(events.Topics.AccountEvents, deleted.AccountID, events.NewAccountEvent(events.EventTypes.AccountDeleted, deleted).WithTraceID(cmd.TraceID)), nil

	case events.EventTypes.CreditAccount:
		var payload events.CreditAccountPayload
		if err := processor.DecodePayload(cmd, &payload); err != nil {
			return processor.Result{}, err
		}
		credited, err := d.accounts.Credit(ctx, payload)
		if err != nil {
			return processor.Result{}, err
		}
		return processor.Reply(events.Topics.AccountEvents, credited.AccountID, events.NewAccountEvent(events.EventTypes.AccountCredited, credited).WithTraceID(cmd.TraceID)), nil

	case events.EventTypes.DebitAccount:
		var payload events.DebitAccountPayload
		if err := processor.DecodePayload(cmd, &payload); err != nil {
			return processor.Result{}, err
		}
		outcome, err := d.accounts.Debit(ctx, payload)
		if err != nil {
			return processor.Result{}, err
		}
		if outcome.Rejected != nil {
			return processor.Reply(events.Topics.AccountEvents, outcome.Rejected.AccountID, events.NewAccountEvent(events.EventTypes.DebitRejected, *outcome.Rejected).WithTraceID(cmd.TraceID)), nil
		}
		return processor.Reply(events.Topics.AccountEvents, outcome.Debited.AccountID, events.NewAccountEvent(events.EventTypes.AccountDebited, *outcome.Debited).WithTraceID(cmd.TraceID)), nil
	}

	return processor.Result{}, fmt.Errorf("%w: %s", processor.ErrUnknownCommand, cmd.Type)
}
