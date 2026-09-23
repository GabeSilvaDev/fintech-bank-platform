package contracts

import (
	"context"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/google/uuid"
)

type PaymentRepository interface {
	Create(ctx context.Context, payment *models.Payment) error
	ReserveKey(ctx context.Context, accountID uuid.UUID, key string, id uuid.UUID) (uuid.UUID, error)
	Get(ctx context.Context, id uuid.UUID) (*models.Payment, error)
	ListByAccount(ctx context.Context, accountID uuid.UUID, limit int) ([]*models.Payment, error)
	Transition(ctx context.Context, id uuid.UUID, from, to models.Status, patch models.Patch) (bool, error)
	BindExternalID(ctx context.Context, externalID string, id uuid.UUID) error
	FindByExternalID(ctx context.Context, externalID string) (uuid.UUID, error)
}

type ProcessedEventStore interface {
	MarkProcessed(ctx context.Context, eventID uuid.UUID) (bool, error)
}

type Clock interface {
	Now() time.Time
}
