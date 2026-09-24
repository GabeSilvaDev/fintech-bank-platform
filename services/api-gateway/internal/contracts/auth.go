package contracts

import (
	"context"

	"github.com/google/uuid"
)

type IdentityProvider interface {
	Register(ctx context.Context, email, password string) (uuid.UUID, error)
	Verify(ctx context.Context, email, password string) (uuid.UUID, error)
}

type TokenIssuer interface {
	Issue(userID uuid.UUID) (string, int, error)
}
