package unit

import (
	"testing"
	"time"

	"github.com/fintech-bank-platform/payment-service/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestConfigDefaults(t *testing.T) {
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "s3cret-s3cret-s3cret")

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
	assert.Equal(t, "s3cret-s3cret-s3cret", cfg.Payment.WebhookSecret)
	assert.Equal(t, "http://localhost:8084/webhooks/gateway", cfg.Payment.WebhookURL)
	assert.Equal(t, 5*time.Minute, cfg.Payment.WebhookTolerance)
	assert.Equal(t, 2*time.Second, cfg.Payment.SettlementDelay)
	assert.True(t, cfg.Sweeper.Enabled)
	assert.Equal(t, time.Minute, cfg.Sweeper.Interval)
	assert.Equal(t, 5*time.Minute, cfg.Sweeper.StaleAfter)
	assert.Equal(t, 24*time.Hour, cfg.Sweeper.MaxAge)
	assert.Equal(t, 100, cfg.Sweeper.Batch)
	assert.Equal(t, 24*time.Hour, cfg.Sweeper.FullScanInterval)
	assert.Equal(t, 30, cfg.Startup.Attempts)
	assert.Equal(t, 2*time.Second, cfg.Startup.Delay)
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "s3cret-s3cret-s3cret")
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
	t.Setenv("SWEEPER_ENABLED", "false")
	t.Setenv("SWEEPER_INTERVAL", "30s")
	t.Setenv("SWEEPER_STALE_AFTER", "2m")
	t.Setenv("SWEEPER_MAX_AGE", "12h")
	t.Setenv("SWEEPER_BATCH", "0")
	t.Setenv("SWEEPER_FULL_SCAN_INTERVAL", "6h")
	t.Setenv("STARTUP_RETRY_ATTEMPTS", "5")
	t.Setenv("STARTUP_RETRY_DELAY", "500ms")

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
	assert.False(t, cfg.Sweeper.Enabled)
	assert.Equal(t, 30*time.Second, cfg.Sweeper.Interval)
	assert.Equal(t, 2*time.Minute, cfg.Sweeper.StaleAfter)
	assert.Equal(t, 12*time.Hour, cfg.Sweeper.MaxAge)
	assert.Equal(t, 100, cfg.Sweeper.Batch)
	assert.Equal(t, 6*time.Hour, cfg.Sweeper.FullScanInterval)
	assert.Equal(t, 5, cfg.Startup.Attempts)
	assert.Equal(t, 500*time.Millisecond, cfg.Startup.Delay)
}

func TestConfigSweeperNonPositiveDurationsFallBackToDefaults(t *testing.T) {
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "s3cret-s3cret-s3cret")
	t.Setenv("SWEEPER_INTERVAL", "0s")
	t.Setenv("SWEEPER_STALE_AFTER", "-1m")
	t.Setenv("SWEEPER_MAX_AGE", "0s")

	cfg, err := config.New()

	assert.NoError(t, err)
	assert.Equal(t, time.Minute, cfg.Sweeper.Interval)
	assert.Equal(t, 5*time.Minute, cfg.Sweeper.StaleAfter)
	assert.Equal(t, 24*time.Hour, cfg.Sweeper.MaxAge)
}

func TestConfigSweeperFullScanIntervalZeroDisablesAndNegativeFallsBack(t *testing.T) {
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "s3cret-s3cret-s3cret")
	t.Setenv("SWEEPER_FULL_SCAN_INTERVAL", "0s")

	cfg, err := config.New()

	assert.NoError(t, err)
	assert.Equal(t, time.Duration(0), cfg.Sweeper.FullScanInterval)

	t.Setenv("SWEEPER_FULL_SCAN_INTERVAL", "-1h")

	cfg, err = config.New()

	assert.NoError(t, err)
	assert.Equal(t, 24*time.Hour, cfg.Sweeper.FullScanInterval)
}

func TestConfigSweeperMaxAgeMustBeShorterThanTheBalanceOperationRetention(t *testing.T) {
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "s3cret-s3cret-s3cret")
	for _, value := range []string{"720h", "721h", "1000h"} {
		t.Setenv("SWEEPER_MAX_AGE", value)

		cfg, err := config.New()

		assert.Nil(t, cfg, value)
		assert.EqualError(t, err, "SWEEPER_MAX_AGE must be shorter than 720h, the balance operation retention", value)
	}

	t.Setenv("SWEEPER_MAX_AGE", "719h")
	cfg, err := config.New()

	assert.NoError(t, err)
	assert.Equal(t, 719*time.Hour, cfg.Sweeper.MaxAge)
}

func TestConfigStartupInvalidValuesFallBackToDefaults(t *testing.T) {
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "s3cret-s3cret-s3cret")
	t.Setenv("STARTUP_RETRY_ATTEMPTS", "0")
	t.Setenv("STARTUP_RETRY_DELAY", "-1s")

	cfg, err := config.New()

	assert.NoError(t, err)
	assert.Equal(t, 30, cfg.Startup.Attempts)
	assert.Equal(t, 2*time.Second, cfg.Startup.Delay)
}

func TestConfigRequiresAWebhookSecret(t *testing.T) {
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "")
	_, err := config.New()
	assert.EqualError(t, err, "PAYMENT_WEBHOOK_SECRET is required")
}

func TestConfigRequiresAStrongWebhookSecret(t *testing.T) {
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "fifteen-chars-x")
	_, err := config.New()
	assert.EqualError(t, err, "PAYMENT_WEBHOOK_SECRET must have at least 16 characters")

	t.Setenv("PAYMENT_WEBHOOK_SECRET", "sixteen-chars-xx")
	cfg, err := config.New()
	assert.NoError(t, err)
	assert.Equal(t, "sixteen-chars-xx", cfg.Payment.WebhookSecret)
}

func TestConfigObservabilityDefaults(t *testing.T) {
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "s3cret-s3cret-s3cret")

	cfg, err := config.New()

	assert.NoError(t, err)
	assert.True(t, cfg.Observability.MetricsEnabled)
	assert.Empty(t, cfg.Observability.OTLPEndpoint)
	assert.Equal(t, 1.0, cfg.Observability.SampleRatio)
}

func TestConfigObservabilityFromEnv(t *testing.T) {
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "s3cret-s3cret-s3cret")
	t.Setenv("METRICS_ENABLED", "false")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")
	t.Setenv("OTEL_SAMPLER_RATIO", "0.5")

	cfg, _ := config.New()

	assert.False(t, cfg.Observability.MetricsEnabled)
	assert.Equal(t, "http://collector:4318", cfg.Observability.OTLPEndpoint)
	assert.Equal(t, 0.5, cfg.Observability.SampleRatio)
}

func TestConfigObservabilityInvalidSampleRatioFallsBack(t *testing.T) {
	t.Setenv("PAYMENT_WEBHOOK_SECRET", "s3cret-s3cret-s3cret")
	t.Setenv("OTEL_SAMPLER_RATIO", "not-a-number")

	cfg, _ := config.New()

	assert.Equal(t, 1.0, cfg.Observability.SampleRatio)
}
