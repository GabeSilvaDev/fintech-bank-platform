//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
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

	owner, err := repo.ReserveKey(ctx, from, "tr-1", transfer.ID)
	require.NoError(t, err)
	require.Equal(t, transfer.ID, owner)
	owner, err = repo.ReserveKey(ctx, from, "tr-1", uuid.New())
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

	page, err := repo.ListByAccount(ctx, to, nil, 10)
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.Equal(t, deposit.ID, page.Items[0].ID)
	require.Equal(t, transfer.ID, page.Items[1].ID)
	page, err = repo.ListByAccount(ctx, to, nil, 1)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	page, err = repo.ListByAccount(ctx, from, nil, 10)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	page, err = repo.ListByAccount(ctx, uuid.New(), nil, 10)
	require.NoError(t, err)
	require.Empty(t, page.Items)

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

func TestTransactionRepositoryReserveKeyIsScopedPerAccount(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewTransactionRepository(session)
	ctx := context.Background()
	accountA, accountB := uuid.New(), uuid.New()
	idA, idB := uuid.New(), uuid.New()

	ownerA, err := repo.ReserveKey(ctx, accountA, "shared-key", idA)
	require.NoError(t, err)
	require.Equal(t, idA, ownerA)

	ownerB, err := repo.ReserveKey(ctx, accountB, "shared-key", idB)
	require.NoError(t, err)
	require.Equal(t, idB, ownerB)

	other := uuid.New()
	ownerA, err = repo.ReserveKey(ctx, accountA, "shared-key", other)
	require.NoError(t, err)
	require.Equal(t, idA, ownerA)
}

func TestTransactionRepositoryTouch(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewTransactionRepository(session)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	tx := &models.Transaction{ID: uuid.New(), Type: models.TypeDeposit, Status: models.StatusPending, AccountID: uuid.New(), AmountCents: 1000, Currency: "BRL", IdempotencyKey: "touch-1", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, repo.Create(ctx, tx))

	touchedAt := now.Add(time.Minute)
	applied, err := repo.Touch(ctx, tx.ID, models.StatusPending, now, touchedAt)
	require.NoError(t, err)
	require.True(t, applied)

	got, err := repo.Get(ctx, tx.ID)
	require.NoError(t, err)
	require.WithinDuration(t, touchedAt, got.UpdatedAt, time.Millisecond)

	applied, err = repo.Touch(ctx, tx.ID, models.StatusPending, now, now.Add(2*time.Minute))
	require.NoError(t, err)
	require.False(t, applied)

	applied, err = repo.Touch(ctx, tx.ID, models.StatusDebited, touchedAt, now.Add(3*time.Minute))
	require.NoError(t, err)
	require.False(t, applied)

	got, err = repo.Get(ctx, tx.ID)
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

func TestTransactionRepositoryListByAccountOrdersAndSkipsMissing(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewTransactionRepository(session)
	ctx := context.Background()
	account := uuid.New()
	base := time.Now().UTC().Truncate(time.Millisecond)

	created := make([]*models.Transaction, 3)
	for i := 0; i < 3; i++ {
		tx := &models.Transaction{ID: uuid.New(), Type: models.TypeDeposit, Status: models.StatusPending, AccountID: account, AmountCents: int64(100 + i), Currency: "BRL", IdempotencyKey: uuid.NewString(), CreatedAt: base.Add(time.Duration(i+1) * time.Second), UpdatedAt: base.Add(time.Duration(i+1) * time.Second)}
		require.NoError(t, repo.Create(ctx, tx))
		created[i] = tx
	}

	missing := uuid.New()
	require.NoError(t, session.Query("INSERT INTO transactions_by_account (account_id, created_at, transaction_id) VALUES (?, ?, ?)", gocql.UUID(account), base, gocql.UUID(missing)).WithContext(ctx).Exec())

	page, err := repo.ListByAccount(ctx, account, nil, 4)
	require.NoError(t, err)
	require.Len(t, page.Items, 3)
	require.Equal(t, created[2].ID, page.Items[0].ID)
	require.Equal(t, created[1].ID, page.Items[1].ID)
	require.Equal(t, created[0].ID, page.Items[2].ID)
	require.Equal(t, 4, page.Scanned)
	require.NotNil(t, page.Last)
	require.WithinDuration(t, base, *page.Last, time.Millisecond)
}

func TestTransactionRepositoryListByAccountChunksBeyondHundred(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewTransactionRepository(session)
	ctx := context.Background()
	account := uuid.New()
	base := time.Now().UTC().Truncate(time.Millisecond)

	ids := make([]uuid.UUID, 150)
	for i := 0; i < 150; i++ {
		tx := &models.Transaction{ID: uuid.New(), Type: models.TypeDeposit, Status: models.StatusPending, AccountID: account, AmountCents: int64(i), Currency: "BRL", IdempotencyKey: uuid.NewString(), CreatedAt: base.Add(time.Duration(i) * time.Millisecond), UpdatedAt: base.Add(time.Duration(i) * time.Millisecond)}
		require.NoError(t, repo.Create(ctx, tx))
		ids[i] = tx.ID
	}

	page, err := repo.ListByAccount(ctx, account, nil, 200)
	require.NoError(t, err)
	require.Len(t, page.Items, 150)
	for i, tx := range page.Items {
		require.Equal(t, ids[149-i], tx.ID)
	}
}

func TestTransactionRepositoryListByAccountPagesWithBeforeCursor(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewTransactionRepository(session)
	ctx := context.Background()
	account := uuid.New()
	base := time.Now().UTC().Truncate(time.Millisecond)

	created := make([]*models.Transaction, 5)
	for i := 0; i < 5; i++ {
		tx := &models.Transaction{ID: uuid.New(), Type: models.TypeDeposit, Status: models.StatusPending, AccountID: account, AmountCents: int64(100 + i), Currency: "BRL", IdempotencyKey: uuid.NewString(), CreatedAt: base.Add(time.Duration(i) * time.Second), UpdatedAt: base.Add(time.Duration(i) * time.Second)}
		require.NoError(t, repo.Create(ctx, tx))
		created[i] = tx
	}

	var seen []uuid.UUID
	var before *time.Time
	for i := 0; i < 10; i++ {
		page, err := repo.ListByAccount(ctx, account, before, 2)
		require.NoError(t, err)
		if len(page.Items) == 0 {
			break
		}
		for _, tx := range page.Items {
			seen = append(seen, tx.ID)
		}
		if len(page.Items) < 2 {
			break
		}
		last := page.Items[len(page.Items)-1].CreatedAt
		before = &last
	}

	require.Len(t, seen, 5)
	require.Equal(t, created[4].ID, seen[0])
	require.Equal(t, created[3].ID, seen[1])
	require.Equal(t, created[2].ID, seen[2])
	require.Equal(t, created[1].ID, seen[3])
	require.Equal(t, created[0].ID, seen[4])
}

func TestTransactionRepositoryListByAccountEmpty(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewTransactionRepository(session)

	page, err := repo.ListByAccount(context.Background(), uuid.New(), nil, 10)
	require.NoError(t, err)
	require.NotNil(t, page.Items)
	require.Empty(t, page.Items)
}
