//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/internal/infrastructure/database"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestBalanceOperationRepository(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewBalanceOperationRepository(session)
	ctx := context.Background()
	accountID := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)

	reserved, err := repo.Reserve(ctx, accountID, "k-1", "credit", now)
	require.NoError(t, err)
	require.True(t, reserved)

	reserved, err = repo.Reserve(ctx, accountID, "k-1", "credit", now)
	require.NoError(t, err)
	require.False(t, reserved)

	operation, err := repo.Get(ctx, accountID, "k-1")
	require.NoError(t, err)
	require.Equal(t, models.OperationPending, operation.Status)
	require.Equal(t, "credit", operation.Kind)
	require.Empty(t, operation.Result)

	completedAt := now.Add(time.Minute)
	require.NoError(t, repo.Complete(ctx, accountID, "k-1", `{"balance_after":10}`, completedAt))

	operation, err = repo.Get(ctx, accountID, "k-1")
	require.NoError(t, err)
	require.Equal(t, models.OperationDone, operation.Status)
	require.Equal(t, `{"balance_after":10}`, operation.Result)
	require.WithinDuration(t, completedAt, operation.UpdatedAt, time.Millisecond)

	require.NoError(t, repo.Complete(ctx, accountID, "k-1", `{"balance_after":20}`, completedAt.Add(time.Minute)))
	operation, err = repo.Get(ctx, accountID, "k-1")
	require.NoError(t, err)
	require.Equal(t, models.OperationDone, operation.Status)
	require.Equal(t, `{"balance_after":10}`, operation.Result)
	require.WithinDuration(t, completedAt, operation.UpdatedAt, time.Millisecond)

	missingKey := uuid.NewString()
	require.NoError(t, repo.Complete(ctx, accountID, missingKey, `{"balance_after":30}`, completedAt))
	_, err = repo.Get(ctx, accountID, missingKey)
	require.ErrorIs(t, err, domain.ErrNotFound)

	require.NoError(t, repo.Release(ctx, accountID, "k-1"))
	operation, err = repo.Get(ctx, accountID, "k-1")
	require.NoError(t, err)
	require.Equal(t, models.OperationDone, operation.Status)

	pendingKey := uuid.NewString()
	reserved, err = repo.Reserve(ctx, accountID, pendingKey, "debit", now)
	require.NoError(t, err)
	require.True(t, reserved)

	require.NoError(t, repo.Release(ctx, accountID, pendingKey))
	_, err = repo.Get(ctx, accountID, pendingKey)
	require.ErrorIs(t, err, domain.ErrNotFound)

	_, err = repo.Get(ctx, accountID, uuid.NewString())
	require.ErrorIs(t, err, domain.ErrNotFound)
}
