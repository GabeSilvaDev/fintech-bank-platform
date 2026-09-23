package unit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/fintech-bank-platform/transaction-service/internal/app/services"
	"github.com/fintech-bank-platform/transaction-service/tests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func assertReconcileTouch(t *testing.T, h *harness, tx *models.Transaction) {
	require.Len(t, h.repo.Touches, 1)
	assert.Equal(t, tx.ID, h.repo.Touches[0].ID)
	assert.Equal(t, tx.Status, h.repo.Touches[0].Status)
	assert.Equal(t, tx.UpdatedAt, h.repo.Touches[0].Observed)
	assert.Equal(t, now, h.repo.Touches[0].Now)
}

func TestReconcilePendingDepositRequestsCredit(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeDeposit, models.StatusPending)

	res, err := h.service.Reconcile(context.Background(), tx)

	assert.NoError(t, err)
	assertReconcileTouch(t, h, tx)
	assert.Len(t, res.Messages, 1)
	msg := res.Messages[0]
	assert.Equal(t, events.Topics.AccountCommands, msg.Topic)
	assert.Equal(t, tx.AccountID.String(), msg.Key)
	assert.Equal(t, events.EventTypes.CreditAccount, msg.Event.Type)
	assert.Equal(t, "reconcile-"+tx.ID.String(), msg.Event.TraceID)
	payload := msg.Event.Payload.(events.CreditAccountPayload)
	assert.Equal(t, tx.AccountID.String(), payload.AccountID)
	assert.Equal(t, tx.ID.String(), payload.Reference)
	assert.Equal(t, models.StepKey(tx.ID, models.StepCredit), payload.IdempotencyKey)
}

func TestReconcilePendingWithdrawalRequestsDebit(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeWithdrawal, models.StatusPending)

	res, err := h.service.Reconcile(context.Background(), tx)

	assert.NoError(t, err)
	assertReconcileTouch(t, h, tx)
	assert.Len(t, res.Messages, 1)
	msg := res.Messages[0]
	assert.Equal(t, events.Topics.AccountCommands, msg.Topic)
	assert.Equal(t, tx.AccountID.String(), msg.Key)
	assert.Equal(t, events.EventTypes.DebitAccount, msg.Event.Type)
	assert.Equal(t, "reconcile-"+tx.ID.String(), msg.Event.TraceID)
	payload := msg.Event.Payload.(events.DebitAccountPayload)
	assert.Equal(t, tx.AccountID.String(), payload.AccountID)
	assert.Equal(t, tx.ID.String(), payload.Reference)
	assert.Equal(t, models.StepKey(tx.ID, models.StepDebit), payload.IdempotencyKey)
}

func TestReconcilePendingTransferRequestsDebit(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusPending)

	res, err := h.service.Reconcile(context.Background(), tx)

	assert.NoError(t, err)
	assertReconcileTouch(t, h, tx)
	assert.Len(t, res.Messages, 1)
	msg := res.Messages[0]
	assert.Equal(t, events.Topics.AccountCommands, msg.Topic)
	assert.Equal(t, tx.AccountID.String(), msg.Key)
	assert.Equal(t, events.EventTypes.DebitAccount, msg.Event.Type)
	assert.Equal(t, "reconcile-"+tx.ID.String(), msg.Event.TraceID)
	payload := msg.Event.Payload.(events.DebitAccountPayload)
	assert.Equal(t, tx.AccountID.String(), payload.AccountID)
	assert.Equal(t, tx.ID.String(), payload.Reference)
	assert.Equal(t, models.StepKey(tx.ID, models.StepDebit), payload.IdempotencyKey)
}

func TestReconcileDebitedTransferRequestsCreditForCounterparty(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusDebited)

	res, err := h.service.Reconcile(context.Background(), tx)

	assert.NoError(t, err)
	assertReconcileTouch(t, h, tx)
	assert.Len(t, res.Messages, 1)
	msg := res.Messages[0]
	assert.Equal(t, events.Topics.AccountCommands, msg.Topic)
	assert.Equal(t, tx.CounterpartyID.String(), msg.Key)
	assert.Equal(t, events.EventTypes.CreditAccount, msg.Event.Type)
	assert.Equal(t, "reconcile-"+tx.ID.String(), msg.Event.TraceID)
	payload := msg.Event.Payload.(events.CreditAccountPayload)
	assert.Equal(t, tx.CounterpartyID.String(), payload.AccountID)
	assert.Equal(t, tx.ID.String(), payload.Reference)
	assert.Equal(t, models.StepKey(tx.ID, models.StepCredit), payload.IdempotencyKey)
}

func TestReconcileDebitedTransferWithoutCounterpartyIsEmpty(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusDebited)
	tx.CounterpartyID = nil
	h.repo.Put(tx)

	res, err := h.service.Reconcile(context.Background(), tx)

	assert.NoError(t, err)
	assertReconcileTouch(t, h, tx)
	assert.Empty(t, res.Messages)
}

func TestReconcileReversingTransferRequestsReversal(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusReversing)
	tx.FailureReason = "account_not_active"
	h.repo.Put(tx)

	res, err := h.service.Reconcile(context.Background(), tx)

	assert.NoError(t, err)
	assertReconcileTouch(t, h, tx)
	assert.Len(t, res.Messages, 1)
	msg := res.Messages[0]
	assert.Equal(t, events.Topics.AccountCommands, msg.Topic)
	assert.Equal(t, tx.AccountID.String(), msg.Key)
	assert.Equal(t, events.EventTypes.CreditAccount, msg.Event.Type)
	assert.Equal(t, "reconcile-"+tx.ID.String(), msg.Event.TraceID)
	payload := msg.Event.Payload.(events.CreditAccountPayload)
	assert.Equal(t, tx.AccountID.String(), payload.AccountID)
	assert.Equal(t, tx.ID.String(), payload.Reference)
	assert.Equal(t, models.StepKey(tx.ID, models.StepReversal), payload.IdempotencyKey)
}

func TestReconcileTerminalStatusesAreEmpty(t *testing.T) {
	statuses := []models.TransactionStatus{
		models.StatusCompleted,
		models.StatusFailed,
		models.StatusReversed,
		models.StatusReversalFailed,
	}
	for _, status := range statuses {
		h := newHarness()
		tx := h.pending(models.TypeTransfer, status)

		res, err := h.service.Reconcile(context.Background(), tx)

		assert.NoError(t, err, status)
		assertReconcileTouch(t, h, tx)
		assert.Empty(t, res.Messages, status)
	}
}

func TestReconcileTouchUsesTheObservedUpdatedAt(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeDeposit, models.StatusPending)
	tx.UpdatedAt = now.Add(-10 * time.Minute)
	h.repo.Put(tx)

	res, err := h.service.Reconcile(context.Background(), tx)

	assert.NoError(t, err)
	require.Len(t, h.repo.Touches, 1)
	assert.Equal(t, tx.UpdatedAt, h.repo.Touches[0].Observed)
	assert.Equal(t, now, h.repo.Touches[0].Now)
	assert.Len(t, res.Messages, 1)
}

func TestReconcileNotAppliedWhenRowUpdatedSinceObserved(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeDeposit, models.StatusPending)
	observed := *tx
	stored := h.repo.Transactions[tx.ID]
	stored.UpdatedAt = now.Add(time.Minute)

	res, err := h.service.Reconcile(context.Background(), &observed)

	assert.ErrorIs(t, err, services.ErrTouchLost)
	assert.Empty(t, res.Messages)
	require.Len(t, h.repo.Touches, 1)
	assert.Equal(t, observed.UpdatedAt, h.repo.Touches[0].Observed)
}

func TestReconcileLostTouchProducesNoMessages(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeDeposit, models.StatusPending)
	h.repo.TouchResults = []tests.TransitionResult{{Applied: false}}

	res, err := h.service.Reconcile(context.Background(), tx)

	assert.ErrorIs(t, err, services.ErrTouchLost)
	assert.Empty(t, res.Messages)
	require.Len(t, h.repo.Touches, 1)
}

func TestReconcileTouchErrorIsPropagated(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeDeposit, models.StatusPending)
	h.repo.TouchResults = []tests.TransitionResult{{Err: errors.New("db down")}}

	res, err := h.service.Reconcile(context.Background(), tx)

	assert.EqualError(t, err, "db down")
	assert.Empty(t, res.Messages)
}

func TestExhaustTouchesAndBuildsTheDeadLetterAlert(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusDebited)

	alert, err := h.service.Exhaust(context.Background(), tx)

	assert.NoError(t, err)
	assertReconcileTouch(t, h, tx)
	assert.Equal(t, events.Topics.TransactionDLQ, alert.Topic)
	assert.Equal(t, tx.AccountID.String(), alert.Key)
	assert.Equal(t, events.EventTypes.TransactionCommandFailed, alert.Event.Type)
	assert.Equal(t, "transaction-service", alert.Event.Source)
	assert.Equal(t, "reconcile-"+tx.ID.String(), alert.Event.TraceID)
	payload := alert.Event.Payload.(events.ErrorPayload)
	assert.Nil(t, payload.OriginalEvent)
	assert.Equal(t, "reconciliation_exhausted", payload.ErrorCode)
	assert.Contains(t, payload.ErrorMessage, tx.ID.String())
	assert.Contains(t, payload.ErrorMessage, "debited")
	assert.Zero(t, payload.Retries)
}

func TestExhaustReportsALostTouch(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeDeposit, models.StatusPending)
	h.repo.TouchResults = []tests.TransitionResult{{Applied: false}}

	_, err := h.service.Exhaust(context.Background(), tx)

	assert.ErrorIs(t, err, services.ErrTouchLost)
}

func TestExhaustPropagatesTouchErrors(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeDeposit, models.StatusPending)
	h.repo.TouchResults = []tests.TransitionResult{{Err: errors.New("cas timeout")}}

	_, err := h.service.Exhaust(context.Background(), tx)

	assert.EqualError(t, err, "cas timeout")
}
