package services

import (
	"context"
	"errors"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/payment-service/internal/contracts"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/pkg/events"
	"github.com/fintech-bank-platform/pkg/processor"
	"github.com/google/uuid"
)

const source = "payment-service"

type IDGenerator func() uuid.UUID

type SystemClock struct{}

func (SystemClock) Now() time.Time {
	return time.Now().UTC()
}

type PaymentService struct {
	repo    contracts.PaymentRepository
	gateway contracts.Gateway
	clock   contracts.Clock
	newID   IDGenerator
}

func NewPaymentService(repo contracts.PaymentRepository, gateway contracts.Gateway, clock contracts.Clock, newID IDGenerator) *PaymentService {
	return &PaymentService{repo: repo, gateway: gateway, clock: clock, newID: newID}
}

func (s *PaymentService) Create(ctx context.Context, cmd events.ProcessPaymentPayload, trace string) (processor.Result, error) {
	payment, err := parsePayment(cmd)
	if err != nil {
		return processor.Result{}, err
	}
	if err := s.record(ctx, payment); err != nil {
		return processor.Result{}, err
	}
	return processor.Result{Messages: []processor.Message{
		toEvents(payment, createdEvent(payment, trace)),
		toAccount(payment, debitCommand(payment, trace)),
	}}, nil
}

func (s *PaymentService) Get(ctx context.Context, id uuid.UUID) (*models.Payment, error) {
	return s.repo.Get(ctx, id)
}

func (s *PaymentService) ListByAccount(ctx context.Context, accountID uuid.UUID, before *time.Time, limit int) (models.Page, error) {
	return s.repo.ListByAccount(ctx, accountID, before, limit)
}

func (s *PaymentService) record(ctx context.Context, payment *models.Payment) error {
	now := s.clock.Now()
	payment.ID = s.newID()
	payment.Status = models.StatusPending
	payment.Currency = models.Currency
	payment.CreatedAt = now
	payment.UpdatedAt = now

	owner, err := s.repo.ReserveKey(ctx, payment.AccountID, payment.IdempotencyKey, payment.ID)
	if err != nil {
		return err
	}
	if owner != payment.ID {
		_, err := s.repo.Get(ctx, owner)
		if err == nil {
			return models.ErrDuplicateKey
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		payment.ID = owner
	}
	return s.repo.Create(ctx, payment)
}
