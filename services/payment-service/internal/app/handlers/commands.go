package handlers

import (
	"context"
	"errors"
	"fmt"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/payment-service/internal/app/services"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/processor"
)

type CommandDispatcher struct {
	payments *services.PaymentService
	log      *logger.Logger
}

func NewCommandDispatcher(payments *services.PaymentService, log *logger.Logger) *CommandDispatcher {
	return &CommandDispatcher{payments: payments, log: log}
}

func (d *CommandDispatcher) Dispatch(ctx context.Context, cmd *events.Event) (processor.Result, error) {
	var res processor.Result
	var err error

	switch cmd.Type {
	case events.EventTypes.ProcessPayment:
		var payload events.ProcessPaymentPayload
		if err := processor.DecodePayload(cmd, &payload); err != nil {
			return processor.Result{}, err
		}
		res, err = d.payments.Create(ctx, payload, cmd.TraceID)
	case events.EventTypes.SubmitPayment:
		var payload events.SubmitPaymentPayload
		if err := processor.DecodePayload(cmd, &payload); err != nil {
			return processor.Result{}, err
		}
		res, err = d.payments.Submit(ctx, payload, cmd.TraceID)
	case events.EventTypes.SettlePayment:
		var payload events.SettlePaymentPayload
		if err := processor.DecodePayload(cmd, &payload); err != nil {
			return processor.Result{}, err
		}
		res, err = d.payments.Settle(ctx, payload, cmd.TraceID)
	default:
		return processor.Result{}, fmt.Errorf("%w: %s", processor.ErrUnknownCommand, cmd.Type)
	}

	if errors.Is(err, models.ErrDuplicateKey) {
		d.log.Info().Str("event_id", cmd.ID).Str("type", cmd.Type).Msg("duplicate idempotency key")
		return processor.Result{}, nil
	}
	return res, err
}
