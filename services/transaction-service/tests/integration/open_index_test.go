//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/transaction-service/internal/app/models"
	"github.com/fintech-bank-platform/transaction-service/internal/infrastructure/database"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func openRowExists(t *testing.T, session *gocql.Session, id uuid.UUID) bool {
	t.Helper()
	var count int
	require.NoError(t, session.Query("SELECT count(*) FROM open_transactions WHERE bucket = ? AND transaction_id = ?", id.String()[:1], gocql.UUID(id)).Scan(&count))
	return count > 0
}

func insertOpenRow(t *testing.T, session *gocql.Session, id uuid.UUID) {
	t.Helper()
	require.NoError(t, session.Query("INSERT INTO open_transactions (bucket, transaction_id) VALUES (?, ?)", id.String()[:1], gocql.UUID(id)).Exec())
}

func deleteOpenRow(t *testing.T, session *gocql.Session, id uuid.UUID) {
	t.Helper()
	require.NoError(t, session.Query("DELETE FROM open_transactions WHERE bucket = ? AND transaction_id = ?", id.String()[:1], gocql.UUID(id)).Exec())
}

func TestCreateIndexesOnlyOpenTransactions(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewTransactionRepository(session)
	ctx := context.Background()
	now := time.Now().UTC()

	open := newStaleCandidate(models.StatusPending, now)
	require.NoError(t, repo.Create(ctx, open))
	settled := newStaleCandidate(models.StatusCompleted, now)
	require.NoError(t, repo.Create(ctx, settled))

	require.True(t, openRowExists(t, session, open.ID))
	require.False(t, openRowExists(t, session, settled.ID))
}

func TestTransitionToATerminalStatusRemovesTheIndexRow(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewTransactionRepository(session)
	ctx := context.Background()
	now := time.Now().UTC()

	tx := newStaleCandidate(models.StatusPending, now)
	tx.Type = models.TypeTransfer
	require.NoError(t, repo.Create(ctx, tx))

	applied, err := repo.Transition(ctx, tx.ID, models.StatusPending, models.StatusDebited, models.Patch{UpdatedAt: now})
	require.NoError(t, err)
	require.True(t, applied)
	require.True(t, openRowExists(t, session, tx.ID))

	applied, err = repo.Transition(ctx, tx.ID, models.StatusPending, models.StatusFailed, models.Patch{UpdatedAt: now})
	require.NoError(t, err)
	require.False(t, applied)
	require.True(t, openRowExists(t, session, tx.ID))

	applied, err = repo.Transition(ctx, tx.ID, models.StatusDebited, models.StatusCompleted, models.Patch{UpdatedAt: now})
	require.NoError(t, err)
	require.True(t, applied)
	require.False(t, openRowExists(t, session, tx.ID))
}

func TestListStalePrunesIndexRowsOfSettledAndMissingTransactions(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewTransactionRepository(session)
	ctx := context.Background()
	now := time.Now().UTC()

	stale := newStaleCandidate(models.StatusDebited, now.Add(-2*time.Minute))
	require.NoError(t, repo.Create(ctx, stale))
	settled := newStaleCandidate(models.StatusCompleted, now.Add(-2*time.Minute))
	require.NoError(t, repo.Create(ctx, settled))
	insertOpenRow(t, session, settled.ID)
	missing := uuid.New()
	insertOpenRow(t, session, missing)

	listed, err := repo.ListStale(ctx, now.Add(-time.Minute), 24*time.Hour, 10)

	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, stale.ID, listed[0].ID)
	require.Equal(t, models.StatusDebited, listed[0].Status)
	require.True(t, openRowExists(t, session, stale.ID))
	require.False(t, openRowExists(t, session, settled.ID))
	require.False(t, openRowExists(t, session, missing))
}

func TestListStaleFindsStaleTransactionsBeyondTheFirstChunk(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewTransactionRepository(session)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 150; i++ {
		require.NoError(t, repo.Create(ctx, newStaleCandidate(models.StatusPending, now)))
	}
	stale := newStaleCandidate(models.StatusReversing, now.Add(-2*time.Minute))
	require.NoError(t, repo.Create(ctx, stale))

	listed, err := repo.ListStale(ctx, now.Add(-time.Minute), 24*time.Hour, 10)

	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, stale.ID, listed[0].ID)
}

func TestListStaleIgnoresTransactionsMissingFromTheIndex(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewTransactionRepository(session)
	ctx := context.Background()
	now := time.Now().UTC()

	stale := newStaleCandidate(models.StatusPending, now.Add(-2*time.Minute))
	require.NoError(t, repo.Create(ctx, stale))
	deleteOpenRow(t, session, stale.ID)

	listed, err := repo.ListStale(ctx, now.Add(-time.Minute), 24*time.Hour, 10)

	require.NoError(t, err)
	require.Empty(t, listed)
}

func TestReindexRestoresIndexRowsOfOpenTransactions(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewTransactionRepository(session)
	ctx := context.Background()
	now := time.Now().UTC()

	lost := newStaleCandidate(models.StatusPending, now.Add(-2*time.Minute))
	require.NoError(t, repo.Create(ctx, lost))
	deleteOpenRow(t, session, lost.ID)
	kept := newStaleCandidate(models.StatusDebited, now)
	require.NoError(t, repo.Create(ctx, kept))
	settled := newStaleCandidate(models.StatusFailed, now.Add(-2*time.Minute))
	require.NoError(t, repo.Create(ctx, settled))

	ensured, err := repo.Reindex(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, ensured)
	require.True(t, openRowExists(t, session, lost.ID))
	require.True(t, openRowExists(t, session, kept.ID))
	require.False(t, openRowExists(t, session, settled.ID))

	listed, err := repo.ListStale(ctx, now.Add(-time.Minute), 24*time.Hour, 10)

	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, lost.ID, listed[0].ID)
}

func TestReindexReportsScanFailures(t *testing.T) {
	session, _ := throwawayKeyspace(t)
	repo := database.NewTransactionRepository(session)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.Reindex(ctx)

	require.Error(t, err)
}
