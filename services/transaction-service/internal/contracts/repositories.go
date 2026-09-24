package contracts

import (
	"context"
	"time"

	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/google/uuid"
)

type TransactionRepository interface {
	Create(ctx context.Context, tx *models.Transaction) error
	ReserveKey(ctx context.Context, accountID uuid.UUID, key string, id uuid.UUID) (uuid.UUID, error)
	Get(ctx context.Context, id uuid.UUID) (*models.Transaction, error)
	ListByAccount(ctx context.Context, accountID uuid.UUID, before *time.Time, limit int) ([]*models.Transaction, error)
	Transition(ctx context.Context, id uuid.UUID, from, to models.TransactionStatus, patch models.Patch) (bool, error)
	ListStale(ctx context.Context, before time.Time, maxAge time.Duration, limit int) ([]*models.Transaction, error)
	Touch(ctx context.Context, id uuid.UUID, status models.TransactionStatus, observed, now time.Time) (bool, error)
	Reindex(ctx context.Context) (int, error)
}

type ProcessedEventStore interface {
	MarkProcessed(ctx context.Context, eventID uuid.UUID) (bool, error)
}

type Clock interface {
	Now() time.Time
}
