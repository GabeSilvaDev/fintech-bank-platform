package handlers

import (
	"context"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/payment-service/internal/app/services"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/processor"
)

type ReplyDispatcher struct {
	payments *services.PaymentService
	log      *logger.Logger
}

func NewReplyDispatcher(payments *services.PaymentService, log *logger.Logger) *ReplyDispatcher {
	return &ReplyDispatcher{payments: payments, log: log}
}

type accountReply struct {
	Reference      string  `json:"reference"`
	IdempotencyKey string  `json:"idempotency_key"`
	BalanceAfter   float64 `json:"balance_after"`
	Reason         string  `json:"reason"`
}

func (d *ReplyDispatcher) Dispatch(ctx context.Context, event *events.Event) (processor.Result, error) {
	switch event.Type {
	case events.EventTypes.AccountCredited, events.EventTypes.AccountDebited, events.EventTypes.DebitRejected, events.EventTypes.CreditRejected:
	default:
		return processor.Result{}, nil
	}

	var payload accountReply
	if err := processor.DecodePayload(event, &payload); err != nil {
		return processor.Result{}, err
	}
	res, err := d.payments.ApplyAccountEvent(ctx, services.Reply{
		Kind:           event.Type,
		Reference:      payload.Reference,
		IdempotencyKey: payload.IdempotencyKey,
		BalanceAfter:   payload.BalanceAfter,
		Reason:         payload.Reason,
		TraceID:        event.TraceID,
	})
	if _, ours := models.ParseReference(payload.Reference); err == nil && len(res.Messages) == 0 && ours {
		d.log.Info().Str("event_id", event.ID).Str("type", event.Type).Str("reference", payload.Reference).Str("idempotency_key", payload.IdempotencyKey).Msg("ignored account event")
	}
	return res, err
}
