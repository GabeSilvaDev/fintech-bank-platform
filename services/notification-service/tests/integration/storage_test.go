//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/fintech-bank-platform/notification-service/internal/app/models"
	"github.com/fintech-bank-platform/notification-service/internal/infrastructure/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPing(t *testing.T) {
	client := redisClient(t)

	require.NoError(t, storage.Ping(client)(context.Background()))
}

func TestStoreMarksOnce(t *testing.T) {
	client := redisClient(t)
	store := storage.NewStore(client)
	eventID := uuid.New()
	key := "notification:processed:" + eventID.String()
	t.Cleanup(func() {
		client.Del(context.Background(), key)
	})

	first, err := store.MarkProcessed(context.Background(), eventID)
	require.NoError(t, err)
	require.True(t, first)

	second, err := store.MarkProcessed(context.Background(), eventID)
	require.NoError(t, err)
	require.False(t, second)

	ttl, err := client.TTL(context.Background(), key).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, 6*24*time.Hour)
	require.LessOrEqual(t, ttl, 7*24*time.Hour)
}

func TestHistoryKeepsTheNewestRecords(t *testing.T) {
	client := redisClient(t)
	history := storage.NewHistory(client, 2)
	userID := uuid.New()
	otherUserID := uuid.New()
	key := "notification:history:" + userID.String()
	t.Cleanup(func() {
		client.Del(context.Background(), key)
	})

	base := time.Now().UTC().Truncate(time.Second)
	records := []models.Record{
		{
			ID:            uuid.NewString(),
			UserID:        userID,
			Channel:       models.ChannelEmail,
			Recipient:     "ana@example.com",
			Subject:       "one",
			Body:          "corpo um",
			SourceEventID: uuid.NewString(),
			SentAt:        base,
		},
		{
			ID:            uuid.NewString(),
			UserID:        userID,
			Channel:       models.ChannelSMS,
			Recipient:     "+5511900000000",
			Subject:       "two",
			Body:          "corpo dois",
			SourceEventID: uuid.NewString(),
			SentAt:        base.Add(time.Minute),
		},
		{
			ID:            uuid.NewString(),
			UserID:        userID,
			Channel:       models.ChannelPush,
			Recipient:     "device-token",
			Subject:       "three",
			Body:          "corpo tres",
			SourceEventID: uuid.NewString(),
			SentAt:        base.Add(2 * time.Minute),
		},
	}

	for _, record := range records {
		require.NoError(t, history.Append(context.Background(), record))
	}

	newest, err := history.List(context.Background(), userID, 10)
	require.NoError(t, err)
	require.Len(t, newest, 2)
	require.Equal(t, records[2], newest[0])
	require.Equal(t, records[1], newest[1])

	limited, err := history.List(context.Background(), userID, 1)
	require.NoError(t, err)
	require.Len(t, limited, 1)
	require.Equal(t, records[2], limited[0])

	empty, err := history.List(context.Background(), otherUserID, 10)
	require.NoError(t, err)
	require.Empty(t, empty)
}
