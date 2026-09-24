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
		Query("INSERT INTO refresh_tokens (token_hash, user_id, family_id, status, expires_at, created_at, family_created_at) VALUES (?, ?, ?, ?, ?, ?, ?) USING TTL ?",
			token.TokenHash, gocql.UUID(token.UserID), gocql.UUID(token.FamilyID), string(token.Status), token.ExpiresAt, token.CreatedAt, token.FamilyCreatedAt, seconds)
	return cassandra.MapWriteError(batch.Exec())
}

func (r *RefreshTokenRepository) Get(ctx context.Context, tokenHash string) (*models.RefreshToken, error) {
	var (
		storedHash, status                    string
		userID, familyID                      gocql.UUID
		expiresAt, createdAt, familyCreatedAt time.Time
	)
	err := r.session.Query("SELECT token_hash, user_id, family_id, status, expires_at, created_at, family_created_at FROM refresh_tokens WHERE token_hash = ?", tokenHash).
		WithContext(ctx).Scan(&storedHash, &userID, &familyID, &status, &expiresAt, &createdAt, &familyCreatedAt)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &models.RefreshToken{
		TokenHash:       storedHash,
		UserID:          uuid.UUID(userID),
		FamilyID:        uuid.UUID(familyID),
		Status:          models.RefreshTokenStatus(status),
		ExpiresAt:       expiresAt,
		CreatedAt:       createdAt,
		FamilyCreatedAt: familyCreatedAt,
	}, nil
}

func (r *RefreshTokenRepository) MarkRotated(ctx context.Context, tokenHash string, ttl time.Duration) (bool, error) {
	applied, err := r.session.Query("UPDATE refresh_tokens USING TTL ? SET status = 'rotated' WHERE token_hash = ? IF status = 'active'", ttlSeconds(ttl), tokenHash).
		WithContext(ctx).MapScanCAS(map[string]interface{}{})
	return applied, cassandra.MapWriteError(err)
}

func (r *RefreshTokenRepository) RevokeFamily(ctx context.Context, familyID uuid.UUID, ttl time.Duration) error {
	err := r.session.Query("INSERT INTO revoked_families (family_id, revoked_at) VALUES (?, ?) USING TTL ?", gocql.UUID(familyID), time.Now().UTC(), ttlSeconds(ttl)).
		WithContext(ctx).Consistency(gocql.LocalQuorum).Exec()
	if err != nil {
		return cassandra.MapWriteError(err)
	}

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
		if err := r.revokeToken(ctx, tokenHash); err != nil {
			return err
		}
	}
	return nil
}

func (r *RefreshTokenRepository) revokeToken(ctx context.Context, tokenHash string) error {
	var (
		status    string
		remaining int
	)
	err := r.session.Query("SELECT status, TTL(user_id) FROM refresh_tokens WHERE token_hash = ?", tokenHash).
		WithContext(ctx).Scan(&status, &remaining)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if status != string(models.RefreshTokenActive) || remaining <= 0 {
		return nil
	}
	err = r.session.Query("UPDATE refresh_tokens USING TTL ? SET status = 'revoked' WHERE token_hash = ?", remaining, tokenHash).WithContext(ctx).Exec()
	return cassandra.MapWriteError(err)
}

func (r *RefreshTokenRepository) FamilyRevoked(ctx context.Context, familyID uuid.UUID) (bool, error) {
	var revokedAt time.Time
	err := r.session.Query("SELECT revoked_at FROM revoked_families WHERE family_id = ?", gocql.UUID(familyID)).
		WithContext(ctx).Consistency(gocql.LocalQuorum).Scan(&revokedAt)
	if errors.Is(err, gocql.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func ttlSeconds(ttl time.Duration) int {
	seconds := int((ttl + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}
