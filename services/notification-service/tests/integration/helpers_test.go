//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
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
	client := &kafka.Client{Addr: kafka.TCP(addrs...), Timeout: 10 * time.Second}
	deadline := time.Now().Add(30 * time.Second)
	for attempt := 1; ; attempt++ {
		err := commitTail(client, group, topic)
		if err == nil {
			return group
		}
		if time.Now().After(deadline) {
			t.Fatalf("could not position group %s at the tail of %s after %d attempts: %v", group, topic, attempt, err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func commitTail(client *kafka.Client, group, topic string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	meta, err := client.Metadata(ctx, &kafka.MetadataRequest{Topics: []string{topic}})
	if err != nil {
		return fmt.Errorf("metadata: %w", err)
	}
	if len(meta.Topics) != 1 {
		return fmt.Errorf("metadata: %d topics returned", len(meta.Topics))
	}
	if meta.Topics[0].Error != nil {
		return fmt.Errorf("metadata: %w", meta.Topics[0].Error)
	}
	if len(meta.Topics[0].Partitions) == 0 {
		return fmt.Errorf("metadata: no partitions")
	}
	requests := make([]kafka.OffsetRequest, 0, len(meta.Topics[0].Partitions))
	for _, partition := range meta.Topics[0].Partitions {
		if partition.Error != nil {
			return fmt.Errorf("metadata: partition %d: %w", partition.ID, partition.Error)
		}
		requests = append(requests, kafka.LastOffsetOf(partition.ID))
	}

	listed, err := client.ListOffsets(ctx, &kafka.ListOffsetsRequest{Topics: map[string][]kafka.OffsetRequest{topic: requests}})
	if err != nil {
		return fmt.Errorf("list offsets: %w", err)
	}
	if len(listed.Topics[topic]) != len(requests) {
		return fmt.Errorf("list offsets: %d of %d partitions returned", len(listed.Topics[topic]), len(requests))
	}
	commits := make([]kafka.OffsetCommit, 0, len(requests))
	for _, partition := range listed.Topics[topic] {
		if partition.Error != nil {
			return fmt.Errorf("list offsets: partition %d: %w", partition.Partition, partition.Error)
		}
		commits = append(commits, kafka.OffsetCommit{Partition: partition.Partition, Offset: partition.LastOffset})
	}

	committed, err := client.OffsetCommit(ctx, &kafka.OffsetCommitRequest{GroupID: group, GenerationID: -1, Topics: map[string][]kafka.OffsetCommit{topic: commits}})
	if err != nil {
		return fmt.Errorf("offset commit: %w", err)
	}
	if len(committed.Topics[topic]) != len(commits) {
		return fmt.Errorf("offset commit: %d of %d partitions acknowledged", len(committed.Topics[topic]), len(commits))
	}
	for _, partition := range committed.Topics[topic] {
		if partition.Error != nil {
			return fmt.Errorf("offset commit: partition %d: %w", partition.Partition, partition.Error)
		}
	}
	return nil
}
