package handlers

import (
	"context"
	"errors"
	"fmt"

	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/fintech-bank-platform/transaction-service/internal/app/services"
)

type CommandDispatcher struct {
	txns *services.TransactionService
	log  *logger.Logger
}

func NewCommandDispatcher(txns *services.TransactionService, log *logger.Logger) *CommandDispatcher {
	return &CommandDispatcher{txns: txns, log: log}
}

func (d *CommandDispatcher) Dispatch(ctx context.Context, cmd *events.Event) (processor.Result, error) {
	var res processor.Result
	var err error

	switch cmd.Type {
	case events.EventTypes.CreateTransaction:
		var payload events.CreateTransactionPayload
		if err := processor.DecodePayload(cmd, &payload); err != nil {
			return processor.Result{}, err
		}
		res, err = d.txns.Create(ctx, payload, cmd.TraceID)
	case events.EventTypes.ProcessTransfer:
		var payload events.ProcessTransferPayload
		if err := processor.DecodePayload(cmd, &payload); err != nil {
			return processor.Result{}, err
		}
		res, err = d.txns.Transfer(ctx, payload, cmd.TraceID)
	default:
		return processor.Result{}, fmt.Errorf("%w: %s", processor.ErrUnknownCommand, cmd.Type)
	}

	if errors.Is(err, models.ErrDuplicateKey) {
		d.log.Info().Str("event_id", cmd.ID).Str("type", cmd.Type).Msg("duplicate idempotency key")
		return processor.Result{}, nil
	}
	return res, err
}
