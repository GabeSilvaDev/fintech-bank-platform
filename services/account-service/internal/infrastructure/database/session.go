package database

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/account-service/internal/contracts"
	"github.com/fintech-bank-platform/pkg/cassandra"
)

func NewSession(cfg contracts.CassandraConfig, keyspace string) (*gocql.Session, error) {
	consistency, err := gocql.ParseConsistencyWrapper(cfg.Consistency)
	if err != nil {
		return nil, err
	}

	cluster := gocql.NewCluster(cfg.Hosts...)
	cluster.Keyspace = keyspace
	cluster.Timeout = cfg.Timeout
	cluster.ConnectTimeout = cfg.ConnectTimeout
	cluster.Consistency = consistency
	cluster.SerialConsistency = gocql.LocalSerial

	return cluster.CreateSession()
}

func Ping(session *gocql.Session) func(context.Context) error {
	return func(ctx context.Context) error {
		return session.Query("SELECT now() FROM system.local").WithContext(ctx).Exec()
	}
}

type executor struct {
	session *gocql.Session
}

func (e executor) Exec(ctx context.Context, statement string, values ...interface{}) error {
	return e.session.Query(statement, values...).WithContext(ctx).Exec()
}

func (e executor) Versions(ctx context.Context, keyspace string) ([]int, error) {
	iter := e.session.Query(fmt.Sprintf("SELECT version FROM %s.schema_migrations", keyspace)).WithContext(ctx).Iter()
	var versions []int
	var version int
	for iter.Scan(&version) {
		versions = append(versions, version)
	}
	return versions, iter.Close()
}

func NewMigrator(session *gocql.Session, keyspace string, files fs.FS) *cassandra.Migrator {
	return cassandra.NewMigrator(executor{session: session}, keyspace, files)
}
