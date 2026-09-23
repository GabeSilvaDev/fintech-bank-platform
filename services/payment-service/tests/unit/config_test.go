package unit

import (
	"testing"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestConfigDefaults(t *testing.T) {
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "s3cret")

	cfg, err := config.New()

	assert.NoError(t, err)
	assert.Equal(t, "0.0.0.0:8084", cfg.Server.Address())
	assert.Equal(t, []string{"localhost:9092"}, cfg.Kafka.Brokers)
	assert.Equal(t, "payment-service", cfg.Kafka.GroupID)
	assert.Equal(t, 5*time.Second, cfg.Kafka.WriteTimeout)
	assert.Equal(t, 10*time.Millisecond, cfg.Kafka.BatchTimeout)
	assert.Equal(t, 20*time.Second, cfg.Kafka.PublishTimeout)
	assert.Equal(t, 3, cfg.Kafka.MaxAttempts)
	assert.Equal(t, []time.Duration{200 * time.Millisecond, time.Second, 5 * time.Second}, cfg.Consumer.RetryBackoff)
	assert.Equal(t, 30*time.Second, cfg.Consumer.DrainTimeout)
	assert.Equal(t, []string{"localhost:9042"}, cfg.Cassandra.Hosts)
	assert.Equal(t, "fintech_payments", cfg.Cassandra.Keyspace)
	assert.Equal(t, "LOCAL_QUORUM", cfg.Cassandra.Consistency)
	assert.Equal(t, 10*time.Second, cfg.Cassandra.Timeout)
	assert.Equal(t, 10*time.Second, cfg.Cassandra.ConnectTimeout)
	assert.Equal(t, "migrations", cfg.Cassandra.MigrationsPath)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.False(t, cfg.Log.Pretty)
	assert.Equal(t, "s3cret", cfg.Payment.WebhookSecret)
	assert.Equal(t, "http://localhost:8084/webhooks/gateway", cfg.Payment.WebhookURL)
	assert.Equal(t, 5*time.Minute, cfg.Payment.WebhookTolerance)
	assert.Equal(t, 2*time.Second, cfg.Payment.SettlementDelay)
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "s3cret")
	t.Setenv("SERVER_HOST", "127.0.0.1")
	t.Setenv("SERVER_PORT", "9000")
	t.Setenv("KAFKA_BROKERS", "k1:9092, k2:9092")
	t.Setenv("KAFKA_GROUP_ID", "pay-test")
	t.Setenv("KAFKA_MAX_ATTEMPTS", "0")
	t.Setenv("CONSUMER_RETRY_BACKOFF", "10ms,20ms")
	t.Setenv("CONSUMER_DRAIN_TIMEOUT", "45s")
	t.Setenv("CASSANDRA_HOSTS", "c1:9042")
	t.Setenv("CASSANDRA_KEYSPACE", "ks")
	t.Setenv("CASSANDRA_CONSISTENCY", "ONE")
	t.Setenv("CASSANDRA_MIGRATIONS_PATH", "/tmp/m")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_PRETTY", "true")
	t.Setenv("PAYMENT_WEBHOOK_URL", "http://svc/hook")
	t.Setenv("PAYMENT_WEBHOOK_TOLERANCE", "1m")
	t.Setenv("PAYMENT_SETTLEMENT_DELAY", "10ms")

	cfg, _ := config.New()

	assert.Equal(t, "127.0.0.1:9000", cfg.Server.Address())
	assert.Equal(t, []string{"k1:9092", "k2:9092"}, cfg.Kafka.Brokers)
	assert.Equal(t, "pay-test", cfg.Kafka.GroupID)
	assert.Equal(t, 3, cfg.Kafka.MaxAttempts)
	assert.Equal(t, []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}, cfg.Consumer.RetryBackoff)
	assert.Equal(t, 45*time.Second, cfg.Consumer.DrainTimeout)
	assert.Equal(t, []string{"c1:9042"}, cfg.Cassandra.Hosts)
	assert.Equal(t, "ks", cfg.Cassandra.Keyspace)
	assert.Equal(t, "ONE", cfg.Cassandra.Consistency)
	assert.Equal(t, "/tmp/m", cfg.Cassandra.MigrationsPath)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.True(t, cfg.Log.Pretty)
	assert.Equal(t, "http://svc/hook", cfg.Payment.WebhookURL)
	assert.Equal(t, time.Minute, cfg.Payment.WebhookTolerance)
	assert.Equal(t, 10*time.Millisecond, cfg.Payment.SettlementDelay)
}

func TestConfigRequiresAWebhookSecret(t *testing.T) {
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "")
	_, err := config.New()
	assert.EqualError(t, err, "PAYMENT_WEBHOOK_SECRET is required")
}
