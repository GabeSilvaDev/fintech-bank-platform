package services

import (
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/processor"
)

func toEvents(payment *models.Payment, event *events.Event) processor.Message {
	return processor.Message{Topic: events.Topics.PaymentEvents, Key: payment.AccountID.String(), Event: event}
}

func toAccount(payment *models.Payment, event *events.Event) processor.Message {
	return processor.Message{Topic: events.Topics.AccountCommands, Key: payment.AccountID.String(), Event: event}
}

func createdEvent(payment *models.Payment, trace string) *events.Event {
	return events.NewPaymentEvent(events.EventTypes.PaymentCreated, events.PaymentCreatedPayload{
		PaymentID:      payment.ID.String(),
		AccountID:      payment.AccountID.String(),
		PaymentMethod:  string(payment.Method),
		Amount:         domain.FromCents(payment.AmountCents),
		Currency:       payment.Currency,
		Recipient:      payment.Recipient,
		Description:    payment.Description,
		IdempotencyKey: payment.IdempotencyKey,
		CreatedAt:      payment.CreatedAt,
	}).WithTraceID(trace)
}

func debitCommand(payment *models.Payment, trace string) *events.Event {
	return events.NewEvent(events.EventTypes.DebitAccount, source, events.DebitAccountPayload{
		AccountID:      payment.AccountID.String(),
		Amount:         domain.FromCents(payment.AmountCents),
		Currency:       payment.Currency,
		Reference:      models.Reference(payment.ID),
		IdempotencyKey: models.StepKey(payment.ID, models.StepDebit),
	}).WithTraceID(trace)
}

func refundCommand(payment *models.Payment, trace string) *events.Event {
	return events.NewEvent(events.EventTypes.CreditAccount, source, events.CreditAccountPayload{
		AccountID:      payment.AccountID.String(),
		Amount:         domain.FromCents(payment.AmountCents),
		Currency:       payment.Currency,
		Reference:      models.Reference(payment.ID),
		IdempotencyKey: models.StepKey(payment.ID, models.StepRefund),
	}).WithTraceID(trace)
}

func submitCommand(payment *models.Payment, trace string) *events.Event {
	return events.NewEvent(events.EventTypes.SubmitPayment, source, events.SubmitPaymentPayload{PaymentID: payment.ID.String()}).WithTraceID(trace)
}

func processedEvent(payment *models.Payment, externalID string, now time.Time, trace string) *events.Event {
	return events.NewPaymentEvent(events.EventTypes.PaymentProcessed, events.PaymentProcessedPayload{
		PaymentID:     payment.ID.String(),
		AccountID:     payment.AccountID.String(),
		PaymentMethod: string(payment.Method),
		Amount:        domain.FromCents(payment.AmountCents),
		Currency:      payment.Currency,
		ExternalID:    externalID,
		Status:        string(models.StatusSubmitted),
		ProcessedAt:   now,
	}).WithTraceID(trace)
}

func completedEvent(payment *models.Payment, externalID string, now time.Time, trace string) *events.Event {
	return events.NewPaymentEvent(events.EventTypes.PaymentCompleted, events.PaymentCompletedPayload{
		PaymentID:     payment.ID.String(),
		AccountID:     payment.AccountID.String(),
		PaymentMethod: string(payment.Method),
		Amount:        domain.FromCents(payment.AmountCents),
		Currency:      payment.Currency,
		Status:        string(models.StatusCompleted),
		ExternalID:    externalID,
		CompletedAt:   now,
	}).WithTraceID(trace)
}

func failedEvent(payment *models.Payment, reason string, status models.Status, now time.Time, trace string) *events.Event {
	return events.NewPaymentEvent(events.EventTypes.PaymentFailed, events.PaymentFailedPayload{
		PaymentID:     payment.ID.String(),
		AccountID:     payment.AccountID.String(),
		PaymentMethod: string(payment.Method),
		Amount:        domain.FromCents(payment.AmountCents),
		Currency:      payment.Currency,
		Reason:        reason,
		Status:        string(status),
		FailedAt:      now,
	}).WithTraceID(trace)
}

func refundFailedAlert(payment *models.Payment, reason, trace string) *events.Event {
	return events.NewPaymentEvent(events.EventTypes.PaymentCommandFailed, events.ErrorPayload{
		ErrorCode:    "refund_failed",
		ErrorMessage: "payment " + payment.ID.String() + " could not be refunded: " + reason,
	}).WithTraceID(trace)
}
