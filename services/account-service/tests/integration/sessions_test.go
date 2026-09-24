//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/fintech-bank-platform/account-service/internal/contracts"
	"github.com/fintech-bank-platform/account-service/internal/infrastructure/database"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func refreshToken(familyID uuid.UUID, at time.Time) *models.RefreshToken {
	return &models.RefreshToken{
		TokenHash:       uuid.NewString(),
		UserID:          uuid.New(),
		FamilyID:        familyID,
		Status:          models.RefreshTokenActive,
		ExpiresAt:       at.Add(time.Hour),
		CreatedAt:       at,
		FamilyCreatedAt: at.Add(-time.Minute),
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

func activeInFamily(t *testing.T, session *gocql.Session, familyID uuid.UUID) []string {
	repo := database.NewRefreshTokenRepository(session)
	var active []string
	for _, hash := range familyHashes(t, session, familyID) {
		token, err := repo.Get(context.Background(), hash)
		require.NoError(t, err)
		if token.Status == models.RefreshTokenActive {
			active = append(active, hash)
		}
	}
	return active
}

func cellTTL(t *testing.T, session *gocql.Session, column, tokenHash string) int {
	var ttl int
	require.NoError(t, session.Query("SELECT TTL("+column+") FROM refresh_tokens WHERE token_hash = ?", tokenHash).Scan(&ttl))
	return ttl
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
	require.WithinDuration(t, token.FamilyCreatedAt, stored.FamilyCreatedAt, time.Millisecond)
	require.Equal(t, []string{token.TokenHash}, familyHashes(t, session, token.FamilyID))
	require.InDelta(t, 3600, cellTTL(t, session, "status", token.TokenHash), 30)

	_, err = repo.Get(ctx, "unknown-"+uuid.NewString())
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestRefreshTokenRepositoryReadsRowsWithoutFamilyStart(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewRefreshTokenRepository(session)
	hash := "legacy-" + uuid.NewString()
	require.NoError(t, session.Query("INSERT INTO refresh_tokens (token_hash, user_id, family_id, status, expires_at, created_at) VALUES (?, ?, ?, 'active', ?, ?)",
		hash, gocql.UUID(uuid.New()), gocql.UUID(uuid.New()), time.Now().Add(time.Hour), time.Now()).Exec())

	stored, err := repo.Get(context.Background(), hash)

	require.NoError(t, err)
	require.True(t, stored.FamilyCreatedAt.IsZero())
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

	applied, err := repo.MarkRotated(ctx, token.TokenHash, 20*time.Minute)
	require.NoError(t, err)
	require.True(t, applied)
	require.InDelta(t, 1200, cellTTL(t, session, "status", token.TokenHash), 30)

	applied, err = repo.MarkRotated(ctx, token.TokenHash, time.Hour)
	require.NoError(t, err)
	require.False(t, applied)

	stored, err := repo.Get(ctx, token.TokenHash)
	require.NoError(t, err)
	require.Equal(t, models.RefreshTokenRotated, stored.Status)

	unknown := "unknown-" + uuid.NewString()
	applied, err = repo.MarkRotated(ctx, unknown, time.Hour)
	require.NoError(t, err)
	require.False(t, applied)
	_, err = repo.Get(ctx, unknown)
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
	require.NoError(t, repo.Create(ctx, first, time.Hour))
	require.NoError(t, repo.Create(ctx, second, 10*time.Minute))
	require.NoError(t, repo.Create(ctx, other, time.Hour))
	_, err := repo.MarkRotated(ctx, first.TokenHash, time.Hour)
	require.NoError(t, err)

	revoked, err := repo.FamilyRevoked(ctx, familyID)
	require.NoError(t, err)
	require.False(t, revoked)

	require.NoError(t, repo.RevokeFamily(ctx, familyID, 2*time.Hour))
	require.NoError(t, repo.RevokeFamily(ctx, familyID, 2*time.Hour))

	revoked, err = repo.FamilyRevoked(ctx, familyID)
	require.NoError(t, err)
	require.True(t, revoked)
	var markerTTL int
	require.NoError(t, session.Query("SELECT TTL(revoked_at) FROM revoked_families WHERE family_id = ?", gocql.UUID(familyID)).Scan(&markerTTL))
	require.InDelta(t, 7200, markerTTL, 30)

	stored, err := repo.Get(ctx, first.TokenHash)
	require.NoError(t, err)
	require.Equal(t, models.RefreshTokenRotated, stored.Status)
	stored, err = repo.Get(ctx, second.TokenHash)
	require.NoError(t, err)
	require.Equal(t, models.RefreshTokenRevoked, stored.Status)
	require.InDelta(t, cellTTL(t, session, "user_id", second.TokenHash), cellTTL(t, session, "status", second.TokenHash), 2)
	require.LessOrEqual(t, cellTTL(t, session, "status", second.TokenHash), 600)

	stored, err = repo.Get(ctx, other.TokenHash)
	require.NoError(t, err)
	require.Equal(t, models.RefreshTokenActive, stored.Status)
	revoked, err = repo.FamilyRevoked(ctx, other.FamilyID)
	require.NoError(t, err)
	require.False(t, revoked)

	applied, err := repo.MarkRotated(ctx, second.TokenHash, time.Hour)
	require.NoError(t, err)
	require.False(t, applied)
}

func TestRefreshTokenRepositoryRevokeSkipsMissingTokenRows(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewRefreshTokenRepository(session)
	ctx := context.Background()
	familyID := uuid.New()
	orphan := "orphan-" + uuid.NewString()
	require.NoError(t, session.Query("INSERT INTO sessions_by_family (family_id, token_hash) VALUES (?, ?)", gocql.UUID(familyID), orphan).Exec())

	require.NoError(t, repo.RevokeFamily(ctx, familyID, time.Hour))

	_, err := repo.Get(ctx, orphan)
	require.ErrorIs(t, err, domain.ErrNotFound)
	require.NoError(t, repo.RevokeFamily(ctx, uuid.New(), time.Hour))
}

func familyOfToken(t *testing.T, session *gocql.Session, token string) uuid.UUID {
	sum := sha256.Sum256([]byte(token))
	stored, err := database.NewRefreshTokenRepository(session).Get(context.Background(), hex.EncodeToString(sum[:]))
	require.NoError(t, err)
	return stored.FamilyID
}

func sessionService(session *gocql.Session) *services.SessionService {
	return services.NewSessionService(database.NewRefreshTokenRepository(session), services.SystemClock{}, contracts.SessionConfig{RefreshTokenTTL: time.Hour, FamilyMaxAge: 24 * time.Hour})
}

func TestSessionServiceAgainstCassandra(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	service := sessionService(session)
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

func TestConcurrentRotationsOfOneTokenLeaveNothingUsable(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	service := sessionService(session)
	repo := database.NewRefreshTokenRepository(session)
	ctx := context.Background()

	for round := 0; round < 3; round++ {
		started, err := service.Start(ctx, uuid.New())
		require.NoError(t, err)
		familyID := familyOfToken(t, session, started.Token)

		var (
			wg        sync.WaitGroup
			mu        sync.Mutex
			successes []services.Session
			failures  int
		)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, next, err := service.Rotate(ctx, started.Token)
				mu.Lock()
				defer mu.Unlock()
				if err == nil {
					successes = append(successes, next)
					return
				}
				assert.ErrorIs(t, err, services.ErrInvalidSession)
				failures++
			}()
		}
		wg.Wait()

		require.LessOrEqual(t, len(successes), 1)
		require.Positive(t, failures)
		for _, next := range successes {
			_, _, err := service.Rotate(ctx, next.Token)
			require.ErrorIs(t, err, services.ErrInvalidSession)
		}
		require.Empty(t, activeInFamily(t, session, familyID))
		revoked, err := repo.FamilyRevoked(ctx, familyID)
		require.NoError(t, err)
		require.True(t, revoked)
	}
}

func TestRevokeWinsOverConcurrentRotations(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	service := sessionService(session)
	ctx := context.Background()

	for round := 0; round < 5; round++ {
		started, err := service.Start(ctx, uuid.New())
		require.NoError(t, err)
		familyID := familyOfToken(t, session, started.Token)

		var (
			wg   sync.WaitGroup
			last string
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			current := started.Token
			for attempt := 0; attempt < 500; attempt++ {
				_, next, err := service.Rotate(ctx, current)
				if err != nil {
					assert.ErrorIs(t, err, services.ErrInvalidSession)
					break
				}
				current = next.Token
			}
			last = current
		}()
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(round*15) * time.Millisecond)
			assert.NoError(t, service.Revoke(ctx, started.Token))
		}()
		wg.Wait()

		_, _, err = service.Rotate(ctx, last)
		require.ErrorIs(t, err, services.ErrInvalidSession)
		require.Empty(t, activeInFamily(t, session, familyID))
	}
}
