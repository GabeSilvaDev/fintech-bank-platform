package services

import (
	"context"
	"errors"
	"time"

	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
)

var ErrTouchLost = errors.New("stale transaction was already touched")

func (s *TransactionService) ListStale(ctx context.Context, before time.Time, limit int) ([]*models.Transaction, error) {
	return s.repo.ListStale(ctx, before, limit)
}

func (s *TransactionService) Reconcile(ctx context.Context, tx *models.Transaction) (processor.Result, error) {
	now := s.clock.Now()
	applied, err := s.repo.Touch(ctx, tx.ID, tx.Status, tx.UpdatedAt, now)
	if err != nil {
		return processor.Result{}, err
	}
	if !applied {
		return processor.Result{}, ErrTouchLost
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
