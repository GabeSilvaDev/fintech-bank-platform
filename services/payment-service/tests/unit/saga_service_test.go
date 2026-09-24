package unit

import (
	"context"
	"errors"
	"testing"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/payment-service/internal/app/services"
	"github.com/fintech-bank-platform/payment-service/tests"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func (h *harness) stored(method models.Method, status models.Status) *models.Payment {
	payment := &models.Payment{ID: uuid.New(), AccountID: uuid.New(), Method: method, Status: status, AmountCents: 4250, Currency: "BRL", Recipient: "Ana", IdempotencyKey: "k", CreatedAt: now, UpdatedAt: now}
	switch method {
	case models.MethodPix:
		payment.PixKey = "ana@example.com"
	case models.MethodBoleto:
		payment.BoletoCode = boleto150
	case models.MethodTED:
		payment.TED = &models.TEDDetails{BankCode: "341", Branch: "0001", Account: "123456", Document: "52998224725"}
	}
	h.repo.Put(payment)
	return payment
}

func reply(kind string, payment *models.Payment, step models.Step, balance int64, reason string) services.Reply {
	return services.Reply{Kind: kind, Reference: models.Reference(payment.ID), IdempotencyKey: models.StepKey(payment.ID, step), BalanceAfter: domain.AmountFromCents(balance), Reason: reason, TraceID: "trace-9"}
}

func TestDebitedRequestsSubmission(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodPix, models.StatusPending)

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.AccountDebited, payment, models.StepDebit, 5750, ""))

	assert.NoError(t, err)
	stored := h.repo.Payments[payment.ID]
	assert.Equal(t, models.StatusDebited, stored.Status)
	assert.Equal(t, int64(5750), *stored.BalanceAfterCents)
	assert.Len(t, res.Messages, 1)
	msg := res.Messages[0]
	assert.Equal(t, events.Topics.PaymentCommands, msg.Topic)
	assert.Equal(t, payment.ID.String(), msg.Key)
	assert.Equal(t, events.EventTypes.SubmitPayment, msg.Event.Type)
	assert.Equal(t, "payment-service", msg.Event.Source)
	assert.Equal(t, "trace-9", msg.Event.TraceID)
	assert.Equal(t, payment.ID.String(), msg.Event.Payload.(events.SubmitPaymentPayload).PaymentID)
}

func TestDebitRejectedFails(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodTED, models.StatusPending)

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.DebitRejected, payment, models.StepDebit, 0, "insufficient_funds"))

	assert.NoError(t, err)
	assert.Equal(t, models.StatusFailed, h.repo.Payments[payment.ID].Status)
	assert.Equal(t, "insufficient_funds", h.repo.Payments[payment.ID].FailureReason)
	msg := res.Messages[0]
	assert.Equal(t, events.Topics.PaymentEvents, msg.Topic)
	assert.Equal(t, payment.AccountID.String(), msg.Key)
	assert.Equal(t, events.EventTypes.PaymentFailed, msg.Event.Type)
	failed := msg.Event.Payload.(events.PaymentFailedPayload)
	assert.Equal(t, payment.ID.String(), failed.PaymentID)
	assert.Equal(t, "ted", failed.PaymentMethod)
	assert.Equal(t, domain.AmountFromCents(4250), failed.Amount)
	assert.Equal(t, "insufficient_funds", failed.Reason)
	assert.Equal(t, "failed", failed.Status)
	assert.Equal(t, now, failed.FailedAt)
}

func TestRefundRepliesSettleTheRefund(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodPix, models.StatusRefunding)
	payment.FailureReason = "pix_key_not_found"
	h.repo.Put(payment)

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.AccountCredited, payment, models.StepRefund, 10000, ""))
	assert.NoError(t, err)
	assert.Equal(t, models.StatusRefunded, h.repo.Payments[payment.ID].Status)
	assert.Equal(t, int64(10000), *h.repo.Payments[payment.ID].BalanceAfterCents)
	failed := res.Messages[0].Event.Payload.(events.PaymentFailedPayload)
	assert.Equal(t, "refunded", failed.Status)
	assert.Equal(t, "pix_key_not_found", failed.Reason)

	stuck := h.stored(models.MethodBoleto, models.StatusRefunding)
	stuck.FailureReason = "boleto_not_found"
	h.repo.Put(stuck)
	res, err = h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.CreditRejected, stuck, models.StepRefund, 0, "account_not_active"))
	assert.NoError(t, err)
	assert.Equal(t, models.StatusRefundFailed, h.repo.Payments[stuck.ID].Status)
	assert.Len(t, res.Messages, 2)
	assert.Equal(t, "refund_failed", res.Messages[0].Event.Payload.(events.PaymentFailedPayload).Status)
	assert.Equal(t, "boleto_not_found", res.Messages[0].Event.Payload.(events.PaymentFailedPayload).Reason)
	alert := res.Messages[1]
	assert.Equal(t, events.Topics.PaymentDLQ, alert.Topic)
	assert.Equal(t, stuck.ID.String(), alert.Key)
	assert.Equal(t, events.EventTypes.PaymentCommandFailed, alert.Event.Type)
	assert.Equal(t, "trace-9", alert.Event.TraceID)
	dlq := alert.Event.Payload.(events.ErrorPayload)
	assert.Equal(t, "refund_failed", dlq.ErrorCode)
	assert.Contains(t, dlq.ErrorMessage, stuck.ID.String())
	assert.Contains(t, dlq.ErrorMessage, "account_not_active")
}

func TestApplyIgnoresForeignStaleAndUnknownReplies(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodPix, models.StatusPending)
	other := uuid.New()

	never := []services.Reply{
		{Kind: events.EventTypes.AccountDebited, Reference: payment.ID.String(), IdempotencyKey: payment.ID.String() + ":debit"},
		{Kind: events.EventTypes.AccountDebited, Reference: models.Reference(payment.ID), IdempotencyKey: "free-form"},
		{Kind: events.EventTypes.AccountDebited, Reference: models.Reference(payment.ID), IdempotencyKey: models.StepKey(other, models.StepDebit)},
		{Kind: events.EventTypes.AccountDebited, Reference: models.Reference(other), IdempotencyKey: models.StepKey(other, models.StepDebit)},
		reply(events.EventTypes.AccountCredited, payment, models.StepDebit, 100, ""),
		reply(events.EventTypes.AccountDebited, payment, models.StepRefund, 100, ""),
		reply(events.EventTypes.AccountCreated, payment, models.StepDebit, 100, ""),
	}
	for i, r := range never {
		res, err := h.service.ApplyAccountEvent(context.Background(), r)
		assert.NoError(t, err, i)
		assert.Empty(t, res.Messages, i)
	}
	assert.Empty(t, h.repo.Transitions)

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.AccountCredited, payment, models.StepRefund, 100, ""))
	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.Equal(t, models.StatusPending, h.repo.Payments[payment.ID].Status)
	assert.Len(t, h.repo.Transitions, 1)
}

func TestApplyPropagatesRepositoryErrors(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodPix, models.StatusPending)

	h.repo.GetErrs = []error{errors.New("db down")}
	_, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.AccountDebited, payment, models.StepDebit, 100, ""))
	assert.EqualError(t, err, "db down")

	h.repo.TransitionResults = []tests.TransitionResult{{Err: errors.New("db down")}}
	_, err = h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.AccountDebited, payment, models.StepDebit, 100, ""))
	assert.EqualError(t, err, "db down")
}

func TestSubmitSettledCompletes(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodPix, models.StatusDebited)
	h.gateway.Submissions = []models.Submission{{ExternalID: "pix_1", Status: models.SubmissionSettled}}

	res, err := h.service.Submit(context.Background(), events.SubmitPaymentPayload{PaymentID: payment.ID.String()}, "trace-3")

	assert.NoError(t, err)
	assert.Equal(t, payment.ID, h.gateway.Calls[0].ID)
	stored := h.repo.Payments[payment.ID]
	assert.Equal(t, models.StatusCompleted, stored.Status)
	assert.Equal(t, "pix_1", stored.ExternalID)
	assert.Equal(t, now, *stored.CompletedAt)
	msg := res.Messages[0]
	assert.Equal(t, events.Topics.PaymentEvents, msg.Topic)
	assert.Equal(t, events.EventTypes.PaymentCompleted, msg.Event.Type)
	assert.Equal(t, "trace-3", msg.Event.TraceID)
	completed := msg.Event.Payload.(events.PaymentCompletedPayload)
	assert.Equal(t, payment.ID.String(), completed.PaymentID)
	assert.Equal(t, "pix", completed.PaymentMethod)
	assert.Equal(t, domain.AmountFromCents(4250), completed.Amount)
	assert.Equal(t, "completed", completed.Status)
	assert.Equal(t, "pix_1", completed.ExternalID)
	assert.Equal(t, now, completed.CompletedAt)
}

func TestSubmitPendingBindsTheExternalID(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodTED, models.StatusDebited)
	h.gateway.Submissions = []models.Submission{{ExternalID: "ted_1", Status: models.SubmissionPending}}

	res, err := h.service.Submit(context.Background(), events.SubmitPaymentPayload{PaymentID: payment.ID.String()}, "t")

	assert.NoError(t, err)
	assert.Equal(t, payment.ID, h.repo.External["ted_1"])
	assert.Equal(t, models.StatusSubmitted, h.repo.Payments[payment.ID].Status)
	assert.Equal(t, "ted_1", h.repo.Payments[payment.ID].ExternalID)
	assert.Equal(t, events.EventTypes.PaymentProcessed, res.Messages[0].Event.Type)
	processed := res.Messages[0].Event.Payload.(events.PaymentProcessedPayload)
	assert.Equal(t, "ted_1", processed.ExternalID)
	assert.Equal(t, "submitted", processed.Status)
	assert.Equal(t, "ted", processed.PaymentMethod)
	assert.Equal(t, now, processed.ProcessedAt)
}

func TestSubmitRejectedRefunds(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodPix, models.StatusDebited)
	h.gateway.Submissions = []models.Submission{{Status: models.SubmissionRejected, Reason: "pix_key_not_found"}}

	res, err := h.service.Submit(context.Background(), events.SubmitPaymentPayload{PaymentID: payment.ID.String()}, "trace-4")

	assert.NoError(t, err)
	assert.Equal(t, models.StatusRefunding, h.repo.Payments[payment.ID].Status)
	assert.Equal(t, "pix_key_not_found", h.repo.Payments[payment.ID].FailureReason)
	msg := res.Messages[0]
	assert.Equal(t, events.Topics.AccountCommands, msg.Topic)
	assert.Equal(t, payment.AccountID.String(), msg.Key)
	assert.Equal(t, events.EventTypes.CreditAccount, msg.Event.Type)
	assert.Equal(t, "payment-service", msg.Event.Source)
	assert.Equal(t, "trace-4", msg.Event.TraceID)
	refund := msg.Event.Payload.(events.CreditAccountPayload)
	assert.Equal(t, payment.AccountID.String(), refund.AccountID)
	assert.Equal(t, domain.AmountFromCents(4250), refund.Amount)
	assert.Equal(t, "payment:"+payment.ID.String(), refund.Reference)
	assert.Equal(t, "payment:"+payment.ID.String()+":refund", refund.IdempotencyKey)

	blank := h.stored(models.MethodPix, models.StatusDebited)
	h.gateway.Submissions = []models.Submission{{Status: models.SubmissionRejected}}
	_, err = h.service.Submit(context.Background(), events.SubmitPaymentPayload{PaymentID: blank.ID.String()}, "t")
	assert.NoError(t, err)
	assert.Equal(t, "rejected_by_provider", h.repo.Payments[blank.ID].FailureReason)
}

func TestSubmitGuardsAndErrors(t *testing.T) {
	h := newHarness()
	_, err := h.service.Submit(context.Background(), events.SubmitPaymentPayload{PaymentID: "x"}, "t")
	assert.Equal(t, "invalid_payment_id", domain.InvalidCode(err))

	_, err = h.service.Submit(context.Background(), events.SubmitPaymentPayload{PaymentID: uuid.NewString()}, "t")
	assert.ErrorIs(t, err, domain.ErrNotFound)

	done := h.stored(models.MethodPix, models.StatusSubmitted)
	res, err := h.service.Submit(context.Background(), events.SubmitPaymentPayload{PaymentID: done.ID.String()}, "t")
	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.Empty(t, h.gateway.Calls)

	payment := h.stored(models.MethodPix, models.StatusDebited)
	h.gateway.Errs = []error{errors.New("provider timeout")}
	_, err = h.service.Submit(context.Background(), events.SubmitPaymentPayload{PaymentID: payment.ID.String()}, "t")
	assert.EqualError(t, err, "provider timeout")
	assert.Equal(t, models.StatusDebited, h.repo.Payments[payment.ID].Status)

	h.gateway.Submissions = []models.Submission{{ExternalID: "ted_9", Status: models.SubmissionPending}}
	h.repo.BindErr = errors.New("db down")
	_, err = h.service.Submit(context.Background(), events.SubmitPaymentPayload{PaymentID: payment.ID.String()}, "t")
	assert.EqualError(t, err, "db down")
	h.repo.BindErr = nil

	h.gateway.Submissions = []models.Submission{{ExternalID: "x", Status: "lost"}}
	_, err = h.service.Submit(context.Background(), events.SubmitPaymentPayload{PaymentID: payment.ID.String()}, "t")
	assert.EqualError(t, err, `unexpected submission status "lost"`)

	h.repo.TransitionResults = []tests.TransitionResult{{Applied: false}}
	res, err = h.service.Submit(context.Background(), events.SubmitPaymentPayload{PaymentID: payment.ID.String()}, "t")
	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
}

func TestSettleCompletesOrRefundsSubmittedPayments(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodTED, models.StatusSubmitted)
	payment.ExternalID = "ted_1"
	h.repo.Put(payment)
	h.repo.External["ted_1"] = payment.ID

	res, err := h.service.Settle(context.Background(), events.SettlePaymentPayload{ExternalID: " ted_1 ", Status: "settled"}, "trace-5")
	assert.NoError(t, err)
	assert.Equal(t, models.StatusCompleted, h.repo.Payments[payment.ID].Status)
	assert.Equal(t, now, *h.repo.Payments[payment.ID].CompletedAt)
	assert.Equal(t, events.EventTypes.PaymentCompleted, res.Messages[0].Event.Type)
	assert.Equal(t, "ted_1", res.Messages[0].Event.Payload.(events.PaymentCompletedPayload).ExternalID)
	assert.Equal(t, "trace-5", res.Messages[0].Event.TraceID)

	res, err = h.service.Settle(context.Background(), events.SettlePaymentPayload{ExternalID: "ted_1", Status: "settled"}, "t")
	assert.NoError(t, err)
	assert.Empty(t, res.Messages)

	rejected := h.stored(models.MethodBoleto, models.StatusSubmitted)
	h.repo.External["bol_1"] = rejected.ID
	res, err = h.service.Settle(context.Background(), events.SettlePaymentPayload{ExternalID: "bol_1", Status: "rejected", Reason: "boleto_not_found"}, "t")
	assert.NoError(t, err)
	assert.Equal(t, models.StatusRefunding, h.repo.Payments[rejected.ID].Status)
	assert.Equal(t, "boleto_not_found", h.repo.Payments[rejected.ID].FailureReason)
	assert.Equal(t, events.EventTypes.CreditAccount, res.Messages[0].Event.Type)
	assert.Equal(t, models.StepKey(rejected.ID, models.StepRefund), res.Messages[0].Event.Payload.(events.CreditAccountPayload).IdempotencyKey)
}

func TestSettleGuardsAndErrors(t *testing.T) {
	h := newHarness()
	_, err := h.service.Settle(context.Background(), events.SettlePaymentPayload{ExternalID: "  ", Status: "settled"}, "t")
	assert.Equal(t, "invalid_external_id", domain.InvalidCode(err))
	_, err = h.service.Settle(context.Background(), events.SettlePaymentPayload{ExternalID: "x", Status: "pending"}, "t")
	assert.Equal(t, "invalid_settlement_status", domain.InvalidCode(err))
	_, err = h.service.Settle(context.Background(), events.SettlePaymentPayload{ExternalID: "unknown", Status: "settled"}, "t")
	assert.ErrorIs(t, err, domain.ErrConflict)
	assert.NotErrorIs(t, err, domain.ErrNotFound)
	assert.EqualError(t, err, domain.ErrConflict.Error()+": external id unknown is not bound yet")

	h.repo.FindErr = errors.New("db down")
	_, err = h.service.Settle(context.Background(), events.SettlePaymentPayload{ExternalID: "unknown", Status: "settled"}, "t")
	assert.EqualError(t, err, "db down")
	h.repo.FindErr = nil

	inflight := h.stored(models.MethodTED, models.StatusDebited)
	h.repo.External["ted_2"] = inflight.ID
	_, err = h.service.Settle(context.Background(), events.SettlePaymentPayload{ExternalID: "ted_2", Status: "settled"}, "t")
	assert.ErrorIs(t, err, domain.ErrConflict)
	assert.Contains(t, err.Error(), inflight.ID.String())

	h.repo.GetErrs = []error{errors.New("db down")}
	_, err = h.service.Settle(context.Background(), events.SettlePaymentPayload{ExternalID: "ted_2", Status: "settled"}, "t")
	assert.EqualError(t, err, "db down")
}

func TestSubmitRejectedEmitsNoRefundWhenTheTransitionIsNotApplied(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodPix, models.StatusDebited)
	h.gateway.Submissions = []models.Submission{{Status: models.SubmissionRejected, Reason: "pix_key_not_found"}}
	h.repo.TransitionResults = []tests.TransitionResult{{Applied: false}}

	res, err := h.service.Submit(context.Background(), events.SubmitPaymentPayload{PaymentID: payment.ID.String()}, "t")

	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.Len(t, h.repo.Transitions, 1)
	assert.Equal(t, models.StatusDebited, h.repo.Transitions[0].From)
	assert.Equal(t, models.StatusRefunding, h.repo.Transitions[0].To)
}

func TestSettleRejectedIgnoresCompletedPayments(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodTED, models.StatusCompleted)
	payment.ExternalID = "ted_done"
	h.repo.Put(payment)
	h.repo.External["ted_done"] = payment.ID

	res, err := h.service.Settle(context.Background(), events.SettlePaymentPayload{ExternalID: "ted_done", Status: "rejected", Reason: "invalid_destination"}, "t")

	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.Empty(t, h.repo.Transitions)
	assert.Equal(t, models.StatusCompleted, h.repo.Payments[payment.ID].Status)
}
