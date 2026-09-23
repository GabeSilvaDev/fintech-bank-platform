package services

import (
	"context"
	"errors"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/processor"
)

var ErrTouchLost = errors.New("stale payment was already touched")

func (s *PaymentService) ListStale(ctx context.Context, before time.Time, limit int) ([]*models.Payment, error) {
	return s.repo.ListStale(ctx, before, limit)
}

func (s *PaymentService) Reconcile(ctx context.Context, payment *models.Payment) (processor.Result, error) {
	now := s.clock.Now()
	applied, err := s.repo.Touch(ctx, payment.ID, payment.Status, payment.UpdatedAt, now)
	if err != nil {
		return processor.Result{}, err
	}
	if !applied {
		return processor.Result{}, ErrTouchLost
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
