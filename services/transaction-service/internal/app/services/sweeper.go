package services

import (
	"context"
	"errors"
	"time"

	"github.com/fintech-bank-platform/pkg/logger"
	"github.com/fintech-bank-platform/pkg/metrics"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/fintech-bank-platform/transaction-service/internal/contracts"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	ResentTotalName    = "reconciliation_resent_total"
	ResentTotalHelp    = "Total number of stale transactions re-sent by the reconciliation sweeper"
	ExhaustedTotalName = "reconciliation_exhausted_total"
	ExhaustedTotalHelp = "Total number of transactions that exceeded the reconciliation max age"
)

type Sweeper struct {
	service   *TransactionService
	publisher contracts.Publisher
	clock     contracts.Clock
	cfg       contracts.SweeperConfig
	log       *logger.Logger
	resent    *prometheus.CounterVec
	exhausted *prometheus.CounterVec
}

func NewSweeper(service *TransactionService, publisher contracts.Publisher, clock contracts.Clock, cfg contracts.SweeperConfig, log *logger.Logger) *Sweeper {
	return (&Sweeper{service: service, publisher: publisher, clock: clock, cfg: cfg, log: log}).WithMetrics(nil)
}

func (s *Sweeper) WithMetrics(m *metrics.Metrics) *Sweeper {
	s.resent = m.CounterVec(ResentTotalName, ResentTotalHelp, "status")
	s.exhausted = m.CounterVec(ExhaustedTotalName, ExhaustedTotalHelp)
	s.exhausted.WithLabelValues()
	return s
}

func (s *Sweeper) RunOnce(ctx context.Context) (int, error) {
	now := s.clock.Now()
	stale, err := s.service.ListStale(ctx, now.Add(-s.cfg.StaleAfter), s.cfg.MaxAge, s.cfg.Batch)
	if err != nil {
		return 0, err
	}

	resent := 0
	for _, tx := range stale {
		if err := ctx.Err(); err != nil {
			return resent, err
		}
		if now.Sub(tx.CreatedAt) > s.cfg.MaxAge {
			s.exhaust(ctx, tx, now)
			continue
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
		s.resent.WithLabelValues(string(tx.Status)).Inc()
		resent++
	}
	return resent, nil
}

func (s *Sweeper) exhaust(ctx context.Context, tx *models.Transaction, now time.Time) {
	if tx.UpdatedAt.After(tx.CreatedAt.Add(s.cfg.MaxAge)) {
		return
	}
	alert, err := s.service.Exhaust(ctx, tx)
	if errors.Is(err, ErrTouchLost) {
		return
	}
	if err != nil {
		s.log.Error().Err(err).Str("transaction_id", tx.ID.String()).Str("status", string(tx.Status)).Msg("reconciliation failed")
		return
	}
	if err := s.publisher.Publish(ctx, alert.Topic, alert.Key, alert.Event); err != nil {
		s.log.Error().Err(err).Str("transaction_id", tx.ID.String()).Str("status", string(tx.Status)).Msg("reconciliation alert publish failed")
	}
	s.exhausted.WithLabelValues().Inc()
	s.log.Error().Str("transaction_id", tx.ID.String()).Str("status", string(tx.Status)).Dur("age", now.Sub(tx.CreatedAt)).Msg("reconciliation exhausted")
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
