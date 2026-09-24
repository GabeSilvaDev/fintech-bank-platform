//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/fintech-bank-platform/transaction-service/internal/infrastructure/database"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestMigratorAppliesOnceAndIsIdempotent(t *testing.T) {
	cfg := cassandraConfig(t)
	keyspace := "it_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	session, err := database.NewSession(cfg, "")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = session.Query(fmt.Sprintf("DROP KEYSPACE IF EXISTS %s", keyspace)).Exec()
		session.Close()
	})
	migrator := database.NewMigrator(session, keyspace, os.DirFS("../../migrations"))

	applied, err := migrator.Up(context.Background())
	require.NoError(t, err)
	require.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8}, applied)

	again, err := migrator.Up(context.Background())
	require.NoError(t, err)
	require.Empty(t, again)

	var count int
	require.NoError(t, session.Query("SELECT count(*) FROM system_schema.tables WHERE keyspace_name = ?", keyspace).Scan(&count))
	require.Equal(t, 7, count)

	var gcGrace int
	require.NoError(t, session.Query("SELECT gc_grace_seconds FROM system_schema.tables WHERE keyspace_name = ? AND table_name = ?", keyspace, "open_transactions").Scan(&gcGrace))
	require.Equal(t, 3600, gcGrace)

	require.NoError(t, database.Ping(session)(context.Background()))
}
