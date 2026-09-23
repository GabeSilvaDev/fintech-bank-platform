package services

import (
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
