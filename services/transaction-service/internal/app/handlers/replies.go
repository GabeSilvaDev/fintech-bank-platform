package handlers

import (
	"context"

	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/fintech-bank-platform/transaction-service/internal/app/services"
)

type ReplyDispatcher struct {
	txns *services.TransactionService
}

func NewReplyDispatcher(txns *services.TransactionService) *ReplyDispatcher {
	return &ReplyDispatcher{txns: txns}
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
	return d.txns.ApplyAccountEvent(ctx, services.Reply{
		Kind:           event.Type,
		Reference:      payload.Reference,
		IdempotencyKey: payload.IdempotencyKey,
		BalanceAfter:   payload.BalanceAfter,
		Reason:         payload.Reason,
		TraceID:        event.TraceID,
	})
}
