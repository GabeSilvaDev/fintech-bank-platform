package services

import (
	"context"
	"errors"
	"time"

	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/transaction-service/internal/contracts"
)

type Sweeper struct {
	service   *TransactionService
	publisher contracts.Publisher
	clock     contracts.Clock
	cfg       contracts.SweeperConfig
	log       *logger.Logger
}

func NewSweeper(service *TransactionService, publisher contracts.Publisher, clock contracts.Clock, cfg contracts.SweeperConfig, log *logger.Logger) *Sweeper {
	return &Sweeper{service: service, publisher: publisher, clock: clock, cfg: cfg, log: log}
}

func (s *Sweeper) RunOnce(ctx context.Context) (int, error) {
	before := s.clock.Now().Add(-s.cfg.StaleAfter)
	stale, err := s.service.ListStale(ctx, before, s.cfg.Batch)
	if err != nil {
		return 0, err
	}

	resent := 0
	for _, tx := range stale {
		if err := ctx.Err(); err != nil {
			return resent, err
		}

		res, err := s.service.Reconcile(ctx, tx)
		if errors.Is(err, ErrTouchLost) {
			continue
		}
		if err != nil {
			s.log.Error().Err(err).Str("transaction_id", tx.ID.String()).Str("status", string(tx.Status)).Msg("reconciliation failed")
			continue
		}
		if len(res.Messages) == 0 {
			s.log.Warn().Str("transaction_id", tx.ID.String()).Str("status", string(tx.Status)).Msg("stale transaction has no step to re-send")
			continue
		}

		published := true
		for _, msg := range res.Messages {
			if err := s.publisher.Publish(ctx, msg.Topic, msg.Key, msg.Event); err != nil {
				s.log.Error().Err(err).Str("transaction_id", tx.ID.String()).Str("status", string(tx.Status)).Msg("reconciliation publish failed")
				published = false
				break
			}
		}
		if !published {
			continue
		}

		s.log.Info().Str("transaction_id", tx.ID.String()).Str("status", string(tx.Status)).Msg("stale transaction re-sent")
		resent++
	}
	return resent, nil
}

func (s *Sweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.RunOnce(ctx); err != nil {
				s.log.Error().Err(err).Msg("reconciliation sweep failed")
			}
		}
	}
}
