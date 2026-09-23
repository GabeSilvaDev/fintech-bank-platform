package unit

import (
	"testing"
	"time"

	"github.com/fintech-bank-platform/transaction-service/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestConfigDefaults(t *testing.T) {
	cfg, err := config.New()

	assert.NoError(t, err)
	assert.Equal(t, "0.0.0.0:8083", cfg.Server.Address())
	assert.Equal(t, []string{"localhost:9092"}, cfg.Kafka.Brokers)
	assert.Equal(t, "transaction-service", cfg.Kafka.GroupID)
	assert.Equal(t, 5*time.Second, cfg.Kafka.WriteTimeout)
	assert.Equal(t, 10*time.Millisecond, cfg.Kafka.BatchTimeout)
	assert.Equal(t, 20*time.Second, cfg.Kafka.PublishTimeout)
	assert.Equal(t, 3, cfg.Kafka.MaxAttempts)
	assert.Equal(t, []time.Duration{200 * time.Millisecond, time.Second, 5 * time.Second}, cfg.Consumer.RetryBackoff)
	assert.Equal(t, 30*time.Second, cfg.Consumer.DrainTimeout)
	assert.Equal(t, []string{"localhost:9042"}, cfg.Cassandra.Hosts)
	assert.Equal(t, "fintech_transactions", cfg.Cassandra.Keyspace)
	assert.Equal(t, "LOCAL_QUORUM", cfg.Cassandra.Consistency)
	assert.Equal(t, 10*time.Second, cfg.Cassandra.Timeout)
	assert.Equal(t, 10*time.Second, cfg.Cassandra.ConnectTimeout)
	assert.Equal(t, "migrations", cfg.Cassandra.MigrationsPath)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.False(t, cfg.Log.Pretty)
	assert.True(t, cfg.Sweeper.Enabled)
	assert.Equal(t, time.Minute, cfg.Sweeper.Interval)
	assert.Equal(t, 5*time.Minute, cfg.Sweeper.StaleAfter)
	assert.Equal(t, 100, cfg.Sweeper.Batch)
	assert.Equal(t, 30, cfg.Startup.Attempts)
	assert.Equal(t, 2*time.Second, cfg.Startup.Delay)
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("SERVER_HOST", "127.0.0.1")
	t.Setenv("SERVER_PORT", "9000")
	t.Setenv("KAFKA_BROKERS", "k1:9092, k2:9092")
	t.Setenv("KAFKA_GROUP_ID", "tx-test")
	t.Setenv("KAFKA_MAX_ATTEMPTS", "0")
	t.Setenv("CONSUMER_RETRY_BACKOFF", "10ms,20ms")
	t.Setenv("CONSUMER_DRAIN_TIMEOUT", "45s")
	t.Setenv("CASSANDRA_HOSTS", "c1:9042")
	t.Setenv("CASSANDRA_KEYSPACE", "ks")
	t.Setenv("CASSANDRA_CONSISTENCY", "ONE")
	t.Setenv("CASSANDRA_MIGRATIONS_PATH", "/tmp/m")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_PRETTY", "true")
	t.Setenv("SWEEPER_ENABLED", "false")
	t.Setenv("SWEEPER_INTERVAL", "30s")
	t.Setenv("SWEEPER_STALE_AFTER", "2m")
	t.Setenv("SWEEPER_BATCH", "0")
	t.Setenv("STARTUP_RETRY_ATTEMPTS", "5")
	t.Setenv("STARTUP_RETRY_DELAY", "500ms")

	cfg, _ := config.New()

	assert.Equal(t, "127.0.0.1:9000", cfg.Server.Address())
	assert.Equal(t, []string{"k1:9092", "k2:9092"}, cfg.Kafka.Brokers)
	assert.Equal(t, "tx-test", cfg.Kafka.GroupID)
	assert.Equal(t, 3, cfg.Kafka.MaxAttempts)
	assert.Equal(t, []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}, cfg.Consumer.RetryBackoff)
	assert.Equal(t, 45*time.Second, cfg.Consumer.DrainTimeout)
	assert.Equal(t, []string{"c1:9042"}, cfg.Cassandra.Hosts)
	assert.Equal(t, "ks", cfg.Cassandra.Keyspace)
	assert.Equal(t, "ONE", cfg.Cassandra.Consistency)
	assert.Equal(t, "/tmp/m", cfg.Cassandra.MigrationsPath)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.True(t, cfg.Log.Pretty)
	assert.False(t, cfg.Sweeper.Enabled)
	assert.Equal(t, 30*time.Second, cfg.Sweeper.Interval)
	assert.Equal(t, 2*time.Minute, cfg.Sweeper.StaleAfter)
	assert.Equal(t, 100, cfg.Sweeper.Batch)
	assert.Equal(t, 5, cfg.Startup.Attempts)
	assert.Equal(t, 500*time.Millisecond, cfg.Startup.Delay)
}

func TestConfigSweeperNonPositiveDurationsFallBackToDefaults(t *testing.T) {
	t.Setenv("SWEEPER_INTERVAL", "0s")
	t.Setenv("SWEEPER_STALE_AFTER", "-1m")

	cfg, err := config.New()

	assert.NoError(t, err)
	assert.Equal(t, time.Minute, cfg.Sweeper.Interval)
	assert.Equal(t, 5*time.Minute, cfg.Sweeper.StaleAfter)
}

func TestConfigStartupInvalidValuesFallBackToDefaults(t *testing.T) {
	t.Setenv("STARTUP_RETRY_ATTEMPTS", "0")
	t.Setenv("STARTUP_RETRY_DELAY", "-1s")

	cfg, err := config.New()

	assert.NoError(t, err)
	assert.Equal(t, 30, cfg.Startup.Attempts)
	assert.Equal(t, 2*time.Second, cfg.Startup.Delay)
}
