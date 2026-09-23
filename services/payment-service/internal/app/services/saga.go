package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/google/uuid"
)

const defaultRejection = "rejected_by_provider"

type Reply struct {
	Kind           string
	Reference      string
	IdempotencyKey string
	BalanceAfter   float64
	Reason         string
	TraceID        string
}

func (s *PaymentService) ApplyAccountEvent(ctx context.Context, reply Reply) (processor.Result, error) {
	id, ok := models.ParseReference(reply.Reference)
	if !ok {
		return processor.Result{}, nil
	}
	keyID, step, ok := models.ParseStepKey(reply.IdempotencyKey)
	if !ok || keyID != id {
		return processor.Result{}, nil
	}
	payment, err := s.repo.Get(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return processor.Result{}, nil
	}
	if err != nil {
		return processor.Result{}, err
	}
	now := s.clock.Now()
	trace := reply.TraceID

	switch {
	case reply.Kind == events.EventTypes.AccountDebited && step == models.StepDebit:
		balance := domain.Cents(reply.BalanceAfter)
		return s.apply(ctx, payment, models.StatusPending, models.StatusDebited,
			models.Patch{BalanceAfterCents: &balance, UpdatedAt: now},
			processor.Message{Topic: events.Topics.PaymentCommands, Key: payment.ID.String(), Event: submitCommand(payment, trace)})
	case reply.Kind == events.EventTypes.DebitRejected && step == models.StepDebit:
		return s.apply(ctx, payment, models.StatusPending, models.StatusFailed,
			models.Patch{FailureReason: &reply.Reason, UpdatedAt: now},
			toEvents(payment, failedEvent(payment, reply.Reason, models.StatusFailed, now, trace)))
	case reply.Kind == events.EventTypes.AccountCredited && step == models.StepRefund:
		balance := domain.Cents(reply.BalanceAfter)
		return s.apply(ctx, payment, models.StatusRefunding, models.StatusRefunded,
			models.Patch{BalanceAfterCents: &balance, UpdatedAt: now},
			toEvents(payment, failedEvent(payment, payment.FailureReason, models.StatusRefunded, now, trace)))
	case reply.Kind == events.EventTypes.CreditRejected && step == models.StepRefund:
		return s.apply(ctx, payment, models.StatusRefunding, models.StatusRefundFailed,
			models.Patch{UpdatedAt: now},
			toEvents(payment, failedEvent(payment, payment.FailureReason, models.StatusRefundFailed, now, trace)),
			processor.Message{Topic: events.Topics.PaymentDLQ, Key: payment.ID.String(), Event: refundFailedAlert(payment, reply.Reason, trace)})
	}
	return processor.Result{}, nil
}

func (s *PaymentService) Submit(ctx context.Context, cmd events.SubmitPaymentPayload, trace string) (processor.Result, error) {
	id, err := uuid.Parse(cmd.PaymentID)
	if err != nil {
		return processor.Result{}, domain.Invalid("invalid_payment_id", "payment_id must be a uuid")
	}
	payment, err := s.repo.Get(ctx, id)
	if err != nil {
		return processor.Result{}, err
	}
	if payment.Status != models.StatusDebited {
		return processor.Result{}, nil
	}

	submission, err := s.gateway.Submit(ctx, payment)
	if err != nil {
		return processor.Result{}, err
	}
	now := s.clock.Now()

	switch submission.Status {
	case models.SubmissionSettled:
		return s.apply(ctx, payment, models.StatusDebited, models.StatusCompleted,
			models.Patch{ExternalID: &submission.ExternalID, CompletedAt: &now, UpdatedAt: now},
			toEvents(payment, completedEvent(payment, submission.ExternalID, now, trace)))
	case models.SubmissionPending:
		if err := s.repo.BindExternalID(ctx, submission.ExternalID, payment.ID); err != nil {
			return processor.Result{}, err
		}
		return s.apply(ctx, payment, models.StatusDebited, models.StatusSubmitted,
			models.Patch{ExternalID: &submission.ExternalID, UpdatedAt: now},
			toEvents(payment, processedEvent(payment, submission.ExternalID, now, trace)))
	case models.SubmissionRejected:
		return s.reject(ctx, payment, models.StatusDebited, submission.Reason, now, trace)
	}
	return processor.Result{}, fmt.Errorf("unexpected submission status %q", submission.Status)
}

func (s *PaymentService) Settle(ctx context.Context, cmd events.SettlePaymentPayload, trace string) (processor.Result, error) {
	externalID := strings.TrimSpace(cmd.ExternalID)
	if externalID == "" {
		return processor.Result{}, domain.Invalid("invalid_external_id", "external_id is required")
	}
	status, ok := models.ParseSettlementStatus(cmd.Status)
	if !ok {
		return processor.Result{}, domain.Invalid("invalid_settlement_status", "status must be settled or rejected")
	}
	id, err := s.repo.FindByExternalID(ctx, externalID)
	if err != nil {
		return processor.Result{}, err
	}
	payment, err := s.repo.Get(ctx, id)
	if err != nil {
		return processor.Result{}, err
	}
	now := s.clock.Now()

	switch payment.Status {
	case models.StatusDebited:
		return processor.Result{}, fmt.Errorf("%w: payment %s is still being submitted", domain.ErrConflict, payment.ID)
	case models.StatusSubmitted:
		if status == models.SubmissionSettled {
			return s.apply(ctx, payment, models.StatusSubmitted, models.StatusCompleted,
				models.Patch{CompletedAt: &now, UpdatedAt: now},
				toEvents(payment, completedEvent(payment, payment.ExternalID, now, trace)))
		}
		return s.reject(ctx, payment, models.StatusSubmitted, cmd.Reason, now, trace)
	}
	return processor.Result{}, nil
}

func (s *PaymentService) reject(ctx context.Context, payment *models.Payment, from models.Status, reason string, now time.Time, trace string) (processor.Result, error) {
	if strings.TrimSpace(reason) == "" {
		reason = defaultRejection
	}
	return s.apply(ctx, payment, from, models.StatusRefunding,
		models.Patch{FailureReason: &reason, UpdatedAt: now},
		toAccount(payment, refundCommand(payment, trace)))
}

func (s *PaymentService) apply(ctx context.Context, payment *models.Payment, from, to models.Status, patch models.Patch, messages ...processor.Message) (processor.Result, error) {
	applied, err := s.repo.Transition(ctx, payment.ID, from, to, patch)
	if err != nil || !applied {
		return processor.Result{}, err
	}
	return processor.Result{Messages: messages}, nil
}
