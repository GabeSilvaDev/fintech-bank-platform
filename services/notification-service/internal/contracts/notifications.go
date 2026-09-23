package contracts

import (
	"context"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/google/uuid"
)

type Directory interface {
	Lookup(ctx context.Context, accountID uuid.UUID) (models.Contact, error)
}

type Sender interface {
	Send(ctx context.Context, message models.Message) error
}

type History interface {
	Append(ctx context.Context, record models.Record) error
	List(ctx context.Context, userID uuid.UUID, limit int) ([]models.Record, error)
}

type Clock interface {
	Now() time.Time
}
