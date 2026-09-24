package database

import (
	"context"
	"errors"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/pkg/cassandra"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/google/uuid"
)

type IdentityRepository struct {
	session *gocql.Session
}

func NewIdentityRepository(session *gocql.Session) *IdentityRepository {
	return &IdentityRepository{session: session}
}

func (r *IdentityRepository) Create(ctx context.Context, identity *models.Identity) (bool, error) {
	applied, err := r.session.Query("INSERT INTO identities_by_email (email, user_id, password_hash, created_at) VALUES (?, ?, ?, ?) IF NOT EXISTS",
		identity.Email, gocql.UUID(identity.UserID), identity.PasswordHash, identity.CreatedAt).
		WithContext(ctx).MapScanCAS(map[string]interface{}{})
	return applied, cassandra.MapWriteError(err)
}

func (r *IdentityRepository) GetByEmail(ctx context.Context, email string) (*models.Identity, error) {
	var (
		storedEmail, passwordHash string
		userID                    gocql.UUID
		createdAt                 time.Time
	)
	err := r.session.Query("SELECT email, user_id, password_hash, created_at FROM identities_by_email WHERE email = ?", email).
		WithContext(ctx).Scan(&storedEmail, &userID, &passwordHash, &createdAt)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &models.Identity{Email: storedEmail, UserID: uuid.UUID(userID), PasswordHash: passwordHash, CreatedAt: createdAt}, nil
}
