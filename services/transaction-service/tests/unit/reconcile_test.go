package unit

import (
	"context"
	"errors"
	"testing"

	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/fintech-bank-platform/transaction-service/tests"
	"github.com/stretchr/testify/assert"
)

func assertReconcileTouch(t *testing.T, h *harness, tx *models.Transaction) {
	assert.Len(t, h.repo.Transitions, 1)
	assert.Equal(t, tx.ID, h.repo.Transitions[0].ID)
	assert.Equal(t, tx.Status, h.repo.Transitions[0].From)
	assert.Equal(t, tx.Status, h.repo.Transitions[0].To)
	assert.Equal(t, now, h.repo.Transitions[0].Patch.UpdatedAt)
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

func TestReconcileLostTouchProducesNoMessages(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeDeposit, models.StatusPending)
	h.repo.TransitionResults = []tests.TransitionResult{{Applied: false}}

	res, err := h.service.Reconcile(context.Background(), tx)

	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.Len(t, h.repo.Transitions, 1)
}

func TestReconcileTouchErrorIsPropagated(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeDeposit, models.StatusPending)
	h.repo.TransitionResults = []tests.TransitionResult{{Err: errors.New("db down")}}

	res, err := h.service.Reconcile(context.Background(), tx)

	assert.EqualError(t, err, "db down")
	assert.Empty(t, res.Messages)
}
