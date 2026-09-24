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
	"github.com/fintech-bank-platform/transaction-service/tests"
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

	depositAccount := uuid.NewString()
	res, err := dispatcher.Dispatch(context.Background(), command(events.EventTypes.CreateTransaction, deposit(depositAccount)))
	assert.NoError(t, err)
	assert.Len(t, res.Messages, 2)
	assert.Equal(t, events.EventTypes.TransactionCreated, res.Messages[0].Event.Type)
	assert.Equal(t, "trace-1", res.Messages[0].Event.TraceID)

	h.nextID = uuid.New()
	res, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.ProcessTransfer, transfer(uuid.NewString(), uuid.NewString())))
	assert.NoError(t, err)
	assert.Equal(t, events.EventTypes.DebitAccount, res.Messages[1].Event.Type)

	res, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.CreateTransaction, deposit(depositAccount)))
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
	logs := &bytes.Buffer{}
	dispatcher := handlers.NewReplyDispatcher(h.service, logger.New(logger.Config{Output: logs}))

	reply := events.NewAccountEvent(events.EventTypes.AccountCredited, events.AccountCreditedPayload{AccountID: tx.AccountID.String(), Amount: domain.AmountFromCents(3000), BalanceAfter: domain.AmountFromCents(13000), Reference: tx.ID.String(), IdempotencyKey: models.StepKey(tx.ID, models.StepCredit)}).WithTraceID("trace-7")
	res, err := dispatcher.Dispatch(context.Background(), reply)
	assert.NoError(t, err)
	assert.Equal(t, events.EventTypes.TransactionCompleted, res.Messages[0].Event.Type)
	assert.Equal(t, "trace-7", res.Messages[0].Event.TraceID)
	assert.Equal(t, models.StatusCompleted, h.repo.Transactions[tx.ID].Status)

	withdrawal := h.pending(models.TypeWithdrawal, models.StatusPending)
	rejected := events.NewAccountEvent(events.EventTypes.DebitRejected, events.DebitRejectedPayload{AccountID: withdrawal.AccountID.String(), Amount: domain.AmountFromCents(3000), Balance: domain.AmountFromCents(100), Reason: "insufficient_funds", Reference: withdrawal.ID.String(), IdempotencyKey: models.StepKey(withdrawal.ID, models.StepDebit)})
	res, err = dispatcher.Dispatch(context.Background(), rejected)
	assert.NoError(t, err)
	assert.Equal(t, "insufficient_funds", res.Messages[0].Event.Payload.(events.TransactionFailedPayload).Reason)
	assert.NotContains(t, logs.String(), "ignored account event")

	res, err = dispatcher.Dispatch(context.Background(), events.NewAccountEvent(events.EventTypes.AccountCreated, events.AccountCreatedPayload{AccountID: "a"}))
	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.NotContains(t, logs.String(), "ignored account event")

	unrelated := uuid.New()
	stray := events.NewAccountEvent(events.EventTypes.AccountDebited, events.AccountDebitedPayload{AccountID: tx.AccountID.String(), Amount: domain.AmountFromCents(3000), BalanceAfter: domain.AmountFromCents(7000), Reference: unrelated.String(), IdempotencyKey: models.StepKey(unrelated, models.StepDebit)})
	res, err = dispatcher.Dispatch(context.Background(), stray)
	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	entry := tests.FromJson(logs.String())
	assert.Equal(t, "info", entry["level"])
	assert.Equal(t, "ignored account event", entry["message"])
	assert.Equal(t, stray.ID, entry["event_id"])
	assert.Equal(t, events.EventTypes.AccountDebited, entry["type"])
	assert.Equal(t, unrelated.String(), entry["reference"])
	assert.Equal(t, models.StepKey(unrelated, models.StepDebit), entry["idempotency_key"])

	_, err = dispatcher.Dispatch(context.Background(), events.NewAccountEvent(events.EventTypes.AccountDebited, "not an object"))
	assert.ErrorIs(t, err, processor.ErrBadPayload)

	logs.Reset()
	paymentID := uuid.New()
	foreign := events.NewAccountEvent(events.EventTypes.AccountDebited, events.AccountDebitedPayload{AccountID: tx.AccountID.String(), Amount: domain.AmountFromCents(3000), BalanceAfter: domain.AmountFromCents(7000), Reference: "payment:" + paymentID.String(), IdempotencyKey: "payment:" + paymentID.String() + ":debit"})
	res, err = dispatcher.Dispatch(context.Background(), foreign)
	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.Empty(t, logs.String())
}

func TestDispatchersDecodeLegacyNumericAmounts(t *testing.T) {
	h := newHarness()
	logs := &bytes.Buffer{}
	commands := handlers.NewCommandDispatcher(h.service, logger.New(logger.Config{Output: logs}))

	cmd, err := events.FromJSON([]byte(`{"type":"transaction.create","payload":{"account_id":"` + uuid.NewString() + `","type":"deposit","amount":100.25,"currency":"BRL","idempotency_key":"legacy-1"}}`))
	assert.NoError(t, err)
	res, err := commands.Dispatch(context.Background(), cmd)
	assert.NoError(t, err)
	assert.Equal(t, int64(10025), h.repo.Transactions[h.nextID].AmountCents)
	assert.Equal(t, domain.AmountFromCents(10025), res.Messages[1].Event.Payload.(events.CreditAccountPayload).Amount)

	cmd, err = events.FromJSON([]byte(`{"type":"transaction.create","payload":{"account_id":"` + uuid.NewString() + `","type":"deposit","amount":1.005,"currency":"BRL","idempotency_key":"legacy-2"}}`))
	assert.NoError(t, err)
	_, err = commands.Dispatch(context.Background(), cmd)
	assert.ErrorIs(t, err, processor.ErrBadPayload)

	tx := h.pending(models.TypeDeposit, models.StatusPending)
	replies := handlers.NewReplyDispatcher(h.service, logger.New(logger.Config{Output: logs}))
	reply, err := events.FromJSON([]byte(`{"type":"account.credited","payload":{"account_id":"` + tx.AccountID.String() + `","amount":30,"balance_after":130.5,"reference":"` + tx.ID.String() + `","idempotency_key":"` + models.StepKey(tx.ID, models.StepCredit) + `"}}`))
	assert.NoError(t, err)
	res, err = replies.Dispatch(context.Background(), reply)
	assert.NoError(t, err)
	assert.Equal(t, int64(13050), *h.repo.Transactions[tx.ID].ToBalanceCents)
	assert.Equal(t, domain.AmountFromCents(13050), res.Messages[0].Event.Payload.(events.TransactionCompletedPayload).BalanceAfter)
}
