//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/transaction-service/internal/contracts"
	"github.com/fintech-bank-platform/transaction-service/internal/infrastructure/database"
	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/require"
)

func cassandraConfig(t *testing.T) contracts.CassandraConfig {
	raw := os.Getenv("CASSANDRA_HOSTS")
	if raw == "" {
		t.Skip("CASSANDRA_HOSTS not set")
	}
	return contracts.CassandraConfig{Hosts: strings.Split(raw, ","), Consistency: "LOCAL_QUORUM", Timeout: 20 * time.Second, ConnectTimeout: 20 * time.Second}
}

func throwawayKeyspace(t *testing.T) (*gocql.Session, string) {
	cfg := cassandraConfig(t)
	keyspace := "it_" + strings.ReplaceAll(uuid.NewString(), "-", "")

	bootstrap, err := database.NewSession(cfg, "")
	require.NoError(t, err)
	migrator := database.NewMigrator(bootstrap, keyspace, os.DirFS("../../migrations"))
	_, err = migrator.Up(context.Background())
	require.NoError(t, err)
	bootstrap.Close()

	session, err := database.NewSession(cfg, keyspace)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = session.Query(fmt.Sprintf("DROP KEYSPACE %s", keyspace)).Exec()
		session.Close()
	})
	return session, keyspace
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
