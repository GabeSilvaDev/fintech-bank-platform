//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/fintech-bank-platform/transaction-service/internal/infrastructure/database"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTransactionRepository(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewTransactionRepository(session)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	from, to := uuid.New(), uuid.New()

	transfer := &models.Transaction{ID: uuid.New(), Type: models.TypeTransfer, Status: models.StatusPending, AccountID: from, CounterpartyID: &to, AmountCents: 3000, Currency: "BRL", Description: "rent", IdempotencyKey: "tr-1", CreatedAt: now, UpdatedAt: now}

	owner, err := repo.ReserveKey(ctx, "tr-1", transfer.ID)
	require.NoError(t, err)
	require.Equal(t, transfer.ID, owner)
	owner, err = repo.ReserveKey(ctx, "tr-1", uuid.New())
	require.NoError(t, err)
	require.Equal(t, transfer.ID, owner)

	require.NoError(t, repo.Create(ctx, transfer))

	got, err := repo.Get(ctx, transfer.ID)
	require.NoError(t, err)
	require.Equal(t, models.TypeTransfer, got.Type)
	require.Equal(t, models.StatusPending, got.Status)
	require.Equal(t, from, got.AccountID)
	require.Equal(t, to, *got.CounterpartyID)
	require.Equal(t, int64(3000), got.AmountCents)
	require.Equal(t, "rent", got.Description)
	require.Equal(t, "tr-1", got.IdempotencyKey)
	require.Equal(t, "", got.FailureReason)
	require.Nil(t, got.FromBalanceCents)
	require.Nil(t, got.ToBalanceCents)
	require.Nil(t, got.CompletedAt)
	require.WithinDuration(t, now, got.CreatedAt, time.Millisecond)

	_, err = repo.Get(ctx, uuid.New())
	require.ErrorIs(t, err, domain.ErrNotFound)

	later := now.Add(time.Second)
	deposit := &models.Transaction{ID: uuid.New(), Type: models.TypeDeposit, Status: models.StatusPending, AccountID: to, AmountCents: 500, Currency: "BRL", IdempotencyKey: "dep-1", CreatedAt: later, UpdatedAt: later}
	require.NoError(t, repo.Create(ctx, deposit))

	list, err := repo.ListByAccount(ctx, to, 10)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, deposit.ID, list[0].ID)
	require.Equal(t, transfer.ID, list[1].ID)
	list, err = repo.ListByAccount(ctx, to, 1)
	require.NoError(t, err)
	require.Len(t, list, 1)
	list, err = repo.ListByAccount(ctx, from, 10)
	require.NoError(t, err)
	require.Len(t, list, 1)
	list, err = repo.ListByAccount(ctx, uuid.New(), 10)
	require.NoError(t, err)
	require.Empty(t, list)

	balance := int64(7000)
	applied, err := repo.Transition(ctx, transfer.ID, models.StatusPending, models.StatusDebited, models.Patch{FromBalanceCents: &balance, UpdatedAt: later})
	require.NoError(t, err)
	require.True(t, applied)
	applied, err = repo.Transition(ctx, transfer.ID, models.StatusPending, models.StatusDebited, models.Patch{FromBalanceCents: &balance, UpdatedAt: later})
	require.NoError(t, err)
	require.False(t, applied)
	applied, err = repo.Transition(ctx, uuid.New(), models.StatusPending, models.StatusDebited, models.Patch{UpdatedAt: later})
	require.NoError(t, err)
	require.False(t, applied)

	got, _ = repo.Get(ctx, transfer.ID)
	require.Equal(t, models.StatusDebited, got.Status)
	require.Equal(t, int64(7000), *got.FromBalanceCents)
	require.Nil(t, got.ToBalanceCents)
	require.Nil(t, got.CompletedAt)

	toBalance := int64(3000)
	reason := "account_not_active"
	applied, err = repo.Transition(ctx, transfer.ID, models.StatusDebited, models.StatusCompleted, models.Patch{ToBalanceCents: &toBalance, FailureReason: &reason, CompletedAt: &later, UpdatedAt: later})
	require.NoError(t, err)
	require.True(t, applied)
	got, _ = repo.Get(ctx, transfer.ID)
	require.Equal(t, models.StatusCompleted, got.Status)
	require.Equal(t, int64(7000), *got.FromBalanceCents)
	require.Equal(t, int64(3000), *got.ToBalanceCents)
	require.Equal(t, "account_not_active", got.FailureReason)
	require.WithinDuration(t, later, *got.CompletedAt, time.Millisecond)
	require.WithinDuration(t, later, got.UpdatedAt, time.Millisecond)
}

func TestProcessedEventStore(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	store := database.NewProcessedEventStore(session)
	id := uuid.New()

	first, err := store.MarkProcessed(context.Background(), id)
	require.NoError(t, err)
	require.True(t, first)
	again, err := store.MarkProcessed(context.Background(), id)
	require.NoError(t, err)
	require.False(t, again)
}
