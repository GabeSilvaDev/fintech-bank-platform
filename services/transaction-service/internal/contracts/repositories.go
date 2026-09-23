package contracts

import (
	"context"
	"time"

	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/google/uuid"
)

type TransactionRepository interface {
	Create(ctx context.Context, tx *models.Transaction) error
	ReserveKey(ctx context.Context, key string, id uuid.UUID) (uuid.UUID, error)
	Get(ctx context.Context, id uuid.UUID) (*models.Transaction, error)
	ListByAccount(ctx context.Context, accountID uuid.UUID, limit int) ([]*models.Transaction, error)
	Transition(ctx context.Context, id uuid.UUID, from, to models.TransactionStatus, patch models.Patch) (bool, error)
	ListStale(ctx context.Context, before time.Time, limit int) ([]*models.Transaction, error)
}

type ProcessedEventStore interface {
	MarkProcessed(ctx context.Context, eventID uuid.UUID) (bool, error)
}

type Clock interface {
	Now() time.Time
}
