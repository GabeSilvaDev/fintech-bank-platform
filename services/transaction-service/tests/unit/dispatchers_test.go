package unit

import (
	"bytes"
	"context"
	"testing"

	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/fintech-bank-platform/transaction-service/internal/app/handlers"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func command(eventType string, payload interface{}) *events.Event {
	return events.NewEvent(eventType, "api-gateway", payload).WithTraceID("trace-1")
}

func TestCommandDispatcherRoutesCreateAndTransfer(t *testing.T) {
	h := newHarness()
	logs := &bytes.Buffer{}
	dispatcher := handlers.NewCommandDispatcher(h.service, logger.New(logger.Config{Output: logs}))

	res, err := dispatcher.Dispatch(context.Background(), command(events.EventTypes.CreateTransaction, deposit(uuid.NewString())))
	assert.NoError(t, err)
	assert.Len(t, res.Messages, 2)
	assert.Equal(t, events.EventTypes.TransactionCreated, res.Messages[0].Event.Type)
	assert.Equal(t, "trace-1", res.Messages[0].Event.TraceID)

	h.nextID = uuid.New()
	res, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.ProcessTransfer, transfer(uuid.NewString(), uuid.NewString())))
	assert.NoError(t, err)
	assert.Equal(t, events.EventTypes.DebitAccount, res.Messages[1].Event.Type)

	res, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.CreateTransaction, deposit(uuid.NewString())))
	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.Contains(t, logs.String(), "duplicate idempotency key")

	_, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.CreateTransaction, "not an object"))
	assert.ErrorIs(t, err, processor.ErrBadPayload)

	_, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.ProcessTransfer, make(chan int)))
	assert.ErrorIs(t, err, processor.ErrBadPayload)

	_, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.CreateTransaction, events.CreateTransactionPayload{AccountID: "x"}))
	assert.Equal(t, "invalid_account_id", domain.InvalidCode(err))

	_, err = dispatcher.Dispatch(context.Background(), command("transaction.reverse", nil))
	assert.ErrorIs(t, err, processor.ErrUnknownCommand)
}

func TestReplyDispatcherRoutesAccountEvents(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeDeposit, models.StatusPending)
	dispatcher := handlers.NewReplyDispatcher(h.service)

	reply := events.NewAccountEvent(events.EventTypes.AccountCredited, events.AccountCreditedPayload{AccountID: tx.AccountID.String(), Amount: 30, BalanceAfter: 130, Reference: tx.ID.String(), IdempotencyKey: models.StepKey(tx.ID, models.StepCredit)}).WithTraceID("trace-7")
	res, err := dispatcher.Dispatch(context.Background(), reply)
	assert.NoError(t, err)
	assert.Equal(t, events.EventTypes.TransactionCompleted, res.Messages[0].Event.Type)
	assert.Equal(t, "trace-7", res.Messages[0].Event.TraceID)
	assert.Equal(t, models.StatusCompleted, h.repo.Transactions[tx.ID].Status)

	withdrawal := h.pending(models.TypeWithdrawal, models.StatusPending)
	rejected := events.NewAccountEvent(events.EventTypes.DebitRejected, events.DebitRejectedPayload{AccountID: withdrawal.AccountID.String(), Amount: 30, Balance: 1, Reason: "insufficient_funds", Reference: withdrawal.ID.String(), IdempotencyKey: models.StepKey(withdrawal.ID, models.StepDebit)})
	res, err = dispatcher.Dispatch(context.Background(), rejected)
	assert.NoError(t, err)
	assert.Equal(t, "insufficient_funds", res.Messages[0].Event.Payload.(events.TransactionFailedPayload).Reason)

	res, err = dispatcher.Dispatch(context.Background(), events.NewAccountEvent(events.EventTypes.AccountCreated, events.AccountCreatedPayload{AccountID: "a"}))
	assert.NoError(t, err)
	assert.Empty(t, res.Messages)

	_, err = dispatcher.Dispatch(context.Background(), events.NewAccountEvent(events.EventTypes.AccountDebited, "not an object"))
	assert.ErrorIs(t, err, processor.ErrBadPayload)
}
