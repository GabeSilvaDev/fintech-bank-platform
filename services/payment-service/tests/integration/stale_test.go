//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/app/models"
	"github.com/fintech-bank-platform/payment-service/internal/infrastructure/database"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newStaleCandidate(status models.Status, updatedAt time.Time) *models.Payment {
	return &models.Payment{
		ID:             uuid.New(),
		AccountID:      uuid.New(),
		Method:         models.MethodPix,
		Status:         status,
		AmountCents:    1000,
		Currency:       "BRL",
		Recipient:      "Ana",
		PixKey:         "ana@example.com",
		IdempotencyKey: "stale-" + uuid.NewString(),
		CreatedAt:      updatedAt,
		UpdatedAt:      updatedAt,
	}
}

func TestListStaleReturnsOnlyStaleNonTerminalPayments(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewPaymentRepository(session)
	ctx := context.Background()
	now := time.Now().UTC()

	stalePending := newStaleCandidate(models.StatusPending, now.Add(-2*time.Minute))
	require.NoError(t, repo.Create(ctx, stalePending))
	freshPending := newStaleCandidate(models.StatusPending, now)
	require.NoError(t, repo.Create(ctx, freshPending))
	staleCompleted := newStaleCandidate(models.StatusCompleted, now.Add(-2*time.Minute))
	require.NoError(t, repo.Create(ctx, staleCompleted))

	stale, err := repo.ListStale(ctx, now.Add(-time.Minute), 10)

	require.NoError(t, err)
	require.Len(t, stale, 1)
	require.Equal(t, stalePending.ID, stale[0].ID)
	require.Equal(t, models.StatusPending, stale[0].Status)
}

func TestListStaleRespectsLimit(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewPaymentRepository(session)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 3; i++ {
		payment := newStaleCandidate(models.StatusDebited, now.Add(-2*time.Minute))
		require.NoError(t, repo.Create(ctx, payment))
	}

	stale, err := repo.ListStale(ctx, now.Add(-time.Minute), 2)

	require.NoError(t, err)
	require.Len(t, stale, 2)
}
