package unit

import (
	"context"
	"testing"

	"github.com/fintech-bank-platform/account-service/internal/app/handlers"
	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func command(eventType string, payload interface{}) *events.Event {
	return events.NewAccountCommand(eventType, payload).WithTraceID("trace-1")
}

func TestDispatchCreateAccount(t *testing.T) {
	h := newHarness()
	dispatcher := handlers.NewDispatcher(h.service)
	cmd := validCreate()

	result, err := dispatcher.Dispatch(context.Background(), command(events.EventTypes.CreateAccount, cmd))

	assert.NoError(t, err)
	assert.Equal(t, events.EventTypes.AccountCreated, result.Event.Type)
	assert.Equal(t, "account-service", result.Event.Source)
	assert.Equal(t, "trace-1", result.Event.TraceID)
	assert.Equal(t, cmd.UserID, result.Key)
	assert.Equal(t, "12345678", result.Event.Payload.(events.AccountCreatedPayload).AccountNumber)
}

func TestDispatchUpdateCloseCreditDebit(t *testing.T) {
	h := newHarness()
	dispatcher := handlers.NewDispatcher(h.service)
	account := h.activeAccount(1000)
	id := account.AccountID.String()

	result, err := dispatcher.Dispatch(context.Background(), command(events.EventTypes.UpdateAccount, events.UpdateAccountPayload{AccountID: id, Name: strPtr("Ana Lima")}))
	assert.NoError(t, err)
	assert.Equal(t, events.EventTypes.AccountUpdated, result.Event.Type)
	assert.Equal(t, id, result.Key)

	result, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.CreditAccount, events.CreditAccountPayload{AccountID: id, Amount: 5, Currency: "BRL"}))
	assert.NoError(t, err)
	assert.Equal(t, events.EventTypes.AccountCredited, result.Event.Type)
	assert.Equal(t, 15.0, result.Event.Payload.(events.AccountCreditedPayload).BalanceAfter)

	result, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.DebitAccount, events.DebitAccountPayload{AccountID: id, Amount: 15, Currency: "BRL"}))
	assert.NoError(t, err)
	assert.Equal(t, events.EventTypes.AccountDebited, result.Event.Type)
	assert.Equal(t, 0.0, result.Event.Payload.(events.AccountDebitedPayload).BalanceAfter)

	result, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.DebitAccount, events.DebitAccountPayload{AccountID: id, Amount: 1, Currency: "BRL"}))
	assert.NoError(t, err)
	assert.Equal(t, events.EventTypes.DebitRejected, result.Event.Type)
	assert.Equal(t, "insufficient_funds", result.Event.Payload.(events.DebitRejectedPayload).Reason)

	result, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.DeleteAccount, events.DeleteAccountPayload{AccountID: id}))
	assert.NoError(t, err)
	assert.Equal(t, events.EventTypes.AccountDeleted, result.Event.Type)
	assert.Equal(t, id, result.Key)
}

func TestDispatchPropagatesServiceErrors(t *testing.T) {
	h := newHarness()
	dispatcher := handlers.NewDispatcher(h.service)
	missing := uuid.NewString()

	_, err := dispatcher.Dispatch(context.Background(), command(events.EventTypes.UpdateAccount, events.UpdateAccountPayload{AccountID: missing, Name: strPtr("Ana Lima")}))
	assert.ErrorIs(t, err, models.ErrNotFound)

	_, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.DeleteAccount, events.DeleteAccountPayload{AccountID: "x"}))
	assert.Equal(t, "invalid_account_id", models.InvalidCode(err))

	_, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.CreditAccount, events.CreditAccountPayload{AccountID: missing, Amount: 1, Currency: "BRL"}))
	assert.ErrorIs(t, err, models.ErrNotFound)

	_, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.DebitAccount, events.DebitAccountPayload{AccountID: missing, Amount: 1, Currency: "BRL"}))
	assert.ErrorIs(t, err, models.ErrNotFound)

	_, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.CreateAccount, events.CreateAccountPayload{UserID: "x"}))
	assert.Equal(t, "invalid_user_id", models.InvalidCode(err))
}

func TestDispatchRejectsUnknownAndMalformed(t *testing.T) {
	h := newHarness()
	dispatcher := handlers.NewDispatcher(h.service)

	_, err := dispatcher.Dispatch(context.Background(), command("account.explode", nil))
	assert.ErrorIs(t, err, handlers.ErrUnknownCommand)

	_, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.CreateAccount, "not an object"))
	assert.ErrorIs(t, err, handlers.ErrBadPayload)

	_, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.CreateAccount, make(chan int)))
	assert.ErrorIs(t, err, handlers.ErrBadPayload)
}

func TestDispatchRejectsMalformedPayloadsForAllCommands(t *testing.T) {
	h := newHarness()
	dispatcher := handlers.NewDispatcher(h.service)

	types := []string{events.EventTypes.UpdateAccount, events.EventTypes.DeleteAccount, events.EventTypes.CreditAccount, events.EventTypes.DebitAccount}
	for _, eventType := range types {
		_, err := dispatcher.Dispatch(context.Background(), command(eventType, "not an object"))
		assert.ErrorIs(t, err, handlers.ErrBadPayload, eventType)
	}
}

func strPtr(s string) *string {
	return &s
}
