//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/app/models"
	"github.com/fintech-bank-platform/account-service/internal/infrastructure/database"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAccountRepository(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewAccountRepository(session)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	account := &models.Account{AccountID: uuid.New(), UserID: uuid.New(), Agency: "0001", Number: "12345678", Type: models.AccountTypeChecking, Status: models.AccountStatusActive, Currency: "BRL", BalanceCents: 0, CreatedAt: now, UpdatedAt: now}

	reserved, err := repo.ReserveNumber(ctx, "0001", "12345678", account.AccountID)
	require.NoError(t, err)
	require.True(t, reserved)
	reserved, err = repo.ReserveNumber(ctx, "0001", "12345678", uuid.New())
	require.NoError(t, err)
	require.False(t, reserved)

	require.NoError(t, repo.Create(ctx, account))

	got, err := repo.Get(ctx, account.AccountID)
	require.NoError(t, err)
	require.Equal(t, account.Number, got.Number)
	require.Equal(t, models.AccountStatusActive, got.Status)
	require.Nil(t, got.ClosedAt)
	require.WithinDuration(t, now, got.CreatedAt, time.Millisecond)

	_, err = repo.Get(ctx, uuid.New())
	require.ErrorIs(t, err, models.ErrNotFound)

	list, err := repo.ListByUser(ctx, account.UserID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	list, err = repo.ListByUser(ctx, uuid.New())
	require.NoError(t, err)
	require.Empty(t, list)

	applied, err := repo.CompareAndSetBalance(ctx, account.AccountID, 0, 1050, now)
	require.NoError(t, err)
	require.True(t, applied)
	applied, err = repo.CompareAndSetBalance(ctx, account.AccountID, 0, 2000, now)
	require.NoError(t, err)
	require.False(t, applied)
	got, _ = repo.Get(ctx, account.AccountID)
	require.Equal(t, int64(1050), got.BalanceCents)

	require.NoError(t, repo.UpdateStatus(ctx, account.AccountID, models.AccountStatusBlocked, now, nil))
	got, _ = repo.Get(ctx, account.AccountID)
	require.Equal(t, models.AccountStatusBlocked, got.Status)
	require.Nil(t, got.ClosedAt)

	applied, err = repo.CompareAndSetBalance(ctx, account.AccountID, 1050, 1000, now)
	require.NoError(t, err)
	require.False(t, applied)
	got, _ = repo.Get(ctx, account.AccountID)
	require.Equal(t, int64(1050), got.BalanceCents)

	applied, err = repo.CloseIfEmpty(ctx, account.AccountID, now)
	require.NoError(t, err)
	require.False(t, applied)
	got, _ = repo.Get(ctx, account.AccountID)
	require.Equal(t, models.AccountStatusBlocked, got.Status)

	closedAt := now.Add(time.Minute)
	require.NoError(t, repo.UpdateStatus(ctx, account.AccountID, models.AccountStatusClosed, closedAt, &closedAt))
	got, _ = repo.Get(ctx, account.AccountID)
	require.Equal(t, models.AccountStatusClosed, got.Status)
	require.NotNil(t, got.ClosedAt)
	require.WithinDuration(t, closedAt, *got.ClosedAt, time.Millisecond)
}

func TestAccountRepositoryCloseIfEmpty(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewAccountRepository(session)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	account := &models.Account{AccountID: uuid.New(), UserID: uuid.New(), Agency: "0001", Number: "87654321", Type: models.AccountTypeSavings, Status: models.AccountStatusActive, Currency: "BRL", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, repo.Create(ctx, account))

	closedAt := now.Add(time.Minute)
	applied, err := repo.CloseIfEmpty(ctx, account.AccountID, closedAt)
	require.NoError(t, err)
	require.True(t, applied)
	got, err := repo.Get(ctx, account.AccountID)
	require.NoError(t, err)
	require.Equal(t, models.AccountStatusClosed, got.Status)
	require.NotNil(t, got.ClosedAt)
	require.WithinDuration(t, closedAt, *got.ClosedAt, time.Millisecond)
	require.WithinDuration(t, closedAt, got.UpdatedAt, time.Millisecond)

	applied, err = repo.CloseIfEmpty(ctx, account.AccountID, closedAt.Add(time.Minute))
	require.NoError(t, err)
	require.False(t, applied)
	got, _ = repo.Get(ctx, account.AccountID)
	require.WithinDuration(t, closedAt, *got.ClosedAt, time.Millisecond)

	applied, err = repo.CompareAndSetBalance(ctx, account.AccountID, 0, 100, now)
	require.NoError(t, err)
	require.False(t, applied)
}

func TestCustomerRepository(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewCustomerRepository(session)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	customer := &models.Customer{UserID: uuid.New(), Name: "Ana Souza", Email: "ana@example.com", Document: "52998224725", Phone: "11999887766", KYCStatus: "pending", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, repo.Upsert(ctx, customer))

	got, err := repo.Get(ctx, customer.UserID)
	require.NoError(t, err)
	require.Equal(t, "Ana Souza", got.Name)
	require.Equal(t, "pending", got.KYCStatus)

	_, err = repo.Get(ctx, uuid.New())
	require.ErrorIs(t, err, models.ErrNotFound)

	name := "Ana Lima"
	later := now.Add(time.Minute)
	require.NoError(t, repo.UpdateProfile(ctx, customer.UserID, &name, nil, nil, later))
	got, _ = repo.Get(ctx, customer.UserID)
	require.Equal(t, "Ana Lima", got.Name)
	require.Equal(t, "ana@example.com", got.Email)
	require.WithinDuration(t, later, got.UpdatedAt, time.Millisecond)

	email, phone := "lima@example.com", "11988887777"
	require.NoError(t, repo.UpdateProfile(ctx, customer.UserID, nil, &email, &phone, later))
	got, _ = repo.Get(ctx, customer.UserID)
	require.Equal(t, "lima@example.com", got.Email)
	require.Equal(t, "11988887777", got.Phone)
}

func TestProcessedEventStore(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	store := database.NewProcessedEventStore(session)
	ctx := context.Background()
	id := uuid.New()

	first, err := store.MarkProcessed(ctx, id)
	require.NoError(t, err)
	require.True(t, first)

	first, err = store.MarkProcessed(ctx, id)
	require.NoError(t, err)
	require.False(t, first)
}
