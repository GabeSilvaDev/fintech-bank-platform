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

type RefreshTokenRepository struct {
	session *gocql.Session
}

func NewRefreshTokenRepository(session *gocql.Session) *RefreshTokenRepository {
	return &RefreshTokenRepository{session: session}
}

func (r *RefreshTokenRepository) Create(ctx context.Context, token *models.RefreshToken, ttl time.Duration) error {
	seconds := ttlSeconds(ttl)
	batch := r.session.Batch(gocql.LoggedBatch).WithContext(ctx).
		Query("INSERT INTO sessions_by_family (family_id, token_hash) VALUES (?, ?) USING TTL ?",
			gocql.UUID(token.FamilyID), token.TokenHash, seconds).
		Query("INSERT INTO refresh_tokens (token_hash, user_id, family_id, status, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?) USING TTL ?",
			token.TokenHash, gocql.UUID(token.UserID), gocql.UUID(token.FamilyID), string(token.Status), token.ExpiresAt, token.CreatedAt, seconds)
	return cassandra.MapWriteError(batch.Exec())
}

func (r *RefreshTokenRepository) Get(ctx context.Context, tokenHash string) (*models.RefreshToken, error) {
	var (
		storedHash, status   string
		userID, familyID     gocql.UUID
		expiresAt, createdAt time.Time
	)
	err := r.session.Query("SELECT token_hash, user_id, family_id, status, expires_at, created_at FROM refresh_tokens WHERE token_hash = ?", tokenHash).
		WithContext(ctx).Scan(&storedHash, &userID, &familyID, &status, &expiresAt, &createdAt)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &models.RefreshToken{
		TokenHash: storedHash,
		UserID:    uuid.UUID(userID),
		FamilyID:  uuid.UUID(familyID),
		Status:    models.RefreshTokenStatus(status),
		ExpiresAt: expiresAt,
		CreatedAt: createdAt,
	}, nil
}

func (r *RefreshTokenRepository) MarkRotated(ctx context.Context, tokenHash string) (bool, error) {
	applied, err := r.session.Query("UPDATE refresh_tokens SET status = 'rotated' WHERE token_hash = ? IF status = 'active'", tokenHash).
		WithContext(ctx).MapScanCAS(map[string]interface{}{})
	return applied, cassandra.MapWriteError(err)
}

func (r *RefreshTokenRepository) RevokeFamily(ctx context.Context, familyID uuid.UUID) error {
	iter := r.session.Query("SELECT token_hash FROM sessions_by_family WHERE family_id = ?", gocql.UUID(familyID)).WithContext(ctx).Iter()
	var (
		hashes []string
		hash   string
	)
	for iter.Scan(&hash) {
		hashes = append(hashes, hash)
	}
	if err := iter.Close(); err != nil {
		return err
	}
	for _, tokenHash := range hashes {
		err := r.session.Query("UPDATE refresh_tokens SET status = 'revoked' WHERE token_hash = ?", tokenHash).WithContext(ctx).Exec()
		if err != nil {
			return cassandra.MapWriteError(err)
		}
	}
	return nil
}

func ttlSeconds(ttl time.Duration) int {
	seconds := int(ttl / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}
