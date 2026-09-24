package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/processor"
)

var ErrTouchLost = errors.New("stale payment was already touched")

const reconciliationExhausted = "reconciliation_exhausted"

func (s *PaymentService) ListStale(ctx context.Context, before time.Time, maxAge time.Duration, limit int) ([]*models.Payment, error) {
	return s.repo.ListStale(ctx, before, maxAge, limit)
}

func (s *PaymentService) Reindex(ctx context.Context) (int, error) {
	return s.repo.Reindex(ctx)
}

func (s *PaymentService) touch(ctx context.Context, payment *models.Payment, now time.Time) error {
	applied, err := s.repo.Touch(ctx, payment.ID, payment.Status, payment.UpdatedAt, now)
	if err != nil {
		return err
	}
	if !applied {
		return ErrTouchLost
	}
	return nil
}

func (s *PaymentService) Exhaust(ctx context.Context, payment *models.Payment) (processor.Message, error) {
	if err := s.touch(ctx, payment, s.clock.Now()); err != nil {
		return processor.Message{}, err
	}
	failed := events.NewEvent(events.EventTypes.PaymentCommandFailed, source, events.ErrorPayload{
		ErrorCode:    reconciliationExhausted,
		ErrorMessage: fmt.Sprintf("payment %s is still %s past the reconciliation age", payment.ID, payment.Status),
	}).WithTraceID("reconcile-" + payment.ID.String())
	return processor.Message{Topic: events.Topics.PaymentDLQ, Key: payment.AccountID.String(), Event: failed}, nil
}

func (s *PaymentService) Reconcile(ctx context.Context, payment *models.Payment) (processor.Result, error) {
	now := s.clock.Now()
	if err := s.touch(ctx, payment, now); err != nil {
		return processor.Result{}, err
	}

	trace := "reconcile-" + payment.ID.String()
	switch payment.Status {
	case models.StatusPending:
		return processor.Result{Messages: []processor.Message{toAccount(payment, debitCommand(payment, trace))}}, nil
	case models.StatusDebited:
		return processor.Result{Messages: []processor.Message{
			{Topic: events.Topics.PaymentCommands, Key: payment.ID.String(), Event: submitCommand(payment, trace)},
		}}, nil
	case models.StatusSubmitted:
		return s.reconcileSubmitted(ctx, payment, now, trace)
	case models.StatusRefunding:
		return processor.Result{Messages: []processor.Message{toAccount(payment, refundCommand(payment, trace))}}, nil
	}
	return processor.Result{}, nil
}

func (s *PaymentService) reconcileSubmitted(ctx context.Context, payment *models.Payment, now time.Time, trace string) (processor.Result, error) {
	submission, err := s.gateway.Submit(ctx, payment)
	if err != nil {
		return processor.Result{}, err
	}

	switch submission.Status {
	case models.SubmissionSettled:
		return s.apply(ctx, payment, models.StatusSubmitted, models.StatusCompleted,
			models.Patch{CompletedAt: &now, UpdatedAt: now},
			toEvents(payment, completedEvent(payment, payment.ExternalID, now, trace)))
	case models.SubmissionRejected:
		return s.reject(ctx, payment, models.StatusSubmitted, submission.Reason, now, trace)
	}
	return processor.Result{}, nil
}
