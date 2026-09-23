//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/fintech-bank-platform/transaction-service/internal/infrastructure/database"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newStaleCandidate(status models.TransactionStatus, updatedAt time.Time) *models.Transaction {
	return &models.Transaction{
		ID:             uuid.New(),
		Type:           models.TypeDeposit,
		Status:         status,
		AccountID:      uuid.New(),
		AmountCents:    1000,
		Currency:       "BRL",
		IdempotencyKey: "stale-" + uuid.NewString(),
		CreatedAt:      updatedAt,
		UpdatedAt:      updatedAt,
	}
}

func TestListStaleReturnsOnlyStaleNonTerminalTransactions(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewTransactionRepository(session)
	ctx := context.Background()
	now := time.Now().UTC()

	stalePending := newStaleCandidate(models.StatusPending, now.Add(-2*time.Minute))
	require.NoError(t, repo.Create(ctx, stalePending))
	freshPending := newStaleCandidate(models.StatusPending, now)
	require.NoError(t, repo.Create(ctx, freshPending))
	staleCompleted := newStaleCandidate(models.StatusCompleted, now.Add(-2*time.Minute))
	require.NoError(t, repo.Create(ctx, staleCompleted))

	stale, err := repo.ListStale(ctx, now.Add(-time.Minute), 24*time.Hour, 10)

	require.NoError(t, err)
	require.Len(t, stale, 1)
	require.Equal(t, stalePending.ID, stale[0].ID)
	require.Equal(t, models.StatusPending, stale[0].Status)
}

func TestListStaleRespectsLimit(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewTransactionRepository(session)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 3; i++ {
		tx := newStaleCandidate(models.StatusPending, now.Add(-2*time.Minute))
		require.NoError(t, repo.Create(ctx, tx))
	}

	stale, err := repo.ListStale(ctx, now.Add(-time.Minute), 24*time.Hour, 2)

	require.NoError(t, err)
	require.Len(t, stale, 2)
}

func TestListStaleSkipsRecordsAlreadyTouchedPastTheMaxAge(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewTransactionRepository(session)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	expired := newStaleCandidate(models.StatusPending, now.Add(-2*time.Hour))
	expired.CreatedAt = now.Add(-25 * time.Hour)
	require.NoError(t, repo.Create(ctx, expired))
	alerted := newStaleCandidate(models.StatusPending, now.Add(-30*time.Minute))
	alerted.CreatedAt = now.Add(-25 * time.Hour)
	require.NoError(t, repo.Create(ctx, alerted))

	stale, err := repo.ListStale(ctx, now.Add(-time.Minute), 24*time.Hour, 10)

	require.NoError(t, err)
	require.Len(t, stale, 1)
	require.Equal(t, expired.ID, stale[0].ID)

	applied, err := repo.Touch(ctx, expired.ID, expired.Status, stale[0].UpdatedAt, now.Add(-10*time.Minute))
	require.NoError(t, err)
	require.True(t, applied)

	stale, err = repo.ListStale(ctx, now.Add(-time.Minute), 24*time.Hour, 10)

	require.NoError(t, err)
	require.Empty(t, stale)
}
