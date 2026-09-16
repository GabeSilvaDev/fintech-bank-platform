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
	"github.com/fintech-bank-platform/account-service/internal/contracts"
	"github.com/fintech-bank-platform/account-service/internal/infrastructure/database"
	"github.com/google/uuid"
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
