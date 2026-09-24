package contracts

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var ErrAccountNotFound = errors.New("account not found")

type IdentityProvider interface {
	Register(ctx context.Context, email, password string) (uuid.UUID, error)
	Verify(ctx context.Context, email, password string) (uuid.UUID, error)
}

type TokenIssuer interface {
	Issue(userID uuid.UUID) (string, int, error)
}

type AccountOwners interface {
	Owner(ctx context.Context, accountID uuid.UUID) (uuid.UUID, error)
}
