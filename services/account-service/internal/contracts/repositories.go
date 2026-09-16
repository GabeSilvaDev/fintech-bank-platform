package contracts

import (
	"context"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/google/uuid"
)

type AccountRepository interface {
	Create(ctx context.Context, account *models.Account) error
	ReserveNumber(ctx context.Context, agency, number string, accountID uuid.UUID) (bool, error)
	Get(ctx context.Context, accountID uuid.UUID) (*models.Account, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]*models.Account, error)
	UpdateStatus(ctx context.Context, accountID uuid.UUID, status models.AccountStatus, updatedAt time.Time, closedAt *time.Time) error
	CompareAndSetBalance(ctx context.Context, accountID uuid.UUID, expected, next int64, updatedAt time.Time) (bool, error)
}

type CustomerRepository interface {
	Upsert(ctx context.Context, customer *models.Customer) error
	Get(ctx context.Context, userID uuid.UUID) (*models.Customer, error)
	UpdateProfile(ctx context.Context, userID uuid.UUID, name, email, phone *string, updatedAt time.Time) error
}

type ProcessedEventStore interface {
	MarkProcessed(ctx context.Context, eventID uuid.UUID) (bool, error)
}

type Migrator interface {
	Up(ctx context.Context) ([]int, error)
}

type Clock interface {
	Now() time.Time
}
