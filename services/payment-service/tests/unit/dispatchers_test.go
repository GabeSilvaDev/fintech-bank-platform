package unit

import (
	"bytes"
	"context"
	"testing"

	"github.com/fintech-bank-platform/payment-service/internal/app/handlers"
	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func command(eventType string, payload interface{}) *events.Event {
	return events.NewEvent(eventType, "api-gateway", payload).WithTraceID("trace-1")
}

func TestCommandDispatcherRoutesEveryCommand(t *testing.T) {
	h := newHarness()
	logs := &bytes.Buffer{}
	dispatcher := handlers.NewCommandDispatcher(h.service, logger.New(logger.Config{Output: logs}))
	account := uuid.NewString()

	res, err := dispatcher.Dispatch(context.Background(), command(events.EventTypes.ProcessPayment, pix(account)))
	assert.NoError(t, err)
	assert.Equal(t, events.EventTypes.PaymentCreated, res.Messages[0].Event.Type)
	assert.Equal(t, "trace-1", res.Messages[0].Event.TraceID)

	h.nextID = uuid.New()
	res, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.ProcessPayment, pix(account)))
	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.Contains(t, logs.String(), "duplicate idempotency key")

	debited := h.stored(models.MethodPix, models.StatusDebited)
	res, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.SubmitPayment, events.SubmitPaymentPayload{PaymentID: debited.ID.String()}))
	assert.NoError(t, err)
	assert.Equal(t, events.EventTypes.PaymentCompleted, res.Messages[0].Event.Type)

	submitted := h.stored(models.MethodTED, models.StatusSubmitted)
	h.repo.External["ted_7"] = submitted.ID
	res, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.SettlePayment, events.SettlePaymentPayload{ExternalID: "ted_7", Status: "settled"}))
	assert.NoError(t, err)
	assert.Equal(t, events.EventTypes.PaymentCompleted, res.Messages[0].Event.Type)

	for _, eventType := range []string{events.EventTypes.ProcessPayment, events.EventTypes.SubmitPayment, events.EventTypes.SettlePayment} {
		_, err = dispatcher.Dispatch(context.Background(), command(eventType, "not an object"))
		assert.ErrorIs(t, err, processor.ErrBadPayload, eventType)
	}

	_, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.ProcessPayment, events.ProcessPaymentPayload{AccountID: "x"}))
	assert.Equal(t, "invalid_account_id", domain.InvalidCode(err))

	_, err = dispatcher.Dispatch(context.Background(), command(events.EventTypes.RefundPayment, nil))
	assert.ErrorIs(t, err, processor.ErrUnknownCommand)
}

func TestReplyDispatcherRoutesAccountEvents(t *testing.T) {
	h := newHarness()
	logs := &bytes.Buffer{}
	dispatcher := handlers.NewReplyDispatcher(h.service, logger.New(logger.Config{Output: logs}))

	payment := h.stored(models.MethodPix, models.StatusPending)
	debited := events.NewAccountEvent(events.EventTypes.AccountDebited, events.AccountDebitedPayload{AccountID: payment.AccountID.String(), Amount: domain.AmountFromCents(4250), BalanceAfter: domain.AmountFromCents(5750), Reference: models.Reference(payment.ID), IdempotencyKey: models.StepKey(payment.ID, models.StepDebit)}).WithTraceID("trace-7")
	res, err := dispatcher.Dispatch(context.Background(), debited)
	assert.NoError(t, err)
	assert.Equal(t, events.EventTypes.SubmitPayment, res.Messages[0].Event.Type)
	assert.Equal(t, "trace-7", res.Messages[0].Event.TraceID)
	assert.Equal(t, int64(5750), *h.repo.Payments[payment.ID].BalanceAfterCents)

	other := h.stored(models.MethodTED, models.StatusPending)
	rejected := events.NewAccountEvent(events.EventTypes.DebitRejected, events.DebitRejectedPayload{AccountID: other.AccountID.String(), Amount: domain.AmountFromCents(4250), Reason: "insufficient_funds", Reference: models.Reference(other.ID), IdempotencyKey: models.StepKey(other.ID, models.StepDebit)})
	res, err = dispatcher.Dispatch(context.Background(), rejected)
	assert.NoError(t, err)
	assert.Equal(t, "insufficient_funds", res.Messages[0].Event.Payload.(events.PaymentFailedPayload).Reason)
	assert.Empty(t, logs.String())

	res, err = dispatcher.Dispatch(context.Background(), events.NewAccountEvent(events.EventTypes.AccountCreated, events.AccountCreatedPayload{AccountID: "a"}))
	assert.NoError(t, err)
	assert.Empty(t, res.Messages)

	transactionID := uuid.New()
	foreign := events.NewAccountEvent(events.EventTypes.AccountCredited, events.AccountCreditedPayload{AccountID: "a", Reference: transactionID.String(), IdempotencyKey: transactionID.String() + ":credit"})
	res, err = dispatcher.Dispatch(context.Background(), foreign)
	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.Empty(t, logs.String())

	stale := events.NewAccountEvent(events.EventTypes.AccountDebited, events.AccountDebitedPayload{AccountID: payment.AccountID.String(), Reference: models.Reference(payment.ID), IdempotencyKey: models.StepKey(payment.ID, models.StepDebit)})
	res, err = dispatcher.Dispatch(context.Background(), stale)
	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.Contains(t, logs.String(), "ignored account event")
	assert.Contains(t, logs.String(), models.Reference(payment.ID))

	_, err = dispatcher.Dispatch(context.Background(), events.NewAccountEvent(events.EventTypes.AccountDebited, "not an object"))
	assert.ErrorIs(t, err, processor.ErrBadPayload)
}

func TestDispatchersDecodeLegacyNumericAmounts(t *testing.T) {
	h := newHarness()
	logs := &bytes.Buffer{}
	commands := handlers.NewCommandDispatcher(h.service, logger.New(logger.Config{Output: logs}))

	cmd, err := events.FromJSON([]byte(`{"type":"payment.process","payload":{"account_id":"` + uuid.NewString() + `","payment_method":"pix","amount":42.5,"currency":"BRL","recipient":"Ana","pix_key":"ana@example.com","idempotency_key":"legacy-1"}}`))
	assert.NoError(t, err)
	res, err := commands.Dispatch(context.Background(), cmd)
	assert.NoError(t, err)
	assert.Equal(t, int64(4250), h.repo.Payments[h.nextID].AmountCents)
	assert.Equal(t, domain.AmountFromCents(4250), res.Messages[0].Event.Payload.(events.PaymentCreatedPayload).Amount)

	cmd, err = events.FromJSON([]byte(`{"type":"payment.process","payload":{"account_id":"` + uuid.NewString() + `","payment_method":"pix","amount":1.005,"currency":"BRL","recipient":"Ana","pix_key":"ana@example.com","idempotency_key":"legacy-2"}}`))
	assert.NoError(t, err)
	_, err = commands.Dispatch(context.Background(), cmd)
	assert.ErrorIs(t, err, processor.ErrBadPayload)

	payment := h.stored(models.MethodPix, models.StatusPending)
	replies := handlers.NewReplyDispatcher(h.service, logger.New(logger.Config{Output: logs}))
	reply, err := events.FromJSON([]byte(`{"type":"account.debited","payload":{"account_id":"` + payment.AccountID.String() + `","amount":42.5,"balance_after":57.5,"reference":"` + models.Reference(payment.ID) + `","idempotency_key":"` + models.StepKey(payment.ID, models.StepDebit) + `"}}`))
	assert.NoError(t, err)
	res, err = replies.Dispatch(context.Background(), reply)
	assert.NoError(t, err)
	assert.Equal(t, events.EventTypes.SubmitPayment, res.Messages[0].Event.Type)
	assert.Equal(t, int64(5750), *h.repo.Payments[payment.ID].BalanceAfterCents)

	large := h.stored(models.MethodPix, models.StatusPending)
	reply, err = events.FromJSON([]byte(`{"type":"account.debited","payload":{"account_id":"` + large.AccountID.String() + `","amount":42.5,"balance_after":152626798.92,"reference":"` + models.Reference(large.ID) + `","idempotency_key":"` + models.StepKey(large.ID, models.StepDebit) + `"}}`))
	assert.NoError(t, err)
	res, err = replies.Dispatch(context.Background(), reply)
	assert.NoError(t, err)
	assert.Equal(t, events.EventTypes.SubmitPayment, res.Messages[0].Event.Type)
	assert.Equal(t, int64(15262679892), *h.repo.Payments[large.ID].BalanceAfterCents)
}
