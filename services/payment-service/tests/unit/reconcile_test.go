package unit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/payment-service/internal/app/services"
	"github.com/fintech-bank-platform/payment-service/tests"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertReconcileTouch(t *testing.T, h *harness, payment *models.Payment) {
	require.Len(t, h.repo.Touches, 1)
	assert.Equal(t, payment.ID, h.repo.Touches[0].ID)
	assert.Equal(t, payment.Status, h.repo.Touches[0].Status)
	assert.Equal(t, payment.UpdatedAt, h.repo.Touches[0].Observed)
	assert.Equal(t, now, h.repo.Touches[0].Now)
}

func TestReconcilePendingRequestsDebit(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodPix, models.StatusPending)

	res, err := h.service.Reconcile(context.Background(), payment)

	assert.NoError(t, err)
	assertReconcileTouch(t, h, payment)
	assert.Len(t, res.Messages, 1)
	msg := res.Messages[0]
	assert.Equal(t, events.Topics.AccountCommands, msg.Topic)
	assert.Equal(t, payment.AccountID.String(), msg.Key)
	assert.Equal(t, events.EventTypes.DebitAccount, msg.Event.Type)
	assert.Equal(t, "reconcile-"+payment.ID.String(), msg.Event.TraceID)
	debit := msg.Event.Payload.(events.DebitAccountPayload)
	assert.Equal(t, payment.AccountID.String(), debit.AccountID)
	assert.Equal(t, models.Reference(payment.ID), debit.Reference)
	assert.Equal(t, models.StepKey(payment.ID, models.StepDebit), debit.IdempotencyKey)
}

func TestReconcileDebitedRequestsSubmission(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodTED, models.StatusDebited)

	res, err := h.service.Reconcile(context.Background(), payment)

	assert.NoError(t, err)
	assertReconcileTouch(t, h, payment)
	assert.Len(t, res.Messages, 1)
	msg := res.Messages[0]
	assert.Equal(t, events.Topics.PaymentCommands, msg.Topic)
	assert.Equal(t, payment.ID.String(), msg.Key)
	assert.Equal(t, events.EventTypes.SubmitPayment, msg.Event.Type)
	assert.Equal(t, "reconcile-"+payment.ID.String(), msg.Event.TraceID)
	assert.Equal(t, payment.ID.String(), msg.Event.Payload.(events.SubmitPaymentPayload).PaymentID)
	assert.Empty(t, h.gateway.Calls)
}

func TestReconcileRefundingRequestsCredit(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodPix, models.StatusRefunding)
	payment.FailureReason = "pix_key_not_found"
	h.repo.Put(payment)

	res, err := h.service.Reconcile(context.Background(), payment)

	assert.NoError(t, err)
	assertReconcileTouch(t, h, payment)
	assert.Len(t, res.Messages, 1)
	msg := res.Messages[0]
	assert.Equal(t, events.Topics.AccountCommands, msg.Topic)
	assert.Equal(t, payment.AccountID.String(), msg.Key)
	assert.Equal(t, events.EventTypes.CreditAccount, msg.Event.Type)
	assert.Equal(t, "reconcile-"+payment.ID.String(), msg.Event.TraceID)
	credit := msg.Event.Payload.(events.CreditAccountPayload)
	assert.Equal(t, payment.AccountID.String(), credit.AccountID)
	assert.Equal(t, models.Reference(payment.ID), credit.Reference)
	assert.Equal(t, models.StepKey(payment.ID, models.StepRefund), credit.IdempotencyKey)
}

func TestReconcileSubmittedSettledCompletes(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodTED, models.StatusSubmitted)
	payment.ExternalID = "ted_1"
	h.repo.Put(payment)
	h.gateway.Submissions = []models.Submission{{ExternalID: "ted_1", Status: models.SubmissionSettled}}

	res, err := h.service.Reconcile(context.Background(), payment)

	assert.NoError(t, err)
	assertReconcileTouch(t, h, payment)
	assert.Equal(t, payment.ID, h.gateway.Calls[0].ID)
	stored := h.repo.Payments[payment.ID]
	assert.Equal(t, models.StatusCompleted, stored.Status)
	assert.Equal(t, now, *stored.CompletedAt)
	assert.Len(t, res.Messages, 1)
	msg := res.Messages[0]
	assert.Equal(t, events.Topics.PaymentEvents, msg.Topic)
	assert.Equal(t, events.EventTypes.PaymentCompleted, msg.Event.Type)
	assert.Equal(t, "reconcile-"+payment.ID.String(), msg.Event.TraceID)
	completed := msg.Event.Payload.(events.PaymentCompletedPayload)
	assert.Equal(t, "ted_1", completed.ExternalID)
	assert.Equal(t, now, completed.CompletedAt)
}

func TestReconcileSubmittedRejectedRefunds(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodPix, models.StatusSubmitted)
	h.gateway.Submissions = []models.Submission{{Status: models.SubmissionRejected, Reason: "pix_key_not_found"}}

	res, err := h.service.Reconcile(context.Background(), payment)

	assert.NoError(t, err)
	assertReconcileTouch(t, h, payment)
	stored := h.repo.Payments[payment.ID]
	assert.Equal(t, models.StatusRefunding, stored.Status)
	assert.Equal(t, "pix_key_not_found", stored.FailureReason)
	assert.Len(t, res.Messages, 1)
	msg := res.Messages[0]
	assert.Equal(t, events.Topics.AccountCommands, msg.Topic)
	assert.Equal(t, payment.AccountID.String(), msg.Key)
	assert.Equal(t, events.EventTypes.CreditAccount, msg.Event.Type)
	assert.Equal(t, "reconcile-"+payment.ID.String(), msg.Event.TraceID)
	refund := msg.Event.Payload.(events.CreditAccountPayload)
	assert.Equal(t, models.StepKey(payment.ID, models.StepRefund), refund.IdempotencyKey)
}

func TestReconcileSubmittedPendingProducesNoMessages(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodBoleto, models.StatusSubmitted)
	payment.ExternalID = "boleto_1"
	h.repo.Put(payment)
	h.gateway.Submissions = []models.Submission{{ExternalID: "boleto_1", Status: models.SubmissionPending}}

	res, err := h.service.Reconcile(context.Background(), payment)

	assert.NoError(t, err)
	assertReconcileTouch(t, h, payment)
	assert.Equal(t, payment.ID, h.gateway.Calls[0].ID)
	assert.Equal(t, models.StatusSubmitted, h.repo.Payments[payment.ID].Status)
	assert.Empty(t, res.Messages)
}

func TestReconcileSubmittedGatewayErrorIsPropagated(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodTED, models.StatusSubmitted)
	h.gateway.Errs = []error{errors.New("provider timeout")}

	res, err := h.service.Reconcile(context.Background(), payment)

	assert.EqualError(t, err, "provider timeout")
	assert.Empty(t, res.Messages)
	assertReconcileTouch(t, h, payment)
}

func TestReconcileTerminalStatusesAreEmpty(t *testing.T) {
	statuses := []models.Status{
		models.StatusCompleted,
		models.StatusFailed,
		models.StatusRefunded,
		models.StatusRefundFailed,
	}
	for _, status := range statuses {
		h := newHarness()
		payment := h.stored(models.MethodPix, status)

		res, err := h.service.Reconcile(context.Background(), payment)

		assert.NoError(t, err, status)
		assertReconcileTouch(t, h, payment)
		assert.Empty(t, res.Messages, status)
	}
}

func TestReconcileTouchUsesTheObservedUpdatedAt(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodPix, models.StatusPending)
	payment.UpdatedAt = now.Add(-10 * time.Minute)
	h.repo.Put(payment)

	res, err := h.service.Reconcile(context.Background(), payment)

	assert.NoError(t, err)
	require.Len(t, h.repo.Touches, 1)
	assert.Equal(t, payment.UpdatedAt, h.repo.Touches[0].Observed)
	assert.Equal(t, now, h.repo.Touches[0].Now)
	assert.Len(t, res.Messages, 1)
}

func TestReconcileNotAppliedWhenRowUpdatedSinceObserved(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodPix, models.StatusPending)
	observed := *payment
	stored := h.repo.Payments[payment.ID]
	stored.UpdatedAt = now.Add(time.Minute)

	res, err := h.service.Reconcile(context.Background(), &observed)

	assert.ErrorIs(t, err, services.ErrTouchLost)
	assert.Empty(t, res.Messages)
	require.Len(t, h.repo.Touches, 1)
	assert.Equal(t, observed.UpdatedAt, h.repo.Touches[0].Observed)
}

func TestReconcileLostTouchProducesNoMessages(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodPix, models.StatusPending)
	h.repo.TouchResults = []tests.TransitionResult{{Applied: false}}

	res, err := h.service.Reconcile(context.Background(), payment)

	assert.ErrorIs(t, err, services.ErrTouchLost)
	assert.Empty(t, res.Messages)
	require.Len(t, h.repo.Touches, 1)
}

func TestReconcileTouchErrorIsPropagated(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodPix, models.StatusPending)
	h.repo.TouchResults = []tests.TransitionResult{{Err: errors.New("db down")}}

	res, err := h.service.Reconcile(context.Background(), payment)

	assert.EqualError(t, err, "db down")
	assert.Empty(t, res.Messages)
}

func TestExhaustTouchesAndBuildsTheDeadLetterAlert(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodPix, models.StatusRefunding)

	alert, err := h.service.Exhaust(context.Background(), payment)

	assert.NoError(t, err)
	assertReconcileTouch(t, h, payment)
	assert.Empty(t, h.gateway.Calls)
	assert.Equal(t, events.Topics.PaymentDLQ, alert.Topic)
	assert.Equal(t, payment.AccountID.String(), alert.Key)
	assert.Equal(t, events.EventTypes.PaymentCommandFailed, alert.Event.Type)
	assert.Equal(t, "payment-service", alert.Event.Source)
	assert.Equal(t, "reconcile-"+payment.ID.String(), alert.Event.TraceID)
	payload := alert.Event.Payload.(events.ErrorPayload)
	assert.Nil(t, payload.OriginalEvent)
	assert.Equal(t, "reconciliation_exhausted", payload.ErrorCode)
	assert.Contains(t, payload.ErrorMessage, payment.ID.String())
	assert.Contains(t, payload.ErrorMessage, "refunding")
	assert.Zero(t, payload.Retries)
}

func TestExhaustReportsALostTouch(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodPix, models.StatusPending)
	h.repo.TouchResults = []tests.TransitionResult{{Applied: false}}

	_, err := h.service.Exhaust(context.Background(), payment)

	assert.ErrorIs(t, err, services.ErrTouchLost)
}

func TestExhaustPropagatesTouchErrors(t *testing.T) {
	h := newHarness()
	payment := h.stored(models.MethodPix, models.StatusPending)
	h.repo.TouchResults = []tests.TransitionResult{{Err: errors.New("cas timeout")}}

	_, err := h.service.Exhaust(context.Background(), payment)

	assert.EqualError(t, err, "cas timeout")
}
