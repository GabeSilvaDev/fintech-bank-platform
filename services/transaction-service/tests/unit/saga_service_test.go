package unit

import (
	"context"
	"errors"
	"testing"

	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/fintech-bank-platform/transaction-service/internal/app/services"
	"github.com/fintech-bank-platform/transaction-service/tests"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func (h *harness) pending(kind models.TransactionType, status models.TransactionStatus) *models.Transaction {
	tx := &models.Transaction{ID: uuid.New(), Type: kind, Status: status, AccountID: uuid.New(), AmountCents: 3000, Currency: "BRL", IdempotencyKey: "k", CreatedAt: now, UpdatedAt: now}
	if kind == models.TypeTransfer {
		to := uuid.New()
		tx.CounterpartyID = &to
	}
	if status == models.StatusDebited || status == models.StatusReversing {
		from := int64(7000)
		tx.FromBalanceCents = &from
	}
	h.repo.Put(tx)
	return tx
}

func reply(kind string, tx *models.Transaction, step models.Step, balance float64, reason string) services.Reply {
	return services.Reply{Kind: kind, Reference: tx.ID.String(), IdempotencyKey: models.StepKey(tx.ID, step), BalanceAfter: balance, Reason: reason, TraceID: "trace-9"}
}

func TestDepositCreditedCompletes(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeDeposit, models.StatusPending)

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.AccountCredited, tx, models.StepCredit, 130, ""))

	assert.NoError(t, err)
	stored := h.repo.Transactions[tx.ID]
	assert.Equal(t, models.StatusCompleted, stored.Status)
	assert.Equal(t, int64(13000), *stored.ToBalanceCents)
	assert.Equal(t, now, *stored.CompletedAt)
	assert.Len(t, res.Messages, 1)
	msg := res.Messages[0]
	assert.Equal(t, events.Topics.TransactionEvents, msg.Topic)
	assert.Equal(t, tx.AccountID.String(), msg.Key)
	assert.Equal(t, events.EventTypes.TransactionCompleted, msg.Event.Type)
	assert.Equal(t, "trace-9", msg.Event.TraceID)
	payload := msg.Event.Payload.(events.TransactionCompletedPayload)
	assert.Equal(t, tx.ID.String(), payload.TransactionID)
	assert.Equal(t, "deposit", payload.Type)
	assert.Equal(t, 30.0, payload.Amount)
	assert.Equal(t, 130.0, payload.BalanceAfter)
	assert.Equal(t, "completed", payload.Status)
	assert.Equal(t, now, payload.CompletedAt)
}

func TestWithdrawalDebitedCompletesAndRejectionsFail(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeWithdrawal, models.StatusPending)

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.AccountDebited, tx, models.StepDebit, 70, ""))
	assert.NoError(t, err)
	assert.Equal(t, models.StatusCompleted, h.repo.Transactions[tx.ID].Status)
	assert.Equal(t, int64(7000), *h.repo.Transactions[tx.ID].FromBalanceCents)
	assert.Equal(t, 70.0, res.Messages[0].Event.Payload.(events.TransactionCompletedPayload).BalanceAfter)

	rejected := h.pending(models.TypeWithdrawal, models.StatusPending)
	res, err = h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.DebitRejected, rejected, models.StepDebit, 0, "insufficient_funds"))
	assert.NoError(t, err)
	assert.Equal(t, models.StatusFailed, h.repo.Transactions[rejected.ID].Status)
	assert.Equal(t, "insufficient_funds", h.repo.Transactions[rejected.ID].FailureReason)
	assert.Equal(t, events.EventTypes.TransactionFailed, res.Messages[0].Event.Type)
	failed := res.Messages[0].Event.Payload.(events.TransactionFailedPayload)
	assert.Equal(t, "insufficient_funds", failed.Reason)
	assert.Equal(t, "withdrawal", failed.Type)
	assert.Equal(t, now, failed.FailedAt)

	deposit := h.pending(models.TypeDeposit, models.StatusPending)
	res, err = h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.CreditRejected, deposit, models.StepCredit, 0, "account_not_active"))
	assert.NoError(t, err)
	assert.Equal(t, models.StatusFailed, h.repo.Transactions[deposit.ID].Status)
	assert.Equal(t, "account_not_active", res.Messages[0].Event.Payload.(events.TransactionFailedPayload).Reason)
}

func TestTransferHappyPath(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusPending)

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.AccountDebited, tx, models.StepDebit, 70, ""))
	assert.NoError(t, err)
	assert.Equal(t, models.StatusDebited, h.repo.Transactions[tx.ID].Status)
	assert.Equal(t, int64(7000), *h.repo.Transactions[tx.ID].FromBalanceCents)
	assert.Len(t, res.Messages, 1)
	assert.Equal(t, events.Topics.AccountCommands, res.Messages[0].Topic)
	assert.Equal(t, tx.CounterpartyID.String(), res.Messages[0].Key)
	credit := res.Messages[0].Event.Payload.(events.CreditAccountPayload)
	assert.Equal(t, tx.CounterpartyID.String(), credit.AccountID)
	assert.Equal(t, 30.0, credit.Amount)
	assert.Equal(t, tx.ID.String(), credit.Reference)
	assert.Equal(t, models.StepKey(tx.ID, models.StepCredit), credit.IdempotencyKey)
	assert.Equal(t, "trace-9", res.Messages[0].Event.TraceID)

	res, err = h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.AccountCredited, tx, models.StepCredit, 30, ""))
	assert.NoError(t, err)
	stored := h.repo.Transactions[tx.ID]
	assert.Equal(t, models.StatusCompleted, stored.Status)
	assert.Equal(t, int64(3000), *stored.ToBalanceCents)
	assert.Equal(t, now, *stored.CompletedAt)
	assert.Equal(t, events.EventTypes.TransferCompleted, res.Messages[0].Event.Type)
	assert.Equal(t, tx.AccountID.String(), res.Messages[0].Key)
	completed := res.Messages[0].Event.Payload.(events.TransferCompletedPayload)
	assert.Equal(t, tx.ID.String(), completed.TransferID)
	assert.Equal(t, tx.AccountID.String(), completed.FromAccountID)
	assert.Equal(t, tx.CounterpartyID.String(), completed.ToAccountID)
	assert.Equal(t, 70.0, completed.FromBalanceAfter)
	assert.Equal(t, 30.0, completed.ToBalanceAfter)
	assert.Equal(t, now, completed.CompletedAt)
}

func TestTransferDebitRejectedFails(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusPending)

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.DebitRejected, tx, models.StepDebit, 0, "insufficient_funds"))

	assert.NoError(t, err)
	assert.Equal(t, models.StatusFailed, h.repo.Transactions[tx.ID].Status)
	assert.Equal(t, events.EventTypes.TransferFailed, res.Messages[0].Event.Type)
	failed := res.Messages[0].Event.Payload.(events.TransferFailedPayload)
	assert.Equal(t, "insufficient_funds", failed.Reason)
	assert.Equal(t, "failed", failed.Status)
	assert.Equal(t, tx.CounterpartyID.String(), failed.ToAccountID)
}

func TestTransferCreditRejectedReversesThenReversed(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusDebited)

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.CreditRejected, tx, models.StepCredit, 0, "account_not_active"))
	assert.NoError(t, err)
	stored := h.repo.Transactions[tx.ID]
	assert.Equal(t, models.StatusReversing, stored.Status)
	assert.Equal(t, "account_not_active", stored.FailureReason)
	assert.Len(t, res.Messages, 1)
	assert.Equal(t, events.Topics.AccountCommands, res.Messages[0].Topic)
	assert.Equal(t, tx.AccountID.String(), res.Messages[0].Key)
	reversal := res.Messages[0].Event.Payload.(events.CreditAccountPayload)
	assert.Equal(t, tx.AccountID.String(), reversal.AccountID)
	assert.Equal(t, models.StepKey(tx.ID, models.StepReversal), reversal.IdempotencyKey)

	res, err = h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.AccountCredited, tx, models.StepReversal, 100, ""))
	assert.NoError(t, err)
	assert.Equal(t, models.StatusReversed, h.repo.Transactions[tx.ID].Status)
	assert.Equal(t, events.EventTypes.TransferFailed, res.Messages[0].Event.Type)
	failed := res.Messages[0].Event.Payload.(events.TransferFailedPayload)
	assert.Equal(t, "reversed", failed.Status)
	assert.Equal(t, "account_not_active", failed.Reason)
}

func TestTransferReversalRejectedNeedsManualHandling(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusReversing)
	tx.FailureReason = "account_not_active"
	h.repo.Put(tx)

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.CreditRejected, tx, models.StepReversal, 0, "account_not_found"))

	assert.NoError(t, err)
	assert.Equal(t, models.StatusReversalFailed, h.repo.Transactions[tx.ID].Status)
	assert.Len(t, res.Messages, 2)
	assert.Equal(t, events.EventTypes.TransferFailed, res.Messages[0].Event.Type)
	assert.Equal(t, "reversal_failed", res.Messages[0].Event.Payload.(events.TransferFailedPayload).Status)
	assert.Equal(t, events.Topics.TransactionDLQ, res.Messages[1].Topic)
	assert.Equal(t, tx.ID.String(), res.Messages[1].Key)
	assert.Equal(t, events.EventTypes.TransactionCommandFailed, res.Messages[1].Event.Type)
	dlq := res.Messages[1].Event.Payload.(events.ErrorPayload)
	assert.Equal(t, "reversal_failed", dlq.ErrorCode)
	assert.Contains(t, dlq.ErrorMessage, tx.ID.String())
	assert.Contains(t, dlq.ErrorMessage, "account_not_found")
}

func TestApplyIgnoresUnrelatedOrStaleReplies(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusPending)
	other := uuid.New()

	neverReachesRepo := []services.Reply{
		{Kind: events.EventTypes.AccountCredited, Reference: "", IdempotencyKey: ""},
		{Kind: events.EventTypes.AccountCredited, Reference: "nope", IdempotencyKey: models.StepKey(tx.ID, models.StepCredit)},
		{Kind: events.EventTypes.AccountCredited, Reference: tx.ID.String(), IdempotencyKey: "free-form-key"},
		{Kind: events.EventTypes.AccountCredited, Reference: tx.ID.String(), IdempotencyKey: models.StepKey(other, models.StepCredit)},
		{Kind: events.EventTypes.AccountCredited, Reference: other.String(), IdempotencyKey: models.StepKey(other, models.StepCredit)},
		reply(events.EventTypes.AccountDebited, tx, models.StepCredit, 1, ""),
		reply(events.EventTypes.AccountCreated, tx, models.StepDebit, 1, ""),
	}
	for i, r := range neverReachesRepo {
		res, err := h.service.ApplyAccountEvent(context.Background(), r)
		assert.NoError(t, err, i)
		assert.Empty(t, res.Messages, i)
	}
	assert.Equal(t, models.StatusPending, h.repo.Transactions[tx.ID].Status)
	assert.Empty(t, h.repo.Transitions)

	staleButAttempted := []services.Reply{
		reply(events.EventTypes.AccountCredited, tx, models.StepCredit, 1, ""),
		reply(events.EventTypes.AccountCredited, tx, models.StepReversal, 1, ""),
	}
	for i, r := range staleButAttempted {
		res, err := h.service.ApplyAccountEvent(context.Background(), r)
		assert.NoError(t, err, i)
		assert.Empty(t, res.Messages, i)
	}
	assert.Equal(t, models.StatusPending, h.repo.Transactions[tx.ID].Status)
	assert.Len(t, h.repo.Transitions, len(staleButAttempted))
	for i, call := range h.repo.Transitions {
		assert.NotEqual(t, models.StatusPending, call.From, i)
	}
}

func TestApplyTreatsLostTransitionAsAlreadyApplied(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeDeposit, models.StatusPending)
	h.repo.TransitionResults = []tests.TransitionResult{{Applied: false}}

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.AccountCredited, tx, models.StepCredit, 130, ""))

	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.Len(t, h.repo.Transitions, 1)
}

func TestDepositIgnoresDebitRejection(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeDeposit, models.StatusPending)

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.DebitRejected, tx, models.StepDebit, 0, "insufficient_funds"))

	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.Equal(t, models.StatusPending, h.repo.Transactions[tx.ID].Status)
	assert.Empty(t, h.repo.Transactions[tx.ID].FailureReason)
	assert.Empty(t, h.repo.Transitions)
}

func TestOnDebitedIgnoresNonWithdrawalNonTransferTypes(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeDeposit, models.StatusPending)

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.AccountDebited, tx, models.StepDebit, 70, ""))

	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
}

func TestOnCreditedIgnoresNonDepositNonTransferTypes(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeWithdrawal, models.StatusPending)

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.AccountCredited, tx, models.StepCredit, 70, ""))

	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
}

func TestOnCreditRejectedIgnoresNonDepositNonTransferTypes(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeWithdrawal, models.StatusPending)

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.CreditRejected, tx, models.StepCredit, 0, "reason"))

	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
}

func TestOnDebitedTransferWithoutCounterpartyIsIgnored(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusPending)
	tx.CounterpartyID = nil
	h.repo.Put(tx)

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.AccountDebited, tx, models.StepDebit, 70, ""))

	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.Empty(t, h.repo.Transitions)
}

func TestOnCreditedTransferWithoutCounterpartyIsIgnored(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusDebited)
	tx.CounterpartyID = nil
	h.repo.Put(tx)

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.AccountCredited, tx, models.StepCredit, 30, ""))

	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.Empty(t, h.repo.Transitions)
}

func TestOnCreditRejectedTransferWithoutCounterpartyIsIgnored(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusDebited)
	tx.CounterpartyID = nil
	h.repo.Put(tx)

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.CreditRejected, tx, models.StepCredit, 0, "reason"))

	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.Empty(t, h.repo.Transitions)
}

func TestOnDebitedTransferAlreadyAppliedIsIgnored(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusPending)
	h.repo.TransitionResults = []tests.TransitionResult{{Applied: false}}

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.AccountDebited, tx, models.StepDebit, 70, ""))

	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.Len(t, h.repo.Transitions, 1)
}

func TestOnCreditRejectedTransferAlreadyAppliedIsIgnored(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusDebited)
	h.repo.TransitionResults = []tests.TransitionResult{{Applied: false}}

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.CreditRejected, tx, models.StepCredit, 0, "reason"))

	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.Len(t, h.repo.Transitions, 1)
}

func TestFailAlreadyAppliedIsIgnored(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeWithdrawal, models.StatusPending)
	h.repo.TransitionResults = []tests.TransitionResult{{Applied: false}}

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.DebitRejected, tx, models.StepDebit, 0, "reason"))

	assert.NoError(t, err)
	assert.Empty(t, res.Messages)
	assert.Len(t, h.repo.Transitions, 1)
}

func TestTransferCompletedWithoutStoredFromBalance(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeTransfer, models.StatusDebited)
	tx.FromBalanceCents = nil
	h.repo.Put(tx)

	res, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.AccountCredited, tx, models.StepCredit, 30, ""))

	assert.NoError(t, err)
	completed := res.Messages[0].Event.Payload.(events.TransferCompletedPayload)
	assert.Equal(t, 0.0, completed.FromBalanceAfter)
}

func TestApplyPropagatesRepositoryErrors(t *testing.T) {
	h := newHarness()
	tx := h.pending(models.TypeDeposit, models.StatusPending)

	h.repo.GetErrs = []error{errors.New("db down")}
	_, err := h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.AccountCredited, tx, models.StepCredit, 1, ""))
	assert.EqualError(t, err, "db down")

	h.repo.TransitionResults = []tests.TransitionResult{{Err: errors.New("db down")}}
	_, err = h.service.ApplyAccountEvent(context.Background(), reply(events.EventTypes.AccountCredited, tx, models.StepCredit, 1, ""))
	assert.EqualError(t, err, "db down")
}
