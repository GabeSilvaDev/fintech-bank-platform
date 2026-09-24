package contracts

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrAccountNotFound = errors.New("account not found")

type IdentityProvider interface {
	Register(ctx context.Context, email, password string) (uuid.UUID, error)
	Verify(ctx context.Context, email, password string) (uuid.UUID, error)
}

type Session struct {
	RefreshToken string
	ExpiresAt    time.Time
}

type SessionProvider interface {
	StartSession(ctx context.Context, userID uuid.UUID) (Session, error)
	RotateSession(ctx context.Context, refreshToken string) (uuid.UUID, Session, error)
	RevokeSession(ctx context.Context, refreshToken string) error
}

type RetryAfter string

func (r RetryAfter) Error() string {
	return "retry after " + string(r) + " seconds"
}

type TokenIssuer interface {
	Issue(userID uuid.UUID) (string, int, error)
}

type AccountOwners interface {
	Owner(ctx context.Context, accountID uuid.UUID) (uuid.UUID, error)
}
