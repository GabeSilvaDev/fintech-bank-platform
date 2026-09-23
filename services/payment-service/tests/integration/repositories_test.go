//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/payment-service/internal/infrastructure/database"
	"github.com/fintech-bank-platform/pkg/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPaymentRepository(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewPaymentRepository(session)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	account := uuid.New()

	ted := &models.Payment{ID: uuid.New(), AccountID: account, Method: models.MethodTED, Status: models.StatusPending, AmountCents: 100000, Currency: "BRL", Recipient: "Bruno Lima", TED: &models.TEDDetails{BankCode: "341", Branch: "0001", Account: "123456", Document: "52998224725"}, Description: "rent", IdempotencyKey: "ted-1", CreatedAt: now, UpdatedAt: now}

	owner, err := repo.ReserveKey(ctx, account, "ted-1", ted.ID)
	require.NoError(t, err)
	require.Equal(t, ted.ID, owner)
	owner, err = repo.ReserveKey(ctx, account, "ted-1", uuid.New())
	require.NoError(t, err)
	require.Equal(t, ted.ID, owner)
	other := uuid.New()
	owner, err = repo.ReserveKey(ctx, uuid.New(), "ted-1", other)
	require.NoError(t, err)
	require.Equal(t, other, owner)

	require.NoError(t, repo.Create(ctx, ted))
	got, err := repo.Get(ctx, ted.ID)
	require.NoError(t, err)
	require.Equal(t, models.MethodTED, got.Method)
	require.Equal(t, models.StatusPending, got.Status)
	require.Equal(t, int64(100000), got.AmountCents)
	require.Equal(t, "Bruno Lima", got.Recipient)
	require.Equal(t, ted.TED, got.TED)
	require.Equal(t, "", got.PixKey)
	require.Equal(t, "rent", got.Description)
	require.Nil(t, got.BalanceAfterCents)
	require.Nil(t, got.CompletedAt)
	require.WithinDuration(t, now, got.CreatedAt, time.Millisecond)

	_, err = repo.Get(ctx, uuid.New())
	require.ErrorIs(t, err, domain.ErrNotFound)

	later := now.Add(time.Second)
	pix := &models.Payment{ID: uuid.New(), AccountID: account, Method: models.MethodPix, Status: models.StatusPending, AmountCents: 500, Currency: "BRL", Recipient: "Ana", PixKey: "ana@example.com", IdempotencyKey: "pix-1", CreatedAt: later, UpdatedAt: later}
	require.NoError(t, repo.Create(ctx, pix))
	got, _ = repo.Get(ctx, pix.ID)
	require.Nil(t, got.TED)
	require.Equal(t, "ana@example.com", got.PixKey)

	list, err := repo.ListByAccount(ctx, account, 10)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, pix.ID, list[0].ID)
	list, err = repo.ListByAccount(ctx, account, 1)
	require.NoError(t, err)
	require.Len(t, list, 1)
	list, err = repo.ListByAccount(ctx, uuid.New(), 10)
	require.NoError(t, err)
	require.Empty(t, list)

	balance := int64(90000)
	applied, err := repo.Transition(ctx, ted.ID, models.StatusPending, models.StatusDebited, models.Patch{BalanceAfterCents: &balance, UpdatedAt: later})
	require.NoError(t, err)
	require.True(t, applied)
	applied, err = repo.Transition(ctx, ted.ID, models.StatusPending, models.StatusDebited, models.Patch{UpdatedAt: later})
	require.NoError(t, err)
	require.False(t, applied)
	applied, err = repo.Transition(ctx, uuid.New(), models.StatusPending, models.StatusDebited, models.Patch{UpdatedAt: later})
	require.NoError(t, err)
	require.False(t, applied)

	external := "ted_" + uuid.NewString()
	require.NoError(t, repo.BindExternalID(ctx, external, ted.ID))
	found, err := repo.FindByExternalID(ctx, external)
	require.NoError(t, err)
	require.Equal(t, ted.ID, found)
	_, err = repo.FindByExternalID(ctx, "missing")
	require.ErrorIs(t, err, domain.ErrNotFound)

	reason := "invalid_destination"
	applied, err = repo.Transition(ctx, ted.ID, models.StatusDebited, models.StatusSubmitted, models.Patch{ExternalID: &external, UpdatedAt: later})
	require.NoError(t, err)
	require.True(t, applied)
	applied, err = repo.Transition(ctx, ted.ID, models.StatusSubmitted, models.StatusRefunding, models.Patch{FailureReason: &reason, CompletedAt: &later, UpdatedAt: later})
	require.NoError(t, err)
	require.True(t, applied)

	got, _ = repo.Get(ctx, ted.ID)
	require.Equal(t, models.StatusRefunding, got.Status)
	require.Equal(t, external, got.ExternalID)
	require.Equal(t, "invalid_destination", got.FailureReason)
	require.Equal(t, int64(90000), *got.BalanceAfterCents)
	require.WithinDuration(t, later, *got.CompletedAt, time.Millisecond)
	require.WithinDuration(t, later, got.UpdatedAt, time.Millisecond)
}

func TestPaymentRepositoryTouch(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewPaymentRepository(session)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	payment := &models.Payment{ID: uuid.New(), AccountID: uuid.New(), Method: models.MethodPix, Status: models.StatusPending, AmountCents: 1000, Currency: "BRL", Recipient: "Ana", PixKey: "ana@example.com", IdempotencyKey: "touch-1", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, repo.Create(ctx, payment))

	touchedAt := now.Add(time.Minute)
	applied, err := repo.Touch(ctx, payment.ID, models.StatusPending, now, touchedAt)
	require.NoError(t, err)
	require.True(t, applied)

	got, err := repo.Get(ctx, payment.ID)
	require.NoError(t, err)
	require.WithinDuration(t, touchedAt, got.UpdatedAt, time.Millisecond)

	applied, err = repo.Touch(ctx, payment.ID, models.StatusPending, now, now.Add(2*time.Minute))
	require.NoError(t, err)
	require.False(t, applied)

	applied, err = repo.Touch(ctx, payment.ID, models.StatusDebited, touchedAt, now.Add(3*time.Minute))
	require.NoError(t, err)
	require.False(t, applied)

	got, err = repo.Get(ctx, payment.ID)
	require.NoError(t, err)
	require.WithinDuration(t, touchedAt, got.UpdatedAt, time.Millisecond)
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
