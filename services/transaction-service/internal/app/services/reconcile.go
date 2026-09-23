package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
)

var ErrTouchLost = errors.New("stale transaction was already touched")

const reconciliationExhausted = "reconciliation_exhausted"

func (s *TransactionService) ListStale(ctx context.Context, before time.Time, maxAge time.Duration, limit int) ([]*models.Transaction, error) {
	return s.repo.ListStale(ctx, before, maxAge, limit)
}

func (s *TransactionService) touch(ctx context.Context, tx *models.Transaction) error {
	applied, err := s.repo.Touch(ctx, tx.ID, tx.Status, tx.UpdatedAt, s.clock.Now())
	if err != nil {
		return err
	}
	if !applied {
		return ErrTouchLost
	}
	return nil
}

func (s *TransactionService) Exhaust(ctx context.Context, tx *models.Transaction) (processor.Message, error) {
	if err := s.touch(ctx, tx); err != nil {
		return processor.Message{}, err
	}
	failed := events.NewEvent(events.EventTypes.TransactionCommandFailed, source, events.ErrorPayload{
		ErrorCode:    reconciliationExhausted,
		ErrorMessage: fmt.Sprintf("transaction %s is still %s past the reconciliation age", tx.ID, tx.Status),
	}).WithTraceID("reconcile-" + tx.ID.String())
	return processor.Message{Topic: events.Topics.TransactionDLQ, Key: tx.AccountID.String(), Event: failed}, nil
}

func (s *TransactionService) Reconcile(ctx context.Context, tx *models.Transaction) (processor.Result, error) {
	if err := s.touch(ctx, tx); err != nil {
		return processor.Result{}, err
	}

	trace := "reconcile-" + tx.ID.String()
	switch {
	case tx.Status == models.StatusPending && tx.Type == models.TypeDeposit:
		return processor.Result{Messages: []processor.Message{
			{Topic: events.Topics.AccountCommands, Key: tx.AccountID.String(), Event: creditCommand(tx, tx.AccountID, models.StepCredit, trace)},
		}}, nil
	case tx.Status == models.StatusPending:
		return processor.Result{Messages: []processor.Message{
			{Topic: events.Topics.AccountCommands, Key: tx.AccountID.String(), Event: debitCommand(tx, tx.AccountID, trace)},
		}}, nil
	case tx.Status == models.StatusDebited && tx.Type == models.TypeTransfer && tx.CounterpartyID != nil:
		return processor.Result{Messages: []processor.Message{
			{Topic: events.Topics.AccountCommands, Key: tx.CounterpartyID.String(), Event: creditCommand(tx, *tx.CounterpartyID, models.StepCredit, trace)},
		}}, nil
	case tx.Status == models.StatusReversing && tx.Type == models.TypeTransfer:
		return processor.Result{Messages: []processor.Message{
			{Topic: events.Topics.AccountCommands, Key: tx.AccountID.String(), Event: creditCommand(tx, tx.AccountID, models.StepReversal, trace)},
		}}, nil
	}
	return processor.Result{}, nil
}
