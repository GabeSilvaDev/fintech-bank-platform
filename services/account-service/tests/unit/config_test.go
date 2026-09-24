package unit

import (
	"runtime"
	"testing"
	"time"

	"github.com/fintech-bank-platform/account-service/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestConfigDefaults(t *testing.T) {
	cfg, err := config.New()

	assert.NoError(t, err)
	assert.Equal(t, "0.0.0.0:8082", cfg.Server.Address())
	assert.Equal(t, []string{"localhost:9092"}, cfg.Kafka.Brokers)
	assert.Equal(t, "account-service", cfg.Kafka.GroupID)
	assert.Equal(t, 5*time.Second, cfg.Kafka.WriteTimeout)
	assert.Equal(t, 10*time.Millisecond, cfg.Kafka.BatchTimeout)
	assert.Equal(t, 20*time.Second, cfg.Kafka.PublishTimeout)
	assert.Equal(t, 3, cfg.Kafka.MaxAttempts)
	assert.Equal(t, []time.Duration{200 * time.Millisecond, time.Second, 5 * time.Second}, cfg.Consumer.RetryBackoff)
	assert.Equal(t, 30*time.Second, cfg.Consumer.DrainTimeout)
	assert.Equal(t, []string{"localhost:9042"}, cfg.Cassandra.Hosts)
	assert.Equal(t, "fintech_accounts", cfg.Cassandra.Keyspace)
	assert.Equal(t, "LOCAL_QUORUM", cfg.Cassandra.Consistency)
	assert.Equal(t, 10*time.Second, cfg.Cassandra.Timeout)
	assert.Equal(t, 10*time.Second, cfg.Cassandra.ConnectTimeout)
	assert.Equal(t, "migrations", cfg.Cassandra.MigrationsPath)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.False(t, cfg.Log.Pretty)
	assert.Equal(t, 30, cfg.Startup.Attempts)
	assert.Equal(t, 2*time.Second, cfg.Startup.Delay)
	assert.Equal(t, 2*runtime.GOMAXPROCS(0), cfg.Identity.HashConcurrency)
	assert.Equal(t, 720*time.Hour, cfg.Session.RefreshTokenTTL)
	assert.Equal(t, 2160*time.Hour, cfg.Session.FamilyMaxAge)
	assert.Equal(t, 5, cfg.Lockout.MaxFailures)
	assert.Equal(t, 15*time.Minute, cfg.Lockout.Window)
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("SERVER_HOST", "127.0.0.1")
	t.Setenv("SERVER_PORT", "9000")
	t.Setenv("KAFKA_BROKERS", "k1:9092, k2:9092")
	t.Setenv("KAFKA_GROUP_ID", "acc-test")
	t.Setenv("KAFKA_MAX_ATTEMPTS", "0")
	t.Setenv("CONSUMER_RETRY_BACKOFF", "10ms,20ms")
	t.Setenv("CONSUMER_DRAIN_TIMEOUT", "45s")
	t.Setenv("CASSANDRA_HOSTS", "c1:9042")
	t.Setenv("CASSANDRA_KEYSPACE", "ks")
	t.Setenv("CASSANDRA_CONSISTENCY", "ONE")
	t.Setenv("CASSANDRA_MIGRATIONS_PATH", "/tmp/m")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_PRETTY", "true")
	t.Setenv("STARTUP_RETRY_ATTEMPTS", "5")
	t.Setenv("STARTUP_RETRY_DELAY", "500ms")

	cfg, _ := config.New()

	assert.Equal(t, "127.0.0.1:9000", cfg.Server.Address())
	assert.Equal(t, []string{"k1:9092", "k2:9092"}, cfg.Kafka.Brokers)
	assert.Equal(t, "acc-test", cfg.Kafka.GroupID)
	assert.Equal(t, 3, cfg.Kafka.MaxAttempts)
	assert.Equal(t, []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}, cfg.Consumer.RetryBackoff)
	assert.Equal(t, 45*time.Second, cfg.Consumer.DrainTimeout)
	assert.Equal(t, []string{"c1:9042"}, cfg.Cassandra.Hosts)
	assert.Equal(t, "ks", cfg.Cassandra.Keyspace)
	assert.Equal(t, "ONE", cfg.Cassandra.Consistency)
	assert.Equal(t, "/tmp/m", cfg.Cassandra.MigrationsPath)
	assert.Equal(t, "debug", cfg.Log.Level)
	assert.True(t, cfg.Log.Pretty)
	assert.Equal(t, 5, cfg.Startup.Attempts)
	assert.Equal(t, 500*time.Millisecond, cfg.Startup.Delay)
}

func TestConfigStartupInvalidValuesFallBackToDefaults(t *testing.T) {
	t.Setenv("STARTUP_RETRY_ATTEMPTS", "0")
	t.Setenv("STARTUP_RETRY_DELAY", "-1s")

	cfg, err := config.New()

	assert.NoError(t, err)
	assert.Equal(t, 30, cfg.Startup.Attempts)
	assert.Equal(t, 2*time.Second, cfg.Startup.Delay)
}

func TestConfigObservabilityDefaults(t *testing.T) {
	cfg, err := config.New()

	assert.NoError(t, err)
	assert.True(t, cfg.Observability.MetricsEnabled)
	assert.Empty(t, cfg.Observability.OTLPEndpoint)
	assert.Equal(t, 1.0, cfg.Observability.SampleRatio)
}

func TestConfigObservabilityFromEnv(t *testing.T) {
	t.Setenv("METRICS_ENABLED", "false")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")
	t.Setenv("OTEL_SAMPLER_RATIO", "0.5")

	cfg, _ := config.New()

	assert.False(t, cfg.Observability.MetricsEnabled)
	assert.Equal(t, "http://collector:4318", cfg.Observability.OTLPEndpoint)
	assert.Equal(t, 0.5, cfg.Observability.SampleRatio)
}

func TestConfigObservabilityInvalidSampleRatioFallsBack(t *testing.T) {
	t.Setenv("OTEL_SAMPLER_RATIO", "not-a-number")

	cfg, _ := config.New()

	assert.Equal(t, 1.0, cfg.Observability.SampleRatio)
}

func TestConfigIdentityHashConcurrencyFromEnv(t *testing.T) {
	t.Setenv("IDENTITY_HASH_CONCURRENCY", "3")

	cfg, err := config.New()

	assert.NoError(t, err)
	assert.Equal(t, 3, cfg.Identity.HashConcurrency)
}

func TestConfigIdentityHashConcurrencyBelowOneFallsBack(t *testing.T) {
	for _, value := range []string{"0", "-2", "many"} {
		t.Setenv("IDENTITY_HASH_CONCURRENCY", value)

		cfg, _ := config.New()

		assert.Equal(t, 2*runtime.GOMAXPROCS(0), cfg.Identity.HashConcurrency, value)
	}
}

func TestConfigRefreshTokenTTLFromEnv(t *testing.T) {
	t.Setenv("REFRESH_TOKEN_TTL", "36h")

	cfg, err := config.New()

	assert.NoError(t, err)
	assert.Equal(t, 36*time.Hour, cfg.Session.RefreshTokenTTL)
}

func TestConfigRefreshTokenTTLInvalidValuesFallBack(t *testing.T) {
	for _, value := range []string{"0", "-1h", "forever"} {
		t.Setenv("REFRESH_TOKEN_TTL", value)

		cfg, _ := config.New()

		assert.Equal(t, 720*time.Hour, cfg.Session.RefreshTokenTTL, value)
	}
}

func TestConfigRefreshFamilyMaxAgeFromEnv(t *testing.T) {
	t.Setenv("REFRESH_FAMILY_MAX_AGE", "240h")

	cfg, err := config.New()

	assert.NoError(t, err)
	assert.Equal(t, 240*time.Hour, cfg.Session.FamilyMaxAge)
}

func TestConfigRefreshFamilyMaxAgeInvalidValuesFallBack(t *testing.T) {
	for _, value := range []string{"0", "-1h", "forever"} {
		t.Setenv("REFRESH_FAMILY_MAX_AGE", value)

		cfg, _ := config.New()

		assert.Equal(t, 2160*time.Hour, cfg.Session.FamilyMaxAge, value)
	}
}

func TestConfigLoginLockoutFromEnv(t *testing.T) {
	t.Setenv("LOGIN_MAX_FAILURES", "3")
	t.Setenv("LOGIN_LOCKOUT_WINDOW", "30m")

	cfg, _ := config.New()

	assert.Equal(t, 3, cfg.Lockout.MaxFailures)
	assert.Equal(t, 30*time.Minute, cfg.Lockout.Window)
}

func TestConfigLoginLockoutInvalidValuesFallBack(t *testing.T) {
	for _, value := range []string{"0", "-1", "abc"} {
		t.Setenv("LOGIN_MAX_FAILURES", value)
		t.Setenv("LOGIN_LOCKOUT_WINDOW", value)

		cfg, _ := config.New()

		assert.Equal(t, 5, cfg.Lockout.MaxFailures, value)
		assert.Equal(t, 15*time.Minute, cfg.Lockout.Window, value)
	}
}
