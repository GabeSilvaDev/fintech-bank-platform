package database

import (
	"context"

	"github.com/apache/cassandra-gocql-driver/v2"
	"github.com/fintech-bank-platform/account-service/internal/contracts"
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
