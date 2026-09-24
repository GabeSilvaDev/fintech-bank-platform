//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/require"
)

func redisClient(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		t.Skip("REDIS_ADDR must be set")
	}

	client := redis.NewClient(&redis.Options{Addr: addr, DB: 15})
	t.Cleanup(func() {
		client.Close()
	})
	return client
}

func groupAtTail(t *testing.T, addrs []string, topic string) string {
	t.Helper()
	group := "it-" + uuid.NewString()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := &kafka.Client{Addr: kafka.TCP(addrs...), Timeout: 10 * time.Second}

	meta, err := client.Metadata(ctx, &kafka.MetadataRequest{Topics: []string{topic}})
	require.NoError(t, err)
	require.Len(t, meta.Topics, 1)
	require.NoError(t, meta.Topics[0].Error)
	require.NotEmpty(t, meta.Topics[0].Partitions)
	requests := make([]kafka.OffsetRequest, 0, len(meta.Topics[0].Partitions))
	for _, partition := range meta.Topics[0].Partitions {
		requests = append(requests, kafka.LastOffsetOf(partition.ID))
	}

	listed, err := client.ListOffsets(ctx, &kafka.ListOffsetsRequest{Topics: map[string][]kafka.OffsetRequest{topic: requests}})
	require.NoError(t, err)
	require.Len(t, listed.Topics[topic], len(requests))
	commits := make([]kafka.OffsetCommit, 0, len(requests))
	for _, partition := range listed.Topics[topic] {
		require.NoError(t, partition.Error)
		commits = append(commits, kafka.OffsetCommit{Partition: partition.Partition, Offset: partition.LastOffset})
	}

	committed, err := client.OffsetCommit(ctx, &kafka.OffsetCommitRequest{GroupID: group, GenerationID: -1, Topics: map[string][]kafka.OffsetCommit{topic: commits}})
	require.NoError(t, err)
	require.Len(t, committed.Topics[topic], len(commits))
	for _, partition := range committed.Topics[topic] {
		require.NoError(t, partition.Error)
	}
	return group
}
