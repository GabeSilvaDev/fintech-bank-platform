//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/fintech-bank-platform/account-service/internal/infrastructure/database"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func refreshToken(familyID uuid.UUID, at time.Time) *models.RefreshToken {
	return &models.RefreshToken{
		TokenHash: uuid.NewString(),
		UserID:    uuid.New(),
		FamilyID:  familyID,
		Status:    models.RefreshTokenActive,
		ExpiresAt: at.Add(time.Hour),
		CreatedAt: at,
	}
}

func familyHashes(t *testing.T, session *gocql.Session, familyID uuid.UUID) []string {
	iter := session.Query("SELECT token_hash FROM sessions_by_family WHERE family_id = ?", gocql.UUID(familyID)).Iter()
	var (
		hashes []string
		hash   string
	)
	for iter.Scan(&hash) {
		hashes = append(hashes, hash)
	}
	require.NoError(t, iter.Close())
	return hashes
}

func TestRefreshTokenRepositoryStoresTokensWithTheirTTL(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewRefreshTokenRepository(session)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Millisecond)
	token := refreshToken(uuid.New(), at)

	require.NoError(t, repo.Create(ctx, token, time.Hour))

	stored, err := repo.Get(ctx, token.TokenHash)
	require.NoError(t, err)
	require.Equal(t, token.TokenHash, stored.TokenHash)
	require.Equal(t, token.UserID, stored.UserID)
	require.Equal(t, token.FamilyID, stored.FamilyID)
	require.Equal(t, models.RefreshTokenActive, stored.Status)
	require.WithinDuration(t, token.ExpiresAt, stored.ExpiresAt, time.Millisecond)
	require.WithinDuration(t, at, stored.CreatedAt, time.Millisecond)
	require.Equal(t, []string{token.TokenHash}, familyHashes(t, session, token.FamilyID))

	var ttl int
	require.NoError(t, session.Query("SELECT TTL(status) FROM refresh_tokens WHERE token_hash = ?", token.TokenHash).Scan(&ttl))
	require.InDelta(t, 3600, ttl, 30)

	_, err = repo.Get(ctx, "unknown-"+uuid.NewString())
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestRefreshTokenRepositoryRowsExpireWithTheConfiguredTTL(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewRefreshTokenRepository(session)
	ctx := context.Background()
	token := refreshToken(uuid.New(), time.Now().UTC())

	require.NoError(t, repo.Create(ctx, token, 500*time.Millisecond))

	require.Eventually(t, func() bool {
		_, err := repo.Get(ctx, token.TokenHash)
		var hash string
		indexed := session.Query("SELECT token_hash FROM sessions_by_family WHERE family_id = ?", gocql.UUID(token.FamilyID)).Scan(&hash)
		return errors.Is(err, domain.ErrNotFound) && errors.Is(indexed, gocql.ErrNotFound)
	}, 10*time.Second, 200*time.Millisecond)
}

func TestRefreshTokenRepositoryMarkRotatedIsCompareAndSet(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewRefreshTokenRepository(session)
	ctx := context.Background()
	token := refreshToken(uuid.New(), time.Now().UTC())
	require.NoError(t, repo.Create(ctx, token, time.Hour))

	applied, err := repo.MarkRotated(ctx, token.TokenHash)
	require.NoError(t, err)
	require.True(t, applied)

	applied, err = repo.MarkRotated(ctx, token.TokenHash)
	require.NoError(t, err)
	require.False(t, applied)

	stored, err := repo.Get(ctx, token.TokenHash)
	require.NoError(t, err)
	require.Equal(t, models.RefreshTokenRotated, stored.Status)

	applied, err = repo.MarkRotated(ctx, "unknown-"+uuid.NewString())
	require.NoError(t, err)
	require.False(t, applied)
	_, err = repo.Get(ctx, "unknown-"+uuid.NewString())
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestRefreshTokenRepositoryRevokesTheWholeFamily(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewRefreshTokenRepository(session)
	ctx := context.Background()
	at := time.Now().UTC()
	familyID := uuid.New()
	first := refreshToken(familyID, at)
	second := refreshToken(familyID, at)
	other := refreshToken(uuid.New(), at)
	for _, token := range []*models.RefreshToken{first, second, other} {
		require.NoError(t, repo.Create(ctx, token, time.Hour))
	}
	_, err := repo.MarkRotated(ctx, first.TokenHash)
	require.NoError(t, err)

	require.NoError(t, repo.RevokeFamily(ctx, familyID))
	require.NoError(t, repo.RevokeFamily(ctx, familyID))

	for _, token := range []*models.RefreshToken{first, second} {
		stored, err := repo.Get(ctx, token.TokenHash)
		require.NoError(t, err)
		require.Equal(t, models.RefreshTokenRevoked, stored.Status)
	}
	stored, err := repo.Get(ctx, other.TokenHash)
	require.NoError(t, err)
	require.Equal(t, models.RefreshTokenActive, stored.Status)

	applied, err := repo.MarkRotated(ctx, second.TokenHash)
	require.NoError(t, err)
	require.False(t, applied)

	require.NoError(t, repo.RevokeFamily(ctx, uuid.New()))
}

func TestSessionServiceAgainstCassandra(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	service := services.NewSessionService(database.NewRefreshTokenRepository(session), services.SystemClock{}, time.Hour)
	ctx := context.Background()
	userID := uuid.New()

	started, err := service.Start(ctx, userID)
	require.NoError(t, err)

	rotatedUser, next, err := service.Rotate(ctx, started.Token)
	require.NoError(t, err)
	require.Equal(t, userID, rotatedUser)

	_, _, err = service.Rotate(ctx, started.Token)
	require.ErrorIs(t, err, services.ErrInvalidSession)
	_, _, err = service.Rotate(ctx, next.Token)
	require.ErrorIs(t, err, services.ErrInvalidSession)

	other, err := service.Start(ctx, userID)
	require.NoError(t, err)
	require.NoError(t, service.Revoke(ctx, other.Token))
	_, _, err = service.Rotate(ctx, other.Token)
	require.ErrorIs(t, err, services.ErrInvalidSession)
	require.NoError(t, service.Revoke(ctx, "unknown"))
}
