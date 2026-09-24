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
	CloseIfEmpty(ctx context.Context, accountID uuid.UUID, closedAt time.Time) (bool, error)
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

type BalanceOperationRepository interface {
	Reserve(ctx context.Context, accountID uuid.UUID, key, kind string, at time.Time) (bool, error)
	Get(ctx context.Context, accountID uuid.UUID, key string) (*models.BalanceOperation, error)
	Complete(ctx context.Context, accountID uuid.UUID, key, result string, at time.Time) error
	Release(ctx context.Context, accountID uuid.UUID, key string) error
}

type IdentityRepository interface {
	Create(ctx context.Context, identity *models.Identity) (bool, error)
	GetByEmail(ctx context.Context, email string) (*models.Identity, error)
}

type LoginFailureRepository interface {
	Get(ctx context.Context, email string) (*models.LoginFailure, error)
	Create(ctx context.Context, failure *models.LoginFailure, ttl time.Duration) (bool, error)
	Replace(ctx context.Context, current, next *models.LoginFailure, ttl time.Duration) (bool, error)
	Clear(ctx context.Context, email string) error
}

type RefreshTokenRepository interface {
	Create(ctx context.Context, token *models.RefreshToken, ttl time.Duration) error
	Get(ctx context.Context, tokenHash string) (*models.RefreshToken, error)
	MarkRotated(ctx context.Context, tokenHash string, ttl time.Duration) (bool, error)
	RevokeFamily(ctx context.Context, familyID uuid.UUID, ttl time.Duration) error
	FamilyRevoked(ctx context.Context, familyID uuid.UUID) (bool, error)
}

type Hasher interface {
	Hash(password string) (string, error)
	Compare(hash, password string) error
}

type Migrator interface {
	Up(ctx context.Context) ([]int, error)
}

type Clock interface {
	Now() time.Time
}
