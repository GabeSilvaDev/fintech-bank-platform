//go:build integration

package integration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/internal/app/services"
	"github.com/fintech-bank-platform/account-service/internal/contracts"
	"github.com/fintech-bank-platform/account-service/internal/infrastructure/database"
	"github.com/fintech-bank-platform/account-service/tests"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func failureTTL(t *testing.T, session *gocql.Session, email string) (int, int) {
	var failures, first int
	require.NoError(t, session.Query("SELECT TTL(failures), TTL(first_failure) FROM login_failures WHERE email = ?", email).Scan(&failures, &first))
	return failures, first
}

func TestLoginFailureRepositoryWritesWithTheWindowTTL(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewLoginFailureRepository(session)
	ctx := context.Background()
	email := uuid.NewString() + "@example.com"
	at := time.Now().UTC().Truncate(time.Millisecond)

	_, err := repo.Get(ctx, email)
	require.ErrorIs(t, err, domain.ErrNotFound)

	created, err := repo.Create(ctx, &models.LoginFailure{Email: email, Failures: 1, FirstFailure: at}, 10*time.Minute)
	require.NoError(t, err)
	require.True(t, created)
	failuresTTL, firstTTL := failureTTL(t, session, email)
	require.InDelta(t, 600, failuresTTL, 30)
	require.InDelta(t, 600, firstTTL, 30)

	created, err = repo.Create(ctx, &models.LoginFailure{Email: email, Failures: 1, FirstFailure: at.Add(time.Second)}, time.Hour)
	require.NoError(t, err)
	require.False(t, created)

	stored, err := repo.Get(ctx, email)
	require.NoError(t, err)
	require.Equal(t, email, stored.Email)
	require.Equal(t, 1, stored.Failures)
	require.WithinDuration(t, at, stored.FirstFailure, time.Millisecond)

	next := &models.LoginFailure{Email: email, Failures: 2, FirstFailure: stored.FirstFailure}
	replaced, err := repo.Replace(ctx, stored, next, 300*time.Millisecond+4*time.Minute)
	require.NoError(t, err)
	require.True(t, replaced)
	failuresTTL, firstTTL = failureTTL(t, session, email)
	require.InDelta(t, 241, failuresTTL, 30)
	require.InDelta(t, 241, firstTTL, 30)

	replaced, err = repo.Replace(ctx, stored, &models.LoginFailure{Email: email, Failures: 9, FirstFailure: stored.FirstFailure}, time.Minute)
	require.NoError(t, err)
	require.False(t, replaced)
	stale := &models.LoginFailure{Email: email, Failures: 2, FirstFailure: stored.FirstFailure.Add(time.Millisecond)}
	replaced, err = repo.Replace(ctx, stale, &models.LoginFailure{Email: email, Failures: 9, FirstFailure: stored.FirstFailure}, time.Minute)
	require.NoError(t, err)
	require.False(t, replaced)

	stored, err = repo.Get(ctx, email)
	require.NoError(t, err)
	require.Equal(t, 2, stored.Failures)

	require.NoError(t, repo.Clear(ctx, email))
	_, err = repo.Get(ctx, email)
	require.ErrorIs(t, err, domain.ErrNotFound)
	require.NoError(t, repo.Clear(ctx, email))

	replaced, err = repo.Replace(ctx, stored, next, time.Minute)
	require.NoError(t, err)
	require.False(t, replaced)
	_, err = repo.Get(ctx, email)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestLoginFailureRowsVanishWhenTheWindowEnds(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewLoginFailureRepository(session)
	ctx := context.Background()
	email := uuid.NewString() + "@example.com"

	created, err := repo.Create(ctx, &models.LoginFailure{Email: email, Failures: 1, FirstFailure: time.Now().UTC()}, 400*time.Millisecond)
	require.NoError(t, err)
	require.True(t, created)

	require.Eventually(t, func() bool {
		_, err := repo.Get(ctx, email)
		return errors.Is(err, domain.ErrNotFound)
	}, 10*time.Second, 200*time.Millisecond)
}

func TestLoginFailureCompareAndSetKeepsEveryConcurrentIncrement(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewLoginFailureRepository(session)
	ctx := context.Background()
	email := uuid.NewString() + "@example.com"
	created, err := repo.Create(ctx, &models.LoginFailure{Email: email, Failures: 1, FirstFailure: time.Now().UTC()}, time.Hour)
	require.NoError(t, err)
	require.True(t, created)

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				current, err := repo.Get(ctx, email)
				if err != nil {
					continue
				}
				applied, err := repo.Replace(ctx, current, &models.LoginFailure{Email: email, Failures: current.Failures + 1, FirstFailure: current.FirstFailure}, time.Hour)
				if err == nil && applied {
					return
				}
			}
		}()
	}
	wg.Wait()

	stored, err := repo.Get(ctx, email)
	require.NoError(t, err)
	require.Equal(t, 7, stored.Failures)
}

func TestIdentityLockoutAgainstCassandra(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	failures := database.NewLoginFailureRepository(session)
	clock := &tests.FakeClock{T: time.Now().UTC().Truncate(time.Millisecond)}
	start := clock.T
	service, err := services.NewIdentityService(database.NewIdentityRepository(session), failures, &tests.FakeHasher{}, clock, contracts.LockoutConfig{MaxFailures: 3, Window: time.Hour})
	require.NoError(t, err)
	ctx := context.Background()
	email := uuid.NewString() + "@example.com"
	userID, err := service.Register(ctx, email, "correct horse")
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		_, err := service.Verify(ctx, email, "wrong horse")
		require.ErrorIs(t, err, services.ErrInvalidCredentials)
	}
	stored, err := failures.Get(ctx, email)
	require.NoError(t, err)
	require.Equal(t, 3, stored.Failures)
	require.WithinDuration(t, start, stored.FirstFailure, time.Millisecond)

	_, err = service.Verify(ctx, email, "correct horse")
	var locked *services.ErrTooManyAttempts
	require.ErrorAs(t, err, &locked)
	require.Equal(t, time.Hour, locked.RetryAfter)

	ghost := uuid.NewString() + "@example.com"
	for i := 0; i < 3; i++ {
		_, err := service.Verify(ctx, ghost, "wrong horse")
		require.ErrorIs(t, err, services.ErrInvalidCredentials)
	}
	_, err = service.Verify(ctx, ghost, "wrong horse")
	require.ErrorAs(t, err, &locked)

	clock.T = start.Add(time.Hour + time.Second)
	_, err = service.Verify(ctx, email, "wrong horse")
	require.ErrorIs(t, err, services.ErrInvalidCredentials)
	stored, err = failures.Get(ctx, email)
	require.NoError(t, err)
	require.Equal(t, 1, stored.Failures)
	require.WithinDuration(t, clock.T, stored.FirstFailure, time.Millisecond)
	failuresTTL, _ := failureTTL(t, session, email)
	require.InDelta(t, 3600, failuresTTL, 30)

	verified, err := service.Verify(ctx, email, "correct horse")
	require.NoError(t, err)
	require.Equal(t, userID, verified)
	_, err = failures.Get(ctx, email)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

type countingHasher struct {
	compares atomic.Int32
}

func (h *countingHasher) Hash(password string) (string, error) {
	return "hashed:" + password, nil
}

func (h *countingHasher) Compare(hash, password string) error {
	h.compares.Add(1)
	if hash != "hashed:"+password {
		return errors.New("mismatch")
	}
	return nil
}

func TestConcurrentWrongPasswordsAreBoundedStrictlyAgainstCassandra(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	hasher := &countingHasher{}
	service, err := services.NewIdentityService(database.NewIdentityRepository(session), database.NewLoginFailureRepository(session), hasher, services.SystemClock{}, contracts.LockoutConfig{MaxFailures: 5, Window: time.Hour})
	require.NoError(t, err)
	service.WithHashConcurrency(32, 10*time.Second)
	ctx := context.Background()
	email := uuid.NewString() + "@example.com"
	_, err = service.Register(ctx, email, "correct horse")
	require.NoError(t, err)

	var (
		wg      sync.WaitGroup
		invalid atomic.Int32
		locked  atomic.Int32
	)
	start := make(chan struct{})
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := service.Verify(ctx, email, "wrong horse")
			var tooMany *services.ErrTooManyAttempts
			switch {
			case errors.Is(err, services.ErrInvalidCredentials):
				invalid.Add(1)
			case errors.As(err, &tooMany):
				locked.Add(1)
			default:
				t.Errorf("unexpected error %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	require.Equal(t, int32(5), invalid.Load())
	require.Equal(t, int32(15), locked.Load())
	require.Equal(t, int32(5), hasher.compares.Load())

	_, err = service.Verify(ctx, email, "correct horse")
	var tooMany *services.ErrTooManyAttempts
	require.ErrorAs(t, err, &tooMany)
	require.Equal(t, int32(5), hasher.compares.Load())
}
